package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/session"
)

// git.readonly locks the container commit identity so the agent cannot overwrite
// it. `git config --global` writes ~/.gitconfig via lock-file + rename, and the
// container home is writable, so file permissions alone don't stop it. Instead COI
// generates a host-side gitconfig holding the identity and mounts it READ-ONLY at
// the container's ~/.gitconfig — a rename over a mount point fails, so the write is
// blocked at the filesystem layer rather than merely discouraged.

// gitConfigQuote renders a git-config value as a quoted string, escaping the two
// characters that are special inside git's double-quoted values. This keeps names
// like "coipond-coder[bot]" intact ([ and ] are ordinary in a value).
func gitConfigQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// renderReadonlyGitConfig produces the full ~/.gitconfig content for a locked
// identity: the author fields plus user.useConfigOnly=true (the same fail-closed
// guard COI would otherwise set live), so nothing else needs writing in-container.
func renderReadonlyGitConfig(id session.GitIdentity) string {
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
func readonlyGitConfigHostPath(homeDir string, id session.GitIdentity) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(id.Name) + "\x00" + strings.TrimSpace(id.Email)))
	name := hex.EncodeToString(sum[:])[:16] + ".gitconfig"
	return filepath.Join(homeDir, ".coi", "git-identity", name)
}

// buildReadonlyGitMount writes the generated read-only gitconfig to the host and
// returns the synthetic MountEntry that binds it read-only at the container's
// ~/.gitconfig. containerHome is the container-side home (e.g. /home/code).
func buildReadonlyGitMount(id session.GitIdentity, containerHome string) (session.MountEntry, error) {
	hostHome, err := os.UserHomeDir()
	if err != nil {
		return session.MountEntry{}, fmt.Errorf("cannot resolve host home for git.readonly: %w", err)
	}
	hostPath := readonlyGitConfigHostPath(hostHome, id)
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return session.MountEntry{}, fmt.Errorf("failed to create git.readonly dir: %w", err)
	}
	// 0644 so the container's code user can read it regardless of uid shift.
	if err := os.WriteFile(hostPath, []byte(renderReadonlyGitConfig(id)), 0o644); err != nil {
		return session.MountEntry{}, fmt.Errorf("failed to write git.readonly config: %w", err)
	}
	return session.MountEntry{
		HostPath:      hostPath,
		ContainerPath: filepath.Clean(containerHome + "/.gitconfig"),
		DeviceName:    "git-identity",
		Readonly:      true,
	}, nil
}
