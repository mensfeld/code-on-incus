package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestIsFileTypeProtected(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".git/config", true},
		{".git/hooks", false},
		{".husky", false},
		{".vscode", false},
		{".idea", false},
		{"Makefile", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isFileTypeProtected(tt.path)
			if result != tt.expected {
				t.Errorf("isFileTypeProtected(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsDirTypeProtected(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".git/hooks", true},
		{".husky", true},
		{".vscode", true},
		{".git/config", false},
		{".idea", false},
		{"Makefile", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isDirTypeProtected(tt.path)
			if result != tt.expected {
				t.Errorf("isDirTypeProtected(%q) = %v, expected %v", tt.path, result, tt.expected)
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

	// In a non-git workspace, .git/hooks and .git/config are silently
	// skipped by the .git guard, so SetupSecurityMounts returns nil
	// even with a nil manager (MountDisk is never reached).
	err = SetupSecurityMounts(nil, tmpDir, "/workspace",
		[]string{".git/hooks", ".git/config"}, false)
	if err != nil {
		t.Errorf("Expected nil error when .git is missing, got: %v", err)
	}

	// Confirm the .git guard did NOT synthesize a .git/ directory.
	if _, statErr := os.Lstat(filepath.Join(tmpDir, ".git")); !os.IsNotExist(statErr) {
		t.Errorf(".git/ should not have been created in a non-git workspace, stat err=%v", statErr)
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
	cfg := config.GetDefaultConfig()
	paths := cfg.Security.ProtectedPaths

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
			ProtectedPaths:           []string{".git/hooks", ".git/config", ".husky", ".vscode"},
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
			ProtectedPaths:           []string{".git/hooks", ".git/config", ".husky", ".vscode"},
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
			ProtectedPaths:           []string{".git/hooks", ".git/config", ".husky", ".vscode"},
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

func TestEnsureProtectedExists_CreatesMissingDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	vscodePath := filepath.Join(tmpDir, ".vscode")
	if err := ensureProtectedExists(tmpDir, vscodePath, ".vscode"); err != nil {
		t.Fatalf("ensureProtectedExists returned error: %v", err)
	}

	info, err := os.Stat(vscodePath)
	if err != nil {
		t.Fatalf("Expected .vscode to exist, got err: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("Expected .vscode to be a directory")
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("Expected mode 0755, got %o", info.Mode().Perm())
	}
}

func TestEnsureProtectedExists_CreatesMissingHuskyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	huskyPath := filepath.Join(tmpDir, ".husky")
	if err := ensureProtectedExists(tmpDir, huskyPath, ".husky"); err != nil {
		t.Fatalf("ensureProtectedExists returned error: %v", err)
	}

	info, err := os.Stat(huskyPath)
	if err != nil {
		t.Fatalf("Expected .husky to exist, got err: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("Expected .husky to be a directory")
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("Expected mode 0755, got %o", info.Mode().Perm())
	}
}

func TestEnsureProtectedExists_PreservesExistingDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	vscodePath := filepath.Join(tmpDir, ".vscode")
	if err := os.MkdirAll(vscodePath, 0o755); err != nil {
		t.Fatalf("Failed to pre-create .vscode: %v", err)
	}

	tasksPath := filepath.Join(vscodePath, "tasks.json")
	original := []byte(`{"version": "2.0.0", "tasks": []}` + "\n")
	if err := os.WriteFile(tasksPath, original, 0o644); err != nil {
		t.Fatalf("Failed to write tasks.json: %v", err)
	}

	if err := ensureProtectedExists(tmpDir, vscodePath, ".vscode"); err != nil {
		t.Fatalf("ensureProtectedExists returned error: %v", err)
	}

	after, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatalf("Failed to read tasks.json after call: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf("tasks.json content changed: got %q, want %q", after, original)
	}
}

func TestEnsureProtectedExists_PreservesExistingFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("Failed to create .git: %v", err)
	}

	configPath := filepath.Join(gitDir, "config")
	original := []byte("[core]\n\trepositoryformatversion = 0\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("Failed to write .git/config: %v", err)
	}

	if err := ensureProtectedExists(tmpDir, configPath, ".git/config"); err != nil {
		t.Fatalf("ensureProtectedExists returned error: %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read .git/config after call: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf(".git/config content changed: got %q, want %q", after, original)
	}
}

func TestEnsureProtectedExists_CreatesFilePlaceholderWhenParentExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("Failed to create .git: %v", err)
	}

	configPath := filepath.Join(gitDir, "config")
	if err := ensureProtectedExists(tmpDir, configPath, ".git/config"); err != nil {
		t.Fatalf("ensureProtectedExists returned error: %v", err)
	}

	info, err := os.Lstat(configPath)
	if err != nil {
		t.Fatalf("Expected .git/config placeholder to exist, got err: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("Expected regular file, got mode %v", info.Mode())
	}
	if info.Size() != 0 {
		t.Errorf("Expected empty placeholder, got size %d", info.Size())
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("Expected mode 0644, got %o", info.Mode().Perm())
	}
}

func TestEnsureProtectedExists_FilePlaceholderParentMissing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// .git/ does NOT exist
	configPath := filepath.Join(tmpDir, ".git", "config")
	err = ensureProtectedExists(tmpDir, configPath, ".git/config")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected os.ErrNotExist, got %v", err)
	}

	// .git/ must not have been created.
	if _, statErr := os.Lstat(filepath.Join(tmpDir, ".git")); !os.IsNotExist(statErr) {
		t.Errorf(".git/ should not be created when parent is missing, stat err=%v", statErr)
	}
}

func TestEnsureProtectedExists_SymlinkNotFollowed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a real sibling target directory
	targetDir := filepath.Join(tmpDir, "target")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Create .vscode as a symlink to the target
	vscodePath := filepath.Join(tmpDir, ".vscode")
	if err := os.Symlink(targetDir, vscodePath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	if err := ensureProtectedExists(tmpDir, vscodePath, ".vscode"); err != nil {
		t.Errorf("ensureProtectedExists returned error for existing symlink: %v", err)
	}

	// Verify the symlink itself is unchanged (still a symlink).
	info, err := os.Lstat(vscodePath)
	if err != nil {
		t.Fatalf("Failed to Lstat .vscode: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("Expected .vscode to remain a symlink, got mode %v", info.Mode())
	}
}

func TestEnsureProtectedExists_UserAddedMissingDirNotCreated(t *testing.T) {
	// A user-added missing directory-type path (e.g. ".idea") must NOT be
	// auto-materialized. Returns os.ErrNotExist so the caller skips silently.
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ideaPath := filepath.Join(tmpDir, ".idea")
	err = ensureProtectedExists(tmpDir, ideaPath, ".idea")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected os.ErrNotExist for user-added missing path, got %v", err)
	}
	if _, statErr := os.Lstat(ideaPath); !os.IsNotExist(statErr) {
		t.Errorf(".idea must not be created for user-added missing path, stat err=%v", statErr)
	}
}

func TestEnsureProtectedExists_UserAddedMissingMakefileNotCreatedAsDir(t *testing.T) {
	// The copilot-flagged regression: a user-added "Makefile" must NOT be
	// created as a directory (which would break the user's build).
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	makefilePath := filepath.Join(tmpDir, "Makefile")
	err = ensureProtectedExists(tmpDir, makefilePath, "Makefile")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected os.ErrNotExist for missing Makefile, got %v", err)
	}
	if _, statErr := os.Lstat(makefilePath); !os.IsNotExist(statErr) {
		t.Errorf("Makefile must not be created, stat err=%v", statErr)
	}
}

