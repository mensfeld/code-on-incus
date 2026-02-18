package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Load loads configuration from all available sources
// Hierarchy (lowest to highest precedence):
// 1. Built-in defaults
// 2. System config (/etc/coi/config.toml)
// 3. User config (~/.config/coi/config.toml)
// 4. Project config (./.coi.toml)
// 5. Environment variables (CLAUDE_ON_INCUS_* or COI_*)
func Load() (*Config, error) {
	// Start with defaults
	cfg := GetDefaultConfig()

	// Load from config files (in order)
	paths := GetConfigPaths()
	for _, path := range paths {
		if err := loadConfigFile(cfg, path); err != nil {
			// Only return error if file exists but can't be parsed
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load config from %s: %w", path, err)
			}
			// File doesn't exist - that's OK, skip it
		}
	}

	// Load from environment variables
	loadFromEnv(cfg)

	// Ensure directories exist
	if err := ensureDirectories(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadConfigFile loads a TOML config file and merges it into cfg
func loadConfigFile(cfg *Config, path string) error {
	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return err
	}

	// Parse TOML file
	var fileCfg Config
	if _, err := toml.DecodeFile(path, &fileCfg); err != nil {
		return err
	}

	// Merge into main config
	cfg.Merge(&fileCfg)

	return nil
}

// loadFromEnv loads configuration from environment variables
func loadFromEnv(cfg *Config) {
	// CLAUDE_ON_INCUS_IMAGE
	if env := os.Getenv("CLAUDE_ON_INCUS_IMAGE"); env != "" {
		cfg.Defaults.Image = env
	}

	// CLAUDE_ON_INCUS_SESSIONS_DIR
	if env := os.Getenv("CLAUDE_ON_INCUS_SESSIONS_DIR"); env != "" {
		cfg.Paths.SessionsDir = ExpandPath(env)
	}

	// CLAUDE_ON_INCUS_STORAGE_DIR
	if env := os.Getenv("CLAUDE_ON_INCUS_STORAGE_DIR"); env != "" {
		cfg.Paths.StorageDir = ExpandPath(env)
	}

	// CLAUDE_ON_INCUS_PERSISTENT
	if env := os.Getenv("CLAUDE_ON_INCUS_PERSISTENT"); env == "true" || env == "1" {
		cfg.Defaults.Persistent = true
	}

	// Limit environment variables (using COI_ prefix for brevity)
	// CPU limits
	if env := os.Getenv("COI_LIMIT_CPU"); env != "" {
		cfg.Limits.CPU.Count = env
	}
	if env := os.Getenv("COI_LIMIT_CPU_ALLOWANCE"); env != "" {
		cfg.Limits.CPU.Allowance = env
	}

	// Memory limits
	if env := os.Getenv("COI_LIMIT_MEMORY"); env != "" {
		cfg.Limits.Memory.Limit = env
	}
	if env := os.Getenv("COI_LIMIT_MEMORY_SWAP"); env != "" {
		cfg.Limits.Memory.Swap = env
	}

	// Disk limits
	if env := os.Getenv("COI_LIMIT_DISK_READ"); env != "" {
		cfg.Limits.Disk.Read = env
	}
	if env := os.Getenv("COI_LIMIT_DISK_WRITE"); env != "" {
		cfg.Limits.Disk.Write = env
	}

	// Runtime limits
	if env := os.Getenv("COI_LIMIT_DURATION"); env != "" {
		cfg.Limits.Runtime.MaxDuration = env
	}
}

