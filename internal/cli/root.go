package cli

import (
	"fmt"
	"regexp"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/spf13/cobra"
)

// Version is the current version of coi (injected via ldflags at build time)
var Version = "dev"

// App holds the shared CLI state that is populated from persistent flags and
// config loading in PersistentPreRunE. Grouping this in a struct rather than
// package-level vars makes it easier to construct an isolated instance in
// tests and clears the path toward a multi-session daemon model.
type App struct {
	workspace       string
	slot            int
	persistent      bool // config-driven: [container] persistent (no CLI flag)
	resume          string
	continueSession string
	profile         string
	cfg             *config.Config
}

// app is the singleton used by the cobra command tree. Execute() resets it to
// a zero value on each call so tests that invoke Execute() multiple times
// start with clean state (cobra re-parses flags, PersistentPreRunE reloads
// the config).
var app = &App{}

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
  coi run -- npm test          # Run a command in the sandbox
  coi run                      # Run the workspace run script (coi-run)
  coi build                    # Build coi image
  coi image list               # List available images
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
		app.cfg, err = config.Load()
		if err != nil {
			// Allow health command to run with defaults even if config is broken
			if cmd.Name() == "health" {
				app.cfg = config.GetDefaultConfig()
			} else {
				return fmt.Errorf("failed to load config: %w", err)
			}
		}

		// Apply profile if specified
		if app.profile != "" {
			if err := app.cfg.ApplyProfile(app.profile); err != nil {
				return err
			}
		}

		// Apply Incus configuration from config file
		container.Configure(app.cfg.Incus.Project, app.cfg.Incus.CodeUser, app.cfg.Incus.CodeUID)

		// Honor [network] use_sudo process-wide so every network sudo entry point
		// (nft probe, iptables fallback, boot block, health/build/clean) behaves
		// as if passwordless sudo were unavailable when disabled.
		network.SetSudoAllowed(app.cfg.Network.SudoAllowed())

		// Persistence is config-driven ([container] persistent, settable per
		// profile) — the former --persistent flag was removed.
		app.persistent = config.BoolVal(app.cfg.Container.Persistent)

		// Silence usage output for RunE errors. Setting this here (in
		// PersistentPreRunE) rather than globally means cobra still prints
		// usage for arg/flag validation errors (which fire before this hook),
		// but won't dump usage for RunE errors like ExitCodeError.
		cmd.SilenceUsage = true

		return nil
	},
}

// removedFlagRe matches the removed --image/--persistent flags as EXACT
// tokens inside a flag-error message ("unknown flag: --image",
// "flag needs an argument: --image"). The boundary check prevents false
// hints for flags that merely share the prefix (--images, --image-tag,
// --persistent-home): the token must be followed by end-of-string,
// whitespace, or '=' — never by more flag-name characters.
var removedFlagRe = regexp.MustCompile(`(--image|--persistent)($|[\s=])`)

// removedFlagHint upgrades the bare "unknown flag" error for the removed
// --image/--persistent flags into an actionable migration message. The flags
// no longer exist anywhere — image selection and persistence are configured
// via [container] image / persistent, settable globally, per project, or per
// profile (edit the profile's config.toml).
func removedFlagHint(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if m := removedFlagRe.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("%s\n\nThe %s flag was removed. Set it via config instead:\n"+
			"  [container]\n"+
			"  image = \"my-image\"      # was --image\n"+
			"  persistent = true       # was --persistent\n"+
			"in ~/.coi/config.toml, ./.coi/config.toml, or a profile (--profile <name>)",
			msg, m[1])
	}
	return err
}

// Execute runs the root command
func Execute(isCoi bool) error {
	// Reset app state so that repeated calls (e.g. in tests) start clean.
	// Cobra re-parses flags on every Execute, so flag-bound fields are
	// repopulated correctly from the fresh zero value.
	*app = App{}

	if !isCoi {
		rootCmd.Use = "claude-on-incus"
	}
	// Prevent cobra from double-printing errors — main.go handles error output.
	rootCmd.SilenceErrors = true
	return rootCmd.Execute()
}

func init() {
	// Global flags available to all commands.
	// NOTE: --image and --persistent were removed on purpose — anything
	// config-shaped goes via config/profiles ([container] image / persistent).
	// removedFlagHint below turns their unknown-flag errors into a migration hint.
	rootCmd.PersistentFlags().StringVarP(&app.workspace, "workspace", "w", ".", "Workspace directory to mount")
	rootCmd.PersistentFlags().IntVar(&app.slot, "slot", 0, "Slot number for parallel sessions (0 = auto-allocate)")
	rootCmd.PersistentFlags().StringVar(&app.resume, "resume", "", "Resume from session ID (omit value to auto-detect)")
	rootCmd.PersistentFlags().Lookup("resume").NoOptDefVal = "auto"
	rootCmd.PersistentFlags().StringVar(&app.continueSession, "continue", "", "Alias for --resume")
	rootCmd.PersistentFlags().Lookup("continue").NoOptDefVal = "auto"
	rootCmd.PersistentFlags().StringVar(&app.profile, "profile", "", "Use named profile")

	rootCmd.SetFlagErrorFunc(removedFlagHint)

	// Add subcommands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(imageCmd)     // coi image <subcommand>
	rootCmd.AddCommand(containerCmd) // New: coi container <subcommand>
	rootCmd.AddCommand(fileCmd)      // New: coi file <subcommand>
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(persistCmd)
	rootCmd.AddCommand(tmuxCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(unfreezeCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(profileCmd)
	rootCmd.AddCommand(schemaCmd)
	rootCmd.AddCommand(trustCmd)
	rootCmd.AddCommand(untrustCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(shutdownCmd)
	rootCmd.AddCommand(monitorCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		if format != "text" && format != "json" {
			return &ExitCodeError{Code: 2, Message: fmt.Sprintf("invalid format %q: must be 'text' or 'json'", format)}
		}
		if format == "json" {
			fmt.Printf(`{"version":%q,"url":"https://github.com/mensfeld/code-on-incus"}`+"\n", "v"+Version)
			return nil
		}
		fmt.Printf("code-on-incus (coi) v%s\n", Version)
		fmt.Println("https://github.com/mensfeld/code-on-incus")
		return nil
	},
}

func init() {
	versionCmd.Flags().String("format", "text", "Output format: text or json")
}
