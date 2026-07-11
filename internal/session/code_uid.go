package session

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// RecordCodeUID stores the session exec UID in the container's
// container.CodeUIDMetadataKey metadata (#588 follow-up). Callers treat
// failure as best-effort: an unrecorded UID only costs consumers a probe
// fallback.
func RecordCodeUID(containerName string, uid int) error {
	return container.IncusExec("config", "set", containerName,
		fmt.Sprintf("%s=%d", container.CodeUIDMetadataKey, uid))
}

// RecordedCodeUID reads the container's recorded session UID; ok is false
// when the key is absent (pre-metadata container), unreadable, or not an
// integer — callers then fall back to probing.
func RecordedCodeUID(containerName string) (int, bool) {
	out, err := container.IncusOutput("config", "get", containerName, container.CodeUIDMetadataKey)
	return parseRecordedUID(out, err)
}

func parseRecordedUID(out string, err error) (int, bool) {
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, false
	}
	return uid, true
}

// EffectiveCodeUID resolves the UID sessions in this container exec as:
// the recorded user.coi.uid metadata when present, else a live probe of the
// code user (pre-metadata containers), lazily recording the probe result so
// the next call is a plain config read.
func EffectiveCodeUID(mgr container.ContainerExecution, containerName, codeUser string) (int, error) {
	return effectiveCodeUID(RecordedCodeUID, mgr, containerName, codeUser)
}

// effectiveCodeUID is EffectiveCodeUID with the metadata reader injectable
// for unit tests (the real reader shells out to incus).
func effectiveCodeUID(recorded func(string) (int, bool), mgr container.ContainerExecution, containerName, codeUser string) (int, error) {
	if uid, ok := recorded(containerName); ok {
		return uid, nil
	}
	uid, err := ResolveCodeUID(mgr, codeUser)
	if err != nil {
		return 0, err
	}
	// Best-effort upgrade: failure only means the next call probes again.
	_ = RecordCodeUID(containerName, uid)
	return uid, nil
}

// DetectCodeUser returns true if the named user account exists inside
// the running container. It is used to decide whether to run sessions
// as `code` or fall back to root — replacing the old broken heuristic
// that matched the image alias literally against "coi-default" and
// misclassified every custom image built from it as a root image.
//
// Implemented on probeCodeUser (shared with ResolveCodeUID): "user not
// present" is recognized by `id`'s own stderr, while incus-level exec
// failures return an error so the caller can decide whether to warn or
// fall back. See probeCodeUser for the argv-injection defence notes.
func DetectCodeUser(mgr container.ContainerExecution, codeUser string) (bool, error) {
	_, exists, err := probeCodeUser(mgr, codeUser)
	return exists, err
}

// codeUserMissing reports whether a probe error means `id` itself ran and
// said the account doesn't exist — as opposed to an incus-level failure
// (daemon unreachable, container stopped mid-race, permission denied,
// missing binary) that ALSO surfaces as *container.ExitError from the CLI
// exec path, with the same non-zero exit code. The stderr text is the only
// reliable discriminator: `id` (GNU coreutils and busybox alike) says
// "no such user"/"unknown user", incus's own failures say "Error: ...".
// Only a genuine no-such-user may fall back to root — misclassifying an
// infra failure would silently misdirect callers to root's tmux socket,
// the exact #588 failure mode.
func codeUserMissing(err error) bool {
	var exitErr *container.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	stderr := strings.ToLower(exitErr.Stderr)
	return strings.Contains(stderr, "no such user") || strings.Contains(stderr, "unknown user")
}

// probeCodeUser is the ONE code-user probe shared by DetectCodeUser and
// ResolveCodeUID (so their error taxonomies cannot drift): it runs
// `id -u <codeUser>` in the container and returns (uid, true, nil) when the
// account exists, (0, false, nil) when `id` reports it missing, and an error
// for anything else — including incus-level exec failures, which are
// distinguished from "no such user" by stderr (see codeUserMissing).
//
// codeUser is passed as a raw argv entry to `id` rather than interpolated
// into a shell string — defence-in-depth against a maliciously crafted
// [incus] code_user config value: `id` receives it as a single argument and
// reports "no such user"; the shell never sees it.
func probeCodeUser(mgr container.ContainerExecution, codeUser string) (int, bool, error) {
	out, err := mgr.ExecArgsCapture(
		[]string{"id", "-u", codeUser},
		container.ExecCommandOptions{Capture: true},
	)
	if err != nil {
		if codeUserMissing(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	uid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, false, fmt.Errorf("unexpected `id -u %s` output %q: %w", codeUser, out, err)
	}
	return uid, true, nil
}

// ResolveCodeUID returns the UID the container's code user ACTUALLY has,
// probed from the container itself, or root (0) when the account doesn't
// exist — images without a code user run their sessions, and therefore
// their tmux server, as root. Prefer EffectiveCodeUID, which consults the
// recorded user.coi.uid metadata first and only probes as a fallback: the
// probe uses the CURRENT config's code_user NAME, so it resolves
// cross-config UID variance but not a container created under a different
// [incus] code_user name.
func ResolveCodeUID(mgr container.ContainerExecution, codeUser string) (int, error) {
	uid, exists, err := probeCodeUser(mgr, codeUser)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil // no code user: sessions run as root
	}
	return uid, nil
}
