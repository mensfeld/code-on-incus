package tool

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CodexTool implements Tool for OpenAI Codex CLI (https://developers.openai.com/codex/cli)
type CodexTool struct {
	permissionMode  string // "bypass" (default) or "interactive"
	model           string // codex model, delivered as -m (e.g. "gpt-5-codex") — empty means unset (codex's own default)
	reasoningEffort string // "minimal", "low", "medium", "high" — delivered as -c model_reasoning_effort=<v>; empty means unset
}

// NewCodex creates a new codex tool instance
func NewCodex() Tool { return &CodexTool{} }

func (c *CodexTool) Name() string { return "codex" }

func (c *CodexTool) Binary() string { return "codex" }

// ConfigDirName returns the config directory for codex (~/.codex, aka CODEX_HOME).
func (c *CodexTool) ConfigDirName() string { return mustBundle("codex").ConfigDir }

func (c *CodexTool) SessionsDirName() string { return "sessions-codex" }

// rolloutSessionFile matches codex session files
// (sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl) and captures the UUID.
var rolloutSessionFile = regexp.MustCompile(`^rollout-.*-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`)

// BuildCommand builds the codex launch command. Every codex flag coi relies on
// lives in this one function so upstream flag drift has a single fix site.
//
// In bypass mode (default) coi passes --dangerously-bypass-approvals-and-sandbox:
// the Incus container is the sandbox, codex's own Landlock sandbox may not work
// nested inside it, and the flag also skips the first-run folder-trust prompt.
// In interactive mode codex keeps its approval prompts with a writable workspace.
//
// The COI sessionID is unused — codex has no flag to set a session ID on a fresh
// launch (sessions get a rollout UUID internally); COI UUIDs are metadata-only,
// as with pi. On resume, `codex resume <uuid>` pins the session discovered by
// DiscoverSessionID, falling back to `codex resume --last` when discovery fails.
// The resume subcommand is assumed to accept the same mode/model flags as the
// root command; if upstream ever drops that, remove the flags from the resume
// path — the host-seeded ~/.codex/config.toml still governs defaults.
func (c *CodexTool) BuildCommand(sessionID string, resume bool, resumeSessionID string) []string {
	cmd := []string{"codex"}

	if resume {
		cmd = append(cmd, "resume")
		if resumeSessionID != "" {
			cmd = append(cmd, resumeSessionID)
		} else {
			cmd = append(cmd, "--last")
		}
	}

	if c.permissionMode == "interactive" {
		cmd = append(cmd, "-s", "workspace-write", "-a", "on-request")
	} else {
		cmd = append(cmd, "--dangerously-bypass-approvals-and-sandbox")
	}

	// Flag values must stay single shell-safe tokens: buildCLICommand joins
	// argv with spaces into a shell command string.
	if c.model != "" {
		cmd = append(cmd, "-m", c.model)
	}
	if c.reasoningEffort != "" {
		cmd = append(cmd, "-c", "model_reasoning_effort="+c.reasoningEffort)
	}

	return cmd
}

// DiscoverSessionID finds the newest codex session UUID in the saved state dir.
// Codex stores sessions as sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl.
// Returns "" when nothing is found — BuildCommand then falls back to
// `codex resume --last`, which is independent of the sessions layout.
func (c *CodexTool) DiscoverSessionID(stateDir string) string {
	sessionsDir := filepath.Join(stateDir, "sessions")

	var newestID string
	var newestTime time.Time
	var newestPath string

	_ = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries; discovery is best-effort
		}
		m := rolloutSessionFile.FindStringSubmatch(d.Name())
		if m == nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries; discovery is best-effort
		}
		// Newest by mtime; tie-break on the lexicographically greatest path
		// (the layout is date-encoded, so greater path = later session).
		if info.ModTime().After(newestTime) ||
			(info.ModTime().Equal(newestTime) && strings.Compare(path, newestPath) > 0) {
			newestTime = info.ModTime()
			newestPath = path
			newestID = m[1]
		}
		return nil
	})

	return newestID
}

// GetSandboxSettings returns no settings for codex. Unlike claude/opencode,
// codex's config file (~/.codex/config.toml) is TOML while coi's settings
// injection (mergeJSONSettings) is JSON-only, so coi never rewrites it — the
// host file is seeded verbatim and everything coi controls (permission mode,
// model, reasoning effort) is delivered as launch flags in BuildCommand.
func (c *CodexTool) GetSandboxSettings() map[string]interface{} {
	return map[string]interface{}{}
}

// SetPermissionMode sets the permission mode for codex.
// Valid values: "bypass" (default) or "interactive" (human-in-the-loop).
// Consumed by BuildCommand (not GetSandboxSettings — see there for why).
func (c *CodexTool) SetPermissionMode(mode string) {
	c.permissionMode = mode
}

// SetModel implements ToolWithModel. Delivered as `-m <model>`;
// when unset codex uses its own default. Configured via [tool.codex] model.
func (c *CodexTool) SetModel(model string) {
	c.model = model
}

// SetEffortLevel implements ToolWithEffortLevel. For codex this maps to
// `model_reasoning_effort` ("minimal", "low", "medium", "high") — codex's
// scale, not Claude's effort levels. Configured via [tool.codex] reasoning_effort.
func (c *CodexTool) SetEffortLevel(level string) {
	c.reasoningEffort = level
}

// EssentialConfigFiles implements ToolWithConfigDirFiles.
func (c *CodexTool) EssentialConfigFiles() []string {
	return mustBundle("codex").Files
}

// SandboxSettingsFileName implements ToolWithConfigDirFiles.
// Empty: codex has no JSON settings file to inject into (see GetSandboxSettings).
func (c *CodexTool) SandboxSettingsFileName() string { return mustBundle("codex").SandboxSettingsFile }

// StateConfigFileName implements ToolWithConfigDirFiles.
// Codex has no sibling state file (everything lives inside ~/.codex).
func (c *CodexTool) StateConfigFileName() string { return mustBundle("codex").StateFile }

// AlwaysSetupConfig implements ToolWithConfigDirFiles.
// Codex needs credentials from ~/.codex (auth.json), so skip setup when the
// host dir is missing — auto-context injection runs independently either way.
func (c *CodexTool) AlwaysSetupConfig() bool { return mustBundle("codex").AlwaysSetup }

// AutoContextFile implements ToolWithAutoContextFile.
// Codex reads ~/.codex/AGENTS.md as global instructions; coi injects its
// managed sandbox-context block there (container-side), never into the
// workspace AGENTS.md, which lives on the host bind-mount.
func (c *CodexTool) AutoContextFile() string { return mustBundle("codex").AutoContextFile }
