package container

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// CheckKernelVersion should parse the real kernel from `uname -r` and return either
// an empty string (kernel meets minimum) or a warning containing the kernel version.
func TestCheckKernelVersionIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("kernel check only runs on Linux")
	}

	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		t.Fatalf("uname -r failed: %v", err)
	}

	raw := strings.TrimSpace(string(out))
	v, err := ParseKernelVersion(raw)
	if err != nil {
		t.Fatalf("ParseKernelVersion(%q) failed: %v", raw, err)
	}

	t.Logf("Parsed kernel version: major=%d minor=%d patch=%d raw=%q", v.Major, v.Minor, v.Patch, v.Raw)

	if v.Major < 3 || v.Major > 99 {
		t.Errorf("major version %d outside reasonable range [3, 99]", v.Major)
	}

	warning := CheckKernelVersion()
	if MeetsMinimumKernelVersion(v) {
		if warning != "" {
			t.Errorf("Kernel %s meets minimum but CheckKernelVersion returned warning: %s", raw, warning)
		}
	} else {
		if warning == "" {
			t.Errorf("Kernel %s is below minimum but CheckKernelVersion returned empty string", raw)
		}
		if !strings.Contains(warning, raw) {
			t.Errorf("Warning should contain kernel version %q, got: %s", raw, warning)
		}
	}
}
