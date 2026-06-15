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
