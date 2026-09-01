package config

import (
	"fmt"
	"net"
	"path/filepath"
)

// ValidateNetworkHosts checks that [[network.hosts]] entries are structurally
// sound: each has a valid IPv4 address and at least one syntactically valid
// hostname. Mode-dependent reachability rules (e.g. refusing RFC1918/metadata
// IPs in allowlist mode) are enforced later, when the entries are applied.
func ValidateNetworkHosts(hosts []HostEntry) error {
	for i, h := range hosts {
		if ip := net.ParseIP(h.IP); ip == nil || ip.To4() == nil {
			return fmt.Errorf("network.hosts[%d]: %q is not a valid IPv4 address", i, h.IP)
		}
		if len(h.Hostnames) == 0 {
			return fmt.Errorf("network.hosts[%d] (%s): at least one hostname is required", i, h.IP)
		}
		for _, name := range h.Hostnames {
			if !hostnameRe.MatchString(name) {
				return fmt.Errorf("network.hosts[%d] (%s): %q is not a valid hostname", i, h.IP, name)
			}
		}
		for _, p := range h.Ports {
			if p < 1 || p > 65535 {
				return fmt.Errorf("network.hosts[%d] (%s): port %d is out of range (1-65535)", i, h.IP, p)
			}
		}
	}
	return nil
}

// ShellConfig contains shell session configuration
type ShellConfig struct {
	UseTmux *bool `toml:"use_tmux"` // Use tmux for session management (default: true)
}

// DetectionConfig holds source URLs and local directories for threat-detection databases.
type DetectionConfig struct {
	GTFOBinsSource string `toml:"gtfobins_source"` // Git URL for GTFOBins repo
	GTFOBinsDir    string `toml:"gtfobins_dir"`    // Local clone directory (~/.coi/gtfobins)
	SigmaSource    string `toml:"sigma_source"`    // Git URL for SigmaHQ/sigma repo
	SigmaDir       string `toml:"sigma_dir"`       // Local clone directory (~/.coi/sigma)
}

// Config represents the complete configuration
type Config struct {
	Container          ContainerConfig          `toml:"container"`
	Defaults           DefaultsConfig           `toml:"defaults"`
	Paths              PathsConfig              `toml:"paths"`
	Incus              IncusConfig              `toml:"incus"`
	Network            NetworkConfig            `toml:"network"`
	Tool               ToolConfig               `toml:"tool"`
	Shell              ShellConfig              `toml:"shell"`
	Mounts             MountsConfig             `toml:"mounts"`
	Sockets            []SocketEntry            `toml:"sockets"`
	Ports              PortsConfig              `toml:"ports"`
	Credentials        []CredentialEntry        `toml:"credentials"`
	Limits             LimitsConfig             `toml:"limits"`
	Git                GitConfig                `toml:"git"`
	SSH                SSHConfig                `toml:"ssh"`
	Security           SecurityConfig           `toml:"security"`
	Monitoring         MonitoringConfig         `toml:"monitoring"`
	Timezone           TimezoneConfig           `toml:"timezone"`
	Detection          DetectionConfig          `toml:"detection"`
	Profiles           map[string]ProfileConfig `toml:"-"` // Populated by loadProfileDirectories, not from TOML
	ProfileContextFile string                   `toml:"-"` // Set by ApplyProfile, read by session setup
}

// BuildConfig defines how to build the project's custom image
type BuildConfig struct {
	Base        string   `toml:"base"`        // Base image (default: "coi")
	Script      string   `toml:"script"`      // Path to build script (relative to config file or absolute)
	Commands    []string `toml:"commands"`    // Inline build commands (alternative to script)
	Compression string   `toml:"compression"` // Image compression algorithm (e.g. "none", "gzip", "xz"; empty = Incus default)
	// Agents selects which AI agents the base image installs (e.g. ["claude"]).
	// Empty/unset installs the default agent set (opt-in agents like codex are
	// excluded — request them explicitly, #698). Names are validated against
	// the tool registry at build time. Issue #454.
	Agents []string `toml:"agents"`
}

// HasBuildConfig returns true if a build configuration is defined (script or commands)
func (b *BuildConfig) HasBuildConfig() bool {
	return b.Script != "" || len(b.Commands) > 0
}

