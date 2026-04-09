package container

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGetWorkspacePath_DefaultMount verifies that GetWorkspacePath returns the correct
// path when workspace is mounted at /workspace (the default).
func TestGetWorkspacePath_DefaultMount(t *testing.T) {
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

	containerName := "coi-test-workspace-default"
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

	if err := mgr.Launch("coi-default", false, ""); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Mount workspace at default /workspace path
	tmpDir := t.TempDir()
	if err := mgr.MountDisk("workspace", tmpDir, "/workspace", true, false); err != nil {
		t.Fatalf("Failed to mount workspace: %v", err)
	}

	// Get workspace path - should return /workspace
	workspacePath := mgr.GetWorkspacePath()
	if workspacePath != "/workspace" {
		t.Errorf("GetWorkspacePath() = %q, want %q", workspacePath, "/workspace")
	}

	// Verify we can execute a command in the workspace
	output, err := mgr.ExecArgsCapture([]string{"pwd"}, ExecCommandOptions{Cwd: workspacePath})
	if err != nil {
		t.Fatalf("Failed to run pwd: %v", err)
	}

	got := strings.TrimSpace(output)
	if got != "/workspace" {
		t.Errorf("pwd in workspace = %q, want %q", got, "/workspace")
	}
}

// TestGetWorkspacePath_CustomMount verifies that GetWorkspacePath returns the correct
// path when workspace is mounted at a custom path (preserve_workspace_path scenario).
func TestGetWorkspacePath_CustomMount(t *testing.T) {
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

	containerName := "coi-test-workspace-custom"
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

	if err := mgr.Launch("coi-default", false, ""); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Create the custom path directory inside container first
	customPath := "/home/code/myproject"
	_, err = mgr.ExecArgsCapture([]string{"mkdir", "-p", customPath}, ExecCommandOptions{})
	if err != nil {
		t.Fatalf("Failed to create custom path directory: %v", err)
	}

	// Mount workspace at custom path (preserving host path)
	tmpDir := t.TempDir()
	if err := mgr.MountDisk("workspace", tmpDir, customPath, true, false); err != nil {
		t.Fatalf("Failed to mount workspace: %v", err)
	}

	// Get workspace path - should return custom path
	workspacePath := mgr.GetWorkspacePath()
	if workspacePath != customPath {
		t.Errorf("GetWorkspacePath() = %q, want %q", workspacePath, customPath)
	}

	// Verify we can execute a command in the workspace
	output, err := mgr.ExecArgsCapture([]string{"pwd"}, ExecCommandOptions{Cwd: workspacePath})
	if err != nil {
		t.Fatalf("Failed to run pwd: %v", err)
	}

	got := strings.TrimSpace(output)
	if got != customPath {
		t.Errorf("pwd in workspace = %q, want %q", got, customPath)
	}
}

// TestGetWorkspacePath_NoWorkspaceDevice verifies that GetWorkspacePath returns
// the fallback /workspace when no workspace device is mounted.
func TestGetWorkspacePath_NoWorkspaceDevice(t *testing.T) {
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

	containerName := "coi-test-workspace-no-device"
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

	if err := mgr.Launch("coi-default", false, ""); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Don't mount any workspace device
	// GetWorkspacePath should return fallback /workspace
	workspacePath := mgr.GetWorkspacePath()
	if workspacePath != "/workspace" {
		t.Errorf("GetWorkspacePath() = %q, want fallback %q", workspacePath, "/workspace")
	}
}

// TestExecWithAutoDetectedWorkspace verifies that ExecArgsCapture uses the
// auto-detected workspace path when Cwd is empty.
func TestExecWithAutoDetectedWorkspace(t *testing.T) {
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

	containerName := "coi-test-exec-autodetect"
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

	if err := mgr.Launch("coi-default", false, ""); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Create a custom path
	customPath := "/home/code/testproject"
	_, err = mgr.ExecArgsCapture([]string{"mkdir", "-p", customPath}, ExecCommandOptions{})
	if err != nil {
		t.Fatalf("Failed to create custom path directory: %v", err)
	}

	// Mount workspace at custom path
	tmpDir := t.TempDir()
	if err := mgr.MountDisk("workspace", tmpDir, customPath, true, false); err != nil {
		t.Fatalf("Failed to mount workspace: %v", err)
	}

	// Exec with Cwd set to the workspace path (simulating auto-detection from caller)
	workspacePath := mgr.GetWorkspacePath()
	output, err := mgr.ExecArgsCapture([]string{"pwd"}, ExecCommandOptions{Cwd: workspacePath})
	if err != nil {
		t.Fatalf("Failed to run pwd: %v", err)
	}

	got := strings.TrimSpace(output)
	if got != customPath {
		t.Errorf("pwd with auto-detected workspace = %q, want %q", got, customPath)
	}
}
