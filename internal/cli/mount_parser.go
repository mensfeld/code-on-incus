package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/session"
)

// warnDroppedMounts prints a per-mount warning for untrusted, unapproved mounts.
// Untrusted project config (a cloned repo's `.coi/config.toml`) can declare
// mounts that bind-mount host paths (host RCE / exfiltration) or sockets that
// forward host capabilities; both are gated behind `coi trust` and warned about
// here when dropped.
func warnDroppedMounts(dropped []session.MountEntry) {
	for _, m := range dropped {
		access := "writable"
		if m.Readonly {
			access = "read-only"
		}
		fmt.Fprintf(os.Stderr,
			"WARNING: ignoring untrusted mount from project config %s:\n"+
				"         host %q -> %q (%s) resolves outside the workspace.\n"+
				"         Run 'coi trust' to approve it (re-approval is required if the\n"+
				"         config later changes), or set %s=1 for CI/automation.\n",
			m.SourcePath, m.HostPath, m.ContainerPath, access, session.TrustEnvVar)
	}
}

// warnDroppedSockets prints a per-socket warning for untrusted, unapproved sockets.
func warnDroppedSockets(dropped []session.SocketEntry) {
	for _, s := range dropped {
		fmt.Fprintf(os.Stderr,
			"WARNING: ignoring untrusted socket from project config %s:\n"+
				"         host %q -> %q forwards a host socket into the container.\n"+
				"         Run 'coi trust' to approve it (re-approval is required if the\n"+
				"         config later changes), or set %s=1 for CI/automation.\n",
			s.SourcePath, s.HostPath, s.ContainerPath, session.TrustEnvVar)
	}
}

// ParseSocketConfig creates a SocketConfig from config file [[sockets]] entries.
func ParseSocketConfig(cfg *config.Config) (*session.SocketConfig, error) {
	sc := &session.SocketConfig{Sockets: []session.SocketEntry{}}
	for i, s := range cfg.Sockets {
		hostPath := config.ExpandPath(s.Host)
		absHost, err := filepath.Abs(hostPath)
		if err != nil {
			return nil, fmt.Errorf("invalid socket host path '%s': %w", s.Host, err)
		}
		if !filepath.IsAbs(s.Container) {
			return nil, fmt.Errorf("socket container path must be absolute: %s", s.Container)
		}
		sc.Sockets = append(sc.Sockets, session.SocketEntry{
			HostPath:      absHost,
			ContainerPath: filepath.Clean(s.Container),
			EnvVar:        s.Env,
			DeviceName:    fmt.Sprintf("socket-%d", i),
			Untrusted:     s.Untrusted,
			SourcePath:    s.SourcePath,
		})
	}
	return sc, nil
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
