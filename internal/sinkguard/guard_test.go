package sinkguard

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
// global stdout/stderr helpers. The print/println builtins (which also write to
// stderr) are matched separately by builtinSinkRe below, since a bare "print("
// substring would false-match the legitimate "fmt.Fprint(".
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

// builtinSinkRe matches the print/println builtins (which write to stderr) as
// standalone calls. The leading (^|[^\w.]) avoids false-matching fmt.Fprint( /
// strings.Sprint( (preceded by a letter) and a foo.print( method call (preceded
// by a dot).
var builtinSinkRe = regexp.MustCompile(`(^|[^\w.])print(ln)?\(`)

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
				flagged := false
				for _, tok := range forbidden {
					if strings.Contains(code, tok) {
						violations = append(violations, formatViolation(pkg, name, i+1, tok, line))
						flagged = true
						break
					}
				}
				if !flagged && builtinSinkRe.MatchString(code) {
					violations = append(violations, formatViolation(pkg, name, i+1, "print/println builtin", line))
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
	return pkg + "/" + file + ":" + strconv.Itoa(line) + ": " + tok + "  ->  " + strings.TrimSpace(src)
}

// TestBuiltinSinkRegex locks the print/println matcher: it must catch the
// builtins as standalone calls but not the legitimate fmt.Fprint*/Sprint* family
// or a .print( method call.
func TestBuiltinSinkRegex(t *testing.T) {
	shouldMatch := []string{
		`	println("leak")`,
		`	print("leak")`,
		`println(x)`,
		`	if true { println(y) }`,
	}
	shouldNotMatch := []string{
		`	fmt.Fprint(os.Stdout, "ok")`, // os.Stdout caught elsewhere; regex itself must not match
		`	fmt.Fprintln(&sb, "ok")`,     // buffer write
		`	s := strings.Sprint("ok")`,   // Sprint
		`	w.Fprintf("ok")`,             // Fprintf method-ish
		`	foo.print("method, not builtin")`,
	}
	for _, s := range shouldMatch {
		if !builtinSinkRe.MatchString(stripComment(s)) {
			t.Errorf("expected builtin match for: %q", s)
		}
	}
	for _, s := range shouldNotMatch {
		if builtinSinkRe.MatchString(stripComment(s)) {
			t.Errorf("unexpected builtin match for: %q", s)
		}
	}
}
