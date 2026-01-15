package session

import (
	"path/filepath"

	"github.com/mensfeld/claude-on-incus/internal/tool"
)

// GetSessionsDir returns the sessions directory path for a given tool.
// For example: ~/.coi/sessions-claude, ~/.coi/sessions-aider, etc.
func GetSessionsDir(baseDir string, t tool.Tool) string {
	return filepath.Join(baseDir, t.SessionsDirName())
}
