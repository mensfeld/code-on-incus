package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedDefaultConfigParses(t *testing.T) {
	// GetDefaultConfig panics if parsing fails — if we get here, it parsed OK
	cfg := GetDefaultConfig()
	if cfg == nil {
		t.Fatal("GetDefaultConfig() returned nil")
	}
}

func TestEmbeddedDefaultConfigValues(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Container.Image != "coi-default" {
		t.Errorf("Expected image 'coi-default', got %q", cfg.Container.Image)
	}
	if cfg.Defaults.Model != "claude-sonnet-4-5" {
		t.Errorf("Expected model 'claude-sonnet-4-5', got %q", cfg.Defaults.Model)
	}
	if cfg.Container.Persistent == nil || *cfg.Container.Persistent {
		t.Error("Expected persistent=false")
	}
	if cfg.Network.Mode != NetworkModeRestricted {
		t.Errorf("Expected network mode 'restricted', got %q", cfg.Network.Mode)
	}
	if cfg.Network.BlockPrivateNetworks == nil || !*cfg.Network.BlockPrivateNetworks {
		t.Error("Expected block_private_networks=true")
	}
	if cfg.Network.BlockMetadataEndpoint == nil || !*cfg.Network.BlockMetadataEndpoint {
		t.Error("Expected block_metadata_endpoint=true")
	}
	if cfg.Tool.Name != "claude" {
		t.Errorf("Expected tool name 'claude', got %q", cfg.Tool.Name)
	}
	if cfg.Tool.AutoContext == nil || !*cfg.Tool.AutoContext {
		t.Error("Expected auto_context=true")
	}
	if cfg.Incus.Project != "default" {
		t.Errorf("Expected incus project 'default', got %q", cfg.Incus.Project)
	}
	if cfg.Incus.CodeUID != 1000 {
		t.Errorf("Expected code_uid 1000, got %d", cfg.Incus.CodeUID)
	}
	if cfg.Git.WritableHooks == nil || *cfg.Git.WritableHooks {
		t.Error("Expected writable_hooks=false")
	}
	if cfg.SSH.ForwardAgent == nil || *cfg.SSH.ForwardAgent {
		t.Error("Expected forward_agent=false")
	}
	if len(cfg.Security.ProtectedPaths) != 4 {
		t.Errorf("Expected 4 protected paths, got %d", len(cfg.Security.ProtectedPaths))
	}
	if cfg.Timezone.Mode != "host" {
		t.Errorf("Expected timezone mode 'host', got %q", cfg.Timezone.Mode)
	}
	if cfg.Monitoring.Enabled == nil || *cfg.Monitoring.Enabled {
		t.Error("Expected monitoring enabled=false")
	}
	if cfg.Monitoring.AutoPauseOnHigh == nil || !*cfg.Monitoring.AutoPauseOnHigh {
		t.Error("Expected auto_pause_on_high=true")
	}
	if cfg.Limits.Memory.Enforce != "soft" {
		t.Errorf("Expected memory enforce 'soft', got %q", cfg.Limits.Memory.Enforce)
	}
	if cfg.Limits.Runtime.AutoStop == nil || !*cfg.Limits.Runtime.AutoStop {
		t.Error("Expected auto_stop=true")
	}
	if cfg.Network.RefreshIntervalMinutes != 30 {
		t.Errorf("Expected refresh interval 30, got %d", cfg.Network.RefreshIntervalMinutes)
	}
}

func TestExpandConfigPaths(t *testing.T) {
	cfg := GetDefaultConfig()
	homeDir, _ := os.UserHomeDir()

	// All paths should be expanded (no ~ prefix)
	if strings.HasPrefix(cfg.Paths.SessionsDir, "~") {
		t.Errorf("SessionsDir not expanded: %q", cfg.Paths.SessionsDir)
	}
	if strings.HasPrefix(cfg.Paths.StorageDir, "~") {
		t.Errorf("StorageDir not expanded: %q", cfg.Paths.StorageDir)
	}
	if strings.HasPrefix(cfg.Paths.LogsDir, "~") {
		t.Errorf("LogsDir not expanded: %q", cfg.Paths.LogsDir)
	}
	if strings.HasPrefix(cfg.Network.Logging.Path, "~") {
		t.Errorf("Network logging path not expanded: %q", cfg.Network.Logging.Path)
	}

	// Verify paths contain home directory
	expectedSessionsDir := filepath.Join(homeDir, ".coi", "sessions")
	if cfg.Paths.SessionsDir != expectedSessionsDir {
		t.Errorf("Expected sessions_dir %q, got %q", expectedSessionsDir, cfg.Paths.SessionsDir)
	}
}

