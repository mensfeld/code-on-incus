package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// GetCgroupPath returns the cgroup v2 path for a container
func GetCgroupPath(ctx context.Context, containerName string) (string, error) {
	// For Incus containers, cgroup v2 path is typically:
	// /sys/fs/cgroup/incus.monitor/<container-name>
	// or /sys/fs/cgroup/lxc.monitor/<container-name>
	// or /sys/fs/cgroup/lxc/<container-name>

	possiblePaths := []string{
		fmt.Sprintf("/sys/fs/cgroup/incus.monitor/%s", containerName),
		fmt.Sprintf("/sys/fs/cgroup/lxc.monitor/%s", containerName),
		fmt.Sprintf("/sys/fs/cgroup/lxc/%s", containerName),
		fmt.Sprintf("/sys/fs/cgroup/incus/%s", containerName),
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Fallback: try to find it via incus info
	return findCgroupPathViaIncus(ctx, containerName)
}

// findCgroupPathViaIncus uses incus info to find the cgroup path
func findCgroupPathViaIncus(ctx context.Context, containerName string) (string, error) {
	// Get container info
	output, err := container.IncusOutput("info", containerName)
	if err != nil {
		return "", fmt.Errorf("failed to get container info: %w", err)
	}

	// Look for PID in the output (format: "PID: 12345")
	var pid string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// Match both "PID:" and "Pid:" for compatibility
		if strings.Contains(line, "PID:") || strings.Contains(line, "Pid:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				pid = parts[1]
				break
			}
		}
	}

	if pid == "" {
		return "", fmt.Errorf("could not find container PID")
	}

	// Read cgroup from /proc/<pid>/cgroup
	cgroupFile := fmt.Sprintf("/proc/%s/cgroup", pid)
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		return "", fmt.Errorf("failed to read cgroup file: %w", err)
	}

	// Parse cgroup v2 format: 0::/path/to/cgroup
	lines = strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "0::") {
			cgroupPath := strings.TrimPrefix(line, "0::")
			cgroupPath = strings.TrimSpace(cgroupPath)
			return filepath.Join("/sys/fs/cgroup", cgroupPath), nil
		}
	}

	return "", fmt.Errorf("could not parse cgroup path from %s", cgroupFile)
}

// CollectResourceStats reads resource usage from cgroup
func CollectResourceStats(ctx context.Context, containerName string) (ResourceStats, error) {
	cgroupPath, err := GetCgroupPath(ctx, containerName)
	if err != nil {
		return ResourceStats{}, fmt.Errorf("failed to get cgroup path: %w", err)
	}

	stats := ResourceStats{}

	// Read CPU stats
	cpuStats, err := readCPUStats(filepath.Join(cgroupPath, "cpu.stat"))
	if err != nil {
		return stats, fmt.Errorf("failed to read CPU stats: %w", err)
	}
	stats.CPUTimeSeconds = cpuStats.total / 1000000.0 // microseconds to seconds
	stats.UserCPUSeconds = cpuStats.user / 1000000.0
	stats.SysCPUSeconds = cpuStats.system / 1000000.0

	// Read memory stats
	memStats, err := readMemoryStats(filepath.Join(cgroupPath, "memory.current"), filepath.Join(cgroupPath, "memory.max"))
	if err != nil {
		return stats, fmt.Errorf("failed to read memory stats: %w", err)
	}
	stats.MemoryMB = memStats.current / 1024.0 / 1024.0
	if memStats.max > 0 && memStats.max != 9223372036854771712 { // max value indicates no limit
		stats.MemoryLimitMB = memStats.max / 1024.0 / 1024.0
	}

	// Read I/O stats
	ioStats, err := readIOStats(filepath.Join(cgroupPath, "io.stat"))
	if err != nil {
		// I/O stats might not be available, don't fail
		stats.IOReadMB = 0
		stats.IOWriteMB = 0
	} else {
		// If stats are zero, try parent cgroup (in case init.scope doesn't track I/O)
		if ioStats.read == 0 && ioStats.write == 0 {
			parentPath := filepath.Dir(cgroupPath)
			parentStats, parentErr := readIOStats(filepath.Join(parentPath, "io.stat"))
			if parentErr == nil && (parentStats.read > 0 || parentStats.write > 0) {
				ioStats = parentStats
			}
		}

		stats.IOReadMB = ioStats.read / 1024.0 / 1024.0
		stats.IOWriteMB = ioStats.write / 1024.0 / 1024.0
	}

	return stats, nil
}

type cpuStats struct {
	total  float64
	user   float64
	system float64
}

func readCPUStats(path string) (cpuStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cpuStats{}, err
	}

	var stats cpuStats
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "usage_usec":
			stats.total = value
		case "user_usec":
			stats.user = value
		case "system_usec":
			stats.system = value
		}
	}

	return stats, nil
}

type memoryStats struct {
	current float64
	max     float64
}

func readMemoryStats(currentPath, maxPath string) (memoryStats, error) {
	var stats memoryStats

	// Read current memory usage
	data, err := os.ReadFile(currentPath)
	if err != nil {
		return stats, err
	}
	current, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return stats, err
	}
	stats.current = current

	// Read memory limit (optional)
	data, err = os.ReadFile(maxPath)
	if err == nil {
		if strings.TrimSpace(string(data)) != "max" {
			maxValue, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err == nil {
				stats.max = maxValue
			}
		}
	}

	return stats, nil
}

type ioStats struct {
	read  float64
	write float64
}

func readIOStats(path string) (ioStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return ioStats{}, err
	}
	defer file.Close()

	var stats ioStats
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Format: <major>:<minor> rbytes=X wbytes=Y ...
		for i := 1; i < len(fields); i++ {
			parts := strings.Split(fields[i], "=")
			if len(parts) != 2 {
				continue
			}

			value, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				continue
			}

			switch parts[0] {
			case "rbytes":
				stats.read += value
			case "wbytes":
				stats.write += value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return ioStats{}, err
	}

	return stats, nil
}