// ContainerConfig consolidates container-shape settings introduced in 0.8.0:
// image, persistence, storage pool, and build configuration.
// Replaces the legacy split of [defaults] image, [defaults] persistent,
// and top-level [build]. The same struct is embedded in both Config and
// ProfileConfig so global and profile configs are symmetric.
type ContainerConfig struct {
	Image           string      `toml:"image"`
	Persistent      *bool       `toml:"persistent"`
	ShutdownTimeout int         `toml:"shutdown_timeout"` // Seconds to wait for graceful shutdown before force-killing (default: 60)
	ReadyTimeout    int         `toml:"ready_timeout"`    // Seconds to wait for a launched container to become ready (default: 30)
	StoragePool     string      `toml:"storage_pool"`
	Alias           string      `toml:"alias"`
	Build           BuildConfig `toml:"build"`
	StaleBaseCheck  string      `toml:"stale_base_check"` // "error", "warn", "off"

	// SessionName decouples the session identity from the workspace path.
	// When set, container names, slot allocation, port allocation, and the
	// saved-session store are all keyed on this name instead of a hash of the
	// workspace's absolute path — so the same persistent session continues
	// (--resume/--continue) even when the workspace is mounted at a different
	// location. Intended for profiles: point different checkouts of a project
	// at one named session. Honored from TRUSTED scope only (~/.coi or
	// COI_CONFIG, and profiles under them): session identity selects which
	// persistent container and saved session state (conversation history,
	// restored credentials) a launch attaches to, so a cloned repo must not
	// be able to attach itself to another project's session.
	SessionName string `toml:"session_name"`
}

// HasContainerConfig reports whether any field is set.
func (c *ContainerConfig) HasContainerConfig() bool {
	return c.Image != "" ||
		c.Persistent != nil ||
		c.ShutdownTimeout != 0 ||
		c.ReadyTimeout != 0 ||
		c.StoragePool != "" ||
		c.Alias != "" ||
		c.StaleBaseCheck != "" ||
		c.SessionName != "" ||
		c.Build.HasBuildConfig()
}

// ShutdownTimeoutSeconds returns the graceful-shutdown window in seconds,
// defaulting to DefaultShutdownTimeoutSeconds when unset.
func (c *ContainerConfig) ShutdownTimeoutSeconds() int {
	if c.ShutdownTimeout <= 0 {
		return DefaultShutdownTimeoutSeconds
	}
	return c.ShutdownTimeout
}

// ReadyTimeoutSeconds returns the container-readiness window in seconds,
// defaulting to 30 when unset. Like the shutdown window, how long to wait
// for a boot is policy, not a per-invocation whim — slow hosts (nested
// virtualization, cold storage pools, loaded CI runners) occasionally need
// more than the default.
func (c *ContainerConfig) ReadyTimeoutSeconds() int {
	if c.ReadyTimeout <= 0 {
		return 30
	}
	return c.ReadyTimeout
}

// TimezoneConfig contains timezone settings for containers
type TimezoneConfig struct {
	Mode string `toml:"mode"` // "host" (default), "fixed", "utc"
	Name string `toml:"name"` // IANA timezone name, only used when mode = "fixed"
}

// SSHConfig contains SSH-related settings
type SSHConfig struct {
	ForwardAgent *bool `toml:"forward_agent"` // Forward host SSH agent to container (default: false)
}

// GitConfig contains git-related security settings
type GitConfig struct {
	WritableHooks *bool `toml:"writable_hooks"` // Allow container to write to .git/hooks (default: false)
	// Name and Email pin an explicit commit identity into the container's global
	// git config, taking precedence over the host's global git config. They are
	// honored only from trusted-scope config (~/.coi/config.toml / $COI_CONFIG);
	// an untrusted project config choosing the author identity is stripped by
	// sanitizeUntrustedGit (a cloned repo must not pick who its commits appear to
	// be authored by).
	Name  string `toml:"name"`
	Email string `toml:"email"`
	// SeedHostIdentity controls whether COI reads the host's global
	// `git config --global user.name/user.email` and installs it in the container
	// when no explicit Name/Email is given. Defaults to true. Set false to keep
	// the fail-closed guard only (git refuses commits until the tool sets an
	// identity), e.g. to avoid copying the host identity into the container.
	SeedHostIdentity *bool `toml:"seed_host_identity"`
	// Readonly, when true, LOCKS the configured identity: instead of writing the
	// container's ~/.gitconfig (which the agent can overwrite), COI mounts the
	// identity read-only at ~/.gitconfig. This locks the WHOLE global gitconfig, so
	// ANY `git config --global …` in the container (name/email, aliases, editor,
	// credential.helper, …) fails on a read-only filesystem — use per-repo
	// `--local` config for anything else. Only takes effect with a resolvable
	// identity (name/email or a seeded host identity); if it cannot be applied the
	// session fails closed rather than falling back to writable. Trusted-scope only,
	// like name/email. Default false (writable, as before).
	Readonly *bool `toml:"readonly"`
}

