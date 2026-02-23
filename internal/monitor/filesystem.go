package monitor

import (
	"context"
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
	totalWriteBytes uint64
}

// NewFilesystemMonitor creates a new filesystem monitor
func NewFilesystemMonitor() *FilesystemMonitor {
	return &FilesystemMonitor{}
}

// Collect gathers filesystem statistics and calculates read/write rates
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
	currentWriteBytes := uint64(resourceStats.IOWriteMB * 1024 * 1024)

	// Collect disk space stats (non-fatal if fails)
	tmpUsed, tmpTotal, tmpPercent, _ := CollectDiskSpace(ctx, containerName)

	// Calculate delta and rate
	if !fm.previousTime.IsZero() {
		elapsed := time.Since(fm.previousTime)
		deltaReadBytes := currentReadBytes - fm.previousSnapshot.totalReadBytes
		deltaWriteBytes := currentWriteBytes - fm.previousSnapshot.totalWriteBytes

		if elapsed.Seconds() > 0 {
			readRateMBPerSec := float64(deltaReadBytes) / 1024 / 1024 / elapsed.Seconds()
			writeRateMBPerSec := float64(deltaWriteBytes) / 1024 / 1024 / elapsed.Seconds()

			stats := FilesystemStats{
				Available:         true,
				TotalReadMB:       float64(deltaReadBytes) / 1024 / 1024,
				ReadRateMBPerSec:  readRateMBPerSec,
				TotalWriteMB:      float64(deltaWriteBytes) / 1024 / 1024,
				WriteRateMBPerSec: writeRateMBPerSec,
				TmpUsedMB:         tmpUsed,
				TmpTotalMB:        tmpTotal,
				TmpUsedPercent:    tmpPercent,
			}

			// Update snapshot
			fm.previousSnapshot.totalReadBytes = currentReadBytes
			fm.previousSnapshot.totalWriteBytes = currentWriteBytes
			fm.previousTime = time.Now()

			return stats, nil
		}
	}

	// First collection, just store baseline
	fm.previousSnapshot.totalReadBytes = currentReadBytes
	fm.previousSnapshot.totalWriteBytes = currentWriteBytes
	fm.previousTime = time.Now()

	return FilesystemStats{
		Available:         true,
		TotalReadMB:       0,
		ReadRateMBPerSec:  0,
		TotalWriteMB:      0,
		WriteRateMBPerSec: 0,
		TmpUsedMB:         tmpUsed,
		TmpTotalMB:        tmpTotal,
		TmpUsedPercent:    tmpPercent,
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

// FilesystemWriteThreat represents suspicious write activity (potential data exfiltration)
type FilesystemWriteThreat struct {
	WriteBytesMB float64 `json:"write_bytes_mb"`
	WriteRate    float64 `json:"write_rate_mb_per_sec"`
	Threshold    float64 `json:"threshold_mb"`
	Duration     string  `json:"duration"`
}

// DetectLargeWrites checks if filesystem write activity exceeds thresholds
// Large writes can indicate data exfiltration (tar, dd, cp of sensitive data)
func DetectLargeWrites(stats FilesystemStats, thresholdMB float64, rateThresholdMBPerSec float64) *FilesystemWriteThreat {
	// Check if write amount exceeds threshold
	if stats.TotalWriteMB > thresholdMB {
		return &FilesystemWriteThreat{
			WriteBytesMB: stats.TotalWriteMB,
			WriteRate:    stats.WriteRateMBPerSec,
			Threshold:    thresholdMB,
			Duration:     "current interval",
		}
	}

	// Check if sustained write rate exceeds threshold
	if rateThresholdMBPerSec > 0 && stats.WriteRateMBPerSec > rateThresholdMBPerSec {
		return &FilesystemWriteThreat{
			WriteBytesMB: stats.TotalWriteMB,
			WriteRate:    stats.WriteRateMBPerSec,
			Threshold:    rateThresholdMBPerSec,
			Duration:     "sustained",
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
