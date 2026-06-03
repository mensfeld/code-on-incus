package limits

import (
	"context"
	"fmt"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// ApplyOptions contains options for applying limits
type ApplyOptions struct {
	ContainerName string
	CPU           CPULimits
	Memory        MemoryLimits
	Disk          DiskLimits
	Runtime       RuntimeLimits
	// Project is informational. The actual Incus project used by the container
	// helpers is container.IncusProject (set globally via container.Configure).
	// Both are always sourced from cfg.Incus.Project so they remain in sync.
	Project string
}

// ApplyResourceLimits applies all resource limits to a container
func ApplyResourceLimits(opts ApplyOptions) error {
	return ApplyResourceLimitsContext(context.Background(), opts)
}

// ApplyResourceLimitsContext applies all resource limits to a container with context support
func ApplyResourceLimitsContext(ctx context.Context, opts ApplyOptions) error {
	// Validate all limits first
	validationErrors := ValidateAll(opts.CPU, opts.Memory, opts.Disk, opts.Runtime)
	if validationErrors != nil {
		return fmt.Errorf("validation failed: %s", FormatValidationErrors(validationErrors))
	}

	// Apply CPU limits
	if err := applyCPULimits(ctx, opts.ContainerName, opts.CPU); err != nil {
		return fmt.Errorf("failed to apply CPU limits: %w", err)
	}

	// Apply memory limits
	if err := applyMemoryLimits(ctx, opts.ContainerName, opts.Memory); err != nil {
		return fmt.Errorf("failed to apply memory limits: %w", err)
	}

	// Apply disk limits
	if err := applyDiskLimits(ctx, opts.ContainerName, opts.Disk); err != nil {
		return fmt.Errorf("failed to apply disk limits: %w", err)
	}

	// Apply process limits
	if err := applyProcessLimits(ctx, opts.ContainerName, opts.Runtime.MaxProcesses); err != nil {
		return fmt.Errorf("failed to apply process limits: %w", err)
	}

	return nil
}

// applyCPULimits applies CPU limits to a container
func applyCPULimits(ctx context.Context, containerName string, cpu CPULimits) error {
	if cpu.Count != "" {
		if err := container.ConfigSet(ctx, containerName, "limits.cpu", cpu.Count); err != nil {
			return err
		}
	}

	if cpu.Allowance != "" {
		if err := container.ConfigSet(ctx, containerName, "limits.cpu.allowance", cpu.Allowance); err != nil {
			return err
		}
	}

	if cpu.Priority != 0 {
		if err := container.ConfigSet(ctx, containerName, "limits.cpu.priority", fmt.Sprintf("%d", cpu.Priority)); err != nil {
			return err
		}
	}

	return nil
}

// applyMemoryLimits applies memory limits to a container
func applyMemoryLimits(ctx context.Context, containerName string, memory MemoryLimits) error {
	if memory.Limit != "" {
		if err := container.ConfigSet(ctx, containerName, "limits.memory", memory.Limit); err != nil {
			return err
		}
	}

	if memory.Enforce != "" {
		if err := container.ConfigSet(ctx, containerName, "limits.memory.enforce", memory.Enforce); err != nil {
			return err
		}
	}

	if memory.Swap != "" {
		swapValue := NormalizeBoolString(memory.Swap)
		if err := container.ConfigSet(ctx, containerName, "limits.memory.swap", swapValue); err != nil {
			return err
		}
	}

	return nil
}

// applyDiskLimits applies disk I/O limits to the root device of a container.
// Disk I/O limits in Incus are device-level config on the root disk, not
// container-level config keys, so we use `incus config device set`.
// When the container's root disk device comes from an Incus profile (not from
// the instance config), Incus refuses to modify it via `config device set`.
// We work around this by ensuring an instance-level root device exists first.
func applyDiskLimits(ctx context.Context, containerName string, disk DiskLimits) error {
	if disk.Read == "" && disk.Write == "" && disk.Max == "" && disk.Priority == 0 {
		return nil
	}

	// Ensure root device is at the instance level before setting disk I/O limits.
	if err := ensureInstanceRootDevice(ctx, containerName); err != nil {
		return fmt.Errorf("failed to ensure instance-level root device: %w", err)
	}

	if disk.Read != "" {
		if out, err := container.DeviceSet(ctx, containerName, "root", "limits.read="+disk.Read); err != nil {
			return fmt.Errorf("incus config device set limits.read=%s failed: %w (output: %s)", disk.Read, err, out)
		}
	}

	if disk.Write != "" {
		if out, err := container.DeviceSet(ctx, containerName, "root", "limits.write="+disk.Write); err != nil {
			return fmt.Errorf("incus config device set limits.write=%s failed: %w (output: %s)", disk.Write, err, out)
		}
	}

	if disk.Max != "" {
		if out, err := container.DeviceSet(ctx, containerName, "root", "limits.max="+disk.Max); err != nil {
			return fmt.Errorf("incus config device set limits.max=%s failed: %w (output: %s)", disk.Max, err, out)
		}
	}

	if disk.Priority != 0 {
		priority := fmt.Sprintf("%d", disk.Priority)
		if out, err := container.DeviceSet(ctx, containerName, "root", "limits.disk.priority="+priority); err != nil {
			return fmt.Errorf("incus config device set limits.disk.priority=%s failed: %w (output: %s)", priority, err, out)
		}
	}

	return nil
}

// ensureInstanceRootDevice ensures the root disk device is defined at the instance
// level rather than only in an Incus profile. Without this, `incus config device set`
// fails with "Device from profile(s) cannot be modified for individual instance."
func ensureInstanceRootDevice(ctx context.Context, containerName string) error {
	pool, err := getRootDevicePool(ctx, containerName)
	if err != nil {
		return err
	}

	// DeviceAdd already handles the "already exists" case internally.
	if _, err := container.DeviceAdd(ctx, containerName, "root", "disk", "path=/", "pool="+pool); err != nil {
		return fmt.Errorf("failed to add instance-level root device: %w", err)
	}
	return nil
}

// getRootDevicePool returns the storage pool name used by the root disk device
// by querying the expanded container config (which includes profile-inherited devices).
// Falls back to "default" if the pool cannot be determined.
func getRootDevicePool(ctx context.Context, containerName string) (string, error) {
	output, err := container.ConfigShow(ctx, containerName, true)
	if err != nil {
		return "default", fmt.Errorf("failed to get expanded container config: %w", err)
	}

	lines := strings.Split(output, "\n")
	inDevices, inRoot := false, false
	for _, line := range lines {
		if line == "devices:" {
			inDevices = true
			continue
		}
		if inDevices && strings.TrimSpace(line) == "root:" {
			inRoot = true
			continue
		}
		if inRoot {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "pool:") {
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1]), nil
				}
			}
			if len(line) > 0 && !strings.HasPrefix(line, "    ") {
				inRoot = false
			}
		}
		if inDevices && len(line) > 0 && !strings.HasPrefix(line, " ") {
			inDevices = false
		}
	}

	return "default", nil
}

