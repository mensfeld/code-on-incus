package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// git.readonly locks the container commit identity so the agent cannot overwrite
// it. `git config --global` writes ~/.gitconfig via lock-file + rename, and the
// container home is writable, so file permissions alone don't stop it. Instead COI
// generates a host-side gitconfig holding the identity and mounts it READ-ONLY at
// the container's ~/.gitconfig — a rename over a mount point fails, so the write is
// blocked at the filesystem layer.
//
// NB: this locks the WHOLE global gitconfig, not only user.name/email — any
// `git config --global …` in the container fails read-only. The agent uses
// per-repo (`--local`) config or env for anything else.
//
// This runs at the git-identity step of Setup, AFTER the container's real home is
// resolved (result.HomeDir — /home/<code> or /root when the image has no code
// user), so the mount always lands where git actually reads it.

const gitReadonlyDeviceName = "git-identity"

// gitReadonlyMounter is the slice of *container.Manager this needs: remove any
// stale device from a previous session (persistent reuse) then add the read-only
// disk. Narrow so the path is unit-testable with a stub.
type gitReadonlyMounter interface {
	RemoveDevice(name string) error
	MountDisk(name, source, path string, shift, readonly bool) error
}

// gitConfigQuote renders a git-config value as a quoted string, escaping the
// characters that are special inside git's double-quoted values (and newlines, so
// a stray control character can't produce a broken multi-line entry). Keeps names
// like "coipond-coder[bot]" intact ([ and ] are ordinary in a value).
func gitConfigQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// renderReadonlyGitConfig produces the full ~/.gitconfig content for a locked
// identity: the author fields plus user.useConfigOnly=true (the same fail-closed
// guard COI would otherwise set live), so nothing else needs writing in-container.
func renderReadonlyGitConfig(id GitIdentity) string {
	return fmt.Sprintf(
		"# Managed by coi (git.readonly): mounted read-only, do not edit.\n"+
			"[user]\n\tname = %s\n\temail = %s\n\tuseConfigOnly = true\n",
		gitConfigQuote(strings.TrimSpace(id.Name)),
		gitConfigQuote(strings.TrimSpace(id.Email)),
	)
}

// readonlyGitConfigHostPath returns the host path COI writes the generated config
// to, keyed on the identity so distinct identities never collide and the file is
// stable across sessions. Lives under ~/.coi so it persists for the container's
// life (an Incus disk device references it) and is not swept from /tmp.
func readonlyGitConfigHostPath(hostHome string, id GitIdentity) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(id.Name) + "\x00" + strings.TrimSpace(id.Email)))
	name := hex.EncodeToString(sum[:])[:16] + ".gitconfig"
	return filepath.Join(hostHome, ".coi", "git-identity", name)
}

// writeReadonlyGitConfigHostFile writes the generated config to the host and
// returns its path.
func writeReadonlyGitConfigHostFile(id GitIdentity) (string, error) {
	hostHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve host home: %w", err)
	}
	hostPath := readonlyGitConfigHostPath(hostHome, id)
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return "", fmt.Errorf("failed to create git.readonly dir: %w", err)
	}
	// 0644 so the container's code user can read it regardless of uid shift.
	if err := os.WriteFile(hostPath, []byte(renderReadonlyGitConfig(id)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write git.readonly config: %w", err)
	}
	return hostPath, nil
}

// SetupGitIdentityReadonly locks the identity by mounting a generated gitconfig
// read-only at homeDir/.gitconfig. homeDir MUST be the container's resolved home
// (/home/<code> or /root). The caller treats a returned error as fatal (fail
// closed): if the user asked to lock the identity and we cannot, the session must
// not proceed with a writable one.
func SetupGitIdentityReadonly(mgr gitReadonlyMounter, homeDir string, id GitIdentity) error {
	hostPath, err := writeReadonlyGitConfigHostFile(id)
	if err != nil {
		return err
	}
	containerPath := filepath.Clean(homeDir + "/.gitconfig")
	// Remove any device left by a previous session (persistent reuse) before
	// re-adding; RemoveDevice is a no-op when absent.
	_ = mgr.RemoveDevice(gitReadonlyDeviceName)
	if err := mgr.MountDisk(gitReadonlyDeviceName, hostPath, containerPath, false, true); err != nil {
		return fmt.Errorf("failed to mount read-only git identity at %s: %w", containerPath, err)
	}
	return nil
}
