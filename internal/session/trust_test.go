package session

import (
	"os"
	"path/filepath"
	"testing"
)

func tm(host, container string, ro, untrusted bool, src string) MountEntry {
	return MountEntry{
		HostPath:      host,
		ContainerPath: container,
		Readonly:      ro,
		Untrusted:     untrusted,
		SourcePath:    src,
	}
}

func ts(host, container, env string, untrusted bool, src string) SocketEntry {
	return SocketEntry{
		HostPath:      host,
		ContainerPath: container,
		EnvVar:        env,
		Untrusted:     untrusted,
		SourcePath:    src,
	}
}

// filterMounts is a thin shim over FilterTrusted for the mounts-only tests.
func filterMounts(mc *MountConfig, ws string) (*MountConfig, []MountEntry) {
	keptMC, dropped, _, _ := FilterTrusted(mc, nil, ws)
	return keptMC, dropped
}

func TestSourceFingerprint_OrderIndependentAndSensitive(t *testing.T) {
	a := []MountEntry{tm("/h1", "/c1", false, true, "s"), tm("/h2", "/c2", true, true, "s")}
	b := []MountEntry{tm("/h2", "/c2", true, true, "s"), tm("/h1", "/c1", false, true, "s")}
	if sourceFingerprint(a, nil) != sourceFingerprint(b, nil) {
		t.Error("fingerprint should be order-independent")
	}
	roChanged := []MountEntry{tm("/h1", "/c1", true, true, "s"), tm("/h2", "/c2", true, true, "s")}
	if sourceFingerprint(a, nil) == sourceFingerprint(roChanged, nil) {
		t.Error("fingerprint should change when a readonly flag changes")
	}
	removed := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	if sourceFingerprint(a, nil) == sourceFingerprint(removed, nil) {
		t.Error("fingerprint should change when a mount is removed")
	}
}

func TestSourceFingerprint_CoversSockets(t *testing.T) {
	mounts := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	sockA := []SocketEntry{ts("/run/a.sock", "/c/a.sock", "A_SOCK", true, "s")}
	sockB := []SocketEntry{ts("/run/b.sock", "/c/a.sock", "A_SOCK", true, "s")}
	if sourceFingerprint(mounts, nil) == sourceFingerprint(mounts, sockA) {
		t.Error("adding a socket should change the fingerprint")
	}
	if sourceFingerprint(mounts, sockA) == sourceFingerprint(mounts, sockB) {
		t.Error("changing a socket host path should change the fingerprint")
	}
	// Order-independent across both kinds.
	sockTwoA := []SocketEntry{
		ts("/run/a.sock", "/c/a.sock", "A", true, "s"),
		ts("/run/b.sock", "/c/b.sock", "B", true, "s"),
	}
	sockTwoB := []SocketEntry{
		ts("/run/b.sock", "/c/b.sock", "B", true, "s"),
		ts("/run/a.sock", "/c/a.sock", "A", true, "s"),
	}
	if sourceFingerprint(mounts, sockTwoA) != sourceFingerprint(mounts, sockTwoB) {
		t.Error("fingerprint should be order-independent across sockets")
	}
}

func TestFilterTrusted_GatesOnlyEscapingUntrusted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "sub"), "/c-in", false, true, src), // untrusted, in-workspace -> kept
		tm(outside, "/c-out", false, true, src),                 // untrusted, escaping    -> gated
		tm(outside, "/c-trusted-scope", false, false, ""),       // trusted scope, escaping -> kept
	}}

	kept, dropped := filterMounts(mc, ws)
	if len(dropped) != 1 || dropped[0].ContainerPath != "/c-out" {
		t.Fatalf("expected only the escaping untrusted mount dropped, got %+v", dropped)
	}
	if len(kept.Mounts) != 2 {
		t.Fatalf("expected 2 kept mounts, got %d", len(kept.Mounts))
	}
}

func TestFilterTrusted_GatesUntrustedSockets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/run/host.sock", "/c/host.sock", "BROKER", true, src), // untrusted -> gated
		ts("/run/trusted.sock", "/c/t.sock", "T", false, ""),      // trusted scope -> kept
	}}

	_, _, keptSC, dropped := FilterTrusted(nil, sc, ws)
	if len(dropped) != 1 || dropped[0].EnvVar != "BROKER" {
		t.Fatalf("expected the untrusted socket dropped, got %+v", dropped)
	}
	if len(keptSC.Sockets) != 1 || keptSC.Sockets[0].EnvVar != "T" {
		t.Fatalf("expected only the trusted-scope socket kept, got %+v", keptSC.Sockets)
	}
}

func TestFilterTrusted_TrustAllEnvBypasses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "1")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, src),
	}}
	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/run/host.sock", "/c/host.sock", "BROKER", true, src),
	}}
	_, droppedM, _, droppedS := FilterTrusted(mc, sc, ws)
	if len(droppedM) != 0 || len(droppedS) != 0 {
		t.Fatalf("COI_TRUST_ALL=1 should bypass gating, got droppedM=%+v droppedS=%+v", droppedM, droppedS)
	}
}

func TestTrustThenRevokeOnMountChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, dropped := filterMounts(mc, ws); len(dropped) != 1 {
		t.Fatal("escaping untrusted mount should be gated before trust")
	}

	sources, err := TrustSources(mc, nil, ws)
	if err != nil || len(sources) != 1 || sources[0] != src {
		t.Fatalf("TrustSources: sources=%v err=%v", sources, err)
	}

	if _, dropped := filterMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("mount should be allowed after trust")
	}

	// Adding another escaping mount changes the fingerprint -> trust no longer
	// matches -> the source is gated again (all its escaping mounts dropped).
	changed := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, src),
		tm(outside, "/c2", false, true, src),
	}}
	if _, dropped := filterMounts(changed, ws); len(dropped) != 2 {
		t.Fatalf("changed mount set should re-arm gating, got dropped=%d", len(dropped))
	}
}

