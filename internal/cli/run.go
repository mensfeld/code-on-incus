package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/alias"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <command> [args...]",
	Short: "Run a command in an ephemeral container",
	Long: `Execute a command in an ephemeral Incus container.

The container is automatically cleaned up after the command completes.

Examples:
  coi run "echo hello"
  coi run "npm test" --slot 2
  coi run --workspace ~/project "make build"
`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCommand,
}

func runCommand(cmd *cobra.Command, args []string) error {
	// Get absolute workspace path
	absWorkspace, err := filepath.Abs(app.workspace)
	if err != nil {
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	// Load workspace-local .coi/config.toml when the workspace differs from the
	// process CWD. config.Load() already loads <CWD>/.coi/config.toml via
	// GetConfigPaths, so we only need to overlay when the user pointed --workspace
	// at a different directory to avoid loading the same file twice.
	if err := overlayWorkspaceConfig(absWorkspace); err != nil {
		return err
	}

	// Validate max_duration early so the user gets a clear error before any container work.
	if app.cfg.Limits.Runtime.MaxDuration != "" {
		if _, err := limits.ParseDuration(app.cfg.Limits.Runtime.MaxDuration); err != nil {
			return fmt.Errorf("invalid max_duration: %w", err)
		}
	}

	// Check if Incus is available
	if !container.Available() {
		return container.IncusNotAvailableError()
	}

	// Check minimum Incus version
	if err := container.CheckMinimumVersion(); err != nil {
		return err
	}

	// Warn if kernel is too old (non-blocking)
	if warning := container.CheckKernelVersion(); warning != "" {
		fmt.Fprintf(os.Stderr, "%s\n", warning)
	}

	// Allocate slot if not specified
	slotNum := app.slot
	if slotNum == 0 {
		slotNum, err = session.AllocateSlot(absWorkspace, 10)
		if err != nil {
			return fmt.Errorf("failed to allocate slot: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Auto-allocated slot %d\n", slotNum)
	}

	// Generate container name
	containerName := session.ContainerName(absWorkspace, slotNum)

	// Determine image: CLI --image flag > config defaults.image > "coi-default"
	img := ResolveImageName(app.imageName, app.cfg)

	// Check if image exists, auto-build from config if possible
	if err := AutoBuildIfNeeded(app.cfg, img); err != nil {
		return err
	}
	if err := CheckAndReportStaleBase(app.cfg, img); err != nil {
		return err
	}

	// Validate the storage pool exists before doing any container work so the
	// user gets an actionable "create with `incus storage create`" error
	// instead of a cryptic Incus failure midway through launch.
	if err := container.ValidateStoragePool(app.cfg.Container.StoragePool); err != nil {
		return err
	}

	// Validate and prepare alias if configured
	effectiveAlias, err := validateAndPrepareAlias(app.cfg.Container.Alias)
	if err != nil {
		return err
	}

	ensureBridgeTrustedZone()

	fmt.Fprintf(os.Stderr, "Launching container %s from image %s...\n", containerName, img)

	// Create manager
	mgr := container.NewManager(containerName)

	// Check if persistent container already exists
	containerExists, err := mgr.Exists()
	if err != nil {
		return fmt.Errorf("failed to check if container exists: %w", err)
	}

	if err := launchOrReuseContainer(mgr, img, app.cfg.Container.StoragePool, containerExists, app.persistent); err != nil {
		return err
	}

	// Set alias metadata on container and register in global registry
	if err := applyContainerAlias(effectiveAlias, containerName, absWorkspace); err != nil {
		return err
	}

	// networkMgr is assigned by applyNetworkIsolation below. Declared here so
	// the teardown closures can capture it by reference.
	var networkMgr *network.Manager

	// Teardowns run LIFO. Register in reverse of desired execution order so
	// the actual sequence is: immutable → network → container.
	pipeline := &session.Pipeline{}
	defer pipeline.Teardown()

	// 1. Container (registered first → executes last in LIFO)
	pipeline.AddTeardown(func() {
		if !app.persistent {
			fmt.Fprintf(os.Stderr, "Cleaning up container %s...\n", containerName)
			_ = mgr.Delete(true)
		} else {
			if running, _ := mgr.Running(); running {
				fmt.Fprintf(os.Stderr, "Stopping persistent container %s...\n", containerName)
				_ = mgr.Stop(false)
			}
		}
	})

	// 2. Network (registered second → executes second; runs before container
	// deletion so firewall rules are removed while the IP is still resolvable)
	pipeline.AddTeardown(func() {
		if networkMgr == nil {
			return
		}
		if err := networkMgr.Teardown(context.Background(), containerName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to teardown network isolation: %v\n", err)
		}
	})

	// 3. Immutable (registered last → executes first; clears host-side bits
	// before any container or network operation so protected files are writable)
	pipeline.AddTeardown(func() {
		logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
		session.RemoveImmutable(containerName, logFn)
	})

	// Apply resource limits (only for new containers, not restarted persistent ones)
	wasRestarted := containerExists && app.persistent
	if !wasRestarted {
		limitsConfig := &app.cfg.Limits
		if limitsConfig != nil && hasAnyLimits(limitsConfig) {
			fmt.Fprintf(os.Stderr, "Applying resource limits...\n")
			applyOpts := limits.ApplyOptions{
				ContainerName: containerName,
				CPU: limits.CPULimits{
					Count:     limitsConfig.CPU.Count,
					Allowance: limitsConfig.CPU.Allowance,
					Priority:  limitsConfig.CPU.Priority,
				},
				Memory: limits.MemoryLimits{
					Limit:   limitsConfig.Memory.Limit,
					Enforce: limitsConfig.Memory.Enforce,
					Swap:    limitsConfig.Memory.Swap,
				},
				Disk: limits.DiskLimits{
					Read:     limitsConfig.Disk.Read,
					Write:    limitsConfig.Disk.Write,
					Max:      limitsConfig.Disk.Max,
					Priority: limitsConfig.Disk.Priority,
				},
				Runtime: limits.RuntimeLimits{
					MaxProcesses: limitsConfig.Runtime.MaxProcesses,
				},
				Project: app.cfg.Incus.Project,
			}
			if err := limits.ApplyResourceLimits(applyOpts); err != nil {
				return fmt.Errorf("failed to apply resource limits: %w", err)
			}
		}
	}

	// Wait for container to be ready
	fmt.Fprintf(os.Stderr, "Waiting for container to be ready...\n")
	if err := waitForContainer(mgr, 30); err != nil {
		return err
	}

	// Configure Docker bridge CIDRs for new containers (not restarted persistent ones)
	if !wasRestarted {
		logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
		if err := session.ConfigureDockerDaemon(mgr, logFn); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to configure Docker daemon: %v\n", err)
		}
	}

	// Remap container user UID/GID if configured UID differs from image default (1000)
	if err := remapContainerUserIfNeeded(mgr, wasRestarted); err != nil {
		return err
	}

	// Determine container workspace path (respects preserve_workspace_path config)
	containerWorkspacePath := resolveContainerWorkspacePath(absWorkspace)

	// Mount workspace (skip if restarting existing persistent container)
	useShift := !app.cfg.Incus.DisableShift
	if err := applyWorkspaceMounts(mgr, containerName, absWorkspace, &containerWorkspacePath, useShift, wasRestarted); err != nil {
		return err
	}

	// Forward SSH agent and apply network isolation if configured
	sshAgentSocketPath, err := applySSHAgentForwarding(mgr, containerName)
	if err != nil {
		return err
	}
	networkMgr, err = applyNetworkIsolation(containerName)
	if err != nil {
		return err
	}

	// Configure timezone in container filesystem
	tz := applyContainerTimezone(mgr)

	// Auto-trust mise config files in the workspace
	session.SetupMiseTrust(mgr, containerWorkspacePath, func(msg string) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	})

	// Start timeout monitor if max_duration is configured.
	// The monitor runs in a background goroutine and stops the container when
	// the duration elapses, which causes the blocking incus exec below to return.
	var timeoutMon *limits.TimeoutMonitor
	if app.cfg.Limits.Runtime.MaxDuration != "" {
		maxDur, _ := limits.ParseDuration(app.cfg.Limits.Runtime.MaxDuration) // already validated above
		autoStop := config.BoolVal(app.cfg.Limits.Runtime.AutoStop)
		if app.cfg.Limits.Runtime.AutoStop == nil {
			autoStop = true // default: auto-stop when limit reached
		}
		stopGraceful := config.BoolVal(app.cfg.Limits.Runtime.StopGraceful)
		runLog := logger.NewDiscard()
		timeoutMon = limits.NewTimeoutMonitor(cmd.Context(), containerName, maxDur, autoStop, stopGraceful, app.cfg.Incus.Project, runLog)
		timeoutMon.Start()
		defer timeoutMon.Stop()
	}

	// Execute command directly (args are already the full command to run)
	fmt.Fprintf(os.Stderr, "Executing: %s\n", strings.Join(args, " "))

	// Build incus exec command directly with proper args
	incusArgs := []string{
		"exec", containerName, "--user", fmt.Sprintf("%d", container.CodeUID),
		"--group", fmt.Sprintf("%d", container.CodeUID), "--cwd", containerWorkspacePath,
	}

	// Add all environment variables (timezone, config, forward_env)
	incusArgs = appendEnvArgs(incusArgs, tz, sshAgentSocketPath)

	incusArgs = append(incusArgs, "--")
	incusArgs = append(incusArgs, args...)

	// Execute and capture output and exit code
	output, err := container.IncusOutputWithArgs(incusArgs...)

	// Print output to stdout (not stderr) so it can be captured
	if output != "" {
		fmt.Print(output)
	}

	// Handle exit codes: if command ran but failed, propagate exit code through cobra
	if err != nil {
		if exitErr, ok := err.(*container.ExitError); ok {
			fmt.Fprintf(os.Stderr, "\nCommand exited with code %d\n", exitErr.ExitCode)
			return &ExitCodeError{Code: exitErr.ExitCode}
		}
		return fmt.Errorf("command failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nCommand completed successfully\n")
	return nil
}

// launchOrReuseContainer restarts an existing persistent container, or
// recreates / creates a fresh one on the given storage pool.
func launchOrReuseContainer(mgr container.ContainerManager, img, pool string, containerExists, persistent bool) error {
	if containerExists && persistent {
		fmt.Fprintf(os.Stderr, "Restarting existing persistent container...\n")
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		return nil
	}
	if containerExists {
		fmt.Fprintf(os.Stderr, "Removing existing container...\n")
		if err := mgr.Delete(true); err != nil {
			return fmt.Errorf("failed to delete existing container: %w", err)
		}
	}
	ephemeral := !persistent
	if err := mgr.Launch(img, ephemeral, pool); err != nil {
		return fmt.Errorf("failed to launch container: %w", err)
	}
	return nil
}

// waitForContainer waits for container to be ready
func waitForContainer(mgr container.ContainerManager, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		running, err := mgr.Running()
		if err != nil {
			return err
		}
		if running {
			// Additional check: try to execute a simple command
			_, err := mgr.ExecCommand("echo ready", container.ExecCommandOptions{Capture: true})
			if err == nil {
				return nil
			}
		}
		// Wait before retry
		if i < maxRetries-1 {
			fmt.Fprintf(os.Stderr, ".")
		}
	}
	return fmt.Errorf("container failed to become ready")
}

