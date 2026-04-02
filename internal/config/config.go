package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the complete configuration
type Config struct {
	Defaults           DefaultsConfig           `toml:"defaults"`
	Paths              PathsConfig              `toml:"paths"`
	Incus              IncusConfig              `toml:"incus"`
	Network            NetworkConfig            `toml:"network"`
	Tool               ToolConfig               `toml:"tool"`
	Mounts             MountsConfig             `toml:"mounts"`
	Limits             LimitsConfig             `toml:"limits"`
	Git                GitConfig                `toml:"git"`
	SSH                SSHConfig                `toml:"ssh"`
	Security           SecurityConfig           `toml:"security"`
	Monitoring         MonitoringConfig         `toml:"monitoring"`
	Timezone           TimezoneConfig           `toml:"timezone"`
	Build              BuildConfig              `toml:"build"`
	Profiles           map[string]ProfileConfig `toml:"-"` // Populated by loadProfileDirectories, not from TOML
	ProfileContextFile string                   `toml:"-"` // Set by ApplyProfile, read by session setup
}

// BuildConfig defines how to build the project's custom image
type BuildConfig struct {
	Base     string   `toml:"base"`     // Base image (default: "coi")
	Script   string   `toml:"script"`   // Path to build script (relative to config file or absolute)
	Commands []string `toml:"commands"` // Inline build commands (alternative to script)
}

// HasBuildConfig returns true if a build configuration is defined (script or commands)
func (b *BuildConfig) HasBuildConfig() bool {
	return b.Script != "" || len(b.Commands) > 0
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
}

// GetEffectiveProtectedPaths returns the combined list of protected paths
func (s *SecurityConfig) GetEffectiveProtectedPaths() []string {
	if s.DisableProtection {
		return nil
	}
	paths := make([]string, 0, len(s.ProtectedPaths)+len(s.AdditionalProtectedPaths))
	paths = append(paths, s.ProtectedPaths...)
	paths = append(paths, s.AdditionalProtectedPaths...)
	return paths
}

// DefaultProtectedPaths returns the default list of paths to protect
func DefaultProtectedPaths() []string {
	return []string{
		".git/hooks",  // Git hooks - execute on git operations
		".git/config", // Git config - can set core.hooksPath to bypass hooks protection
		".husky",      // Husky - popular git hooks manager
		".vscode",     // VS Code - tasks.json can auto-execute, settings.json can inject shell args
	}
}

// DefaultsConfig contains default settings
type DefaultsConfig struct {
	Image       string            `toml:"image"`
	Persistent  *bool             `toml:"persistent"`
	Model       string            `toml:"model"`
	ForwardEnv  []string          `toml:"forward_env"`
	Environment map[string]string `toml:"environment"`
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
	DisableShift bool   `toml:"disable_shift"` // Disable UID shifting (for Colima/Lima environments)
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
	Mode                    NetworkMode          `toml:"mode"`
	BlockPrivateNetworks    *bool                `toml:"block_private_networks"`
	BlockMetadataEndpoint   *bool                `toml:"block_metadata_endpoint"`
	AllowedDomains          []string             `toml:"allowed_domains"`
	RefreshIntervalMinutes  int                  `toml:"refresh_interval_minutes"`
	AllowLocalNetworkAccess *bool                `toml:"allow_local_network_access"` // Allow established connections from entire local network (not just gateway)
	Logging                 NetworkLoggingConfig `toml:"logging"`
}

// NetworkLoggingConfig contains network logging settings
type NetworkLoggingConfig struct {
	Enabled *bool  `toml:"enabled"`
	Path    string `toml:"path"`
}

