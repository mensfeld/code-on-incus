package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// SetupSecurityMounts mounts protected paths as read-only for security.
// This prevents containers from modifying files that could execute automatically
// on the host (git hooks, IDE configs, etc.).
// containerWorkspacePath is the path where the workspace is mounted inside the container
// (either /workspace or the preserved host path).
// Returns nil if no paths need protection.
func SetupSecurityMounts(mgr *container.Manager, workspacePath, containerWorkspacePath string, protectedPaths []string, useShift bool) error {
	if len(protectedPaths) == 0 {
		return nil
	}

	for _, relPath := range protectedPaths {
		if err := setupProtectedPath(mgr, workspacePath, containerWorkspacePath, relPath, useShift); err != nil {
			// Paths that legitimately cannot be protected (non-git workspace,
			// a file-type default whose parent directory is missing, or a
			// user-added path that does not exist in the workspace) surface
			// as os.ErrNotExist and are skipped silently. Any other failure
			// (validation / stat / mount errors) is propagated and surfaces
			// as a warning at the caller in setup.go.
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to protect %s: %w", relPath, err)
			}
		}
	}

	return nil
}

// setupProtectedPath mounts a single path as read-only
func setupProtectedPath(mgr *container.Manager, workspacePath, containerWorkspacePath, relPath string, useShift bool) error {
	if err := validateRelPath(relPath); err != nil {
		return err
	}
	cleaned := filepath.Clean(relPath)

	hostPath := filepath.Join(workspacePath, cleaned)
	containerPath := filepath.Join(containerWorkspacePath, cleaned)

	// For .git paths, check if .git itself is valid FIRST (not a symlink or file)
	// This must happen before we try to create .git/hooks
	if strings.HasPrefix(cleaned, ".git"+string(filepath.Separator)) || cleaned == ".git" {
		gitDir := filepath.Join(workspacePath, ".git")
		gitInfo, err := os.Lstat(gitDir)
		if err != nil {
			if os.IsNotExist(err) {
				return os.ErrNotExist // Not a git repo
			}
			return fmt.Errorf("failed to stat .git: %w", err)
		}
		// Skip if .git is a symlink (worktree pointing elsewhere)
		if gitInfo.Mode()&os.ModeSymlink != 0 {
			return os.ErrNotExist // Treat as non-existent for safety
		}
		// Skip if .git is a file (worktree/submodule gitdir file)
		if !gitInfo.IsDir() {
			return os.ErrNotExist // Treat as non-existent for safety
		}
	}

	if err := ensureProtectedExists(workspacePath, hostPath, cleaned); err != nil {
		return err // os.ErrNotExist surfaces for unknown / user-added missing paths
	}

	// Use Lstat to avoid following symlinks (security measure)
	info, err := os.Lstat(hostPath)
	if err != nil {
		return fmt.Errorf("failed to stat %s after materialization: %w", cleaned, err)
	}

	// Security check: reject symlinks to prevent mounting arbitrary host paths
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to mount for security reasons", cleaned)
	}

	// Generate unique device name from path
	deviceName := pathToDeviceName(cleaned)

	// Mount as read-only
	return mgr.MountDisk(deviceName, hostPath, containerPath, useShift, true)
}

// validateRelPath rejects protected_paths entries that are empty,
// absolute, or attempt to traverse outside the workspace. Returning
// a non-os.ErrNotExist error causes the caller to surface a warning
// rather than silently skipping the entry.
func validateRelPath(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("protected path is empty")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("protected path %q must be relative to workspace", relPath)
	}
	cleaned := filepath.Clean(relPath)
	if cleaned == "." || cleaned == ".." {
		return fmt.Errorf("protected path %q resolves outside workspace", relPath)
	}
	// Reject any ".." segment anywhere in either the original or cleaned
	// form. filepath.Clean collapses inner traversals (e.g. "foo/../bar"
	// becomes "bar"), hiding the attacker's intent — check both.
	for _, seg := range strings.Split(relPath, "/") {
		if seg == ".." {
			return fmt.Errorf("protected path %q must not contain .. segments", relPath)
		}
	}
	for _, seg := range strings.Split(cleaned, string(filepath.Separator)) {
		if seg == ".." {
			return fmt.Errorf("protected path %q must not contain .. segments", relPath)
		}
	}
	return nil
}

// fileTypeProtectedPaths lists entries that are materialized as empty
// placeholder files (rather than directories) when missing. The parent
// directory must already exist — we never synthesize a parent tree.
// Extend this set in lockstep with internal/config/embedded/default_config.toml
// when adding new file-type defaults.
var fileTypeProtectedPaths = map[string]bool{
	".git/config": true,
}