// ensureBridgeTrustedZone ensures the Incus bridge has iptables FORWARD ACCEPT
// rules before launching a container. Without this, DHCP replies are dropped and
// the container never gets an IP when the FORWARD policy is DROP.
func ensureBridgeTrustedZone() {
	if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not ensure bridge forwarding rules: %v\n", err)
	} else if changed {
		fmt.Fprintf(os.Stderr, "Added iptables FORWARD rules for %s (was missing — containers could not get IPs)\n", bridgeName)
	}
}

// remapContainerUserIfNeeded remaps the container's 'code' user UID/GID from the
// image default (1000) to the configured container.CodeUID. Only runs for fresh
// containers that actually have the code user (probed at runtime, since
// custom images built FROM coi-default also inherit it) when the
// configured UID differs from the image default.
func remapContainerUserIfNeeded(mgr container.ContainerManager, wasRestarted bool) error {
	if wasRestarted || container.CodeUID == 1000 {
		return nil
	}
	hasCodeUser, err := session.DetectCodeUser(mgr, container.CodeUser)
	if err != nil {
		// Surface connectivity / exec failures to stderr so an unexpectedly
		// skipped remap doesn't look like a silent success. We still proceed
		// without remapping — the alternative would be to abort `coi run`
		// on a transient probe failure, which is worse UX.
		fmt.Fprintf(os.Stderr,
			"Warning: could not probe container for %s user, skipping UID/GID remap: %v\n",
			container.CodeUser, err)
		return nil
	}
	if !hasCodeUser {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Remapping user %s from UID 1000 to %d...\n", container.CodeUser, container.CodeUID)
	remapCmd := fmt.Sprintf(
		"groupmod -g %d %s && usermod -u %d -g %d %s && chown -R %s:%s /home/%s",
		container.CodeUID, container.CodeUser,
		container.CodeUID, container.CodeUID, container.CodeUser,
		container.CodeUser, container.CodeUser, container.CodeUser,
	)
	if _, err := mgr.ExecCommand(remapCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to remap user %s to UID %d: %w", container.CodeUser, container.CodeUID, err)
	}
	return nil
}

// appendEnvArgs appends --env flags for config environment and forward_env
// to an incus exec args slice.
// tz is the resolved timezone name (may be empty).
// sshAgentSocketPath is the container-side SSH agent socket (may be empty).
func appendEnvArgs(incusArgs []string, tz, sshAgentSocketPath string) []string {
	// Timezone (lowest priority — user can override with config env)
	if tz != "" {
		incusArgs = append(incusArgs, "--env", fmt.Sprintf("TZ=%s", tz))
	}

	// SSH agent socket
	if sshAgentSocketPath != "" {
		incusArgs = append(incusArgs, "--env", fmt.Sprintf("SSH_AUTH_SOCK=%s", sshAgentSocketPath))
	}

	// Static environment from config (defaults.environment + profile environment)
	for k, v := range app.cfg.Defaults.Environment {
		incusArgs = append(incusArgs, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Resolve forward_env from config, look up host values
	for _, name := range app.cfg.Defaults.ForwardEnv {
		if val, ok := os.LookupEnv(name); ok {
			incusArgs = append(incusArgs, "--env", fmt.Sprintf("%s=%s", name, val))
		} else {
			fmt.Fprintf(os.Stderr, "Warning: forward_env variable %q is not set on host, skipping\n", name)
		}
	}

	return incusArgs
}

// hasAnyLimits checks if any limits are configured (used in run.go)
func hasAnyLimits(cfg *config.LimitsConfig) bool {
	if cfg == nil {
		return false
	}

	// Check if any limit is set (non-empty strings or non-zero integers)
	return cfg.CPU.Count != "" ||
		cfg.CPU.Allowance != "" ||
		cfg.CPU.Priority != 0 ||
		cfg.Memory.Limit != "" ||
		cfg.Memory.Enforce != "" ||
		cfg.Memory.Swap != "" ||
		cfg.Disk.Read != "" ||
		cfg.Disk.Write != "" ||
		cfg.Disk.Max != "" ||
		cfg.Disk.Priority != 0 ||
		cfg.Runtime.MaxProcesses != 0
}

// filterWritableGitHooks removes .git/hooks from protected paths when writable hooks are enabled.
func filterWritableGitHooks(paths []string, cfg *config.Config) []string {
	if !config.BoolVal(cfg.Git.WritableHooks) {
		return paths
	}
	gitHooksSuffix := filepath.Join(".git", "hooks")
	filtered := paths[:0]
	for _, p := range paths {
		if !strings.HasSuffix(p, gitHooksSuffix) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// validateAndPrepareAlias validates the alias from config and returns it.
// Returns ("", nil) if no alias is configured.
func validateAndPrepareAlias(aliasStr string) (string, error) {
	if aliasStr == "" {
		return "", nil
	}
	if err := alias.ValidateAlias(aliasStr); err != nil {
		return "", fmt.Errorf("invalid alias: %w", err)
	}
	return aliasStr, nil
}

// applyContainerAlias sets the user.coi.alias metadata on the container and
// registers the alias in the global registry. No-op if alias is empty.
func applyContainerAlias(effectiveAlias, containerName, absWorkspace string) error {
	if effectiveAlias == "" {
		return nil
	}

	if err := container.IncusExec("config", "set", containerName,
		fmt.Sprintf("user.coi.alias=%s", effectiveAlias)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to set alias metadata: %v\n", err)
	}

	reg, err := alias.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load alias registry: %v\n", err)
		return nil
	}
	if err := reg.Register(effectiveAlias, absWorkspace, ""); err != nil {
		return fmt.Errorf("alias conflict: %w", err)
	}
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save alias registry: %v\n", err)
	}

	return nil
}

// overlayWorkspaceConfig loads the workspace-local .coi/config.toml on top of
// the global config when --workspace points to a directory other than the CWD.
// config.Load() already loads <CWD>/.coi/config.toml, so skipping the overlay
// when they match avoids doubling entries like AdditionalProtectedPaths.
func overlayWorkspaceConfig(absWorkspace string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("failed to resolve working directory: %w", err)
	}
	if absWorkspace == absCWD {
		return nil
	}
	if err := app.cfg.OverlayProjectConfig(absWorkspace); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load project config from %s: %w", absWorkspace, err)
	}
	container.Configure(app.cfg.Incus.Project, app.cfg.Incus.CodeUser, app.cfg.Incus.CodeUID)
	return nil
}

// resolveContainerWorkspacePath returns the path inside the container where the
// workspace should be mounted. When preserve_workspace_path is set it mirrors
// the host path, falling back to /workspace if the path conflicts with system dirs.
func resolveContainerWorkspacePath(absWorkspace string) string {
	if !app.cfg.Paths.PreserveWorkspacePath {
		return "/workspace"
	}
	cleanPath := filepath.Clean(absWorkspace)
	disallowedPrefixes := []string{
		"/etc", "/bin", "/sbin", "/usr", "/root", "/boot", "/sys", "/proc", "/dev", "/lib", "/lib64",
	}
	for _, prefix := range disallowedPrefixes {
		if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/") {
			fmt.Fprintf(os.Stderr, "Warning: preserve_workspace_path requested for %q conflicts with system directories; using /workspace instead\n", absWorkspace)
			return "/workspace"
		}
	}
	return cleanPath
}

