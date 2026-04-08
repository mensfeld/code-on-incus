package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/spf13/cobra"
)

var profileFormat string

// profileCmd is the parent command for profile operations
var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage profiles",
	Long:  `List and inspect available profiles from all configuration sources.`,
}

// profileListCmd lists all available profiles
var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available profiles",
	Long: `List all profiles from system, user, and project configuration.

Profiles are defined as directories under profiles/, each containing a config.toml.

Examples:
  coi profile list
  coi profile list --format json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if profileFormat != "text" && profileFormat != "json" {
			return &ExitCodeError{Code: 2, Message: fmt.Sprintf("invalid format '%s': must be 'text' or 'json'", profileFormat)}
		}

		if len(cfg.Profiles) == 0 {
			if profileFormat == "json" {
				fmt.Println("[]")
				return nil
			}
			fmt.Fprintln(os.Stderr, "No profiles configured.")
			return nil
		}

		// Sort profile names for consistent output
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)

		if profileFormat == "json" {
			type profileEntry struct {
				Name       string `json:"name"`
				Image      string `json:"image"`
				Persistent *bool  `json:"persistent"`
				Inherits   string `json:"inherits,omitempty"`
				Source     string `json:"source,omitempty"`
			}
			entries := make([]profileEntry, 0, len(names))
			for _, name := range names {
				p := cfg.Profiles[name]
				entries = append(entries, profileEntry{
					Name:       name,
					Image:      p.Image,
					Persistent: p.Persistent,
					Inherits:   p.Inherits,
					Source:     p.Source,
				})
			}
			jsonData, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %v", err)
			}
			fmt.Println(string(jsonData))
			return nil
		}

		tbl := NewTable("NAME", "IMAGE", "PERSISTENT", "INHERITS", "SOURCE")
		for _, name := range names {
			p := cfg.Profiles[name]
			image := p.Image
			if image == "" {
				image = "(default)"
			}
			persistent := "-"
			if p.Persistent != nil {
				if *p.Persistent {
					persistent = "true"
				} else {
					persistent = "false"
				}
			}
			inherits := "-"
			if p.Inherits != "" {
				inherits = p.Inherits
			}
			source := p.Source
			if source == "" {
				source = "(unknown)"
			}
			tbl.AddRow(name, image, persistent, inherits, source)
		}
		tbl.Render()
		return nil
	},
}

// profileInfoCmd shows details of a specific profile
var profileInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show detailed information about a profile",
	Long: `Display the full configuration of a named profile.

