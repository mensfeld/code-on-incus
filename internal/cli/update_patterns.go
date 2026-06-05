package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const gtfobinsURL = "https://github.com/GTFOBins/GTFOBins.github.io.git"

var (
	upGtfobinsDir string
	upOut         string
	upDryRun      bool
)

var updatePatternsCmd = &cobra.Command{
	Use:   "update-patterns",
	Short: "Refresh exec-pattern database from GTFOBins",
	Long: `Clone (or update) the GTFOBins database and regenerate ~/.coi/exec-patterns.toml.

The patterns file is read by the PROC_EVENTS monitoring daemon at startup, so
running this command is all that is needed to pick up new reverse-shell
techniques without installing a new version of coi.

The GTFOBins repository is cloned to ~/.coi/gtfobins/ on first run and
updated with git pull on subsequent runs. Pattern keywords are derived
automatically from GTFOBins reverse-shell code examples using a heuristic;
use --dry-run to review the output before writing.

Examples:
  coi update-patterns              # update DB and write ~/.coi/exec-patterns.toml
  coi update-patterns --dry-run    # print patterns to stdout without writing
`,
	RunE: updatePatternsCommand,
}

func init() {
	updatePatternsCmd.Flags().StringVar(&upGtfobinsDir, "gtfobins-dir", "", "Override GTFOBins clone directory (default: ~/.coi/gtfobins)")
	updatePatternsCmd.Flags().StringVar(&upOut, "out", "", "Override output file (default: ~/.coi/exec-patterns.toml)")
	updatePatternsCmd.Flags().BoolVar(&upDryRun, "dry-run", false, "Print patterns to stdout instead of writing to file")
}

// gtfoBinFile is the on-disk YAML schema for a single GTFOBins entry.
type gtfoBinFile struct {
	Alias     string                   `yaml:"alias"`
	Functions map[string][]gtfoBinFunc `yaml:"functions"`
}

type gtfoBinFunc struct {
	Code string `yaml:"code"`
}

// genericTokens are tokens discarded by the keyword heuristic.
var genericTokens = map[string]bool{
	"sh": true, "bash": true, "bin": true, "exec": true, "tcp": true,
	"udp": true, "stdin": true, "stdout": true, "stderr": true,
	"true": true, "false": true, "while": true, "do": true, "done": true,
	"if": true, "fi": true, "then": true, "else": true, "for": true,
	"in": true, "end": true, "cat": true, "echo": true, "printf": true,
	"read": true, "local": true, "exit": true, "kill": true, "wait": true,
	"sleep": true, "set": true, "unset": true, "export": true, "source": true,
	"return": true, "break": true, "continue": true, "function": true,
}

func updatePatternsCommand(_ *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	coiDir := filepath.Join(home, ".coi")
	if err := os.MkdirAll(coiDir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", coiDir, err)
	}

	gtfobinsDir := upGtfobinsDir
	if gtfobinsDir == "" {
		gtfobinsDir = filepath.Join(coiDir, "gtfobins")
	}
	outFile := upOut
	if outFile == "" {
		outFile = filepath.Join(coiDir, "exec-patterns.toml")
	}

	// Clone or update the GTFOBins repository.
	if _, err := os.Stat(filepath.Join(gtfobinsDir, ".git")); err == nil {
		fmt.Printf("Updating GTFOBins database at %s...\n", gtfobinsDir)
		if out, err := runGit("-C", gtfobinsDir, "pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull failed: %w\n%s", err, out)
		}
	} else {
		fmt.Printf("Cloning GTFOBins database to %s...\n", gtfobinsDir)
		if out, err := runGit("clone", "--depth=1", gtfobinsURL, gtfobinsDir); err != nil {
			return fmt.Errorf("git clone failed: %w\n%s", err, out)
		}
	}

	// Walk _gtfobins/ and derive patterns.
	binDir := filepath.Join(gtfobinsDir, "_gtfobins")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", binDir, err)
	}

	type pattern struct {
		Name     string   `toml:"name"`
		Arg0     string   `toml:"arg0"`
		Keywords []string `toml:"keywords"`
	}
	type patternFile struct {
		Patterns []pattern `toml:"exec_pattern"`
	}

	var patterns []pattern
	skipped := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		binaryName := entry.Name()
		data, err := os.ReadFile(filepath.Join(binDir, binaryName))
		if err != nil {
			continue
		}

		var gf gtfoBinFile
		if err := yaml.Unmarshal(data, &gf); err != nil {
			continue
		}
		if gf.Alias != "" {
			skipped++
			continue
		}
		rsFuncs, ok := gf.Functions["reverse-shell"]
		if !ok || len(rsFuncs) == 0 {
			continue
		}

		kws := deriveKeywords(binaryName, rsFuncs)
		if len(kws) == 0 {
			// No distinctive keywords could be extracted — skip to avoid matching
			// every invocation of this binary. The binary is listed in the
			// "needs manual review" summary at the end.
			fmt.Printf("  [skip] %s: no distinctive keywords extracted — add manually\n", binaryName)
			continue
		}
		patterns = append(patterns, pattern{
			Name:     binaryName + "-reverse-shell",
			Arg0:     binaryName,
			Keywords: kws,
		})
	}

	fmt.Printf("Found %d binaries with reverse-shell capability (%d aliases skipped).\n",
		len(patterns), skipped)

	pf := patternFile{Patterns: patterns}

	var buf bytes.Buffer
	buf.WriteString("# ~/.coi/exec-patterns.toml\n")
	buf.WriteString("# Managed by: coi update-patterns\n")
	buf.WriteString("# Review keyword arrays and adjust as needed before relying on this file.\n\n")

	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(pf); err != nil {
		return fmt.Errorf("failed to encode TOML: %w", err)
	}

	if upDryRun {
		fmt.Print(buf.String())
		return nil
	}

	if err := os.WriteFile(outFile, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", outFile, err)
	}
	fmt.Printf("Written to %s\n", outFile)
	fmt.Println("Restart the monitoring daemon to pick up the new patterns.")
	return nil
}

