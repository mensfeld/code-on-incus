package container

import (
	"os/exec"
	"strings"
	"testing"
)

// CheckNotPrivileged should return nil on a standard unprivileged container.
// This test creates a container with init (no start), checks it, then cleans up.
func TestCheckNotPrivilegedIntegration(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	exists, err := ImageExists("coi-default")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	containerName := "coi-test-security-priv"

	// Clean up in case of leftover from previous run
	_ = IncusExec("delete", containerName, "--force")

	// Init a container (don't start it — we just need it to exist for config checks)
	if err := IncusExec("init", "coi-default", containerName); err != nil {
		t.Fatalf("Failed to init container: %v", err)
	}
	t.Cleanup(func() {
		_ = IncusExec("delete", containerName, "--force")
	})

	// Should pass — container is unprivileged by default
	if err := CheckNotPrivileged(containerName); err != nil {
		t.Errorf("CheckNotPrivileged should return nil for unprivileged container, got: %v", err)
	}

	// Set privileged on the container
	if err := IncusExec("config", "set", containerName, "security.privileged=true"); err != nil {
		t.Fatalf("Failed to set security.privileged: %v", err)
	}

	// Should fail now
	err = CheckNotPrivileged(containerName)
	if err == nil {
		t.Fatal("CheckNotPrivileged should return error for privileged container")
	}
	if !strings.Contains(err.Error(), "security.privileged") {
		t.Errorf("Error should mention security.privileged, got: %v", err)
	}

	// Unset and verify it passes again
	if err := IncusExec("config", "unset", containerName, "security.privileged"); err != nil {
		t.Fatalf("Failed to unset security.privileged: %v", err)
	}
	if err := CheckNotPrivileged(containerName); err != nil {
		t.Errorf("CheckNotPrivileged should return nil after unsetting, got: %v", err)
	}
}

// CheckNotPrivileged should detect security.privileged=true on the default profile.
func TestCheckNotPrivilegedDefaultProfileIntegration(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}

	exists, err := ImageExists("coi-default")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	containerName := "coi-test-security-profile"

	// Clean up in case of leftover from previous run
	_ = IncusExec("delete", containerName, "--force")

	// Init a container
	if err := IncusExec("init", "coi-default", containerName); err != nil {
		t.Fatalf("Failed to init container: %v", err)
	}
	t.Cleanup(func() {
		_ = IncusExec("delete", containerName, "--force")
		// Always restore the default profile
		_ = IncusExec("profile", "unset", "default", "security.privileged")
	})

	// Set privileged on default profile
	if err := IncusExec("profile", "set", "default", "security.privileged=true"); err != nil {
		t.Fatalf("Failed to set security.privileged on default profile: %v", err)
	}

	// Should fail
	err = CheckNotPrivileged(containerName)
	if err == nil {
		t.Fatal("CheckNotPrivileged should return error when default profile is privileged")
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("Error should mention default profile, got: %v", err)
	}

	// Restore and verify
	if err := IncusExec("profile", "unset", "default", "security.privileged"); err != nil {
		t.Fatalf("Failed to unset security.privileged on default profile: %v", err)
	}
	if err := CheckNotPrivileged(containerName); err != nil {
		t.Errorf("CheckNotPrivileged should return nil after restoring profile, got: %v", err)
	}
}
