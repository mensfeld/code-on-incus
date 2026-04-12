package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/mensfeld/code-on-incus/internal/container"
	"golang.org/x/sys/unix"
)

// ioctl constants for Linux 64-bit (the only platform COI targets).
const (
	fsIOCGetFlags uintptr = 0x80086601 // _IOR('f', 1, long)
	fsIOCSetFlags uintptr = 0x40086602 // _IOW('f', 2, long)
	fsImmutableFL int     = 0x00000010 // FS_IMMUTABLE_FL
)

// ImmutableManifest records which paths had the immutable attribute applied,
// enabling crash recovery: if COI is killed mid-session, `coi clean` can
// find the manifest and clear the bits.
type ImmutableManifest struct {
	ContainerName string   `json:"container_name"`
	Workspace     string   `json:"workspace"`
	Paths         []string `json:"paths"`
	AppliedAt     string   `json:"applied_at"`
}

// ApplyImmutable sets FS_IMMUTABLE_FL on each protected path (host-side).
// For directories, applies recursively. Writes a manifest for crash recovery.
// Returns the list of paths that were made immutable (empty if degraded).
func ApplyImmutable(workspacePath string, protectedPaths []string, containerName string, logger func(string)) []string {
	var applied []string

	for _, relPath := range protectedPaths {
		hostPath := filepath.Join(workspacePath, filepath.Clean(relPath))

		info, err := os.Lstat(hostPath)
		if err != nil {
			continue // path doesn't exist, skip
		}

		// Skip symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if info.IsDir() {
			err = setImmutableRecursive(hostPath)
		} else {
			err = setImmutable(hostPath)
		}

		if err != nil {
			// Graceful degradation: EPERM (no capability), ENOTTY/EOPNOTSUPP
			// (unsupported filesystem like virtiofs/9p/sshfs on macOS)
			if isImmutableUnsupported(err) {
				logger(fmt.Sprintf("Warning: Cannot set immutable attribute on protected paths: %v", err))
				logger("  Protected paths rely on read-only bind mounts only (bypassable with root in container).")
				logger("  To enable host-side immutable protection, run:")
				logger("    sudo setcap cap_linux_immutable=ep $(which coi)")
				return nil
			}
			logger(fmt.Sprintf("Warning: Failed to set immutable on %s: %v", relPath, err))
			continue
		}

		applied = append(applied, relPath)
	}

	// Save manifest for crash recovery
	if len(applied) > 0 {
		manifest := &ImmutableManifest{
			ContainerName: containerName,
			Workspace:     workspacePath,
			Paths:         applied,
			AppliedAt:     time.Now().Format(time.RFC3339),
		}
		if err := saveImmutableManifest(manifest); err != nil {
			logger(fmt.Sprintf("Warning: Failed to save immutable manifest: %v", err))
		}
	}

	return applied
}

// RemoveImmutable clears FS_IMMUTABLE_FL from paths listed in the manifest.
// Safe to call multiple times. Removes the manifest on success.
func RemoveImmutable(containerName string, logger func(string)) {
	manifest := loadImmutableManifest(containerName)
	if manifest == nil {
		return
	}

	var failures int
	for _, relPath := range manifest.Paths {
		hostPath := filepath.Join(manifest.Workspace, filepath.Clean(relPath))

		info, err := os.Lstat(hostPath)
		if err != nil {
			continue // path gone, nothing to clear
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if info.IsDir() {
			err = clearImmutableRecursive(hostPath)
		} else {
			err = clearImmutable(hostPath)
		}

		if err != nil && !isImmutableUnsupported(err) {
			logger(fmt.Sprintf("Warning: Failed to clear immutable on %s: %v", relPath, err))
			failures++
		}
	}

	if failures == 0 {
		removeImmutableManifest(containerName)
	}
}

// CleanStaleImmutableLocks scans ~/.coi/immutable-locks/ for manifests
// whose container no longer exists and clears their immutable bits.
// Returns the number of cleaned locks.
func CleanStaleImmutableLocks(logger func(string)) int {
	dir := immutableManifestDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	cleaned := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		containerName := strings.TrimSuffix(entry.Name(), ".json")

		// Check if the container still exists
		if immutableContainerExists(containerName) {
			continue
		}

		// Container is gone — clear immutable bits and remove manifest
		logger(fmt.Sprintf("Cleaning stale immutable lock for %s", containerName))
		RemoveImmutable(containerName, logger)
		cleaned++
	}

	return cleaned
}

