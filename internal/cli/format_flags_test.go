package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestFormatCommandsHaveJSONAlias enforces the output-format convention: every
// command that exposes `--format` with a text|json choice must also expose the
// `--json` bool alias, so `coi <cmd> --json` works uniformly across the CLI.
// The one intentional exception is `container exec`, whose --format is json|raw
// (not text|json), so a --json alias would be meaningless there.
func TestFormatCommandsHaveJSONAlias(t *testing.T) {
	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		if f := cmd.Flags().Lookup("format"); f != nil {
			isTextJSON := strings.Contains(f.Usage, "text or json")
			hasJSON := cmd.Flags().Lookup("json") != nil
			switch {
			case isTextJSON && !hasJSON:
				t.Errorf("%s: has --format (text|json) but is missing the --json alias", path)
			case !isTextJSON && hasJSON:
				t.Errorf("%s: has --json but its --format is not text|json (%q)", path, f.Usage)
			}
		}
		for _, c := range cmd.Commands() {
			walk(c, path+" "+c.Name())
		}
	}
	walk(rootCmd, "coi")
}

// TestApplyJSONFormatAlias checks the resolver: --json forces "json"; without it
// the --format value is left untouched.
func TestApplyJSONFormatAlias(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
		c.Flags().String("format", "text", "Output format: text or json")
		c.Flags().Bool("json", false, "Alias for --format json")
		return c
	}

	// --json set -> "json" regardless of --format.
	c := newCmd()
	_ = c.ParseFlags([]string{"--json", "--format", "text"})
	format := "text"
	applyJSONFormatAlias(c, &format)
	if format != "json" {
		t.Errorf("--json should force json, got %q", format)
	}

	// --json absent -> --format value unchanged.
	c = newCmd()
	_ = c.ParseFlags([]string{"--format", "text"})
	format = "text"
	applyJSONFormatAlias(c, &format)
	if format != "text" {
		t.Errorf("without --json the format must be unchanged, got %q", format)
	}
}
