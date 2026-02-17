package config

import (
	"os"
	"path/filepath"
)

// Config represents the complete configuration
type Config struct {
	Defaults   DefaultsConfig           `toml:"defaults"`
	Paths      PathsConfig              `toml:"paths"`
	Incus      IncusConfig              `toml:"incus"`
	Network    NetworkConfig            `toml:"network"`
	Tool       ToolConfig               `toml:"tool"`
	Mounts     MountsConfig             `toml:"mounts"`
	Limits     LimitsConfig             `toml:"limits"`
	Git        GitConfig                `toml:"git"`
	Security   SecurityConfig           `toml:"security"`
	Monitoring MonitoringConfig         `toml:"monitoring"`
	Profiles   map[string]ProfileConfig `toml:"profiles"`
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
	Image      string `toml:"image"`
	Persistent bool   `toml:"persistent"`
	Model      string `toml:"model"`
}

// PathsConfig contains path settings
type PathsConfig struct {
	SessionsDir string `toml:"sessions_dir"`
	StorageDir  string `toml:"storage_dir"`
	LogsDir     string `toml:"logs_dir"`
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
	BlockPrivateNetworks    bool                 `toml:"block_private_networks"`
	BlockMetadataEndpoint   bool                 `toml:"block_metadata_endpoint"`
	AllowedDomains          []string             `toml:"allowed_domains"`
	RefreshIntervalMinutes  int                  `toml:"refresh_interval_minutes"`
	AllowLocalNetworkAccess bool                 `toml:"allow_local_network_access"` // Allow established connections from entire local network (not just gateway)
	Logging                 NetworkLoggingConfig `toml:"logging"`
}

// NetworkLoggingConfig contains network logging settings
type NetworkLoggingConfig struct {
	Enabled bool   `toml:"enabled"`
	Path    string `toml:"path"`
}

// ProfileConfig represents a named profile
type ProfileConfig struct {
	Image       string            `toml:"image"`
	Environment map[string]string `toml:"environment"`
	Persistent  bool              `toml:"persistent"`
	Limits      *LimitsConfig     `toml:"limits"`
}

// ToolConfig represents AI coding tool configuration
type ToolConfig struct {
	Name   string `toml:"name"`   // Tool name: "claude", "aider", "cursor", etc.
	Binary string `toml:"binary"` // Binary name to execute (if empty, uses tool name)
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
	AutoStop     bool   `toml:"auto_stop"`     // auto-stop when limit reached
	StopGraceful bool   `toml:"stop_graceful"` // graceful vs force stop
}

// NFTMonitoringConfig contains nftables-based network monitoring settings
type NFTMonitoringConfig struct {
	Enabled            bool   `toml:"enabled"`               // Enable nftables monitoring
	RateLimitPerSecond int    `toml:"rate_limit_per_second"` // Limit log volume (default 100)
	DNSQueryThreshold  int    `toml:"dns_query_threshold"`   // Alert if >N queries/min (default 100)
	LogDNSQueries      bool   `toml:"log_dns_queries"`       // Separate DNS logging (default true)
	LimaHost           string `toml:"lima_host"`             // Lima host for macOS (e.g., "lima-default")
}

