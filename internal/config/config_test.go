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
		Persistent: ptrBool(true),
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
		Persistent: ptrBool(true),
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

	if cfg.Defaults.Persistent == nil || !*cfg.Defaults.Persistent {
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

func TestGitConfigDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	// Default should be to NOT allow writable hooks (protection enabled)
	if cfg.Git.WritableHooks == nil || *cfg.Git.WritableHooks {
		t.Error("Expected default Git.WritableHooks to be false")
	}
}

func TestGitConfigMerge(t *testing.T) {
	ptrBool := func(b bool) *bool { return &b }

	tests := []struct {
		name           string
		baseWritable   *bool
		otherWritable  *bool
		expectedResult *bool
	}{
		{
			name:           "true merged with true",
			baseWritable:   ptrBool(true),
			otherWritable:  ptrBool(true),
			expectedResult: ptrBool(true),
		},
		{
			name:           "true merged with false",
			baseWritable:   ptrBool(true),
			otherWritable:  ptrBool(false),
			expectedResult: ptrBool(false),
		},
		{
			name:           "false merged with true",
			baseWritable:   ptrBool(false),
			otherWritable:  ptrBool(true),
			expectedResult: ptrBool(true),
		},
		{
			name:           "false merged with false",
			baseWritable:   ptrBool(false),
			otherWritable:  ptrBool(false),
			expectedResult: ptrBool(false),
		},
		{
			name:           "true merged with nil (not set)",
			baseWritable:   ptrBool(true),
			otherWritable:  nil,
			expectedResult: ptrBool(true),
		},
		{
			name:           "false merged with nil (not set)",
			baseWritable:   ptrBool(false),
			otherWritable:  nil,
			expectedResult: ptrBool(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Git.WritableHooks = tt.baseWritable

			other := &Config{
				Git: GitConfig{
					WritableHooks: tt.otherWritable,
				},
			}

			base.Merge(other)

			if tt.expectedResult == nil {
				if base.Git.WritableHooks != nil {
					t.Errorf("Expected Git.WritableHooks to be nil, got %v", *base.Git.WritableHooks)
				}
			} else {
				if base.Git.WritableHooks == nil {
					t.Errorf("Expected Git.WritableHooks to be %v, got nil", *tt.expectedResult)
				} else if *base.Git.WritableHooks != *tt.expectedResult {
					t.Errorf("Expected Git.WritableHooks to be %v, got %v", *tt.expectedResult, *base.Git.WritableHooks)
				}
			}
		})
	}
}

func TestSSHConfigDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.SSH.ForwardAgent == nil || *cfg.SSH.ForwardAgent {
		t.Error("Expected default SSH.ForwardAgent to be false")
	}
}

