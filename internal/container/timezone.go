package container

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// validTimezonePattern matches valid IANA timezone names (e.g., "America/New_York", "UTC", "Etc/GMT+5")
// This prevents command injection via crafted timezone names
var validTimezonePattern = regexp.MustCompile(`^[A-Za-z0-9_+\-]+(/[A-Za-z0-9_+\-]+)*$`)

// DetectHostTimezone attempts to detect the host system's timezone using multiple methods.
// Returns the IANA timezone name (e.g., "America/New_York") or "" if detection fails.
func DetectHostTimezone() (string, error) {
	// Method 1: Read /etc/timezone (Debian/Ubuntu)
	if tz, err := detectFromEtcTimezone(); err == nil && tz != "" {
		return tz, nil
	}

	// Method 2: timedatectl (systemd)
	if tz, err := detectFromTimedatectl(); err == nil && tz != "" {
		return tz, nil
	}

	// Method 3: Resolve /etc/localtime symlink
	if tz, err := detectFromLocaltime(); err == nil && tz != "" {
		return tz, nil
	}

	// All methods failed — caller should fall back to UTC
	return "", nil
}

// ValidateTimezone checks whether a timezone name is safe and exists on the host.
func ValidateTimezone(tz string) bool {
	if tz == "" {
		return false
	}

	// Validate format to prevent command injection
	if !validTimezonePattern.MatchString(tz) {
		return false
	}

	// Check that the zoneinfo file exists on the host.
	// Clean the path and verify it stays under /usr/share/zoneinfo/ to prevent traversal.
	const zoneinfoRoot = "/usr/share/zoneinfo"
	zonePath := filepath.Clean(filepath.Join(zoneinfoRoot, tz))
	if !strings.HasPrefix(zonePath, zoneinfoRoot+"/") {
		return false
	}
	info, err := os.Stat(zonePath) //nolint:gosec // path is validated above
	if err != nil {
		return false
	}

	// Must be a regular file, not a directory
	return !info.IsDir()
}

// detectFromEtcTimezone reads the timezone from /etc/timezone (Debian/Ubuntu)
func detectFromEtcTimezone() (string, error) {
	data, err := os.ReadFile("/etc/timezone")
	if err != nil {
		return "", err
	}
	tz := strings.TrimSpace(string(data))
	if tz != "" && ValidateTimezone(tz) {
		return tz, nil
	}
	return "", nil
}

// detectFromTimedatectl uses timedatectl to detect timezone (systemd systems)
func detectFromTimedatectl() (string, error) {
	cmd := exec.Command("timedatectl", "show", "--property=Timezone", "--value")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	tz := strings.TrimSpace(string(output))
	if tz != "" && ValidateTimezone(tz) {
		return tz, nil
	}
	return "", nil
}

// detectFromLocaltime resolves /etc/localtime symlink to extract timezone name
func detectFromLocaltime() (string, error) {
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return "", err
	}

	// Resolve to absolute path if relative
	if !filepath.IsAbs(target) {
		target = filepath.Join("/etc", target)
	}
	target = filepath.Clean(target)

	// Extract timezone from path after /usr/share/zoneinfo/
	const zoneinfoPrefix = "/usr/share/zoneinfo/"
	if idx := strings.Index(target, zoneinfoPrefix); idx != -1 {
		tz := target[idx+len(zoneinfoPrefix):]
		if tz != "" && ValidateTimezone(tz) {
			return tz, nil
		}
	}

	return "", nil
}
