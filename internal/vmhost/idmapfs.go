package vmhost

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// fuseSuperMagic is the statfs f_type shared by the whole FUSE family. Both
// `fuse` and `fuseblk` report it, and `fuseblk` is what OrbStack's macOS share
// reports — the mount at the centre of #678 and #683. virtiofs also reports it
// (the kernel's virtio_fs is FUSE-based and sets no magic of its own).
const fuseSuperMagic = 0x65735546

// v9fsSuperMagic is the statfs f_type of 9p mounts (V9FS_MAGIC). 9p has no
// idmapped-mount support in the kernel at all, and unlike FUSE there is no
// per-mount negotiation that could make it work.
const v9fsSuperMagic = 0x01021997

// SourceBlocksIdmappedMounts reports whether a disk device sourced at path
// should skip Incus's shift=true (idmapped) mount and use raw.idmap instead.
//
// Why a filesystem check rather than a kernel capability check: the exact
// question Incus asks is open_tree(OPEN_TREE_CLONE) followed by
// mount_setattr(MOUNT_ATTR_IDMAP), and that requires root. can_idmap_mount()
// tests ns_capable(fs_userns, CAP_SYS_ADMIN) before it ever consults
// FS_ALLOW_IDMAP, so an unprivileged caller gets EPERM at open_tree and cannot
// tell "this filesystem refuses idmapping" from "you aren't allowed to ask".
// coi runs unprivileged against incusd, and Incus exposes no "can you idmap
// this path" API. environment.kernel_features doesn't help either: it is
// kernel-wide, and the kernels in question DO support idmapped mounts, just
// not on this particular filesystem.
//
// Why FUSE specifically: a FUSE filesystem only permits idmapped mounts when
// its server negotiated FUSE_ALLOW_IDMAP at mount time, and that negotiation
// is not visible from outside the mount. So "source is FUSE" does not decide
// the question in general. Treating it as "skip shift" is wrong only in the
// harmless direction: raw.idmap produces a correct mapping either way, so the
// cost of a false positive is skipping an optimization, while the cost of a
// false negative is a container that fails to start (#678) or, on OrbStack
// 2.2.2 and later, one that starts with a silently unwritable workspace (#683).
//
// This also generalizes past OrbStack. Lima and Colima share the host
// filesystem the same way, which is what the hardcoded VM-host list in this
// package has been approximating: a property of the actual source path beats a
// guess about the VM around it. 9p is in the table for the same reason: a Lima
// instance with mountType: 9p carries none of detect()'s markers (no virtiofs
// mount, no "lima" user), so HandlesUIDMapping does not route around shift for
// it — and where a real Colima/Lima IS detected, that veto still wins inside
// decideUIDMapping, so listing 9p costs those setups nothing.
//
// A path that does not exist yet is judged by its nearest existing ancestor:
// the callers decide shift BEFORE the code that MkdirAll's missing writable
// mount sources, and the ancestor's filesystem is exactly where that MkdirAll
// will land. Any other statfs failure returns false, leaving the caller on its
// existing behaviour and the reactive fallback (#679) as the backstop.
func SourceBlocksIdmappedMounts(path string) bool {
	var st unix.Statfs_t
	err := unix.Statfs(path, &st)
	for errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
		err = unix.Statfs(path, &st)
	}
	if err != nil {
		return false
	}
	// The conversion is load-bearing cross-platform: Statfs_t.Type is int64 on
	// linux but uint32 on darwin, which this repo also builds for.
	return magicBlocksIdmappedMounts(int64(st.Type)) //nolint:unconvert
}

// FirstBlockingSource returns the first of paths that SourceBlocksIdmappedMounts
// rejects, or "" if none do. Empty entries are skipped.
//
// shift is per-device but raw.idmap is per-container, and Incus won't combine
// them, so this is a whole-container decision: one FUSE-backed source among the
// disk devices means every device on that container has to go the raw.idmap
// route. Returning the offending path rather than a bool lets the caller name
// it, since with several mounts configured "which one" is not obvious.
func FirstBlockingSource(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if SourceBlocksIdmappedMounts(p) {
			return p
		}
	}
	return ""
}

// magicBlocksIdmappedMounts is the pure decision behind
// SourceBlocksIdmappedMounts, split out so the filesystem table is testable
// without needing a host that has one of these mounted.
func magicBlocksIdmappedMounts(magic int64) bool {
	return magic == fuseSuperMagic || magic == v9fsSuperMagic
}
