package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Manager provides a clean interface for Incus container operations
type Manager struct {
	ContainerName string
}

// ExitError represents a command that ran but exited with non-zero status
type ExitError struct {
	ExitCode int
	Err      error
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.ExitCode)
}

// NewManager creates a new container manager
func NewManager(containerName string) *Manager {
	return &Manager{
		ContainerName: containerName,
	}
}

// Launch creates a new container from an image
func (m *Manager) Launch(image string, ephemeral bool) error {
	if ephemeral {
		return LaunchContainer(image, m.ContainerName)
	}
	return LaunchContainerPersistent(image, m.ContainerName)
}

// Stop stops the container
func (m *Manager) Stop(force bool) error {
	if force {
		return StopContainer(m.ContainerName)
	}
	return IncusExec("stop", m.ContainerName)
}

// Delete deletes the container
func (m *Manager) Delete(force bool) error {
	if force {
		return DeleteContainer(m.ContainerName)
	}
	return IncusExec("delete", m.ContainerName)
}

// Running checks if the container is running
func (m *Manager) Running() (bool, error) {
	return ContainerRunning(m.ContainerName)
}

// Exists checks if container exists (running or stopped)
func (m *Manager) Exists() (bool, error) {
	output, err := IncusOutput("list", "^"+m.ContainerName+"$", "--format=csv", "--columns=n")
	if err != nil {
		return false, err
	}
	return len(output) > 0 && output != "\n", nil
}

// Start starts a stopped container
func (m *Manager) Start() error {
	return IncusExec("start", m.ContainerName)
}

// MountDisk adds a disk device to the container
func (m *Manager) MountDisk(name, source, path string, shift, readonly bool) error {
	args := []string{
		"config", "device", "add", m.ContainerName, name, "disk",
		fmt.Sprintf("source=%s", source),
		fmt.Sprintf("path=%s", path),
	}
	if shift {
		args = append(args, "shift=true")
	}
	if readonly {
		args = append(args, "readonly=true")
	}

	return IncusExec(args...)
}

// SetTmpfsSize configures the tmpfs size for /tmp in the container
// size should be a string like "2GiB", "1024MiB", etc.
func (m *Manager) SetTmpfsSize(size string) error {
	// For tmpfs, we need to use limits.memory instead of disk device size
	// First, add tmpfs disk device (without size, which would error)
	args := []string{
		"config", "device", "override", m.ContainerName, "tmp", "disk",
		"source=tmpfs",
		"path=/tmp",
	}
	if err := IncusExec(args...); err != nil {
		// If override fails, try adding (container might not have tmp device)
		args[2] = "add"
		if err := IncusExec(args...); err != nil {
			return err
		}
	}

	// Then set the size via limits.memory.tmpfs
	return IncusExec("config", "set", m.ContainerName, fmt.Sprintf("limits.memory.tmpfs=%s", size))
}

// Exec executes a command in the container (no output capture)
func (m *Manager) Exec(args ...string) error {
	cmdArgs := append([]string{"exec", m.ContainerName, "--"}, args...)
	return IncusExec(cmdArgs...)
}

// ExecArgs executes command arguments in the container with options
func (m *Manager) ExecArgs(commandArgs []string, opts ExecCommandOptions) error {
	args := []string{"exec", m.ContainerName}

	// Add force-interactive flag for interactive sessions (required for tmux attach)
	if opts.Interactive {
		args = append(args, "--force-interactive")
	}

	// Add environment variables
	for k, v := range opts.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Add working directory
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}

	// Add user/group
	if opts.User != nil {
		args = append(args, "--user", fmt.Sprintf("%d", *opts.User))
		group := opts.User // default to same as user
		if opts.Group != nil {
			group = opts.Group
		}
		args = append(args, "--group", fmt.Sprintf("%d", *group))
	}

	// Add command arguments
	args = append(args, "--")
	args = append(args, commandArgs...)

	// Support interactive mode
	if opts.Interactive {
		return IncusExecInteractive(args...)
	}

	return IncusExec(args...)
}

// ExecArgsCapture executes a command with raw arguments and captures output (no bash -c wrapping, preserves whitespace)
func (m *Manager) ExecArgsCapture(commandArgs []string, opts ExecCommandOptions) (string, error) {
	args := []string{"exec", m.ContainerName}

	// Add environment variables
	for k, v := range opts.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Add working directory
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}

	// Add user/group
	if opts.User != nil {
		args = append(args, "--user", fmt.Sprintf("%d", *opts.User))
		group := opts.User // default to same as user
		if opts.Group != nil {
			group = opts.Group
		}
		args = append(args, "--group", fmt.Sprintf("%d", *group))
	}

	// Add command arguments
	args = append(args, "--")
	args = append(args, commandArgs...)

	// Use IncusOutputRaw to preserve whitespace
	return IncusOutputRaw(args...)
}

