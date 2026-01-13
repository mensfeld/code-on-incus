package cli

import (
	"fmt"

	"github.com/mensfeld/claude-on-incus/internal/config"
	"github.com/spf13/cobra"
)

const (
	// Version is the current version of coi
	Version = "0.3.0"
)

var (
	// Global flags
	workspace       string
	slot            int
	imageName       string
	persistent      bool
	resume          string
	continueSession string // Alias for resume
	profile         string
	envVars         []string
	storage         string

	// Loaded config
	cfg *config.Config
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "coi",
	Short: "Claude on Incus - Run Claude Code in isolated Incus containers",
	Long: `claude-on-incus (coi) is a CLI tool for running Claude Code in Incus containers
with session persistence, workspace isolation, and multi-slot support.

Examples:
  coi                          # Start interactive Claude session (same as 'coi shell')
  coi shell --slot 2           # Use specific slot
  coi run "npm test"           # Run command in container
  coi build                    # Build coi image
  coi images                   # List available images
  coi list                     # List active sessions
`,
	Version: Version,
	// When called without subcommand, run shell command
	RunE: func(cmd *cobra.Command, args []string) error {
		// Execute shell command with the same args
		return shellCmd.RunE(cmd, args)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Apply profile if specified
		if profile != "" {
			if !cfg.ApplyProfile(profile) {
				return fmt.Errorf("profile '%s' not found", profile)
			}
		}

		// Apply config defaults to flags that weren't explicitly set
		if !cmd.Flags().Changed("persistent") {
			persistent = cfg.Defaults.Persistent
		}

		return nil
	},
}

// Execute runs the root command
func Execute(isCoi bool) error {
	if !isCoi {
		rootCmd.Use = "claude-on-incus"
	}
	return rootCmd.Execute()
}

func init() {
	// Global flags available to all commands
	rootCmd.PersistentFlags().StringVarP(&workspace, "workspace", "w", ".", "Workspace directory to mount")
	rootCmd.PersistentFlags().IntVar(&slot, "slot", 0, "Slot number for parallel sessions (0 = auto-allocate)")
	rootCmd.PersistentFlags().StringVar(&imageName, "image", "", "Custom image to use (default: coi)")
	rootCmd.PersistentFlags().BoolVar(&persistent, "persistent", false, "Reuse container across sessions")
	rootCmd.PersistentFlags().StringVar(&resume, "resume", "", "Resume from session ID (omit value to auto-detect)")
	rootCmd.PersistentFlags().Lookup("resume").NoOptDefVal = "auto"
	rootCmd.PersistentFlags().StringVar(&continueSession, "continue", "", "Alias for --resume")
	rootCmd.PersistentFlags().Lookup("continue").NoOptDefVal = "auto"
	rootCmd.PersistentFlags().StringVar(&profile, "profile", "", "Use named profile")
	rootCmd.PersistentFlags().StringSliceVarP(&envVars, "env", "e", []string{}, "Environment variables (KEY=VALUE)")
	rootCmd.PersistentFlags().StringVar(&storage, "storage", "", "Mount persistent storage")

	// Add subcommands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(imagesCmd)    // Legacy: coi images
	rootCmd.AddCommand(imageCmd)     // New: coi image <subcommand>
	rootCmd.AddCommand(containerCmd) // New: coi container <subcommand>
	rootCmd.AddCommand(fileCmd)      // New: coi file <subcommand>
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(tmuxCmd)
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("claude-on-incus (coi) v%s\n", Version)
		fmt.Println("https://github.com/mensfeld/claude-on-incus")
	},
}
