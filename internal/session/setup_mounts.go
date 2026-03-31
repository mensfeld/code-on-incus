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
		// Create host directory if it doesn't exist
		if err := os.MkdirAll(mount.HostPath, 0o755); err != nil {
			return fmt.Errorf("failed to create mount directory '%s': %w", mount.HostPath, err)
		}

		logger(fmt.Sprintf("Adding mount: %s -> %s", mount.HostPath, mount.ContainerPath))

		// Apply shift setting (all mounts use same shift for now)
		if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, useShift, false); err != nil {
			return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
		}
	}

	return nil
}
