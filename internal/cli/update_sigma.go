package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultSigmaURL = "https://github.com/SigmaHQ/sigma.git"

var (
	upSigmaDir    string
	upSigmaSource string
)

var updateSigmaCmd = &cobra.Command{
	Use:   "sigma",
	Short: "Clone or update the Sigma linux/process_creation rule database",
	Long: `Clone (or update) the Sigma rule database used by the PROC_EVENTS monitoring daemon.

Only the rules/linux/process_creation/ subtree is fetched via a sparse, blobless
clone (~300 KB), keeping the download small despite the repo's large total size.
The rules are stored at ~/.coi/sigma/ and read at daemon startup alongside
GTFOBins patterns.  Restart the monitoring daemon after running this command to
pick up new rules.

On first run the repository is cloned from the configured source URL using a
sparse checkout so only rules/linux/process_creation/ is downloaded.  On
subsequent runs git pull updates the existing clone.

To use a custom fork or mirror:
  coi update sigma --source https://github.com/you/sigma.git

Examples:
  coi update sigma                        # clone/update from official SigmaHQ
  coi update sigma --dry-run              # show what would be done, don't execute
  coi update sigma --source <url>         # use a custom / forked repository
`,
	RunE: updateSigmaCommand,
}

func init() {
	updateSigmaCmd.Flags().StringVar(&upSigmaDir, "sigma-dir", "", "Override clone directory (default: ~/.coi/sigma)")
	updateSigmaCmd.Flags().StringVar(&upSigmaSource, "source", defaultSigmaURL, "Git URL to clone from (used only on first clone)")
	updateSigmaCmd.Flags().Bool("dry-run", false, "Print git commands that would run, without executing them")
}

func updateSigmaCommand(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".coi"), 0o755); err != nil {
		return fmt.Errorf("could not create ~/.coi: %w", err)
	}

	cloneDir := upSigmaDir
	if cloneDir == "" {
		cloneDir = filepath.Join(home, ".coi", "sigma")
	}

	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil {
		// Existing clone — pull latest changes.
		gitArgs := []string{"-C", cloneDir, "pull", "--ff-only", "--depth=1"}
		fmt.Printf("Updating Sigma database at %s...\n", cloneDir)
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
	} else if _, err := os.Stat(cloneDir); err == nil {
		// Directory exists but is not a git repo — refuse to clobber it.
		return fmt.Errorf(
			"%s exists but is not a git repository; remove it and re-run to clone fresh",
			cloneDir,
		)
	} else {
		// Fresh sparse + blobless clone — only fetches rules/linux/process_creation/.
		cloneArgs := []string{"clone", "--depth=1", "--filter=blob:none", "--sparse", upSigmaSource, cloneDir}
		sparseArgs := []string{"-C", cloneDir, "sparse-checkout", "set", "rules/linux/process_creation"}
		fmt.Printf("Cloning Sigma database from %s to %s...\n", upSigmaSource, cloneDir)
		if dryRun {
			fmt.Printf("[dry-run] git %v\n", cloneArgs)
			fmt.Printf("[dry-run] git %v\n", sparseArgs)
			return nil
		}
		out, err := runGit(cloneArgs...)
		if out != "" {
			fmt.Print(out)
		}
		if err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
		out, err = runGit(sparseArgs...)
		if out != "" {
			fmt.Print(out)
		}
		if err != nil {
			return fmt.Errorf("git sparse-checkout failed: %w", err)
		}
	}

	fmt.Println("Done. Restart the monitoring daemon to apply new rules.")
	return nil
}
