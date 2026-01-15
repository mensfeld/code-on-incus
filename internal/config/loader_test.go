package config

import (
	"os"
	"path/filepath"
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

	if !cfg.Defaults.Persistent {
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
