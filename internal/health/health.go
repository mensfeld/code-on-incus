package health

import (
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// CheckStatus represents the status of a health check
type CheckStatus string

const (
	StatusOK      CheckStatus = "ok"
	StatusWarning CheckStatus = "warning"
	StatusFailed  CheckStatus = "failed"
)

// OverallStatus represents the overall health status
type OverallStatus string

const (
	OverallHealthy   OverallStatus = "healthy"
	OverallDegraded  OverallStatus = "degraded"
	OverallUnhealthy OverallStatus = "unhealthy"
)

// HealthCheck represents the result of a single health check
type HealthCheck struct {
	Name    string                 `json:"name"`
	Status  CheckStatus            `json:"status"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// HealthResult represents the overall health check result
type HealthResult struct {
	Status    OverallStatus          `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]HealthCheck `json:"checks"`
	Summary   HealthSummary          `json:"summary"`
}

// HealthSummary provides a summary of health check results
type HealthSummary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Failed   int `json:"failed"`
}

// RunAllChecks runs all health checks and returns the result
func RunAllChecks(cfg *config.Config, verbose bool) *HealthResult {
	checks := make(map[string]HealthCheck)

	// System checks
	checks["os"] = CheckOS()

	// Critical checks
	checks["incus"] = CheckIncus()
	checks["permissions"] = CheckPermissions()
	checks["image"] = CheckImage(cfg.Defaults.Image)
	checks["image_age"] = CheckImageAge(cfg.Defaults.Image)

	// Networking checks
	checks["network_bridge"] = CheckNetworkBridge()
	checks["ip_forwarding"] = CheckIPForwarding()
	checks["firewall"] = CheckFirewall(cfg.Network.Mode)

	// Storage checks
	checks["coi_directory"] = CheckCOIDirectory()
	checks["sessions_directory"] = CheckSessionsDirectory(cfg)
	checks["disk_space"] = CheckDiskSpace()

	// Configuration checks
	checks["config"] = CheckConfiguration(cfg)
	checks["network_mode"] = CheckNetworkMode(cfg.Network.Mode)
	checks["tool"] = CheckTool(cfg.Tool.Name)

	// Status checks
	checks["active_containers"] = CheckActiveContainers()
	checks["saved_sessions"] = CheckSavedSessions(cfg)

	// Container networking checks (critical for detecting real networking issues)
	checks["container_connectivity"] = CheckContainerConnectivity(cfg.Defaults.Image)
	checks["network_restriction"] = CheckNetworkRestriction(cfg.Defaults.Image)

	// Optional checks (only if verbose)
	if verbose {
		checks["dns_resolution"] = CheckDNS()
		checks["passwordless_sudo"] = CheckPasswordlessSudo()
	}

	// Calculate summary
	summary := calculateSummary(checks)

	// Determine overall status
	status := determineStatus(checks)

	return &HealthResult{
		Status:    status,
		Timestamp: time.Now(),
		Checks:    checks,
		Summary:   summary,
	}
}

// calculateSummary calculates the summary from checks
func calculateSummary(checks map[string]HealthCheck) HealthSummary {
	summary := HealthSummary{
		Total: len(checks),
	}

	for _, check := range checks {
		switch check.Status {
		case StatusOK:
			summary.Passed++
		case StatusWarning:
			summary.Warnings++
		case StatusFailed:
			summary.Failed++
		}
	}

	return summary
}

// determineStatus determines the overall status from checks
func determineStatus(checks map[string]HealthCheck) OverallStatus {
	hasFailed := false
	hasWarning := false

	for _, check := range checks {
		switch check.Status {
		case StatusFailed:
			hasFailed = true
		case StatusWarning:
			hasWarning = true
		}
	}

	if hasFailed {
		return OverallUnhealthy
	}
	if hasWarning {
		return OverallDegraded
	}
	return OverallHealthy
}

// ExitCode returns the appropriate exit code for the health result
func (r *HealthResult) ExitCode() int {
	switch r.Status {
	case OverallHealthy:
		return 0
	case OverallDegraded:
		return 1
	case OverallUnhealthy:
		return 2
	default:
		return 2
	}
}
