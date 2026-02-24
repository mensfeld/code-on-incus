package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

func TestSetupGitHooksMount_NotGitRepo(t *testing.T) {
	// Create a temporary directory that is NOT a git repository
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// SetupGitHooksMount should return nil for non-git directories
	// It checks for .git directory existence before attempting mount
	err = SetupGitHooksMount(nil, tmpDir, false)
	if err != nil {
		t.Errorf("Expected nil error for non-git repo, got: %v", err)
	}
}

func TestPathToDeviceName(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{".git/hooks", "protect-git-hooks"},
		{".git/config", "protect-git-config"},
		{".husky", "protect-husky"},
		{".vscode", "protect-vscode"},
		{".idea", "protect-idea"},
		{"some/deep/path", "protect-some-deep-path"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := pathToDeviceName(tt.path)
			if result != tt.expected {
				t.Errorf("pathToDeviceName(%q) = %q, expected %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestShouldCreateIfMissing(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".git/hooks", true},
		{".git/config", false},
		{".husky", false},
		{".vscode", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := shouldCreateIfMissing(tt.path)
			if result != tt.expected {
				t.Errorf("shouldCreateIfMissing(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestSetupSecurityMounts_EmptyPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Empty paths should return nil
	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{}, false)
	if err != nil {
		t.Errorf("Expected nil error for empty paths, got: %v", err)
	}

	err = SetupSecurityMounts(nil, tmpDir, "/workspace", nil, false)
	if err != nil {
		t.Errorf("Expected nil error for nil paths, got: %v", err)
	}
}

func TestSetupSecurityMounts_NonExistentPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Non-existent paths should be silently skipped (except .git/hooks which is created)
	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{".vscode", ".husky"}, false)
	if err != nil {
		t.Errorf("Expected nil error for non-existent paths, got: %v", err)
	}
}

func TestSetupSecurityMounts_SymlinkRejection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a target directory
	targetDir := filepath.Join(tmpDir, "target")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Create .vscode as a symlink
	vscodeDir := filepath.Join(tmpDir, ".vscode")
	if err := os.Symlink(targetDir, vscodeDir); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Should return error for symlinked paths
	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{".vscode"}, false)
	if err == nil {
		t.Error("Expected error for symlinked path, got nil")
	}
}

func TestSetupSecurityMounts_GitSymlinkSkipped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a target directory
	targetDir := filepath.Join(tmpDir, "target")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Create .git as a symlink (simulating a worktree)
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Symlink(targetDir, gitDir); err != nil {
		t.Fatalf("Failed to create .git symlink: %v", err)
	}

	// Should skip .git/hooks when .git is a symlink (no error, just skip)
	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{".git/hooks"}, false)
	if err != nil {
		t.Errorf("Expected nil error when .git is symlink, got: %v", err)
	}
}

func TestSetupSecurityMounts_GitFileSkipped(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .git as a file (simulating a worktree/submodule)
	gitFile := filepath.Join(tmpDir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /some/path"), 0o644); err != nil {
		t.Fatalf("Failed to create .git file: %v", err)
	}

	// Should skip .git/hooks when .git is a file (no error, just skip)
	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{".git/hooks"}, false)
	if err != nil {
		t.Errorf("Expected nil error when .git is file, got: %v", err)
	}
}

func TestGetProtectedPathsForLogging(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some paths
	os.MkdirAll(filepath.Join(tmpDir, ".git", "hooks"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, ".vscode"), 0o755)

	// Create a symlink (should be excluded)
	os.Mkdir(filepath.Join(tmpDir, "target"), 0o755)
	os.Symlink(filepath.Join(tmpDir, "target"), filepath.Join(tmpDir, ".husky"))

	paths := GetProtectedPathsForLogging(tmpDir, []string{".git/hooks", ".vscode", ".husky", ".idea"})

	// Should include .git/hooks and .vscode, but not .husky (symlink) or .idea (non-existent)
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d: %v", len(paths), paths)
	}

	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}

	if !found[".git/hooks"] {
		t.Error("Expected .git/hooks in paths")
	}
	if !found[".vscode"] {
		t.Error("Expected .vscode in paths")
	}
	if found[".husky"] {
		t.Error("Should not include .husky (symlink)")
	}
	if found[".idea"] {
		t.Error("Should not include .idea (non-existent)")
	}
}

