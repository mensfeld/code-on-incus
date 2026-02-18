package container

import (
	"fmt"
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

// TestTmpDefaultUsesDisk verifies that when no tmpfs is configured (the
// default), /tmp lives on the same filesystem as / — i.e. it is backed by
// the container's virtual root disk, not a separate RAM-backed tmpfs.
//
// The assertion has two parts:
//  1. The filesystem type of /tmp is not "tmpfs" (it would be if a separate
//     mount had been applied).
//  2. The total size reported for /tmp equals the total size reported for /,
//     confirming they share the same disk allocation.
func TestTmpDefaultUsesDisk(t *testing.T) {
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

	containerName := "coi-test-tmp-disk-default"
	mgr := NewManager(containerName)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	// Launch with no SetTmpfsSize call — this is the default code path.
	if err := mgr.Launch("coi", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	opts := ExecCommandOptions{}

	// 1. Check filesystem type of /tmp — must not be "tmpfs".
	fstype, err := mgr.ExecArgsCapture([]string{"df", "--output=fstype", "/tmp"}, opts)
	if err != nil {
		t.Fatalf("df --output=fstype /tmp failed: %v", err)
	}
	// df prints a header line then the value; grab the last token.
	fstypeFields := strings.Fields(fstype)
	if len(fstypeFields) < 2 {
		t.Fatalf("Unexpected df fstype output: %q", fstype)
	}
	gotFstype := fstypeFields[len(fstypeFields)-1]
	if gotFstype == "tmpfs" {
		t.Errorf("/tmp filesystem type = %q, want anything other than tmpfs (disk-backed default)", gotFstype)
	}

	// 2. Total size of /tmp must equal total size of / (same disk).
	sizeOfPath := func(path string) (int64, error) {
		out, err := mgr.ExecArgsCapture([]string{"df", "--output=size", path}, opts)
		if err != nil {
			return 0, err
		}
		fields := strings.Fields(out)
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected df output for %s: %q", path, out)
		}
		return strconv.ParseInt(fields[len(fields)-1], 10, 64)
	}

	rootSize, err := sizeOfPath("/")
	if err != nil {
		t.Fatalf("df / failed: %v", err)
	}
	tmpSize, err := sizeOfPath("/tmp")
	if err != nil {
		t.Fatalf("df /tmp failed: %v", err)
	}

	if rootSize != tmpSize {
		t.Errorf("/tmp size = %d KiB, / size = %d KiB — expected equal (same disk)", tmpSize, rootSize)
	}
}
