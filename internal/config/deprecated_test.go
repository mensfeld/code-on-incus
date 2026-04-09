package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTOML(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestCheckDeprecatedConfigFields_DefaultsImage(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[defaults]
image = "old-image"
`)

	err := checkDeprecatedConfigFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"config error",
		"[defaults] image",
		"old-image",
		"[container]",
		`image = "old-image"`,
		migrationDocURL,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q\n--- error ---\n%s", want, msg)
		}
	}
}

func TestCheckDeprecatedConfigFields_DefaultsPersistent(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[defaults]
persistent = true
`)

	err := checkDeprecatedConfigFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"[defaults] persistent",
		"persistent = true",
		"[container]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q\n--- error ---\n%s", want, msg)
		}
	}
}

func TestCheckDeprecatedConfigFields_RootBuild(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[build]
base = "coi"
script = "build.sh"
`)

	err := checkDeprecatedConfigFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"Top-level [build]",
		"[container.build]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q\n--- error ---\n%s", want, msg)
		}
	}
}

func TestCheckDeprecatedConfigFields_AllProblemsReportedAtOnce(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[defaults]
image = "x"
persistent = false

[build]
base = "coi"
`)

	err := checkDeprecatedConfigFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"[defaults] image",
		"[defaults] persistent",
		"Top-level [build]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q (should report all problems at once)\n--- error ---\n%s", want, msg)
		}
	}
}

func TestCheckDeprecatedConfigFields_NewLayoutPasses(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[container]
image = "coi-default"
persistent = true

[container.build]
base = "coi"
script = "build.sh"
`)

	if err := checkDeprecatedConfigFields(path); err != nil {
		t.Errorf("new layout should not trigger migration error, got: %v", err)
	}
}

func TestCheckDeprecatedConfigFields_EmptyFilePasses(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", "")
	if err := checkDeprecatedConfigFields(path); err != nil {
		t.Errorf("empty file should not trigger migration error, got: %v", err)
	}
}

func TestCheckDeprecatedProfileFields_RootImage(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
image = "rust-image"
`)

	err := checkDeprecatedProfileFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"profile error",
		"Root-level image",
		"rust-image",
		"[container]",
		migrationDocURL,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q\n--- error ---\n%s", want, msg)
		}
	}
}

func TestCheckDeprecatedProfileFields_RootPersistent(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
persistent = true
`)

	err := checkDeprecatedProfileFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	if !strings.Contains(err.Error(), "Root-level persistent") {
		t.Errorf("error missing 'Root-level persistent': %v", err)
	}
}

func TestCheckDeprecatedProfileFields_RootBuild(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[build]
base = "coi"
script = "build.sh"
`)

	err := checkDeprecatedProfileFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"Root-level [build]",
		"[container.build]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q\n--- error ---\n%s", want, msg)
		}
	}
}

func TestCheckDeprecatedProfileFields_NewLayoutPasses(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[container]
image = "rust"
persistent = true

[container.build]
base = "coi"
script = "build.sh"
`)

	if err := checkDeprecatedProfileFields(path); err != nil {
		t.Errorf("new profile layout should not trigger migration error, got: %v", err)
	}
}

func TestCheckDeprecatedProfileFields_AllProblemsReportedAtOnce(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
image = "old"
persistent = false

[build]
base = "coi"
`)

	err := checkDeprecatedProfileFields(path)
	if err == nil {
		t.Fatal("expected migration error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"Root-level image",
		"Root-level persistent",
		"Root-level [build]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q (should report all problems at once)\n--- error ---\n%s", want, msg)
		}
	}
}

func TestLoadConfigFile_DeprecatedDefaultsImageFails(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, "config.toml", `
[defaults]
image = "x"
`)

	cfg := GetDefaultConfig()
	err := loadConfigFile(cfg, path)
	if err == nil {
		t.Fatal("expected loadConfigFile to fail on deprecated layout")
	}
	if !strings.Contains(err.Error(), "[defaults] image") {
		t.Errorf("error should mention deprecated field: %v", err)
	}
}

func TestLoadProfileDirectories_DeprecatedRootImageFails(t *testing.T) {
	dir := t.TempDir()
	profilesDir := filepath.Join(dir, "profiles", "rust")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTOML(t, profilesDir, "config.toml", `
image = "rust-image"
`)

	cfg := GetDefaultConfig()
	err := loadProfileDirectories(cfg, dir)
	if err == nil {
		t.Fatal("expected loadProfileDirectories to fail on deprecated profile layout")
	}
	if !strings.Contains(err.Error(), "Root-level image") {
		t.Errorf("error should mention deprecated profile field: %v", err)
	}
}
