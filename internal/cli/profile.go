package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/spf13/cobra"
)

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
  coi profile list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(cfg.Profiles) == 0 {
			fmt.Fprintln(os.Stderr, "No profiles configured.")
			return nil
		}

		// Sort profile names for consistent output
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tIMAGE\tPERSISTENT\tSOURCE")
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
			source := p.Source
			if source == "" {
				source = "(unknown)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, image, persistent, source)
		}
		w.Flush()
		return nil
	},
}

// profileShowCmd shows details of a specific profile
var profileShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show profile details",
	Long: `Display the full configuration of a named profile.

Examples:
  coi profile show rust-dev`,
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

		if p.Image != "" {
			fmt.Printf("image = %q\n", p.Image)
		}
		if p.Context != "" {
			fmt.Printf("context = %q\n", p.Context)
		}
		if p.Persistent != nil {
			fmt.Printf("persistent = %v\n", *p.Persistent)
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

func init() {
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileShowCmd)
}
