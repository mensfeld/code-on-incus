package container

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
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
	Stderr   string // captured stderr from the incus subprocess, if any
}

func (e *ExitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("exit status %d: %s", e.ExitCode, e.Stderr)
	}
	return fmt.Sprintf("exit status %d", e.ExitCode)
}

func (e *ExitError) Unwrap() error { return e.Err }

// NewManager creates a new container manager
func NewManager(containerName string) *Manager {
	return &Manager{
		ContainerName: containerName,
	}
}

// Launch creates a new container from an image on the given storage pool.
// An empty pool falls back to Incus's default pool.
func (m *Manager) Launch(image string, ephemeral bool, pool string) error {
	if ephemeral {
		return LaunchContainer(image, m.ContainerName, pool)
	}
	return LaunchContainerPersistent(image, m.ContainerName, pool)
}

// LaunchWithPreStart launches the container, running preStart after init/config
// but before start (for start-time-only settings like raw.idmap; see #530).
func (m *Manager) LaunchWithPreStart(image string, ephemeral bool, pool string, preStart func() error) error {
	return LaunchContainerWithPreStart(image, m.ContainerName, pool, ephemeral, preStart)
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

// AddProxyDevice adds a proxy device to the container for forwarding connections
// (e.g., Unix sockets). The connect string is the host side, listen is the container side.
func (m *Manager) AddProxyDevice(name, connect, listen string, uid, gid int) error {
	args := []string{
		"config", "device", "add", m.ContainerName, name, "proxy",
		fmt.Sprintf("connect=%s", connect),
		fmt.Sprintf("listen=%s", listen),
		"bind=container",
		fmt.Sprintf("uid=%d", uid),
		fmt.Sprintf("gid=%d", gid),
		"mode=0600",
	}
	return IncusExec(args...)
}

// AddHostPortDevice publishes a container TCP port on the host via a proxy
// device: it listens on listenAddr:hostPort in the HOST namespace and
// connects to 127.0.0.1:containerPort inside the container (bind=host), so
// even dev servers bound to container-localhost are reachable. NAT mode is
// deliberately not used — the userspace forkproxy keeps COI's nft isolation
// rules untouched (#558).
func (m *Manager) AddHostPortDevice(name, listenAddr string, hostPort, containerPort int) error {
	args := []string{
		"config", "device", "add", m.ContainerName, name, "proxy",
		fmt.Sprintf("listen=tcp:%s:%d", listenAddr, hostPort),
		fmt.Sprintf("connect=tcp:127.0.0.1:%d", containerPort),
		"bind=host",
	}
	return IncusExec(args...)
}

// RemoveDevice removes a device from the container (silently ignores if not found)
func (m *Manager) RemoveDevice(name string) error {
	return IncusExecQuiet("config", "device", "remove", m.ContainerName, name)
}

// SetTmpfsSize configures the tmpfs size for /tmp in the container
// size should be a string like "2GiB", "1024MiB", etc.
func (m *Manager) SetTmpfsSize(size string) error {
	args := []string{
		"config", "device", "override", m.ContainerName, "tmp", "disk",
		"source=tmpfs",
		"path=/tmp",
		fmt.Sprintf("size=%s", size),
	}
	if err := IncusExec(args...); err != nil {
		// If override fails, try adding (container might not have tmp device)
		args[2] = "add"
		if err := IncusExec(args...); err != nil {
			return err
		}
	}
	return nil
}

// GetWorkspacePath returns the container path where the "workspace" device is mounted.
// Returns "/workspace" as fallback if the workspace device is not found or cannot be read.
func (m *Manager) GetWorkspacePath() string {
	output, err := IncusOutput("config", "device", "show", m.ContainerName)
	if err != nil {
		return "/workspace" // fallback
	}

	// Parse YAML output to find workspace device path
	// Format is:
	// workspace:
	//   path: /some/path
	//   source: /host/path
	//   type: disk
	lines := strings.Split(output, "\n")
	inWorkspace := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "workspace:" {
			inWorkspace = true
			continue
		}
		if inWorkspace {
			// Check for a new device (line starts without indent)
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break // moved to a different device
			}
			if strings.HasPrefix(trimmed, "path:") {
				path := strings.TrimSpace(strings.TrimPrefix(trimmed, "path:"))
				if path != "" {
					return path
				}
			}
		}
	}

	return "/workspace" // fallback
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
	// Fail fast, before anything is transferred. This function used to clear
	// the way for the final rename with os.RemoveAll(localPath),
	// which recursively deleted whole host trees when a caller passed
	// an existing directory such as "../" as the destination.
	// Refusing an existing destination keeps every deletion
	// explicit and owned by the caller.
	if _, err := os.Lstat(localPath); err == nil {
		return fmt.Errorf("destination %q already exists; remove it or choose another name", localPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	// Incus creates a subdirectory when pulling, so we pull to a temp location
	// then move the contents to the desired location
	tempDir, err := os.MkdirTemp("", "coi-pull-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Pull to temp directory (creates tempDir/dirname/). Capture stderr into
	// the returned error — otherwise a missing source path surfaces as a
	// bare "exit status 1" and callers like session.saveSessionData cannot
	// tell "no config yet" apart from a real failure, turning benign
	// first-run cleanups into "Warning: Failed to save session data" noise.
	source := m.ContainerName + containerPath
	cmdArgs := buildIncusCommand("file", "pull", "-r", source, tempDir)
	cmd := execIncusCommand(cmdArgs)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrMsg := strings.TrimSpace(stderr.String())
		if stderrMsg == "" {
			return err
		}
		return fmt.Errorf("%s: %w", stderrMsg, err)
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

	// The pulled tree is container-controlled (untrusted). Drop every symlink and
	// special file (FIFO/socket/device) BEFORE materializing it on the host —
	// otherwise a container-planted symlink such as
	// `x -> /home/user/.ssh/authorized_keys` would be recreated on the host, a
	// symlink-extraction (Zip-Slip-class) host-tampering vector that per-link
	// target checks cannot fully close (chained symlinks defeat them). Best-effort
	// and done here so it covers both the os.Rename path and the copyDirRecursive
	// cross-device fallback.
	sanitizePulledTree(pulledDir)

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

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

// ErrRemoteIsDirectory is returned by PullFile when the remote source is a
// directory, which requires a recursive pull. Callers can errors.Is against it
// (rather than sniffing error text) to suggest the -r flag.
var ErrRemoteIsDirectory = errors.New("remote path is a directory; use a recursive pull")

// PullFile pulls a single file from the container to the host.
// The destination's parent is created if missing.
// If localPath is an existing file, it is replaced atomically.
// If localPath is an existing directory, nothing happens and an error is returned.
// Content is staged in a temp directory first, so a failed transfer cannot leave a truncated file behind.
func (m *Manager) PullFile(containerPath, localPath string) error {
	if !strings.HasPrefix(containerPath, "/") {
		containerPath = "/" + containerPath
	}

	if fi, err := os.Lstat(localPath); err == nil && fi.IsDir() {
		return fmt.Errorf("destination %q is an existing directory; remove it or choose another name", localPath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Create staging dir
	tempDir, err := os.MkdirTemp("", "coi-pull-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Pull into the staging dir
	source := m.ContainerName + containerPath
	cmdArgs := buildIncusCommand("file", "pull", source, tempDir)
	cmd := execIncusCommand(cmdArgs)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrMsg := strings.TrimSpace(stderr.String())
		// incus reports a non-recursive pull of a directory via stderr; surface it as
		// a typed error so the CLI can suggest -r without sniffing text itself.
		if strings.Contains(stderrMsg, "directory") || strings.Contains(stderrMsg, "recursive") {
			return fmt.Errorf("%s: %w", stderrMsg, ErrRemoteIsDirectory)
		}
		if stderrMsg == "" {
			return err
		}
		return fmt.Errorf("%s: %w", stderrMsg, err)
	}

	// Take whatever incus staged rather than assuming its basename: a single-file
	// pull writes exactly one entry into the (empty) staging dir.
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return fmt.Errorf("expected exactly one entry in staging directory, got %d", len(entries))
	}
	staged := filepath.Join(tempDir, entries[0].Name())
	fi, err := os.Lstat(staged)
	if err != nil {
		return fmt.Errorf("pulled file missing from staging directory: %w", err)
	}
	if fi.IsDir() {
		return fmt.Errorf("refusing to pull %s: %w", containerPath, ErrRemoteIsDirectory)
	}
	// Container content is untrusted so only materialize regular files on the host
	// (drop symlinks/special files, same policy as sanitizePulledTree for trees).
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("refusing to pull %s: not a regular file", containerPath)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	if err := os.Rename(staged, localPath); err != nil {
		if isCrossDeviceError(err) {
			// Copy to a temp file on the destination filesystem, then rename
			// so the destination is still replaced atomically.
			tempDestDir, err := os.MkdirTemp(filepath.Dir(localPath), "coi-pull-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tempDestDir)

			tempTarget := filepath.Join(tempDestDir, filepath.Base(localPath))
			if err := copyFile(staged, tempTarget); err != nil {
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

// sanitizePulledTree drops every entry under root that is not a regular file or
// a directory — i.e. symlinks, FIFOs, sockets, and device nodes.
//
// Content pulled from a container is untrusted. Recreating a container symlink
// on the host is a symlink-extraction (Zip-Slip-class) host-tampering vector,
// and per-link target validation is defeatable by chained symlinks (a symlink to
// the parent plus a link traversing through it both pass a lexical "within root"
// check yet escape at runtime). Dropping ALL symlinks — and special files, which
// can hang or confuse the host-side copy — removes the whole class. Only regular
// files and directories, which cannot point outside the tree, are kept.
//
// Best-effort: walk and remove errors are logged, never fatal, so a transient
// cleanup error cannot turn an otherwise-fine session-state save into a hard
// failure. Removal happens after the walk (no fs mutation inside the WalkDir
// callback; gosec G122).
func sanitizePulledTree(root string) {
	var toRemove []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping unwalkable pulled entry %s: %v\n", path, walkErr)
			return nil
		}
		if d.IsDir() || d.Type().IsRegular() {
			return nil
		}
		// symlink / FIFO / socket / device / other irregular type — drop it.
		fmt.Fprintf(os.Stderr, "Warning: dropping non-regular entry from pulled content: %s\n", path)
		toRemove = append(toRemove, path)
		return nil
	})

	for _, p := range toRemove {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove pulled entry %s: %v\n", p, err)
		}
	}
}

// copySymlink copies a symbolic link from src to dst. Untrusted (container)
// trees are passed through sanitizePulledTree first, which strips all symlinks,
// so this is reached only for trusted/internal copies.
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
	return m.ExecArgs([]string{"chown", "-R", fmt.Sprintf("%d:%d", uid, gid), path}, ExecCommandOptions{})
}

// DirExists checks if a directory exists in the container
func (m *Manager) DirExists(path string) (bool, error) {
	err := m.ExecArgs([]string{"test", "-d", path}, ExecCommandOptions{})
	return err == nil, nil
}

// FileExists checks if a file exists in the container
func (m *Manager) FileExists(path string) (bool, error) {
	err := m.ExecArgs([]string{"test", "-f", path}, ExecCommandOptions{})
	return err == nil, nil
}

// Available checks if Incus is available on this system
func Available() bool {
	// Check if incus binary exists
	if _, err := exec.LookPath("incus"); err != nil {
		return false
	}

	cmd := exec.Command("incus", "--project", IncusProject, "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// IncusNotAvailableError returns a descriptive error explaining why Incus is not
// accessible. It distinguishes the following cases so the user knows exactly
// what to do:
//  1. Incus binary is not installed.
//  2. incus-admin group does not exist (Incus not properly installed).
//  3. User is in the incus-admin group in /etc/group but the current session
//     was started before the group was added — a re-login is needed.
//  4. User is not in the incus-admin group at all.
//  5. Group is active in the session but the Incus daemon is not running.
func IncusNotAvailableError() error {
	if _, err := exec.LookPath("incus"); err != nil {
		return fmt.Errorf("incus is not installed — please install Incus first")
	}

	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("incus is not available — please ensure you are in the incus-admin group")
	}

	incusGroup, err := user.LookupGroup("incus-admin")
	if err != nil {
		// Group doesn't exist — Incus not installed properly or daemon not running.
		return fmt.Errorf("incus is not available — the incus-admin group does not exist; please install Incus")
	}

	// Use os.Getgroups() (getgroups(2)) to get the actual supplementary GIDs
	// of the current process — not the group database. This is what determines
	// whether the running session can actually use incus.
	incusGID, _ := strconv.Atoi(incusGroup.Gid)
	activeGIDs, err := os.Getgroups()
	if err == nil {
		for _, gid := range activeGIDs {
			if gid == incusGID {
				// Group is active but incus still doesn't work — daemon issue.
				return fmt.Errorf("incus is not available — please check that the Incus daemon is running (sudo systemctl start incus)")
			}
		}
	}

	// Group exists but is not active — check whether it's in /etc/group.
	if UserInGroupFile(currentUser.Username, "incus-admin") {
		return fmt.Errorf(
			"you have been added to the incus-admin group but your current session does not have it active yet\n" +
				"Log out and back in, or run: newgrp incus-admin",
		)
	}

	return fmt.Errorf(
		"incus is not available — please add your user to the incus-admin group and re-login:\n" +
			"  sudo usermod -aG incus-admin $USER\n" +
			"Then log out and back in, or run: newgrp incus-admin",
	)
}

// UserInGroupFile reports whether username appears in the member list of
// groupName in /etc/group, regardless of whether the group is active in the
// current session.
func UserInGroupFile(username, groupName string) bool {
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, groupName+":") {
			continue
		}
		// Format: name:password:gid:member1,member2,...
		parts := strings.SplitN(line, ":", 4)
		if len(parts) != 4 {
			continue
		}
		for _, member := range strings.Split(parts[3], ",") {
			if strings.TrimSpace(member) == username {
				return true
			}
		}
	}
	return false
}

// Helper function to create a file with content
func (m *Manager) CreateFile(containerPath, content string) error {
	// Create a unique temp file to avoid collisions with concurrent sessions
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("coi-%s-*", filepath.Base(containerPath)))
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Push to container
	return m.PushFile(tmpPath, containerPath)
}

// ExecHostCommand executes a command on the host (not in container)
func (m *Manager) ExecHostCommand(command string, capture bool) (string, error) {
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
