package cli

import (
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/spf13/cobra"
)

var (
	killForce bool
	killAll   bool
)

var killCmd = &cobra.Command{
	Use:   "kill [container-name...]",
	Short: "Force stop and delete containers immediately",
	Long: `Force stop and delete one or more containers by name.

This immediately force-kills containers without waiting for graceful shutdown.
For graceful shutdown, use 'coi shutdown' instead.

Use 'coi list' to see active containers.

Examples:
  coi kill claude-abc12345-1           # Force kill specific container
  coi kill claude-abc12345-1 claude-xyz78901-2  # Force kill multiple containers
  coi kill --all                       # Force kill all containers (with confirmation)
  coi kill --all --force               # Force kill all without confirmation
`,
	RunE: killCommand,
}

func init() {
	killCmd.Flags().BoolVar(&killForce, "force", false, "Skip confirmation prompts")
	killCmd.Flags().BoolVar(&killAll, "all", false, "Kill all containers")
}

func killCommand(cmd *cobra.Command, args []string) error {
	// Get container names to kill
	var containerNames []string

	if killAll {
		// Get all containers
		containers, err := listActiveContainers()
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		if len(containers) == 0 {
			fmt.Println("No containers to kill")
			return nil
		}

		for _, c := range containers {
			containerNames = append(containerNames, c.Name)
		}

		// Show what will be killed
		fmt.Printf("Found %d container(s):\n", len(containerNames))
		for _, name := range containerNames {
			fmt.Printf("  - %s\n", name)
		}

		// Confirm unless --force
		if !killForce {
			fmt.Print("\nKill all these containers? [y/N]: ")
			var response string
			_, _ = fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}
	} else {
		// Use containers from args
		if len(args) == 0 {
			return fmt.Errorf("no container names provided - use 'coi list' to see active containers")
		}
		containerNames = args

		// Confirm unless --force
		if !killForce && len(containerNames) > 1 {
			fmt.Printf("Kill %d container(s)? [y/N]: ", len(containerNames))
			var response string
			_, _ = fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}
	}

	// Kill each container
	killed := 0
	for _, name := range containerNames {
		fmt.Printf("Killing container %s...\n", name)
		mgr := container.NewManager(name)

		// Check if container exists first
		exists, err := mgr.Exists()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: Failed to check if %s exists: %v\n", name, err)
			continue
		}
		if !exists {
			fmt.Fprintf(os.Stderr, "  Warning: Container %s does not exist\n", name)
			continue
		}

		// Stop container (only if running - skip if already stopped)
		running, err := mgr.Running()
		if err == nil && running {
			if err := mgr.Stop(true); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: Failed to stop %s: %v\n", name, err)
			}
		}

		// Delete container
		if err := mgr.Delete(true); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: Failed to delete %s: %v\n", name, err)
		} else {
			killed++
			fmt.Printf("  ✓ Killed %s\n", name)
		}
	}

	if killed > 0 {
		fmt.Printf("\nKilled %d container(s)\n", killed)
	} else {
		fmt.Println("\nNo containers were killed")
		if len(containerNames) > 0 {
			// User specified containers but none were killed - this is an error
			return fmt.Errorf("failed to kill specified containers")
		}
	}

	return nil
}
