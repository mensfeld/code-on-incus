package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// isColimaOrLimaEnvironment detects if we're running inside a Colima or Lima VM
// These VMs use virtiofs for mounting host directories and already handle UID mapping
// at the VM level, making Incus's shift=true unnecessary and problematic
func isColimaOrLimaEnvironment() bool {
	// Check for virtiofs mounts which are characteristic of Lima/Colima
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	// Lima mounts host directories via virtiofs (e.g., "mount0 on /Users/... type virtiofs")
	// Colima uses Lima under the hood, so same detection applies
	mounts := string(data)
	if strings.Contains(mounts, "virtiofs") {
		return true
	}

	// Additional check: Lima typically runs as the "lima" user
	if user := os.Getenv("USER"); user == "lima" {
		return true
	}

	return false
}

// buildJSONFromSettings converts a settings map to a properly escaped JSON string
// Uses json.Marshal to ensure proper escaping and avoid command injection
func buildJSONFromSettings(settings map[string]interface{}) (string, error) {
	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to marshal settings: %w", err)
	}
	return string(jsonBytes), nil
}

// hasLimits checks if any limits are configured
func hasLimits(cfg *config.LimitsConfig) bool {
	if cfg == nil {
		return false
	}

	// Check if any limit is set (non-empty strings or non-zero integers)
	return cfg.CPU.Count != "" ||
		cfg.CPU.Allowance != "" ||
		cfg.CPU.Priority != 0 ||
		cfg.Memory.Limit != "" ||
		cfg.Memory.Enforce != "" ||
		cfg.Memory.Swap != "" ||
		cfg.Disk.Read != "" ||
		cfg.Disk.Write != "" ||
		cfg.Disk.Max != "" ||
		cfg.Disk.Priority != 0 ||
		cfg.Runtime.MaxProcesses != 0
}
