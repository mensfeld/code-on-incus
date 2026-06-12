package container

import (
	"os/exec"
	"testing"
	"time"
)

// skipUnlessContainerTestable skips the test when Incus is unavailable.
func skipUnlessContainerTestable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}
	exists, err := ImageExists("coi-default")
	if err != nil || !exists {
		t.Skip("coi-default image not found, skipping integration test (run 'coi build' first)")
	}
}

// TestDirExistsAndFileExists_PathWithSpaces verifies that DirExists and FileExists
// handle container paths that contain spaces without word-splitting them via the
// shell.  The previous implementation used fmt.Sprintf("[ -d %s ]", path) which
// split "dir with spaces" into three shell tokens, always returning false for such
// paths.
func TestDirExistsAndFileExists_PathWithSpaces(t *testing.T) {
	skipUnlessContainerTestable(t)

	name := "coi-test-path-spaces-" + t.Name()
	mgr := NewManager(name)

	t.Cleanup(func() {
		_ = mgr.Delete(true)
	})

	if err := IncusExec("launch", "coi-default", name); err != nil {
		t.Fatalf("launch container: %v", err)
	}
	time.Sleep(3 * time.Second)

	// Create a directory with spaces inside the container.
	spacedDir := "/tmp/dir with spaces"
	if err := mgr.ExecArgs([]string{"mkdir", "-p", spacedDir}, ExecCommandOptions{}); err != nil {
		t.Fatalf("mkdir %q: %v", spacedDir, err)
	}

	// Create a file with spaces inside that directory.
	spacedFile := spacedDir + "/file with spaces.txt"
	if err := mgr.ExecArgs([]string{"touch", spacedFile}, ExecCommandOptions{}); err != nil {
		t.Fatalf("touch %q: %v", spacedFile, err)
	}

	// DirExists must return true for the directory.
	exists, err := mgr.DirExists(spacedDir)
	if err != nil {
		t.Fatalf("DirExists(%q): unexpected error: %v", spacedDir, err)
	}
	if !exists {
		t.Errorf("DirExists(%q) = false, want true", spacedDir)
	}

	// FileExists must return true for the file.
	exists, err = mgr.FileExists(spacedFile)
	if err != nil {
		t.Fatalf("FileExists(%q): unexpected error: %v", spacedFile, err)
	}
	if !exists {
		t.Errorf("FileExists(%q) = false, want true", spacedFile)
	}

	// DirExists must return false for a non-existent path.
	exists, err = mgr.DirExists("/tmp/no such dir")
	if err != nil {
		t.Fatalf("DirExists(nonexistent): unexpected error: %v", err)
	}
	if exists {
		t.Errorf("DirExists(nonexistent) = true, want false")
	}

	// FileExists must return false for a non-existent path.
	exists, err = mgr.FileExists("/tmp/no such file.txt")
	if err != nil {
		t.Fatalf("FileExists(nonexistent): unexpected error: %v", err)
	}
	if exists {
		t.Errorf("FileExists(nonexistent) = true, want false")
	}
}