// MonitoringConfig contains security monitoring settings
type MonitoringConfig struct {
	Enabled               bool                `toml:"enabled"`                   // Enable background monitoring
	AutoPauseOnHigh       bool                `toml:"auto_pause_on_high"`        // Pause container on high-severity threats
	AutoKillOnCritical    bool                `toml:"auto_kill_on_critical"`     // Kill container on critical threats
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
			Persistent: false,
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
			BlockPrivateNetworks:  true,
			BlockMetadataEndpoint: true,
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
			RefreshIntervalMinutes: 30,
			Logging: NetworkLoggingConfig{
				Enabled: true,
				Path:    filepath.Join(baseDir, "logs", "network.log"),
			},
		},
		Tool: ToolConfig{
			Name:   "claude",
			Binary: "", // Empty means use tool's default binary name
		},
		Mounts: MountsConfig{
			Default: []MountEntry{},
		},
		Git: GitConfig{
			WritableHooks: ptrBool(false),
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
				TmpfsSize: "2GiB", // Default /tmp size (prevent space exhaustion)
			},
			Runtime: RuntimeLimits{
				MaxDuration:  "",
				MaxProcesses: 0,
				AutoStop:     true,
				StopGraceful: true,
			},
		},
		Monitoring: MonitoringConfig{
			Enabled:               false,
			AutoPauseOnHigh:       true,
			AutoKillOnCritical:    true,
			PollIntervalSec:       2,
			FileReadThresholdMB:   50.0,
			FileReadRateMBPerSec:  10.0,
			AuditLogRetentionDays: 30,
			NFT: NFTMonitoringConfig{
				Enabled:            false,
				RateLimitPerSecond: 100,
				DNSQueryThreshold:  100,
				LogDNSQueries:      true,
				LimaHost:           "", // Empty for native Linux
			},
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
		filepath.Join(workDir, ".coi.toml"),               // Project config
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
	// For booleans, we need a way to distinguish "not set" from "false"
	// In TOML, if a field is not present, it will be false (zero value)
	// This is a limitation - we'll just override if file exists
	c.Defaults.Persistent = other.Defaults.Persistent

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
	// For booleans, we merge if they appear to be explicitly set
	// This is imperfect in TOML but works for most cases
	c.Network.BlockPrivateNetworks = other.Network.BlockPrivateNetworks
	c.Network.BlockMetadataEndpoint = other.Network.BlockMetadataEndpoint
	c.Network.AllowLocalNetworkAccess = other.Network.AllowLocalNetworkAccess

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
	c.Network.Logging.Enabled = other.Network.Logging.Enabled

	// Merge Tool settings
	if other.Tool.Name != "" {
		c.Tool.Name = other.Tool.Name
	}
	if other.Tool.Binary != "" {
		c.Tool.Binary = other.Tool.Binary
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

	// Merge profiles
	for name, profile := range other.Profiles {
		c.Profiles[name] = profile
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

	// Merge runtime limits
	if other.Runtime.MaxDuration != "" {
		base.Runtime.MaxDuration = other.Runtime.MaxDuration
	}
	if other.Runtime.MaxProcesses != 0 {
		base.Runtime.MaxProcesses = other.Runtime.MaxProcesses
	}
	// For booleans, we take the other value if it differs from default
	// This is imperfect but works for most cases
	base.Runtime.AutoStop = other.Runtime.AutoStop
	base.Runtime.StopGraceful = other.Runtime.StopGraceful
}

// mergeMonitoring merges monitoring configurations (other takes precedence)
func mergeMonitoring(base *MonitoringConfig, other *MonitoringConfig) {
	// For booleans, we take the other value
	base.Enabled = other.Enabled
	base.AutoPauseOnHigh = other.AutoPauseOnHigh
	base.AutoKillOnCritical = other.AutoKillOnCritical

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
	base.NFT.Enabled = other.NFT.Enabled
	if other.NFT.RateLimitPerSecond != 0 {
		base.NFT.RateLimitPerSecond = other.NFT.RateLimitPerSecond
	}
	if other.NFT.DNSQueryThreshold != 0 {
		base.NFT.DNSQueryThreshold = other.NFT.DNSQueryThreshold
	}
	base.NFT.LogDNSQueries = other.NFT.LogDNSQueries
	if other.NFT.LimaHost != "" {
		base.NFT.LimaHost = other.NFT.LimaHost
	}
}

// GetProfile returns a profile by name, or nil if not found
func (c *Config) GetProfile(name string) *ProfileConfig {
	if profile, ok := c.Profiles[name]; ok {
		return &profile
	}
	return nil
}

// ApplyProfile applies a profile's settings to the defaults
func (c *Config) ApplyProfile(name string) bool {
	profile := c.GetProfile(name)
	if profile == nil {
		return false
	}

	if profile.Image != "" {
		c.Defaults.Image = profile.Image
	}
	c.Defaults.Persistent = profile.Persistent

	// Apply profile limits if present
	if profile.Limits != nil {
		mergeLimits(&c.Limits, profile.Limits)
	}

	return true
}