func TestSynthesizeDefaultProfile(t *testing.T) {
	cfg := GetDefaultConfig()
	profile := synthesizeDefaultProfile(cfg)

	if profile.Container.Image != "coi-default" {
		t.Errorf("Expected image 'coi-default', got %q", profile.Container.Image)
	}
	if profile.Source != "(built-in)" {
		t.Errorf("Expected source '(built-in)', got %q", profile.Source)
	}
	if profile.Tool == nil || profile.Tool.Name != "claude" {
		t.Error("Expected tool.name=claude")
	}
	if profile.Network == nil || profile.Network.Mode != NetworkModeRestricted {
		t.Error("Expected network mode 'restricted'")
	}
	if profile.Limits == nil {
		t.Error("Expected limits to be set")
	}
	if profile.Paths == nil || profile.Paths.SessionsDir == "" {
		t.Error("Expected paths to be set")
	}
	if profile.Git == nil || profile.Git.WritableHooks == nil {
		t.Error("Expected git config to be set")
	}
	if profile.SSH == nil || profile.SSH.ForwardAgent == nil {
		t.Error("Expected ssh config to be set")
	}
	if profile.Security == nil || len(profile.Security.ProtectedPaths) == 0 {
		t.Error("Expected security config to be set")
	}
	if profile.Monitoring == nil || profile.Monitoring.Enabled == nil {
		t.Error("Expected monitoring config to be set")
	}
	if profile.Timezone == nil || profile.Timezone.Mode != "host" {
		t.Error("Expected timezone mode 'host'")
	}
	if profile.Model != "claude-sonnet-4-5" {
		t.Errorf("Expected model 'claude-sonnet-4-5', got %q", profile.Model)
	}
}

func TestDefaultProfileInList(t *testing.T) {
	// Simulate Load() behavior: inject default profile
	cfg := GetDefaultConfig()

	// Inject default profile like Load() does
	if _, exists := cfg.Profiles["default"]; !exists {
		cfg.Profiles["default"] = synthesizeDefaultProfile(cfg)
	}

	p := cfg.GetProfile("default")
	if p == nil {
		t.Fatal("Expected 'default' profile to exist")
	}
	if p.Source != "(built-in)" {
		t.Errorf("Expected source '(built-in)', got %q", p.Source)
	}
	if p.Container.Image != "coi-default" {
		t.Errorf("Expected image 'coi-default', got %q", p.Container.Image)
	}
}

func TestDefaultProfileOverriddenByDisk(t *testing.T) {
	cfg := GetDefaultConfig()

	// Simulate a disk-based "default" profile already loaded
	cfg.Profiles["default"] = ProfileConfig{
		Container: ContainerConfig{
			Image: "my-custom-default",
		},
		Source: "/home/user/.coi/profiles/default/config.toml",
	}

	// Load() only injects if "default" doesn't exist
	if _, exists := cfg.Profiles["default"]; !exists {
		cfg.Profiles["default"] = synthesizeDefaultProfile(cfg)
	}

	p := cfg.GetProfile("default")
	if p == nil {
		t.Fatal("Expected 'default' profile to exist")
	}
	if p.Container.Image != "my-custom-default" {
		t.Errorf("Expected disk-based image 'my-custom-default', got %q", p.Container.Image)
	}
	if p.Source != "/home/user/.coi/profiles/default/config.toml" {
		t.Errorf("Expected disk-based source, got %q", p.Source)
	}
}

