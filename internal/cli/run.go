package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/spf13/cobra"
)

var (
	capture bool
	timeout int
	format  string
)

var runCmd = &cobra.Command{
	Use:   "run COMMAND",
	Short: "Run a command in an ephemeral container",
	Long: `Execute a command in an ephemeral Incus container.

The container is automatically cleaned up after the command completes.

Examples:
  coi run "echo hello"
  coi run "npm test" --capture
  coi run "pytest" --slot 2
  coi run --workspace ~/project "make build"
`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCommand,
}

func init() {
	runCmd.Flags().BoolVar(&capture, "capture", false, "Capture output instead of streaming")
	runCmd.Flags().IntVar(&timeout, "timeout", 120, "Command timeout in seconds")
	runCmd.Flags().StringVar(&format, "format", "pretty", "Output format (pretty|json)")
}

func runCommand(cmd *cobra.Command, args []string) error {
	// Get absolute workspace path
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	// Check if Incus is available
	if !container.Available() {
		return fmt.Errorf("incus is not available - please install Incus and ensure you're in the incus-admin group")
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
	slotNum := slot
	if slotNum == 0 {
		slotNum, err = session.AllocateSlot(absWorkspace, 10)
		if err != nil {
			return fmt.Errorf("failed to allocate slot: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Auto-allocated slot %d\n", slotNum)
	}

	// Generate container name
	containerName := session.ContainerName(absWorkspace, slotNum)

	// Determine image (use custom if specified, otherwise default)
	img := imageName
	if img == "" {
		img = "coi"
	}

	// Check if image exists
	exists, err := container.ImageExists(img)
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}
	if !exists {
		return fmt.Errorf("image '%s' not found - run 'coi build %s' first", img, img)
	}

	fmt.Fprintf(os.Stderr, "Launching container %s from image %s...\n", containerName, img)

	// Create manager
	mgr := container.NewManager(containerName)

	// Check if persistent container already exists
	containerExists, err := mgr.Exists()
	if err != nil {
		return fmt.Errorf("failed to check if container exists: %w", err)
	}

	if containerExists && persistent {
		// Restart existing persistent container
		fmt.Fprintf(os.Stderr, "Restarting existing persistent container...\n")
		if err := mgr.Start(); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	} else if containerExists {
		// Ephemeral container with same name exists - delete and recreate
		fmt.Fprintf(os.Stderr, "Removing existing container...\n")
		if err := mgr.Delete(true); err != nil {
			return fmt.Errorf("failed to delete existing container: %w", err)
		}
		// Launch new container
		ephemeral := !persistent
		if err := mgr.Launch(img, ephemeral); err != nil {
			return fmt.Errorf("failed to launch container: %w", err)
		}
	} else {
		// Launch new container
		ephemeral := !persistent
		if err := mgr.Launch(img, ephemeral); err != nil {
			return fmt.Errorf("failed to launch container: %w", err)
		}
	}

	// Cleanup container on exit (only if ephemeral)
	defer func() {
		if !persistent {
			fmt.Fprintf(os.Stderr, "Cleaning up container %s...\n", containerName)
			_ = mgr.Delete(true) // Best effort cleanup
		} else {
			// Only stop if container is running (avoids spurious error messages)
			if running, _ := mgr.Running(); running {
				fmt.Fprintf(os.Stderr, "Stopping persistent container %s...\n", containerName)
				_ = mgr.Stop(false) // Best effort stop
			}
		}
	}()

	// Apply resource limits (only for new containers, not restarted persistent ones)
	wasRestarted := containerExists && persistent
	if !wasRestarted {
		limitsConfig := mergeLimitsConfig(cmd)
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
				Project: cfg.Incus.Project,
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

	// Remap container user UID/GID if configured UID differs from image default (1000)
	if err := remapContainerUserIfNeeded(mgr, img, wasRestarted); err != nil {
		return err
	}

	// Determine container workspace path (respects preserve_workspace_path config)
	containerWorkspacePath := "/workspace"
	if cfg.Paths.PreserveWorkspacePath {
		// Validate that the path doesn't conflict with critical system directories
		cleanPath := filepath.Clean(absWorkspace)
		disallowedPrefixes := []string{
			"/etc", "/bin", "/sbin", "/usr", "/root", "/boot", "/sys", "/proc", "/dev", "/lib", "/lib64",
		}
		isDisallowed := false
		for _, prefix := range disallowedPrefixes {
			if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/") {
				isDisallowed = true
				break
			}
		}
		if isDisallowed {
			fmt.Fprintf(os.Stderr, "Warning: preserve_workspace_path requested for %q conflicts with system directories; using /workspace instead\n", absWorkspace)
		} else {
			containerWorkspacePath = cleanPath
		}
	}

	// Mount workspace (skip if restarting existing persistent container)
	useShift := !cfg.Incus.DisableShift
	if !wasRestarted {
		if containerWorkspacePath == absWorkspace {
			fmt.Fprintf(os.Stderr, "Mounting workspace %s -> %s (preserving host path)...\n", absWorkspace, containerWorkspacePath)
		} else {
			fmt.Fprintf(os.Stderr, "Mounting workspace %s -> %s...\n", absWorkspace, containerWorkspacePath)
		}
		if err := mgr.MountDisk("workspace", absWorkspace, containerWorkspacePath, useShift, false); err != nil {
			return fmt.Errorf("failed to mount workspace: %w", err)
		}

		// Parse and validate mount configuration
		mountConfig, err := ParseMountConfig(cfg, mountPairs)
		if err != nil {
			return fmt.Errorf("invalid mount configuration: %w", err)
		}

		// Validate no nested mounts
		if err := session.ValidateMounts(mountConfig); err != nil {
			return fmt.Errorf("mount validation failed: %w", err)
		}

		// Mount all configured directories
		if mountConfig != nil && len(mountConfig.Mounts) > 0 {
			for _, mount := range mountConfig.Mounts {
				// Create host directory if it doesn't exist
				if err := os.MkdirAll(mount.HostPath, 0o755); err != nil {
					return fmt.Errorf("failed to create mount directory '%s': %w", mount.HostPath, err)
				}

				fmt.Fprintf(os.Stderr, "Adding mount: %s -> %s\n", mount.HostPath, mount.ContainerPath)

				if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, useShift, false); err != nil {
					return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
				}
			}
		}

		// Protect security-sensitive paths by mounting read-only (security feature)
		if !writableGitHooks && !cfg.Security.DisableProtection {
			protectedPaths := cfg.Security.GetEffectiveProtectedPaths()
			if len(protectedPaths) > 0 {
				if err := session.SetupSecurityMounts(mgr, absWorkspace, containerWorkspacePath, protectedPaths, useShift); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to setup security mounts: %v\n", err)
				} else {
					// Log which paths were actually protected
					actualPaths := session.GetProtectedPathsForLogging(absWorkspace, protectedPaths)
					if len(actualPaths) > 0 {
						fmt.Fprintf(os.Stderr, "Protected paths (mounted read-only): %s\n", strings.Join(actualPaths, ", "))
					}
				}
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "Reusing existing workspace mount...\n")
		// For restarted containers, get the workspace path from container config
		containerWorkspacePath = mgr.GetWorkspacePath()
	}

	// Configure timezone in container filesystem
	tz := applyContainerTimezone(cmd, mgr)

	// Execute command directly (args are already the full command to run)
	fmt.Fprintf(os.Stderr, "Executing: %s\n", strings.Join(args, " "))

	// Build incus exec command directly with proper args
	incusArgs := []string{
		"exec", containerName, "--user", fmt.Sprintf("%d", container.CodeUID),
		"--group", fmt.Sprintf("%d", container.CodeUID), "--cwd", containerWorkspacePath,
	}

	// Add all environment variables (timezone, config, forward_env, --env flags)
	incusArgs = appendEnvArgs(incusArgs, tz)

	incusArgs = append(incusArgs, "--")
	incusArgs = append(incusArgs, args...)

	// Execute and capture output and exit code
	output, err := container.IncusOutputWithArgs(incusArgs...)

	// Print output to stdout (not stderr) so it can be captured
	if output != "" {
		fmt.Print(output)
	}

	// Handle exit codes: if command ran but failed, exit with same code
	if err != nil {
		// Try to extract exit code from error message
		if exitErr, ok := err.(*container.ExitError); ok {
			fmt.Fprintf(os.Stderr, "\nCommand exited with code %d\n", exitErr.ExitCode)
			os.Exit(exitErr.ExitCode)
		}
		// If we can't extract exit code, return error normally
		return fmt.Errorf("command failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nCommand completed successfully\n")
	return nil
}

// waitForContainer waits for container to be ready
func waitForContainer(mgr *container.Manager, maxRetries int) error {
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

// remapContainerUserIfNeeded remaps the container's 'code' user UID/GID from the
// image default (1000) to the configured container.CodeUID. Only runs for fresh
// COI containers when the configured UID differs from the image default.
func remapContainerUserIfNeeded(mgr *container.Manager, img string, wasRestarted bool) error {
	if wasRestarted || img != "coi" || container.CodeUID == 1000 {
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

// appendEnvArgs appends --env flags for config environment, forward_env, and --env CLI flags
// to an incus exec args slice. Config env is lowest priority, --env flags are highest.
// tz is the resolved timezone name (may be empty).
func appendEnvArgs(incusArgs []string, tz string) []string {
	// Timezone (lowest priority — user can override with --env TZ=...)
	if tz != "" {
		incusArgs = append(incusArgs, "--env", fmt.Sprintf("TZ=%s", tz))
	}

	// Static environment from config (defaults.environment + profile environment)
	for k, v := range cfg.Defaults.Environment {
		incusArgs = append(incusArgs, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Resolve forward_env: merge config + --forward-env flag, deduplicate, look up host values
	forwardNames := config.MergeStringSliceUnique(cfg.Defaults.ForwardEnv, forwardEnvVars)
	for _, name := range forwardNames {
		if val, ok := os.LookupEnv(name); ok {
			incusArgs = append(incusArgs, "--env", fmt.Sprintf("%s=%s", name, val))
		} else {
			fmt.Fprintf(os.Stderr, "Warning: forward_env variable %q is not set on host, skipping\n", name)
		}
	}

	// User-provided --env flags (highest priority — last wins)
	for _, e := range envVars {
		incusArgs = append(incusArgs, "--env", e)
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

// applyContainerTimezone resolves the timezone and configures it inside the container.
// Returns the resolved timezone name (empty for UTC).
func applyContainerTimezone(cmd *cobra.Command, mgr *container.Manager) string {
	tz := resolveTimezone(cmd, cfg)
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