// deriveKeywords extracts up to 2 distinctive tokens from GTFOBins reverse-shell
// code examples for the given binary. Returns nil if no distinctive keywords
// could be determined (caller should not add the pattern to the TOML).
func deriveKeywords(binaryName string, funcs []gtfoBinFunc) []string {
	candidates := map[string]int{} // token → frequency across examples

	for _, f := range funcs {
		seen := map[string]bool{}
		for _, raw := range tokenise(f.Code) {
			token := strings.ToLower(stripPlaceholders(raw))
			if token == "" || !isDistinctive(token, binaryName) {
				continue
			}
			if !seen[token] {
				candidates[token]++
				seen[token] = true
			}
		}
	}

	// Pick the most-frequent candidates (up to 2).
	type kv struct {
		k string
		v int
	}
	var sorted []kv
	for k, v := range candidates {
		sorted = append(sorted, kv{k, v})
	}
	// Sort descending by frequency, then alphabetically for stability.
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].v > sorted[i].v || (sorted[j].v == sorted[i].v && sorted[j].k < sorted[i].k) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	const maxKeywords = 2
	var kws []string
	for i := 0; i < len(sorted) && i < maxKeywords; i++ {
		kws = append(kws, sorted[i].k)
	}
	return kws
}

// stripPlaceholders removes GTFOBins example placeholders from a token, returning
// the structural prefix that is actually distinctive. For example:
//
//	"/dev/tcp/attacker.com/12345" → "/dev/tcp/"
//	"attacker.com"                → ""
func stripPlaceholders(token string) string {
	// Strip the attacker placeholder and everything after it.
	for _, ph := range []string{"attacker.com", "attacker", "/path/to"} {
		if idx := strings.Index(strings.ToLower(token), ph); idx > 0 {
			token = strings.TrimRight(token[:idx], ":.")
		} else if idx == 0 {
			return ""
		}
	}
	// Strip pure-numeric suffixes that look like ports (e.g. "/12345" at end).
	parts := strings.Split(token, "/")
	for len(parts) > 0 {
		last := parts[len(parts)-1]
		if last == "" || isNumericStr(last) {
			parts = parts[:len(parts)-1]
		} else {
			break
		}
	}
	token = strings.Join(parts, "/")
	return token
}

// tokenise splits a GTFOBins code block into tokens on whitespace and common
// shell/code delimiters, returning non-empty tokens.
func tokenise(code string) []string {
	var tokens []string
	current := strings.Builder{}

	// Include comma and equals so function arguments and assignments are split.
	delimiters := ";|&><(){}[]'\"`\t\r\n,="
	for _, ch := range code {
		if ch == ' ' || strings.ContainsRune(delimiters, ch) {
			if s := current.String(); s != "" {
				tokens = append(tokens, s)
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if s := current.String(); s != "" {
		tokens = append(tokens, s)
	}
	return tokens
}

// isDistinctive returns true if the token is interesting as a pattern keyword.
func isDistinctive(token, binaryName string) bool {
	// Skip very short tokens.
	if len(token) < 2 {
		return false
	}
	// Skip the binary name itself.
	if token == strings.ToLower(binaryName) {
		return false
	}
	// Skip generic shell vocabulary.
	if genericTokens[token] {
		return false
	}
	// Skip remaining placeholder patterns.
	if strings.Contains(token, "attacker") || strings.Contains(token, "/path/to") ||
		strings.Contains(token, "0.0.0.0") {
		return false
	}
	// Skip pure-numeric tokens (ports, counts).
	if isNumericStr(token) {
		return false
	}
	// Skip tokens that look like generic single-letter flags (-i, -v, -n).
	if len(token) == 2 && token[0] == '-' {
		return false
	}
	// Skip Perl/shell single-char variables ($i, $p, $c, $s, $r, $x, etc.).
	if len(token) == 2 && token[0] == '$' {
		return false
	}
	// Skip glob-like tokens starting with * or $ that aren't otherwise useful.
	if token[0] == '*' {
		return false
	}
	return true
}

func isNumericStr(s string) bool {
	for _, ch := range s {
		if !unicode.IsDigit(ch) {
			return false
		}
	}
	return s != ""
}

// runGit runs git with the given arguments and returns combined output.
func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