// ProfileConfig represents a named profile
type ProfileConfig struct {
	Image       string            `toml:"image"`
	Context     string            `toml:"context"` // Path to context .md file (resolved relative to profile dir)
	Environment map[string]string `toml:"environment"`
	Persistent  *bool             `toml:"persistent"`
	Limits      *LimitsConfig     `toml:"limits"`
	Tool        *ToolConfig       `toml:"tool"`
	Build       *BuildConfig      `toml:"build"`
	Mounts      []MountEntry      `toml:"mounts"`
	Network     *NetworkConfig    `toml:"network"`
	ForwardEnv  []string          `toml:"forward_env"`
	Source      string            `toml:"-"` // Where this profile was loaded from (not serialized)
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
	EffortLevel string `toml:"effort_level"` // Effort level: "low", "medium", "high" (default: "medium")
}

// MountEntry represents a single directory mount configuration
type MountEntry struct {
	Host      string `toml:"host"`      // Host path (supports ~ expansion)
	Container string `toml:"container"` // Container path (must be absolute)
}

// MountsConfig contains mount-related configuration
type MountsConfig struct {
	Default []MountEntry `toml:"default"` // Default mounts for all sessions
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
	Enabled               *bool               `toml:"enabled"`                   // Enable background monitoring
	AutoPauseOnHigh       *bool               `toml:"auto_pause_on_high"`        // Pause container on high-severity threats
	AutoKillOnCritical    *bool               `toml:"auto_kill_on_critical"`     // Kill container on critical threats
	PollIntervalSec       int                 `toml:"poll_interval_sec"`         // How often to collect stats
	FileReadThresholdMB   float64             `toml:"file_read_threshold_mb"`    // MB read in poll interval before alert
	FileReadRateMBPerSec  float64             `toml:"file_read_rate_mb_per_sec"` // MB/sec sustained rate before alert
	AuditLogRetentionDays int                 `toml:"audit_log_retention_days"`  // How long to keep audit logs
	NFT                   NFTMonitoringConfig `toml:"nft"`                       // nftables network monitoring
}

// GetDefaultConfig returns the default configuration
func GetDefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp" // Fallback if home dir cannot be determined
	}
	baseDir := filepath.Join(homeDir, ".coi")

	return &Config{
		Defaults: DefaultsConfig{
			Image:      "coi",
			Persistent: ptrBool(false),
			Model:      "claude-sonnet-4-5",
		},
		Paths: PathsConfig{
			SessionsDir: filepath.Join(baseDir, "sessions"),
			StorageDir:  filepath.Join(baseDir, "storage"),
			LogsDir:     filepath.Join(baseDir, "logs"),
		},
		Incus: IncusConfig{
			Project:  "default",
			Group:    "incus-admin",
			CodeUID:  1000,
			CodeUser: "code",
		},
		Network: NetworkConfig{
			Mode:                  NetworkModeRestricted,
			BlockPrivateNetworks:  ptrBool(true),
			BlockMetadataEndpoint: ptrBool(true),
			AllowedDomains: []string{
				// Default allowlist for allowlist mode (--network=allowlist)
				// Note: Gateway IP is auto-detected and added automatically
				"8.8.8.8",             // Google DNS (REQUIRED for DNS resolution)
				"1.1.1.1",             // Cloudflare DNS (REQUIRED for DNS resolution)
				"registry.npmjs.org",  // npm package registry
				"npm.pkg.github.com",  // GitHub packages
				"api.anthropic.com",   // Claude API
				"platform.claude.com", // Claude Platform (OAuth, Console)
			},
			AllowLocalNetworkAccess: ptrBool(false),
			RefreshIntervalMinutes:  30,
			Logging: NetworkLoggingConfig{
				Enabled: ptrBool(true),
				Path:    filepath.Join(baseDir, "logs", "network.log"),
			},
		},
		Tool: ToolConfig{
			Name:        "claude",
			Binary:      "", // Empty means use tool's default binary name
			AutoContext: ptrBool(true),
		},
		Mounts: MountsConfig{
			Default: []MountEntry{},
		},
		Git: GitConfig{
			WritableHooks: ptrBool(false),
		},
		SSH: SSHConfig{
			ForwardAgent: ptrBool(false),
		},
		Security: SecurityConfig{
			ProtectedPaths:           DefaultProtectedPaths(),
			AdditionalProtectedPaths: []string{},
			DisableProtection:        false,
		},
		Limits: LimitsConfig{
			CPU: CPULimits{
				Count:     "",
				Allowance: "",
				Priority:  0,
			},
			Memory: MemoryLimits{
				Limit:   "",
				Enforce: "soft",
				Swap:    "true",
			},
			Disk: DiskLimits{
				Read:      "",
				Write:     "",
				Max:       "",
				Priority:  0,
				TmpfsSize: "", // Default: use container root disk. Set "4GiB" etc. for RAM-backed tmpfs.
			},
			Runtime: RuntimeLimits{
				MaxDuration:  "",
				MaxProcesses: 0,
				AutoStop:     ptrBool(true),
				StopGraceful: ptrBool(true),
			},
		},
		Monitoring: MonitoringConfig{
			Enabled:               ptrBool(false),
			AutoPauseOnHigh:       ptrBool(true),
			AutoKillOnCritical:    ptrBool(true),
			PollIntervalSec:       2,
			FileReadThresholdMB:   50.0,
			FileReadRateMBPerSec:  10.0,
			AuditLogRetentionDays: 30,
			NFT: NFTMonitoringConfig{
				Enabled:            ptrBool(false),
				RateLimitPerSecond: 100,
				DNSQueryThreshold:  100,
				LogDNSQueries:      ptrBool(true),
				LimaHost:           "", // Empty for native Linux
			},
		},
		Timezone: TimezoneConfig{
			Mode: "host",
		},
		Profiles: make(map[string]ProfileConfig),
	}
}

