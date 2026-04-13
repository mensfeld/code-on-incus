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
// 2. User config (~/.coi/config.toml)
// 3. Project config (./.coi/config.toml)
// 4. Environment variables (CLAUDE_ON_INCUS_* or COI_*)
//
// Profile directories are scanned independently from config files.
// See GetProfileParentDirs() for the full list of scanned locations.
// Profiles from all scan locations are merged into a single namespace; if the
// same profile name is defined in more than one location Load() returns an
// error asking the user to resolve the conflict.
func Load() (*Config, error) {
	// Check for deprecated .coi.toml in project root
	if err := checkDeprecatedConfig(); err != nil {
		return nil, err
	}

	// Start with defaults
	cfg := GetDefaultConfig()

	// Scan profile directories from all known locations. Profiles are merged
	// into a single namespace; duplicate names across locations are rejected
	// by loadProfileDirectories so it's always clear which profile is used.
	for _, dir := range GetProfileParentDirs() {
		if err := loadProfileDirectories(cfg, dir); err != nil {
			return nil, err
		}
	}

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

	// Inject built-in "default" profile (can be overridden by disk-based profiles)
	if _, exists := cfg.Profiles["default"]; !exists {
		cfg.Profiles["default"] = synthesizeDefaultProfile(cfg)
	}

	// Resolve profile inheritance after all profiles are loaded from all levels
	if err := cfg.ResolveProfileInheritance(); err != nil {
		return nil, fmt.Errorf("profile inheritance error: %w", err)
	}

	// Load from environment variables
	loadFromEnv(cfg)

	// Ensure directories exist
	if err := ensureDirectories(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// OverlayProjectConfig loads the project config from the given workspace directory
// and merges it into cfg. This is used when launching via alias from a different
// directory, so the resolved workspace's .coi/config.toml is applied.
func (c *Config) OverlayProjectConfig(workspaceDir string) error {
	path := filepath.Join(workspaceDir, ".coi", "config.toml")
	return loadConfigFile(c, path)
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

	// Detect pre-0.8.0 layouts and refuse to load with an actionable error.
	if err := checkDeprecatedConfigFields(path); err != nil {
		return err
	}

	configDir := filepath.Dir(path)

	// Resolve build script path relative to config file location
	if fileCfg.Container.Build.Script != "" {
		fileCfg.Container.Build.Script = resolveRelativePath(configDir, fileCfg.Container.Build.Script)
	}

	// Merge into main config
	cfg.Merge(&fileCfg)

	return nil
}

// loadProfileDirectories scans configDir/profiles/ for subdirectories containing config.toml
// and adds them to cfg.Profiles. Each subdirectory name becomes the profile name.
//
// If a profile with the same name is already loaded from a different source
// location, loadProfileDirectories returns an error. Profiles from different
// scan locations (e.g. ~/.coi/profiles/ and ./.coi/profiles/) are merged
// together, and users are expected to use unique names across locations so
// it's always clear which profile is being used.
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

		// Detect duplicate profile name across scan locations. The same path
		// loaded twice is fine (idempotent), but two different paths for the
		// same name is ambiguous — error out and tell the user to rename one.
		if existing, ok := cfg.Profiles[profileName]; ok && existing.Source != "" && existing.Source != profileConfigPath {
			return fmt.Errorf(
				"profile %q defined in multiple locations:\n  %s\n  %s\n"+
					"Rename one of them or delete the duplicate so it's clear which profile is being used",
				profileName, existing.Source, profileConfigPath)
		}

		var profileCfg ProfileConfig
		if _, err := toml.DecodeFile(profileConfigPath, &profileCfg); err != nil {
			return fmt.Errorf("failed to parse profile %q config at %s: %w", profileName, profileConfigPath, err)
		}

		// Detect pre-0.8.0 profile layouts and refuse to load.
		if err := checkDeprecatedProfileFields(profileConfigPath); err != nil {
			return err
		}

		// Resolve paths relative to profile directory
		profileDir := filepath.Join(profilesDir, profileName)
		if profileCfg.Container.Build.Script != "" {
			profileCfg.Container.Build.Script = resolveRelativePath(profileDir, profileCfg.Container.Build.Script)
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
		cfg.Container.Image = env
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
		cfg.Container.Persistent = ptrBool(true)
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

[container]
image = "coi-default"
# Set persistent=true to reuse containers across sessions (keeps installed tools)
persistent = false
# storage_pool selects which Incus storage pool to launch containers in.
# Empty string ("") uses Incus's default pool.
# Create new pools with: incus storage create <name> dir|zfs|btrfs
storage_pool = ""

[defaults]
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

[container.build]
# Build configuration for custom images
# When [container] image is set to a custom name, 'coi build' uses this config.
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
#   context = "CONTEXT.md"    # extra context appended to SANDBOX_CONTEXT.md
#   forward_env = ["RUST_BACKTRACE"]
#
#   [container]
#   image = "coi-rust"
#   persistent = true
#   storage_pool = ""        # empty = inherit from global, or use Incus default
#
#   [container.build]
#   base = "coi"
#   script = "build.sh"      # resolved relative to this config.toml
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
# Default profile directory scan locations:
#   1. ~/.coi/profiles/NAME/config.toml
#   2. .coi/profiles/NAME/config.toml
#
# Additional scan location when COI_CONFIG is set:
#   3. dirname($COI_CONFIG)/profiles/NAME/config.toml
#
# Profiles from all scanned locations are merged into a single namespace — if
# the same name is defined in more than one location, COI will refuse to start
# and ask you to rename one.
#
# Use 'coi profile list' and 'coi profile info <name>' to inspect loaded profiles.
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
