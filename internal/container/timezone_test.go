package container

import (
	"os"
	"testing"
)

func TestValidateTimezone(t *testing.T) {
	tests := []struct {
		name     string
		tz       string
		expected bool
	}{
		{"empty string", "", false},
		{"UTC", "UTC", true},
		{"America/New_York", "America/New_York", true},
		{"Europe/Warsaw", "Europe/Warsaw", true},
		{"Asia/Tokyo", "Asia/Tokyo", true},
		{"Etc/GMT", "Etc/GMT", true},
		{"invalid chars semicolon", "America/New_York;rm -rf /", false},
		{"invalid chars backtick", "`whoami`", false},
		{"invalid chars dollar", "$(cat /etc/passwd)", false},
		{"invalid chars space", "America/ New_York", false},
		{"path traversal", "../../../etc/passwd", false},
		{"nonexistent timezone", "Fake/Timezone", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateTimezone(tt.tz)
			if result != tt.expected {
				t.Errorf("ValidateTimezone(%q) = %v, want %v", tt.tz, result, tt.expected)
			}
		})
	}
}

func TestValidTimezonePattern(t *testing.T) {
	tests := []struct {
		name     string
		tz       string
		expected bool
	}{
		{"simple", "UTC", true},
		{"with slash", "America/New_York", true},
		{"with underscore", "America/New_York", true},
		{"with plus", "Etc/GMT+5", true},
		{"with minus", "Etc/GMT-5", true},
		{"double slash", "America/Argentina/Buenos_Aires", true},
		{"semicolon injection", "UTC;rm -rf /", false},
		{"backtick injection", "`whoami`", false},
		{"dollar injection", "$(cat /etc/passwd)", false},
		{"space", "America/ New_York", false},
		{"dot dot", "..", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validTimezonePattern.MatchString(tt.tz)
			if result != tt.expected {
				t.Errorf("validTimezonePattern.MatchString(%q) = %v, want %v", tt.tz, result, tt.expected)
			}
		})
	}
}

func TestDetectHostTimezone(t *testing.T) {
	tz, err := DetectHostTimezone()
	if err != nil {
		t.Fatalf("DetectHostTimezone() returned error: %v", err)
	}

	// On a properly configured system, this should return something.
	// It's OK if it returns "" on a minimal CI environment.
	if tz != "" {
		if !ValidateTimezone(tz) {
			t.Errorf("DetectHostTimezone() returned invalid timezone: %q", tz)
		}
		t.Logf("Detected host timezone: %s", tz)
	} else {
		t.Log("No timezone detected (may be expected in minimal environments)")
	}
}

func TestDetectFromEtcTimezone(t *testing.T) {
	// Only run if /etc/timezone exists
	if _, err := os.Stat("/etc/timezone"); os.IsNotExist(err) {
		t.Skip("/etc/timezone not found")
	}

	tz, err := detectFromEtcTimezone()
	if err != nil {
		t.Fatalf("detectFromEtcTimezone() returned error: %v", err)
	}

	if tz != "" {
		if !ValidateTimezone(tz) {
			t.Errorf("detectFromEtcTimezone() returned invalid timezone: %q", tz)
		}
		t.Logf("Timezone from /etc/timezone: %s", tz)
	}
}

func TestDetectFromLocaltime(t *testing.T) {
	// Only run if /etc/localtime is a symlink
	if _, err := os.Readlink("/etc/localtime"); err != nil {
		t.Skip("/etc/localtime is not a symlink")
	}

	tz, err := detectFromLocaltime()
	if err != nil {
		t.Fatalf("detectFromLocaltime() returned error: %v", err)
	}

	if tz != "" {
		if !ValidateTimezone(tz) {
			t.Errorf("detectFromLocaltime() returned invalid timezone: %q", tz)
		}
		t.Logf("Timezone from /etc/localtime symlink: %s", tz)
	}
}
