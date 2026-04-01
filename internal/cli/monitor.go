package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/monitor"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/spf13/cobra"
)

var (
	monitorJSON  bool
	monitorWatch int
)

func init() {
	monitorCmd.Flags().BoolVar(&monitorJSON, "json", false, "Output in JSON format")
	monitorCmd.Flags().IntVar(&monitorWatch, "watch", 0, "Watch mode: update every N seconds (0 = one-shot)")

	rootCmd.AddCommand(monitorCmd)
}

var monitorCmd = &cobra.Command{
	Use:   "monitor [container]",
	Short: "Display real-time security monitoring for a container",
	Long: `Display real-time security monitoring for a container.

This command shows current container metrics including:
- Network connections (with suspicious connection detection)
- Running processes (with reverse shell detection)
- Filesystem activity (workspace read monitoring)
- Resource usage (CPU, memory, I/O)
- Security threats and alerts

If no container name is provided, it will attempt to detect the container
from the current workspace.

Examples:
  coi monitor                    # Auto-detect container, one-shot
  coi monitor coi-abc-1          # Monitor specific container
  coi monitor --json             # JSON output
  coi monitor --watch 2          # Update every 2 seconds`,
	RunE: monitorCommand,
}

func monitorCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine container name using 3-tier resolution:
	// 1. Positional argument
	// 2. COI_CONTAINER environment variable
	// 3. Auto-detect from workspace
	containerName, err := resolveMonitorContainer(args)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Monitoring container: %s\n", containerName)

	// Get allowed CIDRs from network config
	allowedCIDRs := []string{}
	if cfg.Network.Mode == config.NetworkModeAllowlist {
		// Convert allowed domains to CIDRs (simplified - in full implementation would resolve)
		// For now, just pass empty list
		allowedCIDRs = []string{}
	}

	// Create collector
	collector := monitor.NewCollector(containerName, "", "", allowedCIDRs)
	detector := monitor.NewDetector(cfg.Monitoring.FileReadThresholdMB, cfg.Monitoring.FileReadRateMBPerSec)

	// Watch mode or one-shot
	if monitorWatch > 0 {
		return runMonitorWatch(ctx, collector, detector, monitorWatch)
	}

	// One-shot collection
	snapshot, err := collector.Collect(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect metrics: %w", err)
	}

	// Detect threats
	snapshot.Threats = detector.Analyze(snapshot)

	// Output
	if monitorJSON {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(monitor.FormatSnapshot(snapshot))
	}

	return nil
}

// resolveMonitorContainer resolves the container name using the following strategy:
// 1. Use positional arg if provided
// 2. Check COI_CONTAINER environment variable
// 3. Find container for current workspace
func resolveMonitorContainer(args []string) (string, error) {
	// 1. Use positional arg if provided
	if len(args) > 0 {
		name := args[0]
		mgr := container.NewManager(name)
		exists, err := mgr.Exists()
		if err != nil {
			return "", fmt.Errorf("failed to check container: %w", err)
		}
		if !exists {
			return "", fmt.Errorf("container '%s' not found", name)
		}
		return name, nil
	}

	// 2. Check COI_CONTAINER environment variable
	if envContainer := os.Getenv("COI_CONTAINER"); envContainer != "" {
		mgr := container.NewManager(envContainer)
		exists, err := mgr.Exists()
		if err != nil {
			return "", fmt.Errorf("failed to check container: %w", err)
		}
		if !exists {
			return "", fmt.Errorf("container '%s' from COI_CONTAINER not found", envContainer)
		}
		return envContainer, nil
	}

	// 3. Find container for current workspace
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	sessions, err := session.ListWorkspaceSessions(absWorkspace)
	if err != nil {
		return "", fmt.Errorf("failed to list workspace sessions: %w", err)
	}

	if len(sessions) == 0 {
		return "", fmt.Errorf("no COI containers found for current workspace - pass container name as argument")
	}

	if len(sessions) > 1 {
		var names []string
		for _, name := range sessions {
			names = append(names, name)
		}
		return "", fmt.Errorf("multiple COI containers found for workspace, pass container name as argument: %s", strings.Join(names, ", "))
	}

	// Exactly one container
	for _, name := range sessions {
		return name, nil
	}

	return "", fmt.Errorf("no COI containers found for current workspace")
}

func runMonitorWatch(ctx context.Context, collector *monitor.Collector, detector *monitor.Detector, intervalSec int) error {
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	// Initial collection
	snapshot, err := collector.Collect(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect metrics: %w", err)
	}
	snapshot.Threats = detector.Analyze(snapshot)

	// Clear screen and display
	fmt.Print("\033[2J\033[H") // Clear screen, move cursor to top
	fmt.Print(monitor.FormatSnapshot(snapshot))
	fmt.Printf("\nLast Updated: %s | Press Ctrl+C to exit\n", time.Now().Format("2006-01-02 15:04:05"))

	// Watch loop
	for {
		select {
		case <-ticker.C:
			snapshot, err := collector.Collect(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}
			snapshot.Threats = detector.Analyze(snapshot)

			// Clear screen and display
			fmt.Print("\033[2J\033[H") // Clear screen, move cursor to top
			fmt.Print(monitor.FormatSnapshot(snapshot))
			fmt.Printf("\nLast Updated: %s | Press Ctrl+C to exit\n", time.Now().Format("2006-01-02 15:04:05"))

		case <-ctx.Done():
			return nil
		}
	}
}

// Audit log command - TODO: Implement or remove
// var monitorAuditCmd = &cobra.Command{
// 	Use:   "audit [container]",
// 	Short: "View audit log for a container",
// 	Long: `View audit log entries for a container.
//
// The audit log contains all monitoring events, threats, and security alerts
// recorded during container sessions.
//
// Examples:
//   coi monitor audit                          # Last 100 entries
//   coi monitor audit coi-abc-1                # Specific container
//   coi monitor audit --export=report.json     # Export to file`,
// 	RunE: monitorAuditCommand,
// }

func monitorAuditCommand(cmd *cobra.Command, args []string) error { //nolint:unused // TODO: Implement or remove
	// Determine container name
	var containerName string
	if len(args) > 0 {
		containerName = args[0]
	} else {
		return fmt.Errorf("container name required")
	}

	// Get audit log path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	auditLogPath := filepath.Join(homeDir, ".coi", "audit", containerName+".jsonl")

	// Check if audit log exists
	if _, err := os.Stat(auditLogPath); os.IsNotExist(err) {
		return fmt.Errorf("no audit log found for container %s", containerName)
	}

	// Read audit log
	data, err := os.ReadFile(auditLogPath)
	if err != nil {
		return fmt.Errorf("failed to read audit log: %w", err)
	}

	// Display (simplified - just output raw JSON lines for now)
	fmt.Println(string(data))

	return nil
}