func TestInheritFromDefault(t *testing.T) {
	cfg := GetDefaultConfig()

	// Inject default profile
	cfg.Profiles["default"] = synthesizeDefaultProfile(cfg)

	// Add a child profile that inherits from default
	cfg.Profiles["custom"] = ProfileConfig{
		Inherits: "default",
		Container: ContainerConfig{
			Image: "my-custom-image",
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["custom"]
	if child.Container.Image != "my-custom-image" {
		t.Errorf("Expected child image 'my-custom-image', got %q", child.Container.Image)
	}
	// Should inherit network from default
	if child.Network == nil || child.Network.Mode != NetworkModeRestricted {
		t.Error("Expected network mode 'restricted' inherited from default")
	}
	// Should inherit tool from default
	if child.Tool == nil || child.Tool.Name != "claude" {
		t.Error("Expected tool.name='claude' inherited from default")
	}
	// Should inherit git from default
	if child.Git == nil || child.Git.WritableHooks == nil || *child.Git.WritableHooks {
		t.Error("Expected writable_hooks=false inherited from default")
	}
}

func TestNewProfileFieldsMerge(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Model: "parent-model",
		Git:   &GitConfig{WritableHooks: ptrBool(false)},
		SSH:   &SSHConfig{ForwardAgent: ptrBool(false)},
		Timezone: &TimezoneConfig{
			Mode: "host",
		},
		Paths: &PathsConfig{
			SessionsDir: "/parent/sessions",
			StorageDir:  "/parent/storage",
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Model:    "child-model",
		SSH:      &SSHConfig{ForwardAgent: ptrBool(true)},
		Paths: &PathsConfig{
			SessionsDir: "/child/sessions",
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]

	// Model: child wins
	if child.Model != "child-model" {
		t.Errorf("Expected model 'child-model', got %q", child.Model)
	}

	// Git: inherited from parent
	if child.Git == nil || child.Git.WritableHooks == nil || *child.Git.WritableHooks {
		t.Error("Expected writable_hooks=false from parent")
	}

	// SSH: child wins
	if child.SSH == nil || child.SSH.ForwardAgent == nil || !*child.SSH.ForwardAgent {
		t.Error("Expected forward_agent=true from child")
	}

	// Timezone: inherited from parent
	if child.Timezone == nil || child.Timezone.Mode != "host" {
		t.Error("Expected timezone mode 'host' from parent")
	}

	// Paths: deep merge — SessionsDir from child, StorageDir from parent
	if child.Paths == nil {
		t.Fatal("Expected paths to be set")
	}
	if child.Paths.SessionsDir != "/child/sessions" {
		t.Errorf("Expected sessions_dir '/child/sessions', got %q", child.Paths.SessionsDir)
	}
	if child.Paths.StorageDir != "/parent/storage" {
		t.Errorf("Expected storage_dir '/parent/storage', got %q", child.Paths.StorageDir)
	}
}

func TestApplyProfileNewFields(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["test"] = ProfileConfig{
		Model: "custom-model",
		Git:   &GitConfig{WritableHooks: ptrBool(true)},
		SSH:   &SSHConfig{ForwardAgent: ptrBool(true)},
		Timezone: &TimezoneConfig{
			Mode: "utc",
		},
		Monitoring: &MonitoringConfig{
			Enabled: ptrBool(true),
		},
	}

	if err := cfg.ApplyProfile("test"); err != nil {
		t.Fatalf("ApplyProfile failed: %v", err)
	}

	if cfg.Defaults.Model != "custom-model" {
		t.Errorf("Expected model 'custom-model', got %q", cfg.Defaults.Model)
	}
	if cfg.Git.WritableHooks == nil || !*cfg.Git.WritableHooks {
		t.Error("Expected writable_hooks=true after apply")
	}
	if cfg.SSH.ForwardAgent == nil || !*cfg.SSH.ForwardAgent {
		t.Error("Expected forward_agent=true after apply")
	}
	if cfg.Timezone.Mode != "utc" {
		t.Errorf("Expected timezone mode 'utc', got %q", cfg.Timezone.Mode)
	}
	if cfg.Monitoring.Enabled == nil || !*cfg.Monitoring.Enabled {
		t.Error("Expected monitoring enabled=true after apply")
	}
}
