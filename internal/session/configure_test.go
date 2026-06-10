package session

import (
	"encoding/json"
	"testing"
)

func TestConfigureOptions_ContainerNameRequired(t *testing.T) {
	opts := ConfigureOptions{}
	_, err := ConfigureContainer(opts)
	if err == nil {
		t.Fatal("expected error when container name is empty")
	}
	if err.Error() != "container name is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigureResultJSON(t *testing.T) {
	result := &ConfigureResult{
		SSHAgentSocketPath: "/tmp/ssh-agent.sock",
		Timezone:           "Europe/Warsaw",
		NetworkApplied:     "restricted",
		HomeDir:            "/home/code",
	}

	jsonStr, err := ConfigureResultJSON(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON and round-trips correctly
	var parsed ConfigureResult
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.SSHAgentSocketPath != result.SSHAgentSocketPath {
		t.Errorf("SSHAgentSocketPath: got %q, want %q", parsed.SSHAgentSocketPath, result.SSHAgentSocketPath)
	}
	if parsed.Timezone != result.Timezone {
		t.Errorf("Timezone: got %q, want %q", parsed.Timezone, result.Timezone)
	}
	if parsed.NetworkApplied != result.NetworkApplied {
		t.Errorf("NetworkApplied: got %q, want %q", parsed.NetworkApplied, result.NetworkApplied)
	}
	if parsed.HomeDir != result.HomeDir {
		t.Errorf("HomeDir: got %q, want %q", parsed.HomeDir, result.HomeDir)
	}
}

func TestConfigureResultJSON_OmitsEmpty(t *testing.T) {
	result := &ConfigureResult{
		HomeDir: "/home/code",
	}

	jsonStr, err := ConfigureResultJSON(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify empty fields with omitempty are not present
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if _, ok := raw["ssh_agent_socket_path"]; ok {
		t.Error("expected ssh_agent_socket_path to be omitted when empty")
	}
	if _, ok := raw["timezone"]; ok {
		t.Error("expected timezone to be omitted when empty")
	}
	if _, ok := raw["network_applied"]; ok {
		t.Error("expected network_applied to be omitted when empty")
	}
	if _, ok := raw["home_dir"]; !ok {
		t.Error("expected home_dir to always be present")
	}
}

func TestConfigureOptions_DefaultLogger(t *testing.T) {
	// Verify that a nil logger doesn't cause a panic
	opts := ConfigureOptions{
		ContainerName: "nonexistent-container-for-test",
	}

	// This will fail because the container doesn't exist, but it should
	// not panic due to nil logger
	_, err := ConfigureContainer(opts)
	if err == nil {
		t.Fatal("expected error for nonexistent container")
	}
}
