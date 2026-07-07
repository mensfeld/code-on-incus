package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/vmhost"
)

// ConfigureUIDMapping decides how the workspace mount maps host UIDs into the
// container and applies raw.idmap when needed. It is the single source of truth
// for both the shell (session.Setup) and run pipelines so they cannot diverge
// (issue #530). Returns the effective shift flag (for MountDisk) and whether
// raw.idmap was applied.
//
// Host-VM detection (whether the VM already handles UID mapping, as Colima/Lima
// do — but NOT OrbStack) is delegated to the shared internal/vmhost package.
//
// See decideUIDMapping for the case matrix; the key rule is that a host-UID /
// code-UID mismatch (macOS user 501, CI runner 1001, mapped to code=1000)
// ALWAYS sets raw.idmap and turns shift off — the previous code gated this on
// !disableShift, so the Colima/Lima path silently skipped it and writes to a
// non-1000-owned workspace failed.
//
// raw.idmap only takes effect at container START, so a caller that has ALREADY
// started the container (the run pipeline uses `incus launch` = create+start)
// must restart it when idmapApplied is true; the shell pipeline sets it before
// its own start, so it ignores idmapApplied.
func ConfigureUIDMapping(containerName string, disableShift bool, logger func(string)) (useShift, idmapApplied bool) {
	if logger == nil {
		logger = func(string) {}
	}
	// Detect() reads /proc/mounts etc.; call it once and reuse.
	hostHandlesUIDMapping := vmhost.Detect().HandlesUIDMapping()
	var idmap string
	useShift, idmap = decideUIDMapping(os.Getuid(), container.CodeUID, disableShift, hostHandlesUIDMapping)

	if idmap != "" {
		logger(fmt.Sprintf("Host UID %d differs from container code UID %d, using raw.idmap: %s",
			os.Getuid(), container.CodeUID, idmap))
		if err := container.IncusExec("config", "set", containerName, "raw.idmap", idmap); err != nil {
			logger(fmt.Sprintf("Warning: Failed to set raw.idmap: %v", err))
			return useShift, false
		}
		return useShift, true
	}

	if !useShift {
		logger("UID shifting disabled (Colima/Lima or configured); host UID matches container code UID")
	} else if disableShift || hostHandlesUIDMapping {
		// Colima/Lima was detected but host UID happens to equal code UID.
		logger("Auto-detected Colima/Lima environment - disabling UID shifting")
	}
	return useShift, false
}

// decideUIDMapping is the pure decision behind ConfigureUIDMapping (no I/O), so
// the case matrix — especially the issue #530 regression where a UID mismatch
// was skipped under Colima/Lima — is unit-testable. Returns the effective shift
// flag and the raw.idmap value to set (empty = none).
//
// A host-UID/code-UID mismatch ALWAYS wins: raw.idmap is set and shift is off,
// regardless of disableShift or Colima/Lima. shift=true only translates root,
// not arbitrary UIDs, and cannot be combined with raw.idmap.
func decideUIDMapping(hostUID, codeUID int, disableShift, hostHandlesUIDMapping bool) (useShift bool, idmap string) {
	if !disableShift && hostHandlesUIDMapping {
		disableShift = true
	}
	if hostUID != codeUID {
		return false, fmt.Sprintf("both %d %d", hostUID, codeUID)
	}
	return !disableShift, ""
}

// mergeJSONSettings merges settings into existing JSON content with one-level deep merge.
// If both existing and new values for a key are maps, their entries are merged;
// otherwise the new value overwrites. Returns indented JSON with trailing newline.
// If existingContent is empty or invalid JSON, settings are used as the base and
// parseErr is set to the JSON parse error (callers should log a warning).
func mergeJSONSettings(existingContent []byte, settings map[string]interface{}) (result []byte, parseErr error, err error) {
	existing := make(map[string]interface{})

	// Parse existing content if non-empty
	trimmed := strings.TrimSpace(string(existingContent))
	if len(trimmed) > 0 {
		if unmarshalErr := json.Unmarshal([]byte(trimmed), &existing); unmarshalErr != nil {
			// Invalid JSON — start fresh with settings only, report to caller
			existing = make(map[string]interface{})
			parseErr = unmarshalErr
		}
	}

	// Normalize settings through JSON round-trip to convert typed maps
	// (e.g., map[string]string) to map[string]interface{} for consistent
	// type assertions during merge
	normalizedSettings := make(map[string]interface{})
	settingsBytes, marshalErr := json.Marshal(settings)
	if marshalErr != nil {
		return nil, parseErr, fmt.Errorf("failed to normalize settings: %w", marshalErr)
	}
	if unmarshalErr := json.Unmarshal(settingsBytes, &normalizedSettings); unmarshalErr != nil {
		return nil, parseErr, fmt.Errorf("failed to normalize settings: %w", unmarshalErr)
	}

	// One-level deep merge
	for k, v := range normalizedSettings {
		newMap, newIsMap := v.(map[string]interface{})
		existMap, existIsMap := existing[k].(map[string]interface{})

		if newIsMap && existIsMap {
			for mk, mv := range newMap {
				existMap[mk] = mv
			}
		} else {
			existing[k] = v
		}
	}

	out, marshalErr := json.MarshalIndent(existing, "", "  ")
	if marshalErr != nil {
		return nil, parseErr, fmt.Errorf("failed to marshal merged settings: %w", marshalErr)
	}

	return append(out, '\n'), parseErr, nil
}

