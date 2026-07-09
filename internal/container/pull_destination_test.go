package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPullFileRefusesDirectoryDestination verifies PullFile's fail-fast guard:
// an existing directory at the destination is refused before anything is
// transferred. The manager points at a container that does not exist, so a
// passing test also proves incus is never contacted.
func TestPullFileRefusesDirectoryDestination(t *testing.T) {
	mgr := NewManager("coi-test-no-such-container")
	dest := t.TempDir()

	err := mgr.PullFile("/tmp/anything.txt", dest)
	if err == nil {
		t.Fatal("expected error for existing directory destination, got nil")
	}
	if !strings.Contains(err.Error(), "existing directory") {
		t.Fatalf("expected 'existing directory' error, got: %v", err)
	}
}

// TestPullFileDirectoryGuardKeepsContents verifies the guard leaves the
// destination directory and its contents untouched.
func TestPullFileDirectoryGuardKeepsContents(t *testing.T) {
	mgr := NewManager("coi-test-no-such-container")
	dest := t.TempDir()
	sentinel := filepath.Join(dest, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.PullFile("/tmp/anything.txt", dest); err == nil {
		t.Fatal("expected error for existing directory destination, got nil")
	}

	data, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel file was lost: %v", err)
	}
	if string(data) != "keep me" {
		t.Fatalf("sentinel content changed: %q", data)
	}
}
