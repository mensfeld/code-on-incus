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
// dir holds no real config, fall back to the same config dir under the shared
// Mac home so `coi shell` picks up the Mac user's real credentials. On Linux
// (KindUnknown) this is a no-op and returns the guest path unchanged.
//
// configFiles are the tool's essential config filenames (e.g.
// [".credentials.json", "settings.json", …]); their presence — not mere
// directory non-emptiness — is what marks a dir as "real config worth
// seeding", so a dir holding only stray files can't shadow the populated one.
// When neither the guest nor any shared home holds one of these files there is
// nothing to seed anywhere, so the guest path is returned unchanged.
func HostToolConfigDir(guestHome, configDirName string, configFiles []string) string {
	guestPath := filepath.Join(guestHome, configDirName)
	if configDirName == "" || Detect() == KindUnknown {
		return guestPath
	}
	hasConfig := func(dir string) bool { return dirHasAnyFile(dir, configFiles) }
	return ResolveHostConfigDir(guestPath, macHostConfigCandidates(configDirName), hasConfig)
}

// ResolveHostConfigDir chooses between the guest's own config dir and config
// dirs found under a shared Mac home. The guest path wins whenever it already
// holds real config (the user authenticated inside the VM); otherwise the
// first shared-home candidate that holds real config is used; if none do the
// guest path is returned unchanged so callers behave exactly as before (the
// seeding step then simply skips the missing files). Pure and injectable so
// the selection logic is unit-testable without a real filesystem.
func ResolveHostConfigDir(guestPath string, candidates []string, hasConfig func(string) bool) string {
	if hasConfig(guestPath) {
		return guestPath
	}
	for _, c := range candidates {
		if c != guestPath && hasConfig(c) {
			return c
		}
	}
	return guestPath
}

// macHostConfigCandidates builds the list of <configDirName> paths to look for
// under the Mac homes shared into this guest, reading the live /proc/mounts.
func macHostConfigCandidates(configDirName string) []string {
	mountsBytes, _ := os.ReadFile("/proc/mounts")
	return buildMacHostConfigCandidates(string(mountsBytes), configDirName, listSubdirs)
}

// buildMacHostConfigCandidates is the pure core of macHostConfigCandidates,
// with directory listing injected for testability. A shared mount is normally
// the Mac home itself (/Users/alice), so <mount>/<configDirName> is the sole
// candidate — no descent, so we neither read the whole home over virtiofs nor
// mistake a nested stray .claude for the real one. Only when the whole /Users
// parent is shared do we descend one level (/Users/*/<configDirName>).
// Deduplicated, order-stable.
func buildMacHostConfigCandidates(mounts, configDirName string, listSubdirs func(string) []string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, m := range macHostMounts(mounts) {
		if m == "/Users" {
			for _, name := range listSubdirs(m) {
				add(filepath.Join(m, name, configDirName))
			}
			continue
		}
		add(filepath.Join(m, configDirName))
	}
	return out
}

// listSubdirs returns the names of the immediate subdirectories of p, or nil
// if p can't be read.
func listSubdirs(p string) []string {
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
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
		// /proc/mounts fields: source mountpoint fstype ...; the mountpoint is
		// octal-escaped by the kernel (space -> \040, etc.), so decode it before
		// use or a Mac home path containing those characters never resolves.
		mountpoint := unescapeMountField(fields[1])
		fstype := fields[2]
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

// mountFieldUnescaper decodes the octal escapes the kernel writes into
// /proc/mounts fields: space, tab, newline, and backslash (see the kernel's
// mangle_path). These are the only characters escaped there.
var mountFieldUnescaper = strings.NewReplacer(
	`\040`, " ",
	`\011`, "\t",
	`\012`, "\n",
	`\134`, `\`,
)

func unescapeMountField(s string) string { return mountFieldUnescaper.Replace(s) }

// dirHasAnyFile reports whether dir contains at least one of the named files —
// the "has real tool config worth seeding" signal for ResolveHostConfigDir.
// Keyed on actual config files rather than mere directory non-emptiness so a
// dir holding only unrelated/stray files does not shadow one with real config.
func dirHasAnyFile(dir string, files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}
