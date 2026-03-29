package container

import (
	"strings"
	"testing"
)

func TestParseKernelVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		major   int
		minor   int
		patch   int
		wantErr bool
	}{
		{"standard version", "6.17.0-19-generic", 6, 17, 0, false},
		{"simple version", "5.15.0", 5, 15, 0, false},
		{"major.minor only", "6.1", 6, 1, 0, false},
		{"with suffix", "5.4.0-150-generic", 5, 4, 0, false},
		{"old kernel", "4.15.0-213-generic", 4, 15, 0, false},
		{"with whitespace", "  6.5.0  ", 6, 5, 0, false},
		{"high patch", "5.15.131", 5, 15, 131, false},
		{"empty string", "", 0, 0, 0, true},
		{"no minor", "6", 0, 0, 0, true},
		{"invalid major", "abc.1.0", 0, 0, 0, true},
		{"invalid minor", "6.abc.0", 0, 0, 0, true},
		{"invalid patch", "6.1.abc", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseKernelVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Major != tt.major {
				t.Errorf("major = %d, want %d", v.Major, tt.major)
			}
			if v.Minor != tt.minor {
				t.Errorf("minor = %d, want %d", v.Minor, tt.minor)
			}
			if v.Patch != tt.patch {
				t.Errorf("patch = %d, want %d", v.Patch, tt.patch)
			}
		})
	}
}

func TestMeetsMinimumKernelVersion(t *testing.T) {
	tests := []struct {
		name  string
		major int
		minor int
		want  bool
	}{
		{"exact minimum", 5, 15, true},
		{"above minimum minor", 5, 19, true},
		{"below minimum minor", 5, 14, false},
		{"below minimum major", 4, 15, false},
		{"higher major", 6, 0, true},
		{"much higher", 6, 17, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &KernelVersion{Major: tt.major, Minor: tt.minor, Raw: "test"}
			got := MeetsMinimumKernelVersion(v)
			if got != tt.want {
				t.Errorf("MeetsMinimumKernelVersion(%d.%d) = %v, want %v", tt.major, tt.minor, got, tt.want)
			}
		})
	}
}

func TestFormatKernelWarning(t *testing.T) {
	v := &KernelVersion{Major: 4, Minor: 15, Patch: 0, Raw: "4.15.0-213-generic"}
	msg := FormatKernelWarning(v)

	if !strings.Contains(msg, "4.15.0-213-generic") {
		t.Error("message should contain the actual version")
	}
	if !strings.Contains(msg, "5.15") {
		t.Error("message should contain the minimum version")
	}
	if !strings.Contains(msg, "WARNING") {
		t.Error("message should contain WARNING prefix")
	}
	if !strings.Contains(msg, "upgrading") {
		t.Error("message should suggest upgrading")
	}
}