// IsSeedHostIdentityEnabled reports whether host-global git identity seeding is
// enabled. Defaults to true when the field is not explicitly set (nil receiver
// or nil field), matching the pre-flag behavior.
func (g *GitConfig) IsSeedHostIdentityEnabled() bool {
	if g == nil || g.SeedHostIdentity == nil {
		return true
	}
	return *g.SeedHostIdentity
}

// IsReadonlyEnabled reports whether the configured git identity should be locked
// read-only in the container. Default false (nil receiver or field).
func (g *GitConfig) IsReadonlyEnabled() bool {
	return g != nil && g.Readonly != nil && *g.Readonly
}

// SecurityConfig contains security-related settings for workspace protection
type SecurityConfig struct {
	// ProtectedPaths is a list of paths (relative to workspace) to mount read-only
	// These paths are protected to prevent containers from modifying files that could
	// execute automatically on the host (e.g., git hooks, IDE configs, etc.)
	// Defaults: [".git/hooks", ".git/config", ".husky", ".vscode"]
	ProtectedPaths []string `toml:"protected_paths"`
	// AdditionalProtectedPaths allows adding more paths without replacing defaults
	AdditionalProtectedPaths []string `toml:"additional_protected_paths"`
	// DisableProtection completely disables read-only mounting of protected paths
	DisableProtection bool `toml:"disable_protection"`
	// HostImmutable applies the Linux immutable attribute (chattr +i) on protected
	// paths on the host before starting the container. This prevents the unshare+umount
	// bypass of read-only bind mounts. Requires CAP_LINUX_IMMUTABLE on the coi binary.
	// Default: true. Set to false to disable.
	HostImmutable *bool `toml:"host_immutable"`
	// WritablePaths removes specific entries from the effective protected paths,
	// re-allowing the container to write them (e.g. [".claude/settings.json"] to
	// let the agent manage its own project settings). It is the generic opt-out
	// for any protected path. Only honored from trusted-scope config
	// (~/.coi/config.toml or $COI_CONFIG) — an untrusted project config cannot
	// remove protections (see sanitizeUntrustedSecurity), so a cloned repo cannot
	// turn off read-only protection of host-auto-executing files.
	WritablePaths []string `toml:"writable_paths"`
	// SecretPaths is a list of workspace-relative globs to MASK inside the
	// container: each match is mounted read-only over an empty file/dir so the
	// contained agent can neither read its contents nor modify it (e.g.
	// [".env", "*.pem", "secrets/**"]). The host file is left untouched. Unlike
	// protected_paths (read-only, but contents stay readable), this hides
	// contents — repo-local secret read-exfil + tamper protection. Purely
	// additive, so it is honored from any scope and merges as a union: an
	// untrusted project config can only ADD denies, never remove them.
	SecretPaths []string `toml:"secret_paths"`
}

