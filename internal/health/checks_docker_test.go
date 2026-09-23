package health

import (
	"testing"
)

// TestCheckDockerForwardPolicy_Structure verifies the health check returns a
// valid HealthCheck with the expected details fields.
func TestCheckDockerForwardPolicy_Structure(t *testing.T) {
	result := CheckDockerForwardPolicy()

	if result.Name != "docker_forward_policy" {
		t.Errorf("Name = %q, want %q", result.Name, "docker_forward_policy")
	}

	if result.Status != StatusOK && result.Status != StatusWarning && result.Status != StatusFailed {
		t.Errorf("Status = %q, want one of ok/warning/failed", result.Status)
	}

	if result.Message == "" {
		t.Error("Message is empty")
	}

	// Verify expected details fields exist
	expectedFields := []string{"docker_running", "forward_policy_drop", "nft_available", "iptables_available"}
	for _, field := range expectedFields {
		if _, ok := result.Details[field]; !ok {
			t.Errorf("Details missing expected field %q", field)
		}
	}

	t.Logf("CheckDockerForwardPolicy: status=%s message=%q details=%v",
		result.Status, result.Message, result.Details)
}
