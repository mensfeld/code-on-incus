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

	const maxFileSize = 1 << 20 // 1 MiB — guard against malicious/accidental large files

	var patterns []execPattern
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxFileSize {
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
	// Operate on the lowercased form throughout. strings.Index below runs against
	// it and the caller lowercases the result anyway, so indexing and slicing
	// share one string. This avoids a bounds panic (issue #505): the previous
	// code computed `lower` once but reassigned `token` inside the loop, so once
	// an earlier placeholder shortened `token`, a later placeholder's index — a
	// position in the original, longer string — could overrun the shortened
	// token. E.g. "tcp-connect:attacker.com:12345": "attacker.com" (idx 12)
	// shortened token to "tcp-connect" (len 11), then "attacker" (also idx 12 in
	// the stale string) did token[:12] -> "slice bounds out of range [:12] with
	// length 11". Slicing the same string we index into makes idx <= len always.
	token = strings.ToLower(token)
	for _, ph := range []string{"attacker.com", "attacker", "/path/to"} {
		idx := strings.Index(token, ph)
		if idx == 0 {
			return ""
		}
		if idx > 0 {
			token = strings.TrimRight(token[:idx], ":.")
		}
	}
	// Strip pure-numeric path segments (e.g. port numbers), working from the
	// end. A trailing slash is preserved so that "/dev/tcp/" stays intact even
	// after removing a numeric component like the local port in "/inet/tcp/0/".
	parts := strings.Split(token, "/")
	hasTrailingSlash := len(parts) > 0 && parts[len(parts)-1] == ""
	end := len(parts)
	if hasTrailingSlash {
		end-- // exclude the trailing empty segment while stripping
	}
	for end > 0 && gtfobinsIsNumeric(parts[end-1]) {
		end--
	}
	result := strings.Join(parts[:end], "/")
	if hasTrailingSlash {
		result += "/" // re-attach the trailing slash
	}
	return result
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
