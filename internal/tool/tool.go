package tool

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/mensfeld/code-on-incus/internal/tool/credentials"
)

//go:embed templates/sandbox_context.md.tmpl
var sandboxContextTemplate string

// mustBundle looks up a named credential bundle from the embedded catalog
// (internal/tool/credentials). Panics if missing — a builtin tool
// referencing an unknown bundle name is a programming error (a typo in this
// package or a catalog entry that was renamed/removed), not a runtime
// condition to recover from.
func mustBundle(name string) credentials.Bundle {
	b, ok := credentials.Lookup(name)
	if !ok {
		panic(fmt.Sprintf("tool: unknown credential bundle %q", name))
	}
	return b
}

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
	effortLevel    string // "low", "medium", "high", "xhigh", "max", "auto" — empty means unset (user controls interactively)
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
	return mustBundle("claude").ConfigDir
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
		settings["permissions"] = map[string]interface{}{
			"defaultMode":                       "bypassPermissions",
			"skipDangerousModePermissionPrompt": true,
		}
	}

	// Suppress the effort-level startup prompt without locking the level itself.
	settings["effortLevelAccepted"] = true
	settings["hasSeenEffortPrompt"] = true
	settings["effortCalloutDismissed"] = true

	// Only inject the effort level when explicitly configured. Without this,
	// Claude uses its own default and the user can change it interactively.
	// Setting CLAUDE_CODE_EFFORT_LEVEL locks the level and prevents changes.
	if c.effortLevel != "" {
		settings["effortLevel"] = c.effortLevel
		settings["env"] = map[string]string{
			"CLAUDE_CODE_EFFORT_LEVEL": c.effortLevel,
		}
	}

	return settings
}

// EssentialConfigFiles implements ToolWithConfigDirFiles.
func (c *ClaudeTool) EssentialConfigFiles() []string {
	return mustBundle("claude").Files
}

// SandboxSettingsFileName implements ToolWithConfigDirFiles.
func (c *ClaudeTool) SandboxSettingsFileName() string {
	return mustBundle("claude").SandboxSettingsFile
}

// StateConfigFileName implements ToolWithConfigDirFiles.
// Claude uses ~/.claude.json as a sibling state file next to ~/.claude/.
func (c *ClaudeTool) StateConfigFileName() string { return mustBundle("claude").StateFile }

// AlwaysSetupConfig implements ToolWithConfigDirFiles.
// Claude needs credentials from ~/.claude, so skip setup when host dir is missing.
func (c *ClaudeTool) AlwaysSetupConfig() bool { return mustBundle("claude").AlwaysSetup }

// AutoContextFile implements ToolWithAutoContextFile.
// Claude Code automatically reads ~/.claude/CLAUDE.md as user-level instructions.
func (c *ClaudeTool) AutoContextFile() string { return mustBundle("claude").AutoContextFile }

// SetEffortLevel sets the effort level for Claude Code.
// Valid values: "low", "medium", "high", "xhigh", "max", "auto".
// When set, CLAUDE_CODE_EFFORT_LEVEL is injected and locks the level for the session.
// When empty (default), the user can change the effort level interactively inside Claude.
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

// ToolWithPreLaunch is implemented by tools that need filesystem setup
// inside the container before the main command runs. Each returned entry
// is a separate argv slice executed via ExecArgs (no shell interpretation),
// eliminating shell injection risks.
type ToolWithPreLaunch interface {
	Tool
	// PreLaunch returns commands to execute inside the container before
	// the tool is launched. Each entry is a separate exec call.
	// Return nil if no pre-launch steps are needed.
	PreLaunch() [][]string
}

// PortInfo describes one container port published on the host.
type PortInfo struct {
	Name          string // "" for pool ports
	HostPort      int
	ContainerPort int
	Listen        string // host listen address ("", "127.0.0.1" and "0.0.0.0" mean localhost works)
	Pool          bool   // identity-mapped pool port (host == container number)
	EnvVar        string // COI_PORT_<NAME> for named entries, "" for pool
}

// MountInfo describes an extra directory mounted into the container.
type MountInfo struct {
	ContainerPath string
}