Examples:
  coi profile info rust-dev`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		p := cfg.GetProfile(name)
		if p == nil {
			return fmt.Errorf("profile '%s' not found", name)
		}

		fmt.Printf("Profile: %s\n", name)
		if p.Source != "" {
			fmt.Printf("Source:  %s\n", p.Source)
		}
		fmt.Println()

		if p.Inherits != "" {
			fmt.Printf("inherits = %q\n", p.Inherits)
		}
		if p.Image != "" {
			fmt.Printf("image = %q\n", p.Image)
		}
		if p.Context != "" {
			fmt.Printf("context = %q\n", p.Context)
		}
		if p.Persistent != nil {
			fmt.Printf("persistent = %v\n", *p.Persistent)
		}
		if p.Model != "" {
			fmt.Printf("model = %q\n", p.Model)
		}
		if len(p.ForwardEnv) > 0 {
			fmt.Printf("forward_env = [%s]\n", formatStringSlice(p.ForwardEnv))
		}

		if len(p.Environment) > 0 {
			fmt.Println()
			fmt.Println("[environment]")
			keys := make([]string, 0, len(p.Environment))
			for k := range p.Environment {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%s = %q\n", k, p.Environment[k])
			}
		}

		if p.Tool != nil {
			fmt.Println()
			fmt.Println("[tool]")
			if p.Tool.Name != "" {
				fmt.Printf("name = %q\n", p.Tool.Name)
			}
			if p.Tool.Binary != "" {
				fmt.Printf("binary = %q\n", p.Tool.Binary)
			}
			if p.Tool.PermissionMode != "" {
				fmt.Printf("permission_mode = %q\n", p.Tool.PermissionMode)
			}
			if p.Tool.ContextFile != "" {
				fmt.Printf("context_file = %q\n", p.Tool.ContextFile)
			}
			if p.Tool.AutoContext != nil {
				fmt.Printf("auto_context = %v\n", *p.Tool.AutoContext)
			}
			if p.Tool.Claude.EffortLevel != "" {
				fmt.Println()
				fmt.Println("[tool.claude]")
				fmt.Printf("effort_level = %q\n", p.Tool.Claude.EffortLevel)
			}
		}

		if p.Build != nil {
			fmt.Println()
			fmt.Println("[build]")
			if p.Build.Base != "" {
				fmt.Printf("base = %q\n", p.Build.Base)
			}
			if p.Build.Script != "" {
				fmt.Printf("script = %q\n", p.Build.Script)
			}
			if len(p.Build.Commands) > 0 {
				fmt.Printf("commands = [%s]\n", formatStringSlice(p.Build.Commands))
			}
		}

		if len(p.Mounts) > 0 {
			for _, m := range p.Mounts {
				fmt.Println()
				fmt.Println("[[mounts]]")
				fmt.Printf("host = %q\n", m.Host)
				fmt.Printf("container = %q\n", m.Container)
			}
		}

		if p.Network != nil {
			fmt.Println()
			fmt.Println("[network]")
			if p.Network.Mode != "" {
				fmt.Printf("mode = %q\n", string(p.Network.Mode))
			}
			if len(p.Network.AllowedDomains) > 0 {
				fmt.Printf("allowed_domains = [%s]\n", formatStringSlice(p.Network.AllowedDomains))
			}
		}

		if p.Limits != nil {
			printLimits(p.Limits)
		}

		if p.Paths != nil {
			fmt.Println()
			fmt.Println("[paths]")
			if p.Paths.SessionsDir != "" {
				fmt.Printf("sessions_dir = %q\n", p.Paths.SessionsDir)
			}
			if p.Paths.StorageDir != "" {
				fmt.Printf("storage_dir = %q\n", p.Paths.StorageDir)
			}
			if p.Paths.LogsDir != "" {
				fmt.Printf("logs_dir = %q\n", p.Paths.LogsDir)
			}
			if p.Paths.PreserveWorkspacePath {
				fmt.Println("preserve_workspace_path = true")
			}
		}

		if p.Incus != nil {
			fmt.Println()
			fmt.Println("[incus]")
			if p.Incus.Project != "" {
				fmt.Printf("project = %q\n", p.Incus.Project)
			}
			if p.Incus.Group != "" {
				fmt.Printf("group = %q\n", p.Incus.Group)
			}
			if p.Incus.CodeUID != 0 {
				fmt.Printf("code_uid = %d\n", p.Incus.CodeUID)
			}
			if p.Incus.CodeUser != "" {
				fmt.Printf("code_user = %q\n", p.Incus.CodeUser)
			}
		}

		if p.Git != nil {
			fmt.Println()
			fmt.Println("[git]")
			if p.Git.WritableHooks != nil {
				fmt.Printf("writable_hooks = %v\n", *p.Git.WritableHooks)
			}
		}

		if p.SSH != nil {
			fmt.Println()
			fmt.Println("[ssh]")
			if p.SSH.ForwardAgent != nil {
				fmt.Printf("forward_agent = %v\n", *p.SSH.ForwardAgent)
			}
		}

		if p.Security != nil {
			fmt.Println()
			fmt.Println("[security]")
			if len(p.Security.ProtectedPaths) > 0 {
				fmt.Printf("protected_paths = [%s]\n", formatStringSlice(p.Security.ProtectedPaths))
			}
			if len(p.Security.AdditionalProtectedPaths) > 0 {
				fmt.Printf("additional_protected_paths = [%s]\n", formatStringSlice(p.Security.AdditionalProtectedPaths))
			}
			if p.Security.DisableProtection {
				fmt.Println("disable_protection = true")
			}
		}

		if p.Monitoring != nil {
			fmt.Println()
			fmt.Println("[monitoring]")
			if p.Monitoring.Enabled != nil {
				fmt.Printf("enabled = %v\n", *p.Monitoring.Enabled)
			}
			if p.Monitoring.AutoPauseOnHigh != nil {
				fmt.Printf("auto_pause_on_high = %v\n", *p.Monitoring.AutoPauseOnHigh)
			}
			if p.Monitoring.AutoKillOnCritical != nil {
				fmt.Printf("auto_kill_on_critical = %v\n", *p.Monitoring.AutoKillOnCritical)
			}
		}

		if p.Timezone != nil {
			fmt.Println()
			fmt.Println("[timezone]")
			if p.Timezone.Mode != "" {
				fmt.Printf("mode = %q\n", p.Timezone.Mode)
			}
			if p.Timezone.Name != "" {
				fmt.Printf("name = %q\n", p.Timezone.Name)
			}
		}

		return nil
	},
}

func formatStringSlice(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}

func printLimits(l *config.LimitsConfig) {
	if l.CPU.Count != "" || l.CPU.Allowance != "" || l.CPU.Priority != 0 {
		fmt.Println()
		fmt.Println("[limits.cpu]")
		if l.CPU.Count != "" {
			fmt.Printf("count = %q\n", l.CPU.Count)
		}
		if l.CPU.Allowance != "" {
			fmt.Printf("allowance = %q\n", l.CPU.Allowance)
		}
		if l.CPU.Priority != 0 {
			fmt.Printf("priority = %d\n", l.CPU.Priority)
		}
	}
	if l.Memory.Limit != "" || l.Memory.Enforce != "" || l.Memory.Swap != "" {
		fmt.Println()
		fmt.Println("[limits.memory]")
		if l.Memory.Limit != "" {
			fmt.Printf("limit = %q\n", l.Memory.Limit)
		}
		if l.Memory.Enforce != "" {
			fmt.Printf("enforce = %q\n", l.Memory.Enforce)
		}
		if l.Memory.Swap != "" {
			fmt.Printf("swap = %q\n", l.Memory.Swap)
		}
	}
	if l.Disk.Read != "" || l.Disk.Write != "" || l.Disk.Max != "" {
		fmt.Println()
		fmt.Println("[limits.disk]")
		if l.Disk.Read != "" {
			fmt.Printf("read = %q\n", l.Disk.Read)
		}
		if l.Disk.Write != "" {
			fmt.Printf("write = %q\n", l.Disk.Write)
		}
		if l.Disk.Max != "" {
			fmt.Printf("max = %q\n", l.Disk.Max)
		}
	}
	if l.Runtime.MaxDuration != "" || l.Runtime.MaxProcesses != 0 {
		fmt.Println()
		fmt.Println("[limits.runtime]")
		if l.Runtime.MaxDuration != "" {
			fmt.Printf("max_duration = %q\n", l.Runtime.MaxDuration)
		}
		if l.Runtime.MaxProcesses != 0 {
			fmt.Printf("max_processes = %d\n", l.Runtime.MaxProcesses)
		}
	}
}

// profileShowCmd is a hidden backward-compatible alias for profileInfoCmd
var profileShowCmd = &cobra.Command{
	Use:    "show <name>",
	Short:  profileInfoCmd.Short,
	Long:   profileInfoCmd.Long,
	Args:   profileInfoCmd.Args,
	RunE:   profileInfoCmd.RunE,
	Hidden: true,
}

// resolveProfileDir determines the target directory for a new profile.
func resolveProfileDir(name string, forceUser, forceProject bool) (string, error) {
	if forceUser {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		return filepath.Join(homeDir, ".coi", "profiles", name), nil
	}

	if forceProject {
		absWorkspace, err := filepath.Abs(workspace)
		if err != nil {
			return "", fmt.Errorf("cannot resolve workspace: %w", err)
		}
		return filepath.Join(absWorkspace, ".coi", "profiles", name), nil
	}

	// Default: project if .coi/ exists, otherwise user home
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("cannot resolve workspace: %w", err)
	}
	coiDir := filepath.Join(absWorkspace, ".coi")
	if info, err := os.Stat(coiDir); err == nil && info.IsDir() {
		return filepath.Join(coiDir, "profiles", name), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(homeDir, ".coi", "profiles", name), nil
}

// profileCreateCmd creates a new profile
var profileCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new profile",
	Long: `Create a new profile directory with a minimal config.toml.

