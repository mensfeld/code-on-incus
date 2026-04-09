package container

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var (
	CodeUID      = 1000
	CodeUser     = "code"
	IncusGroup   = "incus-admin"
	IncusProject = "default"
)

// Configure sets the package-level Incus configuration variables.
// This should be called after loading the config file to apply user settings.
func Configure(project, group, codeUser string, codeUID int) {
	IncusProject = project
	IncusGroup = group
	CodeUser = codeUser
	CodeUID = codeUID
}

// execIncusCommand creates an exec.Cmd for running incus commands.
// On Linux, it wraps the command with sg for group permissions.
// On macOS, it runs incus directly (no incus-admin group).
func execIncusCommand(cmdArgs []string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		// macOS: run incus directly without sg wrapper
		// cmdArgs is in format: [IncusGroup, "-c", "incus --project ... command"]
		// Extract the actual incus command from the third element
		incusCmd := cmdArgs[2] // "incus --project ... command"
		return exec.Command("sh", "-c", incusCmd)
	}
	// Linux: use sg for group permissions
	return exec.Command("sg", cmdArgs...)
}

// execIncusCommandContext creates a context-aware exec.Cmd for running incus commands.
// On Linux, it wraps the command with sg for group permissions.
// On macOS, it runs incus directly (no incus-admin group).
//
// WaitDelay is set so that when the context is cancelled, cmd.Wait returns
// promptly instead of blocking until all child-process pipes are closed.
func execIncusCommandContext(ctx context.Context, cmdArgs []string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		incusCmd := cmdArgs[2]
		cmd = exec.CommandContext(ctx, "sh", "-c", incusCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sg", cmdArgs...)
	}
	cmd.WaitDelay = time.Second
	return cmd
}

// IncusExecContext executes an Incus command with context support
func IncusExecContext(ctx context.Context, args ...string) error {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, cmdArgs)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IncusExec executes an Incus command via sg wrapper for group permissions (Linux) or directly (macOS)
func IncusExec(args ...string) error {
	return IncusExecContext(context.Background(), args...)
}

// IncusExecInteractive executes an Incus command with stdin/stdout/stderr attached
func IncusExecInteractive(args ...string) error {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommand(cmdArgs)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IncusExecQuietContext executes an Incus command silently with context support
func IncusExecQuietContext(ctx context.Context, args ...string) error {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, cmdArgs)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// IncusExecQuiet executes an Incus command silently (suppress stdout/stderr)
func IncusExecQuiet(args ...string) error {
	return IncusExecQuietContext(context.Background(), args...)
}

// IncusOutputContext executes an Incus command with context support and returns the output (trimmed)
func IncusOutputContext(ctx context.Context, args ...string) (string, error) {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, cmdArgs)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusOutput executes an Incus command and returns the output (trimmed)
func IncusOutput(args ...string) (string, error) {
	return IncusOutputContext(context.Background(), args...)
}

// IncusOutputRawContext executes an Incus command with context support and returns the output (not trimmed)
func IncusOutputRawContext(ctx context.Context, args ...string) (string, error) {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, cmdArgs)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	output := stdout.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusOutputRaw executes an Incus command and returns the output (not trimmed)
func IncusOutputRaw(args ...string) (string, error) {
	return IncusOutputRawContext(context.Background(), args...)
}

// IncusOutputWithStderrContext executes an Incus command with context support and returns combined stdout+stderr
func IncusOutputWithStderrContext(ctx context.Context, args ...string) (string, error) {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, cmdArgs)

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()
	output := strings.TrimSpace(combined.String())

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusOutputWithStderr executes an Incus command and returns combined stdout+stderr
// This is useful when error messages from Incus need to be inspected (e.g., "already frozen")
func IncusOutputWithStderr(args ...string) (string, error) {
	return IncusOutputWithStderrContext(context.Background(), args...)
}

// IncusOutputWithArgsContext executes incus with raw args and context support (no additional wrapping)
func IncusOutputWithArgsContext(ctx context.Context, args ...string) (string, error) {
	// Build command with project flag
	incusArgs := append([]string{"--project", IncusProject}, args...)

	// Build properly quoted command
	quotedArgs := make([]string, len(incusArgs))
	for i, arg := range incusArgs {
		quotedArgs[i] = shellQuote(arg)
	}

	incusCmd := "incus " + strings.Join(quotedArgs, " ")
	sgArgs := []string{IncusGroup, "-c", incusCmd}

	cmd := execIncusCommandContext(ctx, sgArgs)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
			}
		}
		return output, err
	}

	return output, nil
}

