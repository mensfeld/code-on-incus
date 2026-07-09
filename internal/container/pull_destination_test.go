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

// TestPullDirectoryRefusesExistingDestination verifies PullDirectory's
// fail-fast guard: any existing destination (directory or file) is refused
// before anything is transferred, and nothing is deleted. This is the
// regression guard for the data-wipe bug where an existing destination was
// recursively cleared with os.RemoveAll. The nonexistent container proves
// incus is never contacted.
func TestPullDirectoryRefusesExistingDestination(t *testing.T) {
	mgr := NewManager("coi-test-no-such-container")
	dest := t.TempDir()
	sentinel := filepath.Join(dest, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Existing directory destination.
	err := mgr.PullDirectory("/root/.claude", dest)
	if err == nil {
		t.Fatal("expected error for existing directory destination, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel file was lost: %v", err)
	}

	// Existing file destination.
	err = mgr.PullDirectory("/root/.claude", sentinel)
	if err == nil {
		t.Fatal("expected error for existing file destination, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
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
