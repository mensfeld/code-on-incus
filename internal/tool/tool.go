package tool

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

//go:embed templates/sandbox_context.md.tmpl
var sandboxContextTemplate string

// Tool represents an AI coding tool that can be run in COI containers
type Tool interface {
	// Name returns the tool name (e.g., "claude", "aider", "cursor")
	Name() string

	// Binary returns the binary name to execute
	Binary() string

	// ConfigDirName returns config directory name (e.g., ".claude", ".config/opencode")
	// Return "" if tool uses ENV API keys instead of config files
	ConfigDirName() string

	// SessionsDirName returns the sessions directory name for this tool
	// (e.g., "sessions-claude", "sessions-aider")
	SessionsDirName() string

	// BuildCommand builds the command line for execution
	// sessionID: COI session ID
	// resume: whether to resume an existing session
	// resumeSessionID: the tool's internal session ID (if resuming)
	BuildCommand(sessionID string, resume bool, resumeSessionID string) []string

	// DiscoverSessionID finds the tool's internal session ID from saved state
	// stateDir: path to the tool's config directory with saved state
	// Return "" if tool doesn't support session resume (will start fresh each time)
	DiscoverSessionID(stateDir string) string

	// GetSandboxSettings returns settings to inject for sandbox/bypass permissions
	// Return empty map if tool doesn't need settings injection
	GetSandboxSettings() map[string]interface{}
}

// ToolWithConfigDirFiles is implemented by every tool that uses a config
// directory (ConfigDirName != ""). It tells setupCLIConfig which files to
// copy, where to inject sandbox settings, and whether a sibling state file
// (e.g. ~/.claude.json) exists.
type ToolWithConfigDirFiles interface {
	Tool

	// EssentialConfigFiles returns filenames inside the config directory
	// that should be copied from the host (e.g. ["settings.json", ".credentials.json"]).
	EssentialConfigFiles() []string

	// SandboxSettingsFileName returns the filename inside the config directory
	// where GetSandboxSettings() should be injected (e.g. "settings.json", "opencode.json").
	SandboxSettingsFileName() string

	// StateConfigFileName returns the name of a JSON state file that lives as
	// a sibling next to the config directory (e.g. ".claude.json").
	// Return "" if the tool has no such file.
	StateConfigFileName() string

	// AlwaysSetupConfig returns true if setupCLIConfig should run even when
	// the host config directory doesn't exist (e.g. opencode needs sandbox
	// injection regardless). Return false to skip setup when there's nothing
	// to copy from the host (e.g. Claude needs credentials from ~/.claude).
	AlwaysSetupConfig() bool
}

// ClaudeTool implements Tool for Claude Code
type ClaudeTool struct {
	effortLevel    string // "low", "medium", "high" - defaults to "medium" if empty
	permissionMode string // "bypass" (default) or "interactive"
}

// NewClaude creates a new Claude tool instance
func NewClaude() Tool {
	return &ClaudeTool{}
}

func (c *ClaudeTool) Name() string {
	return "claude"
}

func (c *ClaudeTool) Binary() string {
	return "claude"
}

func (c *ClaudeTool) ConfigDirName() string {
	return ".claude"
}

func (c *ClaudeTool) SessionsDirName() string {
	return "sessions-claude"
}

func (c *ClaudeTool) BuildCommand(sessionID string, resume bool, resumeSessionID string) []string {
	// Base command with flags
	cmd := []string{"claude", "--verbose"}

	// Only add bypass permissions when not in interactive mode
	if c.permissionMode != "interactive" {
		cmd = append(cmd, "--permission-mode", "bypassPermissions")
	}

	// Add session/resume flag
	if resume {
		if resumeSessionID != "" {
			cmd = append(cmd, "--resume", resumeSessionID)
		} else {
			cmd = append(cmd, "--resume")
		}
	} else {
		cmd = append(cmd, "--session-id", sessionID)
	}

	return cmd
}

func (c *ClaudeTool) DiscoverSessionID(stateDir string) string {
	// Claude stores sessions as .jsonl files in projects/-workspace/
	// This logic is extracted from cleanup.go:387-411
	projectsDir := filepath.Join(stateDir, "projects", "-workspace")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}

	// Find the first .jsonl file (Claude session file)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			return strings.TrimSuffix(entry.Name(), ".jsonl")
		}
	}

	return ""
}

