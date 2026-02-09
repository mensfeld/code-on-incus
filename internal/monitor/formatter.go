package monitor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FormatSnapshot formats a monitoring snapshot as human-readable text
func FormatSnapshot(snapshot MonitorSnapshot) string {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("Container: %s", snapshot.ContainerName))
	if snapshot.ContainerIP != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", snapshot.ContainerIP))
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n", snapshot.Timestamp.Format(time.RFC3339)))
	sb.WriteString(strings.Repeat("━", 70) + "\n\n")

	// Threats summary
	if len(snapshot.Threats) > 0 {
		criticalCount := 0
		highCount := 0
		warningCount := 0
		infoCount := 0

		for _, threat := range snapshot.Threats {
			switch threat.Level {
			case ThreatLevelCritical:
				criticalCount++
			case ThreatLevelHigh:
				highCount++
			case ThreatLevelWarning:
				warningCount++
			case ThreatLevelInfo:
				infoCount++
			}
		}

		sb.WriteString("⚠ THREATS DETECTED: ")
		parts := []string{}
		if criticalCount > 0 {
			parts = append(parts, fmt.Sprintf("%d critical", criticalCount))
		}
		if highCount > 0 {
			parts = append(parts, fmt.Sprintf("%d high", highCount))
		}
		if warningCount > 0 {
			parts = append(parts, fmt.Sprintf("%d warning", warningCount))
		}
		if infoCount > 0 {
			parts = append(parts, fmt.Sprintf("%d info", infoCount))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("\n\n")

		// Recent threats
		sb.WriteString("RECENT THREATS\n")
		for _, threat := range snapshot.Threats {
			levelStr := string(threat.Level)
			switch threat.Level {
			case ThreatLevelCritical:
				levelStr = "CRITICAL"
			case ThreatLevelHigh:
				levelStr = "HIGH    "
			case ThreatLevelWarning:
				levelStr = "WARNING "
			case ThreatLevelInfo:
				levelStr = "INFO    "
			}

			sb.WriteString(fmt.Sprintf("  [%s] %s  %s\n",
				threat.Timestamp.Format("15:04:05"),
				levelStr,
				threat.Title))
			sb.WriteString(fmt.Sprintf("                      %s\n", threat.Description))
			if threat.Action != "" && threat.Action != "logged" {
				sb.WriteString(fmt.Sprintf("                      → Action: %s\n", threat.Action))
			}
			sb.WriteString("\n")
		}
	}

	// Network stats
	sb.WriteString(fmt.Sprintf("NETWORK (%d active connections", snapshot.Network.ActiveConnections))
	if snapshot.Network.SuspiciousCount > 0 {
		sb.WriteString(fmt.Sprintf(", %d suspicious", snapshot.Network.SuspiciousCount))
	}
	sb.WriteString(")\n")

	if len(snapshot.Network.Connections) > 0 {
		sb.WriteString("  Protocol  Local Address       Remote Address       State        Status\n")
		for _, conn := range snapshot.Network.Connections {
			status := "✓ Normal"
			if conn.Suspicious {
				status = "⚠ SUSPICIOUS"
			}
			sb.WriteString(fmt.Sprintf("  %-8s  %-18s  %-18s  %-11s  %s\n",
				conn.Protocol, conn.LocalAddr, conn.RemoteAddr, conn.State, status))
		}
	} else {
		sb.WriteString("  No active connections\n")
	}
	sb.WriteString("\n")

	// Process stats
	if snapshot.Processes.Available {
		sb.WriteString(fmt.Sprintf("PROCESSES (%d running)\n", snapshot.Processes.TotalCount))
		if len(snapshot.Processes.Processes) > 0 {
			sb.WriteString("  PID    User   Command                                  Flags\n")
			for _, proc := range snapshot.Processes.Processes {
				flags := ""
				if proc.EnvAccess {
					flags = "⚠ ENV SCAN"
				}

				// Truncate long commands
				cmd := proc.Command
				if len(cmd) > 40 {
					cmd = cmd[:37] + "..."
				}

				sb.WriteString(fmt.Sprintf("  %-6d %-6s %-40s %s\n",
					proc.PID, proc.User, cmd, flags))
			}
		}
	} else {
		sb.WriteString("PROCESSES\n  Not available\n")
	}
	sb.WriteString("\n")

	// Filesystem stats
	if snapshot.Filesystem.Available {
		sb.WriteString("FILESYSTEM\n")
		sb.WriteString(fmt.Sprintf("  Workspace Reads:  %.2f MB (%.2f MB/sec)\n",
			snapshot.Filesystem.TotalReadMB, snapshot.Filesystem.ReadRateMBPerSec))
		if snapshot.Filesystem.FilesAccessed > 0 {
			sb.WriteString(fmt.Sprintf("  Files Accessed:   %d\n", snapshot.Filesystem.FilesAccessed))
		}
	} else {
		sb.WriteString("FILESYSTEM\n  Not available\n")
	}
	sb.WriteString("\n")

	// Resource stats
	sb.WriteString("RESOURCES\n")
	sb.WriteString(fmt.Sprintf("  CPU:     %.1fs total (%.1fs user, %.1fs system)\n",
		snapshot.Resources.CPUTimeSeconds, snapshot.Resources.UserCPUSeconds, snapshot.Resources.SysCPUSeconds))
	if snapshot.Resources.MemoryLimitMB > 0 {
		memPercent := (snapshot.Resources.MemoryMB / snapshot.Resources.MemoryLimitMB) * 100
		sb.WriteString(fmt.Sprintf("  Memory:  %.0f MB / %.0f MB (%.1f%%)\n",
			snapshot.Resources.MemoryMB, snapshot.Resources.MemoryLimitMB, memPercent))
	} else {
		sb.WriteString(fmt.Sprintf("  Memory:  %.0f MB\n", snapshot.Resources.MemoryMB))
	}
	sb.WriteString(fmt.Sprintf("  I/O:     %.0f MB read, %.0f MB write\n",
		snapshot.Resources.IOReadMB, snapshot.Resources.IOWriteMB))

	// Errors
	if len(snapshot.Errors) > 0 {
		sb.WriteString("\nERRORS\n")
		for _, err := range snapshot.Errors {
			sb.WriteString(fmt.Sprintf("  - %s\n", err))
		}
	}

	return sb.String()
}

// FormatSnapshotJSON formats a monitoring snapshot as JSON
func FormatSnapshotJSON(snapshot MonitorSnapshot) (string, error) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatThreatAlert formats a threat event as a colored alert
func FormatThreatAlert(threat ThreatEvent) string {
	color := "\033[33m" // Yellow for warning
	if threat.Level == ThreatLevelCritical || threat.Level == ThreatLevelHigh {
		color = "\033[31m" // Red for high/critical
	}
	reset := "\033[0m"

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n%s⚠ SECURITY ALERT [%s]%s\n", color, strings.ToUpper(string(threat.Level)), reset))
	sb.WriteString(fmt.Sprintf("%s%s%s\n", color, threat.Title, reset))
	sb.WriteString(fmt.Sprintf("%s\n", threat.Description))
	if threat.Action != "" && threat.Action != "logged" {
		sb.WriteString(fmt.Sprintf("\n→ Action taken: %s\n", threat.Action))
	}
	sb.WriteString("\n")

	return sb.String()
}
