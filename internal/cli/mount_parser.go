package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/session"
)

// ParseMountConfig creates MountConfig from config file mounts.
//
// absWorkspace is the resolved workspace directory. Mounts that came from an
// untrusted, project-scoped config (MountEntry.FromProject) must resolve to a
// host path INSIDE the workspace; otherwise they are rejected. This stops a
// cloned repo — or an in-container agent that planted a .coi/config.toml — from
// bind-mounting arbitrary host paths (e.g. ~, ~/.ssh, /etc) into the container.
// Mounts from the user's own ~/.coi/config.toml or an explicit COI_CONFIG are
// not restricted.
func ParseMountConfig(cfg *config.Config, absWorkspace string) (*session.MountConfig, error) {
	mountConfig := &session.MountConfig{
		Mounts: []session.MountEntry{},
	}

	cleanWorkspace := filepath.Clean(absWorkspace)
	deviceNameCounter := 0

	// Add config file default mounts
	for _, cfgMount := range cfg.Mounts.Default {
		// Expand host path
		hostPath := config.ExpandPath(cfgMount.Host)
		absHost, err := filepath.Abs(hostPath)
		if err != nil {
			return nil, fmt.Errorf("invalid config mount host path '%s': %w", cfgMount.Host, err)
		}

		// Validate container path is absolute
		if !filepath.IsAbs(cfgMount.Container) {
			return nil, fmt.Errorf("config mount container path must be absolute: %s", cfgMount.Container)
		}

		// Confine untrusted (project-scoped) mounts to the workspace.
		if cfgMount.FromProject && !isWithin(cleanWorkspace, absHost) {
			return nil, fmt.Errorf(
				"project config mount host path %q escapes the workspace and is not allowed; "+
					"move this mount to your ~/.coi/config.toml (or set COI_CONFIG) if you intend it",
				cfgMount.Host)
		}

		mountConfig.Mounts = append(mountConfig.Mounts, session.MountEntry{
			HostPath:      absHost,
			ContainerPath: filepath.Clean(cfgMount.Container),
			DeviceName:    fmt.Sprintf("mount-%d", deviceNameCounter),
			Readonly:      cfgMount.Readonly,
		})
		deviceNameCounter++
	}

	return mountConfig, nil
}

// isWithin reports whether absPath is the directory dir itself or a path nested
// inside it. Both arguments must be absolute and cleaned.
func isWithin(dir, absPath string) bool {
	if absPath == dir {
		return true
	}
	rel, err := filepath.Rel(dir, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
