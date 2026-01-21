package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/spf13/cobra"
)

// containerCmd is the parent command for all container operations
var containerCmd = &cobra.Command{
	Use:   "container",
	Short: "Manage Incus containers",
	Long:  `Low-level container operations for launching, stopping, executing commands, and managing containers.`,
}

// containerLaunchCmd launches a new container from an image
var containerLaunchCmd = &cobra.Command{
	Use:   "launch <image> <name>",
	Short: "Launch a new container from an image",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		image := args[0]
		name := args[1]

		ephemeral, _ := cmd.Flags().GetBool("ephemeral")

		mgr := container.NewManager(name)
		if err := mgr.Launch(image, ephemeral); err != nil {
			return exitError(1, fmt.Sprintf("failed to launch container: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Container %s launched from %s\n", name, image)
		return nil
	},
}

// containerStartCmd starts a stopped container
var containerStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start a stopped container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		mgr := container.NewManager(name)
		if err := mgr.Start(); err != nil {
			return exitError(1, fmt.Sprintf("failed to start container: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Container %s started\n", name)
		return nil
	},
}

// containerStopCmd stops a running container
var containerStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop a running container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		force, _ := cmd.Flags().GetBool("force")

		mgr := container.NewManager(name)
		if err := mgr.Stop(force); err != nil {
			return exitError(1, fmt.Sprintf("failed to stop container: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Container %s stopped\n", name)
		return nil
	},
}

// containerDeleteCmd deletes a container
var containerDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		force, _ := cmd.Flags().GetBool("force")

		mgr := container.NewManager(name)
		if err := mgr.Delete(force); err != nil {
			return exitError(1, fmt.Sprintf("failed to delete container: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Container %s deleted\n", name)
		return nil
	},
}

// containerExecCmd executes a command in a container
var containerExecCmd = &cobra.Command{
	Use:   "exec <name> -- <command>",
	Short: "Execute a command in a container",
	Long: `Execute a command inside a container with full context control.

Examples:
  # Run as root
  coi container exec my-container -- ls -la /

  # Run as specific user with env vars
  coi container exec my-container --user 1000 --env FOO=bar --cwd /workspace -- npm test

  # Capture output as JSON
  coi container exec my-container --capture -- echo "hello world"`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		containerName := args[0]
		commandArgs := args[1:] // Keep as separate arguments

		if len(commandArgs) == 0 {
			return exitError(2, "no command specified (use -- before command)")
		}

		capture, _ := cmd.Flags().GetBool("capture")
		format, _ := cmd.Flags().GetString("format")
		mgr := container.NewManager(containerName)

		// Validate that --format requires --capture
		if cmd.Flags().Changed("format") && !capture {
			return exitError(2, "--format flag requires --capture flag")
		}

		// Validate format value
		if format != "json" && format != "raw" {
			return exitError(2, fmt.Sprintf("invalid format '%s': must be 'json' or 'raw'", format))
		}

		if capture {
			// For capture mode, use ExecArgsCapture (no bash -c wrapping, preserves whitespace)
			// Parse flags
			userFlag, _ := cmd.Flags().GetInt("user")
			groupFlag, _ := cmd.Flags().GetInt("group")
			envVars, _ := cmd.Flags().GetStringArray("env")
			cwd, _ := cmd.Flags().GetString("cwd")

			// Parse env vars
			env := make(map[string]string)
			for _, e := range envVars {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					env[parts[0]] = parts[1]
				}
			}

			opts := container.ExecCommandOptions{
				Cwd: cwd,
				Env: env,
			}

			if cmd.Flags().Changed("user") {
				opts.User = &userFlag
			}
			if cmd.Flags().Changed("group") {
				opts.Group = &groupFlag
			}

			output, err := mgr.ExecArgsCapture(commandArgs, opts)

			// Handle raw format - output stdout and exit with proper code
			if format == "raw" {
				fmt.Print(output) // No newline, preserve exact output
				if err != nil {
					// Extract actual exit code if available, otherwise use 1
					exitCode := 1
					if exitErr, ok := err.(*container.ExitError); ok {
						exitCode = exitErr.ExitCode
					}
					os.Exit(exitCode)
				}
				return nil
			}

			// Handle JSON format (default)
			exitCode := 0
			stderr := ""
			if err != nil {
				// Extract actual exit code if available, otherwise use 1
				exitCode = 1
				if exitErr, ok := err.(*container.ExitError); ok {
					exitCode = exitErr.ExitCode
				}
				stderr = err.Error()
			}

			result := map[string]interface{}{
				"stdout":    output,
				"stderr":    stderr,
				"exit_code": exitCode,
			}
			jsonOutput, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(jsonOutput))
			return nil
		}

		// For non-capture mode, use ExecArgs with options
		userFlag, _ := cmd.Flags().GetInt("user")
		groupFlag, _ := cmd.Flags().GetInt("group")
		envVars, _ := cmd.Flags().GetStringArray("env")
		cwd, _ := cmd.Flags().GetString("cwd")

		// Parse env vars
		env := make(map[string]string)
		for _, e := range envVars {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				env[parts[0]] = parts[1]
			}
		}

		opts := container.ExecCommandOptions{
			Cwd: cwd,
			Env: env,
		}

		if cmd.Flags().Changed("user") {
			opts.User = &userFlag
		}
		if cmd.Flags().Changed("group") {
			opts.Group = &groupFlag
		}

		err := mgr.ExecArgs(commandArgs, opts)
		if err != nil {
			return exitError(1, fmt.Sprintf("command failed: %v", err))
		}

		return nil
	},
}