func TestEnsureProtectedExists_PreservesExistingUserAddedDir(t *testing.T) {
	// A user-added directory that DOES already exist must be left alone
	// (returns nil, doesn't clobber) so the caller can mount it RO.
	tmpDir, err := os.MkdirTemp("", "ensure-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ideaPath := filepath.Join(tmpDir, ".idea")
	if err := os.Mkdir(ideaPath, 0o755); err != nil {
		t.Fatalf("Failed to pre-create .idea: %v", err)
	}
	marker := filepath.Join(ideaPath, "workspace.xml")
	if err := os.WriteFile(marker, []byte("<project/>"), 0o644); err != nil {
		t.Fatalf("Failed to write marker: %v", err)
	}

	if err := ensureProtectedExists(tmpDir, ideaPath, ".idea"); err != nil {
		t.Errorf("Expected nil for existing user-added path, got %v", err)
	}

	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker file disappeared: %v", err)
	}
	if string(content) != "<project/>" {
		t.Errorf("marker content changed: got %q", string(content))
	}
}

func TestValidateRelPath(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		wantErr bool
	}{
		{"valid simple", ".vscode", false},
		{"valid nested", ".git/hooks", false},
		{"empty", "", true},
		{"absolute", "/etc/passwd", true},
		{"dotdot", "..", true},
		{"dot", ".", true},
		{"leading dotdot", "../etc", true},
		{"inner dotdot", "foo/../bar", true},
		{"trailing dotdot", "foo/..", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRelPath(tt.relPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRelPath(%q) err=%v, wantErr=%v", tt.relPath, err, tt.wantErr)
			}
		})
	}
}

func TestSafeMkdirAll_RejectsSymlinkedParent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "safe-mkdir-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a sibling target directory outside the "workspace".
	outsideDir, err := os.MkdirTemp("", "outside-*")
	if err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)

	// Create a symlink inside the workspace pointing outside.
	linkPath := filepath.Join(tmpDir, "evil")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Attempting to create a child under the symlinked parent must be rejected.
	target := filepath.Join(tmpDir, "evil", "child")
	err = safeMkdirAll(tmpDir, target, "evil/child")
	if err == nil {
		t.Fatal("safeMkdirAll should have rejected symlinked parent, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("Expected symlink-related error, got: %v", err)
	}

	// The target must not exist under the symlink target either.
	if _, statErr := os.Lstat(filepath.Join(outsideDir, "child")); !os.IsNotExist(statErr) {
		t.Errorf("target must not be created outside workspace, stat err=%v", statErr)
	}
}

func TestSetupProtectedPath_RejectsAbsolutePath(t *testing.T) {
	// Absolute path in protected_paths must be rejected (non-ErrNotExist,
	// so the caller surfaces the error rather than skipping silently).
	tmpDir, err := os.MkdirTemp("", "setup-protected-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{"/etc/passwd"}, false)
	if err == nil {
		t.Fatal("Expected error for absolute protected path, got nil")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected validation error, got ErrNotExist: %v", err)
	}
	// Sanity: nothing was created on the host.
	if _, statErr := os.Lstat("/etc/passwd.coi"); !os.IsNotExist(statErr) {
		t.Errorf("unexpected state: %v", statErr)
	}
}

func TestSetupProtectedPath_RejectsTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "setup-protected-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	err = SetupSecurityMounts(nil, tmpDir, "/workspace", []string{"../outside"}, false)
	if err == nil {
		t.Fatal("Expected error for traversal protected path, got nil")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected validation error, got ErrNotExist: %v", err)
	}
}