// ensureDirectories creates necessary directories if they don't exist
func ensureDirectories(cfg *Config) error {
	dirs := []string{
		cfg.Paths.SessionsDir,
		cfg.Paths.StorageDir,
		cfg.Paths.LogsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// WriteExample writes an example config file to the specified path
func WriteExample(path string) error {
	example := `# Claude on Incus Configuration
# See: https://github.com/mensfeld/code-on-incus

[defaults]
image = "coi"
# Set persistent=true to reuse containers across sessions (keeps installed tools)
persistent = false
model = "claude-sonnet-4-5"

[paths]
sessions_dir = "~/.coi/sessions"
storage_dir = "~/.coi/storage"
logs_dir = "~/.coi/logs"

[incus]
project = "default"
group = "incus-admin"
code_uid = 1000
code_user = "code"

[mounts]
# Default mounts applied to all sessions
# These can be overridden by CLI flags

# Example: Mount AWS credentials (read-only recommended)
# [[mounts.default]]
# host = "~/.aws"
# container = "/home/user/.aws"

# Example: Mount shared data directory
# [[mounts.default]]
# host = "~/shared-data"
# container = "/data"

# Example: Mount Docker socket (advanced users)
# [[mounts.default]]
# host = "/var/run/docker.sock"
# container = "/var/run/docker.sock"

[limits]
# Resource and time limits for containers (empty = unlimited)

[limits.cpu]
# CPU count: "2", "0-3", "0,1,3" or "" for unlimited
count = ""
# CPU allowance: "50%", "25ms/100ms" or "" for unlimited
allowance = ""
# CPU priority: 0-10 (higher = more priority)
priority = 0

[limits.memory]
# Memory limit: "512MiB", "2GiB", "50%" or "" for unlimited
limit = ""
# Enforcement mode: "hard" or "soft"
enforce = "soft"
# Swap: "true", "false", or size like "1GiB"
swap = "true"

[limits.disk]
# Disk read rate: "10MiB/s", "1000iops" or "" for unlimited
read = ""
# Disk write rate: "5MiB/s", "1000iops" or "" for unlimited
write = ""
# Combined read+write limit (overrides read/write if set)
max = ""
# Disk priority: 0-10 (higher = more priority)
priority = 0
# /tmp storage backend (default: "" = use container root disk).
# Leave empty to let /tmp share the container's virtual disk — no RAM used,
# no size cap, cleaned up when the container is deleted.
# Set to a size like "4GiB" to use a RAM-backed tmpfs instead (faster but
# limited; useful when builds produce very large temp data).
tmpfs_size = ""

[limits.runtime]
# Maximum container runtime: "2h", "30m", "1h30m" or "" for unlimited
max_duration = ""
# Maximum processes: 100 or 0 for unlimited
max_processes = 0
# Auto-stop when max_duration reached
auto_stop = true
# Graceful stop (true) or force stop (false)
stop_graceful = true

[git]
# Allow container to write to .git/hooks (default: false)
# By default, .git/hooks is mounted read-only to prevent containers from
# modifying git hooks that could execute malicious code on the host
# Set to true if you need the container to manage git hooks (same as --writable-git-hooks flag)
writable_hooks = false

[security]
# Security-sensitive paths mounted read-only to prevent containers from modifying
# files that could execute automatically on the host. These paths are protected
# to prevent supply chain attacks where a compromised AI tool injects malicious code.
#
# Default protected paths:
#   - .git/hooks  (git hooks execute on git operations)
#   - .git/config (can set core.hooksPath to bypass hooks protection)
#   - .husky      (husky git hooks manager)
#   - .vscode     (VS Code tasks.json can auto-execute, settings.json can inject shell args)
#
# To replace the default list entirely:
# protected_paths = [".git/hooks", ".git/config"]
#
# To add additional paths without replacing defaults:
# additional_protected_paths = [".idea", "Makefile"]
#
# To disable protection entirely (not recommended):
# disable_protection = true

# Example profile for Rust development with persistent container
# [profiles.rust]
# image = "coi-rust"
# environment = { RUST_BACKTRACE = "1" }
# persistent = true

# Example profile for web development
# [profiles.web]
# image = "coi"
# environment = { NODE_ENV = "development" }
# persistent = true

# Example profile with resource limits
# [profiles.limited]
# image = "coi"
# persistent = false
# [profiles.limited.limits.cpu]
# count = "2"
# allowance = "50%"
# [profiles.limited.limits.memory]
# limit = "2GiB"
# [profiles.limited.limits.runtime]
# max_duration = "2h"
`

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write file
	return os.WriteFile(path, []byte(example), 0o644)
}