// ExecCommandOptions holds options for executing commands
type ExecCommandOptions struct {
	User        *int
	Group       *int
	Cwd         string
	Env         map[string]string
	Capture     bool
	Interactive bool // Attach stdin/stdout/stderr for interactive sessions
}

// ExecCommand executes a bash command in the container with user context
func (m *Manager) ExecCommand(command string, opts ExecCommandOptions) (string, error) {
	args := []string{"exec", m.ContainerName}

	// Add force-interactive flag for interactive sessions (required for tmux attach)
	if opts.Interactive {
		args = append(args, "--force-interactive")
	}

	// Add environment variables
	for k, v := range opts.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Add working directory
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}

	// Add user/group
	if opts.User != nil {
		args = append(args, "--user", fmt.Sprintf("%d", *opts.User))
		group := opts.User // default to same as user
		if opts.Group != nil {
			group = opts.Group
		}
		args = append(args, "--group", fmt.Sprintf("%d", *group))
	}

	// Add command
	args = append(args, "--", "bash", "-c", command)

	if opts.Capture {
		return IncusOutput(args...)
	}

	if opts.Interactive {
		return "", IncusExecInteractive(args...)
	}

	return "", IncusExec(args...)
}

// PushFile pushes a file into the container
func (m *Manager) PushFile(source, destination string) error {
	// Ensure destination starts with /
	if destination[0] != '/' {
		destination = "/" + destination
	}
	dest := m.ContainerName + destination
	return IncusFilePush(source, dest)
}

// PullDirectory pulls a directory from the container recursively
func (m *Manager) PullDirectory(containerPath, localPath string) error {
	// Incus creates a subdirectory when pulling, so we pull to a temp location
	// then move the contents to the desired location
	tempDir, err := os.MkdirTemp("", "coi-pull-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Pull to temp directory (creates tempDir/dirname/)
	source := m.ContainerName + containerPath
	if err := IncusExec("file", "pull", "-r", source, tempDir); err != nil {
		return err
	}

	// Find the pulled directory (it will be the only item in tempDir)
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no files pulled")
	}

	// Move the pulled directory to the desired location
	pulledDir := filepath.Join(tempDir, entries[0].Name())
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	// Remove destination if it exists
	os.RemoveAll(localPath)

	// Rename (move) the pulled directory to the final location
	// If rename fails with cross-device error, fall back to copy via a temp dir
	if err := os.Rename(pulledDir, localPath); err != nil {
		if isCrossDeviceError(err) {
			// Create a temporary directory on the same filesystem as localPath
			tempDestDir, err := os.MkdirTemp(filepath.Dir(localPath), "coi-pull-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tempDestDir)

			// Copy into a temp target, then atomically rename to the final location
			tempTarget := filepath.Join(tempDestDir, filepath.Base(localPath))
			if err := copyDirRecursive(pulledDir, tempTarget); err != nil {
				return err
			}
			return os.Rename(tempTarget, localPath)
		}
		return err
	}
	return nil
}

// isCrossDeviceError checks if the error is a cross-device link error (EXDEV)
func isCrossDeviceError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		if errno, ok := linkErr.Err.(syscall.Errno); ok {
			return errno == syscall.EXDEV
		}
	}
	return false
}

