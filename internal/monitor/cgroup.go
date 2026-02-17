package monitor

import (
	"bufio"
	"context"
	"fmt"
	"log"
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

	log.Printf("[cgroup] Searching for cgroup path for container: %s", containerName)
	for _, path := range possiblePaths {
		log.Printf("[cgroup] Trying: %s", path)
		if _, err := os.Stat(path); err == nil {
			log.Printf("[cgroup] ✓ Found cgroup path: %s", path)
			return path, nil
		}
	}
	log.Printf("[cgroup] None of the standard paths exist, trying incus info fallback...")

	// Fallback: try to find it via incus info
	log.Printf("[cgroup] Standard paths not found, using incus info fallback")
	path, err := findCgroupPathViaIncus(ctx, containerName)
	if err == nil {
		log.Printf("[cgroup] Found cgroup path via incus: %s", path)
	}
	return path, err
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
		log.Printf("[cgroup] WARNING: Failed to read I/O stats for %s: %v", containerName, err)
		stats.IOReadMB = 0
		stats.IOWriteMB = 0
	} else {
		// If stats are zero, try parent cgroup (in case init.scope doesn't track I/O)
		if ioStats.read == 0 && ioStats.write == 0 {
			log.Printf("[cgroup] I/O stats from init.scope are zero, trying parent cgroup...")
			parentPath := filepath.Dir(cgroupPath)
			parentStats, parentErr := readIOStats(filepath.Join(parentPath, "io.stat"))
			if parentErr == nil && (parentStats.read > 0 || parentStats.write > 0) {
				log.Printf("[cgroup] Using parent cgroup I/O stats from: %s", parentPath)
				ioStats = parentStats
			}
		}

		stats.IOReadMB = ioStats.read / 1024.0 / 1024.0
		stats.IOWriteMB = ioStats.write / 1024.0 / 1024.0
		log.Printf("[cgroup] I/O stats for %s: read=%.2fMB write=%.2fMB", containerName, stats.IOReadMB, stats.IOWriteMB)
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
	log.Printf("[cgroup] Reading I/O stats from: %s", path)

	file, err := os.Open(path)
	if err != nil {
		log.Printf("[cgroup] Failed to open I/O stat file: %v", err)
		return ioStats{}, err
	}
	defer file.Close()

	var stats ioStats
	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		log.Printf("[cgroup] I/O stat line %d: %q", lineCount, line) //nolint:gosec // G706: line is from kernel cgroup file, not user input

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
				log.Printf("[cgroup] Found rbytes: %.0f (total read now: %.0f)", value, stats.read) //nolint:gosec // G706: float from kernel cgroup file, not user-controlled
			case "wbytes":
				stats.write += value
				log.Printf("[cgroup] Found wbytes: %.0f (total write now: %.0f)", value, stats.write) //nolint:gosec // G706: float from kernel cgroup file, not user-controlled
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[cgroup] Scanner error: %v", err)
		return ioStats{}, err
	}

	if lineCount == 0 {
		log.Printf("[cgroup] WARNING: I/O stat file is EMPTY at %s", path)
	}

	log.Printf("[cgroup] Final I/O stats: read=%.2fMB write=%.2fMB (from %d lines)",
		stats.read/1024/1024, stats.write/1024/1024, lineCount)

	return stats, nil
}
