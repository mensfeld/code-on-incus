package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultGTFOBinsURL = "https://github.com/GTFOBins/GTFOBins.github.io.git"

var (
	upGtfobinsDir string
	upSource      string
)

var updatePatternsCmd = &cobra.Command{
	Use:   "patterns",
	Short: "Clone or update the GTFOBins reverse-shell database",
	Long: `Clone (or update) the GTFOBins database used by the PROC_EVENTS monitoring daemon.

The database is stored at ~/.coi/gtfobins/ and read directly by the daemon at
startup — no intermediate files are generated. Restart the monitoring daemon
after running this command to pick up new patterns.

On first run the repository is cloned from the configured source URL. On
subsequent runs git pull is used, so the remote URL is remembered automatically.
To switch to a different source (e.g. a fork), delete ~/.coi/gtfobins/ and
re-run with --source pointing at the new URL.

To use your own fork or a customised copy:
  coi update patterns --source https://github.com/you/GTFOBins.github.io.git

Examples:
  coi update patterns                        # clone/update from official GTFOBins
  coi update patterns --dry-run              # show what would be done, don't execute
  coi update patterns --source <url>         # use a custom / forked repository
`,
	RunE: updatePatternsCommand,
}

func init() {
	updatePatternsCmd.Flags().StringVar(&upGtfobinsDir, "gtfobins-dir", "", "Override clone directory (default: ~/.coi/gtfobins)")
	updatePatternsCmd.Flags().StringVar(&upSource, "source", defaultGTFOBinsURL, "Git URL to clone from (used only on first clone)")
	updatePatternsCmd.Flags().Bool("dry-run", false, "Print git command that would run, without executing it")
}

func updatePatternsCommand(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".coi"), 0o755); err != nil {
		return fmt.Errorf("could not create ~/.coi: %w", err)
	}

	cloneDir := upGtfobinsDir
	if cloneDir == "" {
		cloneDir = filepath.Join(home, ".coi", "gtfobins")
	}

	var gitArgs []string
	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil {
		// Existing clone — update in-place. The remote URL is already recorded
		// in .git/config; --source is intentionally ignored after the first clone
		// so the user's chosen source persists across runs.
		gitArgs = []string{"-C", cloneDir, "pull", "--ff-only", "--depth=1"}
		fmt.Printf("Updating GTFOBins database at %s...\n", cloneDir)
	} else {
		// Fresh clone.
		gitArgs = []string{"clone", "--depth=1", upSource, cloneDir}
		fmt.Printf("Cloning GTFOBins database from %s to %s...\n", upSource, cloneDir)
	}

	if dryRun {
		fmt.Printf("[dry-run] git %v\n", gitArgs)
		return nil
	}

	out, err := runGit(gitArgs...)
	if out != "" {
		fmt.Print(out)
	}
	if err != nil {
		return fmt.Errorf("git failed: %w", err)
	}

	fmt.Println("Done. Restart the monitoring daemon to apply new patterns.")
	return nil
}

// runGit runs git with the given arguments and returns combined output.
func runGit(args ...string) (string, error) {
	c := exec.Command("git", args...)
	out, err := c.CombinedOutput()
	return string(out), err
}
