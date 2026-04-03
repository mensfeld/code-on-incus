package container

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadonlyMount_CanReadFile verifies that a read-only mounted directory
// allows reading files inside the container.
func TestReadonlyMount_CanReadFile(t *testing.T) {
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

	containerName := "coi-test-readonly-mount-read"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	// Remove any leftover container from a previous run
	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi-default", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Create temp dir with a test file on host
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "hello from readonly mount"
	if err := os.WriteFile(testFile, []byte(testContent), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Mount it read-only
	containerPath := "/mnt/readonly-test"
	if err := mgr.MountDisk("ro-test", tmpDir, containerPath, true, true); err != nil {
		t.Fatalf("Failed to mount readonly directory: %v", err)
	}

	// Verify file is readable inside container
	output, err := mgr.ExecArgsCapture([]string{"cat", containerPath + "/test.txt"}, ExecCommandOptions{})
	if err != nil {
		t.Fatalf("Failed to read file from readonly mount: %v", err)
	}

	got := strings.TrimSpace(output)
	if got != testContent {
		t.Errorf("Read content = %q, want %q", got, testContent)
	}
}

// TestReadonlyMount_CannotWrite verifies that a read-only mounted directory
// prevents writing files inside the container.
func TestReadonlyMount_CannotWrite(t *testing.T) {
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

	containerName := "coi-test-readonly-mount-write"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	// Remove any leftover container from a previous run
	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi-default", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Create temp dir on host
	tmpDir := t.TempDir()

	// Mount it read-only
	containerPath := "/mnt/readonly-write-test"
	if err := mgr.MountDisk("ro-write-test", tmpDir, containerPath, true, true); err != nil {
		t.Fatalf("Failed to mount readonly directory: %v", err)
	}

	// Attempt to write inside the readonly mount — should fail
	_, err = mgr.ExecArgsCapture([]string{"touch", containerPath + "/should-fail.txt"}, ExecCommandOptions{})
	if err == nil {
		t.Error("Expected write to readonly mount to fail, but it succeeded")
	}
}

// TestReadonlyMount_VsWritableMount verifies that a writable mount allows writing
// while a readonly mount does not, in the same container.
func TestReadonlyMount_VsWritableMount(t *testing.T) {
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

	containerName := "coi-test-readonly-vs-writable"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	// Remove any leftover container from a previous run
	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi-default", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Create two temp dirs
	roDir := t.TempDir()
	rwDir := t.TempDir()

	// Put a file in the readonly dir
	if err := os.WriteFile(filepath.Join(roDir, "readonly-file.txt"), []byte("readonly"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Mount one readonly, one writable
	if err := mgr.MountDisk("ro-mount", roDir, "/mnt/ro", true, true); err != nil {
		t.Fatalf("Failed to mount readonly: %v", err)
	}
	if err := mgr.MountDisk("rw-mount", rwDir, "/mnt/rw", true, false); err != nil {
		t.Fatalf("Failed to mount writable: %v", err)
	}

	// Read from readonly should work
	output, err := mgr.ExecArgsCapture([]string{"cat", "/mnt/ro/readonly-file.txt"}, ExecCommandOptions{})
	if err != nil {
		t.Fatalf("Failed to read from readonly mount: %v", err)
	}
	if strings.TrimSpace(output) != "readonly" {
		t.Errorf("Readonly content = %q, want %q", strings.TrimSpace(output), "readonly")
	}

	// Write to writable should work
	_, err = mgr.ExecArgsCapture([]string{"touch", "/mnt/rw/writable-file.txt"}, ExecCommandOptions{})
	if err != nil {
		t.Error("Expected write to writable mount to succeed")
	}

	// Write to readonly should fail
	_, err = mgr.ExecArgsCapture([]string{"touch", "/mnt/ro/should-fail.txt"}, ExecCommandOptions{})
	if err == nil {
		t.Error("Expected write to readonly mount to fail")
	}
}
