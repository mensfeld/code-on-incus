package config

import (
	"os"
	"path/filepath"
	"testing"
)

const trustTestMountConfig = `
[[mounts.default]]
host = "/tmp/some-host-dir"
container = "/mnt/x"
`

func writeMountConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(trustTestMountConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func findMount(cfg *Config, container string) (MountEntry, bool) {
	for _, m := range cfg.Mounts.Default {
		if m.Container == container {
			return m, true
		}
	}
	return MountEntry{}, false
}

func TestLoadConfigFileScoped_UntrustedMarksMounts(t *testing.T) {
	path := writeMountConfig(t)
	cfg := GetDefaultConfig()
	if err := loadConfigFileScoped(cfg, path, false); err != nil {
		t.Fatalf("loadConfigFileScoped: %v", err)
	}
	m, ok := findMount(cfg, "/mnt/x")
	if !ok {
		t.Fatal("mount /mnt/x was not loaded")
	}
	if !m.Untrusted {
		t.Error("mount from untrusted config should have Untrusted=true")
	}
	abs, _ := filepath.Abs(path)
	if m.SourcePath != abs {
		t.Errorf("SourcePath = %q, want %q", m.SourcePath, abs)
	}
}

const untrustedProfileConfig = `
[network]
mode = "open"
allow_local_network_access = true

[[mounts]]
host = "/tmp/evil-host-dir"
container = "/mnt/h"
`

func writeProfile(t *testing.T, body string) (root, cfgPath string) {
	t.Helper()
	root = t.TempDir()
	profDir := filepath.Join(root, "profiles", "dev")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(profDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, cfgPath
}

// A project-scoped (untrusted) profile must have its network downgrades refused
// and its mounts tagged untrusted — closing the profile bypass of the H4/M5 gate.
func TestLoadProfileDirectories_UntrustedTagsMountsAndSanitizesNetwork(t *testing.T) {
	root, cfgPath := writeProfile(t, untrustedProfileConfig)
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, root, false); err != nil {
		t.Fatalf("loadProfileDirectories: %v", err)
	}
	p, ok := cfg.Profiles["dev"]
	if !ok {
		t.Fatal("profile dev not loaded")
	}
	if p.Network == nil {
		t.Fatal("profile network is nil")
	}
	if p.Network.Mode == NetworkModeOpen {
		t.Error("untrusted profile network.mode=open should have been sanitized")
	}
	if p.Network.AllowLocalNetworkAccess != nil && *p.Network.AllowLocalNetworkAccess {
		t.Error("untrusted profile allow_local_network_access=true should have been sanitized")
	}
	if len(p.Mounts) != 1 {
		t.Fatalf("expected 1 profile mount, got %d", len(p.Mounts))
	}
	if !p.Mounts[0].Untrusted {
		t.Error("project-scoped profile mount must be marked Untrusted")
	}
	abs, _ := filepath.Abs(cfgPath)
	if p.Mounts[0].SourcePath != abs {
		t.Errorf("SourcePath = %q, want %q", p.Mounts[0].SourcePath, abs)
	}
}

func TestLoadProfileDirectories_TrustedDoesNotTag(t *testing.T) {
	root, _ := writeProfile(t, "[[mounts]]\nhost = \"/tmp/x\"\ncontainer = \"/mnt/x\"\n")
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, root, true); err != nil {
		t.Fatalf("loadProfileDirectories: %v", err)
	}
	p := cfg.Profiles["dev"]
	if len(p.Mounts) != 1 {
		t.Fatalf("expected 1 profile mount, got %d", len(p.Mounts))
	}
	if p.Mounts[0].Untrusted {
		t.Error("trusted profile mount must not be marked Untrusted")
	}
}

const trustTestSocketConfig = `
[[sockets]]
host = "/run/host.sock"
container = "/run/c.sock"
env = "BROKER"
`

func writeSocketConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(trustTestSocketConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigFileScoped_UntrustedMarksSockets(t *testing.T) {
	path := writeSocketConfig(t)
	cfg := GetDefaultConfig()
	if err := loadConfigFileScoped(cfg, path, false); err != nil {
		t.Fatalf("loadConfigFileScoped: %v", err)
	}
	if len(cfg.Sockets) != 1 {
		t.Fatalf("expected 1 socket, got %d", len(cfg.Sockets))
	}
	s := cfg.Sockets[0]
	if !s.Untrusted {
		t.Error("socket from untrusted config should have Untrusted=true")
	}
	abs, _ := filepath.Abs(path)
	if s.SourcePath != abs {
		t.Errorf("SourcePath = %q, want %q", s.SourcePath, abs)
	}
}

func TestLoadConfigFileScoped_TrustedDoesNotMarkSockets(t *testing.T) {
	path := writeSocketConfig(t)
	cfg := GetDefaultConfig()
	if err := loadConfigFileScoped(cfg, path, true); err != nil {
		t.Fatalf("loadConfigFileScoped: %v", err)
	}
	if len(cfg.Sockets) != 1 {
		t.Fatalf("expected 1 socket, got %d", len(cfg.Sockets))
	}
	if cfg.Sockets[0].Untrusted || cfg.Sockets[0].SourcePath != "" {
		t.Errorf("socket from trusted config must not be marked untrusted: %+v", cfg.Sockets[0])
	}
}

func TestLoadProfileDirectories_UntrustedMarksSockets(t *testing.T) {
	body := "[[sockets]]\nhost = \"/run/host.sock\"\ncontainer = \"/run/c.sock\"\nenv = \"BROKER\"\n"
	root, cfgPath := writeProfile(t, body)
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, root, false); err != nil {
		t.Fatalf("loadProfileDirectories: %v", err)
	}
	p := cfg.Profiles["dev"]
	if len(p.Sockets) != 1 {
		t.Fatalf("expected 1 profile socket, got %d", len(p.Sockets))
	}
	if !p.Sockets[0].Untrusted {
		t.Error("project-scoped profile socket must be marked Untrusted")
	}
	abs, _ := filepath.Abs(cfgPath)
	if p.Sockets[0].SourcePath != abs {
		t.Errorf("SourcePath = %q, want %q", p.Sockets[0].SourcePath, abs)
	}
}

func TestLoadConfigFileScoped_TrustedDoesNotMarkMounts(t *testing.T) {
	path := writeMountConfig(t)
	cfg := GetDefaultConfig()
	if err := loadConfigFileScoped(cfg, path, true); err != nil {
		t.Fatalf("loadConfigFileScoped: %v", err)
	}
	m, ok := findMount(cfg, "/mnt/x")
	if !ok {
		t.Fatal("mount /mnt/x was not loaded")
	}
	if m.Untrusted {
		t.Error("mount from trusted config must not be marked Untrusted")
	}
	if m.SourcePath != "" {
		t.Errorf("trusted mount SourcePath should be empty, got %q", m.SourcePath)
	}
}