// IncusOutputWithArgs executes incus with raw args (no additional wrapping)
func IncusOutputWithArgs(args ...string) (string, error) {
	return IncusOutputWithArgsContext(context.Background(), args...)
}

// IncusFilePushContext pushes a file into a container with context support
func IncusFilePushContext(ctx context.Context, source, destination string) error {
	cmdArgs := buildIncusCommand("file", "push", source, destination)
	cmd := execIncusCommandContext(ctx, cmdArgs)
	return cmd.Run()
}

// IncusFilePush pushes a file into a container
func IncusFilePush(source, destination string) error {
	return IncusFilePushContext(context.Background(), source, destination)
}

// LaunchContainer launches an ephemeral container on the given storage pool.
// An empty pool means "use Incus's default pool".
// Uses init+configure+start (not launch) so security flags are set before first boot.
func LaunchContainer(imageAlias, containerName, pool string) error {
	args := []string{"init", imageAlias, containerName, "--ephemeral"}
	if pool != "" {
		args = append(args, "-s", pool)
	}
	if err := IncusExec(args...); err != nil {
		return err
	}
	if err := EnableDockerSupport(containerName); err != nil {
		return err
	}
	if err := CheckNotPrivileged(containerName); err != nil {
		return err
	}
	return IncusExec("start", containerName)
}

// LaunchContainerPersistent launches a non-ephemeral container on the given
// storage pool. An empty pool means "use Incus's default pool".
// Uses init+configure+start (not launch) so security flags are set before first boot.
func LaunchContainerPersistent(imageAlias, containerName, pool string) error {
	args := []string{"init", imageAlias, containerName}
	if pool != "" {
		args = append(args, "-s", pool)
	}
	if err := IncusExec(args...); err != nil {
		return err
	}
	if err := EnableDockerSupport(containerName); err != nil {
		return err
	}
	if err := CheckNotPrivileged(containerName); err != nil {
		return err
	}
	return IncusExec("start", containerName)
}

// EnableDockerSupport configures the container to support Docker/nested containers.
//
// This function sets security flags and sysctl overrides required for Docker:
//   - security.nesting=true: Enables nested containerization
//   - security.syscalls.intercept.mknod=true: Safe device node creation
//   - security.syscalls.intercept.setxattr=true: Safe filesystem attribute handling
//   - linux.sysctl.net.ipv4.ip_unprivileged_port_start=0: Allows binding to low ports
//     and prevents runc from failing with "permission denied" on sysctl writes (#187)
//
// These flags must be set before the container's first boot so the kernel loads
// the correct seccomp profile. Setting them on a running container is a race
// condition that can cause Docker Compose to fail with sysctl permission errors.
//
// Note: If an error occurs during configuration, the container may be left in a
// partially configured state with some but not all flags set. Future troubleshooting
// should verify all four settings are properly configured if Docker isn't working.
func EnableDockerSupport(containerName string) error {
	// Enable container nesting for Docker support
	if err := IncusExec("config", "set", containerName, "security.nesting=true"); err != nil {
		return err
	}

	// Enable syscall interception for mknod (device node creation)
	if err := IncusExec("config", "set", containerName, "security.syscalls.intercept.mknod=true"); err != nil {
		return err
	}

	// Enable syscall interception for setxattr (filesystem attributes)
	if err := IncusExec("config", "set", containerName, "security.syscalls.intercept.setxattr=true"); err != nil {
		return err
	}

	// Allow unprivileged port binding and prevent runc sysctl permission errors.
	// Newer runc versions (1.3.x) try to write net.ipv4.ip_unprivileged_port_start
	// via a detached procfs mount, which AppArmor blocks in nested containers.
	// Pre-setting this sysctl at the Incus level avoids the permission denied error.
	if err := IncusExec("config", "set", containerName, "linux.sysctl.net.ipv4.ip_unprivileged_port_start=0"); err != nil {
		return err
	}

	return nil
}

// StopContainer stops a container
func StopContainer(containerName string) error {
	return IncusExec("stop", containerName, "--force")
}

// DeleteContainer deletes a container forcefully
func DeleteContainer(containerName string) error {
	return IncusExecQuiet("delete", containerName, "--force")
}

// ContainerRunning checks if a container is running
func ContainerRunning(containerName string) (bool, error) {
	output, err := IncusOutput("list", containerName, "--format=json")
	if err != nil {
		return false, err
	}

	var containers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return false, err
	}

	for _, c := range containers {
		if c.Name == containerName && c.Status == "Running" {
			return true, nil
		}
	}

	return false, nil
}