func TestSSHConfigMerge(t *testing.T) {
	ptrBool := func(b bool) *bool { return &b }

	tests := []struct {
		name           string
		baseForward    *bool
		otherForward   *bool
		expectedResult *bool
	}{
		{
			name:           "false merged with true",
			baseForward:    ptrBool(false),
			otherForward:   ptrBool(true),
			expectedResult: ptrBool(true),
		},
		{
			name:           "true merged with false",
			baseForward:    ptrBool(true),
			otherForward:   ptrBool(false),
			expectedResult: ptrBool(false),
		},
		{
			name:           "false merged with nil (not set)",
			baseForward:    ptrBool(false),
			otherForward:   nil,
			expectedResult: ptrBool(false),
		},
		{
			name:           "true merged with nil (not set)",
			baseForward:    ptrBool(true),
			otherForward:   nil,
			expectedResult: ptrBool(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.SSH.ForwardAgent = tt.baseForward

			other := &Config{
				SSH: SSHConfig{
					ForwardAgent: tt.otherForward,
				},
			}

			base.Merge(other)

			if tt.expectedResult == nil {
				if base.SSH.ForwardAgent != nil {
					t.Errorf("Expected SSH.ForwardAgent to be nil, got %v", *base.SSH.ForwardAgent)
				}
			} else {
				if base.SSH.ForwardAgent == nil {
					t.Errorf("Expected SSH.ForwardAgent to be %v, got nil", *tt.expectedResult)
				} else if *base.SSH.ForwardAgent != *tt.expectedResult {
					t.Errorf("Expected SSH.ForwardAgent to be %v, got %v", *tt.expectedResult, *base.SSH.ForwardAgent)
				}
			}
		})
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

func TestDefaultTmpfsSize(t *testing.T) {
	cfg := GetDefaultConfig()

	// Default is empty: /tmp uses the container's root disk, not a RAM-backed tmpfs.
	if cfg.Limits.Disk.TmpfsSize != "" {
		t.Errorf("Expected default TmpfsSize '' (disk-backed), got '%s'", cfg.Limits.Disk.TmpfsSize)
	}
}

func TestLimitsMergeTmpfsSize(t *testing.T) {
	tests := []struct {
		name         string
		baseTmpfs    string
		otherTmpfs   string
		expectedSize string
	}{
		{
			name:         "other overrides base",
			baseTmpfs:    "2GiB",
			otherTmpfs:   "8GiB",
			expectedSize: "8GiB",
		},
		{
			name:         "empty other does not override base",
			baseTmpfs:    "4GiB",
			otherTmpfs:   "",
			expectedSize: "4GiB",
		},
		{
			name:         "both empty stays empty",
			baseTmpfs:    "",
			otherTmpfs:   "",
			expectedSize: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Limits.Disk.TmpfsSize = tt.baseTmpfs

			other := &Config{}
			other.Limits.Disk.TmpfsSize = tt.otherTmpfs

			base.Merge(other)

			if base.Limits.Disk.TmpfsSize != tt.expectedSize {
				t.Errorf("TmpfsSize: expected %q, got %q", tt.expectedSize, base.Limits.Disk.TmpfsSize)
			}
		})
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

func TestContextFileMerge(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		base     string
		other    string
		expected string
	}{
		{
			name:     "empty base + set other = expanded other",
			base:     "",
			other:    "~/my-context.md",
			expected: filepath.Join(homeDir, "my-context.md"),
		},
		{
			name:     "set base + empty other = preserved base",
			base:     "/some/path.md",
			other:    "",
			expected: "/some/path.md",
		},
		{
			name:     "set base + set other = expanded other",
			base:     "/old/path.md",
			other:    "~/new-context.md",
			expected: filepath.Join(homeDir, "new-context.md"),
		},
		{
			name:     "both empty stays empty",
			base:     "",
			other:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Tool.ContextFile = tt.base

			other := &Config{
				Tool: ToolConfig{
					ContextFile: tt.other,
				},
			}

			base.Merge(other)

			if base.Tool.ContextFile != tt.expected {
				t.Errorf("Expected ContextFile %q, got %q", tt.expected, base.Tool.ContextFile)
			}
		})
	}
}

func TestClaudeEffortLevelMerge(t *testing.T) {
	tests := []struct {
		name          string
		baseEffort    string
		otherEffort   string
		expectedLevel string
	}{
		{
			name:          "merge effort from empty base",
			baseEffort:    "",
			otherEffort:   "high",
			expectedLevel: "high",
		},
		{
			name:          "merge effort overwrites base",
			baseEffort:    "low",
			otherEffort:   "medium",
			expectedLevel: "medium",
		},
		{
			name:          "empty other preserves base",
			baseEffort:    "high",
			otherEffort:   "",
			expectedLevel: "high",
		},
		{
			name:          "both empty stays empty",
			baseEffort:    "",
			otherEffort:   "",
			expectedLevel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Tool.Claude.EffortLevel = tt.baseEffort

			other := &Config{
				Tool: ToolConfig{
					Claude: ClaudeToolConfig{
						EffortLevel: tt.otherEffort,
					},
				},
			}

			base.Merge(other)

			if base.Tool.Claude.EffortLevel != tt.expectedLevel {
				t.Errorf("Expected effort level '%s', got '%s'", tt.expectedLevel, base.Tool.Claude.EffortLevel)
			}
		})
	}
}

