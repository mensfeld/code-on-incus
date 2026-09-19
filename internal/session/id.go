package session

import (
	"crypto/rand"
	"fmt"
	"regexp"
)

// sessionIDRe mirrors the config session_name pattern (config.sessionNameRe):
// letters, digits, '.', '_', '-'; must start alphanumeric; max 64 chars.
// Generated ids (UUIDs) and saved-session directory names already satisfy it.
var sessionIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// ValidateSessionID rejects a session id that isn't a safe single token. The id
// is used as a filesystem path component and, for `coi tool spec`, is joined
// into a shell-run launch command (and referenced via `"$(cat …/<id>.prompt)"`),
// so a value with spaces, shell metacharacters, or path separators would break
// or inject the command. Callers that accept an id from outside coi (e.g.
// `--session-id`) must validate it; ids coi generates/discovers itself already
// conform.
func ValidateSessionID(id string) error {
	if sessionIDRe.MatchString(id) {
		return nil
	}
	return fmt.Errorf("invalid session id %q: must match %s (letters, digits, '.', '_', '-'; start alphanumeric; max 64 chars)", id, sessionIDRe.String())
}

// GenerateSessionID creates a new session ID in UUID format
// Returns a UUID v4 format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
func GenerateSessionID() (string, error) {
	bytes := make([]byte, 16) // 16 bytes for UUID
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}

	// Set version (4) and variant bits for UUID v4
	bytes[6] = (bytes[6] & 0x0f) | 0x40 // Version 4
	bytes[8] = (bytes[8] & 0x3f) | 0x80 // Variant 10

	// Format as UUID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	), nil
}
