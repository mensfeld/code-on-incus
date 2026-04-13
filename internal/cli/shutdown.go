package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/network"
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
	shutdownCmd.Flags().BoolVarP(&shutdownForce, "force", "f", false, "Skip confirmation prompts")
	shutdownCmd.Flags().BoolVarP(&shutdownAll, "all", "a", false, "Shutdown all containers")
}

func shutdownCommand(cmd *cobra.Command, args []string) error {
	containerNames, err := resolveContainerArgs(args, shutdownAll, shutdownForce, "Shutdown")
	if err != nil {
		return err
	}
	if containerNames == nil {
		return nil
	}

	// Shutdown each container
	shutdown := 0
	for _, name := range containerNames {
		fmt.Printf("Shutting down container %s (timeout: %ds)...\n", name, shutdownTimeout)
		mgr := container.NewManager(name)

		// Check if container exists at all before attempting anything
		exists, err := mgr.Exists()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: Failed to check if %s exists: %v\n", name, err)
			continue
		}
		if !exists {
			fmt.Fprintf(os.Stderr, "  Warning: Container %s does not exist\n", name)
			continue
		}

		// Get container IP and veth name BEFORE stopping/deleting (needed for firewall cleanup)
		var containerIP string
		var vethName string
		if network.FirewallAvailable() {
			containerIP, _ = network.GetContainerIPFast(name)
			vethName, _ = network.GetContainerVethName(name)
		}

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

		// Clean up firewall rules BEFORE deleting container
		if containerIP != "" {
			if err := cleanupFirewallRulesForIP(containerIP); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: Failed to cleanup firewall rules: %v\n", err)
			}
			// Also clean up NFT monitoring rules for this IP
			if err := cleanupNFTMonitoringRulesForIP(containerIP); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: Failed to cleanup NFT monitoring rules: %v\n", err)
			}
		}

		// Delete container (may already be gone if ephemeral or cleaned by shell process)
		if err := mgr.Delete(true); err != nil {
			// Check if container is already gone — that counts as success
			if exists, existsErr := mgr.Exists(); existsErr == nil && !exists {
				shutdown++
				fmt.Printf("  ✓ Shutdown %s (already removed)\n", name)
			} else {
				fmt.Fprintf(os.Stderr, "  Warning: Failed to delete %s: %v\n", name, err)
			}
		} else {
			shutdown++
			fmt.Printf("  ✓ Shutdown %s\n", name)
		}

		// Clean up firewalld zone binding AFTER container deletion
		if vethName != "" {
			if err := network.RemoveVethFromFirewalldZone(vethName); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: Failed to cleanup firewalld zone binding: %v\n", err)
			}
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