// GetConfigPaths returns the list of config file paths to check (in order)
// If COI_CONFIG environment variable is set, it is added as highest priority
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
		"/etc/coi/config.toml",                            // System config
		filepath.Join(homeDir, ".config/coi/config.toml"), // User config
		filepath.Join(workDir, ".coi", "config.toml"),     // Project config (.coi/config.toml)
	}

	// COI_CONFIG environment variable has highest priority
	if envConfig := os.Getenv("COI_CONFIG"); envConfig != "" {
		paths = append(paths, envConfig)
	}

	return paths
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

// Merge merges another config into this one (other takes precedence)
func (c *Config) Merge(other *Config) {
	// Merge defaults
	if other.Defaults.Image != "" {
		c.Defaults.Image = other.Defaults.Image
	}
	if other.Defaults.Model != "" {
		c.Defaults.Model = other.Defaults.Model
	}
	if other.Defaults.Persistent != nil {
		c.Defaults.Persistent = other.Defaults.Persistent
	}

	// Merge forward_env (append without duplicates)
	if len(other.Defaults.ForwardEnv) > 0 {
		c.Defaults.ForwardEnv = MergeStringSliceUnique(c.Defaults.ForwardEnv, other.Defaults.ForwardEnv)
	}

	// Merge environment (other takes precedence for overlapping keys)
	if len(other.Defaults.Environment) > 0 {
		if c.Defaults.Environment == nil {
			c.Defaults.Environment = make(map[string]string)
		}
		for k, v := range other.Defaults.Environment {
			c.Defaults.Environment[k] = v
		}
	}

	// Merge paths
	if other.Paths.SessionsDir != "" {
		c.Paths.SessionsDir = ExpandPath(other.Paths.SessionsDir)
	}
	if other.Paths.StorageDir != "" {
		c.Paths.StorageDir = ExpandPath(other.Paths.StorageDir)
	}
	if other.Paths.LogsDir != "" {
		c.Paths.LogsDir = ExpandPath(other.Paths.LogsDir)
	}
	if other.Paths.PreserveWorkspacePath {
		c.Paths.PreserveWorkspacePath = true
	}

	// Merge Incus settings
	if other.Incus.Project != "" {
		c.Incus.Project = other.Incus.Project
	}
	if other.Incus.Group != "" {
		c.Incus.Group = other.Incus.Group
	}
	if other.Incus.CodeUID != 0 {
		c.Incus.CodeUID = other.Incus.CodeUID
	}
	if other.Incus.CodeUser != "" {
		c.Incus.CodeUser = other.Incus.CodeUser
	}

	// Merge Network settings
	if other.Network.Mode != "" {
		c.Network.Mode = other.Network.Mode
	}
	if other.Network.BlockPrivateNetworks != nil {
		c.Network.BlockPrivateNetworks = other.Network.BlockPrivateNetworks
	}
	if other.Network.BlockMetadataEndpoint != nil {
		c.Network.BlockMetadataEndpoint = other.Network.BlockMetadataEndpoint
	}
	if other.Network.AllowLocalNetworkAccess != nil {
		c.Network.AllowLocalNetworkAccess = other.Network.AllowLocalNetworkAccess
	}

	// Merge allowed domains (replace entirely if set)
	if len(other.Network.AllowedDomains) > 0 {
		c.Network.AllowedDomains = other.Network.AllowedDomains
	}

	// Merge refresh interval
	if other.Network.RefreshIntervalMinutes != 0 {
		c.Network.RefreshIntervalMinutes = other.Network.RefreshIntervalMinutes
	}

	if other.Network.Logging.Path != "" {
		c.Network.Logging.Path = ExpandPath(other.Network.Logging.Path)
	}
	if other.Network.Logging.Enabled != nil {
		c.Network.Logging.Enabled = other.Network.Logging.Enabled
	}

	// Merge Tool settings
	if other.Tool.Name != "" {
		c.Tool.Name = other.Tool.Name
	}
	if other.Tool.Binary != "" {
		c.Tool.Binary = other.Tool.Binary
	}
	// Merge permission mode
	if other.Tool.PermissionMode != "" {
		c.Tool.PermissionMode = other.Tool.PermissionMode
	}
	// Merge context file path
	if other.Tool.ContextFile != "" {
		c.Tool.ContextFile = ExpandPath(other.Tool.ContextFile)
	}
	// Merge auto-context setting
	if other.Tool.AutoContext != nil {
		c.Tool.AutoContext = other.Tool.AutoContext
	}
	// Merge Claude-specific settings
	if other.Tool.Claude.EffortLevel != "" {
		c.Tool.Claude.EffortLevel = other.Tool.Claude.EffortLevel
	}
	// For DisableShift, if the other config sets it to true, use it
	if other.Incus.DisableShift {
		c.Incus.DisableShift = true
	}

	// Merge mounts - append from other config
	if len(other.Mounts.Default) > 0 {
		c.Mounts.Default = append(c.Mounts.Default, other.Mounts.Default...)
	}

	// Merge limits
	mergeLimits(&c.Limits, &other.Limits)

	// Merge git settings
	// Only override if explicitly set in the other config (nil means not set)
	if other.Git.WritableHooks != nil {
		c.Git.WritableHooks = other.Git.WritableHooks
	}

	// Merge SSH settings
	if other.SSH.ForwardAgent != nil {
		c.SSH.ForwardAgent = other.SSH.ForwardAgent
	}

	// Merge security settings
	if len(other.Security.ProtectedPaths) > 0 {
		c.Security.ProtectedPaths = other.Security.ProtectedPaths
	}
	if len(other.Security.AdditionalProtectedPaths) > 0 {
		c.Security.AdditionalProtectedPaths = append(c.Security.AdditionalProtectedPaths, other.Security.AdditionalProtectedPaths...)
	}
	if other.Security.DisableProtection {
		c.Security.DisableProtection = true
	}

	// Merge monitoring
	mergeMonitoring(&c.Monitoring, &other.Monitoring)

	// Merge timezone
	if other.Timezone.Mode != "" {
		c.Timezone.Mode = other.Timezone.Mode
	}
	if other.Timezone.Name != "" {
		c.Timezone.Name = other.Timezone.Name
	}

	// Merge build config
	if other.Build.Base != "" {
		c.Build.Base = other.Build.Base
	}
	if other.Build.Script != "" {
		c.Build.Script = other.Build.Script
	}
	if len(other.Build.Commands) > 0 {
		c.Build.Commands = other.Build.Commands
	}
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

	if profile.Image != "" {
		c.Defaults.Image = profile.Image
	}
	if profile.Persistent != nil {
		c.Defaults.Persistent = profile.Persistent
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

	// Apply profile limits if present
	if profile.Limits != nil {
		mergeLimits(&c.Limits, profile.Limits)
	}

	// Apply profile tool settings if present
	if profile.Tool != nil {
		if profile.Tool.Name != "" {
			c.Tool.Name = profile.Tool.Name
		}
		if profile.Tool.Binary != "" {
			c.Tool.Binary = profile.Tool.Binary
		}
		if profile.Tool.PermissionMode != "" {
			c.Tool.PermissionMode = profile.Tool.PermissionMode
		}
		if profile.Tool.ContextFile != "" {
			c.Tool.ContextFile = ExpandPath(profile.Tool.ContextFile)
		}
		if profile.Tool.AutoContext != nil {
			c.Tool.AutoContext = profile.Tool.AutoContext
		}
		if profile.Tool.Claude.EffortLevel != "" {
			c.Tool.Claude.EffortLevel = profile.Tool.Claude.EffortLevel
		}
	}

	// Apply profile build config if present
	if profile.Build != nil {
		if profile.Build.Base != "" {
			c.Build.Base = profile.Build.Base
		}
		if profile.Build.Script != "" {
			c.Build.Script = profile.Build.Script
		}
		if len(profile.Build.Commands) > 0 {
			c.Build.Commands = profile.Build.Commands
		}
	}

	// Apply profile mounts if present (append to defaults)
	if len(profile.Mounts) > 0 {
		c.Mounts.Default = append(c.Mounts.Default, profile.Mounts...)
	}

	// Apply profile network settings if present
	if profile.Network != nil {
		if profile.Network.Mode != "" {
			c.Network.Mode = profile.Network.Mode
		}
		if profile.Network.BlockPrivateNetworks != nil {
			c.Network.BlockPrivateNetworks = profile.Network.BlockPrivateNetworks
		}
		if profile.Network.BlockMetadataEndpoint != nil {
			c.Network.BlockMetadataEndpoint = profile.Network.BlockMetadataEndpoint
		}
		if profile.Network.AllowLocalNetworkAccess != nil {
			c.Network.AllowLocalNetworkAccess = profile.Network.AllowLocalNetworkAccess
		}
		if len(profile.Network.AllowedDomains) > 0 {
			c.Network.AllowedDomains = profile.Network.AllowedDomains
		}
		if profile.Network.RefreshIntervalMinutes != 0 {
			c.Network.RefreshIntervalMinutes = profile.Network.RefreshIntervalMinutes
		}
		if profile.Network.Logging.Path != "" {
			c.Network.Logging.Path = ExpandPath(profile.Network.Logging.Path)
		}
		if profile.Network.Logging.Enabled != nil {
			c.Network.Logging.Enabled = profile.Network.Logging.Enabled
		}
	}

	// Apply profile forward_env if present
	if len(profile.ForwardEnv) > 0 {
		c.Defaults.ForwardEnv = MergeStringSliceUnique(c.Defaults.ForwardEnv, profile.ForwardEnv)
	}

	// Apply profile context file if present
	if profile.Context != "" {
		c.ProfileContextFile = profile.Context
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
	if p.Build != nil && p.Build.Script != "" {
		if _, err := os.Stat(p.Build.Script); err != nil {
			return fmt.Errorf("profile '%s': build script %q does not exist", name, p.Build.Script)
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

	// Validate network mode if set
	if p.Network != nil && p.Network.Mode != "" {
		switch p.Network.Mode {
		case "open", "restricted", "allowlist":
			// valid
		default:
			return fmt.Errorf("profile '%s': invalid network mode %q (must be open, restricted, or allowlist)", name, p.Network.Mode)
		}
	}

	return nil
}
