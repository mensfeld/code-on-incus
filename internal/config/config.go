package config

import (
	"os"
	"path/filepath"
)

// Config represents the complete configuration
type Config struct {
	Defaults DefaultsConfig           `toml:"defaults"`
	Paths    PathsConfig              `toml:"paths"`
	Incus    IncusConfig              `toml:"incus"`
	Network  NetworkConfig            `toml:"network"`
	Tool     ToolConfig               `toml:"tool"`
	Mounts   MountsConfig             `toml:"mounts"`
	Limits   LimitsConfig             `toml:"limits"`
	Profiles map[string]ProfileConfig `toml:"profiles"`
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
	Read     string `toml:"read"`     // "10MiB/s", "1000iops", "" (unlimited)
	Write    string `toml:"write"`    // "5MiB/s", "1000iops", "" (unlimited)
	Max      string `toml:"max"`      // combined read+write limit
	Priority int    `toml:"priority"` // 0-10
}

// RuntimeLimits contains time-based and process limits
type RuntimeLimits struct {
	MaxDuration  string `toml:"max_duration"`  // "2h", "30m", "1h30m", "" (unlimited)
	MaxProcesses int    `toml:"max_processes"` // 0 = unlimited
	AutoStop     bool   `toml:"auto_stop"`     // auto-stop when limit reached
	StopGraceful bool   `toml:"stop_graceful"` // graceful vs force stop
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
				Read:     "",
				Write:    "",
				Max:      "",
				Priority: 0,
			},
			Runtime: RuntimeLimits{
				MaxDuration:  "",
				MaxProcesses: 0,
				AutoStop:     true,
				StopGraceful: true,
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
