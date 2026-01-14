package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg == nil {
		t.Fatal("Expected default config, got nil")
	}

	// Check defaults
	if cfg.Defaults.Image != "coi" {
		t.Errorf("Expected default image 'coi', got '%s'", cfg.Defaults.Image)
	}

	if cfg.Defaults.Model != "claude-sonnet-4-5" {
		t.Errorf("Expected default model 'claude-sonnet-4-5', got '%s'", cfg.Defaults.Model)
	}

	// Check Incus config
	if cfg.Incus.Project != "default" {
		t.Errorf("Expected project 'default', got '%s'", cfg.Incus.Project)
	}

	if cfg.Incus.CodeUID != 1000 {
		t.Errorf("Expected CodeUID 1000, got %d", cfg.Incus.CodeUID)
	}

	// Check paths are set
	if cfg.Paths.SessionsDir == "" {
		t.Error("Expected sessions_dir to be set")
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "expand tilde",
			input:    "~/test",
			expected: filepath.Join(homeDir, "test"),
		},
		{
			name:     "expand tilde only",
			input:    "~",
			expected: homeDir,
		},
		{
			name:     "no expansion needed",
			input:    "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "empty path",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandPath(tt.input)
			if result != tt.expected {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConfigMerge(t *testing.T) {
	base := GetDefaultConfig()
	base.Defaults.Image = "base-image"
	base.Defaults.Model = "base-model"

	other := &Config{
		Defaults: DefaultsConfig{
			Image: "other-image",
			// Model not set - should not override
		},
		Incus: IncusConfig{
			CodeUID: 2000, // Override
		},
	}

	base.Merge(other)

	// Check that other.Image overrode base.Image
	if base.Defaults.Image != "other-image" {
		t.Errorf("Expected image 'other-image', got '%s'", base.Defaults.Image)
	}

	// Check that base.Model remained because other.Model was empty
	if base.Defaults.Model != "base-model" {
		t.Errorf("Expected model 'base-model', got '%s'", base.Defaults.Model)
	}

	// Check that CodeUID was overridden
	if base.Incus.CodeUID != 2000 {
		t.Errorf("Expected CodeUID 2000, got %d", base.Incus.CodeUID)
	}
}

func TestGetProfile(t *testing.T) {
	cfg := GetDefaultConfig()

	// Add a test profile
	cfg.Profiles["test"] = ProfileConfig{
		Image:      "test-image",
		Persistent: true,
	}

	// Test getting existing profile
	profile := cfg.GetProfile("test")
	if profile == nil {
		t.Fatal("Expected profile, got nil")
	}

	if profile.Image != "test-image" {
		t.Errorf("Expected image 'test-image', got '%s'", profile.Image)
	}

	// Test getting non-existent profile
	missing := cfg.GetProfile("nonexistent")
	if missing != nil {
		t.Error("Expected nil for non-existent profile")
	}
}

func TestApplyProfile(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Defaults.Image = "original-image"

	// Add a test profile
	cfg.Profiles["rust"] = ProfileConfig{
		Image:      "rust-image",
		Persistent: true,
	}

	// Apply the profile
	success := cfg.ApplyProfile("rust")
	if !success {
		t.Error("Expected ApplyProfile to return true")
	}

	// Check that defaults were updated
	if cfg.Defaults.Image != "rust-image" {
		t.Errorf("Expected image 'rust-image', got '%s'", cfg.Defaults.Image)
	}

	if !cfg.Defaults.Persistent {
		t.Error("Expected persistent to be true")
	}

	// Try to apply non-existent profile
	success = cfg.ApplyProfile("nonexistent")
	if success {
		t.Error("Expected ApplyProfile to return false for non-existent profile")
	}
}

func TestGetConfigPaths(t *testing.T) {
	paths := GetConfigPaths()

	if len(paths) < 3 {
		t.Errorf("Expected at least 3 config paths, got %d", len(paths))
	}

	// Check that paths are in expected order
	expectedPaths := []string{
		"/etc/coi/config.toml",
	}

	for i, expected := range expectedPaths {
		if paths[i] != expected {
			t.Errorf("Path[%d]: expected %q, got %q", i, expected, paths[i])
		}
	}

	// Check that user config path contains .config
	homeDir, _ := os.UserHomeDir()
	expectedUserPath := filepath.Join(homeDir, ".config/coi/config.toml")
	if paths[1] != expectedUserPath {
		t.Errorf("User config path: expected %q, got %q", expectedUserPath, paths[1])
	}
}

func TestToolConfigDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Tool.Name != "claude" {
		t.Errorf("Expected default tool name 'claude', got '%s'", cfg.Tool.Name)
	}

	if cfg.Tool.Binary != "" {
		t.Errorf("Expected default tool binary to be empty, got '%s'", cfg.Tool.Binary)
	}
}

func TestToolConfigMerge(t *testing.T) {
	base := GetDefaultConfig()
	base.Tool.Name = "claude"
	base.Tool.Binary = ""

	tests := []struct {
		name           string
		otherName      string
		otherBinary    string
		expectedName   string
		expectedBinary string
	}{
		{
			name:           "merge tool name only",
			otherName:      "aider",
			otherBinary:    "",
			expectedName:   "aider",
			expectedBinary: "",
		},
		{
			name:           "merge binary only",
			otherName:      "",
			otherBinary:    "custom-claude",
			expectedName:   "claude",
			expectedBinary: "custom-claude",
		},
		{
			name:           "merge both",
			otherName:      "aider",
			otherBinary:    "custom-aider",
			expectedName:   "aider",
			expectedBinary: "custom-aider",
		},
		{
			name:           "merge neither (empty stays)",
			otherName:      "",
			otherBinary:    "",
			expectedName:   "claude",
			expectedBinary: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset base for each test
			testBase := GetDefaultConfig()
			testBase.Tool.Name = "claude"
			testBase.Tool.Binary = ""

			other := &Config{
				Tool: ToolConfig{
					Name:   tt.otherName,
					Binary: tt.otherBinary,
				},
			}

			testBase.Merge(other)

			if testBase.Tool.Name != tt.expectedName {
				t.Errorf("Expected tool name '%s', got '%s'", tt.expectedName, testBase.Tool.Name)
			}

			if testBase.Tool.Binary != tt.expectedBinary {
				t.Errorf("Expected tool binary '%s', got '%s'", tt.expectedBinary, testBase.Tool.Binary)
			}
		})
	}
}
