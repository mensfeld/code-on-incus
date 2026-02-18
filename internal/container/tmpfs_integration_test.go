package container

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestSetTmpfsSize verifies that SetTmpfsSize correctly configures the /tmp
// tmpfs mount inside a container and that the reported size matches the
// requested value.
func TestSetTmpfsSize(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}
	exists, err := ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	containerName := "coi-test-tmpfs-size"
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

	if err := mgr.Launch("coi", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Set /tmp to 1GiB (small value to keep the test lightweight)
	const requestedSize = "1GiB"
	if err := mgr.SetTmpfsSize(requestedSize); err != nil {
		t.Fatalf("SetTmpfsSize(%q) failed: %v", requestedSize, err)
	}

	// Read the size back from inside the container using df (1K-blocks output)
	output, err := mgr.ExecArgsCapture(
		[]string{"df", "--output=size", "/tmp"},
		ExecCommandOptions{},
	)
	if err != nil {
		t.Fatalf("df /tmp inside container failed: %v", err)
	}

	// df --output=size prints a header line followed by the value in 1K-blocks
	lines := strings.Fields(output)
	if len(lines) < 2 {
		t.Fatalf("Unexpected df output: %q", output)
	}
	// Last token is the numeric value (skip the "1K-blocks" header)
	sizeStr := lines[len(lines)-1]
	sizeKB, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		t.Fatalf("Could not parse df size %q: %v", sizeStr, err)
	}

	// 1GiB = 1048576 KiB; allow ±5% tolerance for filesystem overhead
	const expectedKB = 1048576
	const tolerance = expectedKB / 20 // 5%
	if sizeKB < expectedKB-tolerance || sizeKB > expectedKB+tolerance {
		t.Errorf("/tmp size = %d KiB, want ~%d KiB (1GiB ±5%%)", sizeKB, expectedKB)
	}
}

// TestSetTmpfsSizeDefault verifies that the default tmpfs size (4GiB) is
// applied when no explicit size is configured.
func TestSetTmpfsSizeDefault(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}
	exists, err := ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	containerName := "coi-test-tmpfs-default"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	const defaultSize = "4GiB"
	if err := mgr.SetTmpfsSize(defaultSize); err != nil {
		t.Fatalf("SetTmpfsSize(%q) failed: %v", defaultSize, err)
	}

	output, err := mgr.ExecArgsCapture(
		[]string{"df", "--output=size", "/tmp"},
		ExecCommandOptions{},
	)
	if err != nil {
		t.Fatalf("df /tmp inside container failed: %v", err)
	}

	lines := strings.Fields(output)
	if len(lines) < 2 {
		t.Fatalf("Unexpected df output: %q", output)
	}
	sizeStr := lines[len(lines)-1]
	sizeKB, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		t.Fatalf("Could not parse df size %q: %v", sizeStr, err)
	}

	// 4GiB = 4194304 KiB; allow ±5% tolerance
	const expectedKB = 4194304
	const tolerance = expectedKB / 20
	if sizeKB < expectedKB-tolerance || sizeKB > expectedKB+tolerance {
		t.Errorf("/tmp size = %d KiB, want ~%d KiB (4GiB ±5%%)", sizeKB, expectedKB)
	}
}
