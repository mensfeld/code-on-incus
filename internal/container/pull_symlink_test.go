package container

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// sanitizePulledTree must drop every symlink and special file from an untrusted
// pulled tree while preserving regular files and directories. Dropping ALL
// symlinks (not just ones whose target escapes lexically) is what closes the
// chained-symlink bypass, so the chained case below must be removed too.
func TestSanitizePulledTree(t *testing.T) {
	root := t.TempDir()

	// Regular file (must survive).
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested dir with a regular file (both must survive).
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Safe intra-tree relative symlink — previously preserved, now dropped.
	if err := os.Symlink("real.txt", filepath.Join(root, "safe-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real.txt", filepath.Join(sub, "up-link")); err != nil {
		t.Fatal(err)
	}
	// Absolute host path (dropped).
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "abs-evil")); err != nil {
		t.Fatal(err)
	}
	// Relative traversal escaping the root (dropped).
	if err := os.Symlink("../../../../etc/shadow", filepath.Join(sub, "esc-evil")); err != nil {
		t.Fatal(err)
	}
	// Chained bypass: `b -> .` plus `evil -> b/../escape` both pass a lexical
	// within-root check yet escape at runtime. Dropping all symlinks kills it.
	if err := os.Symlink(".", filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b/../../../../etc/hosts", filepath.Join(root, "chain-evil")); err != nil {
		t.Fatal(err)
	}
	// Special file (FIFO) — dropped. Skip the assertion if mkfifo is unsupported.
	fifo := filepath.Join(root, "pipe")
	fifoCreated := syscall.Mkfifo(fifo, 0o644) == nil

	sanitizePulledTree(root)

	mustExist := []string{"real.txt", "sub", filepath.Join("sub", "nested.txt")}
	for _, p := range mustExist {
		if _, err := os.Lstat(filepath.Join(root, p)); err != nil {
			t.Errorf("expected %s to survive sanitization: %v", p, err)
		}
	}

	mustBeGone := []string{
		"safe-link",
		filepath.Join("sub", "up-link"),
		"abs-evil",
		filepath.Join("sub", "esc-evil"),
		"b",
		"chain-evil",
	}
	if fifoCreated {
		mustBeGone = append(mustBeGone, "pipe")
	}
	for _, p := range mustBeGone {
		if _, err := os.Lstat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("expected non-regular entry %s to be removed, stat err=%v", p, err)
		}
	}
}

// sanitizePulledTree is best-effort: an unreadable subdirectory must not abort
// the sweep — entries it can reach are still cleaned, and it never panics.
func TestSanitizePulledTreeBestEffort(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based unreadable-dir case is meaningless as root")
	}
	root := t.TempDir()

	if err := os.Symlink("/etc/passwd", filepath.Join(root, "top-evil")); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/shadow", filepath.Join(locked, "inner-evil")); err != nil {
		t.Fatal(err)
	}
	// Make the subdir unreadable so WalkDir errors on its children.
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(locked, 0o755) }()

	// Must not panic and must still drop what it can reach (the top-level link).
	sanitizePulledTree(root)

	if _, err := os.Lstat(filepath.Join(root, "top-evil")); !os.IsNotExist(err) {
		t.Errorf("reachable symlink should still be dropped despite unreadable subdir, stat err=%v", err)
	}
}
