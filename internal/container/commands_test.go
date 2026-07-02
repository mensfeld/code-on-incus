package container

import (
	"os"
	"testing"
)

// Under `go test` stdin is not a terminal (pipe or /dev/null), so the
// pass-through branch is exercised directly; the TTY branch is its logical
// complement (StdinIsTerminal() == true → nil).
func TestStreamedStdin_NonTerminalPassesThrough(t *testing.T) {
	if StdinIsTerminal() {
		t.Skip("test environment has a TTY stdin; cannot exercise the pipe branch")
	}
	if got := streamedStdin(); got != os.Stdin {
		t.Errorf("non-terminal stdin must be attached for piped input, got %v", got)
	}
}

// The invariant the streamed exec relies on: a TTY stdin must NOT be attached
// (nil keeps incus exec non-interactive: no PTY raw mode, Ctrl+C still
// signals coi, stdin-reading commands get EOF). This test pins the mapping
// between the two functions rather than the environment.
func TestStreamedStdin_TerminalMapsToNil(t *testing.T) {
	if StdinIsTerminal() {
		if got := streamedStdin(); got != nil {
			t.Errorf("terminal stdin must not be attached (would enter PTY mode), got %v", got)
		}
	} else {
		if got := streamedStdin(); got == nil {
			t.Error("non-terminal stdin should be attached")
		}
	}
}