By default, creates in .coi/profiles/ if inside a project with .coi/ directory,
otherwise creates in ~/.coi/profiles/. Use --user or --project to override.

Examples:
  coi profile create rust-dev --image coi-rust --inherits default
  coi profile create my-profile --persistent --project`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// Validate name
		if name == "" {
			return fmt.Errorf("profile name cannot be empty")
		}
		if strings.Contains(name, "/") || strings.Contains(name, "\\") {
			return fmt.Errorf("profile name cannot contain slashes")
		}
		if name == "default" {
			return fmt.Errorf("cannot create a profile named 'default' (reserved for built-in profile)")
		}

		forceUser, _ := cmd.Flags().GetBool("user")
		forceProject, _ := cmd.Flags().GetBool("project")
		if forceUser && forceProject {
			return fmt.Errorf("--user and --project are mutually exclusive")
		}

		profileDir, err := resolveProfileDir(name, forceUser, forceProject)
		if err != nil {
			return err
		}

		// Check directory doesn't already exist
		if _, err := os.Stat(profileDir); err == nil {
			return fmt.Errorf("profile directory already exists: %s", profileDir)
		}

		// Build TOML content from flags
		var lines []string
		if image, _ := cmd.Flags().GetString("image"); image != "" {
			lines = append(lines, fmt.Sprintf("image = %q", image))
		}
		if inherits, _ := cmd.Flags().GetString("inherits"); inherits != "" {
			lines = append(lines, fmt.Sprintf("inherits = %q", inherits))
		}
		if persistent, _ := cmd.Flags().GetBool("persistent"); persistent {
			lines = append(lines, "persistent = true")
		}

		content := strings.Join(lines, "\n")
		if content != "" {
			content += "\n"
		}

		// Create directory and write config
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return fmt.Errorf("failed to create profile directory: %w", err)
		}

		configPath := filepath.Join(profileDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
			// Clean up directory on write failure
			os.RemoveAll(profileDir)
			return fmt.Errorf("failed to write config.toml: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Created profile '%s' at %s\n", name, configPath)
		return nil
	},
}

// profileEditCmd opens a profile's config in an editor
var profileEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a profile's config.toml in your editor",
	Long: `Open a profile's config.toml in $VISUAL, $EDITOR, or vi.

