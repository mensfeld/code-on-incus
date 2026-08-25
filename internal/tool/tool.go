package tool

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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
	model          string // Claude model, delivered as ANTHROPIC_MODEL — empty means unset (Claude Code's own default)
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
		settings["skipDangerousModePermissionPrompt"] = true
		settings["permissions"] = map[string]interface{}{
			"defaultMode": "bypassPermissions",
		}
	}

	// Suppress the effort-level startup prompt without locking the level itself.
	settings["effortLevelAccepted"] = true
	settings["hasSeenEffortPrompt"] = true
	settings["effortCalloutDismissed"] = true

	// Build the Claude Code env block from the explicitly-configured knobs. Each
	// is injected only when set; unset means Claude uses its own default (and, for
	// effort, the user can still change it interactively).
	env := map[string]string{}

	// Setting CLAUDE_CODE_EFFORT_LEVEL locks the level and prevents changes.
	if c.effortLevel != "" {
		settings["effortLevel"] = c.effortLevel
		env["CLAUDE_CODE_EFFORT_LEVEL"] = c.effortLevel
	}

	// ANTHROPIC_MODEL selects the model Claude Code runs (e.g. "opus",
	// "claude-opus-4-8"). Configured via [tool.claude] model.
	if c.model != "" {
		env["ANTHROPIC_MODEL"] = c.model
	}

	if len(env) > 0 {
		settings["env"] = env
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

// SetModel sets the model for Claude Code.
// The value is passed through verbatim as ANTHROPIC_MODEL (e.g. "opus",
// "claude-opus-4-8"). When empty (default), no model is injected and Claude
// Code uses its own default.
func (c *ClaudeTool) SetModel(model string) {
	c.model = model
}

// ToolWithModel is an optional interface for tools that support selecting a
// model (e.g., Claude via ANTHROPIC_MODEL).
type ToolWithModel interface {
	Tool
	// SetModel sets the model for the tool. The accepted values depend on the
	// tool (Claude accepts aliases like "opus" and full model IDs).
	SetModel(model string)
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
	AllowedPorts       []int       // Egress destination-port allowlist (empty = all ports)
	DNSServers         []string    // Pinned DNS resolvers, :53 restricted to these (empty = unrestricted)
	AllowedDomains     []string    // Allowlist-mode reachable destinations (hostnames/IPs/CIDRs)
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

// withDefaults fills the fields setup leaves zero-valued (OS name, architecture)
// with the same fallbacks the human-readable renderer uses, so the .md and .json
// context files can never disagree on them.
func (info ContextInfo) withDefaults() ContextInfo {
	if info.OSName == "" {
		info.OSName = "Ubuntu (container)"
	}
	if info.Architecture == "" {
		info.Architecture = runtime.GOARCH
	}
	return info
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
	info = info.withDefaults()
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

	// Surface the fine-grained egress controls so the agent knows exactly what it
	// can and cannot reach and does not waste turns dialing blocked ports/resolvers.
	// Appended to NetworkLimitation, which the template renders only when non-empty.
	//
	// These are gated on the mode that actually ENFORCES them, not merely on the
	// config being present: allowed_ports/dns_servers are inert in open mode (which
	// installs a blanket accept), and dns_servers is inert in allowlist mode (which
	// blocks all DNS and is rejected at setup). Announcing a cap the firewall never
	// installed would tell the agent egress is filtered when it is wide open.
	portsEnforced := info.NetworkMode == "restricted" || info.NetworkMode == "allowlist"
	dnsEnforced := info.NetworkMode == "restricted"
	var egress []string
	if portsEnforced && len(info.AllowedPorts) > 0 {
		ports := make([]string, len(info.AllowedPorts))
		for i, p := range info.AllowedPorts {
			ports[i] = strconv.Itoa(p)
		}
		egress = append(egress, "outbound is restricted to destination port(s) "+strings.Join(ports, ", ")+
			" — all other ports are blocked (including on the local network), so services on non-listed ports are unreachable")
	}
	if dnsEnforced && len(info.DNSServers) > 0 {
		egress = append(egress, "DNS is pinned to "+strings.Join(info.DNSServers, ", ")+
			" on port 53 — queries to any other resolver are blocked")
	}
	if info.NetworkMode == "allowlist" && len(info.AllowedDomains) > 0 {
		egress = append(egress, "the only reachable outbound destinations are: "+strings.Join(info.AllowedDomains, ", "))
	}
	if len(egress) > 0 {
		joined := strings.Join(egress, "; ")
		if data.NetworkLimitation != "" {
			data.NetworkLimitation += ". Additionally, " + joined
		} else {
			data.NetworkLimitation = "Egress is filtered: " + joined
		}
		data.NetworkDesc += " — egress-filtered (see Limitations below)"
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

// sandboxContextSchemaVersion is the version of the SANDBOX_CONTEXT.json schema.
// Bump it on any backwards-incompatible change so programmatic consumers can
// gate on it.
const sandboxContextSchemaVersion = 1

// SandboxContextJSON is the machine-readable form of the sandbox context,
// written to ~/SANDBOX_CONTEXT.json next to the human-readable .md (#705). It is
// a STABLE PUBLIC CONTRACT — deliberately decoupled from the internal
// ContextInfo struct so that renaming/reshaping internals never silently
// changes what external nodes parse. Change it only with a SchemaVersion bump.
type SandboxContextJSON struct {
	SchemaVersion int `json:"schema_version"`

	ContainerName string `json:"container_name"`
	ToolName      string `json:"tool_name"`
	WorkspacePath string `json:"workspace_path"`
	HomeDir       string `json:"home_dir"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	Timezone      string `json:"timezone,omitempty"`
	Persistent    bool   `json:"persistent"`
	RunAsRoot     bool   `json:"run_as_root"`

	Network SandboxNetworkJSON `json:"network"`

	SSHAgentForwarded  bool              `json:"ssh_agent_forwarded"`
	GHCLIAuthenticated bool              `json:"gh_cli_authenticated"`
	ForwardedEnvVars   []string          `json:"forwarded_env_vars"`
	ProtectedPaths     []string          `json:"protected_paths"`
	ExtraMounts        []string          `json:"extra_mounts"` // container paths
	PublishedPorts     []SandboxPortJSON `json:"published_ports"`

	Limits SandboxLimitsJSON `json:"limits"`

	ProfileContext string `json:"profile_context,omitempty"`
}

// SandboxNetworkJSON carries the effective egress posture. AllowedPorts/
// DNSServers/AllowedDomains are the configured values; combine with Mode to know
// which are actually enforced (ports bite in restricted/allowlist, DNS pinning
// in restricted, domains in allowlist).
type SandboxNetworkJSON struct {
	Mode           string   `json:"mode"` // restricted | open | allowlist | ""
	AllowedPorts   []int    `json:"allowed_ports"`
	DNSServers     []string `json:"dns_servers"`
	AllowedDomains []string `json:"allowed_domains"`
}

// SandboxPortJSON is one container port published on the host.
type SandboxPortJSON struct {
	Name          string `json:"name,omitempty"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Listen        string `json:"listen,omitempty"`
	Pool          bool   `json:"pool"`
	EnvVar        string `json:"env_var,omitempty"`
}

// SandboxLimitsJSON carries the resource limits ("" = unlimited).
type SandboxLimitsJSON struct {
	CPU         string `json:"cpu,omitempty"`
	Memory      string `json:"memory,omitempty"`
	MaxDuration string `json:"max_duration,omitempty"`
}

// RenderContextFileJSON serializes a ContextInfo to the SANDBOX_CONTEXT.json
// contract. Tool-agnostic, like RenderContextFileContent; the result is written
// to ~/SANDBOX_CONTEXT.json by setup. List fields are emitted as [] (never null)
// so consumers never special-case a missing array. No timestamp is included, so
// the output is deterministic and does not churn on persistent-container reuse.
func RenderContextFileJSON(info ContextInfo) (string, error) {
	info = info.withDefaults()

	mounts := make([]string, 0, len(info.ExtraMounts))
	for _, m := range info.ExtraMounts {
		mounts = append(mounts, m.ContainerPath)
	}

	ports := make([]SandboxPortJSON, 0, len(info.PublishedPorts))
	for _, p := range info.PublishedPorts {
		// PortInfo and SandboxPortJSON share identical fields (tags aside), so a
		// direct conversion copies them; if the two ever diverge this stops
		// compiling, forcing an explicit mapping rather than a silent mismatch.
		ports = append(ports, SandboxPortJSON(p))
	}

	out := SandboxContextJSON{
		SchemaVersion: sandboxContextSchemaVersion,
		ContainerName: info.ContainerName,
		ToolName:      info.ToolName,
		WorkspacePath: info.WorkspacePath,
		HomeDir:       info.HomeDir,
		OS:            info.OSName,
		Architecture:  info.Architecture,
		Timezone:      info.Timezone,
		Persistent:    info.Persistent,
		RunAsRoot:     info.RunAsRoot,
		Network: SandboxNetworkJSON{
			Mode:           info.NetworkMode,
			AllowedPorts:   nonNilInts(info.AllowedPorts),
			DNSServers:     nonNilStrings(info.DNSServers),
			AllowedDomains: nonNilStrings(info.AllowedDomains),
		},
		SSHAgentForwarded:  info.SSHAgentForwarded,
		GHCLIAuthenticated: info.GHCLIAuthenticated,
		ForwardedEnvVars:   nonNilStrings(info.ForwardedEnvVars),
		ProtectedPaths:     nonNilStrings(info.ProtectedPaths),
		ExtraMounts:        mounts,
		PublishedPorts:     ports,
		Limits: SandboxLimitsJSON{
			CPU:         info.CPULimit,
			Memory:      info.MemoryLimit,
			MaxDuration: info.MaxDuration,
		},
		ProfileContext: info.ProfileContext,
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal sandbox context JSON: %w", err)
	}
	return string(b) + "\n", nil
}

// nonNilStrings / nonNilInts return an empty (non-nil) slice for a nil input so
// the field marshals as [] rather than null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilInts(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}