// applyWorkspaceMounts mounts the workspace and all configured additional directories
// into the container, then applies security (read-only) mounts. For restarted persistent
// containers it retrieves the existing workspace path from the container config instead.
func applyWorkspaceMounts(mgr container.ContainerManager, containerName, absWorkspace string, containerWorkspacePath *string, useShift, wasRestarted bool) error {
	if wasRestarted {
		fmt.Fprintf(os.Stderr, "Reusing existing workspace mount...\n")
		*containerWorkspacePath = mgr.GetWorkspacePath()
		return nil
	}

	if *containerWorkspacePath == absWorkspace {
		fmt.Fprintf(os.Stderr, "Mounting workspace %s -> %s (preserving host path)...\n", absWorkspace, *containerWorkspacePath)
	} else {
		fmt.Fprintf(os.Stderr, "Mounting workspace %s -> %s...\n", absWorkspace, *containerWorkspacePath)
	}
	if err := mgr.MountDisk("workspace", absWorkspace, *containerWorkspacePath, useShift, false); err != nil {
		return fmt.Errorf("failed to mount workspace: %w", err)
	}

	mountConfig, err := ParseMountConfig(app.cfg)
	if err != nil {
		return fmt.Errorf("invalid mount configuration: %w", err)
	}
	if err := session.ValidateMounts(mountConfig); err != nil {
		return fmt.Errorf("mount validation failed: %w", err)
	}
	if mountConfig != nil {
		for _, mount := range mountConfig.Mounts {
			if err := addMount(mgr, mount, useShift); err != nil {
				return err
			}
		}
	}

	if !app.cfg.Security.DisableProtection {
		if err := applySecurityMounts(mgr, absWorkspace, *containerWorkspacePath, containerName, useShift); err != nil {
			return err
		}
	}
	return nil
}