// immutableContainerExists checks if a container still exists via incus.
func immutableContainerExists(name string) bool {
	mgr := container.NewManager(name)
	exists, err := mgr.Exists()
	return err == nil && exists
}

// setImmutable sets FS_IMMUTABLE_FL on a single file or directory.
func setImmutable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	flags, err := ioctlGetFlags(f)
	if err != nil {
		return fmt.Errorf("ioctl get flags on %s: %w", path, err)
	}

	flags |= fsImmutableFL
	if err := ioctlSetFlags(f, flags); err != nil {
		return fmt.Errorf("ioctl set flags on %s: %w", path, err)
	}

	return nil
}

// clearImmutable clears FS_IMMUTABLE_FL from a single file or directory.
func clearImmutable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	flags, err := ioctlGetFlags(f)
	if err != nil {
		return fmt.Errorf("ioctl get flags on %s: %w", path, err)
	}

	flags &^= fsImmutableFL
	if err := ioctlSetFlags(f, flags); err != nil {
		return fmt.Errorf("ioctl set flags on %s: %w", path, err)
	}

	return nil
}

// setImmutableRecursive walks a directory tree and sets FS_IMMUTABLE_FL
// on every entry, starting from leaves and ending with the root directory.
// Setting the directory last ensures we can still traverse it while setting
// children.
func setImmutableRecursive(dirPath string) error {
	var files []string
	var dirs []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			dirs = append(dirs, path)
		} else {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", dirPath, err)
	}

	// Set immutable on files first
	for _, f := range files {
		if err := setImmutable(f); err != nil {
			return err
		}
	}

	// Set immutable on directories in reverse order (deepest first, root last)
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := setImmutable(dirs[i]); err != nil {
			return err
		}
	}

	return nil
}

// clearImmutableRecursive walks a directory tree and clears FS_IMMUTABLE_FL.
// Clears the root directory first so we can traverse into it, then children.
func clearImmutableRecursive(dirPath string) error {
	if err := clearImmutable(dirPath); err != nil {
		return err
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dirPath, err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(dirPath, entry.Name())

		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			if err := clearImmutableRecursive(entryPath); err != nil {
				return err
			}
		} else {
			if err := clearImmutable(entryPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// ioctlGetFlags reads the FS flags (FS_IOC_GETFLAGS) from the file.
func ioctlGetFlags(f *os.File) (int, error) {
	var flags int
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), fsIOCGetFlags, uintptr(unsafe.Pointer(&flags)))
	if errno != 0 {
		return 0, errno
	}
	return flags, nil
}

// ioctlSetFlags writes the FS flags (FS_IOC_SETFLAGS) to the file.
func ioctlSetFlags(f *os.File, flags int) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), fsIOCSetFlags, uintptr(unsafe.Pointer(&flags)))
	if errno != 0 {
		return errno
	}
	return nil
}

// isImmutableUnsupported returns true for errors that indicate the
// immutable attribute is not available (missing capability, unsupported
// filesystem). These trigger graceful degradation rather than hard errors.
func isImmutableUnsupported(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	// Check for specific errno values wrapped inside ioctl error messages
	errStr := err.Error()
	return strings.Contains(errStr, "operation not permitted") ||
		strings.Contains(errStr, "inappropriate ioctl") ||
		strings.Contains(errStr, "not supported") ||
		strings.Contains(errStr, "operation not supported")
}

// immutableManifestDir returns the directory for immutable lock manifests.
// It is a variable so tests can override it.
var immutableManifestDir = defaultImmutableManifestDir

func defaultImmutableManifestDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	return filepath.Join(homeDir, ".coi", "immutable-locks")
}

// saveImmutableManifest writes the manifest to disk.
func saveImmutableManifest(manifest *ImmutableManifest) error {
	dir := immutableManifestDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	path := filepath.Join(dir, manifest.ContainerName+".json")
	return os.WriteFile(path, data, 0o644)
}

// loadImmutableManifest reads the manifest for a container.
func loadImmutableManifest(containerName string) *ImmutableManifest {
	path := filepath.Join(immutableManifestDir(), containerName+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var manifest ImmutableManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	return &manifest
}

// removeImmutableManifest deletes the manifest file for a container.
func removeImmutableManifest(containerName string) {
	path := filepath.Join(immutableManifestDir(), containerName+".json")
	os.Remove(path) //nolint:errcheck // best-effort cleanup
}
