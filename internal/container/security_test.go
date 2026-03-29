package container

import (
	"testing"
)

func TestContainsPrivilegedValue(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"exact true", "true", true},
		{"true with newline", "true\n", true},
		{"true with whitespace", "  true  ", true},
		{"false", "false", false},
		{"empty string", "", false},
		{"false with newline", "false\n", false},
		{"partial match", "trueish", false},
		{"contains true", "not true", false},
		{"uppercase", "TRUE", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsPrivilegedValue(tt.output)
			if got != tt.want {
				t.Errorf("containsPrivilegedValue(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}
