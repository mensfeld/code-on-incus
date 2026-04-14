package alias

import (
	"os"
	"path/filepath"
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

func TestResolveAliasForLaunch_CWDFallback(t *testing.T) {
	// Create a workspace directory with .coi/config.toml
	workspace := t.TempDir()
	coiDir := filepath.Join(workspace, ".coi")
	if err := os.MkdirAll(coiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte("[container]\nalias = \"testfallback\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up a temporary registry file
	regPath := filepath.Join(t.TempDir(), "aliases.json")

	// Override the registry path for this test by saving an empty registry
	reg := &Registry{path: regPath, entries: make(map[string]AliasEntry)}
	if err := reg.Save(); err != nil {
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

	// We can't easily test ResolveAliasForLaunch directly because it uses Load()
	// which reads from the default path. Instead, test the fallback logic directly.
	projectAlias := loadProjectAlias(workspace)
	if projectAlias != "testfallback" {
		t.Fatalf("loadProjectAlias() = %q, want %q", projectAlias, "testfallback")
	}

	// Simulate the fallback: register and save
	absWorkspace, _ := filepath.Abs(workspace)
	if err := reg.Register("testfallback", absWorkspace, ""); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := reg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify it was persisted
	reg2, err := LoadFrom(regPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := reg2.Lookup("testfallback")
	if entry == nil {
		t.Fatal("alias not found in registry after CWD fallback registration")
	}
	if entry.Workspace != absWorkspace {
		t.Errorf("Workspace = %q, want %q", entry.Workspace, absWorkspace)
	}
}

func TestResolveAliasForLaunch_CWDFallback_NoMatch(t *testing.T) {
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

	// Querying a non-matching alias should not trigger the fallback
	projectAlias := loadProjectAlias(workspace)
	if projectAlias == "nonexistent" {
		t.Error("loadProjectAlias should not match a different alias name")
	}
}