After the editor exits, the file is re-parsed and validated.
A warning is printed if the TOML is invalid, but the file is not deleted.

Examples:
  coi profile edit rust-dev
  EDITOR=nano coi profile edit my-profile`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		p := cfg.GetProfile(name)
		if p == nil {
			return fmt.Errorf("profile '%s' not found", name)
		}
		if p.Source == "(built-in)" {
			return fmt.Errorf("cannot edit built-in profile 'default'")
		}

		sourcePath := p.Source

		// Determine editor
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}

		editorCmd := exec.Command(editor, sourcePath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			return fmt.Errorf("editor exited with error: %w", err)
		}

		// Re-parse and validate
		var profileCfg config.ProfileConfig
		if _, err := toml.DecodeFile(sourcePath, &profileCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s contains invalid TOML: %v\n", sourcePath, err)
			return nil
		}
		if err := profileCfg.Validate(name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: profile validation failed: %v\n", err)
		}

		return nil
	},
}

// profileDeleteCmd deletes a profile directory
var profileDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a profile",
	Long: `Delete a profile directory and its config.toml.

Without --force, prompts for confirmation before deleting.

Examples:
  coi profile delete rust-dev
  coi profile delete old-profile --force`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		p := cfg.GetProfile(name)
		if p == nil {
			return fmt.Errorf("profile '%s' not found", name)
		}
		if p.Source == "(built-in)" {
			return fmt.Errorf("cannot delete built-in profile 'default'")
		}

		profileDir := filepath.Dir(p.Source)
		force, _ := cmd.Flags().GetBool("force")

		if !force {
			fmt.Fprintf(os.Stderr, "This will delete profile '%s' at %s\n", name, profileDir)
			if !confirmAction("Continue?") {
				fmt.Fprintln(os.Stderr, "Aborted.")
				return nil
			}
		}

		if err := os.RemoveAll(profileDir); err != nil {
			return fmt.Errorf("failed to delete profile directory: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Deleted profile '%s' (%s)\n", name, profileDir)
		return nil
	},
}

func init() {
	profileListCmd.Flags().StringVar(&profileFormat, "format", "text", "Output format: text or json")

	profileCreateCmd.Flags().String("image", "", "Set the image alias")
	profileCreateCmd.Flags().String("inherits", "", "Set the parent profile to inherit from")
	profileCreateCmd.Flags().Bool("persistent", false, "Set persistent = true")
	profileCreateCmd.Flags().Bool("user", false, "Force creation in ~/.coi/profiles/")
	profileCreateCmd.Flags().Bool("project", false, "Force creation in ./.coi/profiles/")

	profileDeleteCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")

	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileInfoCmd)
	profileCmd.AddCommand(profileShowCmd)
	profileCmd.AddCommand(profileCreateCmd)
	profileCmd.AddCommand(profileEditCmd)
	profileCmd.AddCommand(profileDeleteCmd)
}
