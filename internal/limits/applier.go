package limits

import (
	"fmt"
	"os/exec"
	"strings"
)

// ApplyOptions contains options for applying limits
type ApplyOptions struct {
	ContainerName string
	CPU           CPULimits
	Memory        MemoryLimits
	Disk          DiskLimits
	Runtime       RuntimeLimits
	Project       string // Incus project name
}

// ApplyResourceLimits applies all resource limits to a container
func ApplyResourceLimits(opts ApplyOptions) error {
	// Validate all limits first
	validationErrors := ValidateAll(opts.CPU, opts.Memory, opts.Disk, opts.Runtime)
	if validationErrors != nil {
		return fmt.Errorf("validation failed: %s", FormatValidationErrors(validationErrors))
	}

	// Apply CPU limits
	if err := applyCPULimits(opts.ContainerName, opts.CPU, opts.Project); err != nil {
		return fmt.Errorf("failed to apply CPU limits: %w", err)
	}

	// Apply memory limits
	if err := applyMemoryLimits(opts.ContainerName, opts.Memory, opts.Project); err != nil {
		return fmt.Errorf("failed to apply memory limits: %w", err)
	}

	// Apply disk limits
	if err := applyDiskLimits(opts.ContainerName, opts.Disk, opts.Project); err != nil {
		return fmt.Errorf("failed to apply disk limits: %w", err)
	}

	// Apply process limits
	if err := applyProcessLimits(opts.ContainerName, opts.Runtime.MaxProcesses, opts.Project); err != nil {
		return fmt.Errorf("failed to apply process limits: %w", err)
	}

	return nil
}

// applyCPULimits applies CPU limits to a container
func applyCPULimits(containerName string, cpu CPULimits, project string) error {
	// Apply CPU count
	if cpu.Count != "" {
		if err := setIncusConfig(containerName, "limits.cpu", cpu.Count, project); err != nil {
			return err
		}
	}

	// Apply CPU allowance
	if cpu.Allowance != "" {
		if err := setIncusConfig(containerName, "limits.cpu.allowance", cpu.Allowance, project); err != nil {
			return err
		}
	}

	// Apply CPU priority
	if cpu.Priority != 0 {
		if err := setIncusConfig(containerName, "limits.cpu.priority", fmt.Sprintf("%d", cpu.Priority), project); err != nil {
			return err
		}
	}

	return nil
}

// applyMemoryLimits applies memory limits to a container
func applyMemoryLimits(containerName string, memory MemoryLimits, project string) error {
	// Apply memory limit
	if memory.Limit != "" {
		if err := setIncusConfig(containerName, "limits.memory", memory.Limit, project); err != nil {
			return err
		}
	}

	// Apply memory enforcement mode
	if memory.Enforce != "" {
		if err := setIncusConfig(containerName, "limits.memory.enforce", memory.Enforce, project); err != nil {
			return err
		}
	}

	// Apply memory swap
	if memory.Swap != "" {
		swapValue := NormalizeBoolString(memory.Swap)
		if err := setIncusConfig(containerName, "limits.memory.swap", swapValue, project); err != nil {
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
func applyDiskLimits(containerName string, disk DiskLimits, project string) error {
	if disk.Read == "" && disk.Write == "" && disk.Max == "" && disk.Priority == 0 {
		return nil
	}

	// Ensure root device is at the instance level before setting disk I/O limits.
	if err := ensureInstanceRootDevice(containerName, project); err != nil {
		return fmt.Errorf("failed to ensure instance-level root device: %w", err)
	}

	if disk.Read != "" {
		if err := setIncusDeviceConfig(containerName, "root", "limits.read", disk.Read, project); err != nil {
			return err
		}
	}

	if disk.Write != "" {
		if err := setIncusDeviceConfig(containerName, "root", "limits.write", disk.Write, project); err != nil {
			return err
		}
	}

	if disk.Max != "" {
		if err := setIncusDeviceConfig(containerName, "root", "limits.max", disk.Max, project); err != nil {
			return err
		}
	}

	if disk.Priority != 0 {
		if err := setIncusDeviceConfig(containerName, "root", "limits.disk.priority", fmt.Sprintf("%d", disk.Priority), project); err != nil {
			return err
		}
	}

	return nil
}

// ensureInstanceRootDevice ensures the root disk device is defined at the instance
// level rather than only in an Incus profile. Without this, `incus config device set`
// fails with "Device from profile(s) cannot be modified for individual instance."
func ensureInstanceRootDevice(containerName, project string) error {
	pool, err := getRootDevicePool(containerName, project)
	if err != nil {
		return err
	}

	args := []string{"config", "device", "add"}
	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}
	args = append(args, containerName, "root", "disk", "path=/", fmt.Sprintf("pool=%s", pool))

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Device already exists at the instance level — that's exactly what we want.
		if strings.Contains(strings.ToLower(outputStr), "already") {
			return nil
		}
		return fmt.Errorf("failed to add instance-level root device: %w (output: %s)", err, outputStr)
	}
	return nil
}

// getRootDevicePool returns the storage pool name used by the root disk device
// by querying the expanded container config (which includes profile-inherited devices).
// Falls back to "default" if the pool cannot be determined.
func getRootDevicePool(containerName, project string) (string, error) {
	args := []string{"config", "show", "--expanded"}
	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}
	args = append(args, containerName)

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "default", fmt.Errorf("failed to get expanded container config: %w", err)
	}

	lines := strings.Split(string(output), "\n")
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
func applyProcessLimits(containerName string, maxProcesses int, project string) error {
	if maxProcesses > 0 {
		if err := setIncusConfig(containerName, "limits.processes", fmt.Sprintf("%d", maxProcesses), project); err != nil {
			return err
		}
	}
	return nil
}