// directoryTypeProtectedPaths lists entries that are materialized as
// empty directories when missing. Only the built-in default paths that
// closed FLAWS Finding 2 are auto-created; user-added entries in
// additional_protected_paths must already exist on disk and are
// silently skipped otherwise (see ensureProtectedExists). This avoids
// guessing whether a user-added path like "Makefile" was intended as a
// file or a directory.
var directoryTypeProtectedPaths = map[string]bool{
	".git/hooks": true,
	".husky":     true,
	".vscode":    true,
}

func isFileTypeProtected(relPath string) bool {
	return fileTypeProtectedPaths[relPath]
}

func isDirTypeProtected(relPath string) bool {
	return directoryTypeProtectedPaths[relPath]
}

// ensureProtectedExists materializes hostPath so it has a real inode
// to read-only-mount over. Known default directory-type entries are
// created via a symlink-safe walk. The known file-type entry
// (.git/config) gets an empty placeholder, but only when its parent
// directory already exists. User-added paths that do not exist on
// disk return os.ErrNotExist and are silently skipped by the caller
// — the user is responsible for creating them beforehand.
func ensureProtectedExists(workspacePath, hostPath, relPath string) error {
	if _, err := os.Lstat(hostPath); err == nil {
		return nil // already exists, don't clobber
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat %s: %w", relPath, err)
	}

	switch {
	case isFileTypeProtected(relPath):
		return createProtectedFilePlaceholder(hostPath, relPath)
	case isDirTypeProtected(relPath):
		return safeMkdirAll(workspacePath, hostPath, relPath)
	default:
		// User-added path that does not exist. Do not guess whether
		// the user wanted a directory or a file — skip silently so
		// the caller's os.ErrNotExist filter swallows it.
		return os.ErrNotExist
	}
}

// createProtectedFilePlaceholder creates an empty file at hostPath,
// but only if its parent directory already exists and is not a
// symlink. It never synthesizes a parent tree.
func createProtectedFilePlaceholder(hostPath, relPath string) error {
	parent := filepath.Dir(hostPath)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("failed to stat parent of %s: %w", relPath, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("parent of %s is a symlink; refusing to create placeholder", relPath)
	}
	if !parentInfo.IsDir() {
		return os.ErrNotExist
	}

	f, err := os.OpenFile(hostPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil // lost a harmless race
		}
		return fmt.Errorf("failed to create protected file placeholder %s: %w", relPath, err)
	}
	return f.Close()
}

// safeMkdirAll walks the components of relPath under workspacePath,
// creating each missing component with a plain os.Mkdir. Any existing
// parent component that is a symlink (or not a directory) is refused
// — this prevents a repo-controlled protected_paths entry from causing
// host directory creation outside the workspace via symlinked parents.
func safeMkdirAll(workspacePath, hostPath, relPath string) error {
	rel, err := filepath.Rel(workspacePath, hostPath)
	if err != nil {
		return fmt.Errorf("failed to compute relative path for %s: %w", relPath, err)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := workspacePath
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to create %s: parent component %s is a symlink", relPath, current)
			}
			if !info.IsDir() {
				return fmt.Errorf("refusing to create %s: parent component %s is not a directory", relPath, current)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat %s: %w", current, err)
		}
		if err := os.Mkdir(current, 0o755); err != nil {
			return fmt.Errorf("failed to create protected directory %s: %w", relPath, err)
		}
	}
	return nil
}

// pathToDeviceName converts a path to a valid Incus device name
func pathToDeviceName(path string) string {
	// Replace path separators and dots with dashes
	name := strings.ReplaceAll(path, "/", "-")
	name = strings.ReplaceAll(name, ".", "")
	// Remove leading dash if present
	name = strings.TrimPrefix(name, "-")
	// Prefix with "protect-" for clarity
	return "protect-" + name
}

// SetupGitHooksMount is a convenience function for backwards compatibility
// It mounts .git/hooks as read-only for security.
// Deprecated: Use SetupSecurityMounts with config.Security.GetEffectiveProtectedPaths() instead
func SetupGitHooksMount(mgr *container.Manager, workspacePath string, useShift bool) error {
	// Use /workspace as the default container path for backwards compatibility
	return SetupSecurityMounts(mgr, workspacePath, "/workspace", []string{".git/hooks"}, useShift)
}

// GetProtectedPathsForLogging returns a human-readable list of protected paths
// that actually exist in the workspace
func GetProtectedPathsForLogging(workspacePath string, protectedPaths []string) []string {
	var existing []string
	for _, relPath := range protectedPaths {
		hostPath := filepath.Join(workspacePath, relPath)
		if info, err := os.Lstat(hostPath); err == nil {
			// Skip symlinks in the list
			if info.Mode()&os.ModeSymlink == 0 {
				existing = append(existing, relPath)
			}
		} else if relPath == ".git/hooks" {
			// .git/hooks will be created, so include it if .git exists
			gitDir := filepath.Join(workspacePath, ".git")
			if gitInfo, err := os.Lstat(gitDir); err == nil && gitInfo.IsDir() {
				existing = append(existing, relPath)
			}
		}
	}
	return existing
}