// GetEffectiveProtectedPaths returns the combined list of protected paths
// (protected_paths + additional_protected_paths) minus any writable_paths
// opt-outs. Matching is slash-normalized so config entries are platform-stable.
func (s *SecurityConfig) GetEffectiveProtectedPaths() []string {
	if s.DisableProtection {
		return nil
	}
	writable := make(map[string]bool, len(s.WritablePaths))
	for _, w := range s.WritablePaths {
		writable[filepath.ToSlash(w)] = true
	}
	paths := make([]string, 0, len(s.ProtectedPaths)+len(s.AdditionalProtectedPaths))
	for _, p := range append(append([]string{}, s.ProtectedPaths...), s.AdditionalProtectedPaths...) {
		if writable[filepath.ToSlash(p)] {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// IsWritablePath reports whether relPath was explicitly opted out of protection
// via [security] writable_paths (a trusted-scope-only list). Separators are
// normalized so an entry written with either slash style still matches. Callers
// that discover protected paths dynamically (e.g. per-worktree git configs that
// GetEffectiveProtectedPaths cannot enumerate) use this to honor the same opt-out.
func (s *SecurityConfig) IsWritablePath(relPath string) bool {
	target := filepath.ToSlash(relPath)
	for _, w := range s.WritablePaths {
		if filepath.ToSlash(w) == target {
			return true
		}
	}
	return false
}

// IsHostImmutableEnabled returns whether host-side immutable protection is enabled.
// Defaults to true when the field is not explicitly set.
func (s *SecurityConfig) IsHostImmutableEnabled() bool {
	if s.HostImmutable == nil {
		return true
	}
	return *s.HostImmutable
}

// DefaultsConfig contains default settings
type DefaultsConfig struct {
	// Profile names the profile to apply when `--profile` is not passed, so a
	// user's opinionated setup applies without retyping it (#607). `coi` gives
	// this profile; `coi --profile default` still gives the synthesized clone of
	// global config. Honored ONLY from trusted-scope config (an untrusted
	// project config redirecting the no-flag default could downgrade the user's
	// chosen environment) — stripped from project config at load time. A name
	// that does not resolve to a known profile is a hard error at startup.
	Profile     string            `toml:"profile"`
	ForwardEnv  []string          `toml:"forward_env"`
	Environment map[string]string `toml:"environment"`
	// EnvCommands maps env var names to host commands run at session start; the
	// trimmed stdout becomes the value. Honored ONLY from trusted-scope config
	// (running a host command is host code execution) — stripped from untrusted
	// project config/profiles at load time. The minted value is present in the
	// container env for the session.
	EnvCommands map[string]string `toml:"env_commands"`
	// EnvCommandTimeout bounds each env_commands invocation (duration string,
	// e.g. "30s"). Empty defaults to 30s.
	EnvCommandTimeout string `toml:"env_command_timeout"`
}

// PathsConfig contains path settings
type PathsConfig struct {
	SessionsDir           string `toml:"sessions_dir"`
	StorageDir            string `toml:"storage_dir"`
	LogsDir               string `toml:"logs_dir"`
	PreserveWorkspacePath bool   `toml:"preserve_workspace_path"` // Mount workspace at same path as host (e.g., /home/user/project instead of /workspace)
}

// IncusConfig contains Incus-specific settings
type IncusConfig struct {
	Project      string `toml:"project"`
	Group        string `toml:"group"`
	CodeUID      int    `toml:"code_uid"`
	CodeUser     string `toml:"code_user"`
	DisableShift bool   `toml:"disable_shift"` // Force UID shifting off; use raw.idmap instead. Rarely needed now: coi statfs's the workspace source and skips shift on its own for FUSE-family filesystems, which covers the OrbStack/Colima/Lima host shares this used to be set by hand for (#683). Keep it for a source coi's check clears but that still can't do idmapped mounts — the symptom is a start failure with "idmapping abilities are required but aren't supported on system" (#678), or a workspace that mounts but is unwritable.
}

// NetworkMode represents the network isolation mode
type NetworkMode string

// NetworkConfig contains network isolation settings
type NetworkConfig struct {
	Mode                  NetworkMode `toml:"mode"`
	BlockPrivateNetworks  *bool       `toml:"block_private_networks"`
	BlockMetadataEndpoint *bool       `toml:"block_metadata_endpoint"`
	// AllowedDomains is the allowlist-mode reachable-destination list: hostnames,
	// IPv4 addresses, and IPv4 CIDRs. Each entry may carry a per-destination port
	// scope as ":ports" — a comma list of single ports and/or lo-hi ranges, e.g.
	// "github.com:443", "192.168.1.50:8080", "10.0.0.0/8:22", "svc:8000-8100". An
	// entry with no port inherits the global allowed_ports (else all ports), so a
	// per-entry scope tightens just that destination. IPv4 only; wildcards rejected.
	AllowedDomains          []string `toml:"allowed_domains"`
	RefreshIntervalMinutes  int      `toml:"refresh_interval_minutes"`
	AllowLocalNetworkAccess *bool    `toml:"allow_local_network_access"` // Allow established connections from entire local network (not just gateway)
	// UseSudo controls whether COI may invoke `sudo` for network operations (nft,
	// iptables). Defaults to true. When false, COI never shells out to sudo: it
	// behaves as if passwordless sudo were unavailable, so `restricted`/`allowlist`
	// modes error out (no silent downgrade) and `open` mode runs without privileged
	// rules. For users who decline the installer's /etc/sudoers.d/coi-nft rule.
	UseSudo *bool                `toml:"use_sudo"`
	Logging NetworkLoggingConfig `toml:"logging"`
	// Hosts are static name→address entries written into the container's
	// /etc/hosts, with firewall reachability applied to match the active mode
	// (#605). Honored ONLY from trusted-scope config — a name→IP mapping is a
	// spoofing primitive and reachability punches a firewall hole, so an
	// untrusted project config's entries are stripped at load time.
	Hosts []HostEntry `toml:"hosts"`
	// DNSServers pins the resolvers the container may reach on port 53. In
	// restricted mode COI accepts :53 only to these addresses and rejects every
	// other off-box DNS query, so a compromised container cannot bypass your
	// resolver by talking straight to a public one (e.g. 8.8.8.8). The pin applies
	// to ALL destinations, including the LAN and even when allow_local_network_access
	// is set — so a LAN resolver (a Pi-hole) must be listed here by its exact IP to
	// stay reachable on :53. The bridge's own resolver (input path) is left
	// untouched, so normal resolution keeps working. IPv4 addresses only.
	// Incompatible with allowlist mode, which blocks all DNS and uses /etc/hosts.
	// Honored ONLY from trusted-scope config: a resolver pin from an untrusted
	// project config is a DNS-redirect primitive, so untrusted entries are stripped
	// at load time (see sanitizeUntrustedNetwork).
	DNSServers []string `toml:"dns_servers"`
	// AllowedPorts restricts outbound destination ports. When set, only these
	// TCP/UDP dports are allowed to permitted destinations (ICMP echo still
	// works); everything else is rejected. In restricted mode it caps the
	// otherwise-open internet egress; in allowlist mode it further constrains the
	// allowlisted hosts. The cap applies to LAN traffic too — even when
	// allow_local_network_access is set, the local network is reachable only on
	// these ports (so a compromised container cannot pivot to SSH/DBs on the LAN).
	// nil/empty keeps the default (all ports). Bridge-provided DNS is unaffected,
	// so include 53 here if the container resolves via an off-box resolver. Honored
	// ONLY from trusted-scope config.
	AllowedPorts []int `toml:"allowed_ports"`
}

// HostEntry maps one IPv4 address to one or more hostnames for the container's
// /etc/hosts. It is the config form of `[[network.hosts]]`.
//
// Ports optionally scopes the firewall reachability of this host to specific
// TCP/UDP destination ports (e.g. [443]) — the same per-destination cap Phase 3
// gives allowed_domains, so you can open a single LAN service (redmine:443)
// without widening the rest of egress. Empty inherits the global allowed_ports
// (else all ports). Applied in restricted and allowlist mode; ignored in open
// mode, which blocks nothing.
type HostEntry struct {
	IP        string   `toml:"ip"`
	Hostnames []string `toml:"hostnames"`
	Ports     []int    `toml:"ports"`
}

// NetworkLoggingConfig contains network logging settings
type NetworkLoggingConfig struct {
	Enabled *bool  `toml:"enabled"`
	Path    string `toml:"path"`
}

// ProfileConfig represents a named profile
type ProfileConfig struct {
	Inherits    string            `toml:"inherits"` // Parent profile name for inheritance
	Container   ContainerConfig   `toml:"container"`
	Context     string            `toml:"context"` // Path to context .md file (resolved relative to profile dir)
	Environment map[string]string `toml:"environment"`
	EnvCommands map[string]string `toml:"env_commands"`
	Limits      *LimitsConfig     `toml:"limits"`
	Tool        *ToolConfig       `toml:"tool"`
	Mounts      []MountEntry      `toml:"mounts"`
	Sockets     []SocketEntry     `toml:"sockets"`
	Ports       *PortsConfig      `toml:"ports"`
	Credentials []CredentialEntry `toml:"credentials"`
	Network     *NetworkConfig    `toml:"network"`
	ForwardEnv  []string          `toml:"forward_env"`
	Source      string            `toml:"-"` // Where this profile was loaded from (not serialized)
	// Trusted records whether the profile was loaded from a trusted scan root
	// (~/.coi or the COI_CONFIG dir), stamped by loadProfileDirectories at
	// load time — the authoritative signal for post-inheritance trust checks,
	// instead of lexically reconstructing the root from Source.
	Trusted bool `toml:"-"`

	// Extended fields — previously Config-only, now available in profiles
	Paths      *PathsConfig      `toml:"paths"`
	Incus      *IncusConfig      `toml:"incus"`
	Git        *GitConfig        `toml:"git"`
	SSH        *SSHConfig        `toml:"ssh"`
	Security   *SecurityConfig   `toml:"security"`
	Monitoring *MonitoringConfig `toml:"monitoring"`
	Timezone   *TimezoneConfig   `toml:"timezone"`
	Shell      *ShellConfig      `toml:"shell"`
}

// ToolConfig represents AI coding tool configuration
type ToolConfig struct {
	Name            string           `toml:"name"`              // Tool name: "claude", "aider", "cursor", etc.
	Binary          string           `toml:"binary"`            // Binary name to execute (if empty, uses tool name)
	PermissionMode  string           `toml:"permission_mode"`   // Permission mode: "bypass" (default) or "interactive"
	ContextFile     string           `toml:"context_file"`      // Path to custom context .md file (supports ~ expansion; trusted scope only — reads a host file into the container)
	AutoContext     *bool            `toml:"auto_context"`      // Auto-inject sandbox context into tool's native system (default: true)
	ContextJSON     *bool            `toml:"context_json"`      // Write ~/SANDBOX_CONTEXT.json for programmatic consumers (default: true)
	ContextJSONFile string           `toml:"context_json_file"` // Path to custom context .json file (supports ~ expansion; overrides the generated JSON; trusted scope only)
	Claude          ClaudeToolConfig `toml:"claude"`            // Claude-specific settings
	Codex           CodexToolConfig  `toml:"codex"`             // Codex-specific settings
}

// ClaudeToolConfig contains Claude Code-specific settings
type ClaudeToolConfig struct {
	EffortLevel string `toml:"effort_level"` // Effort level: "low", "medium", "high", "xhigh", "max", "auto" (unset = user controls interactively)
	Model       string `toml:"model"`        // Claude model, delivered as ANTHROPIC_MODEL (e.g. "opus", "claude-opus-4-8"); unset = Claude Code's own default
}

// CodexToolConfig contains OpenAI Codex CLI-specific settings
type CodexToolConfig struct {
	Model           string `toml:"model"`            // Codex model, delivered as -m (e.g. "gpt-5-codex"); unset = codex's own default
	ReasoningEffort string `toml:"reasoning_effort"` // "minimal", "low", "medium", "high" — delivered as -c model_reasoning_effort=<v>; unset = codex's own default
}

// MountEntry represents a single directory mount configuration
type MountEntry struct {
	Host      string `toml:"host"`      // Host path (supports ~ expansion)
	Container string `toml:"container"` // Container path (must be absolute)
	Readonly  bool   `toml:"readonly"`  // Mount read-only (default: false)

	// Untrusted is set programmatically (never from TOML) when this mount was
	// loaded from an untrusted, project-scope config file. Such mounts that
	// resolve outside the workspace are gated behind explicit trust (`coi trust`)
	// to prevent a cloned repo from bind-mounting host paths writable (host RCE).
	Untrusted bool `toml:"-"`
	// SourcePath is the absolute path of the config file this mount came from.
	// Only populated for untrusted mounts; used to look up/record trust.
	SourcePath string `toml:"-"`
}

// MountsConfig contains mount-related configuration
type MountsConfig struct {
	Default []MountEntry `toml:"default"` // Default mounts for all sessions
}

// SocketEntry forwards a host unix socket into the container via an Incus proxy
// device (the secret/endpoint stays on the host; only the socket crosses in).
// It generalizes SSH agent forwarding; the SSH agent (`[ssh] forward_agent`)
// remains a built-in case synthesized at setup time.
type SocketEntry struct {
	Host      string `toml:"host"`      // Host socket path (supports ~ expansion)
	Container string `toml:"container"` // In-container socket path (must be absolute)
	Env       string `toml:"env"`       // Optional env var NAME set to the container path

	// Untrusted/SourcePath are set programmatically (never from TOML) when this
	// entry came from an untrusted, project-scope config file. Forwarding a host
	// socket exposes a host capability, so untrusted entries are gated behind
	// explicit trust (`coi trust`), like escaping mounts.
	Untrusted  bool   `toml:"-"`
	SourcePath string `toml:"-"`
}

// CredentialEntry represents a single host credential source to copy into a
// container: either a reference to a named catalog bundle (see
// internal/tool/credentials), or an ad-hoc host/container file pair.
// Exactly one of Bundle, or Host+Container, must be set.
type CredentialEntry struct {
	Bundle    string `toml:"bundle"`    // catalog bundle name, e.g. "ollama"
	Host      string `toml:"host"`      // ad-hoc host path (supports ~ expansion)
	Container string `toml:"container"` // ad-hoc container path (must be absolute)
	Mode      string `toml:"mode"`      // optional chmod mode, e.g. "0600"

	// Untrusted/SourcePath are set programmatically (never from TOML) when
	// this entry came from an untrusted, project-scope config file. Only
	// ad-hoc entries (Bundle == "") are ever marked Untrusted. A bundle
	// reference can only select a name from coi's own vetted catalog, not an
	// attacker-chosen host path, so it carries the same trust level the
	// builtin tool credential seeding already has.
	Untrusted  bool   `toml:"-"`
	SourcePath string `toml:"-"`
}

// PortsConfig publishes container TCP ports on the host via Incus proxy
// devices, so agent-started services inside a container are reachable as
// localhost:<port> on the host (#558). Two complementary forms:
//
//   - Pool: N identity-mapped ports per session (host port == container
//     port), allocated per (workspace, slot) — see internal/session
//     AllocateHostPort — and announced to the agent via the sandbox context
//     file and COI_PORTS. Zero per-service declarations: the agent binds
//     any pool port and the user opens the SAME number on localhost.
//   - Map: named entries for services with fixed container ports (e.g. a
//     compose stack's postgres on 5432); host side auto-allocated or pinned.
//
// Allocation is deterministic with no coordination state, so the section is
// safe to share via profiles: parallel slots and different workspaces get
// distinct, stable ports by construction. Pinned host ports are bind-probed
// before any container work and abort the session if taken.
type PortsConfig struct {
	Pool int         `toml:"pool"` // Number of identity-mapped ports to publish (0 = none)
	Map  []PortEntry `toml:"map"`  // Named fixed-container-port publications

	// PoolUntrusted/PoolSourcePath are set programmatically (never from
	// TOML) when the pool value came from an untrusted, project-scope config
	// file; map entries carry their own per-entry flags so mixed
	// trusted+untrusted scopes gate independently. A repo declaring host
	// listeners can squat well-known localhost ports, so untrusted
	// pool/entries are gated behind explicit trust (`coi trust`).
	// PoolTrustedFallback remembers a trusted pool value that an untrusted
	// overlay overwrote, so the trust gate restores it instead of dropping
	// the user's own pool to zero (see mergePortsInto).
	PoolUntrusted       bool   `toml:"-"`
	PoolSourcePath      string `toml:"-"`
	PoolTrustedFallback int    `toml:"-"`
}

// HasPorts reports whether the section declares anything to publish.
func (p *PortsConfig) HasPorts() bool {
	return p != nil && (p.Pool > 0 || len(p.Map) > 0)
}

// PortEntry is one named [[ports.map]] publication.
type PortEntry struct {
	Name      string `toml:"name"`      // Identifier; exposed as COI_PORT_<NAME> (upper-cased)
	Container int    `toml:"container"` // TCP port inside the container (1-65535)
	Host      int    `toml:"host"`      // Optional exact host port (0 = auto per workspace/slot)
	Listen    string `toml:"listen"`    // Host listen address (default "127.0.0.1")

	// Untrusted/SourcePath: set programmatically for entries from an
	// untrusted, project-scope config file (see PortsConfig).
	Untrusted  bool   `toml:"-"`
	SourcePath string `toml:"-"`
}

// LimitsConfig contains resource and time limits for containers
type LimitsConfig struct {
	CPU     CPULimits     `toml:"cpu"`
	Memory  MemoryLimits  `toml:"memory"`
	Disk    DiskLimits    `toml:"disk"`
	Runtime RuntimeLimits `toml:"runtime"`
}

// CPULimits contains CPU resource limits
type CPULimits struct {
	Count     string `toml:"count"`     // "2", "0-3", "" (unlimited)
	Allowance string `toml:"allowance"` // "50%", "25ms/100ms"
	Priority  int    `toml:"priority"`  // 0-10
}

// MemoryLimits contains memory resource limits
type MemoryLimits struct {
	Limit   string `toml:"limit"`   // "512MiB", "2GiB", "50%", "" (unlimited)
	Enforce string `toml:"enforce"` // "hard" or "soft"
	Swap    string `toml:"swap"`    // "true", "false", or size
}

// DiskLimits contains disk I/O resource limits
type DiskLimits struct {
	Read      string `toml:"read"`       // "10MiB/s", "1000iops", "" (unlimited)
	Write     string `toml:"write"`      // "5MiB/s", "1000iops", "" (unlimited)
	Max       string `toml:"max"`        // combined read+write limit
	Priority  int    `toml:"priority"`   // 0-10
	TmpfsSize string `toml:"tmpfs_size"` // /tmp size: "2GiB", "1024MiB" (default: "2GiB")
}

// RuntimeLimits contains time-based and process limits
type RuntimeLimits struct {
	MaxDuration  string `toml:"max_duration"`  // "2h", "30m", "1h30m", "" (unlimited)
	MaxProcesses int    `toml:"max_processes"` // 0 = unlimited
	AutoStop     *bool  `toml:"auto_stop"`     // auto-stop when limit reached
	StopGraceful *bool  `toml:"stop_graceful"` // graceful vs force stop
}

// NFTMonitoringConfig contains nftables-based network monitoring settings
type NFTMonitoringConfig struct {
	Enabled            *bool  `toml:"enabled"`               // Enable nftables monitoring
	RateLimitPerSecond int    `toml:"rate_limit_per_second"` // Limit log volume (default 100)
	DNSQueryThreshold  int    `toml:"dns_query_threshold"`   // Alert if >N queries/min (default 100)
	LogDNSQueries      *bool  `toml:"log_dns_queries"`       // Separate DNS logging (default true)
	LimaHost           string `toml:"lima_host"`             // Lima host for macOS (e.g., "lima-default")
}

// MonitoringConfig contains security monitoring settings
type MonitoringConfig struct {
	Enabled                   *bool               `toml:"enabled"`                      // Enable background monitoring
	AutoPauseOnHigh           *bool               `toml:"auto_pause_on_high"`           // Pause container on high-severity threats
	AutoKillOnCritical        *bool               `toml:"auto_kill_on_critical"`        // Kill container on critical threats
	PollIntervalSec           int                 `toml:"poll_interval_sec"`            // How often to collect stats
	FileReadThresholdMB       float64             `toml:"file_read_threshold_mb"`       // MB read in poll interval before alert
	FileReadRateMBPerSec      float64             `toml:"file_read_rate_mb_per_sec"`    // MB/sec sustained rate before alert
	ProcessCountThreshold     int                 `toml:"process_count_threshold"`      // Max processes before fork-bomb alert (0 = disabled)
	ProcessSpawnRateThreshold *int                `toml:"process_spawn_rate_threshold"` // Max processes spawned per poll interval (0 = disabled, nil = inherit default)
	AuditLogRetentionDays     int                 `toml:"audit_log_retention_days"`     // How long to keep audit logs
	NFT                       NFTMonitoringConfig `toml:"nft"`                          // nftables network monitoring
}

// SudoAllowed reports whether COI may invoke `sudo` for network operations.
// Defaults to true; set `[network] use_sudo = false` to opt out (no sudoers
// rule required, at the cost of restricted/allowlist enforcement).
func (n *NetworkConfig) SudoAllowed() bool {
	return n == nil || n.UseSudo == nil || *n.UseSudo
}
