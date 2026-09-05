package terminal

import "testing"

func TestSanitizeTerm(t *testing.T) {
	tests := []struct {
		name string
		term string
		want string
	}{
		// Empty TERM
		{
			name: "empty term",
			term: "",
			want: "xterm-256color",
		},
		// Modern terminals that need mapping
		{
			name: "ghostty terminal",
			term: "xterm-ghostty",
			want: "xterm-256color",
		},
		{
			name: "wezterm",
			term: "wezterm",
			want: "xterm-256color",
		},
		{
			name: "alacritty",
			term: "alacritty",
			want: "xterm-256color",
		},
		{
			// The value kitty ACTUALLY exports by default (#772). The prior
			// test used a bare "kitty" that kitty never sets, so the real
			// xterm-kitty fell through to passthrough and broke `coi shell`.
			name: "kitty (real TERM)",
			term: "xterm-kitty",
			want: "xterm-256color",
		},
		{
			name: "kitty bare",
			term: "kitty",
			want: "xterm-256color",
		},
		{
			name: "foot",
			term: "foot",
			want: "xterm-256color",
		},
		{
			name: "foot-extra",
			term: "foot-extra",
			want: "xterm-256color",
		},
		{
			name: "rio",
			term: "rio",
			want: "xterm-256color",
		},
		{
			name: "contour",
			term: "contour",
			want: "xterm-256color",
		},
		{
			name: "st-256color",
			term: "st-256color",
			want: "xterm-256color",
		},
		{
			// xterm-16color/xterm-88color live in ncurses-term, not the minimal
			// ncurses-base, so they're mapped to the always-present superset
			// rather than trusted to exist (#772 review).
			name: "xterm-16color maps to 256color",
			term: "xterm-16color",
			want: "xterm-256color",
		},
		{
			name: "xterm-88color maps to 256color",
			term: "xterm-88color",
			want: "xterm-256color",
		},
		{
			name: "tmux-256color",
			term: "tmux-256color",
			want: "xterm-256color",
		},
		{
			name: "screen-256color",
			term: "screen-256color",
			want: "xterm-256color",
		},
		// Standard terminals that should pass through
		{
			name: "xterm",
			term: "xterm",
			want: "xterm",
		},
		{
			name: "xterm-256color",
			term: "xterm-256color",
			want: "xterm-256color",
		},
		{
			name: "vt100",
			term: "vt100",
			want: "vt100",
		},
		{
			name: "screen",
			term: "screen",
			want: "screen",
		},
		{
			name: "linux",
			term: "linux",
			want: "linux",
		},
		// Edge cases
		{
			name: "unknown custom term",
			term: "my-custom-term",
			want: "my-custom-term", // Pass through unknown terms
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeTerm(tt.term)
			if got != tt.want {
				t.Errorf("SanitizeTerm(%q) = %q, want %q", tt.term, got, tt.want)
			}
		})
	}
}

func TestSanitizeTermDeterministic(t *testing.T) {
	// Test that sanitization is deterministic
	term := "xterm-ghostty"
	result1 := SanitizeTerm(term)
	result2 := SanitizeTerm(term)

	if result1 != result2 {
		t.Errorf("SanitizeTerm() not deterministic: %s != %s", result1, result2)
	}
}

func TestSanitizeTermFromEnvFlag(t *testing.T) {
	// Test that exotic TERM values passed via -e flag get sanitized
	// This ensures users can't bypass sanitization by using -e TERM=xterm-ghostty
	tests := []struct {
		name     string
		userTerm string
		want     string
	}{
		{
			name:     "user passes exotic TERM via -e flag",
			userTerm: "xterm-ghostty",
			want:     "xterm-256color",
		},
		{
			name:     "user passes wezterm via -e flag",
			userTerm: "wezterm",
			want:     "xterm-256color",
		},
		{
			name:     "user passes standard TERM via -e flag",
			userTerm: "xterm-256color",
			want:     "xterm-256color",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeTerm(tt.userTerm)
			if got != tt.want {
				t.Errorf("SanitizeTerm(%q) = %q, want %q (user should not be able to bypass sanitization)", tt.userTerm, got, tt.want)
			}
		})
	}
}
