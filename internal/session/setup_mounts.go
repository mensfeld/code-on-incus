package session

import (
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// setupMounts mounts all configured directories to the container
func setupMounts(mgr *container.Manager, mountConfig *MountConfig, useShift bool, logger func(string)) error {
	if mountConfig == nil || len(mountConfig.Mounts) == 0 {
		return nil
	}

	for _, mount := range mountConfig.Mounts {
		if mount.Readonly {
			// For readonly mounts, skip creating host directory — if the source
			// doesn't exist, log a warning and skip the mount instead of creating
			// an empty directory (which defeats the purpose).
			if _, err := os.Stat(mount.HostPath); err != nil {
				if os.IsNotExist(err) {
					logger(fmt.Sprintf("Warning: readonly mount source %s does not exist, skipping", mount.HostPath))
					continue
				}
				return fmt.Errorf("failed to stat readonly mount source '%s': %w", mount.HostPath, err)
			}
		} else {
			// Create host directory if it doesn't exist (writable mounts only)
			if err := os.MkdirAll(mount.HostPath, 0o755); err != nil {
				return fmt.Errorf("failed to create mount directory '%s': %w", mount.HostPath, err)
			}
		}

		if mount.Readonly {
			logger(fmt.Sprintf("Adding mount (read-only): %s -> %s", mount.HostPath, mount.ContainerPath))
		} else {
			logger(fmt.Sprintf("Adding mount: %s -> %s", mount.HostPath, mount.ContainerPath))
		}

		// Apply shift setting (all mounts use same shift for now)
		if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, useShift, mount.Readonly); err != nil {
			return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
		}
	}

	return nil
}
