package alias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectAlias(t *testing.T) {
	t.Run("valid config with alias", func(t *testing.T) {
		dir := t.TempDir()
		coiDir := filepath.Join(dir, ".coi")
		if err := os.MkdirAll(coiDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nalias = \"myproject\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := loadProjectAlias(dir)
		if got != "myproject" {
			t.Errorf("loadProjectAlias() = %q, want %q", got, "myproject")
		}
	})

	t.Run("no config file", func(t *testing.T) {
		dir := t.TempDir()
		got := loadProjectAlias(dir)
		if got != "" {
			t.Errorf("loadProjectAlias() = %q, want empty", got)
		}
	})

	t.Run("config without alias", func(t *testing.T) {
		dir := t.TempDir()
		coiDir := filepath.Join(dir, ".coi")
		if err := os.MkdirAll(coiDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nimage = \"ubuntu\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := loadProjectAlias(dir)
		if got != "" {
			t.Errorf("loadProjectAlias() = %q, want empty", got)
		}
	})

	t.Run("empty alias", func(t *testing.T) {
		dir := t.TempDir()
		coiDir := filepath.Join(dir, ".coi")
		if err := os.MkdirAll(coiDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nalias = \"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := loadProjectAlias(dir)
		if got != "" {
			t.Errorf("loadProjectAlias() = %q, want empty", got)
		}
	})

	t.Run("malformed toml", func(t *testing.T) {
		dir := t.TempDir()
		coiDir := filepath.Join(dir, ".coi")
		if err := os.MkdirAll(coiDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("not valid toml [[["), 0o644); err != nil {
			t.Fatal(err)
		}

		got := loadProjectAlias(dir)
		if got != "" {
			t.Errorf("loadProjectAlias() = %q, want empty", got)
		}
	})
}

// withTempHome sets HOME to a temp directory so Load() uses an isolated
// aliases.json. Returns a cleanup function to restore the original HOME.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	return tmpHome
}

func TestResolveAliasForLaunch_CWDFallback(t *testing.T) {
	tmpHome := withTempHome(t)

	// Create a workspace directory with .coi/config.toml
	workspace := t.TempDir()
	coiDir := filepath.Join(workspace, ".coi")
	if err := os.MkdirAll(coiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nalias = \"testfallback\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change CWD to the workspace to trigger the fallback
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Call the production function — alias is not in registry, so
	// CWD fallback should kick in and auto-register it.
	resolved, err := ResolveAliasForLaunch("testfallback")
	if err != nil {
		t.Fatalf("ResolveAliasForLaunch() returned error: %v", err)
	}

	absWorkspace, _ := filepath.Abs(workspace)
	if resolved.Workspace != absWorkspace {
		t.Errorf("Workspace = %q, want %q", resolved.Workspace, absWorkspace)
	}

	// Verify the alias was persisted to the registry
	regPath := filepath.Join(tmpHome, ".coi", "aliases.json")
	reg, err := LoadFrom(regPath)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}
	entry := reg.Lookup("testfallback")
	if entry == nil {
		t.Fatal("alias not found in registry after CWD fallback auto-registration")
	}
	if entry.Workspace != absWorkspace {
		t.Errorf("persisted Workspace = %q, want %q", entry.Workspace, absWorkspace)
	}
}

func TestResolveAliasForLaunch_CWDFallback_NoMatch(t *testing.T) {
	withTempHome(t)

	// Create a workspace directory with a different alias
	workspace := t.TempDir()
	coiDir := filepath.Join(workspace, ".coi")
	if err := os.MkdirAll(coiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nalias = \"differentalias\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change CWD to the workspace
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Querying a non-matching alias should fail — CWD has "differentalias"
	// but we ask for "nonexistent".
	_, err = ResolveAliasForLaunch("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-matching alias, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestResolveAliasForLaunch_CWDFallback_SlotSuffix(t *testing.T) {
	withTempHome(t)

	workspace := t.TempDir()
	coiDir := filepath.Join(workspace, ".coi")
	if err := os.MkdirAll(coiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nalias = \"slottest\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(workspace)
	t.Cleanup(func() { os.Chdir(origDir) })

	// Alias with slot suffix should resolve via CWD fallback
	resolved, err := ResolveAliasForLaunch("slottest-3")
	if err != nil {
		t.Fatalf("ResolveAliasForLaunch(\"slottest-3\") returned error: %v", err)
	}
	if resolved.Slot != 3 {
		t.Errorf("Slot = %d, want 3", resolved.Slot)
	}
}
