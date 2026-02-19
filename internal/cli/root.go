package cli

import (
	"fmt"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/spf13/cobra"
)

// Version is the current version of coi (injected via ldflags at build time)
var Version = "dev"

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
	mountPairs      []string // --mount flag for custom mounts
	networkMode     string

	// Git security flag
	writableGitHooks bool

	// Monitoring flag
	enableMonitoring bool

	// Limit flags
	limitCPU           string
	limitCPUAllowance  string
	limitCPUPriority   int
	limitMemory        string
	limitMemorySwap    string
	limitMemoryEnforce string
	limitDiskRead      string
	limitDiskWrite     string
	limitDiskMax       string
	limitDiskPriority  int
	limitProcesses     int
	limitDuration      string

	// Loaded config
	cfg *config.Config
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "coi",
	Short: "Code on Incus - Run AI coding tools in isolated Incus containers",
	Long: `code-on-incus (coi) is a CLI tool for running AI coding assistants in Incus containers
with session persistence, workspace isolation, and multi-slot support.

By default runs Claude Code. Other tools can be configured via the tool.name config option.

Examples:
  coi                          # Start interactive AI coding session (same as 'coi shell')
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
	rootCmd.PersistentFlags().StringArrayVar(&mountPairs, "mount", []string{}, "Mount directory (HOST:CONTAINER, repeatable)")
	rootCmd.PersistentFlags().StringVar(&networkMode, "network", "", "Network mode: restricted (default), open")
	rootCmd.PersistentFlags().BoolVar(&writableGitHooks, "writable-git-hooks", false,
		"Allow container to write to .git/hooks (disables security protection)")
	rootCmd.PersistentFlags().BoolVar(&enableMonitoring, "monitor", false,
		"Enable security monitoring with automatic threat response")

	// Resource limit flags
	rootCmd.PersistentFlags().StringVar(&limitCPU, "limit-cpu", "", "CPU count limit (e.g., '2', '0-3', '0,1,3')")
	rootCmd.PersistentFlags().StringVar(&limitCPUAllowance, "limit-cpu-allowance", "", "CPU allowance (e.g., '50%', '25ms/100ms')")
	rootCmd.PersistentFlags().IntVar(&limitCPUPriority, "limit-cpu-priority", 0, "CPU priority (0-10)")
	rootCmd.PersistentFlags().StringVar(&limitMemory, "limit-memory", "", "Memory limit (e.g., '2GiB', '512MiB', '50%')")
	rootCmd.PersistentFlags().StringVar(&limitMemorySwap, "limit-memory-swap", "", "Memory swap (true, false, or size)")
	rootCmd.PersistentFlags().StringVar(&limitMemoryEnforce, "limit-memory-enforce", "", "Memory enforce mode (hard or soft)")
	rootCmd.PersistentFlags().StringVar(&limitDiskRead, "limit-disk-read", "", "Disk read rate (e.g., '10MiB/s', '1000iops')")
	rootCmd.PersistentFlags().StringVar(&limitDiskWrite, "limit-disk-write", "", "Disk write rate (e.g., '5MiB/s', '1000iops')")
	rootCmd.PersistentFlags().StringVar(&limitDiskMax, "limit-disk-max", "", "Combined disk I/O limit")
	rootCmd.PersistentFlags().IntVar(&limitDiskPriority, "limit-disk-priority", 0, "Disk priority (0-10)")
	rootCmd.PersistentFlags().IntVar(&limitProcesses, "limit-processes", 0, "Max processes (0 = unlimited)")
	rootCmd.PersistentFlags().StringVar(&limitDuration, "limit-duration", "", "Max runtime (e.g., '2h', '30m', '1h30m')")

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
	rootCmd.AddCommand(persistCmd)
	rootCmd.AddCommand(tmuxCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(resumeCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("code-on-incus (coi) v%s\n", Version)
		fmt.Println("https://github.com/mensfeld/code-on-incus")
	},
}

// mergeLimitsConfig merges limits from config and CLI flags
// CLI flags take precedence over config file
func mergeLimitsConfig(cmd *cobra.Command) *config.LimitsConfig {
	limits := &config.LimitsConfig{
		CPU:     cfg.Limits.CPU,
		Memory:  cfg.Limits.Memory,
		Disk:    cfg.Limits.Disk,
		Runtime: cfg.Limits.Runtime,
	}

	// Apply CLI flag overrides (only if flag was explicitly set)
	if cmd.Flags().Changed("limit-cpu") {
		limits.CPU.Count = limitCPU
	}
	if cmd.Flags().Changed("limit-cpu-allowance") {
		limits.CPU.Allowance = limitCPUAllowance
	}
	if cmd.Flags().Changed("limit-cpu-priority") {
		limits.CPU.Priority = limitCPUPriority
	}
	if cmd.Flags().Changed("limit-memory") {
		limits.Memory.Limit = limitMemory
	}
	if cmd.Flags().Changed("limit-memory-swap") {
		limits.Memory.Swap = limitMemorySwap
	}
	if cmd.Flags().Changed("limit-memory-enforce") {
		limits.Memory.Enforce = limitMemoryEnforce
	}
	if cmd.Flags().Changed("limit-disk-read") {
		limits.Disk.Read = limitDiskRead
	}
	if cmd.Flags().Changed("limit-disk-write") {
		limits.Disk.Write = limitDiskWrite
	}
	if cmd.Flags().Changed("limit-disk-max") {
		limits.Disk.Max = limitDiskMax
	}
	if cmd.Flags().Changed("limit-disk-priority") {
		limits.Disk.Priority = limitDiskPriority
	}
	if cmd.Flags().Changed("limit-processes") {
		limits.Runtime.MaxProcesses = limitProcesses
	}
	if cmd.Flags().Changed("limit-duration") {
		limits.Runtime.MaxDuration = limitDuration
	}

	return limits
}
