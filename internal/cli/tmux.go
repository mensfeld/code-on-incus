package cli

import (
	"fmt"

	"github.com/mensfeld/claude-on-incus/internal/container"
	"github.com/spf13/cobra"
)

var tmuxCmd = &cobra.Command{
	Use:   "tmux",
	Short: "Interact with tmux sessions in containers",
	Long: `Send commands to or capture output from AI coding sessions running in tmux.
This is primarily for automated workflows.`,
}

var tmuxSendCmd = &cobra.Command{
	Use:   "send SESSION_NAME COMMAND",
	Short: "Send a command to a tmux session",
	Long: `Send a command to a running tmux session in a container.
The session name should be the container name (e.g., coi-abc123-1).`,
	Args: cobra.ExactArgs(2),
	RunE: tmuxSendCommand,
}

var tmuxCaptureCmd = &cobra.Command{
	Use:   "capture SESSION_NAME",
	Short: "Capture output from a tmux session",
	Long: `Capture the current pane output from a tmux session.
The session name should be the container name (e.g., coi-abc123-1).`,
	Args: cobra.ExactArgs(1),
	RunE: tmuxCaptureCommand,
}

var tmuxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active tmux sessions",
	Long:  `List all active tmux sessions across all containers.`,
	RunE:  tmuxListCommand,
}

func init() {
	tmuxCmd.AddCommand(tmuxSendCmd)
	tmuxCmd.AddCommand(tmuxCaptureCmd)
	tmuxCmd.AddCommand(tmuxListCmd)
}

func tmuxSendCommand(cmd *cobra.Command, args []string) error {
	containerName := args[0]
	command := args[1]

	mgr := container.NewManager(containerName)

	// Check if container is running
	running, err := mgr.Running()
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}
	if !running {
		return fmt.Errorf("container %s is not running", containerName)
	}

	// Send command to tmux session
	tmuxSession := fmt.Sprintf("coi-%s", containerName)
	tmuxCmd := fmt.Sprintf("tmux send-keys -t %s %q Enter", tmuxSession, command)

	opts := container.ExecCommandOptions{
		Interactive: false,
		Capture:     true,
	}

	_, err = mgr.ExecCommand(tmuxCmd, opts)
	if err != nil {
		return fmt.Errorf("failed to send command to tmux session: %w", err)
	}

	fmt.Printf("Sent command to session %s\n", tmuxSession)
	return nil
}

func tmuxCaptureCommand(cmd *cobra.Command, args []string) error {
	containerName := args[0]

	mgr := container.NewManager(containerName)

	// Check if container is running
	running, err := mgr.Running()
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}
	if !running {
		return fmt.Errorf("container %s is not running", containerName)
	}

	// Capture tmux pane output
	tmuxSession := fmt.Sprintf("coi-%s", containerName)
	tmuxCmd := fmt.Sprintf("tmux capture-pane -t %s -p", tmuxSession)

	opts := container.ExecCommandOptions{
		Interactive: false,
		Capture:     true,
	}

	output, err := mgr.ExecCommand(tmuxCmd, opts)
	if err != nil {
		return fmt.Errorf("failed to capture tmux output: %w", err)
	}

	fmt.Print(output)
	return nil
}

func tmuxListCommand(cmd *cobra.Command, args []string) error {
	// List all running containers with configured prefix
	containers, err := container.ListContainers("coi-.*")
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		fmt.Println("No active sessions")
		return nil
	}

	fmt.Println("Active sessions:")
	for _, c := range containers {
		mgr := container.NewManager(c)

		// Check if container is running
		running, err := mgr.Running()
		if err != nil || !running {
			continue
		}

		// Check if tmux session exists
		tmuxSession := fmt.Sprintf("coi-%s", c)
		tmuxCmd := fmt.Sprintf("tmux has-session -t %s 2>/dev/null", tmuxSession)

		opts := container.ExecCommandOptions{
			Interactive: false,
			Capture:     false,
		}

		_, err = mgr.ExecCommand(tmuxCmd, opts)
		if err == nil {
			fmt.Printf("  - %s (tmux session: %s)\n", c, tmuxSession)
		}
	}

	return nil
}
