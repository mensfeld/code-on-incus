package cli

import (
	"fmt"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/session"
)

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
		})
		deviceNameCounter++
	}

	return mountConfig, nil
}
