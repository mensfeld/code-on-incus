package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/spf13/cobra"
)

var (
	shutdownTimeout int
	shutdownForce   bool
	shutdownAll     bool
)

var shutdownCmd = &cobra.Command{
	Use:   "shutdown [container-name...]",
	Short: "Gracefully stop and delete containers",
	Long: `Gracefully stop and delete one or more containers by name.

This attempts a graceful shutdown first, waiting for the timeout before
force-killing if necessary.

Use 'coi list' to see active containers.

Examples:
  coi shutdown claude-abc12345-1             # Graceful shutdown (60s timeout)
  coi shutdown --timeout=30 claude-abc12345-1  # 30 second timeout
  coi shutdown --all                         # Shutdown all containers
  coi shutdown --all --force                 # Shutdown all without confirmation
`,
	RunE: shutdownCommand,
}

func init() {
	shutdownCmd.Flags().IntVar(&shutdownTimeout, "timeout", 60, "Timeout in seconds to wait for graceful shutdown before force-killing")
	shutdownCmd.Flags().BoolVar(&shutdownForce, "force", false, "Skip confirmation prompts")
	shutdownCmd.Flags().BoolVar(&shutdownAll, "all", false, "Shutdown all containers")
	rootCmd.AddCommand(shutdownCmd)
}

func shutdownCommand(cmd *cobra.Command, args []string) error {
	// Get container names to shutdown
	var containerNames []string

	if shutdownAll {
		// Get all containers
		containers, err := listActiveContainers()
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		if len(containers) == 0 {
			fmt.Println("No containers to shutdown")
			return nil
		}

		for _, c := range containers {
			containerNames = append(containerNames, c.Name)
		}

		// Show what will be shutdown
		fmt.Printf("Found %d container(s):\n", len(containerNames))
		for _, name := range containerNames {
			fmt.Printf("  - %s\n", name)
		}

		// Confirm unless --force
		if !shutdownForce {
			fmt.Print("\nShutdown all these containers? [y/N]: ")
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
		if !shutdownForce && len(containerNames) > 1 {
			fmt.Printf("Shutdown %d container(s)? [y/N]: ", len(containerNames))
			var response string
			_, _ = fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}
	}

	// Shutdown each container
	shutdown := 0
	for _, name := range containerNames {
		fmt.Printf("Shutting down container %s (timeout: %ds)...\n", name, shutdownTimeout)
		mgr := container.NewManager(name)

		// Check if container is running
		running, err := mgr.Running()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: Failed to check status of %s: %v\n", name, err)
			continue
		}

		if running {
			// First attempt graceful stop
			fmt.Printf("  Attempting graceful shutdown...\n")
			gracefulDone := make(chan error, 1)
			go func() {
				gracefulDone <- mgr.Stop(false) // graceful stop
			}()

			// Wait for graceful stop or timeout
			select {
			case err := <-gracefulDone:
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: Graceful stop failed: %v\n", err)
				} else {
					fmt.Printf("  Graceful shutdown successful\n")
				}
			case <-time.After(time.Duration(shutdownTimeout) * time.Second):
				// Check if container stopped during timeout (avoids spurious errors)
				if stillRunning, _ := mgr.Running(); stillRunning {
					fmt.Printf("  Timeout reached, force-killing...\n")
					if err := mgr.Stop(true); err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: Force stop failed: %v\n", err)
					}
				} else {
					fmt.Printf("  Container stopped during timeout\n")
				}
			}
		}

		// Delete container
		if err := mgr.Delete(true); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: Failed to delete %s: %v\n", name, err)
		} else {
			shutdown++
			fmt.Printf("  ✓ Shutdown %s\n", name)
		}
	}

	if shutdown > 0 {
		fmt.Printf("\nShutdown %d container(s)\n", shutdown)
	} else {
		fmt.Println("\nNo containers were shutdown")
		if len(containerNames) > 0 {
			// User specified containers but none were shutdown - this is an error
			return fmt.Errorf("failed to shutdown specified containers")
		}
	}

	return nil
}
