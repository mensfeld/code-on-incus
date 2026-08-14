package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/BurntSushi/toml"
)

// hostnameRe matches a single DNS name (RFC1123-ish): dot-separated labels of
// letters/digits/hyphens, each 1–63 chars and not starting/ending with a hyphen.
var hostnameRe = regexp.MustCompile(
	`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

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
	// Empty/unset installs all supported agents (the default). Names are validated
	// against the tool registry at build time. Issue #454.
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

// DefaultShutdownTimeoutSeconds is the graceful-shutdown window applied when
// [container] shutdown_timeout is unset — the single source for every
// consumer (coi shutdown, session cleanup's wait for an in-flight poweroff).
const DefaultShutdownTimeoutSeconds = 60

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

const (
	// NetworkModeRestricted blocks local/internal networks, allows internet
	NetworkModeRestricted NetworkMode = "restricted"
	// NetworkModeOpen allows all network access (current behavior)
	NetworkModeOpen NetworkMode = "open"
	// NetworkModeAllowlist allows only specific domains (with RFC1918 always blocked)
	NetworkModeAllowlist NetworkMode = "allowlist"
)

// NetworkConfig contains network isolation settings
type NetworkConfig struct {
	Mode                    NetworkMode `toml:"mode"`
	BlockPrivateNetworks    *bool       `toml:"block_private_networks"`
	BlockMetadataEndpoint   *bool       `toml:"block_metadata_endpoint"`
	AllowedDomains          []string    `toml:"allowed_domains"`
	RefreshIntervalMinutes  int         `toml:"refresh_interval_minutes"`
	AllowLocalNetworkAccess *bool       `toml:"allow_local_network_access"` // Allow established connections from entire local network (not just gateway)
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
	// resolver by talking straight to a public one (e.g. 8.8.8.8). The bridge's
	// own resolver (input path) is left untouched, so normal resolution keeps
	// working. IPv4 addresses only. Incompatible with allowlist mode, which
	// blocks all DNS and uses /etc/hosts. Honored ONLY from trusted-scope config:
	// a resolver pin from an untrusted project config is a DNS-redirect primitive,
	// so untrusted entries are stripped at load time (see sanitizeUntrustedNetwork).
	DNSServers []string `toml:"dns_servers"`
	// AllowedPorts restricts outbound destination ports. When set, only these
	// TCP/UDP dports are allowed to permitted destinations (ICMP echo still
	// works); everything else is rejected. In restricted mode it caps the
	// otherwise-open internet egress; in allowlist mode it further constrains the
	// allowlisted hosts. nil/empty keeps the default (all ports). Bridge-provided
	// DNS is unaffected, so include 53 here if the container resolves via an
	// off-box resolver. Honored ONLY from trusted-scope config.
	AllowedPorts []int `toml:"allowed_ports"`
}

// HostEntry maps one IPv4 address to one or more hostnames for the container's
// /etc/hosts. It is the config form of `[[network.hosts]]`.
type HostEntry struct {
	IP        string   `toml:"ip"`
	Hostnames []string `toml:"hostnames"`
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
	Name           string           `toml:"name"`            // Tool name: "claude", "aider", "cursor", etc.
	Binary         string           `toml:"binary"`          // Binary name to execute (if empty, uses tool name)
	PermissionMode string           `toml:"permission_mode"` // Permission mode: "bypass" (default) or "interactive"
	ContextFile    string           `toml:"context_file"`    // Path to custom context .md file (supports ~ expansion)
	AutoContext    *bool            `toml:"auto_context"`    // Auto-inject sandbox context into tool's native system (default: true)
	Claude         ClaudeToolConfig `toml:"claude"`          // Claude-specific settings
}

// ClaudeToolConfig contains Claude Code-specific settings
type ClaudeToolConfig struct {
	EffortLevel string `toml:"effort_level"` // Effort level: "low", "medium", "high", "xhigh", "max", "auto" (unset = user controls interactively)
	Model       string `toml:"model"`        // Claude model, delivered as ANTHROPIC_MODEL (e.g. "opus", "claude-opus-4-8"); unset = Claude Code's own default
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

// GetDefaultConfig returns the default configuration by parsing the embedded default config TOML.
func GetDefaultConfig() *Config {
	cfg := &Config{}
	if _, err := toml.Decode(string(EmbeddedDefaultConfig), cfg); err != nil {
		// Fatal: embedded config is broken — programming error
		panic(fmt.Sprintf("failed to parse embedded default config: %v", err))
	}

	// Expand ~ in all path fields
	expandConfigPaths(cfg)

	// Initialize runtime-only fields
	cfg.Profiles = make(map[string]ProfileConfig)

	// Ensure empty slices are initialized (TOML doesn't set them)
	if cfg.Security.AdditionalProtectedPaths == nil {
		cfg.Security.AdditionalProtectedPaths = []string{}
	}
	if cfg.Mounts.Default == nil {
		cfg.Mounts.Default = []MountEntry{}
	}

	return cfg
}

// expandConfigPaths expands ~ in all path fields of the config.
// Called once at the end of Merge and ApplyProfile so that path-merging
// helpers can work with raw (unexpanded) strings.
func expandConfigPaths(cfg *Config) {
	cfg.Paths.SessionsDir = ExpandPath(cfg.Paths.SessionsDir)
	cfg.Paths.StorageDir = ExpandPath(cfg.Paths.StorageDir)
	cfg.Paths.LogsDir = ExpandPath(cfg.Paths.LogsDir)
	cfg.Tool.ContextFile = ExpandPath(cfg.Tool.ContextFile)
	cfg.Network.Logging.Path = ExpandPath(cfg.Network.Logging.Path)
	cfg.Detection.GTFOBinsDir = ExpandPath(cfg.Detection.GTFOBinsDir)
	cfg.Detection.SigmaDir = ExpandPath(cfg.Detection.SigmaDir)
}

// cloneSlice returns a shallow copy of a slice (nil-safe).
func cloneSlice[S ~[]E, E any](in S) S {
	if in == nil {
		return nil
	}
	out := make(S, len(in))
	copy(out, in)
	return out
}

// cloneMap returns a shallow copy of a map (nil-safe).
func cloneMap[M ~map[K]V, K comparable, V any](in M) M {
	if in == nil {
		return nil
	}
	out := make(M, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// synthesizeDefaultProfile creates a ProfileConfig from the loaded Config,
// representing the "default" built-in profile.
// All struct fields are copied by value (not aliased) so that applying
// or merging the default profile cannot mutate the original Config.
func synthesizeDefaultProfile(cfg *Config) ProfileConfig {
	limits := cfg.Limits
	tool := cfg.Tool
	shell := cfg.Shell
	network := cfg.Network
	network.AllowedDomains = cloneSlice(cfg.Network.AllowedDomains)
	network.DNSServers = cloneSlice(cfg.Network.DNSServers)
	network.AllowedPorts = cloneSlice(cfg.Network.AllowedPorts)
	paths := cfg.Paths
	incus := cfg.Incus
	git := cfg.Git
	ssh := cfg.SSH
	security := cfg.Security
	security.ProtectedPaths = cloneSlice(cfg.Security.ProtectedPaths)
	security.AdditionalProtectedPaths = cloneSlice(cfg.Security.AdditionalProtectedPaths)
	security.WritablePaths = cloneSlice(cfg.Security.WritablePaths)
	security.SecretPaths = cloneSlice(cfg.Security.SecretPaths)
	monitoring := cfg.Monitoring
	timezone := cfg.Timezone

	container := cfg.Container
	container.Build.Commands = cloneSlice(cfg.Container.Build.Commands)
	container.Build.Agents = cloneSlice(cfg.Container.Build.Agents)

	p := ProfileConfig{
		Container:   container,
		Environment: cloneMap(cfg.Defaults.Environment),
		EnvCommands: cloneMap(cfg.Defaults.EnvCommands),
		ForwardEnv:  cloneSlice(cfg.Defaults.ForwardEnv),
		Limits:      &limits,
		Tool:        &tool,
		Shell:       &shell,
		Network:     &network,
		Mounts:      cloneSlice(cfg.Mounts.Default),
		Sockets:     cloneSlice(cfg.Sockets),
		Ports:       clonePortsConfig(&cfg.Ports),
		Credentials: cloneSlice(cfg.Credentials),
		Paths:       &paths,
		Incus:       &incus,
		Git:         &git,
		SSH:         &ssh,
		Security:    &security,
		Monitoring:  &monitoring,
		Timezone:    &timezone,
		Source:      "(built-in)",
	}
	return p
}

// HardenedProfileSecretPaths is the default secret-mask set bundled by the
// built-in "hardened" profile. Exported so docs/tests can reference one source.
var HardenedProfileSecretPaths = []string{
	// Generic env / key material
	".env", "*.pem", "*.key", "*.p12", "id_rsa", "id_ed25519",
	// Credential / config files that commonly carry secrets in a repo
	".npmrc", ".netrc", ".git-credentials",
	"credentials.json", "service_account.json", "kubeconfig", "database.yml",
	// Terraform state/vars (frequently contain plaintext secrets)
	"*.tfvars", "*.tfstate",
	// Catch-all secret dir
	"secrets/**",
}

// synthesizeHardenedProfile returns the built-in "hardened" profile: a hardened
// preset for opening untrusted / freshly-cloned repositories. It bundles COI's
// strongest EXISTING controls (no new enforcement, no in-shell policing) so
// `coi shell --profile hardened` is a one-flag, maximally-safe way to inspect code
// you don't trust.
//
// Unlike the "default" profile this is a FIXED baseline, not a clone of the
// user's resolved config: it sets only the hardened overrides and lets every
// other field fall through. It can be overridden by a same-named disk profile.
//
// Limitation: profile merges are additive for slice fields, so it cannot
// *subtract* a globally-configured forward_env, nor force protections back on if
// the user globally set disable_protection=true. It hardens network egress,
// secrets, immutability, ephemerality, SSH-agent forwarding, and monitoring.
func synthesizeHardenedProfile() ProfileConfig {
	t, f := true, false
	return ProfileConfig{
		Source: "(built-in)",
		// Ephemeral: nothing from a risky session persists.
		Container: ContainerConfig{Persistent: &f},
		// No exfil path: internet-only, block LAN + cloud metadata endpoints.
		Network: &NetworkConfig{
			Mode:                    NetworkModeRestricted,
			BlockPrivateNetworks:    &t,
			BlockMetadataEndpoint:   &t,
			AllowLocalNetworkAccess: &f,
		},
		// Never forward the host SSH agent into an untrusted repo's container
		// (overrides a global forward_agent = true).
		SSH: &SSHConfig{ForwardAgent: &f},
		// Host-side immutability on; mask common secret files (union-merged with
		// any the user already configured). protected_paths defaults already cover
		// .claude/settings*.json, .git/hooks, .coi, etc.
		Security: &SecurityConfig{
			HostImmutable: &t,
			SecretPaths:   cloneSlice(HardenedProfileSecretPaths),
		},
		// Catch in-container exfil / reverse-shell attempts and auto-respond.
		Monitoring: &MonitoringConfig{
			Enabled:            &t,
			AutoPauseOnHigh:    &t,
			AutoKillOnCritical: &t,
			NFT:                NFTMonitoringConfig{Enabled: &t},
		},
	}
}

// GetConfigPaths returns the list of config file paths to check (in order).
// COI looks for configuration in two places:
//  1. ~/.coi/config.toml        (user, co-located with sessions/storage/logs)
//  2. ./.coi/config.toml        (current project)
//
// If COI_CONFIG environment variable is set, it is added as highest priority.
func GetConfigPaths() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	paths := []string{
		filepath.Join(homeDir, ".coi", "config.toml"), // User config
		filepath.Join(workDir, ".coi", "config.toml"), // Project config
	}

	// COI_CONFIG environment variable has highest priority
	if envConfig := os.Getenv("COI_CONFIG"); envConfig != "" {
		paths = append(paths, envConfig)
	}

	return paths
}

// GetProfileParentDirs returns directories to scan for the "profiles/"
// subdirectory. Profiles found under any of these locations are merged into
// a single namespace; the loader rejects duplicate profile names across
// locations so it is always unambiguous which profile is in use.
//
// Scanned locations:
//  1. ~/.coi                    (user home; co-located with sessions/storage/logs)
//  2. ./.coi                    (current project)
//  3. dirname($COI_CONFIG)      (if COI_CONFIG is set)
func GetProfileParentDirs() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	dirs := []string{
		filepath.Join(homeDir, ".coi"), // 1. User home
		filepath.Join(workDir, ".coi"), // 2. Project
	}

	// COI_CONFIG environment variable: scan its parent dir for profiles too
	if envConfig := os.Getenv("COI_CONFIG"); envConfig != "" {
		dirs = append(dirs, filepath.Dir(envConfig))
	}

	return dirs
}

// ptrBool returns a pointer to a bool value
func ptrBool(b bool) *bool {
	return &b
}

// BoolVal safely dereferences a *bool, returning false if nil
func BoolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

// SudoAllowed reports whether COI may invoke `sudo` for network operations.
// Defaults to true; set `[network] use_sudo = false` to opt out (no sudoers
// rule required, at the cost of restricted/allowlist enforcement).
func (n *NetworkConfig) SudoAllowed() bool {
	return n == nil || n.UseSudo == nil || *n.UseSudo
}

// IntVal dereferences a *int config pointer, returning 0 if nil.
func IntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ExpandPath expands ~ in paths to home directory
func ExpandPath(path string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path // Return path as-is if home dir cannot be determined
		}
		if len(path) == 1 {
			return homeDir
		}
		return filepath.Join(homeDir, path[1:])
	}
	return path
}

// Merge merges another config into this one (other takes precedence).
func (c *Config) Merge(other *Config) {
	mergeContainerInto(&c.Container, &other.Container)

	if other.Defaults.Profile != "" {
		c.Defaults.Profile = other.Defaults.Profile
	}
	if len(other.Defaults.ForwardEnv) > 0 {
		c.Defaults.ForwardEnv = MergeStringSliceUnique(c.Defaults.ForwardEnv, other.Defaults.ForwardEnv)
	}
	if len(other.Defaults.Environment) > 0 {
		if c.Defaults.Environment == nil {
			c.Defaults.Environment = make(map[string]string)
		}
		for k, v := range other.Defaults.Environment {
			c.Defaults.Environment[k] = v
		}
	}
	if len(other.Defaults.EnvCommands) > 0 {
		if c.Defaults.EnvCommands == nil {
			c.Defaults.EnvCommands = make(map[string]string)
		}
		for k, v := range other.Defaults.EnvCommands {
			c.Defaults.EnvCommands[k] = v
		}
	}
	if other.Defaults.EnvCommandTimeout != "" {
		c.Defaults.EnvCommandTimeout = other.Defaults.EnvCommandTimeout
	}
	if len(other.Mounts.Default) > 0 {
		c.Mounts.Default = append(c.Mounts.Default, other.Mounts.Default...)
	}
	if len(other.Sockets) > 0 {
		c.Sockets = append(c.Sockets, other.Sockets...)
	}
	mergePortsInto(&c.Ports, &other.Ports)
	if len(other.Credentials) > 0 {
		c.Credentials = append(c.Credentials, other.Credentials...)
	}

	mergePathsInto(&c.Paths, &other.Paths)
	mergeIncusInto(&c.Incus, &other.Incus)
	mergeNetworkInto(&c.Network, &other.Network)
	mergeShellInto(&c.Shell, &other.Shell)
	mergeToolInto(&c.Tool, &other.Tool)
	mergeLimits(&c.Limits, &other.Limits)
	mergeGitInto(&c.Git, &other.Git)
	mergeSSHInto(&c.SSH, &other.SSH)
	mergeSecurityInto(&c.Security, &other.Security)
	mergeMonitoring(&c.Monitoring, &other.Monitoring)
	mergeTimezoneInto(&c.Timezone, &other.Timezone)
	mergeDetectionInto(&c.Detection, &other.Detection)

	expandConfigPaths(c)
}

// mergeLimits merges limit configurations (other takes precedence)
func mergeLimits(base *LimitsConfig, other *LimitsConfig) {
	// Merge CPU limits
	if other.CPU.Count != "" {
		base.CPU.Count = other.CPU.Count
	}
	if other.CPU.Allowance != "" {
		base.CPU.Allowance = other.CPU.Allowance
	}
	if other.CPU.Priority != 0 {
		base.CPU.Priority = other.CPU.Priority
	}

	// Merge memory limits
	if other.Memory.Limit != "" {
		base.Memory.Limit = other.Memory.Limit
	}
	if other.Memory.Enforce != "" {
		base.Memory.Enforce = other.Memory.Enforce
	}
	if other.Memory.Swap != "" {
		base.Memory.Swap = other.Memory.Swap
	}

	// Merge disk limits
	if other.Disk.Read != "" {
		base.Disk.Read = other.Disk.Read
	}
	if other.Disk.Write != "" {
		base.Disk.Write = other.Disk.Write
	}
	if other.Disk.Max != "" {
		base.Disk.Max = other.Disk.Max
	}
	if other.Disk.Priority != 0 {
		base.Disk.Priority = other.Disk.Priority
	}
	if other.Disk.TmpfsSize != "" {
		base.Disk.TmpfsSize = other.Disk.TmpfsSize
	}

	// Merge runtime limits
	if other.Runtime.MaxDuration != "" {
		base.Runtime.MaxDuration = other.Runtime.MaxDuration
	}
	if other.Runtime.MaxProcesses != 0 {
		base.Runtime.MaxProcesses = other.Runtime.MaxProcesses
	}
	if other.Runtime.AutoStop != nil {
		base.Runtime.AutoStop = other.Runtime.AutoStop
	}
	if other.Runtime.StopGraceful != nil {
		base.Runtime.StopGraceful = other.Runtime.StopGraceful
	}
}

// mergeMonitoring merges monitoring configurations (other takes precedence)
func mergeMonitoring(base *MonitoringConfig, other *MonitoringConfig) {
	if other.Enabled != nil {
		base.Enabled = other.Enabled
	}
	if other.AutoPauseOnHigh != nil {
		base.AutoPauseOnHigh = other.AutoPauseOnHigh
	}
	if other.AutoKillOnCritical != nil {
		base.AutoKillOnCritical = other.AutoKillOnCritical
	}

	// Merge thresholds
	if other.PollIntervalSec != 0 {
		base.PollIntervalSec = other.PollIntervalSec
	}
	if other.FileReadThresholdMB != 0 {
		base.FileReadThresholdMB = other.FileReadThresholdMB
	}
	if other.FileReadRateMBPerSec != 0 {
		base.FileReadRateMBPerSec = other.FileReadRateMBPerSec
	}
	if other.ProcessCountThreshold != 0 {
		base.ProcessCountThreshold = other.ProcessCountThreshold
	}
	if other.ProcessSpawnRateThreshold != nil {
		base.ProcessSpawnRateThreshold = other.ProcessSpawnRateThreshold
	}
	if other.AuditLogRetentionDays != 0 {
		base.AuditLogRetentionDays = other.AuditLogRetentionDays
	}

	// Merge NFT monitoring
	if other.NFT.Enabled != nil {
		base.NFT.Enabled = other.NFT.Enabled
	}
	if other.NFT.RateLimitPerSecond != 0 {
		base.NFT.RateLimitPerSecond = other.NFT.RateLimitPerSecond
	}
	if other.NFT.DNSQueryThreshold != 0 {
		base.NFT.DNSQueryThreshold = other.NFT.DNSQueryThreshold
	}
	if other.NFT.LogDNSQueries != nil {
		base.NFT.LogDNSQueries = other.NFT.LogDNSQueries
	}
	if other.NFT.LimaHost != "" {
		base.NFT.LimaHost = other.NFT.LimaHost
	}
}

// MergeStringSliceUnique appends items from other to base, skipping duplicates
func MergeStringSliceUnique(base, other []string) []string {
	seen := make(map[string]bool, len(base))
	for _, s := range base {
		seen[s] = true
	}
	for _, s := range other {
		if !seen[s] {
			base = append(base, s)
			seen[s] = true
		}
	}
	return base
}

// GetProfile returns a profile by name, or nil if not found
func (c *Config) GetProfile(name string) *ProfileConfig {
	if profile, ok := c.Profiles[name]; ok {
		return &profile
	}
	return nil
}

// ApplyProfile applies a profile's settings to the defaults.
// Returns an error if the profile is not found or fails validation.
func (c *Config) ApplyProfile(name string) error {
	profile := c.GetProfile(name)
	if profile == nil {
		return fmt.Errorf("profile '%s' not found", name)
	}

	if err := profile.Validate(name); err != nil {
		return err
	}

	// Save the project-level alias before merging — profiles must not
	// override it because aliases are workspace-specific and a profile
	// used across multiple projects must not stamp them with a single name.
	projectAlias := c.Container.Alias
	mergeContainerInto(&c.Container, &profile.Container)
	if projectAlias != "" {
		c.Container.Alias = projectAlias
	}
	if profile.Context != "" {
		c.ProfileContextFile = profile.Context
	}

	// Apply profile environment if present
	if len(profile.Environment) > 0 {
		if c.Defaults.Environment == nil {
			c.Defaults.Environment = make(map[string]string)
		}
		for k, v := range profile.Environment {
			c.Defaults.Environment[k] = v
		}
	}
	if len(profile.EnvCommands) > 0 {
		if c.Defaults.EnvCommands == nil {
			c.Defaults.EnvCommands = make(map[string]string)
		}
		for k, v := range profile.EnvCommands {
			c.Defaults.EnvCommands[k] = v
		}
	}
	if len(profile.ForwardEnv) > 0 {
		c.Defaults.ForwardEnv = MergeStringSliceUnique(c.Defaults.ForwardEnv, profile.ForwardEnv)
	}
	if len(profile.Mounts) > 0 {
		c.Mounts.Default = append(c.Mounts.Default, profile.Mounts...)
	}
	if len(profile.Sockets) > 0 {
		c.Sockets = append(c.Sockets, profile.Sockets...)
	}
	if profile.Ports != nil {
		mergePortsInto(&c.Ports, profile.Ports)
	}
	if len(profile.Credentials) > 0 {
		c.Credentials = append(c.Credentials, profile.Credentials...)
	}

	// Apply struct sections
	if profile.Limits != nil {
		mergeLimits(&c.Limits, profile.Limits)
	}
	if profile.Monitoring != nil {
		mergeMonitoring(&c.Monitoring, profile.Monitoring)
	}
	if profile.Tool != nil {
		mergeToolInto(&c.Tool, profile.Tool)
	}
	if profile.Shell != nil {
		mergeShellInto(&c.Shell, profile.Shell)
	}
	if profile.Network != nil {
		mergeNetworkInto(&c.Network, profile.Network)
	}
	if profile.Paths != nil {
		mergePathsInto(&c.Paths, profile.Paths)
	}
	if profile.Incus != nil {
		mergeIncusInto(&c.Incus, profile.Incus)
	}
	if profile.Git != nil {
		mergeGitInto(&c.Git, profile.Git)
	}
	if profile.SSH != nil {
		mergeSSHInto(&c.SSH, profile.SSH)
	}
	if profile.Security != nil {
		mergeSecurityInto(&c.Security, profile.Security)
	}
	if profile.Timezone != nil {
		mergeTimezoneInto(&c.Timezone, profile.Timezone)
	}

	expandConfigPaths(c)
	return nil
}

// ReapplyProfileContainer re-merges ONLY the container section of the named
// profile into the config. Used after a workspace-config overlay so an
// explicitly requested profile keeps winning over the project's
// [container] settings (image, persistent, storage pool, build) — the
// overlay merges the project config on top of the profile applied earlier
// in PersistentPreRunE. Unlike a full ApplyProfile, this is idempotent:
// mergeContainerInto/mergeBuildInto are field-level overrides with no
// appends, so it is safe to call after a profile was already applied
// (a full re-apply would duplicate profile mounts and sockets).
// The project alias is preserved, mirroring ApplyProfile.
func (c *Config) ReapplyProfileContainer(name string) error {
	profile := c.GetProfile(name)
	if profile == nil {
		return fmt.Errorf("profile '%s' not found", name)
	}
	projectAlias := c.Container.Alias
	mergeContainerInto(&c.Container, &profile.Container)
	if projectAlias != "" {
		c.Container.Alias = projectAlias
	}
	return nil
}

// mergeContainerInto merges src into dst for the container-shape section.
func mergeContainerInto(dst *ContainerConfig, src *ContainerConfig) {
	if src == nil {
		return
	}
	if src.Image != "" {
		dst.Image = src.Image
	}
	if src.Persistent != nil {
		dst.Persistent = src.Persistent
	}
	if src.ShutdownTimeout != 0 {
		dst.ShutdownTimeout = src.ShutdownTimeout
	}
	if src.ReadyTimeout != 0 {
		dst.ReadyTimeout = src.ReadyTimeout
	}
	if src.StoragePool != "" {
		dst.StoragePool = src.StoragePool
	}
	if src.Alias != "" {
		dst.Alias = src.Alias
	}
	if src.StaleBaseCheck != "" {
		dst.StaleBaseCheck = src.StaleBaseCheck
	}
	if src.SessionName != "" {
		dst.SessionName = src.SessionName
	}
	mergeBuildInto(&dst.Build, &src.Build)
}

// mergePortsInto overlays src's ports section onto dst: a non-zero pool wins
// (pool provenance follows the winning source), and map entries with a name
// dst already has REPLACE the earlier entry while new names append — so
// re-applying a profile synthesized from the merged config (the "default"
// profile) is a no-op instead of doubling every entry, and a later scope can
// override an earlier entry's ports without duplicating its device name.
// When an UNTRUSTED pool overwrites a trusted one, the trusted value is
// remembered in PoolTrustedFallback so the trust gate can fall back to it
// instead of silently disabling the user's own pool.
func mergePortsInto(dst, src *PortsConfig) {
	if src == nil || !src.HasPorts() {
		return
	}
	if src.Pool > 0 {
		switch {
		case src.PoolUntrusted && dst.Pool > 0 && !dst.PoolUntrusted:
			dst.PoolTrustedFallback = dst.Pool
		case !src.PoolUntrusted:
			dst.PoolTrustedFallback = 0 // a trusted winner needs no fallback
		}
		dst.Pool = src.Pool
		dst.PoolUntrusted = src.PoolUntrusted
		dst.PoolSourcePath = src.PoolSourcePath
	}
	for _, e := range src.Map {
		replaced := false
		for i := range dst.Map {
			if dst.Map[i].Name == e.Name {
				dst.Map[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			dst.Map = append(dst.Map, e)
		}
	}
}

func clonePortsConfig(src *PortsConfig) *PortsConfig {
	if src == nil {
		return nil
	}
	out := *src
	out.Map = cloneSlice(src.Map)
	return &out
}

// maxInheritanceDepth is the maximum allowed inheritance chain depth
const maxInheritanceDepth = 10

// mergeProfiles merges a parent profile into a child profile.
// Maps merge (child wins), arrays replace if child defines them, scalars child wins if set.
// Struct pointers deep-merge field by field if child defines the section.
func mergeProfiles(parent, child ProfileConfig) ProfileConfig {
	result := child

	// Container section: deep field-by-field merge starting from parent
	mergedContainer := parent.Container
	mergeContainerInto(&mergedContainer, &child.Container)
	result.Container = mergedContainer

	// Scalars: child overrides parent if set
	if result.Context == "" {
		result.Context = parent.Context
	}

	// Maps: deep merge — parent keys preserved, child keys override
	if len(parent.Environment) > 0 {
		merged := make(map[string]string, len(parent.Environment)+len(result.Environment))
		for k, v := range parent.Environment {
			merged[k] = v
		}
		for k, v := range result.Environment {
			if v == "" {
				// Empty string clears inherited key
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		result.Environment = merged
	}

	// EnvCommands: deep merge with the same semantics as Environment — parent
	// keys preserved, child keys override, an empty child value clears the
	// inherited key. Without this a child profile silently loses the parent's
	// env_commands (they are trusted-scope, so this only affects trusted profiles).
	if len(parent.EnvCommands) > 0 {
		merged := make(map[string]string, len(parent.EnvCommands)+len(result.EnvCommands))
		for k, v := range parent.EnvCommands {
			merged[k] = v
		}
		for k, v := range result.EnvCommands {
			if v == "" {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		result.EnvCommands = merged
	}

	// Arrays: if child defines them, they fully replace parent's. If not, inherit.
	if result.Mounts == nil {
		result.Mounts = parent.Mounts
	}
	if result.Sockets == nil {
		result.Sockets = parent.Sockets
	}
	if result.Ports == nil {
		result.Ports = parent.Ports
	}
	if result.Credentials == nil {
		result.Credentials = parent.Credentials
	}
	if result.ForwardEnv == nil {
		result.ForwardEnv = parent.ForwardEnv
	}

	// Struct pointers: deep field-by-field merge if child defines section
	result.Limits = mergeStructPtr(parent.Limits, result.Limits, mergeLimitsInto)
	result.Tool = mergeStructPtr(parent.Tool, result.Tool, mergeToolInto)
	result.Network = mergeStructPtr(parent.Network, result.Network, mergeNetworkInto)
	result.Monitoring = mergeStructPtr(parent.Monitoring, result.Monitoring, mergeMonitoringInto)

	// New extended fields: scalar and struct pointer merges
	result.Paths = mergeStructPtr(parent.Paths, result.Paths, mergePathsInto)
	result.Incus = mergeStructPtr(parent.Incus, result.Incus, mergeIncusInto)
	result.Git = mergeStructPtr(parent.Git, result.Git, mergeGitInto)
	result.SSH = mergeStructPtr(parent.SSH, result.SSH, mergeSSHInto)
	result.Security = mergeStructPtr(parent.Security, result.Security, mergeSecurityInto)
	result.Timezone = mergeStructPtr(parent.Timezone, result.Timezone, mergeTimezoneInto)
	result.Shell = mergeStructPtr(parent.Shell, result.Shell, mergeShellInto)

	return result
}

// mergeStructPtr merges two struct pointers: if child is nil, inherit parent;
// if both set, deep-merge child fields into a copy of parent.
func mergeStructPtr[T any](parent, child *T, mergeFn func(dst *T, src *T)) *T {
	if child == nil {
		return parent
	}
	if parent == nil {
		return child
	}
	merged := *parent
	mergeFn(&merged, child)
	return &merged
}

func mergeLimitsInto(dst *LimitsConfig, src *LimitsConfig) {
	mergeLimits(dst, src)
}

func mergeToolInto(dst *ToolConfig, src *ToolConfig) {
	if src.Name != "" {
		dst.Name = src.Name
	}
	if src.Binary != "" {
		dst.Binary = src.Binary
	}
	if src.PermissionMode != "" {
		dst.PermissionMode = src.PermissionMode
	}
	if src.ContextFile != "" {
		dst.ContextFile = src.ContextFile
	}
	if src.AutoContext != nil {
		dst.AutoContext = src.AutoContext
	}
	if src.Claude.EffortLevel != "" {
		dst.Claude.EffortLevel = src.Claude.EffortLevel
	}
	if src.Claude.Model != "" {
		dst.Claude.Model = src.Claude.Model
	}
}

func mergeBuildInto(dst *BuildConfig, src *BuildConfig) {
	if src.Base != "" {
		dst.Base = src.Base
	}
	if src.Script != "" {
		dst.Script = src.Script
	}
	if src.Commands != nil {
		dst.Commands = src.Commands
	}
	if src.Compression != "" {
		dst.Compression = src.Compression
	}
	if len(src.Agents) > 0 {
		dst.Agents = src.Agents
	}
}

func mergeNetworkInto(dst *NetworkConfig, src *NetworkConfig) {
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	if src.BlockPrivateNetworks != nil {
		dst.BlockPrivateNetworks = src.BlockPrivateNetworks
	}
	if src.BlockMetadataEndpoint != nil {
		dst.BlockMetadataEndpoint = src.BlockMetadataEndpoint
	}
	if src.AllowLocalNetworkAccess != nil {
		dst.AllowLocalNetworkAccess = src.AllowLocalNetworkAccess
	}
	if src.UseSudo != nil {
		dst.UseSudo = src.UseSudo
	}
	if src.AllowedDomains != nil {
		dst.AllowedDomains = src.AllowedDomains
	}
	if src.RefreshIntervalMinutes != 0 {
		dst.RefreshIntervalMinutes = src.RefreshIntervalMinutes
	}
	if src.Hosts != nil {
		dst.Hosts = src.Hosts
	}
	if src.DNSServers != nil {
		dst.DNSServers = src.DNSServers
	}
	if src.AllowedPorts != nil {
		dst.AllowedPorts = src.AllowedPorts
	}
	if src.Logging.Path != "" {
		dst.Logging.Path = src.Logging.Path
	}
	if src.Logging.Enabled != nil {
		dst.Logging.Enabled = src.Logging.Enabled
	}
}

func mergeMonitoringInto(dst *MonitoringConfig, src *MonitoringConfig) {
	mergeMonitoring(dst, src)
}

func mergePathsInto(dst *PathsConfig, src *PathsConfig) {
	if src.SessionsDir != "" {
		dst.SessionsDir = src.SessionsDir
	}
	if src.StorageDir != "" {
		dst.StorageDir = src.StorageDir
	}
	if src.LogsDir != "" {
		dst.LogsDir = src.LogsDir
	}
	if src.PreserveWorkspacePath {
		dst.PreserveWorkspacePath = true
	}
}

func mergeIncusInto(dst *IncusConfig, src *IncusConfig) {
	if src.Project != "" {
		dst.Project = src.Project
	}
	if src.Group != "" {
		dst.Group = src.Group
	}
	if src.CodeUID != 0 {
		dst.CodeUID = src.CodeUID
	}
	if src.CodeUser != "" {
		dst.CodeUser = src.CodeUser
	}
	if src.DisableShift {
		dst.DisableShift = true
	}
}

func mergeGitInto(dst *GitConfig, src *GitConfig) {
	if src.WritableHooks != nil {
		dst.WritableHooks = src.WritableHooks
	}
	if src.Name != "" {
		dst.Name = src.Name
	}
	if src.Email != "" {
		dst.Email = src.Email
	}
	if src.SeedHostIdentity != nil {
		dst.SeedHostIdentity = src.SeedHostIdentity
	}
}

func mergeSSHInto(dst *SSHConfig, src *SSHConfig) {
	if src.ForwardAgent != nil {
		dst.ForwardAgent = src.ForwardAgent
	}
}

func mergeSecurityInto(dst *SecurityConfig, src *SecurityConfig) {
	if len(src.ProtectedPaths) > 0 {
		dst.ProtectedPaths = src.ProtectedPaths
	}
	if len(src.AdditionalProtectedPaths) > 0 {
		dst.AdditionalProtectedPaths = MergeStringSliceUnique(dst.AdditionalProtectedPaths, src.AdditionalProtectedPaths)
	}
	if src.DisableProtection {
		dst.DisableProtection = true
	}
	if src.HostImmutable != nil {
		dst.HostImmutable = src.HostImmutable
	}
	if len(src.WritablePaths) > 0 {
		dst.WritablePaths = MergeStringSliceUnique(dst.WritablePaths, src.WritablePaths)
	}
	if len(src.SecretPaths) > 0 {
		dst.SecretPaths = MergeStringSliceUnique(dst.SecretPaths, src.SecretPaths)
	}
}

func mergeTimezoneInto(dst *TimezoneConfig, src *TimezoneConfig) {
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	if src.Name != "" {
		dst.Name = src.Name
	}
}

func mergeShellInto(dst *ShellConfig, src *ShellConfig) {
	if src.UseTmux != nil {
		dst.UseTmux = src.UseTmux
	}
}

func mergeDetectionInto(dst *DetectionConfig, src *DetectionConfig) {
	if src.GTFOBinsSource != "" {
		dst.GTFOBinsSource = src.GTFOBinsSource
	}
	if src.GTFOBinsDir != "" {
		dst.GTFOBinsDir = src.GTFOBinsDir
	}
	if src.SigmaSource != "" {
		dst.SigmaSource = src.SigmaSource
	}
	if src.SigmaDir != "" {
		dst.SigmaDir = src.SigmaDir
	}
}

// ResolveProfileInheritance resolves all inheritance chains in loaded profiles.
// It flattens each profile so that after resolution, profiles are self-contained.
// Detects cycles and enforces a maximum inheritance depth.
func (c *Config) ResolveProfileInheritance() error {
	// First pass: validate chain lengths and detect cycles before any resolution.
	// This ensures max depth is checked against the full chain, not just recursion depth.
	for name := range c.Profiles {
		if c.Profiles[name].Inherits == "" {
			continue
		}
		visited := map[string]bool{name: true}
		current := name
		for {
			parent := c.Profiles[current].Inherits
			if parent == "" {
				break
			}
			if visited[parent] {
				return fmt.Errorf("profile inheritance cycle detected involving %q", parent)
			}
			if _, exists := c.Profiles[parent]; !exists {
				return fmt.Errorf("profile %q inherits from %q, but parent profile not found", current, parent)
			}
			if len(visited) > maxInheritanceDepth {
				return fmt.Errorf("profile inheritance chain exceeds maximum depth of %d", maxInheritanceDepth)
			}
			visited[parent] = true
			current = parent
		}
	}

	// Second pass: resolve inheritance by merging parent into child.
	resolved := make(map[string]bool, len(c.Profiles))

	var resolve func(name string) error
	resolve = func(name string) error {
		if resolved[name] {
			return nil
		}

		profile := c.Profiles[name]
		if profile.Inherits == "" {
			resolved[name] = true
			return nil
		}

		parentName := profile.Inherits

		// Resolve parent first
		if err := resolve(parentName); err != nil {
			return err
		}

		// Parent is now resolved — merge
		parent := c.Profiles[parentName]
		merged := mergeProfiles(parent, profile)

		// Preserve the original direct parent name for display/inspection
		// while still flattening the effective configuration values.
		merged.Inherits = parentName

		c.Profiles[name] = merged
		resolved[name] = true
		return nil
	}

	for name := range c.Profiles {
		if err := resolve(name); err != nil {
			return err
		}
	}

	return nil
}

// Validate checks that a profile's configuration is valid.
// Called when the profile is actually used (--profile flag), not at load time.
func (p *ProfileConfig) Validate(name string) error {
	// Validate context file exists if specified
	if p.Context != "" {
		if _, err := os.Stat(p.Context); err != nil {
			return fmt.Errorf("profile '%s': context file %q does not exist", name, p.Context)
		}
	}

	// Validate build script exists if specified
	if p.Container.Build.Script != "" {
		if _, err := os.Stat(p.Container.Build.Script); err != nil {
			return fmt.Errorf("profile '%s': build script %q does not exist", name, p.Container.Build.Script)
		}
	}

	// Validate mount entries are complete
	for i, m := range p.Mounts {
		if m.Host == "" {
			return fmt.Errorf("profile '%s': mount[%d] is missing 'host' path", name, i)
		}
		if m.Container == "" {
			return fmt.Errorf("profile '%s': mount[%d] is missing 'container' path", name, i)
		}
	}

	// Validate credential entries: exactly one of bundle or host+container.
	for i, cr := range p.Credentials {
		hasBundle := cr.Bundle != ""
		hasAdHoc := cr.Host != "" || cr.Container != ""
		if hasBundle && hasAdHoc {
			return fmt.Errorf("profile '%s': credentials[%d] must set either 'bundle' or 'host'+'container', not both", name, i)
		}
		if !hasBundle && !hasAdHoc {
			return fmt.Errorf("profile '%s': credentials[%d] must set either 'bundle' or 'host'+'container'", name, i)
		}
		if hasAdHoc {
			if cr.Host == "" {
				return fmt.Errorf("profile '%s': credentials[%d] is missing 'host' path", name, i)
			}
			if cr.Container == "" {
				return fmt.Errorf("profile '%s': credentials[%d] is missing 'container' path", name, i)
			}
		}
		if cr.Mode != "" {
			if _, err := strconv.ParseUint(cr.Mode, 8, 32); err != nil {
				return fmt.Errorf("profile '%s': credentials[%d] has invalid 'mode' %q (must be an octal file mode, e.g. \"0600\"): %w", name, i, cr.Mode, err)
			}
		}
	}

	// Validate port entries: name required and unique, container port in
	// range, optional host pin in range, listen a bare address if set.
	seenPortNames := map[string]bool{}
	var portEntries []PortEntry
	if p.Ports != nil {
		if p.Ports.Pool < 0 || p.Ports.Pool > 10 {
			return fmt.Errorf("profile '%s': [ports] pool must be 0-10, got %d", name, p.Ports.Pool)
		}
		portEntries = p.Ports.Map
	}
	for i, pe := range portEntries {
		if pe.Name == "" {
			return fmt.Errorf("profile '%s': ports[%d] is missing 'name'", name, i)
		}
		if seenPortNames[pe.Name] {
			return fmt.Errorf("profile '%s': ports[%d] duplicates name %q", name, i, pe.Name)
		}
		seenPortNames[pe.Name] = true
		if pe.Container < 1 || pe.Container > 65535 {
			return fmt.Errorf("profile '%s': ports[%d] (%s) 'container' must be a TCP port (1-65535), got %d", name, i, pe.Name, pe.Container)
		}
		if pe.Host != 0 && (pe.Host < 1 || pe.Host > 65535) {
			return fmt.Errorf("profile '%s': ports[%d] (%s) 'host' must be a TCP port (1-65535) or omitted for auto-allocation, got %d", name, i, pe.Name, pe.Host)
		}
		if pe.Listen != "" && net.ParseIP(pe.Listen) == nil {
			return fmt.Errorf("profile '%s': ports[%d] (%s) 'listen' must be an IP address (e.g. \"127.0.0.1\"), got %q", name, i, pe.Name, pe.Listen)
		}
	}

	// Validate network mode if set
	if p.Network != nil && p.Network.Mode != "" {
		switch p.Network.Mode {
		case "open", "restricted", "allowlist":
			// valid
		default:
			return fmt.Errorf("profile '%s': invalid network mode %q (must be open, restricted, or allowlist)", name, p.Network.Mode)
		}
	}

	// Validate [[network.hosts]] entries if set
	if p.Network != nil && len(p.Network.Hosts) > 0 {
		if err := ValidateNetworkHosts(p.Network.Hosts); err != nil {
			return fmt.Errorf("profile '%s': %w", name, err)
		}
	}

	return nil
}
