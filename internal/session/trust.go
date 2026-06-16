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
// untrusted-resource gate. Intended for CI/automation where the operator already
// controls the config and prompting is impractical. It is safe because only the
// invoking shell can set it — a cloned repo's config cannot.
const TrustEnvVar = "COI_TRUST_ALL"

// trustStore is the on-disk format: a map from an untrusted config file's
// absolute path to a single fingerprint covering ALL host-affecting resources it
// declares that need approval — its escaping mounts AND its forwarded sockets.
// `coi trust` approves a source for everything it declares; changing any gated
// mount or socket re-arms the prompt.
type trustStore struct {
	Approvals map[string]string `toml:"approvals"`
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
	if ts.Approvals == nil {
		ts.Approvals = map[string]string{}
	}
	return ts.Approvals, nil
}

func saveTrustStore(m map[string]string) error {
	path, err := TrustStorePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Write to a temp file in the same dir then rename, so a crash mid-write
	// can't truncate/corrupt the live store (which would silently drop all
	// approvals on the next launch).
	tmp, err := os.CreateTemp(dir, ".trust-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	// os.CreateTemp already creates the file 0600 on Unix, which is what we want.
	if err := toml.NewEncoder(tmp).Encode(trustStore{Approvals: m}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
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
// outside the workspace — the gated set.
func escapingUntrustedMounts(mounts []MountEntry, workspace string) []MountEntry {
	var out []MountEntry
	for _, m := range mounts {
		if m.Untrusted && m.HostPath != "" && hostEscapesWorkspace(workspace, m.HostPath) {
			out = append(out, m)
		}
	}
	return out
}

// untrustedSockets returns the untrusted forwarded sockets — the gated set. A
// forwarded host socket exposes a host capability with no "within workspace"
// notion, so ALL untrusted sockets need approval (not just escaping ones).
func untrustedSockets(sockets []SocketEntry) []SocketEntry {
	var out []SocketEntry
	for _, s := range sockets {
		if s.Untrusted && s.HostPath != "" {
			out = append(out, s)
		}
	}
	return out
}

// sourceFingerprint returns a stable sha256 over the gated mounts and sockets a
// single source declares (order-independent). Adding/removing/changing any gated
// mount or socket changes the hash and re-arms the trust prompt; unrelated config
// edits do not. Lines are type-prefixed and %q-quoted so distinct sets cannot
// collide via embedded separators.
func sourceFingerprint(mounts []MountEntry, sockets []SocketEntry) string {
	lines := make([]string, 0, len(mounts)+len(sockets))
	for _, m := range mounts {
		lines = append(lines, fmt.Sprintf("mount:%q|%q|%t", m.HostPath, m.ContainerPath, m.Readonly))
	}
	for _, s := range sockets {
		lines = append(lines, fmt.Sprintf("socket:%q|%q|%q", s.HostPath, s.ContainerPath, s.EnvVar))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// trustedSources computes, per source path, whether the source's gated mounts
// and sockets are all approved (combined fingerprint matches the store).
func trustedSources(mc *MountConfig, sc *SocketConfig, workspace string, store map[string]string) map[string]bool {
	mountsBySrc := map[string][]MountEntry{}
	if mc != nil {
		for _, m := range escapingUntrustedMounts(mc.Mounts, workspace) {
			mountsBySrc[m.SourcePath] = append(mountsBySrc[m.SourcePath], m)
		}
	}
	socketsBySrc := map[string][]SocketEntry{}
	if sc != nil {
		for _, s := range untrustedSockets(sc.Sockets) {
			socketsBySrc[s.SourcePath] = append(socketsBySrc[s.SourcePath], s)
		}
	}
	srcs := map[string]bool{}
	for src := range mountsBySrc {
		srcs[src] = true
	}
	for src := range socketsBySrc {
		srcs[src] = true
	}
	out := map[string]bool{}
	for src := range srcs {
		out[src] = store[src] != "" && store[src] == sourceFingerprint(mountsBySrc[src], socketsBySrc[src])
	}
	return out
}

// FilterTrusted removes untrusted mounts that escape the workspace and untrusted
// forwarded sockets, unless their source's combined fingerprint is recorded as
// trusted (or COI_TRUST_ALL is set). Returns filtered configs plus the dropped
// mounts/sockets (so the caller can warn). Trusted-scope and in-workspace mounts
// are never gated.
func FilterTrusted(mc *MountConfig, sc *SocketConfig, workspace string) (*MountConfig, []MountEntry, *SocketConfig, []SocketEntry) {
	if TrustAllViaEnv() {
		return mc, nil, sc, nil
	}
	// Cheap exit when there's nothing gated, before loading the store.
	var escMounts []MountEntry
	if mc != nil {
		escMounts = escapingUntrustedMounts(mc.Mounts, workspace)
	}
	var untSockets []SocketEntry
	if sc != nil {
		untSockets = untrustedSockets(sc.Sockets)
	}
	if len(escMounts) == 0 && len(untSockets) == 0 {
		return mc, nil, sc, nil
	}

	store, _ := loadTrustStore() // missing/unreadable store → nothing trusted
	trusted := trustedSources(mc, sc, workspace, store)

	keptMC := mc
	var droppedMounts []MountEntry
	if mc != nil {
		keptMC = &MountConfig{Mounts: make([]MountEntry, 0, len(mc.Mounts))}
		for _, m := range mc.Mounts {
			if m.Untrusted && m.HostPath != "" && hostEscapesWorkspace(workspace, m.HostPath) && !trusted[m.SourcePath] {
				droppedMounts = append(droppedMounts, m)
				continue
			}
			keptMC.Mounts = append(keptMC.Mounts, m)
		}
	}

	keptSC := sc
	var droppedSockets []SocketEntry
	if sc != nil {
		keptSC = &SocketConfig{Sockets: make([]SocketEntry, 0, len(sc.Sockets))}
		for _, s := range sc.Sockets {
			if s.Untrusted && s.HostPath != "" && !trusted[s.SourcePath] {
				droppedSockets = append(droppedSockets, s)
				continue
			}
			keptSC.Sockets = append(keptSC.Sockets, s)
		}
	}
	return keptMC, droppedMounts, keptSC, droppedSockets
}

// TrustSources records trust for every source that declares gated mounts and/or
// sockets in mc/sc, pinning the current combined fingerprint. Returns the set of
// source paths that were trusted.
func TrustSources(mc *MountConfig, sc *SocketConfig, workspace string) ([]string, error) {
	mountsBySrc := map[string][]MountEntry{}
	if mc != nil {
		for _, m := range escapingUntrustedMounts(mc.Mounts, workspace) {
			mountsBySrc[m.SourcePath] = append(mountsBySrc[m.SourcePath], m)
		}
	}
	socketsBySrc := map[string][]SocketEntry{}
	if sc != nil {
		for _, s := range untrustedSockets(sc.Sockets) {
			socketsBySrc[s.SourcePath] = append(socketsBySrc[s.SourcePath], s)
		}
	}
	srcs := map[string]bool{}
	for src := range mountsBySrc {
		srcs[src] = true
	}
	for src := range socketsBySrc {
		srcs[src] = true
	}
	if len(srcs) == 0 {
		return nil, nil
	}
	store, err := loadTrustStore()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(srcs))
	for src := range srcs {
		store[src] = sourceFingerprint(mountsBySrc[src], socketsBySrc[src])
		out = append(out, src)
	}
	sort.Strings(out)
	if err := saveTrustStore(store); err != nil {
		return nil, err
	}
	return out, nil
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

// ListTrusted returns the current trust store (source path -> fingerprint).
func ListTrusted() (map[string]string, error) {
	return loadTrustStore()
}

// UntrustedSourcePaths returns the distinct source config paths of untrusted
// mounts and sockets — the keys under which trust is recorded. `coi untrust` uses
// this to revoke by the exact stored key (resolved at load) rather than
// reconstructing it, which would diverge on symlinked/alias/non-default paths.
func UntrustedSourcePaths(mc *MountConfig, sc *SocketConfig) []string {
	seen := map[string]bool{}
	var out []string
	add := func(src string) {
		if src != "" && !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}
	if mc != nil {
		for _, m := range mc.Mounts {
			if m.Untrusted {
				add(m.SourcePath)
			}
		}
	}
	if sc != nil {
		for _, s := range sc.Sockets {
			if s.Untrusted {
				add(s.SourcePath)
			}
		}
	}
	sort.Strings(out)
	return out
}