func TestMergeBoolZeroValueBug(t *testing.T) {
	// This test demonstrates a bug where merging a zero-value Config (simulating
	// a TOML file that only sets string fields) overwrites security-critical
	// boolean defaults with false. For example, a user config at
	// ~/.config/coi/config.toml that only sets image = "my-image" should NOT
	// reset block_private_networks from true to false.

	base := GetDefaultConfig()

	// Verify defaults are set correctly before merge
	if base.Network.BlockPrivateNetworks == nil || !*base.Network.BlockPrivateNetworks {
		t.Fatal("Expected default BlockPrivateNetworks to be true")
	}
	if base.Network.BlockMetadataEndpoint == nil || !*base.Network.BlockMetadataEndpoint {
		t.Fatal("Expected default BlockMetadataEndpoint to be true")
	}
	if base.Monitoring.AutoPauseOnHigh == nil || !*base.Monitoring.AutoPauseOnHigh {
		t.Fatal("Expected default AutoPauseOnHigh to be true")
	}
	if base.Monitoring.AutoKillOnCritical == nil || !*base.Monitoring.AutoKillOnCritical {
		t.Fatal("Expected default AutoKillOnCritical to be true")
	}
	if base.Limits.Runtime.AutoStop == nil || !*base.Limits.Runtime.AutoStop {
		t.Fatal("Expected default AutoStop to be true")
	}
	if base.Limits.Runtime.StopGraceful == nil || !*base.Limits.Runtime.StopGraceful {
		t.Fatal("Expected default StopGraceful to be true")
	}
	if base.Network.Logging.Enabled == nil || !*base.Network.Logging.Enabled {
		t.Fatal("Expected default NetworkLogging.Enabled to be true")
	}
	if base.Monitoring.NFT.LogDNSQueries == nil || !*base.Monitoring.NFT.LogDNSQueries {
		t.Fatal("Expected default NFT.LogDNSQueries to be true")
	}

	// Create a zero-value config, simulating a TOML file that only sets
	// image = "my-image" (all booleans remain nil / zero-value).
	other := &Config{
		Defaults: DefaultsConfig{
			Image: "my-image",
		},
	}

	base.Merge(other)

	// After merge, all security-critical bool defaults must survive.
	if base.Network.BlockPrivateNetworks == nil || !*base.Network.BlockPrivateNetworks {
		t.Error("BlockPrivateNetworks was silently reset to false by merge")
	}
	if base.Network.BlockMetadataEndpoint == nil || !*base.Network.BlockMetadataEndpoint {
		t.Error("BlockMetadataEndpoint was silently reset to false by merge")
	}
	if base.Monitoring.AutoPauseOnHigh == nil || !*base.Monitoring.AutoPauseOnHigh {
		t.Error("AutoPauseOnHigh was silently reset to false by merge")
	}
	if base.Monitoring.AutoKillOnCritical == nil || !*base.Monitoring.AutoKillOnCritical {
		t.Error("AutoKillOnCritical was silently reset to false by merge")
	}
	if base.Limits.Runtime.AutoStop == nil || !*base.Limits.Runtime.AutoStop {
		t.Error("AutoStop was silently reset to false by merge")
	}
	if base.Limits.Runtime.StopGraceful == nil || !*base.Limits.Runtime.StopGraceful {
		t.Error("StopGraceful was silently reset to false by merge")
	}
	if base.Network.Logging.Enabled == nil || !*base.Network.Logging.Enabled {
		t.Error("NetworkLogging.Enabled was silently reset to false by merge")
	}
	if base.Monitoring.NFT.LogDNSQueries == nil || !*base.Monitoring.NFT.LogDNSQueries {
		t.Error("NFT.LogDNSQueries was silently reset to false by merge")
	}

	// Verify the image WAS overridden (merge still works for string fields).
	if base.Defaults.Image != "my-image" {
		t.Errorf("Expected image 'my-image', got '%s'", base.Defaults.Image)
	}
}

func TestPermissionModeMerge(t *testing.T) {
	tests := []struct {
		name         string
		baseMode     string
		otherMode    string
		expectedMode string
	}{
		{
			name:         "empty base + interactive other = interactive",
			baseMode:     "",
			otherMode:    "interactive",
			expectedMode: "interactive",
		},
		{
			name:         "bypass base + interactive other = interactive",
			baseMode:     "bypass",
			otherMode:    "interactive",
			expectedMode: "interactive",
		},
		{
			name:         "interactive base + empty other = interactive (preserved)",
			baseMode:     "interactive",
			otherMode:    "",
			expectedMode: "interactive",
		},
		{
			name:         "empty base + empty other = empty",
			baseMode:     "",
			otherMode:    "",
			expectedMode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Tool.PermissionMode = tt.baseMode

			other := &Config{
				Tool: ToolConfig{
					PermissionMode: tt.otherMode,
				},
			}

			base.Merge(other)

			if base.Tool.PermissionMode != tt.expectedMode {
				t.Errorf("Expected permission mode '%s', got '%s'", tt.expectedMode, base.Tool.PermissionMode)
			}
		})
	}
}

func TestForwardEnvDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	if len(cfg.Defaults.ForwardEnv) != 0 {
		t.Errorf("Expected default ForwardEnv to be empty, got %v", cfg.Defaults.ForwardEnv)
	}
	if len(cfg.Defaults.Environment) != 0 {
		t.Errorf("Expected default Environment to be empty/nil, got %v", cfg.Defaults.Environment)
	}
}

