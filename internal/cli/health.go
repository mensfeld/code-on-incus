package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/health"
	"github.com/spf13/cobra"
)

var (
	healthFormat  string
	healthVerbose bool
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check system health and dependencies",
	Long: `Check all system dependencies and report their status.

This helps diagnose setup issues and verify your environment is correctly configured.

Examples:
  coi health                  # Basic health check (text output)
  coi health --format json    # JSON output for scripting
  coi health --verbose        # Include additional checks

Exit codes:
  0 = healthy (all checks pass)
  1 = degraded (warnings but functional)
  2 = unhealthy (critical failures)
`,
	RunE: healthCommand,
}

func init() {
	healthCmd.Flags().StringVar(&healthFormat, "format", "text", "Output format: text or json")
	healthCmd.Flags().BoolVarP(&healthVerbose, "verbose", "v", false, "Include additional verbose checks")
}

func healthCommand(cmd *cobra.Command, args []string) error {
	// Validate format
	if healthFormat != "text" && healthFormat != "json" {
		return fmt.Errorf("invalid format '%s': must be 'text' or 'json'", healthFormat)
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		// Even if config fails to load, we want to run health checks
		cfg = config.GetDefaultConfig()
	}

	// Run all health checks
	result := health.RunAllChecks(cfg, healthVerbose)

	// Output based on format
	if healthFormat == "json" {
		return outputHealthJSON(result)
	}

	return outputHealthText(result)
}

// outputHealthJSON outputs health check results as JSON
func outputHealthJSON(result *health.HealthResult) error {
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(jsonData))

	// Exit with appropriate code
	os.Exit(result.ExitCode())
	return nil
}

// outputHealthText outputs health check results as human-readable text
func outputHealthText(result *health.HealthResult) error {
	fmt.Println("Code on Incus Health Check")
	fmt.Println("==========================")
	fmt.Println()

	// Group checks by category
	categories := map[string][]string{
		"SYSTEM":        {"os"},
		"CRITICAL":      {"incus", "permissions", "image", "image_age"},
		"NETWORKING":    {"network_bridge", "ip_forwarding", "firewall"},
		"STORAGE":       {"coi_directory", "sessions_directory", "disk_space"},
		"CONFIGURATION": {"config", "network_mode", "tool"},
		"STATUS":        {"active_containers", "saved_sessions"},
		"OPTIONAL":      {"dns_resolution", "passwordless_sudo"},
	}

	// Category order
	categoryOrder := []string{"SYSTEM", "CRITICAL", "NETWORKING", "STORAGE", "CONFIGURATION", "STATUS", "OPTIONAL"}

	for _, category := range categoryOrder {
		checkNames := categories[category]
		hasChecks := false

		// Check if any checks in this category exist
		for _, name := range checkNames {
			if _, ok := result.Checks[name]; ok {
				hasChecks = true
				break
			}
		}

		if !hasChecks {
			continue
		}

		fmt.Printf("%s:\n", category)

		for _, name := range checkNames {
			check, ok := result.Checks[name]
			if !ok {
				continue
			}

			// Format status indicator
			var statusIcon string
			switch check.Status {
			case health.StatusOK:
				statusIcon = "[OK]"
			case health.StatusWarning:
				statusIcon = "[WARN]"
			case health.StatusFailed:
				statusIcon = "[FAIL]"
			}

			// Format the check name for display
			displayName := formatCheckName(name)

			fmt.Printf("  %-6s %-18s %s\n", statusIcon, displayName, check.Message)
		}
		fmt.Println()
	}

	// Print any checks that weren't in a category
	printedNames := make(map[string]bool)
	for _, names := range categories {
		for _, name := range names {
			printedNames[name] = true
		}
	}

	var uncategorized []string
	for name := range result.Checks {
		if !printedNames[name] {
			uncategorized = append(uncategorized, name)
		}
	}

	if len(uncategorized) > 0 {
		sort.Strings(uncategorized)
		fmt.Println("OTHER:")
		for _, name := range uncategorized {
			check := result.Checks[name]
			var statusIcon string
			switch check.Status {
			case health.StatusOK:
				statusIcon = "[OK]"
			case health.StatusWarning:
				statusIcon = "[WARN]"
			case health.StatusFailed:
				statusIcon = "[FAIL]"
			}
			displayName := formatCheckName(name)
			fmt.Printf("  %-6s %-18s %s\n", statusIcon, displayName, check.Message)
		}
		fmt.Println()
	}

	// Print summary
	fmt.Printf("STATUS: %s\n", strings.ToUpper(string(result.Status)))

	if result.Summary.Failed > 0 {
		fmt.Printf("%d of %d checks failed", result.Summary.Failed, result.Summary.Total)
		if result.Summary.Warnings > 0 {
			fmt.Printf(", %d warnings", result.Summary.Warnings)
		}
		fmt.Println()
	} else if result.Summary.Warnings > 0 {
		fmt.Printf("%d checks passed with %d warnings\n", result.Summary.Passed, result.Summary.Warnings)
	} else {
		fmt.Printf("All %d checks passed\n", result.Summary.Total)
	}

	// Exit with appropriate code
	os.Exit(result.ExitCode())
	return nil
}

// formatCheckName converts snake_case check names to Title Case for display
func formatCheckName(name string) string {
	// Special cases for better display
	specialCases := map[string]string{
		"os":                 "Operating system",
		"incus":              "Incus",
		"permissions":        "Permissions",
		"image":              "Default image",
		"image_age":          "Image age",
		"network_bridge":     "Network bridge",
		"ip_forwarding":      "IP forwarding",
		"firewall":           "Firewalld",
		"coi_directory":      "COI directory",
		"sessions_directory": "Sessions dir",
		"disk_space":         "Disk space",
		"config":             "Config loaded",
		"network_mode":       "Network mode",
		"tool":               "Tool",
		"active_containers":  "Containers",
		"saved_sessions":     "Saved sessions",
		"dns_resolution":     "DNS resolution",
		"passwordless_sudo":  "Passwordless sudo",
	}

	if displayName, ok := specialCases[name]; ok {
		return displayName
	}

	// Default: convert snake_case to Title Case
	words := strings.Split(name, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}
