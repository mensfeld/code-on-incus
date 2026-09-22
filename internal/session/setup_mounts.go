package session

import (
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// ResolveMountShift picks the effective UID/GID shift flag for a single
// configured mount, folding a per-mount `shift` override (#604) into the
// session-wide decision.
//
//   - override == nil        → sessionUseShift (the workspace's decision; the common case)
//   - *override && !rawIdmap → true  (force an idmapped mount; host files show as `code`)
//   - !*override             → false (opt this mount out of shifting)
//
// A per-mount shift=true is dropped to false (with a warning) when the
// container uses raw.idmap for UID mapping: shift and raw.idmap are mutually
// exclusive (Incus rejects the combination) and raw.idmap already remaps the
// mount to the container user, so the override is both unsafe and redundant.
func ResolveMountShift(sessionUseShift bool, override *bool, rawIdmapActive bool, deviceName string, logger func(string)) bool {
	if override == nil {
		return sessionUseShift
	}
	if *override && rawIdmapActive {
		if logger != nil {
			logger(fmt.Sprintf("Mount %s: shift=true ignored — this container uses raw.idmap for UID mapping (mutually exclusive with shift); the mount is already remapped to the container user", deviceName))
		}
		return false
	}
	return *override
}

// setupMounts mounts all configured directories to the container
func setupMounts(mgr container.ContainerDevices, mountConfig *MountConfig, useShift, rawIdmapActive bool, logger func(string)) error {
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

		// Apply the effective shift setting: the session-wide decision unless
		// the mount carries an explicit `shift` override (#604).
		shift := ResolveMountShift(useShift, mount.Shift, rawIdmapActive, mount.DeviceName, logger)
		if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, shift, mount.Readonly); err != nil {
			return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
		}
	}

	return nil
}
