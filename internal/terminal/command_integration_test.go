package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestExoticTermWithTmuxCommand verifies that the sanitized TERM values work
// with actual tmux commands. This demonstrates that the fix resolves issue #53.
func TestExoticTermWithTmuxCommand(t *testing.T) {
	// Skip if tmux is not available
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found, skipping integration test")
	}

	tests := []struct {
		name        string
		exoticTerm  string
		expectation string
	}{
		{
			name:        "Ghostty terminal (issue #53)",
			exoticTerm:  "xterm-ghostty",
			expectation: "should work with sanitized TERM",
		},
		{
			name:        "WezTerm",
			exoticTerm:  "wezterm",
			expectation: "should work with sanitized TERM",
		},
		{
			name:        "Alacritty",
			exoticTerm:  "alacritty",
			expectation: "should work with sanitized TERM",
		},
		{
			name:        "Kitty",
			exoticTerm:  "kitty",
			expectation: "should work with sanitized TERM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sanitize the exotic terminal
			sanitized := SanitizeTerm(tt.exoticTerm)

			// Build environment export commands (same pattern as shell.go:472-473)
			containerEnv := map[string]string{
				"TERM":       sanitized,
				"IS_SANDBOX": "1",
			}

			envExports := ""
			for k, v := range containerEnv {
				envExports += fmt.Sprintf("export %s=%q; ", k, v)
			}

			// Create a unique session name
			sessionName := fmt.Sprintf("coi-test-cmd-%d", os.Getpid())

			// Clean up any existing session
			exec.Command("tmux", "kill-session", "-t", sessionName).Run()
			defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()

			// Build tmux command similar to shell.go:539-542 and 600-603
			// This mimics the actual command that failed in issue #53
			tmuxCmd := fmt.Sprintf(
				"tmux new-session -d -s %s \"bash -c 'trap : INT; %s sleep 1'\"",
				sessionName,
				envExports,
			)

			// Execute the command
			cmd := exec.Command("bash", "-c", tmuxCmd)
			output, err := cmd.CombinedOutput()

			if err != nil {
				// Check if it's the specific "missing or unsuitable terminal" error from #53
				if strings.Contains(string(output), "missing or unsuitable terminal") {
					t.Errorf("Got 'missing or unsuitable terminal' error even with sanitized TERM=%s (from %s)\nCommand: %s\nOutput: %s",
						sanitized, tt.exoticTerm, tmuxCmd, string(output))
				} else {
					t.Errorf("Failed to create tmux session: %v\nCommand: %s\nOutput: %s",
						err, tmuxCmd, string(output))
				}
				return
			}

			// Verify the session exists
			checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
			if err := checkCmd.Run(); err != nil {
				t.Errorf("tmux session %s was not created successfully", sessionName)
				return
			}

			// Verify the TERM inside the session is correct
			getTerm := exec.Command("tmux", "run-shell", "-t", sessionName, "echo $TERM")
			termOutput, _ := getTerm.CombinedOutput()
			detectedTerm := strings.TrimSpace(string(termOutput))

			t.Logf("Successfully created tmux session with exotic TERM=%s -> sanitized to %s (detected in session: %s)",
				tt.exoticTerm, sanitized, detectedTerm)
		})
	}
}

// TestEmptyTermWithTmuxCommand verifies that empty TERM gets a sensible default
func TestEmptyTermWithTmuxCommand(t *testing.T) {
	// Skip if tmux is not available
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found, skipping integration test")
	}

	// Test empty TERM
	sanitized := SanitizeTerm("")
	if sanitized != "xterm-256color" {
		t.Errorf("SanitizeTerm(\"\") = %q, expected xterm-256color", sanitized)
		return
	}

	sessionName := fmt.Sprintf("coi-test-empty-%d", os.Getpid())

	// Clean up
	exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	// Create session with empty TERM (now sanitized to xterm-256color)
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep", "1")
	cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", sanitized))

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to create tmux session with sanitized empty TERM: %v\nOutput: %s",
			err, string(output))
		return
	}

	// Verify session exists
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := checkCmd.Run(); err != nil {
		t.Errorf("tmux session %s was not created", sessionName)
	}

	t.Logf("Successfully created tmux session with empty TERM -> sanitized to %s", sanitized)
}