func TestTrust_CombinedMountAndSocketSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}
	sc := &SocketConfig{Sockets: []SocketEntry{ts("/run/host.sock", "/c/host.sock", "BROKER", true, src)}}

	// Both gated before trust.
	_, dM, _, dS := FilterTrusted(mc, sc, ws)
	if len(dM) != 1 || len(dS) != 1 {
		t.Fatalf("both mount and socket should be gated before trust, dM=%d dS=%d", len(dM), len(dS))
	}

	// One approval covers both.
	sources, err := TrustSources(mc, sc, ws)
	if err != nil || len(sources) != 1 {
		t.Fatalf("TrustSources: sources=%v err=%v", sources, err)
	}
	_, dM, _, dS = FilterTrusted(mc, sc, ws)
	if len(dM) != 0 || len(dS) != 0 {
		t.Fatalf("both should be trusted after approval, dM=%d dS=%d", len(dM), len(dS))
	}

	// Changing the socket alone re-arms the combined fingerprint -> both gated.
	scChanged := &SocketConfig{Sockets: []SocketEntry{ts("/run/other.sock", "/c/host.sock", "BROKER", true, src)}}
	_, dM, _, dS = FilterTrusted(mc, scChanged, ws)
	if len(dM) != 1 || len(dS) != 1 {
		t.Fatalf("changing the socket should re-arm gating for the whole source, dM=%d dS=%d", len(dM), len(dS))
	}
}

func TestUntrustSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, err := TrustSources(mc, nil, ws); err != nil {
		t.Fatal(err)
	}
	if _, dropped := filterMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("should be trusted")
	}

	n, err := UntrustSources([]string{src})
	if err != nil || n != 1 {
		t.Fatalf("UntrustSources n=%d err=%v", n, err)
	}
	if _, dropped := filterMounts(mc, ws); len(dropped) != 1 {
		t.Fatal("should be gated again after untrust")
	}
}

func TestHostEscapesWorkspace_Symlinks(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	// In-workspace symlink pointing outside the workspace.
	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatal(err)
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "link")) {
		t.Error("in-workspace symlink to an outside dir must be detected as escaping")
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "link", "sub")) {
		t.Error("a path through an in-workspace symlink must be escaping")
	}

	// Dangling symlink whose target is outside (target does not exist).
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(ws, "dangling")); err != nil {
		t.Fatal(err)
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "dangling")) {
		t.Error("dangling symlink pointing outside must be escaping")
	}

	// Genuine in-workspace paths (existing and not-yet-existing) are not escaping.
	realDir := filepath.Join(ws, "realdir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if hostEscapesWorkspace(ws, realDir) {
		t.Error("a real in-workspace dir must not be escaping")
	}
	if hostEscapesWorkspace(ws, filepath.Join(ws, "notyet")) {
		t.Error("a non-existent in-workspace path must not be escaping")
	}
}

func TestFilterTrusted_SymlinkEscapeGated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(ws, ".coi", "config.toml")
	// HostPath is lexically inside the workspace but resolves outside via symlink.
	mc := &MountConfig{Mounts: []MountEntry{tm(filepath.Join(ws, "link"), "/c", false, true, src)}}
	_, dropped := filterMounts(mc, ws)
	if len(dropped) != 1 {
		t.Fatalf("symlink-escaping untrusted mount must be gated, dropped=%d", len(dropped))
	}
}

func TestFilterTrusted_GatesReadonlyEscaping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	// A READ-ONLY escaping untrusted mount still exfiltrates host data, so it
	// must be gated too.
	mc := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", true, true, filepath.Join(ws, ".coi", "config.toml")),
	}}
	if _, dropped := filterMounts(mc, ws); len(dropped) != 1 {
		t.Fatalf("read-only escaping untrusted mount must be gated, dropped=%d", len(dropped))
	}
}

func TestUntrustedSourcePaths(t *testing.T) {
	ws := t.TempDir()
	srcA := filepath.Join(ws, ".coi", "config.toml")
	srcB := filepath.Join(ws, ".coi", "profiles", "dev", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{
		tm("/x", "/cx", false, true, srcA),
		tm("/y", "/cy", false, true, srcA), // dup source
		tm("/w", "/cw", false, false, ""),  // trusted scope -> excluded
	}}
	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/z", "/cz", "Z", true, srcB),
		ts("/t", "/ct", "T", false, ""), // trusted scope -> excluded
	}}
	got := UntrustedSourcePaths(mc, sc)
	want := []string{srcA, srcB} // sorted: ".coi/config.toml" < ".coi/profiles/..."
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("UntrustedSourcePaths = %v, want sorted distinct %v", got, want)
	}
}

func TestFilterTrusted_NoEscapingIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "a"), "/a", false, true, filepath.Join(ws, ".coi", "config.toml")),
		tm("/anywhere", "/b", false, false, ""), // trusted scope
	}}
	kept, dropped := filterMounts(mc, ws)
	if len(dropped) != 0 || len(kept.Mounts) != 2 {
		t.Fatalf("no escaping untrusted mounts should mean no gating; dropped=%d kept=%d",
			len(dropped), len(kept.Mounts))
	}
}
