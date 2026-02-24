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

	// Mount workspace (skip if restarting existing persistent container)
	useShift := !cfg.Incus.DisableShift
	if !wasRestarted {
		fmt.Fprintf(os.Stderr, "Mounting workspace %s...\n", absWorkspace)
		if err := mgr.MountDisk("workspace", absWorkspace, "/workspace", useShift, false); err != nil {
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
		// Note: coi run always uses /workspace as the container path
		if !writableGitHooks && !cfg.Security.DisableProtection {
			protectedPaths := cfg.Security.GetEffectiveProtectedPaths()
			if len(protectedPaths) > 0 {
				if err := session.SetupSecurityMounts(mgr, absWorkspace, "/workspace", protectedPaths, useShift); err != nil {
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
	}

	// Execute command directly (args are already the full command to run)
	fmt.Fprintf(os.Stderr, "Executing: %s\n", strings.Join(args, " "))

	// Build incus exec command directly with proper args
	incusArgs := []string{
		"exec", containerName, "--user", fmt.Sprintf("%d", container.CodeUID),
		"--group", fmt.Sprintf("%d", container.CodeUID), "--cwd", "/workspace",
	}

	// Add environment variables from -e flags
	for _, e := range envVars {
		incusArgs = append(incusArgs, "--env", e)
	}

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
