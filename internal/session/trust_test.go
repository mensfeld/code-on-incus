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

func TestMountFingerprint_OrderIndependentAndSensitive(t *testing.T) {
	a := []MountEntry{tm("/h1", "/c1", false, true, "s"), tm("/h2", "/c2", true, true, "s")}
	b := []MountEntry{tm("/h2", "/c2", true, true, "s"), tm("/h1", "/c1", false, true, "s")}
	if MountFingerprint(a) != MountFingerprint(b) {
		t.Error("fingerprint should be order-independent")
	}
	roChanged := []MountEntry{tm("/h1", "/c1", true, true, "s"), tm("/h2", "/c2", true, true, "s")}
	if MountFingerprint(a) == MountFingerprint(roChanged) {
		t.Error("fingerprint should change when a readonly flag changes")
	}
	removed := []MountEntry{tm("/h1", "/c1", false, true, "s")}
	if MountFingerprint(a) == MountFingerprint(removed) {
		t.Error("fingerprint should change when a mount is removed")
	}
}

func TestFilterTrustedMounts_GatesOnlyEscapingUntrusted(t *testing.T) {
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

	kept, dropped := FilterTrustedMounts(mc, ws)
	if len(dropped) != 1 || dropped[0].ContainerPath != "/c-out" {
		t.Fatalf("expected only the escaping untrusted mount dropped, got %+v", dropped)
	}
	if len(kept.Mounts) != 2 {
		t.Fatalf("expected 2 kept mounts, got %d", len(kept.Mounts))
	}
}

func TestFilterTrustedMounts_TrustAllEnvBypasses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "1")
	ws := t.TempDir()
	outside := t.TempDir()
	mc := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, filepath.Join(ws, ".coi", "config.toml")),
	}}
	if _, dropped := FilterTrustedMounts(mc, ws); len(dropped) != 0 {
		t.Fatalf("COI_TRUST_ALL=1 should bypass gating, got dropped=%+v", dropped)
	}
}

func TestTrustThenRevokeOnMountChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, dropped := FilterTrustedMounts(mc, ws); len(dropped) != 1 {
		t.Fatal("escaping untrusted mount should be gated before trust")
	}

	sources, err := TrustEscapingMounts(mc, ws)
	if err != nil || len(sources) != 1 || sources[0] != src {
		t.Fatalf("TrustEscapingMounts: sources=%v err=%v", sources, err)
	}

	if _, dropped := FilterTrustedMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("mount should be allowed after trust")
	}

	// Adding another escaping mount changes the fingerprint -> trust no longer
	// matches -> the source is gated again (all its escaping mounts dropped).
	changed := &MountConfig{Mounts: []MountEntry{
		tm(outside, "/c", false, true, src),
		tm(outside, "/c2", false, true, src),
	}}
	if _, dropped := FilterTrustedMounts(changed, ws); len(dropped) != 2 {
		t.Fatalf("changed mount set should re-arm gating, got dropped=%d", len(dropped))
	}
}

func TestUntrustSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(ws, ".coi", "config.toml")
	mc := &MountConfig{Mounts: []MountEntry{tm(outside, "/c", false, true, src)}}

	if _, err := TrustEscapingMounts(mc, ws); err != nil {
		t.Fatal(err)
	}
	if _, dropped := FilterTrustedMounts(mc, ws); len(dropped) != 0 {
		t.Fatal("should be trusted")
	}

	n, err := UntrustSources([]string{src})
	if err != nil || n != 1 {
		t.Fatalf("UntrustSources n=%d err=%v", n, err)
	}
	if _, dropped := FilterTrustedMounts(mc, ws); len(dropped) != 1 {
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

func TestFilterTrustedMounts_SymlinkEscapeGated(t *testing.T) {
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
	_, dropped := FilterTrustedMounts(mc, ws)
	if len(dropped) != 1 {
		t.Fatalf("symlink-escaping untrusted mount must be gated, dropped=%d", len(dropped))
	}
}

func TestFilterTrustedMounts_NoEscapingIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TrustEnvVar, "")
	ws := t.TempDir()
	mc := &MountConfig{Mounts: []MountEntry{
		tm(filepath.Join(ws, "a"), "/a", false, true, filepath.Join(ws, ".coi", "config.toml")),
		tm("/anywhere", "/b", false, false, ""), // trusted scope
	}}
	kept, dropped := FilterTrustedMounts(mc, ws)
	if len(dropped) != 0 || len(kept.Mounts) != 2 {
		t.Fatalf("no escaping untrusted mounts should mean no gating; dropped=%d kept=%d",
			len(dropped), len(kept.Mounts))
	}
}
