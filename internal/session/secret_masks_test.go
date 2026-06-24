package session

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func relsOf(masks []secretMask) []string {
	out := make([]string, 0, len(masks))
	for _, m := range masks {
		out = append(out, m.relPath)
	}
	sort.Strings(out)
	return out
}

func isDirOf(masks []secretMask, rel string) (bool, bool) {
	for _, m := range masks {
		if m.relPath == rel {
			return m.isDir, true
		}
	}
	return false, false
}

func TestExpandSecretPaths(t *testing.T) {
	ws := t.TempDir()
	// .env (file), key.pem + other.pem (glob), secrets/ dir with a file inside,
	// nested/deep.pem (not matched by top-level *.pem), and a plain file.
	writeFile := func(rel, content string) {
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(".env", "SECRET=1")
	writeFile("key.pem", "k")
	writeFile("other.pem", "k")
	writeFile("secrets/db", "pw")
	writeFile("nested/deep.pem", "k")
	writeFile("README.md", "x")

	masks := ExpandSecretPaths(ws, []string{".env", "*.pem", "secrets/**"})
	got := relsOf(masks)
	want := []string{".env", "key.pem", "other.pem", "secrets"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want, got)
			break
		}
	}
	// secrets is the masked directory; nested/deep.pem must NOT be matched by the
	// non-recursive *.pem (only top-level).
	if dir, ok := isDirOf(masks, "secrets"); !ok || !dir {
		t.Errorf("secrets should be masked as a directory")
	}
	if dir, ok := isDirOf(masks, ".env"); !ok || dir {
		t.Errorf(".env should be masked as a file")
	}
	for _, m := range masks {
		if m.relPath == "nested/deep.pem" || m.relPath == "README.md" {
			t.Errorf("unexpected match: %s", m.relPath)
		}
	}
}

func TestExpandSecretPaths_SkipsMissingAndSymlinks(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "real-secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "real-secret"), filepath.Join(ws, "link.pem")); err != nil {
		t.Fatal(err)
	}

	// A nonexistent path and a symlink must both be skipped (you can't mask what
	// isn't there, and we never follow symlinks).
	masks := ExpandSecretPaths(ws, []string{"does-not-exist", "link.pem", "*.pem"})
	if len(masks) != 0 {
		t.Errorf("expected no masks (missing + symlink only), got %v", relsOf(masks))
	}
}

func TestExpandSecretPaths_RejectsTraversal(t *testing.T) {
	ws := t.TempDir()
	// Even if the parent exists, a traversal pattern must be rejected by
	// validateRelPath (no masking outside the workspace).
	masks := ExpandSecretPaths(ws, []string{"../etc/passwd", "..", ""})
	if len(masks) != 0 {
		t.Errorf("traversal/empty patterns must be rejected, got %v", relsOf(masks))
	}
}

func TestMaskDeviceName(t *testing.T) {
	cases := map[string]string{
		".env":      "mask-env",
		"key.pem":   "mask-keypem",
		"secrets":   "mask-secrets",
		"a/b/c.pem": "mask-a-b-cpem",
	}
	for in, want := range cases {
		if got := maskDeviceName(in); got != want {
			t.Errorf("maskDeviceName(%q) = %q, want %q", in, got, want)
		}
		// Must be distinct from the read-only protect-* device name.
		if maskDeviceName(in) == pathToDeviceName(in) {
			t.Errorf("mask and protect device names collide for %q", in)
		}
	}
}
