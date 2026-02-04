package health

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/network"
)

// TestCheckContainerConnectivity_NoImage verifies that the check is skipped
// when the specified image doesn't exist.
func TestCheckContainerConnectivity_NoImage(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Use a non-existent image name
	result := CheckContainerConnectivity("non-existent-image-12345")

	if result.Name != "container_connectivity" {
		t.Errorf("Expected check name 'container_connectivity', got '%s'", result.Name)
	}

	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning when image doesn't exist, got %s", result.Status)
	}

	if !strings.Contains(result.Message, "Skipped") || !strings.Contains(result.Message, "image not available") {
		t.Errorf("Expected message about skipped/image not available, got '%s'", result.Message)
	}

	t.Logf("Correctly skipped check for non-existent image: %s", result.Message)
}

// TestCheckContainerConnectivity_WithImage verifies the full connectivity check
// when a valid image exists. This test actually launches a container.
func TestCheckContainerConnectivity_WithImage(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Check if the default 'coi' image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	// Run the actual connectivity check
	result := CheckContainerConnectivity("coi")

	if result.Name != "container_connectivity" {
		t.Errorf("Expected check name 'container_connectivity', got '%s'", result.Name)
	}

	// The check should complete (not hang) and return a definitive status
	switch result.Status {
	case StatusOK:
		t.Logf("Container connectivity check passed: %s", result.Message)
		if result.Details != nil {
			t.Logf("Details: dns_test=%v, http_test=%v", result.Details["dns_test"], result.Details["http_test"])
		}
	case StatusWarning:
		t.Logf("Container connectivity check warning: %s", result.Message)
	case StatusFailed:
		t.Logf("Container connectivity check failed (expected if network is misconfigured): %s", result.Message)
	default:
		t.Errorf("Unexpected status: %s", result.Status)
	}

	// Verify no leftover containers
	containers, err := container.ListContainers("^coi-health-check-")
	if err != nil {
		t.Errorf("Failed to list containers: %v", err)
	}
	if len(containers) > 0 {
		t.Errorf("Found leftover health check containers: %v", containers)
	}
}

// TestCheckContainerConnectivity_EmptyImageName verifies that empty image name
// defaults to "coi".
func TestCheckContainerConnectivity_EmptyImageName(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Check if the default 'coi' image exists
	exists, err := container.ImageExists("coi")
	if err != nil {
		t.Skip("could not check for coi image, skipping integration test")
	}

	// Run with empty image name
	result := CheckContainerConnectivity("")

	if result.Name != "container_connectivity" {
		t.Errorf("Expected check name 'container_connectivity', got '%s'", result.Name)
	}

	if !exists {
		// Should be skipped if image doesn't exist
		if result.Status != StatusWarning {
			t.Errorf("Expected StatusWarning when default image doesn't exist, got %s", result.Status)
		}
		t.Logf("Correctly handled missing default image: %s", result.Message)
	} else {
		// Should run the check if image exists
		if result.Status == StatusWarning && strings.Contains(result.Message, "Skipped") {
			t.Errorf("Should not skip when default coi image exists, got: %s", result.Message)
		}
		t.Logf("Check ran with default image: status=%s, message=%s", result.Status, result.Message)
	}
}

// TestCheckContainerConnectivity_Cleanup verifies that containers are cleaned up
// even when the check fails or encounters errors.
func TestCheckContainerConnectivity_Cleanup(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Check if the default 'coi' image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test")
	}

	// Count containers before
	containersBefore, _ := container.ListContainers("^coi-health-check-")

	// Run multiple checks to ensure cleanup works
	for i := 0; i < 3; i++ {
		_ = CheckContainerConnectivity("coi")
	}

	// Count containers after
	containersAfter, err := container.ListContainers("^coi-health-check-")
	if err != nil {
		t.Errorf("Failed to list containers: %v", err)
	}

	if len(containersAfter) > len(containersBefore) {
		t.Errorf("Found %d new leftover containers after running checks: %v",
			len(containersAfter)-len(containersBefore), containersAfter)
	} else {
		t.Logf("Cleanup verified: no leftover containers after %d checks", 3)
	}
}