// addMount adds a single configured directory mount to the container.
func addMount(mgr container.ContainerManager, mount session.MountEntry, useShift bool) error {
	if mount.Readonly {
		if _, err := os.Stat(mount.HostPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Warning: readonly mount source %s does not exist, skipping\n", mount.HostPath)
				return nil
			}
			return fmt.Errorf("failed to stat readonly mount source '%s': %w", mount.HostPath, err)
		}
		fmt.Fprintf(os.Stderr, "Adding mount (read-only): %s -> %s\n", mount.HostPath, mount.ContainerPath)
	} else {
		if err := os.MkdirAll(mount.HostPath, 0o755); err != nil {
			return fmt.Errorf("failed to create mount directory '%s': %w", mount.HostPath, err)
		}
		fmt.Fprintf(os.Stderr, "Adding mount: %s -> %s\n", mount.HostPath, mount.ContainerPath)
	}
	if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, useShift, mount.Readonly); err != nil {
		return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
	}
	return nil
}

// applySecurityMounts sets up read-only protection mounts and optional host immutable flags.
func applySecurityMounts(mgr container.ContainerManager, absWorkspace, containerWorkspacePath, containerName string, useShift bool) error {
	protectedPaths := filterWritableGitHooks(app.cfg.Security.GetEffectiveProtectedPaths(), app.cfg)
	if len(protectedPaths) == 0 {
		return nil
	}
	if err := session.SetupSecurityMounts(mgr, absWorkspace, containerWorkspacePath, protectedPaths, useShift); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to setup security mounts: %v\n", err)
	} else {
		actualPaths := session.GetProtectedPathsForLogging(absWorkspace, protectedPaths)
		if len(actualPaths) > 0 {
			fmt.Fprintf(os.Stderr, "Protected paths (mounted read-only): %s\n", strings.Join(actualPaths, ", "))
		}
	}
	if app.cfg.Security.IsHostImmutableEnabled() {
		logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
		immutablePaths := session.ApplyImmutable(absWorkspace, protectedPaths, containerName, logFn)
		if len(immutablePaths) > 0 {
			fmt.Fprintf(os.Stderr, "Host-side immutable protection applied: %s\n", strings.Join(immutablePaths, ", "))
		}
	}
	return nil
}