// SetupMiseTrust configures MISE_TRUSTED_CONFIG_PATHS so mise automatically
// trusts config files (mise.toml, .tool-versions, etc.) in the workspace.
// The env var is written to both /etc/profile.d/ (login shells) and prepended
// to /etc/bash.bashrc (non-login interactive shells, sourced before ~/.bashrc
// where mise activates). Non-fatal: logs a warning on failure.
func SetupMiseTrust(mgr container.ContainerExecution, containerWorkspacePath string, logger func(string)) {
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

// GitIdentity is a concrete git author identity resolved outside the container.
type GitIdentity struct {
	Name  string
	Email string
}

// Complete reports whether both identity fields are present.
func (i GitIdentity) Complete() bool {
	return strings.TrimSpace(i.Name) != "" && strings.TrimSpace(i.Email) != ""
}

// SetupGitIdentityGuard configures git to require explicit user.name and
// user.email before allowing commits. This prevents AI tools from committing
// as the container's default "code" user. The setting is applied globally
// (--global) so it covers all repos inside the container.
// Non-fatal: logs a warning on failure.
func SetupGitIdentityGuard(mgr container.ContainerExecution, homeDir string, logger func(string)) {
	cmd := fmt.Sprintf(
		`HOME=%s git config --global user.useConfigOnly true`,
		shellEscape(homeDir),
	)
	if _, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true}); err != nil {
		logger(fmt.Sprintf("Warning: Failed to set git user.useConfigOnly: %v", err))
	}
}

// SetupGitIdentity writes a complete, pre-resolved identity into container git
// config. It intentionally does not guess inside the container; when the host
// side cannot provide both fields, user.useConfigOnly remains the fail-closed
// boundary and git will refuse commits rather than allowing model fabrication.
func SetupGitIdentity(mgr container.ContainerExecution, homeDir string, identity GitIdentity, logger func(string)) {
	if !identity.Complete() {
		return
	}
	cmd := fmt.Sprintf(
		`HOME=%s git config --global user.name %s && HOME=%s git config --global user.email %s`,
		shellEscape(homeDir),
		shellEscape(strings.TrimSpace(identity.Name)),
		shellEscape(homeDir),
		shellEscape(strings.TrimSpace(identity.Email)),
	)
	if _, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true}); err != nil {
		logger(fmt.Sprintf("Warning: Failed to configure git identity: %v", err))
		return
	}
	logger("Configured container git identity from host global git config")
}

// SetupClaudeManagedSettings writes /etc/claude-code/managed-settings.json
// inside the container to disable the "Enable auto mode?" prompt that newer
// Claude Code versions show at startup. The managed-settings path is the only
// way to set disableAutoMode — it cannot be set via user settings.
// Non-fatal: logs a warning on failure.
// Accepts ContainerManager (not a sub-interface) because it uses both
// ExecCommand (ContainerExecution) and CreateFile (ContainerFiles).
func SetupClaudeManagedSettings(mgr container.ContainerManager, logger func(string)) {
	mkdirCmd := "mkdir -p /etc/claude-code"
	if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		logger(fmt.Sprintf("Warning: Failed to create Claude managed settings directory: %v", err))
		return
	}
	content := `{"disableAutoMode": "disable"}` + "\n"
	if err := mgr.CreateFile("/etc/claude-code/managed-settings.json", content); err != nil {
		logger(fmt.Sprintf("Warning: Failed to write Claude managed settings: %v", err))
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
