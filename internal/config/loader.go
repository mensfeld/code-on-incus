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
# See: https://github.com/mensfeld/claude-on-incus

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
`

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write file
	return os.WriteFile(path, []byte(example), 0o644)
}
