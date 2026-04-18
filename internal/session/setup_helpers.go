package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
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

// SetupMiseTrust configures MISE_TRUSTED_CONFIG_PATHS so mise automatically
// trusts config files (mise.toml, .tool-versions, etc.) in the workspace.
// The env var is written to both /etc/profile.d/ (login shells) and prepended
// to /etc/bash.bashrc (non-login interactive shells, sourced before ~/.bashrc
// where mise activates). Non-fatal: logs a warning on failure.
func SetupMiseTrust(mgr *container.Manager, containerWorkspacePath string, logger func(string)) {
	exportLine := fmt.Sprintf(`export MISE_TRUSTED_CONFIG_PATHS="%s"`, containerWorkspacePath)
	trustCmd := fmt.Sprintf(
		`printf '%%s\n' '%s' > /etc/profile.d/coi-mise-trust.sh && `+
			`sed -i '/MISE_TRUSTED_CONFIG_PATHS/d' /etc/bash.bashrc && `+
			`sed -i '1i %s' /etc/bash.bashrc`,
		exportLine, exportLine,
	)
	if _, err := mgr.ExecCommand(trustCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		logger(fmt.Sprintf("Warning: Failed to configure mise workspace trust: %v", err))
	}
}

// SetupGitIdentityGuard configures git to require explicit user.name and
// user.email before allowing commits. This prevents AI tools from committing
// as the container's default "code" user. The setting is applied globally
// (--global) so it covers all repos inside the container.
// Non-fatal: logs a warning on failure.
func SetupGitIdentityGuard(mgr *container.Manager, homeDir string, logger func(string)) {
	cmd := fmt.Sprintf(
		`HOME=%s git config --global user.useConfigOnly true`,
		homeDir,
	)
	if _, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true}); err != nil {
		logger(fmt.Sprintf("Warning: Failed to set git user.useConfigOnly: %v", err))
	}
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