// ContextInfo provides dynamic information about the container environment
// for generating the sandbox context file (~/SANDBOX_CONTEXT.md).
type ContextInfo struct {
	WorkspacePath      string      // Mount point inside container (e.g., "/workspace")
	HomeDir            string      // Home directory inside container (e.g., "/home/code")
	Persistent         bool        // Whether the container persists between sessions
	NetworkMode        string      // "restricted", "open", "allowlist", or ""
	SSHAgentForwarded  bool        // Whether host SSH agent is forwarded
	RunAsRoot          bool        // Whether the tool runs as root
	OSName             string      // OS name (e.g., "Ubuntu 22.04")
	Architecture       string      // CPU architecture (e.g., "amd64", "arm64")
	ProtectedPaths     []string    // Paths mounted read-only for security
	GHCLIAuthenticated bool        // Whether GitHub CLI auth is available (GH_TOKEN or GITHUB_TOKEN forwarded)
	ForwardedEnvVars   []string    // Names of host environment variables forwarded into the container
	Timezone           string      // IANA timezone (e.g., "America/New_York"), empty = UTC
	ExtraMounts        []MountInfo // Additional mounted paths beyond workspace
	PublishedPorts     []PortInfo  // Container ports published on the host (#558)
	CPULimit           string      // e.g., "2" or "0-3", empty = unlimited
	MemoryLimit        string      // e.g., "2GiB", empty = unlimited
	MaxDuration        string      // e.g., "2h", empty = unlimited
	ToolName           string      // e.g., "claude", "aider"
	ContainerName      string      // Incus container name
	ProfileContext     string      // User-provided profile context content (from profile CONTEXT.md)
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
	TimezoneDesc        string
	ExtraMounts         string // Comma-joined container paths
	HasExtraMounts      bool
	HasPorts            bool
	PoolPortsDesc       string // e.g. "23410, 23411, 23412" (identity-mapped)
	FirstPoolPort       string // first pool port, for the concrete usage example
	NamedPortsDesc      string // one line per named mapping
	ResourceLimits      string // e.g., "2 CPUs, 2GiB memory"
	HasResourceLimits   bool
	MaxDuration         string
	HasMaxDuration      bool
	ToolName            string
	ContainerName       string
	ProfileContext      string
	HasProfileContext   bool
	HasGitAuth          bool
	SSHAgentForwarded   bool
	GHCLIAuthenticated  bool
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
		data.GitHubCLIDesc = "Authenticated via forwarded token (gh CLI ready to use; note: token may have limited scope/permissions)"
	}

	// Git auth flags — the template renders conditional instructions based on these.
	if info.SSHAgentForwarded || info.GHCLIAuthenticated {
		data.HasGitAuth = true
		data.SSHAgentForwarded = info.SSHAgentForwarded
		data.GHCLIAuthenticated = info.GHCLIAuthenticated
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

	// Timezone
	data.TimezoneDesc = "UTC"
	if info.Timezone != "" {
		data.TimezoneDesc = info.Timezone
	}

	// Tool name
	data.ToolName = "AI coding tool"
	if info.ToolName != "" {
		data.ToolName = info.ToolName
	}

	// Container name
	data.ContainerName = info.ContainerName

	// Extra mounts
	if len(info.ExtraMounts) > 0 {
		paths := make([]string, len(info.ExtraMounts))
		for i, m := range info.ExtraMounts {
			paths[i] = m.ContainerPath
		}
		data.ExtraMounts = strings.Join(paths, ", ")
		data.HasExtraMounts = true
	}

	// Published ports (#558)
	if len(info.PublishedPorts) > 0 {
		data.HasPorts = true
		var pool []string
		var named []string
		for _, p := range info.PublishedPorts {
			if p.Pool {
				pool = append(pool, fmt.Sprintf("%d", p.HostPort))
			} else {
				// A pin to a specific non-loopback listen address is NOT
				// reachable via the host's localhost — name the real address.
				host := "localhost"
				if p.Listen != "" && p.Listen != "127.0.0.1" && p.Listen != "0.0.0.0" {
					host = p.Listen
				}
				named = append(named, fmt.Sprintf("- %s: bind container port %d — the user reaches it at http://%s:%d (%s=%d)",
					p.Name, p.ContainerPort, host, p.HostPort, p.EnvVar, p.HostPort))
			}
		}
		data.PoolPortsDesc = strings.Join(pool, ", ")
		data.NamedPortsDesc = strings.Join(named, "\n")
		if len(pool) > 0 {
			data.FirstPoolPort = pool[0]
		}
	}

	// Resource limits
	var limitParts []string
	if info.CPULimit != "" {
		limitParts = append(limitParts, info.CPULimit+" CPUs")
	}
	if info.MemoryLimit != "" {
		limitParts = append(limitParts, info.MemoryLimit+" memory")
	}
	if len(limitParts) > 0 {
		data.ResourceLimits = strings.Join(limitParts, ", ")
		data.HasResourceLimits = true
	}

	// Max duration
	if info.MaxDuration != "" {
		data.MaxDuration = info.MaxDuration
		data.HasMaxDuration = true
	}

	// Profile context
	if info.ProfileContext != "" {
		data.ProfileContext = info.ProfileContext
		data.HasProfileContext = true
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