// PublishContainer publishes a stopped container as an image
func PublishContainer(containerName, aliasName, description, compression string) (string, error) {
	// Stop container if running (ignore error if already stopped)
	running, _ := ContainerRunning(containerName)
	if running {
		if err := StopContainer(containerName); err != nil {
			return "", err
		}
	}

	// Build publish command
	args := []string{"publish", containerName, "--alias", aliasName}
	if compression != "" {
		args = append(args, "--compression", compression)
	}
	if description != "" {
		args = append(args, fmt.Sprintf("description=%s", description))
	}

	// Execute and capture output
	output, err := IncusOutput(args...)
	if err != nil {
		return "", err
	}

	// Extract fingerprint from output
	re := regexp.MustCompile(`fingerprint:\s*([a-f0-9]+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract fingerprint from output")
	}

	fingerprint := matches[1]

	// Cleanup container after successful publish
	if err := DeleteContainer(containerName); err != nil {
		return fingerprint, err // Return fingerprint even if cleanup fails
	}

	return fingerprint, nil
}

// DeleteImage deletes an image by alias
func DeleteImage(aliasName string) error {
	return IncusExecQuiet("image", "delete", aliasName)
}

// ImageExists checks if an image with the given alias exists
func ImageExists(aliasName string) (bool, error) {
	output, err := IncusOutput("image", "list", "--format=json")
	if err != nil {
		return false, err
	}

	var images []struct {
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal([]byte(output), &images); err != nil {
		return false, err
	}

	for _, img := range images {
		for _, alias := range img.Aliases {
			if alias.Name == aliasName {
				return true, nil
			}
		}
	}

	return false, nil
}

// ListImagesByPrefix lists images by alias prefix
func ListImagesByPrefix(prefix string) ([]string, error) {
	output, err := IncusOutput("image", "list", "--format=json")
	if err != nil {
		return nil, err
	}

	var images []struct {
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal([]byte(output), &images); err != nil {
		return nil, err
	}

	var matching []string
	for _, img := range images {
		for _, alias := range img.Aliases {
			if strings.HasPrefix(alias.Name, prefix) {
				matching = append(matching, alias.Name)
			}
		}
	}

	return matching, nil
}

// ListContainers lists all containers matching a name pattern
func ListContainers(pattern string) ([]string, error) {
	output, err := IncusOutput("list", "--format=json")
	if err != nil {
		return nil, err
	}

	var containers []struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return nil, err
	}

	// Compile pattern as regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	var matching []string
	for _, c := range containers {
		if re.MatchString(c.Name) {
			matching = append(matching, c.Name)
		}
	}

	return matching, nil
}

// buildIncusCommand builds the full incus command with project flag
func buildIncusCommand(args ...string) []string {
	incusArgs := append([]string{"--project", IncusProject}, args...)

	// Properly quote arguments for shell execution
	quotedArgs := make([]string, len(incusArgs))
	for i, arg := range incusArgs {
		quotedArgs[i] = shellQuote(arg)
	}

	incusCmd := "incus " + strings.Join(quotedArgs, " ")
	return []string{IncusGroup, "-c", incusCmd}
}

// shellQuote quotes a string for safe use in a shell command
func shellQuote(s string) string {
	// If string contains no special characters, don't quote
	if regexp.MustCompile(`^[a-zA-Z0-9@%+=:,./_-]+$`).MatchString(s) {
		return s
	}

	// Otherwise, single-quote and escape any single quotes
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// SnapshotCreate creates a snapshot of a container
func SnapshotCreate(containerName, snapshotName string, stateful bool) error {
	args := []string{"snapshot", "create", containerName, snapshotName}
	if stateful {
		args = append(args, "--stateful")
	}
	return IncusExec(args...)
}

// SnapshotList lists snapshots for a container in JSON format
func SnapshotList(containerName string) (string, error) {
	return IncusOutput("snapshot", "list", containerName, "--format=json")
}

// SnapshotRestore restores a container from a snapshot
func SnapshotRestore(containerName, snapshotName string, stateful bool) error {
	args := []string{"snapshot", "restore", containerName, snapshotName}
	if stateful {
		args = append(args, "--stateful")
	}
	return IncusExec(args...)
}

// SnapshotDelete deletes a snapshot from a container
func SnapshotDelete(containerName, snapshotName string) error {
	return IncusExec("snapshot", "delete", containerName, snapshotName)
}
