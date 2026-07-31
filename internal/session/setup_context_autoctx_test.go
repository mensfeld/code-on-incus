package session

import (
	"os"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// fakeAutoCtxManager is an in-memory ContainerManager that simulates just enough
// of a container filesystem for injectAutoContextFile: `mkdir -p`, the
// `test -f … && echo exists || echo missing` existence probe, CreateFile,
// PushFile (staging a host temp file), a plain `cat <path>` read, and a
// `cat <staging> >> <dest>` append. Every other ContainerManager method is
// inherited from the embedded (nil) interface and is never called here, so a
// call to one would panic loudly rather than pass silently.
//
// The simulated filesystem persists across calls (map keyed by path), which is
// exactly the condition that makes #674 reproducible: a persistent container's
// ~/.claude/CLAUDE.md survives from one session to the next.
type fakeAutoCtxManager struct {
	container.ContainerManager
	files map[string]string
}

func newFakeAutoCtxManager() *fakeAutoCtxManager {
	return &fakeAutoCtxManager{files: map[string]string{}}
}

func (f *fakeAutoCtxManager) ExecCommand(cmd string, _ container.ExecCommandOptions) (string, error) {
	fields := strings.Fields(cmd)
	switch {
	case strings.HasPrefix(cmd, "mkdir -p"):
		return "", nil
	case strings.HasPrefix(cmd, "test -f "):
		// test -f <path> && echo exists || echo missing
		if _, ok := f.files[fields[2]]; ok {
			return "exists\n", nil
		}
		return "missing\n", nil
	case strings.HasPrefix(cmd, "cat ") && strings.Contains(cmd, ">>"):
		// cat <staging> >> <dest> && rm -f <staging>
		staging, dest := fields[1], fields[3]
		f.files[dest] += f.files[staging]
		delete(f.files, staging)
		return "", nil
	case strings.HasPrefix(cmd, "cat "):
		// plain read: cat <path>
		return f.files[fields[1]], nil
	default:
		return "", nil
	}
}

func (f *fakeAutoCtxManager) CreateFile(path, content string) error {
	f.files[path] = content
	return nil
}

func (f *fakeAutoCtxManager) PushFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f.files[dest] = string(data)
	return nil
}

func (f *fakeAutoCtxManager) Chown(_ string, _, _ int) error { return nil }

// fakeAutoCtxTool implements tool.ToolWithAutoContextFile with Claude's
// auto-context path. Only AutoContextFile is exercised by injectAutoContextFile;
// the embedded (nil) Tool interface covers the rest of the method set.
type fakeAutoCtxTool struct {
	tool.ToolWithAutoContextFile
}

func (fakeAutoCtxTool) AutoContextFile() string { return ".claude/CLAUDE.md" }

// TestInjectAutoContextFile_DoesNotAccumulateAcrossSessions reproduces #674:
// on a persistent container reused across sessions, the COI sandbox context block
// (headed "# COI Sandbox Environment") is appended to ~/.claude/CLAUDE.md on every
// session instead of replacing the previous copy, so the file grows without bound
// (the reporter observed 16 copies / 108k chars, over Claude Code's 40k limit).
//
// Because "# COI Sandbox Environment" appears exactly once per rendered block, the
// number of occurrences equals the number of copies. Driving injectAutoContextFile
// across several sessions against one persistent home must leave exactly one copy.
func TestInjectAutoContextFile_DoesNotAccumulateAcrossSessions(t *testing.T) {
	mgr := newFakeAutoCtxManager()
	acf := fakeAutoCtxTool{}
	homeDir := "/home/code"
	logger := func(string) {}

	content := tool.RenderContextFileContent(tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
		NetworkMode:   "restricted",
	})
	if !strings.Contains(content, "# COI Sandbox Environment") {
		t.Fatalf("precondition: rendered sandbox context should contain the header marker")
	}

	const sessions = 3
	for i := 0; i < sessions; i++ {
		if err := injectAutoContextFile(mgr, acf, content, homeDir, logger); err != nil {
			t.Fatalf("session %d: injectAutoContextFile failed: %v", i+1, err)
		}
	}

	claudeMD := mgr.files["/home/code/.claude/CLAUDE.md"]
	copies := strings.Count(claudeMD, "# COI Sandbox Environment")
	if copies != 1 {
		t.Errorf("#674: the COI sandbox context block must appear exactly once in "+
			"~/.claude/CLAUDE.md after %d sessions, but found %d copies (%d chars) — it is being "+
			"appended on every session instead of replaced, so the file grows without bound.",
			sessions, copies, len(claudeMD))
	}
}

