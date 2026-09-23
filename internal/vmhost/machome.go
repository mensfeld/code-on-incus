package vmhost

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HostToolConfigDir returns the host directory a tool's credentials/config
// should be seeded from for a container launched from THIS process.
//
// Normally that is simply <guestHome>/<configDirName>. But on macOS coi runs
// inside a Colima/Lima/OrbStack Linux VM (see install.sh — a native darwin
// build is refused), so os.UserHomeDir() is the *guest* home (e.g.
// /home/lima), not the Mac user's home. The Mac home is instead shared into
// the guest over virtiofs/9p under /Users. A fresh guest home has no
// ~/.claude, so the default path finds nothing to seed and the container boots
// an unauthenticated tool. When we detect a Mac VM and the guest's own config
// dir is empty, fall back to the same config dir under the shared Mac home so
// `coi shell` picks up the Mac user's real credentials. On Linux (KindUnknown)
// this is a no-op and returns the guest path unchanged.
func HostToolConfigDir(guestHome, configDirName string) string {
	guestPath := filepath.Join(guestHome, configDirName)
	if configDirName == "" || Detect() == KindUnknown {
		return guestPath
	}
	return ResolveHostConfigDir(guestPath, macHostConfigCandidates(configDirName), dirHasEntries)
}

// ResolveHostConfigDir chooses between the guest's own config dir and config
// dirs found under a shared Mac home. The guest path wins whenever it already
// holds real config (the user authenticated inside the VM); otherwise the
// first non-empty shared-home candidate is used; if nothing is populated the
// guest path is returned unchanged so callers behave exactly as before (the
// seeding step then simply skips the missing files). Pure and injectable so
// the selection logic is unit-testable without a real filesystem.
func ResolveHostConfigDir(guestPath string, candidates []string, nonEmpty func(string) bool) string {
	if nonEmpty(guestPath) {
		return guestPath
	}
	for _, c := range candidates {
		if c != guestPath && nonEmpty(c) {
			return c
		}
	}
	return guestPath
}

// macHostConfigCandidates builds the list of <configDirName> paths to look for
// under the Mac homes shared into this guest. A shared mount is usually the Mac
// home itself (/Users/alice), so <mount>/<configDirName> is the candidate; but
// a setup that shares the whole /Users parent is also handled by descending one
// level (/Users/*/<configDirName>). Deduplicated, order-stable.
func macHostConfigCandidates(configDirName string) []string {
	mountsBytes, _ := os.ReadFile("/proc/mounts")
	var out []string
	seen := make(map[string]bool)
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, m := range macHostMounts(string(mountsBytes)) {
		add(filepath.Join(m, configDirName))
		entries, err := os.ReadDir(m)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				add(filepath.Join(m, e.Name(), configDirName))
			}
		}
	}
	return out
}

// macHostMounts returns the mountpoints under /Users that a Mac VM shares into
// the guest via virtiofs (Lima/Colima/OrbStack) or 9p (Lima with
// mountType: 9p) — i.e. where the Mac user's real home is visible from inside
// the VM. Pure over a /proc/mounts blob; sorted and deduplicated for stable
// candidate ordering.
func macHostMounts(mounts string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// /proc/mounts fields: source mountpoint fstype ...
		mountpoint, fstype := fields[1], fields[2]
		if fstype != "virtiofs" && fstype != "9p" {
			continue
		}
		if mountpoint != "/Users" && !strings.HasPrefix(mountpoint, "/Users/") {
			continue
		}
		if !seen[mountpoint] {
			seen[mountpoint] = true
			out = append(out, mountpoint)
		}
	}
	sort.Strings(out)
	return out
}

// dirHasEntries reports whether a directory exists and contains at least one
// entry — the "has real config" signal for ResolveHostConfigDir.
func dirHasEntries(p string) bool {
	entries, err := os.ReadDir(p)
	return err == nil && len(entries) > 0
}
