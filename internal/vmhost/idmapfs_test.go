package vmhost

import (
	"path/filepath"
	"testing"
)

// TestMagicBlocksIdmappedMounts guards the filesystem table behind the #683
// proactive shift skip. The FUSE magic is the one that matters (OrbStack's Mac
// share reports fuseblk); everything else must fall through so a normal
// workspace keeps using shift.
func TestMagicBlocksIdmappedMounts(t *testing.T) {
	cases := []struct {
		name  string
		magic int64
		want  bool
	}{
		{"fuse / fuseblk", 0x65735546, true},
		{"btrfs", 0x9123683e, false},
		{"tmpfs", 0x01021994, false},
		{"ext4", 0xef53, false},
		{"xfs", 0x58465342, false},
		{"overlayfs", 0x794c7630, false},
		{"zfs", 0x2fc12fc1, false},
		{"9p", 0x01021997, false}, // deliberately not listed; see SourceBlocksIdmappedMounts
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

// TestSourceBlocksIdmappedMountsUnreadablePath pins the fail-open behaviour: a
// path that can't be stat'd must not silently turn shift off for everyone, it
// must leave the caller on its existing behaviour with the reactive fallback
// underneath.
func TestSourceBlocksIdmappedMountsUnreadablePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "workspace")
	if SourceBlocksIdmappedMounts(missing) {
		t.Errorf("a path that cannot be stat'd must return false, got true for %s", missing)
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
