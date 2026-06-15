package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// TrustEnvVar, when set to a truthy value (1/true/yes), bypasses the
// untrusted-mount gate. Intended for CI/automation where the operator already
// controls the config and prompting is impractical. It is safe because only the
// invoking shell can set it — a cloned repo's config cannot.
const TrustEnvVar = "COI_TRUST_ALL"

// trustStore is the on-disk format: a map from an untrusted config file's
// absolute path to the fingerprint of the escaping mounts approved from it.
type trustStore struct {
	Mounts map[string]string `toml:"mounts"`
}

// TrustAllViaEnv reports whether the COI_TRUST_ALL opt-out is set.
func TrustAllViaEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(TrustEnvVar))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// TrustStorePath returns the trust store location (~/.coi/trust.toml). The store
// lives in the trusted user scope; inside a container .coi is a read-only mount,
// so an agent cannot forge entries.
func TrustStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".coi", "trust.toml"), nil
}

func loadTrustStore() (map[string]string, error) {
	path, err := TrustStorePath()
	if err != nil {
		return nil, err
	}
	var ts trustStore
	if _, err := toml.DecodeFile(path, &ts); err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if ts.Mounts == nil {
		ts.Mounts = map[string]string{}
	}
	return ts.Mounts, nil
}

func saveTrustStore(m map[string]string) error {
	path, err := TrustStorePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(trustStore{Mounts: m})
}

// isWithinWorkspace reports whether hostPath is the workspace dir or nested in
// it. Both arguments should be absolute.
func isWithinWorkspace(workspace, hostPath string) bool {
	workspace = filepath.Clean(workspace)
	hostPath = filepath.Clean(hostPath)
	if hostPath == workspace {
		return true
	}
	rel, err := filepath.Rel(workspace, hostPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// realPath resolves symlinks in p as far as it can: it resolves the longest
// existing prefix with filepath.EvalSymlinks and re-appends the non-existent
// remainder, following a dangling symlink via Readlink. This makes the
// workspace-escape check robust against an in-workspace symlink that points
// outside — a purely lexical check would miss it, but Incus follows the link at
// mount time. Best-effort and bounded against symlink cycles; falls back to the
// cleaned lexical path if nothing resolves.
func realPath(p string) string {
	if p == "" {
		return ""
	}
	cur := filepath.Clean(p)
	var rest []string
	rejoin := func(base string) string {
		full := base
		for i := len(rest) - 1; i >= 0; i-- {
			full = filepath.Join(full, rest[i])
		}
		return full
	}
	for i := 0; i < 256; i++ { // bound: defend against symlink cycles
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return rejoin(resolved)
		}
		// cur doesn't fully resolve. If it is itself a (possibly dangling)
		// symlink, follow its target and retry.
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			if target, rerr := os.Readlink(cur); rerr == nil {
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(cur), target)
				}
				cur = filepath.Clean(target)
				continue
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur { // reached root without resolving — lexical fallback
			return rejoin(cur)
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
	return rejoin(cur)
}

// hostEscapesWorkspace reports whether hostPath resolves outside the workspace,
// resolving symlinks on both so an in-workspace symlink to an outside directory
// cannot pass as contained.
func hostEscapesWorkspace(workspace, hostPath string) bool {
	return !isWithinWorkspace(realPath(workspace), realPath(hostPath))
}

// escapingUntrustedMounts returns the untrusted mounts whose host path resolves
// outside the workspace — the set that must be trusted before use.
func escapingUntrustedMounts(mounts []MountEntry, workspace string) []MountEntry {
	var out []MountEntry
	for _, m := range mounts {
		if m.Untrusted && m.HostPath != "" && hostEscapesWorkspace(workspace, m.HostPath) {
			out = append(out, m)
		}
	}
	return out
}

// MountFingerprint returns a stable sha256 over the given mounts' host path,
// container path and read-only flag (order-independent). It identifies exactly
// the set of host mounts a user approved: adding, removing, or changing any of
// these mounts changes the hash and re-arms the trust prompt. Unrelated config
// edits (base image, network mode, etc.) do not affect it.
func MountFingerprint(mounts []MountEntry) string {
	lines := make([]string, 0, len(mounts))
	for _, m := range mounts {
		// %q quotes/escapes each field so a '|' or newline embedded in a path
		// cannot make two different mount sets serialize identically (the
		// quoted forms are unambiguous), which would otherwise defeat the
		// "changing the mount set re-arms trust" guarantee.
		lines = append(lines, fmt.Sprintf("%q|%q|%t", m.HostPath, m.ContainerPath, m.Readonly))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// FilterTrustedMounts removes untrusted mounts that escape the workspace unless
// their source config file's escaping-mount fingerprint is recorded as trusted
// (or COI_TRUST_ALL is set). Returns the filtered config and the dropped mounts
// (so the caller can warn). Trusted-scope mounts and in-workspace mounts are
// never gated.
func FilterTrustedMounts(mc *MountConfig, workspace string) (*MountConfig, []MountEntry) {
	if mc == nil {
		return mc, nil
	}
	escaping := escapingUntrustedMounts(mc.Mounts, workspace)
	if len(escaping) == 0 || TrustAllViaEnv() {
		return mc, nil
	}

	store, _ := loadTrustStore() // missing/unreadable store → nothing trusted

	bySource := map[string][]MountEntry{}
	for _, m := range escaping {
		bySource[m.SourcePath] = append(bySource[m.SourcePath], m)
	}
	trustedSource := map[string]bool{}
	for src, ms := range bySource {
		trustedSource[src] = store[src] != "" && store[src] == MountFingerprint(ms)
	}

	kept := &MountConfig{Mounts: make([]MountEntry, 0, len(mc.Mounts))}
	var dropped []MountEntry
	for _, m := range mc.Mounts {
		if m.Untrusted && m.HostPath != "" && hostEscapesWorkspace(workspace, m.HostPath) && !trustedSource[m.SourcePath] {
			dropped = append(dropped, m)
			continue
		}
		kept.Mounts = append(kept.Mounts, m)
	}
	return kept, dropped
}

// TrustEscapingMounts records trust for every source config file that declares
// escaping untrusted mounts in mc, pinning the current escaping-mount
// fingerprint. Returns the set of source paths that were trusted (empty if there
// is nothing to trust).
func TrustEscapingMounts(mc *MountConfig, workspace string) ([]string, error) {
	if mc == nil {
		return nil, nil
	}
	escaping := escapingUntrustedMounts(mc.Mounts, workspace)
	if len(escaping) == 0 {
		return nil, nil
	}
	bySource := map[string][]MountEntry{}
	for _, m := range escaping {
		bySource[m.SourcePath] = append(bySource[m.SourcePath], m)
	}
	store, err := loadTrustStore()
	if err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(bySource))
	for src, ms := range bySource {
		store[src] = MountFingerprint(ms)
		sources = append(sources, src)
	}
	sort.Strings(sources)
	if err := saveTrustStore(store); err != nil {
		return nil, err
	}
	return sources, nil
}

// UntrustSources removes trust entries for the given source paths. Returns the
// number of entries removed.
func UntrustSources(sources []string) (int, error) {
	store, err := loadTrustStore()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range sources {
		if _, ok := store[s]; ok {
			delete(store, s)
			n++
		}
	}
	if n > 0 {
		if err := saveTrustStore(store); err != nil {
			return 0, err
		}
	}
	return n, nil
}

// ListTrusted returns the current trust store (path -> fingerprint).
func ListTrusted() (map[string]string, error) {
	return loadTrustStore()
}