// containerExistsCmd checks if a container exists
var containerExistsCmd = &cobra.Command{
	Use:   "exists <name>",
	Short: "Check if a container exists",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		mgr := container.NewManager(name)
		exists, err := mgr.Exists()
		if err != nil {
			return exitError(1, fmt.Sprintf("failed to check container: %v", err))
		}

		if !exists {
			return exitError(1, "")
		}

		return nil
	},
}

// containerRunningCmd checks if a container is running
var containerRunningCmd = &cobra.Command{
	Use:   "running <name>",
	Short: "Check if a container is running",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		mgr := container.NewManager(name)
		running, err := mgr.Running()
		if err != nil {
			return exitError(1, fmt.Sprintf("failed to check container: %v", err))
		}

		if !running {
			return exitError(1, "")
		}

		return nil
	},
}

// containerMountCmd mounts a disk to a container
var containerMountCmd = &cobra.Command{
	Use:   "mount <name> <device-name> <source> <path>",
	Short: "Add a disk device to a container",
	Long: `Mount a host directory into a container.

Example:
  coi container mount my-container workspace /home/user/project /workspace --shift`,
	Args: cobra.ExactArgs(4),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		deviceName := args[1]
		source := args[2]
		path := args[3]

		shift, _ := cmd.Flags().GetBool("shift")

		mgr := container.NewManager(name)
		if err := mgr.MountDisk(deviceName, source, path, shift); err != nil {
			return exitError(1, fmt.Sprintf("failed to mount disk: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Disk mounted: %s -> %s:%s\n", source, name, path)
		return nil
	},
}

// exitError returns an error with a specific exit code
func exitError(code int, message string) error {
	if message != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	}
	os.Exit(code)
	return nil // Never reached, but needed for type
}

func init() {
	// Add flags to launch command
	containerLaunchCmd.Flags().Bool("ephemeral", false, "Create ephemeral container")

	// Add flags to stop command
	containerStopCmd.Flags().Bool("force", false, "Force stop")

	// Add flags to delete command
	containerDeleteCmd.Flags().Bool("force", false, "Force delete even if running")

	// Add flags to exec command
	containerExecCmd.Flags().Int("user", 0, "User ID to run as")
	containerExecCmd.Flags().Int("group", 0, "Group ID to run as")
	containerExecCmd.Flags().StringArray("env", []string{}, "Environment variable (KEY=VALUE)")
	containerExecCmd.Flags().String("cwd", "/workspace", "Working directory")
	containerExecCmd.Flags().Bool("capture", false, "Capture output as JSON")
	containerExecCmd.Flags().String("format", "json", "Output format when using --capture: json or raw")

	// Add flags to mount command
	containerMountCmd.Flags().Bool("shift", true, "Enable UID/GID shifting")

	// Add subcommands to container command
	containerCmd.AddCommand(containerLaunchCmd)
	containerCmd.AddCommand(containerStartCmd)
	containerCmd.AddCommand(containerStopCmd)
	containerCmd.AddCommand(containerDeleteCmd)
	containerCmd.AddCommand(containerExecCmd)
	containerCmd.AddCommand(containerExistsCmd)
	containerCmd.AddCommand(containerRunningCmd)
	containerCmd.AddCommand(containerMountCmd)
}
