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

func tc(host, container, mode string, untrusted bool, src string) CredentialEntry {
	return CredentialEntry{
		HostPath:      host,
		ContainerPath: container,
		Mode:          mode,
		Untrusted:     untrusted,
		SourcePath:    src,
	}
}

// filterMounts is a thin shim over FilterTrusted for the mounts-only tests.
func filterMounts(mc *MountConfig, ws string) (*MountConfig, []MountEntry) {
	keptMC, dropped, _, _, _, _ := FilterTrusted(mc, nil, nil, ws)
	return keptMC, dropped
}

// filterCreds is a thin shim over FilterTrusted for the credentials-only tests.
func filterCreds(cc *CredentialConfig, ws string) (*CredentialConfig, []CredentialEntry) {
	_, _, _, _, keptCC, dropped := FilterTrusted(nil, nil, cc, ws)
	return keptCC, dropped
}

func TestSourceFingerprint_OrderIndependentAndSensitive(t *testing.T) {
	a := []MountEntry{tm("/h1", "/c1", false, true, "s"), tm("/h2", "/c2", true, true, "s")}
	b := []MountEntry{tm("/h2", "/c2", true, true, "s"), tm("/h1", "/c1", false, true, "s")}
	if sourceFingerprint(a, nil, nil) != sourceFingerprint(b, nil, nil) {
		t.Error("fingerprint should be order-independent")
	}
	roChanged := []MountEntry{tm("/h1", "/c1", true, true, "s"), tm("/h2", "/c2", true, true, "s")}
	if sourceFingerprint(a, nil, nil) == sourceFingerprint(roChanged, nil, nil) {
		t.Error("fingerprint should change when a readonly flag changes")
	}
	removed := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	if sourceFingerprint(a, nil, nil) == sourceFingerprint(removed, nil, nil) {
		t.Error("fingerprint should change when a mount is removed")
	}
}

func TestSourceFingerprint_CoversSockets(t *testing.T) {
	mounts := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	sockA := []SocketEntry{ts("/run/a.sock", "/c/a.sock", "A_SOCK", true, "s")}
	sockB := []SocketEntry{ts("/run/b.sock", "/c/a.sock", "A_SOCK", true, "s")}
	if sourceFingerprint(mounts, nil, nil) == sourceFingerprint(mounts, sockA, nil) {
		t.Error("adding a socket should change the fingerprint")
	}
	if sourceFingerprint(mounts, sockA, nil) == sourceFingerprint(mounts, sockB, nil) {
		t.Error("changing a socket host path should change the fingerprint")
	}
	sockTwoA := []SocketEntry{
		ts("/run/a.sock", "/c/a.sock", "A", true, "s"),
		ts("/run/b.sock", "/c/b.sock", "B", true, "s"),
	}
	sockTwoB := []SocketEntry{
		ts("/run/b.sock", "/c/b.sock", "B", true, "s"),
		ts("/run/a.sock", "/c/a.sock", "A", true, "s"),
	}
	if sourceFingerprint(mounts, sockTwoA, nil) != sourceFingerprint(mounts, sockTwoB, nil) {
		t.Error("fingerprint should be order-independent across sockets")
	}
}

func TestSourceFingerprint_CoversCredentials(t *testing.T) {
	mounts := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	credA := []CredentialEntry{tc("/home/u/.ollama/id_ed25519", ".ollama/id_ed25519", "0600", true, "s")}
	credB := []CredentialEntry{tc("/home/u/.ollama/id_ed25519", ".ollama/id_ed25519", "0644", true, "s")}
	if sourceFingerprint(mounts, nil, nil) == sourceFingerprint(mounts, nil, credA) {
		t.Error("adding a credential entry should change the fingerprint")
	}
	if sourceFingerprint(mounts, nil, credA) == sourceFingerprint(mounts, nil, credB) {
		t.Error("changing a credential entry's mode should change the fingerprint")
	}
}

func TestFilterTrusted_GatesOnlyEscapingUntrusted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "sub"), "/c-in", false, true, src),
		tm(outside, "/c-out", false, true, src),
		tm(outside, "/c-trusted-scope", false, false, ""),
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
		ts("/run/host.sock", "/c/host.sock", "BROKER", true, src),
		ts("/run/trusted.sock", "/c/t.sock", "T", false, ""),
	}}

	_, _, keptSC, dropped, _, _ := FilterTrusted(nil, sc, nil, ws)
	if len(dropped) != 1 || dropped[0].EnvVar != "BROKER" {
		t.Fatalf("expected the untrusted socket dropped, got %+v", dropped)
	}
	if len(keptSC.Sockets) != 1 || keptSC.Sockets[0].EnvVar != "T" {
		t.Fatalf("expected only the trusted-scope socket kept, got %+v", keptSC.Sockets)
	}
}

func TestFilterTrusted_GatesUntrustedAdHocCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	cc := &CredentialConfig{Entries: []CredentialEntry{
		tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0600", true, src),
		tc("/home/u/.gh/token", "/home/code/.gh/token", "", false, ""),
	}}

	kept, dropped := filterCreds(cc, ws)
	if len(dropped) != 1 || dropped[0].ContainerPath != "/home/code/.aws/credentials" {
		t.Fatalf("expected the untrusted ad-hoc credential dropped, got %+v", dropped)
	}
	if len(kept.Entries) != 1 || kept.Entries[0].ContainerPath != "/home/code/.gh/token" {
		t.Fatalf("expected only the trusted-scope credential kept, got %+v", kept.Entries)
	}
}