// applySSHAgentForwarding forwards the host SSH agent into the container when
// ssh.forward_agent is true in config. Returns the container-side socket path.
func applySSHAgentForwarding(mgr container.ContainerManager, containerName string) (string, error) {
	if !config.BoolVal(app.cfg.SSH.ForwardAgent) {
		return "", nil
	}
	logger := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
	socketPath, err := session.SetupSSHAgentForwarding(mgr, containerName, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: SSH agent forwarding failed: %v\n", err)
		return "", nil
	}
	if socketPath != "" {
		fmt.Fprintf(os.Stderr, "SSH agent forwarding configured: %s\n", socketPath)
	}
	return socketPath, nil
}

// applyNetworkIsolation installs firewall rules for the container when
// network.mode is set to something other than "open" in config.
// Returns the Manager so the caller can Teardown before container deletion.
func applyNetworkIsolation(containerName string) (*network.Manager, error) {
	networkConfig := app.cfg.Network
	if networkConfig.Mode == "" || networkConfig.Mode == config.NetworkModeOpen {
		return nil, nil
	}
	if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not ensure bridge forwarding rules: %v\n", err)
	} else if changed {
		fmt.Fprintf(os.Stderr, "Added iptables FORWARD rules for %s\n", bridgeName)
	}
	nm := network.NewManager(&networkConfig, logger.NewDiscard())
	if err := nm.SetupForContainer(context.Background(), containerName); err != nil {
		return nil, fmt.Errorf("failed to setup network isolation: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Network isolation applied: %s\n", networkConfig.Mode)
	return nm, nil
}

// applyContainerTimezone resolves the timezone and configures it inside the container.
// Returns the resolved timezone name (empty for UTC).
func applyContainerTimezone(mgr container.ContainerManager) string {
	tz := resolveTimezone(app.cfg)
	if tz != "" {
		tzCmd := fmt.Sprintf(
			"ln -sf /usr/share/zoneinfo/%s /etc/localtime && echo %s > /etc/timezone",
			tz, tz,
		)
		if _, err := mgr.ExecCommand(tzCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to set timezone: %v\n", err)
		}
	} else {
		// Explicitly reset to UTC — important for persistent containers that may
		// have had a different timezone applied in a previous session.
		resetCmd := "ln -sf /usr/share/zoneinfo/UTC /etc/localtime && echo UTC > /etc/timezone"
		if _, err := mgr.ExecCommand(resetCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to reset timezone to UTC: %v\n", err)
		}
	}
	return tz
}
