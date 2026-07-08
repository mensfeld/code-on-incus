package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// setupCredentials copies each configured credential entry (catalog bundle or
// ad-hoc) from host to container, chowns it to the container's code user, and
// chmods it if Mode is set. Tolerant of a missing host file (e.g. the user
// hasn't signed into the referenced provider yet), logs and skips rather
// than failing the whole session. Safe to call again on session resume: each
// entry is independently idempotent (re-push, re-chown, re-chmod).
func setupCredentials(mgr container.ContainerManager, homeDir string, entries []CredentialEntry, logger func(string)) error {
	for _, entry := range entries {
		if _, err := os.Stat(entry.HostPath); err != nil {
			logger(fmt.Sprintf("  - Skipping credential %s (not found on host)", entry.HostPath))
			continue
		}

		dest := entry.ContainerPath
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(homeDir, dest)
		}

		destDir := filepath.Dir(dest)
		mkdirCmd := fmt.Sprintf("mkdir -p %s", destDir)
		if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			return fmt.Errorf("failed to create %s: %w", destDir, err)
		}

		logger(fmt.Sprintf("  - Copying credential %s -> %s", entry.HostPath, dest))
		if err := mgr.PushFile(entry.HostPath, dest); err != nil {
			logger(fmt.Sprintf("  - Warning: Failed to copy %s: %v", entry.HostPath, err))
			continue
		}

		if homeDir != "/root" {
			if err := mgr.Chown(dest, container.CodeUID, container.CodeUID); err != nil {
				logger(fmt.Sprintf("  - Warning: Failed to chown %s: %v", dest, err))
			}
		}

		if entry.Mode != "" {
			if err := mgr.ExecArgs([]string{"chmod", entry.Mode, dest}, container.ExecCommandOptions{}); err != nil {
				logger(fmt.Sprintf("  - Warning: Failed to chmod %s to %s: %v", dest, entry.Mode, err))
			}
		}
	}
	return nil
}
