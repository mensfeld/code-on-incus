package container

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestIncusOutputContext_Cancellation verifies that IncusOutputContext returns
// promptly when its context is cancelled, rather than blocking for the full
// duration of the underlying command.
func TestIncusOutputContext_Cancellation(t *testing.T) {
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

	containerName := "coi-test-ctx-output"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi-default", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Run a 30s sleep with a 2s timeout — should return well before 30s
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err = IncusOutputContext(ctx, "exec", containerName, "--", "sleep", "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected error from cancelled context, got nil")
	}

	// Should have returned in roughly 2s, not 30s. Allow up to 5s for overhead.
	if elapsed > 5*time.Second {
		t.Errorf("IncusOutputContext took %v, expected cancellation within ~2s", elapsed)
	}
}

// TestIncusExecContext_Cancellation verifies that IncusExecContext returns
// promptly when its context is cancelled.
func TestIncusExecContext_Cancellation(t *testing.T) {
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

	containerName := "coi-test-ctx-exec"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi-default", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	err = IncusExecContext(ctx, "exec", containerName, "--", "sleep", "30")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected error from cancelled context, got nil")
	}

	if elapsed > 5*time.Second {
		t.Errorf("IncusExecContext took %v, expected cancellation within ~2s", elapsed)
	}
}

// TestIncusOutputContext_Success verifies that IncusOutputContext works
// correctly for a normal (non-cancelled) command.
func TestIncusOutputContext_Success(t *testing.T) {
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

	containerName := "coi-test-ctx-success"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi-default", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := IncusOutputContext(ctx, "exec", containerName, "--", "echo", "hello")
	if err != nil {
		t.Fatalf("IncusOutputContext failed: %v", err)
	}

	if output != "hello" {
		t.Errorf("Expected output %q, got %q", "hello", output)
	}
}
