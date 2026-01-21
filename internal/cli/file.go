package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/spf13/cobra"
)

// fileCmd is the parent command for all file operations
var fileCmd = &cobra.Command{
	Use:   "file",
	Short: "Transfer files to/from containers",
	Long:  `Push and pull files and directories between the host and containers.`,
}

// filePushCmd pushes files/directories into a container
var filePushCmd = &cobra.Command{
	Use:   "push <local-path> <container>:<remote-path>",
	Short: "Push a file or directory into a container",
	Long: `Push a file or directory from the host into a container.

Examples:
  # Push file
  coi file push ./config.json my-container:/workspace/config.json

  # Push directory
  coi file push -r ./src my-container:/workspace/src`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		localPath := args[0]
		destination := args[1]

		recursive, _ := cmd.Flags().GetBool("recursive")

		// Parse destination (container:path)
		parts := strings.SplitN(destination, ":", 2)
		if len(parts) != 2 {
			return exitError(2, "destination must be in format 'container:path'")
		}
		containerName := parts[0]
		remotePath := parts[1]

		mgr := container.NewManager(containerName)

		// Check if source exists
		info, err := os.Stat(localPath)
		if err != nil {
			return exitError(1, fmt.Sprintf("source does not exist: %v", err))
		}

		// Push file or directory
		if info.IsDir() {
			if !recursive {
				return exitError(2, "source is a directory, use -r flag")
			}
			if err := mgr.PushDirectory(localPath, remotePath); err != nil {
				return exitError(1, fmt.Sprintf("failed to push directory: %v", err))
			}
			fmt.Fprintf(os.Stderr, "Pushed directory %s -> %s:%s\n", localPath, containerName, remotePath)
		} else {
			if err := mgr.PushFile(localPath, remotePath); err != nil {
				return exitError(1, fmt.Sprintf("failed to push file: %v", err))
			}
			fmt.Fprintf(os.Stderr, "Pushed file %s -> %s:%s\n", localPath, containerName, remotePath)
		}

		return nil
	},
}

// filePullCmd pulls files/directories from a container
var filePullCmd = &cobra.Command{
	Use:   "pull <container>:<remote-path> <local-path>",
	Short: "Pull a file or directory from a container",
	Long: `Pull a file or directory from a container to the host.

Example:
  # Pull directory (e.g., save Claude session data)
  coi file pull -r my-container:/root/.claude ./saved-sessions/session-123/`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		localPath := args[1]

		recursive, _ := cmd.Flags().GetBool("recursive")

		// Parse source (container:path)
		parts := strings.SplitN(source, ":", 2)
		if len(parts) != 2 {
			return exitError(2, "source must be in format 'container:path'")
		}
		containerName := parts[0]
		remotePath := parts[1]

		mgr := container.NewManager(containerName)

		// For now, always pull recursively if -r is specified
		if recursive {
			if err := mgr.PullDirectory(remotePath, localPath); err != nil {
				return exitError(1, fmt.Sprintf("failed to pull directory: %v", err))
			}
			fmt.Fprintf(os.Stderr, "Pulled directory %s:%s -> %s\n", containerName, remotePath, localPath)
		} else {
			// Pull single file - use the same PullDirectory but with single file path
			if err := mgr.PullDirectory(remotePath, localPath); err != nil {
				return exitError(1, fmt.Sprintf("failed to pull file: %v", err))
			}
			fmt.Fprintf(os.Stderr, "Pulled file %s:%s -> %s\n", containerName, remotePath, localPath)
		}

		return nil
	},
}

func init() {
	// Add flags to push command
	filePushCmd.Flags().BoolP("recursive", "r", false, "Push directory recursively")

	// Add flags to pull command
	filePullCmd.Flags().BoolP("recursive", "r", false, "Pull directory recursively")

	// Add subcommands to file command
	fileCmd.AddCommand(filePushCmd)
	fileCmd.AddCommand(filePullCmd)
}