func (c *ClaudeTool) GetSandboxSettings() map[string]interface{} {
	settings := map[string]interface{}{}

	// Only inject bypass permission settings when not in interactive mode
	if c.permissionMode != "interactive" {
		settings["allowDangerouslySkipPermissions"] = true
		settings["bypassPermissionsModeAccepted"] = true
		settings["permissions"] = map[string]string{
			"defaultMode": "bypassPermissions",
		}
	}

	// Set effort level (default to "medium" if not configured)
	// We set both effortLevel directly AND via env var for maximum compatibility
	effortLevel := c.effortLevel
	if effortLevel == "" {
		effortLevel = "medium"
	}
	settings["effortLevel"] = effortLevel

	// Effort prompt suppression flags (discovered by examining .claude.json after manual selection)
	settings["effortLevelAccepted"] = true
	settings["hasSeenEffortPrompt"] = true
	settings["effortCalloutDismissed"] = true

	// Also set via env section (CLAUDE_CODE_EFFORT_LEVEL) as documented
	settings["env"] = map[string]string{
		"CLAUDE_CODE_EFFORT_LEVEL": effortLevel,
	}

	return settings
}

// EssentialConfigFiles implements ToolWithConfigDirFiles.
func (c *ClaudeTool) EssentialConfigFiles() []string {
	return []string{".credentials.json", "config.yml", "settings.json", "CLAUDE.md"}
}

// SandboxSettingsFileName implements ToolWithConfigDirFiles.
func (c *ClaudeTool) SandboxSettingsFileName() string { return "settings.json" }

// StateConfigFileName implements ToolWithConfigDirFiles.
// Claude uses ~/.claude.json as a sibling state file next to ~/.claude/.
func (c *ClaudeTool) StateConfigFileName() string { return ".claude.json" }

// AlwaysSetupConfig implements ToolWithConfigDirFiles.
// Claude needs credentials from ~/.claude, so skip setup when host dir is missing.
func (c *ClaudeTool) AlwaysSetupConfig() bool { return false }

// AutoContextFile implements ToolWithAutoContextFile.
// Claude Code automatically reads ~/.claude/CLAUDE.md as user-level instructions.
func (c *ClaudeTool) AutoContextFile() string { return ".claude/CLAUDE.md" }

// SetEffortLevel sets the effort level for Claude Code.
// Valid values: "low", "medium", "high".
// This prevents the interactive effort prompt during autonomous sessions.
func (c *ClaudeTool) SetEffortLevel(level string) {
	c.effortLevel = level
}

// ToolWithEffortLevel is an optional interface for tools that support
// configurable effort levels (e.g., Claude's low/medium/high effort).
type ToolWithEffortLevel interface {
	Tool
	// SetEffortLevel sets the effort level for the tool.
	// Valid values depend on the tool (e.g., "low", "medium", "high" for Claude).
	SetEffortLevel(level string)
}

// ToolWithContainerEnv is an optional interface for tools that need extra
// environment variables set inside the container (e.g., to redirect data
// storage to the workspace mount so it persists across ephemeral sessions).
type ToolWithContainerEnv interface {
	Tool
	// GetContainerEnv returns environment variables to set when executing
	// the tool inside the container. workspacePath is the mount point
	// inside the container (e.g. "/workspace").
	GetContainerEnv(workspacePath string) map[string]string
}

// SetPermissionMode sets the permission mode for Claude Code.
// Valid values: "bypass" (default, all permissions auto-granted) or "interactive" (human-in-the-loop).
func (c *ClaudeTool) SetPermissionMode(mode string) {
	c.permissionMode = mode
}

// ToolWithPermissionMode is an optional interface for tools that support
// configurable permission modes (e.g., bypass vs interactive).
type ToolWithPermissionMode interface {
	Tool
	// SetPermissionMode sets the permission mode for the tool.
	// Valid values: "bypass" (default) or "interactive" (human-in-the-loop).
	SetPermissionMode(mode string)
}

// ToolWithAutoContextFile is implemented by tools that auto-load context
// from a file (e.g., Claude's ~/.claude/CLAUDE.md). The setup flow writes
// sandbox context content to this file so the tool loads it at session start.
type ToolWithAutoContextFile interface {
	Tool
	// AutoContextFile returns the path relative to home dir where sandbox
	// context should be written for the tool to auto-load at session start.
	AutoContextFile() string
}

// ToolWithAutoContextPath is implemented by tools that reference the sandbox
// context file path in their config (e.g., OpenCode's instructions field).
// The setup flow calls SetAutoContextPath before config injection so the
// path appears in the tool's sandbox settings.
type ToolWithAutoContextPath interface {
	Tool
	// SetAutoContextPath sets the absolute path to the sandbox context file
	// so the tool can reference it in its configuration.
	SetAutoContextPath(path string)
}

