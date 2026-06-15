package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/session"
)

// gateUntrustedMounts drops mounts from untrusted project config whose host path
// escapes the workspace unless the source config has been trusted (`coi trust`)
// or COI_TRUST_ALL is set, warning about each dropped mount. This prevents a
// cloned repo's `.coi/config.toml` from bind-mounting host paths (writable →
// host RCE; read-only → exfiltration) without the user's explicit approval.
func gateUntrustedMounts(mc *session.MountConfig, workspace string) *session.MountConfig {
	kept, dropped := session.FilterTrustedMounts(mc, workspace)
	for _, m := range dropped {
		access := "writable"
		if m.Readonly {
			access = "read-only"
		}
		fmt.Fprintf(os.Stderr,
			"WARNING: ignoring untrusted mount from project config %s:\n"+
				"         host %q -> %q (%s) resolves outside the workspace.\n"+
				"         Run 'coi trust' to approve it (re-approval is required if the mount\n"+
				"         config later changes), or set %s=1 for CI/automation.\n",
			m.SourcePath, m.HostPath, m.ContainerPath, access, session.TrustEnvVar)
	}
	return kept
}

// ParseMountConfig creates MountConfig from config file mounts
func ParseMountConfig(cfg *config.Config) (*session.MountConfig, error) {
	mountConfig := &session.MountConfig{
		Mounts: []session.MountEntry{},
	}

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

		mountConfig.Mounts = append(mountConfig.Mounts, session.MountEntry{
			HostPath:      absHost,
			ContainerPath: filepath.Clean(cfgMount.Container),
			DeviceName:    fmt.Sprintf("mount-%d", deviceNameCounter),
			Readonly:      cfgMount.Readonly,
			Untrusted:     cfgMount.Untrusted,
			SourcePath:    cfgMount.SourcePath,
		})
		deviceNameCounter++
	}

	return mountConfig, nil
}
