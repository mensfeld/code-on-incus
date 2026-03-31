package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	// Clean environment
	cleanEnv := func() {
		os.Unsetenv("CLAUDE_ON_INCUS_IMAGE")
		os.Unsetenv("CLAUDE_ON_INCUS_PERSISTENT")
	}
	defer cleanEnv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("Expected config, got nil")
	}

	// Should have defaults
	if cfg.Defaults.Image == "" {
		t.Error("Expected default image to be set")
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("CLAUDE_ON_INCUS_IMAGE", "env-image")
	os.Setenv("CLAUDE_ON_INCUS_PERSISTENT", "1")
	defer func() {
		os.Unsetenv("CLAUDE_ON_INCUS_IMAGE")
		os.Unsetenv("CLAUDE_ON_INCUS_PERSISTENT")
	}()

	cfg := GetDefaultConfig()
	loadFromEnv(cfg)

	if cfg.Defaults.Image != "env-image" {
		t.Errorf("Expected image 'env-image', got '%s'", cfg.Defaults.Image)
	}

	if cfg.Defaults.Persistent == nil || !*cfg.Defaults.Persistent {
		t.Error("Expected persistent to be true from env")
	}
}

func TestLoadConfigFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
[defaults]
image = "test-image"
model = "test-model"

[incus]
code_uid = 2000
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Load the config
	cfg := GetDefaultConfig()
	if err := loadConfigFile(cfg, configPath); err != nil {
		t.Fatalf("loadConfigFile() failed: %v", err)
	}

	// Verify values
	if cfg.Defaults.Image != "test-image" {
		t.Errorf("Expected image 'test-image', got '%s'", cfg.Defaults.Image)
	}

	if cfg.Defaults.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", cfg.Defaults.Model)
	}

	if cfg.Incus.CodeUID != 2000 {
		t.Errorf("Expected CodeUID 2000, got %d", cfg.Incus.CodeUID)
	}
}

func TestLoadConfigFileNotExists(t *testing.T) {
	cfg := GetDefaultConfig()
	err := loadConfigFile(cfg, "/nonexistent/path/config.toml")

	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	if !os.IsNotExist(err) {
		t.Errorf("Expected os.IsNotExist error, got: %v", err)
	}
}

func TestLoadConfigFileInvalid(t *testing.T) {
	// Create an invalid TOML file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.toml")

	invalidContent := `
[defaults
image = "broken
`

	if err := os.WriteFile(configPath, []byte(invalidContent), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := GetDefaultConfig()
	err := loadConfigFile(cfg, configPath)

	if err == nil {
		t.Error("Expected error for invalid TOML")
	}
}

func TestWriteExample(t *testing.T) {
	tmpDir := t.TempDir()
	examplePath := filepath.Join(tmpDir, "example.toml")

	if err := WriteExample(examplePath); err != nil {
		t.Fatalf("WriteExample() failed: %v", err)
	}

	// Check file exists
	if _, err := os.Stat(examplePath); err != nil {
		t.Errorf("Example file not created: %v", err)
	}

	// Read and verify it's valid TOML
	cfg := GetDefaultConfig()
	if err := loadConfigFile(cfg, examplePath); err != nil {
		t.Errorf("Example file is not valid TOML: %v", err)
	}
}

func TestBuildScriptPathResolution(t *testing.T) {
	// Create a temporary config file with a relative build.script path
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".coi")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
[build]
script = "build.sh"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg := GetDefaultConfig()
	if err := loadConfigFile(cfg, configPath); err != nil {
		t.Fatalf("loadConfigFile() failed: %v", err)
	}

	// Script should be resolved relative to config file directory (.coi/build.sh)
	expectedPath := filepath.Join(configDir, "build.sh")
	if cfg.Build.Script != expectedPath {
		t.Errorf("Expected script path %q, got %q", expectedPath, cfg.Build.Script)
	}
}