// copyDirRecursive copies a directory recursively from src to dst
func copyDirRecursive(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Handle symlinks
		if entry.Type()&os.ModeSymlink != 0 {
			if err := copySymlink(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copySymlink copies a symbolic link from src to dst
func copySymlink(src, dst string) error {
	link, err := os.Readlink(src)
	if err != nil {
		return err
	}
	return os.Symlink(link, dst)
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode()) //nolint:gosec // G703: dst is a validated internal path passed by the caller, not user-supplied
	if err != nil {
		return err
	}

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		// Best-effort close; prefer the copy error if both occur.
		_ = dstFile.Close()
		return err
	}

	// Propagate any error that occurs while flushing/closing the writable file.
	return dstFile.Close()
}

// PushDirectory pushes a directory to the container recursively
func (m *Manager) PushDirectory(localPath, containerPath string) error {
	// Check if source directory exists
	if info, err := os.Stat(localPath); err != nil || !info.IsDir() {
		return nil // Skip if not a directory (intentional nilerr)
	}

	// Incus creates a subdirectory when pushing, so we push to the parent
	// e.g., pushing /local/dir to container/remote/parent/ creates /remote/parent/dir
	// To get /remote/dir, we need to push to container/remote/
	parentPath := containerPath[:strings.LastIndex(containerPath, "/")+1]
	if parentPath == "" {
		parentPath = "/"
	}
	dest := m.ContainerName + parentPath
	return IncusExec("file", "push", "-r", localPath, dest)
}

// Chown changes ownership of a path in the container
func (m *Manager) Chown(path string, uid, gid int) error {
	cmd := fmt.Sprintf("chown -R %d:%d %s", uid, gid, path)
	_, err := m.ExecCommand(cmd, ExecCommandOptions{})
	return err
}

// DirExists checks if a directory exists in the container
func (m *Manager) DirExists(path string) (bool, error) {
	cmd := fmt.Sprintf("[ -d %s ]", path)
	_, err := m.ExecCommand(cmd, ExecCommandOptions{})
	return err == nil, nil
}

// FileExists checks if a file exists in the container
func (m *Manager) FileExists(path string) (bool, error) {
	cmd := fmt.Sprintf("[ -f %s ]", path)
	_, err := m.ExecCommand(cmd, ExecCommandOptions{})
	return err == nil, nil
}

// Available checks if Incus is available on this system
func Available() bool {
	// Check if incus binary exists
	if _, err := exec.LookPath("incus"); err != nil {
		return false
	}

	// On macOS, run incus directly without sg group switching
	// macOS doesn't have the incus-admin group like Linux
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("incus", "--project", IncusProject, "info")
	} else {
		// Linux - use sg to run with group permissions
		cmd = exec.Command("sg", IncusGroup, "-c", fmt.Sprintf("incus --project %s info", IncusProject))
	}

	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// ImageExistsGlobal checks if an image exists (class method equivalent)
func ImageExistsGlobal(imageAlias string) (bool, error) {
	return ImageExists(imageAlias)
}

// Helper function to create a file with content
func (m *Manager) CreateFile(containerPath, content string) error {
	// Create temp file locally
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("coi-%s", filepath.Base(containerPath)))
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	// Push to container
	return m.PushFile(tmpFile, containerPath)
}

// ExecHostCommand executes a command on the host (not in container)
func (m *Manager) ExecHostCommand(command string, capture bool) (string, error) {
	// Use sg wrapper if needed, otherwise direct execution
	cmd := exec.Command("sh", "-c", command)

	if capture {
		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	return "", cmd.Run()
}

// SnapshotInfo holds information about a container snapshot
type SnapshotInfo struct {
	Name        string     `json:"name"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Stateful    bool       `json:"stateful"`
	Description string     `json:"description,omitempty"`
}

// CreateSnapshot creates a snapshot of the container
func (m *Manager) CreateSnapshot(name string, stateful bool) error {
	return SnapshotCreate(m.ContainerName, name, stateful)
}

// ListSnapshots lists all snapshots for the container
func (m *Manager) ListSnapshots() ([]SnapshotInfo, error) {
	output, err := SnapshotList(m.ContainerName)
	if err != nil {
		return nil, err
	}

	// Parse JSON output from incus snapshot list
	var rawSnapshots []struct {
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
		ExpiresAt string `json:"expires_at"`
		Stateful  bool   `json:"stateful"`
	}

	if err := json.Unmarshal([]byte(output), &rawSnapshots); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot list: %w", err)
	}

	// Convert to SnapshotInfo
	snapshots := make([]SnapshotInfo, 0, len(rawSnapshots))
	for _, raw := range rawSnapshots {
		info := SnapshotInfo{
			Name:     raw.Name,
			Stateful: raw.Stateful,
		}

		// Parse created_at
		if raw.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, raw.CreatedAt); err == nil {
				info.CreatedAt = t
			}
		}

		// Parse expires_at if present
		if raw.ExpiresAt != "" && raw.ExpiresAt != "0001-01-01T00:00:00Z" {
			if t, err := time.Parse(time.RFC3339, raw.ExpiresAt); err == nil {
				info.ExpiresAt = &t
			}
		}

		snapshots = append(snapshots, info)
	}

	return snapshots, nil
}

// RestoreSnapshot restores the container from a snapshot
func (m *Manager) RestoreSnapshot(name string, stateful bool) error {
	return SnapshotRestore(m.ContainerName, name, stateful)
}

// DeleteSnapshot deletes a snapshot from the container
func (m *Manager) DeleteSnapshot(name string) error {
	return SnapshotDelete(m.ContainerName, name)
}

// SnapshotExists checks if a snapshot exists for the container
func (m *Manager) SnapshotExists(name string) (bool, error) {
	snapshots, err := m.ListSnapshots()
	if err != nil {
		return false, err
	}

	for _, s := range snapshots {
		if s.Name == name {
			return true, nil
		}
	}

	return false, nil
}

// GetSnapshotInfo retrieves detailed information about a specific snapshot
func (m *Manager) GetSnapshotInfo(name string) (*SnapshotInfo, error) {
	snapshots, err := m.ListSnapshots()
	if err != nil {
		return nil, err
	}

	for _, s := range snapshots {
		if s.Name == name {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("snapshot '%s' not found for container '%s'", name, m.ContainerName)
}