// setIncusDeviceConfig sets a device-level configuration key using `incus config device set`.
func setIncusDeviceConfig(containerName, device, key, value, project string) error {
	args := []string{"config", "device", "set"}

	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}

	args = append(args, containerName, device, fmt.Sprintf("%s=%s", key, value))

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("incus config device set %s=%s failed: %w (output: %s)", key, value, err, string(output))
	}

	return nil
}

// setIncusConfig sets a configuration key on a container using incus config set
func setIncusConfig(containerName, key, value, project string) error {
	args := []string{"config", "set"}

	// Add project flag if specified
	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}

	args = append(args, containerName, fmt.Sprintf("%s=%s", key, value))

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("incus config set %s=%s failed: %w (output: %s)", key, value, err, string(output))
	}

	return nil
}

// RemoveLimits removes all limits from a container
func RemoveLimits(containerName, project string) error {
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
		_ = unsetIncusConfig(containerName, limit, project)
	}

	// Device-level disk I/O limits on the root device
	deviceLimits := []string{
		"limits.read",
		"limits.write",
		"limits.max",
		"limits.disk.priority",
	}
	for _, limit := range deviceLimits {
		_ = unsetIncusDeviceConfig(containerName, "root", limit, project)
	}

	return nil
}

// unsetIncusDeviceConfig unsets a device-level configuration key.
func unsetIncusDeviceConfig(containerName, device, key, project string) error {
	args := []string{"config", "device", "set"}

	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}

	// Setting to empty string effectively removes the limit from the device
	args = append(args, containerName, device, fmt.Sprintf("%s=", key))

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		if strings.Contains(outputStr, "not found") || strings.Contains(outputStr, "doesn't exist") {
			return nil
		}
		// If the device is from a profile (limits were never applied at instance level), skip.
		if strings.Contains(outputStr, "Device from profile") {
			return nil
		}
		return fmt.Errorf("incus config device set %s= failed: %w (output: %s)", key, err, outputStr)
	}

	return nil
}

// unsetIncusConfig unsets a configuration key on a container
func unsetIncusConfig(containerName, key, project string) error {
	args := []string{"config", "unset"}

	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}

	args = append(args, containerName, key)

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if error is because key doesn't exist (which is fine)
		if strings.Contains(string(output), "not found") || strings.Contains(string(output), "doesn't exist") {
			return nil
		}
		return fmt.Errorf("incus config unset %s failed: %w (output: %s)", key, err, string(output))
	}

	return nil
}

// GetCurrentLimits retrieves current limits from a container
// This can be used for debugging or displaying current settings
func GetCurrentLimits(containerName, project string) (map[string]string, error) {
	args := []string{"config", "show"}

	if project != "" && project != "default" {
		args = append(args, "--project", project)
	}

	args = append(args, containerName)

	cmd := exec.Command("incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get container config: %w (output: %s)", err, string(output))
	}

	// Parse the output to extract limits
	// This is a simplified version - full implementation would parse YAML
	limits := make(map[string]string)
	lines := strings.Split(string(output), "\n")
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