// applyProcessLimits applies process limits to a container
func applyProcessLimits(ctx context.Context, containerName string, maxProcesses int) error {
	if maxProcesses > 0 {
		if err := container.ConfigSet(ctx, containerName, "limits.processes", fmt.Sprintf("%d", maxProcesses)); err != nil {
			return err
		}
	}
	return nil
}

// RemoveLimits removes all limits from a container
func RemoveLimits(containerName, project string) error {
	return RemoveLimitsContext(context.Background(), containerName, project)
}

// RemoveLimitsContext removes all limits from a container with context support
func RemoveLimitsContext(ctx context.Context, containerName, project string) error {
	// Container-level limits
	containerLimits := []string{
		"limits.cpu",
		"limits.cpu.allowance",
		"limits.cpu.priority",
		"limits.memory",
		"limits.memory.enforce",
		"limits.memory.swap",
		"limits.processes",
	}
	for _, limit := range containerLimits {
		// Best-effort: ignore not-found errors (limit was never set).
		out, _ := container.ConfigUnset(ctx, containerName, limit)
		_ = out
	}

	// Device-level disk I/O limits on the root device (set to empty to remove)
	deviceLimits := []string{
		"limits.read",
		"limits.write",
		"limits.max",
		"limits.disk.priority",
	}
	for _, limit := range deviceLimits {
		// Best-effort: ignore not-found and profile-inherited-device errors.
		out, _ := container.DeviceSet(ctx, containerName, "root", limit+"=")
		_ = out
	}

	return nil
}

// GetCurrentLimits retrieves current limits from a container
func GetCurrentLimits(containerName, project string) (map[string]string, error) {
	return GetCurrentLimitsContext(context.Background(), containerName, project)
}

// GetCurrentLimitsContext retrieves current limits from a container with context support
func GetCurrentLimitsContext(ctx context.Context, containerName, project string) (map[string]string, error) {
	output, err := container.ConfigShow(ctx, containerName, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get container config: %w", err)
	}

	limits := make(map[string]string)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "limits.") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				limits[key] = value
			}
		}
	}

	return limits, nil
}
