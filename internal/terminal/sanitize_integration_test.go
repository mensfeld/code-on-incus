package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSanitizeTerm_Integration verifies that exotic terminal types can actually
// be used with tmux after sanitization. This is a real integration test that
// requires tmux to be installed.
func TestSanitizeTerm_Integration(t *testing.T) {
	// Skip if tmux is not available
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found, skipping integration test")
	}

	exoticTerminals := []string{
		"xterm-ghostty",
		"wezterm",
		"alacritty",
		"kitty",
	}

	for _, exoticTerm := range exoticTerminals {
		t.Run(exoticTerm, func(t *testing.T) {
			// Sanitize the exotic terminal type
			sanitized := SanitizeTerm(exoticTerm)
			if sanitized != "xterm-256color" {
				t.Errorf("SanitizeTerm(%q) = %q, expected xterm-256color", exoticTerm, sanitized)
				return
			}

			// Generate unique session name for this test
			sessionName := fmt.Sprintf("coi-test-%s-%d", strings.ReplaceAll(exoticTerm, "-", ""), os.Getpid())

			// Clean up any existing session
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()

			// Try to create a tmux session with the sanitized TERM
			// This mimics what coi shell does internally
			cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep", "1")
			cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", sanitized))

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("Failed to create tmux session with sanitized TERM=%s (from %s): %v\nOutput: %s",
					sanitized, exoticTerm, err, string(output))
				return
			}

			// Verify the session was created successfully
			checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
			if err := checkCmd.Run(); err != nil {
				t.Errorf("tmux session %s was not created successfully", sessionName)
			}

			t.Logf("Successfully created tmux session with TERM=%s (sanitized from %s)", sanitized, exoticTerm)
		})
	}
}

// TestSanitizeTerm_DirectUseFailure demonstrates that exotic terminals fail
// without sanitization. This test is expected to fail when using the exotic
// TERM directly, proving that our sanitization is necessary.
func TestSanitizeTerm_DirectUseFailure(t *testing.T) {
	// Skip if tmux is not available
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found, skipping integration test")
	}

	// Skip in short mode to avoid failures in CI
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	exoticTerm := "xterm-ghostty"
	sessionName := fmt.Sprintf("coi-test-exotic-%d", os.Getpid())

	// Clean up any existing session
	exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	// Try to create a tmux session with the EXOTIC (unsanitized) TERM
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep", "1")
	cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", exoticTerm))

	output, err := cmd.CombinedOutput()

	// We EXPECT this to fail, demonstrating the problem
	if err != nil {
		if strings.Contains(string(output), "missing or unsuitable terminal") {
			t.Logf("EXPECTED FAILURE: tmux rejected exotic TERM=%s with error: %s", exoticTerm, string(output))
			t.Logf("This demonstrates why SanitizeTerm is necessary")
		} else {
			// If it failed for a different reason, that's unexpected
			t.Logf("tmux failed with unexpected error: %v\nOutput: %s", err, string(output))
		}
		return
	}

	// If it succeeded, the system has terminfo for this exotic terminal
	t.Logf("NOTE: System has terminfo for %s, so sanitization wasn't strictly needed on this system", exoticTerm)
}