// ContextInfo provides dynamic information about the container environment
// for generating the sandbox context file (~/SANDBOX_CONTEXT.md).
type ContextInfo struct {
	WorkspacePath      string   // Mount point inside container (e.g., "/workspace")
	HomeDir            string   // Home directory inside container (e.g., "/home/code")
	Persistent         bool     // Whether the container persists between sessions
	NetworkMode        string   // "restricted", "open", "allowlist", or ""
	SSHAgentForwarded  bool     // Whether host SSH agent is forwarded
	RunAsRoot          bool     // Whether the tool runs as root
	OSName             string   // OS name (e.g., "Ubuntu 22.04")
	Architecture       string   // CPU architecture (e.g., "amd64", "arm64")
	ProtectedPaths     []string // Paths mounted read-only for security
	GHCLIAuthenticated bool     // Whether GitHub CLI auth is available (GH_TOKEN or GITHUB_TOKEN forwarded)
	ForwardedEnvVars   []string // Names of host environment variables forwarded into the container
}

// contextTemplateData holds the resolved values passed to the context file template.
type contextTemplateData struct {
	WorkspacePath       string
	HomeDir             string
	OSDesc              string
	ArchDesc            string
	PersistenceDesc     string
	NetworkDesc         string
	NetworkLimitation   string
	SSHDesc             string
	GitHubCLIDesc       string
	DockerDesc          string
	UserDesc            string
	SudoDesc            string
	ProtectedPaths      string
	Persistent          bool
	ForwardedEnvVars    string
	HasForwardedEnvVars bool
}

// RenderContextFileContent renders the embedded sandbox context template with
// dynamic environment info. This is tool-agnostic — the resulting file is
// placed at ~/SANDBOX_CONTEXT.md by setup and can be consumed by any AI tool.
func RenderContextFileContent(info ContextInfo) string {
	data := contextTemplateData{
		WorkspacePath:   info.WorkspacePath,
		HomeDir:         info.HomeDir,
		OSDesc:          info.OSName,
		ArchDesc:        info.Architecture,
		PersistenceDesc: "Ephemeral (destroyed after session ends)",
		Persistent:      info.Persistent,
		NetworkDesc:     "Unknown",
		SSHDesc:         "Not available",
		GitHubCLIDesc:   "Not authenticated",
		DockerDesc:      "Available (Docker-in-Docker)",
		UserDesc:        "Non-root user (code)",
		SudoDesc:        "Available via passwordless sudo",
	}

	if data.OSDesc == "" {
		data.OSDesc = "Ubuntu (container)"
	}
	if data.ArchDesc == "" {
		data.ArchDesc = runtime.GOARCH
	}

	if info.Persistent {
		data.PersistenceDesc = "Persistent (survives between sessions)"
	}

	switch info.NetworkMode {
	case "restricted":
		data.NetworkDesc = "Restricted (internet allowed, local/private networks blocked)"
		data.NetworkLimitation = "Internet access is allowed, but connections to local/private networks are blocked by firewall rules"
	case "open":
		data.NetworkDesc = "Open (all network access allowed)"
	case "allowlist":
		data.NetworkDesc = "Allowlist (only pre-approved domains allowed)"
		data.NetworkLimitation = "Only pre-approved domains are reachable; all other outbound connections and private networks are blocked"
	case "":
		data.NetworkDesc = "Default (no explicit network policy)"
	}

	if info.SSHAgentForwarded {
		data.SSHDesc = "Forwarded from host (available via SSH_AUTH_SOCK)"
	}

	if info.GHCLIAuthenticated {
		data.GitHubCLIDesc = "Authenticated via forwarded token (gh CLI ready to use)"
	}

	if len(info.ForwardedEnvVars) > 0 {
		data.ForwardedEnvVars = strings.Join(info.ForwardedEnvVars, ", ")
		data.HasForwardedEnvVars = true
	}

	if info.RunAsRoot {
		data.UserDesc = "Root user"
		data.SudoDesc = "Already running as root"
	}

	if len(info.ProtectedPaths) > 0 {
		data.ProtectedPaths = strings.Join(info.ProtectedPaths, ", ")
	}

	tmpl, err := template.New("context").Parse(sandboxContextTemplate)
	if err != nil {
		// Should never happen with an embedded template; return raw template as fallback.
		return sandboxContextTemplate
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return sandboxContextTemplate
	}

	return buf.String()
}
