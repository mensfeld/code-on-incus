package container

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const (
	// MinKernelVersionMajor is the minimum recommended kernel major version
	MinKernelVersionMajor = 5
	// MinKernelVersionMinor is the minimum recommended kernel minor version
	MinKernelVersionMinor = 15
)

// KernelVersion represents a parsed Linux kernel version
type KernelVersion struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// ParseKernelVersion parses a kernel version string like "6.17.0-19-generic"
func ParseKernelVersion(str string) (*KernelVersion, error) {
	str = strings.TrimSpace(str)
	if str == "" {
		return nil, fmt.Errorf("empty kernel version string")
	}

	// Split on "-" first to get the numeric part, then split on "."
	numericPart := strings.SplitN(str, "-", 2)[0]
	parts := strings.SplitN(numericPart, ".", 4)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid kernel version format: %q (expected major.minor)", str)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version in %q: %w", str, err)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minor version in %q: %w", str, err)
	}

	patch := 0
	if len(parts) >= 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid patch version in %q: %w", str, err)
		}
	}

	return &KernelVersion{
		Major: major,
		Minor: minor,
		Patch: patch,
		Raw:   str,
	}, nil
}

// MeetsMinimumKernelVersion checks if the given kernel version meets the minimum (>= 5.15)
func MeetsMinimumKernelVersion(v *KernelVersion) bool {
	if v.Major > MinKernelVersionMajor {
		return true
	}
	if v.Major == MinKernelVersionMajor {
		return v.Minor >= MinKernelVersionMinor
	}
	return false
}

// CheckKernelVersion checks the running kernel version and returns a warning message
// if it is below the minimum recommended version. Returns "" if the version is OK,
// on non-Linux platforms, or on any failure (graceful degradation).
func CheckKernelVersion() string {
	if runtime.GOOS != "linux" {
		return ""
	}

	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}

	v, err := ParseKernelVersion(string(out))
	if err != nil {
		return ""
	}

	if !MeetsMinimumKernelVersion(v) {
		return FormatKernelWarning(v)
	}

	return ""
}

// FormatKernelWarning returns an actionable warning for users running old kernels
func FormatKernelWarning(v *KernelVersion) string {
	return fmt.Sprintf(
		"WARNING: Kernel %s is below the recommended minimum %d.%d. "+
			"Older kernels may have known CVEs and lack security features "+
			"needed for safe container isolation. "+
			"Please consider upgrading your kernel to %d.%d or newer.",
		v.Raw, MinKernelVersionMajor, MinKernelVersionMinor,
		MinKernelVersionMajor, MinKernelVersionMinor,
	)
}