func TestForwardEnvMerge(t *testing.T) {
	tests := []struct {
		name     string
		base     []string
		other    []string
		expected []string
	}{
		{
			name:     "empty base, non-empty other",
			base:     nil,
			other:    []string{"A", "B"},
			expected: []string{"A", "B"},
		},
		{
			name:     "non-empty base, empty other (preserved)",
			base:     []string{"A"},
			other:    nil,
			expected: []string{"A"},
		},
		{
			name:     "overlapping lists are deduplicated",
			base:     []string{"A", "B"},
			other:    []string{"B", "C"},
			expected: []string{"A", "B", "C"},
		},
		{
			name:     "both empty stays empty",
			base:     nil,
			other:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Defaults.ForwardEnv = tt.base

			other := &Config{
				Defaults: DefaultsConfig{
					ForwardEnv: tt.other,
				},
			}

			base.Merge(other)

			if len(base.Defaults.ForwardEnv) != len(tt.expected) {
				t.Fatalf("ForwardEnv length: expected %d, got %d (%v)", len(tt.expected), len(base.Defaults.ForwardEnv), base.Defaults.ForwardEnv)
			}
			for i, v := range tt.expected {
				if base.Defaults.ForwardEnv[i] != v {
					t.Errorf("ForwardEnv[%d]: expected %q, got %q", i, v, base.Defaults.ForwardEnv[i])
				}
			}
		})
	}
}

func TestEnvironmentMerge(t *testing.T) {
	base := GetDefaultConfig()
	base.Defaults.Environment = map[string]string{"A": "1", "B": "2"}

	other := &Config{
		Defaults: DefaultsConfig{
			Environment: map[string]string{"B": "override", "C": "3"},
		},
	}

	base.Merge(other)

	expected := map[string]string{"A": "1", "B": "override", "C": "3"}
	for k, v := range expected {
		if base.Defaults.Environment[k] != v {
			t.Errorf("Environment[%q]: expected %q, got %q", k, v, base.Defaults.Environment[k])
		}
	}
}

func TestApplyProfileEnvironment(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["test"] = ProfileConfig{
		Environment: map[string]string{"RUST_BACKTRACE": "1", "FOO": "bar"},
	}

	cfg.ApplyProfile("test")

	if cfg.Defaults.Environment == nil {
		t.Fatal("Expected Environment to be set after ApplyProfile")
	}
	if cfg.Defaults.Environment["RUST_BACKTRACE"] != "1" {
		t.Errorf("Expected RUST_BACKTRACE=1, got %q", cfg.Defaults.Environment["RUST_BACKTRACE"])
	}
	if cfg.Defaults.Environment["FOO"] != "bar" {
		t.Errorf("Expected FOO=bar, got %q", cfg.Defaults.Environment["FOO"])
	}
}

func TestBuildConfigMerge(t *testing.T) {
	tests := []struct {
		name             string
		baseBase         string
		baseScript       string
		baseCommands     []string
		otherBase        string
		otherScript      string
		otherCommands    []string
		expectedBase     string
		expectedScript   string
		expectedCommands []string
	}{
		{
			name:             "empty base, set other",
			otherBase:        "coi-custom",
			otherScript:      "/path/to/build.sh",
			otherCommands:    []string{"echo hello"},
			expectedBase:     "coi-custom",
			expectedScript:   "/path/to/build.sh",
			expectedCommands: []string{"echo hello"},
		},
		{
			name:             "set base, empty other preserves base",
			baseBase:         "coi-rust",
			baseScript:       "/old/build.sh",
			baseCommands:     []string{"cargo build"},
			expectedBase:     "coi-rust",
			expectedScript:   "/old/build.sh",
			expectedCommands: []string{"cargo build"},
		},
		{
			name:             "other overrides base",
			baseBase:         "coi-old",
			baseScript:       "/old/build.sh",
			baseCommands:     []string{"old command"},
			otherBase:        "coi-new",
			otherScript:      "/new/build.sh",
			otherCommands:    []string{"new command"},
			expectedBase:     "coi-new",
			expectedScript:   "/new/build.sh",
			expectedCommands: []string{"new command"},
		},
		{
			name:             "commands replace entirely (not append)",
			baseCommands:     []string{"cmd1", "cmd2"},
			otherCommands:    []string{"cmd3"},
			expectedCommands: []string{"cmd3"},
		},
		{
			name:         "empty commands does not clear base commands",
			baseCommands: []string{"cmd1"},
			// otherCommands is nil
			expectedCommands: []string{"cmd1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Build.Base = tt.baseBase
			base.Build.Script = tt.baseScript
			base.Build.Commands = tt.baseCommands

			other := &Config{
				Build: BuildConfig{
					Base:     tt.otherBase,
					Script:   tt.otherScript,
					Commands: tt.otherCommands,
				},
			}

			base.Merge(other)

			if base.Build.Base != tt.expectedBase {
				t.Errorf("Build.Base: expected %q, got %q", tt.expectedBase, base.Build.Base)
			}
			if base.Build.Script != tt.expectedScript {
				t.Errorf("Build.Script: expected %q, got %q", tt.expectedScript, base.Build.Script)
			}
			if len(base.Build.Commands) != len(tt.expectedCommands) {
				t.Fatalf("Build.Commands length: expected %d, got %d (%v)", len(tt.expectedCommands), len(base.Build.Commands), base.Build.Commands)
			}
			for i, v := range tt.expectedCommands {
				if base.Build.Commands[i] != v {
					t.Errorf("Build.Commands[%d]: expected %q, got %q", i, v, base.Build.Commands[i])
				}
			}
		})
	}
}