func TestBuildScriptAbsolutePath(t *testing.T) {
	// Absolute paths should pass through unchanged
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	configContent := `
[build]
script = "/absolute/path/to/build.sh"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg := GetDefaultConfig()
	if err := loadConfigFile(cfg, configPath); err != nil {
		t.Fatalf("loadConfigFile() failed: %v", err)
	}

	if cfg.Build.Script != "/absolute/path/to/build.sh" {
		t.Errorf("Expected absolute path to be preserved, got %q", cfg.Build.Script)
	}
}

func TestBuildScriptTildePath(t *testing.T) {
	// Tilde paths should be expanded
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	configContent := `
[build]
script = "~/build-scripts/build.sh"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg := GetDefaultConfig()
	if err := loadConfigFile(cfg, configPath); err != nil {
		t.Fatalf("loadConfigFile() failed: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expectedPath := filepath.Join(homeDir, "build-scripts/build.sh")
	if cfg.Build.Script != expectedPath {
		t.Errorf("Expected tilde-expanded path %q, got %q", expectedPath, cfg.Build.Script)
	}
}

func TestDotCoiTomlDeprecationError(t *testing.T) {
	// Create a temp directory and change to it
	tmpDir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get cwd: %v", err)
	}
	defer os.Chdir(oldDir) //nolint:errcheck

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create .coi.toml
	if err := os.WriteFile(filepath.Join(tmpDir, ".coi.toml"), []byte("[defaults]\nimage = \"test\"\n"), 0o644); err != nil {
		t.Fatalf("Failed to create .coi.toml: %v", err)
	}

	// Load should fail with deprecation error
	_, loadErr := Load()
	if loadErr == nil {
		t.Fatal("Expected error when .coi.toml exists, got nil")
	}

	expectedMsg := "found .coi.toml in project root"
	if !strings.Contains(loadErr.Error(), expectedMsg) {
		t.Errorf("Expected error containing %q, got: %v", expectedMsg, loadErr)
	}

	// Should also mention migration command
	expectedMigration := "mkdir -p .coi && mv .coi.toml .coi/config.toml"
	if !strings.Contains(loadErr.Error(), expectedMigration) {
		t.Errorf("Expected error containing migration instructions %q, got: %v", expectedMigration, loadErr)
	}
}

func TestDotCoiDirConfigLoads(t *testing.T) {
	// Create a temp directory with .coi/config.toml and change to it
	tmpDir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get cwd: %v", err)
	}
	defer os.Chdir(oldDir) //nolint:errcheck

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	// Create .coi/config.toml
	coiDir := filepath.Join(tmpDir, ".coi")
	if err := os.MkdirAll(coiDir, 0o755); err != nil {
		t.Fatalf("Failed to create .coi dir: %v", err)
	}
	configContent := `
[defaults]
image = "coi-test-project"
`
	if err := os.WriteFile(filepath.Join(coiDir, "config.toml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to create config.toml: %v", err)
	}

	cfg, loadErr := Load()
	if loadErr != nil {
		t.Fatalf("Load() failed: %v", loadErr)
	}

	if cfg.Defaults.Image != "coi-test-project" {
		t.Errorf("Expected image 'coi-test-project', got %q", cfg.Defaults.Image)
	}
}

func TestResolveRelativePath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		baseDir  string
		path     string
		expected string
	}{
		{
			name:     "relative path",
			baseDir:  "/project/.coi",
			path:     "build.sh",
			expected: "/project/.coi/build.sh",
		},
		{
			name:     "absolute path unchanged",
			baseDir:  "/project/.coi",
			path:     "/usr/local/bin/build.sh",
			expected: "/usr/local/bin/build.sh",
		},
		{
			name:     "tilde path expanded",
			baseDir:  "/project/.coi",
			path:     "~/scripts/build.sh",
			expected: filepath.Join(homeDir, "scripts/build.sh"),
		},
		{
			name:     "nested relative path",
			baseDir:  "/project/.coi",
			path:     "scripts/build.sh",
			expected: "/project/.coi/scripts/build.sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveRelativePath(tt.baseDir, tt.path)
			if result != tt.expected {
				t.Errorf("resolveRelativePath(%q, %q) = %q, want %q", tt.baseDir, tt.path, result, tt.expected)
			}
		})
	}
}

func TestEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Paths: PathsConfig{
			SessionsDir: filepath.Join(tmpDir, "sessions"),
			StorageDir:  filepath.Join(tmpDir, "storage"),
			LogsDir:     filepath.Join(tmpDir, "logs"),
		},
	}

	if err := ensureDirectories(cfg); err != nil {
		t.Fatalf("ensureDirectories() failed: %v", err)
	}

	// Check directories were created
	dirs := []string{cfg.Paths.SessionsDir, cfg.Paths.StorageDir, cfg.Paths.LogsDir}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil {
			t.Errorf("Directory not created: %s: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("Expected directory, got file: %s", dir)
		}
	}
}
