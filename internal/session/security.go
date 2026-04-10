package session

import (
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
			// or a file-type entry whose parent directory is missing) surface
			// as os.ErrNotExist and are skipped silently. Any other failure
			// (MkdirAll/stat/mount errors) is propagated and surfaces as a
			// warning at the caller in setup.go.
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to protect %s: %w", relPath, err)
			}
		}
	}

	return nil
}

// setupProtectedPath mounts a single path as read-only
func setupProtectedPath(mgr *container.Manager, workspacePath, containerWorkspacePath, relPath string, useShift bool) error {
	hostPath := filepath.Join(workspacePath, relPath)
	containerPath := filepath.Join(containerWorkspacePath, relPath)

	// For .git paths, check if .git itself is valid FIRST (not a symlink or file)
	// This must happen before we try to create .git/hooks
	if strings.HasPrefix(relPath, ".git/") || relPath == ".git" {
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

	if err := ensureProtectedExists(hostPath, relPath); err != nil {
		return err // os.ErrNotExist surfaces for file-type with missing parent
	}

	// Use Lstat to avoid following symlinks (security measure)
	info, err := os.Lstat(hostPath)
	if err != nil {
		return fmt.Errorf("failed to stat %s after materialization: %w", relPath, err)
	}

	// Security check: reject symlinks to prevent mounting arbitrary host paths
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to mount for security reasons", relPath)
	}

	// Generate unique device name from path
	deviceName := pathToDeviceName(relPath)

	// Mount as read-only
	return mgr.MountDisk(deviceName, hostPath, containerPath, useShift, true)
}

// fileTypeProtectedPaths lists entries that should be protected as
// single files (empty placeholder) rather than as directories. Every
// other entry in protected_paths is treated as a directory and
// created with MkdirAll if missing. Extend this set in lockstep with
// internal/config/embedded/default_config.toml when adding new
// file-type defaults.
var fileTypeProtectedPaths = map[string]bool{
	".git/config": true,
}

func isFileTypeProtected(relPath string) bool {
	return fileTypeProtectedPaths[relPath]
}

// ensureProtectedExists materializes hostPath so it has a real inode
// to read-only-mount over. For directory-type entries, creates an
// empty dir. For file-type entries, creates an empty placeholder
// only if the parent dir already exists — we NEVER synthesize an
// entire parent tree. Existing files/dirs are left untouched.
func ensureProtectedExists(hostPath, relPath string) error {
	if _, err := os.Lstat(hostPath); err == nil {
		return nil // already exists, don't clobber
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat %s: %w", relPath, err)
	}

	if !isFileTypeProtected(relPath) {
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return fmt.Errorf("failed to create protected directory %s: %w", relPath, err)
		}
		return nil
	}

	// File-type: only create when parent exists.
	parent := filepath.Dir(hostPath)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("failed to stat parent of %s: %w", relPath, err)
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