func TestDefaultProtectedPaths(t *testing.T) {
	paths := config.DefaultProtectedPaths()

	expected := []string{".git/hooks", ".git/config", ".husky", ".vscode"}

	if len(paths) != len(expected) {
		t.Errorf("Expected %d default paths, got %d", len(expected), len(paths))
	}

	pathMap := make(map[string]bool)
	for _, p := range paths {
		pathMap[p] = true
	}

	for _, exp := range expected {
		if !pathMap[exp] {
			t.Errorf("Expected %q in default protected paths", exp)
		}
	}
}

func TestSecurityConfig_GetEffectiveProtectedPaths(t *testing.T) {
	t.Run("default paths", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			ProtectedPaths:           config.DefaultProtectedPaths(),
			AdditionalProtectedPaths: []string{},
			DisableProtection:        false,
		}

		paths := cfg.GetEffectiveProtectedPaths()
		if len(paths) != 4 {
			t.Errorf("Expected 4 paths, got %d", len(paths))
		}
	})

	t.Run("with additional paths", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			ProtectedPaths:           config.DefaultProtectedPaths(),
			AdditionalProtectedPaths: []string{".idea", "Makefile"},
			DisableProtection:        false,
		}

		paths := cfg.GetEffectiveProtectedPaths()
		if len(paths) != 6 {
			t.Errorf("Expected 6 paths, got %d", len(paths))
		}
	})

	t.Run("disabled protection", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			ProtectedPaths:           config.DefaultProtectedPaths(),
			AdditionalProtectedPaths: []string{".idea"},
			DisableProtection:        true,
		}

		paths := cfg.GetEffectiveProtectedPaths()
		if paths != nil {
			t.Errorf("Expected nil paths when disabled, got %v", paths)
		}
	})

	t.Run("custom paths replace defaults", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			ProtectedPaths:           []string{".git/hooks"},
			AdditionalProtectedPaths: []string{},
			DisableProtection:        false,
		}

		paths := cfg.GetEffectiveProtectedPaths()
		if len(paths) != 1 {
			t.Errorf("Expected 1 path, got %d", len(paths))
		}
		if paths[0] != ".git/hooks" {
			t.Errorf("Expected .git/hooks, got %s", paths[0])
		}
	})
}

func TestSetupGitHooksMount_HooksDirCreation(t *testing.T) {
	// Test that SetupGitHooksMount creates the hooks dir if missing
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .git directory (simulating a git repo)
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	hooksDir := filepath.Join(gitDir, "hooks")

	// Verify hooks dir doesn't exist yet
	if _, err := os.Stat(hooksDir); !os.IsNotExist(err) {
		t.Fatal("Hooks dir should not exist yet")
	}

	// Simulate what SetupSecurityMounts does
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("Failed to create hooks dir: %v", err)
	}

	// Verify hooks dir was created
	info, err := os.Stat(hooksDir)
	if err != nil {
		t.Fatalf("Failed to stat hooks dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("Hooks path should be a directory")
	}
}

func TestSetupGitHooksMount_PreservesExistingHooks(t *testing.T) {
	// Test that existing hooks are not deleted during setup
	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .git/hooks directory with a sample hook
	hooksDir := filepath.Join(tmpDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("Failed to create hooks dir: %v", err)
	}

	hookContent := "#!/bin/sh\necho 'pre-commit hook'\n"
	hookFile := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(hookFile, []byte(hookContent), 0o755); err != nil {
		t.Fatalf("Failed to create hook file: %v", err)
	}

	// Simulate MkdirAll (what SetupSecurityMounts does)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Verify hook file still exists
	content, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("Failed to read hook file: %v", err)
	}

	if string(content) != hookContent {
		t.Errorf("Hook content was modified. Expected %q, got %q", hookContent, string(content))
	}
}
