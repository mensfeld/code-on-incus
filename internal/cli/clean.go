package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/spf13/cobra"
)

var (
	cleanAll      bool
	cleanForce    bool
	cleanSessions bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Cleanup containers and sessions",
	Long: `Cleanup stopped containers and old session data.

By default, cleans only stopped containers. Use flags to control what gets cleaned.

Examples:
  coi clean                    # Clean stopped containers
  coi clean --sessions         # Clean saved session data
  coi clean --all              # Clean everything
  coi clean --all --force      # Clean without confirmation
`,
	RunE: cleanCommand,
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "Clean all containers and sessions")
	cleanCmd.Flags().BoolVar(&cleanForce, "force", false, "Skip confirmation prompts")
	cleanCmd.Flags().BoolVar(&cleanSessions, "sessions", false, "Clean saved session data")
}

func cleanCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get configured tool to determine tool-specific sessions directory
	toolInstance, err := getConfiguredTool(cfg)
	if err != nil {
		return err
	}

	// Get tool-specific sessions directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	baseDir := filepath.Join(homeDir, ".coi")
	sessionsDir := session.GetSessionsDir(baseDir, toolInstance)

	cleaned := 0

	// Clean stopped containers
	if cleanAll || (!cleanSessions) {
		fmt.Println("Checking for stopped claude-on-incus containers...")

		containers, err := listActiveContainers()
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		stoppedContainers := []string{}
		for _, c := range containers {
			if c.Status == "Stopped" || c.Status == "STOPPED" {
				stoppedContainers = append(stoppedContainers, c.Name)
			}
		}

		if len(stoppedContainers) > 0 {
			fmt.Printf("Found %d stopped container(s):\n", len(stoppedContainers))
			for _, name := range stoppedContainers {
				fmt.Printf("  - %s\n", name)
			}

			if !cleanForce {
				fmt.Print("\nDelete these containers? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response) // Ignore error, default to "no" if read fails
				if response != "y" && response != "Y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			for _, name := range stoppedContainers {
				fmt.Printf("Deleting container %s...\n", name)
				mgr := container.NewManager(name)
				if err := mgr.Delete(true); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to delete %s: %v\n", name, err)
				} else {
					cleaned++
				}
			}
		} else {
			fmt.Println("  (no stopped containers found)")
		}
	}

	// Clean saved sessions
	if cleanAll || cleanSessions {
		fmt.Println("\nChecking for saved session data...")

		entries, err := os.ReadDir(sessionsDir)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read sessions directory: %w", err)
		}

		sessionDirs := []string{}
		for _, entry := range entries {
			if entry.IsDir() {
				sessionDirs = append(sessionDirs, entry.Name())
			}
		}

		if len(sessionDirs) > 0 {
			fmt.Printf("Found %d session(s):\n", len(sessionDirs))
			for _, name := range sessionDirs {
				fmt.Printf("  - %s\n", name)
			}

			if !cleanForce {
				fmt.Print("\nDelete all session data? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response) // Ignore error, default to "no" if read fails
				if response != "y" && response != "Y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			for _, name := range sessionDirs {
				sessionPath := filepath.Join(sessionsDir, name)
				fmt.Printf("Deleting session %s...\n", name)
				if err := os.RemoveAll(sessionPath); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to delete %s: %v\n", name, err)
				} else {
					cleaned++
				}
			}
		} else {
			fmt.Println("  (no saved sessions found)")
		}
	}

	if cleaned > 0 {
		fmt.Printf("\n✓ Cleaned %d item(s)\n", cleaned)
	} else {
		fmt.Println("\nNothing to clean.")
	}

	return nil
}
