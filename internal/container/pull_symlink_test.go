package container

import (
	"os"
	"path/filepath"
	"testing"
)

// sanitizePulledSymlinks must drop symlinks whose target escapes the pulled tree
// (absolute paths, or `..` traversal) while preserving intra-tree relative links
// and regular files.
func TestSanitizePulledSymlinks(t *testing.T) {
	root := t.TempDir()

	// Regular file (must survive).
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Safe intra-tree relative symlink (must survive).
	if err := os.Symlink("real.txt", filepath.Join(root, "safe-link")); err != nil {
		t.Fatal(err)
	}
	// A nested dir with a safe relative link back up into the tree (must survive).
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real.txt", filepath.Join(sub, "up-link")); err != nil {
		t.Fatal(err)
	}
	// Unsafe: absolute host path (must be dropped).
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "abs-evil")); err != nil {
		t.Fatal(err)
	}
	// Unsafe: relative traversal escaping the root (must be dropped).
	if err := os.Symlink("../../../../etc/shadow", filepath.Join(sub, "esc-evil")); err != nil {
		t.Fatal(err)
	}

	if err := sanitizePulledSymlinks(root); err != nil {
		t.Fatalf("sanitizePulledSymlinks: %v", err)
	}

	mustExist := []string{"real.txt", "safe-link", filepath.Join("sub", "up-link")}
	for _, p := range mustExist {
		if _, err := os.Lstat(filepath.Join(root, p)); err != nil {
			t.Errorf("expected %s to survive sanitization: %v", p, err)
		}
	}
	mustBeGone := []string{"abs-evil", filepath.Join("sub", "esc-evil")}
	for _, p := range mustBeGone {
		if _, err := os.Lstat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("expected unsafe symlink %s to be removed, stat err=%v", p, err)
		}
	}
}

func TestIsWithinDir(t *testing.T) {
	cases := []struct {
		dir, path string
		want      bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b", "/a/b/c", true},
		{"/a/b", "/a/b/c/d", true},
		{"/a/b", "/a", false},
		{"/a/b", "/a/c", false},
		{"/a/b", "/etc/passwd", false},
		{"/a/b", "/a/bc", false}, // sibling with shared prefix must not count as inside
	}
	for _, c := range cases {
		if got := isWithinDir(c.dir, c.path); got != c.want {
			t.Errorf("isWithinDir(%q, %q) = %v, want %v", c.dir, c.path, got, c.want)
		}
	}
}