func TestBuildConfigHasBuildConfig(t *testing.T) {
	tests := []struct {
		name     string
		build    BuildConfig
		expected bool
	}{
		{
			name:     "empty config",
			build:    BuildConfig{},
			expected: false,
		},
		{
			name:     "script only",
			build:    BuildConfig{Script: "/path/to/build.sh"},
			expected: true,
		},
		{
			name:     "commands only",
			build:    BuildConfig{Commands: []string{"echo hello"}},
			expected: true,
		},
		{
			name:     "both script and commands",
			build:    BuildConfig{Script: "/path/to/build.sh", Commands: []string{"echo hello"}},
			expected: true,
		},
		{
			name:     "base only is not enough",
			build:    BuildConfig{Base: "coi"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.HasBuildConfig(); got != tt.expected {
				t.Errorf("HasBuildConfig() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildConfigEdgeCases(t *testing.T) {
	t.Run("empty commands slice is not build config", func(t *testing.T) {
		b := BuildConfig{Commands: []string{}}
		if b.HasBuildConfig() {
			t.Error("Empty commands slice should not count as build config")
		}
	})

	t.Run("nil commands is not build config", func(t *testing.T) {
		b := BuildConfig{Commands: nil}
		if b.HasBuildConfig() {
			t.Error("Nil commands should not count as build config")
		}
	})

	t.Run("whitespace-only script is build config", func(t *testing.T) {
		// A non-empty string counts — validation happens at build time
		b := BuildConfig{Script: " "}
		if !b.HasBuildConfig() {
			t.Error("Non-empty script string should count as build config")
		}
	})

	t.Run("merge does not mix script and commands across configs", func(t *testing.T) {
		base := GetDefaultConfig()
		base.Build.Script = "/path/to/build.sh"

		other := &Config{
			Build: BuildConfig{
				Commands: []string{"echo hello"},
			},
		}

		base.Merge(other)

		// Both should be set after merge (script from base, commands from other)
		if base.Build.Script != "/path/to/build.sh" {
			t.Errorf("Script should be preserved from base, got %q", base.Build.Script)
		}
		if len(base.Build.Commands) != 1 || base.Build.Commands[0] != "echo hello" {
			t.Errorf("Commands should be set from other, got %v", base.Build.Commands)
		}
	})
}

func TestPreserveWorkspacePathMerge(t *testing.T) {
	tests := []struct {
		name     string
		base     bool
		other    bool
		expected bool
	}{
		{
			name:     "false base, true other = true",
			base:     false,
			other:    true,
			expected: true,
		},
		{
			name:     "true base, false other = true (sticky)",
			base:     true,
			other:    false,
			expected: true,
		},
		{
			name:     "false base, false other = false",
			base:     false,
			other:    false,
			expected: false,
		},
		{
			name:     "true base, true other = true",
			base:     true,
			other:    true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := GetDefaultConfig()
			base.Paths.PreserveWorkspacePath = tt.base

			other := &Config{
				Paths: PathsConfig{
					PreserveWorkspacePath: tt.other,
				},
			}

			base.Merge(other)

			if base.Paths.PreserveWorkspacePath != tt.expected {
				t.Errorf("Expected PreserveWorkspacePath=%v, got %v", tt.expected, base.Paths.PreserveWorkspacePath)
			}
		})
	}
}
