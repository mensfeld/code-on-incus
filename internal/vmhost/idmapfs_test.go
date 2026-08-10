package vmhost

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMagicBlocksIdmappedMounts guards the filesystem table behind the #683
// proactive shift skip. The FUSE magic is the one that matters (OrbStack's Mac
// share reports fuseblk); 9p is listed because it has no idmapped-mount support
// and its Lima mountType: 9p users carry none of detect()'s markers; everything
// else must fall through so a normal workspace keeps using shift.
func TestMagicBlocksIdmappedMounts(t *testing.T) {
	cases := []struct {
		name  string
		magic int64
		want  bool
	}{
		{"fuse / fuseblk", 0x65735546, true},
		{"9p", 0x01021997, true},
		{"btrfs", 0x9123683e, false},
		{"tmpfs", 0x01021994, false},
		{"ext4", 0xef53, false},
		{"xfs", 0x58465342, false},
		{"overlayfs", 0x794c7630, false},
		{"zfs", 0x2fc12fc1, false},
		{"zero", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := magicBlocksIdmappedMounts(c.magic); got != c.want {
				t.Errorf("magicBlocksIdmappedMounts(%#x) = %v, want %v", c.magic, got, c.want)
			}
		})
	}
}

// TestSourceBlocksIdmappedMountsMissingPathUsesAncestor pins the ENOENT
// behaviour: a path that does not exist yet is judged by its nearest existing
// ancestor, because the callers decide shift before the code that MkdirAll's
// missing writable mount sources — the ancestor's filesystem is where that
// MkdirAll will land. On a non-FUSE temp dir the answer is false either way;
// the point is that the walk terminates and agrees with the ancestor.
func TestSourceBlocksIdmappedMountsMissingPathUsesAncestor(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "not", "created", "yet")
	if got, want := SourceBlocksIdmappedMounts(missing), SourceBlocksIdmappedMounts(dir); got != want {
		t.Errorf("missing path should be judged by its existing ancestor: got %v, ancestor %v", got, want)
	}

	// A file in the middle of the path (ENOTDIR) walks up the same way.
	file := filepath.Join(dir, "plainfile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(file, "child")
	if got, want := SourceBlocksIdmappedMounts(under), SourceBlocksIdmappedMounts(dir); got != want {
		t.Errorf("path under a plain file should be judged by its ancestor: got %v, ancestor %v", got, want)
	}
}

// TestSourceBlocksIdmappedMountsUnreadablePath pins the fail-open behaviour for
// errors other than a missing path: a path that can't be stat'd for permission
// reasons must not silently turn shift off for everyone, it must leave the
// caller on its existing behaviour with the reactive fallback underneath.
func TestSourceBlocksIdmappedMountsUnreadablePath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission-based statfs failure not constructible")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	inside := filepath.Join(locked, "workspace")
	if SourceBlocksIdmappedMounts(inside) {
		t.Errorf("an EACCES path must fail open (return false), got true for %s", inside)
	}
}

// TestSourceBlocksIdmappedMountsRealDir checks the syscall path actually runs
// and classifies an ordinary directory. t.TempDir() is on whatever the test
// host uses for temp (tmpfs, ext4, btrfs, overlay in a container) — none of
// which are FUSE, so this holds anywhere CI might run.
func TestSourceBlocksIdmappedMountsRealDir(t *testing.T) {
	if SourceBlocksIdmappedMounts(t.TempDir()) {
		t.Error("an ordinary temp directory must not be classified as FUSE-backed")
	}
}

// TestFirstBlockingSource covers the list behaviour: empty entries are skipped,
// and a set of ordinary directories yields no blocker. The positive case needs
// a real FUSE mount, which a CI host won't have, so the classification itself
// is covered by TestMagicBlocksIdmappedMounts instead.
func TestFirstBlockingSource(t *testing.T) {
	dir := t.TempDir()

	if got := FirstBlockingSource(); got != "" {
		t.Errorf("no sources should yield no blocker, got %q", got)
	}
	if got := FirstBlockingSource("", "", ""); got != "" {
		t.Errorf("empty sources should be skipped, got %q", got)
	}
	if got := FirstBlockingSource(dir, "", dir); got != "" {
		t.Errorf("ordinary directories should not block, got %q", got)
	}
}
