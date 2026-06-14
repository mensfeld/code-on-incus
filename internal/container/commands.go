package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	CodeUID      = 1000
	CodeUser     = "code"
	IncusProject = "default"
)

// Configure sets the package-level Incus configuration variables.
// This should be called after loading the config file to apply user settings.
func Configure(project, codeUser string, codeUID int) {
	IncusProject = project
	CodeUser = codeUser
	CodeUID = codeUID
}

// execIncusCommand creates an exec.Cmd for running an incus command string
// via "sh -c". The user must be in the incus-admin group in their current
// session (log out / log back in after usermod -aG).
func execIncusCommand(incusCmd string) *exec.Cmd {
	return exec.Command("sh", "-c", incusCmd)
}

// execIncusCommandContext creates a context-aware exec.Cmd for running an
// incus command string via "sh -c".
//
// WaitDelay is set so that when the context is cancelled, cmd.Wait returns
// promptly instead of blocking until all child-process pipes are closed.
func execIncusCommandContext(ctx context.Context, incusCmd string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", incusCmd)
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

// IncusExec executes an Incus command
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

// ImportImage imports a container image into Incus from a metadata tarball and
// a rootfs squashfs file, assigning the given alias.
// Equivalent to: incus --project <project> image import <lxdTar> <squashfs> --alias <alias>
func ImportImage(lxdTar, squashfs, alias string) error {
	cmdStr := buildIncusCommand("image", "import", lxdTar, squashfs, "--alias", alias)
	cmd := execIncusCommand(cmdStr)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("incus image import failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IncusOutputContext executes an Incus command with context support and returns the output (trimmed)
func IncusOutputContext(ctx context.Context, args ...string) (string, error) {
	cmdArgs := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, cmdArgs)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
				Stderr:   strings.TrimSpace(stderr.String()),
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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
				Stderr:   strings.TrimSpace(stderr.String()),
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
	incusCmd := buildIncusCommand(args...)
	cmd := execIncusCommandContext(ctx, incusCmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, &ExitError{
				ExitCode: exitErr.ExitCode(),
				Err:      err,
				Stderr:   strings.TrimSpace(stderr.String()),
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

// StartWithIsolationFallback starts a non-ephemeral container that has
// security.idmap.isolated set, with automatic fallback if the host doesn't
// support it. Intended for non-ephemeral containers (setup.go path) — stopped
// containers are not deleted, so a simple unset+retry is sufficient.
//
// On some hosts (nested containers, CI runners), forkstart exits non-zero even
// though the LXC process started successfully. We poll ContainerRunning for up
// to 5 s before deciding the container failed to start.
func StartWithIsolationFallback(containerName string) error {
	if err := IncusExec("start", containerName); err == nil {
		return nil
	}
	// Poll: maybe it came up despite the forkstart error (soft error on some hosts).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if running, _ := ContainerRunning(containerName); running {
			return nil
		}
	}
	// Container didn't come up; unset isolation and retry.
	fmt.Fprintf(os.Stderr, "Warning: UID namespace isolation not available in this environment, disabling and retrying\n")
	_ = IncusExecQuiet("config", "unset", containerName, "security.idmap.isolated")
	if retryErr := IncusExec("start", containerName); retryErr != nil {
		if running, _ := ContainerRunning(containerName); running {
			return nil
		}
		return retryErr
	}
	return nil
}

func startWithIsolationFallback(containerName string) error {
	return StartWithIsolationFallback(containerName)
}

// LaunchContainer launches an ephemeral container on the given storage pool.
// An empty pool means "use Incus's default pool".
// Uses init+configure+start (not launch) so security flags are set before first boot.
func LaunchContainer(imageAlias, containerName, pool string) error {
	if err := initAndConfigureContainer(imageAlias, containerName, pool, true); err != nil {
		return err
	}
	// Non-fatal: unset and retry at start time if the environment lacks subuid space.
	_ = IsolateUIDNamespace(containerName)
	if err := IncusExec("start", containerName); err == nil {
		return nil
	}
	// Start failed. Poll briefly to distinguish a soft forkstart error (container
	// comes up anyway) from Incus async cleanup (ephemeral container gets deleted).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if running, _ := ContainerRunning(containerName); running {
			return nil // soft forkstart error; container is up
		}
		// Check for deletion: stopped ephemeral containers are deleted by Incus.
		out, err := IncusOutput("list", "^"+containerName+"$", "--format=csv", "--columns=n")
		if err == nil && strings.TrimSpace(out) == "" {
			// Recreate without isolation and start fresh.
			fmt.Fprintf(os.Stderr, "Warning: UID namespace isolation not supported in this environment\n")
			return initConfigureAndStart(imageAlias, containerName, pool, true)
		}
	}
	// Container exists but didn't start; unset isolation and retry.
	fmt.Fprintf(os.Stderr, "Warning: UID namespace isolation not supported in this environment, retrying\n")
	_ = IncusExecQuiet("config", "unset", containerName, "security.idmap.isolated")
	if retryErr := IncusExec("start", containerName); retryErr != nil {
		if running, _ := ContainerRunning(containerName); running {
			return nil
		}
		return retryErr
	}
	return nil
}

// LaunchContainerPersistent launches a non-ephemeral container on the given
// storage pool. An empty pool means "use Incus's default pool".
// Uses init+configure+start (not launch) so security flags are set before first boot.
func LaunchContainerPersistent(imageAlias, containerName, pool string) error {
	if err := initAndConfigureContainer(imageAlias, containerName, pool, false); err != nil {
		return err
	}
	// Non-fatal: unset and retry at start time if the environment lacks subuid space.
	_ = IsolateUIDNamespace(containerName)
	return startWithIsolationFallback(containerName)
}

// initAndConfigureContainer creates a container and applies the required config flags.
func initAndConfigureContainer(imageAlias, containerName, pool string, ephemeral bool) error {
	args := []string{"init", imageAlias, containerName}
	if ephemeral {
		args = append(args, "--ephemeral")
	}
	if pool != "" {
		args = append(args, "-s", pool)
	}
	if err := IncusExec(args...); err != nil {
		return err
	}
	if err := EnableDockerSupport(containerName); err != nil {
		return err
	}
	if err := DisableGuestAPI(containerName); err != nil {
		return err
	}
	return CheckNotPrivileged(containerName)
}

// initConfigureAndStart creates, configures, and starts a container without
// security.idmap.isolated — used as the isolation-unsupported fallback path.
func initConfigureAndStart(imageAlias, containerName, pool string, ephemeral bool) error {
	if err := initAndConfigureContainer(imageAlias, containerName, pool, ephemeral); err != nil {
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

// DisableGuestAPI prevents the Incus guest API (/dev/incus) from being
// accessible inside the container. The guest API exposes host source paths
// in the device topology, leaking the host username and workspace layout
// (see FLAWS.md Finding 3). COI communicates with containers via the host
// admin socket and does not need the guest API.
func DisableGuestAPI(containerName string) error {
	return IncusExec("config", "set", containerName, "security.guestapi=false")
}

// IsolateUIDNamespace enables UID/GID namespace isolation for the container.
// When multiple containers run simultaneously, each gets a unique, non-overlapping
// slice of the host UID/GID space. Without this flag all unprivileged containers
// share the same host-side UID range (typically 100000–165535), so a file
// written by one container as host UID 100000 is readable by another container
// whose UID 0 maps to the same host UID. This must be set before the container
// starts — changing it on a running container has no effect.
func IsolateUIDNamespace(containerName string) error {
	return IncusExec("config", "set", containerName, "security.idmap.isolated=true")
}

// EnableNICSecurity hardens the container's primary bridge NIC (eth0) against
// network-isolation bypass. It MUST be applied before the container's first boot
// — Incus wires the bridge-port filters when the NIC is brought up.
//
// COI's egress allowlist is enforced by host nftables rules that match the
// container's source IP (`ip saddr <containerIP>`) in a chain whose default
// policy is accept. Without anti-spoofing, in-container root (which holds
// CAP_NET_ADMIN over its own netns) can add a second source IP or spoof its
// MAC so packets miss every saddr-keyed rule and fall through to the accept
// policy. This sets, on eth0:
//
//   - security.ipv4_filtering=true : the bridge drops any packet whose source IP
//     is not the container's DHCP-leased address (defeats source-IP spoofing).
//   - security.mac_filtering=true  : same for the source MAC (implied by
//     ipv4_filtering but set explicitly for clarity/robustness).
//   - security.port_isolation=true : the bridge blocks this port from talking to
//     OTHER ports on the same bridge (sibling COI containers), preventing L2
//     lateral movement that the routed-path nft rules never observe.
//
// eth0 is inherited from the default profile, so it is first overridden onto the
// instance (Incus refuses `config device set` on a profile-supplied device).
// Returns an error if any key cannot be set; callers treat it as non-fatal so
// unmanaged/static/macvlan NICs degrade to nft-only enforcement.
func EnableNICSecurity(containerName string) error {
	const nic = "eth0"
	// Materialize the profile NIC at the instance level so device keys can be
	// set on it. Ignored on failure: a re-run (e.g. persistent container that
	// was already overridden) reports "already exists", and the explicit
	// `config device set` calls below surface any real problem.
	_ = IncusExecQuiet("config", "device", "override", containerName, nic)
	for _, kv := range []string{
		"security.ipv4_filtering=true",
		"security.mac_filtering=true",
		"security.port_isolation=true",
	} {
		if err := IncusExec("config", "device", "set", containerName, nic, kv); err != nil {
			return fmt.Errorf("failed to set %s on %s: %w", kv, nic, err)
		}
	}
	return nil
}

// DisableIPv6AtBoot disables IPv6 inside the container from the kernel's first
// instant by setting the disable_ipv6 sysctls as Incus config before boot. This
// closes the IPv6 egress window that otherwise exists between container start and
// the host-side IPv6 nft drop being installed by the network manager.
//
// It is defence-in-depth only: in-container root can re-enable IPv6 at runtime,
// which is why the host-side ip6 drop (network.ApplyIPv6BlockForContainer) is the
// actual enforced boundary. Must be set before first boot.
func DisableIPv6AtBoot(containerName string) error {
	if err := IncusExec("config", "set", containerName,
		"linux.sysctl.net.ipv6.conf.all.disable_ipv6=1"); err != nil {
		return err
	}
	return IncusExec("config", "set", containerName,
		"linux.sysctl.net.ipv6.conf.default.disable_ipv6=1")
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

// buildIncusCommand builds the full incus command string with project flag.
func buildIncusCommand(args ...string) string {
	incusArgs := append([]string{"--project", IncusProject}, args...)

	// Properly quote arguments for shell execution
	quotedArgs := make([]string, len(incusArgs))
	for i, arg := range incusArgs {
		quotedArgs[i] = shellQuote(arg)
	}

	return "incus " + strings.Join(quotedArgs, " ")
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

// ConfigSet sets a configuration key on a container.
func ConfigSet(ctx context.Context, containerName, key, value string) error {
	return IncusExecContext(ctx, "config", "set", containerName, key+"="+value)
}

// ConfigUnset removes a configuration key from a container.
// Returns the combined stdout+stderr output (useful for error inspection) and any error.
func ConfigUnset(ctx context.Context, containerName, key string) (string, error) {
	return IncusOutputWithStderrContext(ctx, "config", "unset", containerName, key)
}

// ConfigShow returns the container's YAML configuration.
// If expanded is true, profile-inherited devices and config are included.
func ConfigShow(ctx context.Context, containerName string, expanded bool) (string, error) {
	args := []string{"config", "show"}
	if expanded {
		args = append(args, "--expanded")
	}
	args = append(args, containerName)
	return IncusOutputContext(ctx, args...)
}

// DeviceAdd adds a device to a container.
// Returns the combined stdout+stderr output and a nil error.
// If the device already exists, a nil error is returned (idempotent).
func DeviceAdd(ctx context.Context, containerName string, deviceArgs ...string) (string, error) {
	args := append([]string{"config", "device", "add", containerName}, deviceArgs...)
	out, err := IncusOutputWithStderrContext(ctx, args...)
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) && strings.Contains(strings.ToLower(out), "already") {
			return out, nil
		}
	}
	return out, err
}

// DeviceSet sets a device-level configuration key on a container.
// Returns the combined stdout+stderr output (useful for error inspection) and any error.
func DeviceSet(ctx context.Context, containerName, device, keyValue string) (string, error) {
	return IncusOutputWithStderrContext(ctx, "config", "device", "set", containerName, device, keyValue)
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