// TestInjectAutoContextFile_PreservesHostContent verifies that when the tool's
// auto-context file already contains user/host content (e.g. a CLAUDE.md copied
// from the host), injecting the sandbox context preserves that content and still
// keeps exactly one managed COI block across repeated sessions.
func TestInjectAutoContextFile_PreservesHostContent(t *testing.T) {
	mgr := newFakeAutoCtxManager()
	acf := fakeAutoCtxTool{}
	homeDir := "/home/code"
	destPath := "/home/code/.claude/CLAUDE.md"
	logger := func(string) {}

	const userMarker = "MY-PROJECT-INSTRUCTIONS-DO-NOT-DROP"
	mgr.files[destPath] = "# My project rules\n\n" + userMarker + "\n"

	content := tool.RenderContextFileContent(tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
	})

	for i := 0; i < 3; i++ {
		if err := injectAutoContextFile(mgr, acf, content, homeDir, logger); err != nil {
			t.Fatalf("session %d: injectAutoContextFile failed: %v", i+1, err)
		}
	}

	got := mgr.files[destPath]
	if n := strings.Count(got, userMarker); n != 1 {
		t.Errorf("host/user content must be preserved exactly once, found %d occurrences of %q", n, userMarker)
	}
	if n := strings.Count(got, "# COI Sandbox Environment"); n != 1 {
		t.Errorf("expected exactly one COI sandbox block alongside preserved content, found %d", n)
	}
}

// TestInjectAutoContextFile_HealsLegacyDuplicates covers the migration path for
// #674: a file already bloated by the pre-fix code (many unmarked copies joined
// by the old "# COI Sandbox Context" separator) must collapse to a single copy
// once the fixed injection runs, and stay there.
func TestInjectAutoContextFile_HealsLegacyDuplicates(t *testing.T) {
	mgr := newFakeAutoCtxManager()
	acf := fakeAutoCtxTool{}
	homeDir := "/home/code"
	destPath := "/home/code/.claude/CLAUDE.md"
	logger := func(string) {}

	content := tool.RenderContextFileContent(tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
		NetworkMode:   "restricted",
	})
	// Simulate the old (pre-#674) accumulation: three unmarked copies joined by
	// the legacy separator line.
	oldSep := "\n\n# COI Sandbox Context\n\n"
	mgr.files[destPath] = content + oldSep + content + oldSep + content

	for i := 0; i < 3; i++ {
		if err := injectAutoContextFile(mgr, acf, content, homeDir, logger); err != nil {
			t.Fatalf("session %d: injectAutoContextFile failed: %v", i+1, err)
		}
	}

	got := mgr.files[destPath]
	if n := strings.Count(got, "# COI Sandbox Environment"); n != 1 {
		t.Errorf("legacy duplicates must be healed to exactly one copy, found %d (%d chars)", n, len(got))
	}
}

// TestInjectAutoContextFile_HealsLegacyPreservingHostContent verifies the legacy
// cleanup keeps genuine user/host content that preceded the old coi copies.
func TestInjectAutoContextFile_HealsLegacyPreservingHostContent(t *testing.T) {
	mgr := newFakeAutoCtxManager()
	acf := fakeAutoCtxTool{}
	homeDir := "/home/code"
	destPath := "/home/code/.claude/CLAUDE.md"
	logger := func(string) {}

	content := tool.RenderContextFileContent(tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
	})
	const userMarker = "MY-PROJECT-RULES-KEEP-ME"
	host := "# My project rules\n\n" + userMarker + "\n"
	mgr.files[destPath] = host + "\n\n# COI Sandbox Context\n\n" + content + "\n\n# COI Sandbox Context\n\n" + content

	if err := injectAutoContextFile(mgr, acf, content, homeDir, logger); err != nil {
		t.Fatalf("injectAutoContextFile failed: %v", err)
	}

	got := mgr.files[destPath]
	if n := strings.Count(got, userMarker); n != 1 {
		t.Errorf("host content must be preserved exactly once, found %d occurrences of %q", n, userMarker)
	}
	if n := strings.Count(got, "# COI Sandbox Environment"); n != 1 {
		t.Errorf("legacy coi copies must be healed to one block, found %d", n)
	}
}

// TestInjectAutoContextFile_ContentContainingMarkerStaysIdempotent covers finding
// #1: when the injected content (e.g. a user-provided context_file) merely
// mentions the end-marker text inline — as a substring, not a standalone line —
// line-anchored matching must not treat it as a delimiter, so the block stays a
// single copy across sessions rather than being mis-parsed and duplicated.
func TestInjectAutoContextFile_ContentContainingMarkerStaysIdempotent(t *testing.T) {
	mgr := newFakeAutoCtxManager()
	acf := fakeAutoCtxTool{}
	homeDir := "/home/code"
	destPath := "/home/code/.claude/CLAUDE.md"
	logger := func(string) {}

	const sentinel = "CUSTOM-CONTEXT-SENTINEL"
	content := "# COI Sandbox Environment\n" + sentinel +
		"\nsee the " + autoCtxEndMarker + " marker inline\n"

	for i := 0; i < 3; i++ {
		if err := injectAutoContextFile(mgr, acf, content, homeDir, logger); err != nil {
			t.Fatalf("session %d: injectAutoContextFile failed: %v", i+1, err)
		}
	}

	got := mgr.files[destPath]
	if n := strings.Count(got, sentinel); n != 1 {
		t.Errorf("content mentioning the end marker inline must remain a single block, found %d copies (%d chars)", n, len(got))
	}
}
