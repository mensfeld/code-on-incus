package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// secretMask is an expanded secret path to mask, with whether it is a directory.
type secretMask struct {
	relPath string
	isDir   bool
}

// ExpandSecretPaths resolves the workspace-relative secret_paths globs to
// concrete existing entries to mask. Supported patterns:
//   - exact paths (".env"),
//   - filepath.Glob patterns ("*.pem", "config/*.key"),
//   - a trailing "/**" ("secrets/**"), which masks the base directory and thereby
//     everything under it.
//
// Only entries that exist on disk are returned (you cannot mask what is not
// there). Symlinks are skipped — we never follow them when masking. Pure; safe to
// unit-test without a container.
func ExpandSecretPaths(workspacePath string, secretPaths []string) []secretMask {
	seen := make(map[string]bool)
	var out []secretMask

	add := func(rel string) {
		rel = filepath.Clean(rel)
		if seen[rel] || validateRelPath(rel) != nil {
			return
		}
		info, err := os.Lstat(filepath.Join(workspacePath, rel))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return // does not exist, or a symlink we refuse to follow
		}
		seen[rel] = true
		out = append(out, secretMask{relPath: rel, isDir: info.IsDir()})
	}

	for _, pat := range secretPaths {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if base, ok := strings.CutSuffix(pat, "/**"); ok {
			add(base) // mask the whole directory
			continue
		}
		matches, err := filepath.Glob(filepath.Join(workspacePath, filepath.FromSlash(pat)))
		if err != nil {
			continue // malformed pattern
		}
		for _, m := range matches {
			if rel, err := filepath.Rel(workspacePath, m); err == nil {
				add(rel)
			}
		}
	}
	return out
}

// maskDeviceName derives a unique Incus device name for a masked path, distinct
// from the protect-* names used by read-only protected paths.
func maskDeviceName(relPath string) string {
	return "mask" + strings.TrimPrefix(pathToDeviceName(relPath), "protect")
}

// ensureMaskSources creates the empty file and empty directory used as read-only
// mask sources under ~/.coi/masks, returning their host paths.
func ensureMaskSources() (emptyFile, emptyDir string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	maskRoot := filepath.Join(home, ".coi", "masks")
	emptyDir = filepath.Join(maskRoot, "empty-dir")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		return "", "", err
	}
	emptyFile = filepath.Join(maskRoot, "empty-file")
	if _, statErr := os.Stat(emptyFile); os.IsNotExist(statErr) {
		f, ferr := os.OpenFile(emptyFile, os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			return "", "", ferr
		}
		_ = f.Close()
	}
	return emptyFile, emptyDir, nil
}

// SetupSecretMasks masks each existing secret path by mounting an empty,
// read-only file/dir over it inside the container, so the contained agent can
// neither read its contents nor modify it. The host file is left untouched.
// Returns the list of relPaths actually masked (for logging). Issue #494.
func SetupSecretMasks(mgr container.ContainerDevices, workspacePath, containerWorkspacePath string, secretPaths []string, useShift bool) ([]string, error) {
	masks := ExpandSecretPaths(workspacePath, secretPaths)
	if len(masks) == 0 {
		return nil, nil
	}
	emptyFile, emptyDir, err := ensureMaskSources()
	if err != nil {
		return nil, fmt.Errorf("failed to create secret mask sources: %w", err)
	}

	var masked []string
	for _, m := range masks {
		source := emptyFile
		if m.isDir {
			source = emptyDir
		}
		containerPath := filepath.Join(containerWorkspacePath, m.relPath)
		if err := mgr.MountDisk(maskDeviceName(m.relPath), source, containerPath, useShift, true); err != nil {
			return masked, fmt.Errorf("failed to mask secret path %s: %w", m.relPath, err)
		}
		masked = append(masked, m.relPath)
	}
	return masked, nil
}
