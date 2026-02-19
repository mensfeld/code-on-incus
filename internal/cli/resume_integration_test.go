package cli

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// TestResumeCommand_FrozenContainer tests resuming a frozen container
func TestResumeCommand_FrozenContainer(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Check if the coi image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test")
	}

	// Create a test container
	containerName := "coi-test-resume-frozen"

	// Clean up any existing container
	container.IncusOutput("delete", containerName, "--force")

	// Launch container
	_, err = container.IncusOutput("launch", "coi", containerName)
	if err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}
	defer container.IncusOutput("delete", containerName, "--force")

	// Wait for container to be ready
	time.Sleep(2 * time.Second)

	// Freeze the container
	_, err = container.IncusOutput("pause", containerName)
	if err != nil {
		t.Fatalf("Failed to pause container: %v", err)
	}

	// Verify it's frozen
	status, err := getContainerStatus(containerName)
	if err != nil {
		t.Fatalf("Failed to get container status: %v", err)
	}
	if status != "Frozen" {
		t.Fatalf("Expected container to be Frozen, got %s", status)
	}

	// Resume the container
	err = resumeContainer(containerName)
	if err != nil {
		t.Fatalf("Failed to resume container: %v", err)
	}

	// Verify it's running
	status, err = getContainerStatus(containerName)
	if err != nil {
		t.Fatalf("Failed to get container status: %v", err)
	}
	if status != "RUNNING" {
		t.Errorf("Expected container to be RUNNING after resume, got %s", status)
	}

	t.Log("Successfully resumed frozen container")
}

// TestResumeCommand_NotFrozen tests that resume fails on non-frozen container
func TestResumeCommand_NotFrozen(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Check if the coi image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test")
	}

	// Create a test container
	containerName := "coi-test-resume-running"

	// Clean up any existing container
	container.IncusOutput("delete", containerName, "--force")

	// Launch container
	_, err = container.IncusOutput("launch", "coi", containerName)
	if err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}
	defer container.IncusOutput("delete", containerName, "--force")

	// Wait for container to be ready
	time.Sleep(2 * time.Second)

	// Try to resume a running container (should fail)
	err = resumeContainer(containerName)
	if err == nil {
		t.Error("Expected error when resuming non-frozen container, got nil")
	}

	if !strings.Contains(err.Error(), "not frozen") {
		t.Errorf("Expected error message about 'not frozen', got: %v", err)
	}

	t.Log("Correctly rejected resume on non-frozen container")
}

// TestResumeCommand_NonExistent tests that resume fails on non-existent container
func TestResumeCommand_NonExistent(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Try to resume a non-existent container
	err := resumeContainer("coi-nonexistent-container-12345")
	if err == nil {
		t.Error("Expected error when resuming non-existent container, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected error message about 'not found', got: %v", err)
	}

	t.Log("Correctly rejected resume on non-existent container")
}

// TestResumeAllFrozen tests resuming all frozen containers
func TestResumeAllFrozen(t *testing.T) {
	// Skip if incus is not available
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}

	// Skip if incus daemon is not running
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	// Check if the coi image exists
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test")
	}

	// Create two test containers
	containers := []string{"coi-test-resume-all-1", "coi-test-resume-all-2"}

	// Clean up any existing containers
	for _, name := range containers {
		container.IncusOutput("delete", name, "--force")
	}

	// Launch containers
	for _, name := range containers {
		_, err = container.IncusOutput("launch", "coi", name)
		if err != nil {
			t.Fatalf("Failed to launch container %s: %v", name, err)
		}
		defer container.IncusOutput("delete", name, "--force")
	}

	// Wait for containers to be ready
	time.Sleep(3 * time.Second)

	// Freeze both containers
	for _, name := range containers {
		_, err = container.IncusOutput("pause", name)
		if err != nil {
			t.Fatalf("Failed to pause container %s: %v", name, err)
		}
	}

	// Resume all frozen containers
	err = resumeAllFrozen()
	if err != nil {
		t.Fatalf("Failed to resume all frozen: %v", err)
	}

	// Verify both are running
	for _, name := range containers {
		status, err := getContainerStatus(name)
		if err != nil {
			t.Fatalf("Failed to get container %s status: %v", name, err)
		}
		if status != "RUNNING" {
			t.Errorf("Expected container %s to be RUNNING after resumeAllFrozen, got %s", name, status)
		}
	}

	t.Log("Successfully resumed all frozen containers")
}
