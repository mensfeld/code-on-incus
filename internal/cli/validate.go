package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	coischema "github.com/mensfeld/code-on-incus/schema"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate COI configuration files",
	Long: `Validate COI configuration files and report any errors as JSON.

Exit code 0 means the file is valid; exit code 1 means it is not.
Output is always a JSON object so it can be consumed by scripts and web UIs
without importing a JSON Schema library.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

type validateResult struct {
	Valid  bool                        `json:"valid"`
	Errors []coischema.ValidationIssue `json:"errors"`
}

func writeValidateResult(r validateResult) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
}

var validateProfileCmd = &cobra.Command{
	Use:   "profile <path>",
	Short: "Validate a profile config.toml against the COI JSON Schema",
	Long: `Validate a profile config.toml against the COI JSON Schema.

Always outputs a JSON object with a "valid" boolean and an "errors" array.
Each error has a "path" (JSON Pointer to the offending field) and a "message".

Exit code 0 = valid, 1 = invalid.

Examples:
  # Check whether a profile is valid
  coi validate profile ~/.coi/profiles/myproject/config.toml

  # Use from a shell script
  if coi validate profile config.toml | jq -e '.valid'; then
    echo "ok"
  fi

  # Collect error messages in Ruby
  result = JSON.parse(` + "`" + `coi validate profile #{path}` + "`" + `)
  result["errors"].each { |e| puts "#{e["path"]}: #{e["message"]}" }`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]

		var rawMap map[string]any
		if _, err := toml.DecodeFile(path, &rawMap); err != nil {
			writeValidateResult(validateResult{
				Valid:  false,
				Errors: []coischema.ValidationIssue{{Path: "", Message: fmt.Sprintf("failed to parse TOML: %s", err)}},
			})
			os.Exit(1)
		}

		schemaErr := coischema.ValidateProfileMap(rawMap)
		if schemaErr == nil {
			writeValidateResult(validateResult{Valid: true, Errors: []coischema.ValidationIssue{}})
			return nil
		}

		writeValidateResult(validateResult{
			Valid:  false,
			Errors: coischema.ExtractErrors(schemaErr),
		})
		os.Exit(1)
		return nil
	},
}

func init() {
	validateCmd.AddCommand(validateProfileCmd)
}
