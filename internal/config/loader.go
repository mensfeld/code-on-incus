package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Load loads configuration from all available sources
// Hierarchy (lowest to highest precedence):
// 1. Built-in defaults
// 2. System config (/etc/coi/config.toml)
// 3. User config (~/.config/coi/config.toml)
// 4. Project config (./.coi/config.toml)
// 5. Environment variables (CLAUDE_ON_INCUS_* or COI_*)
func Load() (*Config, error) {
	// Check for deprecated .coi.toml in project root
	if err := checkDeprecatedConfig(); err != nil {
		return nil, err
	}

	// Start with defaults
	cfg := GetDefaultConfig()

	// Load from config files (in order)
	paths := GetConfigPaths()
	for _, path := range paths {
		// Always scan for profile directories at this config level,
		// even if the config file itself doesn't exist.
		configDir := filepath.Dir(path)
		if err := loadProfileDirectories(cfg, configDir); err != nil {
			return nil, err
		}

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

	configDir := filepath.Dir(path)

	// Resolve build script path relative to config file location
	if fileCfg.Build.Script != "" {
		fileCfg.Build.Script = resolveRelativePath(configDir, fileCfg.Build.Script)
	}

	// Merge into main config
	cfg.Merge(&fileCfg)

	return nil
}

// loadProfileDirectories scans configDir/profiles/ for subdirectories containing config.toml
// and adds them to cfg.Profiles. Each subdirectory name becomes the profile name.
func loadProfileDirectories(cfg *Config, configDir string) error {
	profilesDir := filepath.Join(configDir, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil // Directory doesn't exist or can't be read — that's fine
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		profileName := entry.Name()
		profileConfigPath := filepath.Join(profilesDir, profileName, "config.toml")

		if _, err := os.Stat(profileConfigPath); err != nil {
			continue // No config.toml in this subdirectory
		}

		var profileCfg ProfileConfig
		if _, err := toml.DecodeFile(profileConfigPath, &profileCfg); err != nil {
			return fmt.Errorf("failed to parse profile %q config at %s: %w", profileName, profileConfigPath, err)
		}

		// Resolve paths relative to profile directory
		profileDir := filepath.Join(profilesDir, profileName)
		if profileCfg.Build != nil && profileCfg.Build.Script != "" {
			profileCfg.Build.Script = resolveRelativePath(profileDir, profileCfg.Build.Script)
		}
		if profileCfg.Context != "" {
			profileCfg.Context = resolveRelativePath(profileDir, profileCfg.Context)
		}

		// Tag with source location
		profileCfg.Source = profileConfigPath

		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]ProfileConfig)
		}
		cfg.Profiles[profileName] = profileCfg
	}
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
		cfg.Defaults.Persistent = ptrBool(true)
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
# Forward host environment variables into the container by name
# Values are read from the host at session start — never stored in config
# forward_env = ["ANTHROPIC_API_KEY", "GITHUB_TOKEN", "AWS_ACCESS_KEY_ID"]

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
# Each mount can optionally be read-only (readonly = true)

# Example: Mount Claude Code skills (read-only)
# [[mounts.default]]
# host = "~/.claude/skills"
# container = "/home/code/.claude/skills"
# readonly = true

# Example: Mount Claude Code commands (read-only)
# [[mounts.default]]
# host = "~/.claude/commands"
# container = "/home/code/.claude/commands"
# readonly = true

# Example: Mount AWS credentials (read-only recommended)
# [[mounts.default]]
# host = "~/.aws"
# container = "/home/code/.aws"
# readonly = true

# Example: Mount shared data directory (read-write)
# [[mounts.default]]
# host = "~/shared-data"
# container = "/data"

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

[timezone]
# Timezone mode: "host" (inherit from host, default), "fixed", "utc"
mode = "host"
# Fixed timezone name (only when mode = "fixed")
# name = "Europe/Warsaw"

[ssh]
# Forward host SSH agent to container (default: false)
# When enabled, the host's SSH_AUTH_SOCK is forwarded into the container via an
# Incus proxy device. This allows git over SSH to work inside the container without
# copying SSH keys.
forward_agent = false

[git]
# Allow container to write to .git/hooks (default: false)
# By default, .git/hooks is mounted read-only to prevent containers from
# modifying git hooks that could execute malicious code on the host
# Set to true if you need the container to manage git hooks
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

[build]
# Build configuration for custom images
# When defaults.image is set to a custom name, 'coi build' uses this config.
# base = "coi"                    # Base image to build from (default: "coi")
# script = "build.sh"             # Path to build script (relative to config file)
# commands = ["mise install ruby@3.3", "gem install bundler"]  # Or inline commands

# === Profiles ===
# Profiles are self-contained directories under profiles/.
# Each directory contains its own config.toml (and optionally a build script).
#
# Directory structure:
#   .coi/
#   ├── config.toml              # project config
#   └── profiles/
#       ├── rust-dev/
#       │   ├── config.toml      # profile config
#       │   └── build.sh         # profile-specific build script
#       └── python-ml/
#           ├── config.toml
#           └── setup.sh
#
# Profile directory config.toml example (.coi/profiles/rust-dev/config.toml):
#   image = "coi-rust"
#   persistent = true
#   context = "CONTEXT.md"    # extra context appended to SANDBOX_CONTEXT.md
#   forward_env = ["RUST_BACKTRACE"]
#
#   [environment]
#   RUST_BACKTRACE = "1"
#
#   [tool]
#   name = "claude"
#   permission_mode = "bypass"
#
#   [tool.claude]
#   effort_level = "high"
#
#   [build]
#   base = "coi"
#   script = "build.sh"    # resolved relative to this config.toml
#
#   [[mounts]]
#   host = "~/.cargo"
#   container = "/home/code/.cargo"
#
#   [limits.cpu]
#   count = "4"
#
#   [network]
#   mode = "restricted"
#
# Profile directory scan locations (lowest to highest precedence):
#   1. /etc/coi/profiles/NAME/config.toml
#   2. ~/.config/coi/profiles/NAME/config.toml
#   3. .coi/profiles/NAME/config.toml
#
# Use 'coi profile list' and 'coi profile show <name>' to inspect loaded profiles.
`

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write file
	return os.WriteFile(path, []byte(example), 0o644)
}

// checkDeprecatedConfig checks for the deprecated .coi.toml file in the current directory.
// Returns an error with migration instructions if found.
func checkDeprecatedConfig() error {
	workDir, err := os.Getwd()
	if err != nil {
		return nil // Can't check, skip
	}

	oldPath := filepath.Join(workDir, ".coi.toml")
	if _, err := os.Stat(oldPath); err == nil {
		return fmt.Errorf(
			"found .coi.toml in project root. As of this version, project config must be placed in .coi/config.toml. " +
				"Please move your config: mkdir -p .coi && mv .coi.toml .coi/config.toml",
		)
	}

	return nil
}

// resolveRelativePath resolves a path relative to a base directory.
// Absolute paths and ~-prefixed paths are returned as-is (with ~ expanded).
func resolveRelativePath(baseDir, path string) string {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return ExpandPath(path)
	}
	return filepath.Join(baseDir, path)
}