func TestFilterTrusted_NeverGatesBundleCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")

	// Untrusted=true but BundleName set: a bundle reference from untrusted
	// project config must never be gated, matching the trust level builtin
	// tool credentials already have.
	cc := &CredentialConfig{Entries: []CredentialEntry{
		{HostPath: "/home/u/.ollama/id_ed25519", ContainerPath: ".ollama/id_ed25519", BundleName: "ollama", Untrusted: true, SourcePath: src},
	}}

	kept, dropped := filterCreds(cc, ws)
	if len(dropped) != 0 {
		t.Fatalf("bundle-sourced credential must never be gated, got dropped=%+v", dropped)
	}
	if len(kept.Entries) != 1 {
		t.Fatalf("expected the bundle credential kept, got %+v", kept.Entries)
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
	cc := &CredentialConfig{Entries: []CredentialEntry{
		tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0600", true, src),
	}}
	_, droppedM, _, droppedS, _, droppedC := FilterTrusted(mc, sc, cc, ws)
	if len(droppedM) != 0 || len(droppedS) != 0 || len(droppedC) != 0 {
		t.Fatalf("COI_TRUST_ALL=1 should bypass gating, got droppedM=%+v droppedS=%+v droppedC=%+v", droppedM, droppedS, droppedC)
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

	sources, err := TrustSources(mc, nil, nil, ws)
	if err != nil || len(sources) != 1 || sources[0] != src {
		t.Fatalf("TrustSources: sources=%v err=%v", sources, err)
	}

	if _, dropped := filterMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("mount should be allowed after trust")
	}

	changed := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, src),
		tm(outside, "/c2", false, true, src),
	}}
	if _, dropped := filterMounts(changed, ws); len(dropped) != 2 {
		t.Fatalf("changed mount set should re-arm gating, got dropped=%d", len(dropped))
	}
}

func TestTrust_CombinedMountSocketAndCredentialSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}
	sc := &SocketConfig{Sockets: []SocketEntry{ts("/run/host.sock", "/c/host.sock", "BROKER", true, src)}}
	cc := &CredentialConfig{Entries: []CredentialEntry{tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0600", true, src)}}

	_, dM, _, dS, _, dC := FilterTrusted(mc, sc, cc, ws)
	if len(dM) != 1 || len(dS) != 1 || len(dC) != 1 {
		t.Fatalf("all three should be gated before trust, dM=%d dS=%d dC=%d", len(dM), len(dS), len(dC))
	}

	sources, err := TrustSources(mc, sc, cc, ws)
	if err != nil || len(sources) != 1 {
		t.Fatalf("TrustSources: sources=%v err=%v", sources, err)
	}
	_, dM, _, dS, _, dC = FilterTrusted(mc, sc, cc, ws)
	if len(dM) != 0 || len(dS) != 0 || len(dC) != 0 {
		t.Fatalf("all three should be trusted after approval, dM=%d dS=%d dC=%d", len(dM), len(dS), len(dC))
	}

	// Changing the credential entry alone re-arms the combined fingerprint.
	ccChanged := &CredentialConfig{Entries: []CredentialEntry{tc("/home/u/.aws/credentials", "/home/code/.aws/credentials", "0644", true, src)}}
	_, dM, _, dS, _, dC = FilterTrusted(mc, sc, ccChanged, ws)
	if len(dM) != 1 || len(dS) != 1 || len(dC) != 1 {
		t.Fatalf("changing the credential entry should re-arm gating for the whole source, dM=%d dS=%d dC=%d", len(dM), len(dS), len(dC))
	}
}

func TestUntrustSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, err := TrustSources(mc, nil, nil, ws); err != nil {
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

	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatal(err)
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "link")) {
		t.Error("in-workspace symlink to an outside dir must be detected as escaping")
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "link", "sub")) {
		t.Error("a path through an in-workspace symlink must be escaping")
	}

	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(ws, "dangling")); err != nil {
		t.Fatal(err)
	}
	if !hostEscapesWorkspace(ws, filepath.Join(ws, "dangling")) {
		t.Error("dangling symlink pointing outside must be escaping")
	}

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
		tm("/y", "/cy", false, true, srcA),
		tm("/w", "/cw", false, false, ""),
	}}
	sc := &SocketConfig{Sockets: []SocketEntry{
		ts("/z", "/cz", "Z", true, srcB),
		ts("/t", "/ct", "T", false, ""),
	}}
	got := UntrustedSourcePaths(mc, sc, nil)
	want := []string{srcA, srcB}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("UntrustedSourcePaths = %v, want sorted distinct %v", got, want)
	}
}

func TestUntrustedSourcePaths_IncludesCredentialsButNotBundles(t *testing.T) {
	ws := t.TempDir()
	srcA := filepath.Join(ws, ".coi", "config.toml")
	cc := &CredentialConfig{Entries: []CredentialEntry{
		tc("/x", "/cx", "", true, srcA),
		{HostPath: "/y", ContainerPath: "/cy", BundleName: "ollama", Untrusted: true, SourcePath: srcA}, // bundle -> excluded
	}}
	got := UntrustedSourcePaths(nil, nil, cc)
	if len(got) != 1 || got[0] != srcA {
		t.Fatalf("UntrustedSourcePaths = %v, want [%s]", got, srcA)
	}
}

func TestFilterTrusted_NoEscapingIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "a"), "/a", false, true, filepath.Join(ws, ".coi", "config.toml")),
		tm("/anywhere", "/b", false, false, ""),
	}}
	kept, dropped := filterMounts(mc, ws)
	if len(dropped) != 0 || len(kept.Mounts) != 2 {
		t.Fatalf("no escaping untrusted mounts should mean no gating; dropped=%d kept=%d",
			len(dropped), len(kept.Mounts))
	}
}
