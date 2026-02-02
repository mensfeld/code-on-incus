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

// applyDiskLimits applies disk I/O limits to a container
func applyDiskLimits(containerName string, disk DiskLimits, project string) error {
	// Apply disk read limit
	if disk.Read != "" {
		if err := setIncusConfig(containerName, "limits.read", disk.Read, project); err != nil {
			return err
		}
	}

	// Apply disk write limit
	if disk.Write != "" {
		if err := setIncusConfig(containerName, "limits.write", disk.Write, project); err != nil {
			return err
		}
	}

	// Apply combined disk limit (overrides read/write)
	if disk.Max != "" {
		if err := setIncusConfig(containerName, "limits.max", disk.Max, project); err != nil {
			return err
		}
	}

	// Apply disk priority
	if disk.Priority != 0 {
		if err := setIncusConfig(containerName, "limits.disk.priority", fmt.Sprintf("%d", disk.Priority), project); err != nil {
			return err
		}
	}

	return nil
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
	limits := []string{
		"limits.cpu",
		"limits.cpu.allowance",
		"limits.cpu.priority",
		"limits.memory",
		"limits.memory.enforce",
		"limits.memory.swap",
		"limits.read",
		"limits.write",
		"limits.max",
		"limits.disk.priority",
		"limits.processes",
	}

	for _, limit := range limits {
		// Continue even if unsetting fails (limit might not be set)
		// We intentionally ignore errors here to allow cleanup to proceed
		_ = unsetIncusConfig(containerName, limit, project)
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
