package sinkguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guardedPackages run (in whole or in part) while a `coi shell` session is
// interactively attached — the monitoring daemons, the runtime-limit watchdog,
// and the network background IP refresher all execute in the coi process whose
// os.Stdout/os.Stderr are, at that point, the user's tmux terminal. They must
// therefore never write to a terminal sink; diagnostics go to the file-based
// SessionLogger (directly, via a callback, or via a package logger that routes
// to it). This is the regression guard for the issue #372 class: it fails CI if
// any such write is (re)introduced.
var guardedPackages = []string{
	"internal/monitor",
	"internal/nftmonitor",
	"internal/limits",
	"internal/network",
}

// forbidden tokens denote terminal sinks. "os.Stdout"/"os.Stderr" catch
// fmt.Fprint*(os.Stderr, …) and direct references while leaving buffer writes
// such as fmt.Fprintf(&sb, …) untouched. The fmt.Print*/log.* entries catch the
// global stdout/stderr helpers. (The print/println builtins are intentionally
// omitted: "print(" is a substring of the legitimate "fmt.Fprint(".)
var forbidden = []string{
	"os.Stdout",
	"os.Stderr",
	"fmt.Print(",
	"fmt.Printf(",
	"fmt.Println(",
	"log.Print(",
	"log.Printf(",
	"log.Println(",
	"log.Fatal",
	"log.Panic",
}

// allowMarker exempts a single source line. Use it ONLY for the documented
// stderr/standard-logger fallbacks that fire when no session logger is set
// (pre-attach / non-session CLI callers), never to silence a real leak.
const allowMarker = "terminal-sink-ok:"

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod)")
		}
		dir = parent
	}
}

// stripComment removes a line-comment so a forbidden token mentioned only in a
// comment (e.g. a doc comment referencing os.Stderr) is not flagged.
func stripComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

func TestSessionRuntimePackagesDoNotWriteToTerminal(t *testing.T) {
	root := repoRoot(t)
	var violations []string

	for _, pkg := range guardedPackages {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", pkg, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for i, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, allowMarker) {
					continue
				}
				code := stripComment(line)
				for _, tok := range forbidden {
					if strings.Contains(code, tok) {
						violations = append(violations, formatViolation(pkg, name, i+1, tok, line))
						break
					}
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("terminal-sink writes found in session-runtime packages (issue #372 class).\n"+
			"Route diagnostics to the SessionLogger instead; only the documented no-session "+
			"fallbacks may carry a %q comment.\n\n%s",
			allowMarker, strings.Join(violations, "\n"))
	}
}

func formatViolation(pkg, file string, line int, tok, src string) string {
	return pkg + "/" + file + ":" + itoa(line) + ": " + tok + "  ->  " + strings.TrimSpace(src)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
