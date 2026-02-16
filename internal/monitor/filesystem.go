package monitor

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// FilesystemMonitor tracks filesystem I/O statistics with delta calculation
type FilesystemMonitor struct {
	mu               sync.Mutex
	previousSnapshot ioSnapshot
	previousTime     time.Time
}

type ioSnapshot struct {
	totalReadBytes  uint64
	totalWriteBytes uint64 //nolint:unused // Reserved for future write monitoring
}

// NewFilesystemMonitor creates a new filesystem monitor
func NewFilesystemMonitor() *FilesystemMonitor {
	return &FilesystemMonitor{}
}

// Collect gathers filesystem statistics and calculates read rates
func (fm *FilesystemMonitor) Collect(ctx context.Context, containerName string) (FilesystemStats, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Get current resource stats (includes I/O)
	resourceStats, err := CollectResourceStats(ctx, containerName)
	if err != nil {
		return FilesystemStats{Available: false}, err
	}

	// Convert to bytes (resourceStats is in MB)
	currentReadBytes := uint64(resourceStats.IOReadMB * 1024 * 1024)

	log.Printf("[filesystem] Current cumulative I/O: %.2f MB (from cgroup)", resourceStats.IOReadMB)

	// Collect disk space stats (non-fatal if fails)
	tmpUsed, tmpTotal, tmpPercent, diskErr := CollectDiskSpace(ctx, containerName)
	if diskErr != nil {
		log.Printf("[filesystem] Warning: Failed to collect disk space: %v", diskErr)
	}

	// Calculate delta and rate
	if !fm.previousTime.IsZero() {
		elapsed := time.Since(fm.previousTime)
		deltaBytes := currentReadBytes - fm.previousSnapshot.totalReadBytes

		log.Printf("[filesystem] Baseline: %.2f MB, Current: %.2f MB, Delta: %.2f MB, Elapsed: %.2fs",
			float64(fm.previousSnapshot.totalReadBytes)/1024/1024,
			float64(currentReadBytes)/1024/1024,
			float64(deltaBytes)/1024/1024,
			elapsed.Seconds())

		if elapsed.Seconds() > 0 {
			rateMBPerSec := float64(deltaBytes) / 1024 / 1024 / elapsed.Seconds()

			stats := FilesystemStats{
				Available:        true,
				TotalReadMB:      float64(deltaBytes) / 1024 / 1024,
				ReadRateMBPerSec: rateMBPerSec,
				TmpUsedMB:        tmpUsed,
				TmpTotalMB:       tmpTotal,
				TmpUsedPercent:   tmpPercent,
			}

			// DEBUG: Log filesystem I/O delta
			log.Printf("[filesystem] ✓ Returning delta: %.2fMB read in %.2fs (rate: %.2f MB/s)",
				stats.TotalReadMB, elapsed.Seconds(), rateMBPerSec)
			if diskErr == nil {
				log.Printf("[filesystem] /tmp: %.0fMB / %.0fMB (%.1f%%)", tmpUsed, tmpTotal, tmpPercent)
			}

			// Update snapshot
			fm.previousSnapshot.totalReadBytes = currentReadBytes
			fm.previousTime = time.Now()

			return stats, nil
		} else {
			log.Printf("[filesystem] WARNING: Elapsed time <= 0, skipping delta calculation")
		}
	}

	// First collection, just store baseline
	log.Printf("[filesystem] First collection - setting baseline: %.2f MB", float64(currentReadBytes)/1024/1024)
	fm.previousSnapshot.totalReadBytes = currentReadBytes
	fm.previousTime = time.Now()

	return FilesystemStats{
		Available:        true,
		TotalReadMB:      0,
		ReadRateMBPerSec: 0,
		TmpUsedMB:        tmpUsed,
		TmpTotalMB:       tmpTotal,
		TmpUsedPercent:   tmpPercent,
	}, nil
}

// DetectLargeReads checks if filesystem read activity exceeds thresholds
func DetectLargeReads(stats FilesystemStats, thresholdMB float64, rateThresholdMBPerSec float64) *FilesystemThreat {
	// Check if read amount exceeds threshold
	if stats.TotalReadMB > thresholdMB {
		return &FilesystemThreat{
			ReadBytesMB: stats.TotalReadMB,
			ReadRate:    stats.ReadRateMBPerSec,
			Threshold:   thresholdMB,
			Duration:    "current interval",
		}
	}

	// Check if sustained read rate exceeds threshold
	if rateThresholdMBPerSec > 0 && stats.ReadRateMBPerSec > rateThresholdMBPerSec {
		return &FilesystemThreat{
			ReadBytesMB: stats.TotalReadMB,
			ReadRate:    stats.ReadRateMBPerSec,
			Threshold:   rateThresholdMBPerSec,
			Duration:    "sustained",
		}
	}

	return nil
}

// CollectDiskSpace gathers disk space stats for /tmp and other critical paths
func CollectDiskSpace(ctx context.Context, containerName string) (tmpUsedMB, tmpTotalMB, tmpUsedPercent float64, err error) {
	// Execute df command in container to get /tmp usage
	output, err := container.IncusOutput("exec", containerName, "--", "df", "-BM", "/tmp")
	if err != nil {
		return 0, 0, 0, err
	}

	// Parse df output (format: Filesystem 1M-blocks Used Available Use% Mounted on)
	// Example: tmpfs        2048M  100M   1948M   5% /tmp
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return 0, 0, 0, nil // No data
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return 0, 0, 0, nil // Unexpected format
	}

	// Parse size (remove 'M' suffix)
	totalStr := strings.TrimSuffix(fields[1], "M")
	usedStr := strings.TrimSuffix(fields[2], "M")
	usedPercentStr := strings.TrimSuffix(fields[4], "%")

	total, _ := strconv.ParseFloat(totalStr, 64)
	used, _ := strconv.ParseFloat(usedStr, 64)
	usedPercent, _ := strconv.ParseFloat(usedPercentStr, 64)

	return used, total, usedPercent, nil
}
