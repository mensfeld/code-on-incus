package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// gtfobinsDir is the subdirectory within the clone that contains per-binary YAML files.
const gtfobinsBinDir = "_gtfobins"

// genericTokens are tokens discarded by the keyword derivation heuristic.
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

// gtfoBinFile is the on-disk YAML schema for a single GTFOBins entry.
type gtfoBinFile struct {
	Alias     string                   `yaml:"alias"`
	Functions map[string][]gtfoBinFunc `yaml:"functions"`
}

type gtfoBinFunc struct {
	Code string `yaml:"code"`
}

// loadExecPatternsFromGTFOBins walks cloneDir/_gtfobins/, parses every YAML file,
// and returns an execPattern for each binary that has a reverse-shell function and
// at least one distinctive keyword. Files with an alias field or no reverse-shell
// entries are skipped.
func loadExecPatternsFromGTFOBins(cloneDir string) []execPattern {
	binDir := filepath.Join(cloneDir, gtfobinsBinDir)
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return nil
	}

	var patterns []execPattern
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
			continue
		}
		rsFuncs, ok := gf.Functions["reverse-shell"]
		if !ok || len(rsFuncs) == 0 {
			continue
		}

		kws := deriveKeywords(binaryName, rsFuncs)
		if len(kws) == 0 {
			// No discriminating keywords found — omit to avoid matching all
			// invocations of this binary. Compiled-in defaults cover the gaps.
			continue
		}
		patterns = append(patterns, execPattern{
			Name:     binaryName + "-reverse-shell",
			Arg0:     binaryName,
			Keywords: kws,
		})
	}
	return patterns
}

// deriveKeywords extracts up to 2 distinctive tokens from GTFOBins reverse-shell
// code examples for the given binary. Returns nil if no distinctive keywords
// could be determined.
func deriveKeywords(binaryName string, funcs []gtfoBinFunc) []string {
	candidates := map[string]int{} // token → frequency across examples

	for _, f := range funcs {
		seen := map[string]bool{}
		for _, raw := range gtfobinsTokenise(f.Code) {
			token := strings.ToLower(gtfobinsStripPlaceholders(raw))
			if token == "" || !gtfobinsIsDistinctive(token, binaryName) {
				continue
			}
			if !seen[token] {
				candidates[token]++
				seen[token] = true
			}
		}
	}

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

// gtfobinsStripPlaceholders removes GTFOBins example placeholders from a token,
// returning the structural prefix that is actually distinctive. For example:
//
//	"/dev/tcp/attacker.com/12345" → "/dev/tcp/"
//	"attacker.com"                → ""
func gtfobinsStripPlaceholders(token string) string {
	lower := strings.ToLower(token)
	for _, ph := range []string{"attacker.com", "attacker", "/path/to"} {
		if idx := strings.Index(lower, ph); idx > 0 {
			token = strings.TrimRight(token[:idx], ":.")
		} else if idx == 0 {
			return ""
		}
	}
	// Strip pure-numeric path suffixes that look like ports.
	parts := strings.Split(token, "/")
	for len(parts) > 0 {
		last := parts[len(parts)-1]
		if last == "" || gtfobinsIsNumeric(last) {
			parts = parts[:len(parts)-1]
		} else {
			break
		}
	}
	return strings.Join(parts, "/")
}

// gtfobinsTokenise splits a GTFOBins code block into tokens on whitespace and
// common shell/code delimiters.
func gtfobinsTokenise(code string) []string {
	var tokens []string
	current := strings.Builder{}
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

// gtfobinsIsDistinctive returns true if the token is useful as a pattern keyword.
func gtfobinsIsDistinctive(token, binaryName string) bool {
	if len(token) < 2 {
		return false
	}
	if token == strings.ToLower(binaryName) {
		return false
	}
	if genericTokens[token] {
		return false
	}
	if strings.Contains(token, "attacker") || strings.Contains(token, "/path/to") ||
		strings.Contains(token, "0.0.0.0") {
		return false
	}
	if gtfobinsIsNumeric(token) {
		return false
	}
	// Skip single-letter flags (-i, -v) and single-char shell variables ($i, $p).
	if len(token) == 2 && (token[0] == '-' || token[0] == '$') {
		return false
	}
	// Skip glob-like tokens starting with *.
	if token[0] == '*' {
		return false
	}
	return true
}

func gtfobinsIsNumeric(s string) bool {
	for _, ch := range s {
		if !unicode.IsDigit(ch) {
			return false
		}
	}
	return s != ""
}
