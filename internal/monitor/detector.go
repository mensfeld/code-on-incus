package monitor

import (
	"fmt"
	"log"

	"github.com/google/uuid"
)

// Detector analyzes monitoring snapshots for security threats
type Detector struct {
	fileReadThresholdMB  float64
	fileReadRateMBPerSec float64
}

// NewDetector creates a new threat detector
func NewDetector(fileReadThresholdMB, fileReadRateMBPerSec float64) *Detector {
	return &Detector{
		fileReadThresholdMB:  fileReadThresholdMB,
		fileReadRateMBPerSec: fileReadRateMBPerSec,
	}
}

// Analyze examines a snapshot and returns detected threats
func (d *Detector) Analyze(snapshot MonitorSnapshot) []ThreatEvent {
	var threats []ThreatEvent

	// 1. Detect reverse shells
	if snapshot.Processes.Available {
		reverseShells := DetectReverseShells(snapshot.Processes.Processes)
		for _, rs := range reverseShells {
			threats = append(threats, ThreatEvent{
				ID:        uuid.New().String(),
				Timestamp: snapshot.Timestamp,
				Level:     ThreatLevelCritical,
				Category:  "process",
				Title:     "Reverse shell detected",
				Description: fmt.Sprintf("Process '%s' (PID %d) matches reverse shell pattern '%s'",
					rs.Command, rs.PID, rs.Pattern),
				Evidence: rs,
				Action:   "pending",
			})
		}
	}

	// 2. Detect environment scanning
	if snapshot.Processes.Available {
		envScans := DetectEnvScanning(snapshot.Processes.Processes)
		for _, es := range envScans {
			threats = append(threats, ThreatEvent{
				ID:        uuid.New().String(),
				Timestamp: snapshot.Timestamp,
				Level:     ThreatLevelWarning,
				Category:  "environment",
				Title:     "Environment variable scanning detected",
				Description: fmt.Sprintf("Process '%s' (PID %d) is accessing environment variables",
					es.Command, es.PID),
				Evidence: es,
				Action:   "pending",
			})
		}
	}

	// 3. Detect unexpected network connections
	suspiciousConns := []Connection{}
	for _, conn := range snapshot.Network.Connections {
		if conn.Suspicious {
			suspiciousConns = append(suspiciousConns, conn)
		}
	}

	for _, conn := range suspiciousConns {
		level := ThreatLevelHigh
		// Elevate to critical if it's a known C2 port or metadata endpoint
		if extractPort(conn.RemoteAddr) == 4444 || extractPort(conn.RemoteAddr) == 5555 ||
			extractIP(conn.RemoteAddr) == "169.254.169.254" {
			level = ThreatLevelCritical
		}

		threats = append(threats, ThreatEvent{
			ID:        uuid.New().String(),
			Timestamp: snapshot.Timestamp,
			Level:     level,
			Category:  "network",
			Title:     "Unexpected network connection",
			Description: fmt.Sprintf("Connection to %s: %s",
				conn.RemoteAddr, conn.SuspectReason),
			Evidence: NetworkThreat{
				Connection: conn,
				Reason:     conn.SuspectReason,
				RemoteHost: extractIP(conn.RemoteAddr),
			},
			Action: "pending",
		})
	}

	// 4. Detect large workspace reads (possible data exfiltration)
	if snapshot.Filesystem.Available {
		log.Printf("[detector] Filesystem available, checking for large reads (threshold: %.2f MB)", d.fileReadThresholdMB)
		log.Printf("[detector] Filesystem stats: TotalReadMB=%.2f, ReadRate=%.2f MB/s",
			snapshot.Filesystem.TotalReadMB, snapshot.Filesystem.ReadRateMBPerSec)
		fsExfil := DetectLargeReads(snapshot.Filesystem, d.fileReadThresholdMB, d.fileReadRateMBPerSec)
		if fsExfil != nil {
			log.Printf("[detector] FILESYSTEM THREAT DETECTED: %.2f MB read", fsExfil.ReadBytesMB)
			threats = append(threats, ThreatEvent{
				ID:        uuid.New().String(),
				Timestamp: snapshot.Timestamp,
				Level:     ThreatLevelHigh,
				Category:  "filesystem",
				Title:     "Large workspace read detected",
				Description: fmt.Sprintf("Read %.2f MB at %.2f MB/sec (threshold: %.2f MB)",
					fsExfil.ReadBytesMB, fsExfil.ReadRate, fsExfil.Threshold),
				Evidence: fsExfil,
				Action:   "pending",
			})
		} else {
			log.Printf("[detector] No filesystem threat: read %.2f MB below threshold %.2f MB",
				snapshot.Filesystem.TotalReadMB, d.fileReadThresholdMB)
		}
	} else {
		log.Printf("[detector] Filesystem stats NOT available")
	}

	// 5. Detect low disk space (WARNING level)
	if snapshot.Filesystem.Available && snapshot.Filesystem.TmpTotalMB > 0 {
		// Warn if /tmp is >80% full
		if snapshot.Filesystem.TmpUsedPercent > 80 {
			log.Printf("[detector] WARNING: /tmp is %.1f%% full (%.0f/%.0fMB)",
				snapshot.Filesystem.TmpUsedPercent,
				snapshot.Filesystem.TmpUsedMB,
				snapshot.Filesystem.TmpTotalMB)
			threats = append(threats, ThreatEvent{
				ID:        uuid.New().String(),
				Timestamp: snapshot.Timestamp,
				Level:     ThreatLevelWarning,
				Category:  "filesystem",
				Title:     "Low disk space on /tmp",
				Description: fmt.Sprintf("/tmp is %.1f%% full (%.0fMB used of %.0fMB total). Consider increasing tmpfs_size in config.",
					snapshot.Filesystem.TmpUsedPercent,
					snapshot.Filesystem.TmpUsedMB,
					snapshot.Filesystem.TmpTotalMB),
				Evidence: map[string]interface{}{
					"tmp_used_mb":      snapshot.Filesystem.TmpUsedMB,
					"tmp_total_mb":     snapshot.Filesystem.TmpTotalMB,
					"tmp_used_percent": snapshot.Filesystem.TmpUsedPercent,
				},
				Action: "pending",
			})
		}
	}

	return threats
}