// TestCheckNetworkRestriction_NoFirewall verifies that the check is skipped
// when firewalld is not available.
func TestCheckNetworkRestriction_NoFirewall(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// This test only makes sense if firewall is NOT available
	if network.FirewallAvailable() {
		t.Skip("firewalld is available, cannot test no-firewall scenario")
	}

	result := CheckNetworkRestriction("coi")

	if result.Name != "network_restriction" {
		t.Errorf("Expected check name 'network_restriction', got '%s'", result.Name)
	}

	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning when firewall not available, got %s", result.Status)
	}

	if !strings.Contains(result.Message, "firewalld not available") {
		t.Errorf("Expected message about firewalld not available, got '%s'", result.Message)
	}

	t.Logf("Correctly skipped check when firewall unavailable: %s", result.Message)
}

// TestCheckNetworkRestriction_NoImage verifies that the check is skipped
// when the specified image doesn't exist.
func TestCheckNetworkRestriction_NoImage(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Skip if firewall is not available
	if !network.FirewallAvailable() {
		t.Skip("firewalld not available, skipping integration test")
	}

	// Use a non-existent image name
	result := CheckNetworkRestriction("non-existent-image-12345")

	if result.Name != "network_restriction" {
		t.Errorf("Expected check name 'network_restriction', got '%s'", result.Name)
	}

	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning when image doesn't exist, got %s", result.Status)
	}

	if !strings.Contains(result.Message, "image not available") {
		t.Errorf("Expected message about image not available, got '%s'", result.Message)
	}

	t.Logf("Correctly skipped check for non-existent image: %s", result.Message)
}

// TestCheckNetworkRestriction_WithImage verifies the full network restriction check
// when firewall and image are available.
func TestCheckNetworkRestriction_WithImage(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Skip if firewall is not available
	if !network.FirewallAvailable() {
		t.Skip("firewalld not available, skipping integration test")
	}

	// Check if the default 'coi' image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	// Run the network restriction check
	result := CheckNetworkRestriction("coi")

	if result.Name != "network_restriction" {
		t.Errorf("Expected check name 'network_restriction', got '%s'", result.Name)
	}

	// The check should complete and return a definitive status
	switch result.Status {
	case StatusOK:
		t.Logf("Network restriction check passed: %s", result.Message)
		if result.Details != nil {
			t.Logf("Details: external_access=%v, private_blocked=%v",
				result.Details["external_access"], result.Details["private_blocked"])
		}
	case StatusWarning:
		t.Logf("Network restriction check warning: %s", result.Message)
	case StatusFailed:
		t.Logf("Network restriction check failed: %s", result.Message)
		if result.Details != nil {
			t.Logf("Details: %v", result.Details)
		}
	default:
		t.Errorf("Unexpected status: %s", result.Status)
	}

	// Verify no leftover containers
	containers, err := container.ListContainers("^coi-restriction-check-")
	if err != nil {
		t.Errorf("Failed to list containers: %v", err)
	}
	if len(containers) > 0 {
		t.Errorf("Found leftover restriction check containers: %v", containers)
	}
}

// TestCheckNetworkRestriction_Cleanup verifies that containers and firewall rules
// are cleaned up after the check.
func TestCheckNetworkRestriction_Cleanup(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Skip if firewall is not available
	if !network.FirewallAvailable() {
		t.Skip("firewalld not available, skipping integration test")
	}

	// Check if the default 'coi' image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test")
	}

	// Count containers before
	containersBefore, _ := container.ListContainers("^coi-restriction-check-")

	// Run the check
	_ = CheckNetworkRestriction("coi")

	// Count containers after
	containersAfter, err := container.ListContainers("^coi-restriction-check-")
	if err != nil {
		t.Errorf("Failed to list containers: %v", err)
	}

	if len(containersAfter) > len(containersBefore) {
		t.Errorf("Found %d new leftover containers after check: %v",
			len(containersAfter)-len(containersBefore), containersAfter)
	} else {
		t.Logf("Cleanup verified: no leftover containers after restriction check")
	}
}
