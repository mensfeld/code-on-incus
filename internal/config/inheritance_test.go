package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileInheritanceScalarOverride(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Container: ContainerConfig{
			Image:      "parent-image",
			Persistent: ptrBool(true),
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Container: ContainerConfig{
			Image: "child-image",
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if child.Container.Image != "child-image" {
		t.Errorf("Expected child image 'child-image', got %q", child.Container.Image)
	}
	// Persistent should be inherited from parent
	if child.Container.Persistent == nil || !*child.Container.Persistent {
		t.Error("Expected persistent=true inherited from parent")
	}
}

func TestProfileInheritanceEnvironmentMerge(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Environment: map[string]string{
			"EDITOR":         "vim",
			"RUST_BACKTRACE": "1",
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Environment: map[string]string{
			"RUST_BACKTRACE": "full",
			"NEW_VAR":        "yes",
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if child.Environment["EDITOR"] != "vim" {
		t.Errorf("Expected EDITOR=vim inherited, got %q", child.Environment["EDITOR"])
	}
	if child.Environment["RUST_BACKTRACE"] != "full" {
		t.Errorf("Expected RUST_BACKTRACE=full (child override), got %q", child.Environment["RUST_BACKTRACE"])
	}
	if child.Environment["NEW_VAR"] != "yes" {
		t.Errorf("Expected NEW_VAR=yes, got %q", child.Environment["NEW_VAR"])
	}
}

func TestProfileInheritanceEnvironmentClear(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Environment: map[string]string{
			"SECRET_KEY": "abc123",
			"KEEP_ME":    "yes",
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Environment: map[string]string{
			"SECRET_KEY": "", // Clear inherited value
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if _, exists := child.Environment["SECRET_KEY"]; exists {
		t.Error("Expected SECRET_KEY to be cleared by empty string")
	}
	if child.Environment["KEEP_ME"] != "yes" {
		t.Errorf("Expected KEEP_ME=yes inherited, got %q", child.Environment["KEEP_ME"])
	}
}

func TestProfileInheritanceMountsReplace(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Mounts: []MountEntry{
			{Host: "~/.ssh", Container: "/home/code/.ssh"},
			{Host: "~/.cargo", Container: "/home/code/.cargo"},
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Mounts: []MountEntry{
			{Host: "~/.cargo", Container: "/home/code/.cargo"},
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if len(child.Mounts) != 1 {
		t.Fatalf("Expected 1 mount (child replaces parent), got %d", len(child.Mounts))
	}
	if child.Mounts[0].Host != "~/.cargo" {
		t.Errorf("Expected mount host '~/.cargo', got %q", child.Mounts[0].Host)
	}
}

func TestProfileInheritanceMountsInherited(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Mounts: []MountEntry{
			{Host: "~/.ssh", Container: "/home/code/.ssh"},
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Container: ContainerConfig{
			Image: "child-image",
		},
		// No mounts defined — should inherit parent's
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if len(child.Mounts) != 1 {
		t.Fatalf("Expected 1 mount inherited from parent, got %d", len(child.Mounts))
	}
	if child.Mounts[0].Host != "~/.ssh" {
		t.Errorf("Expected inherited mount host '~/.ssh', got %q", child.Mounts[0].Host)
	}
}

func TestProfileInheritanceForwardEnvReplace(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		ForwardEnv: []string{"SSH_AUTH_SOCK", "RUST_BACKTRACE"},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits:   "parent",
		ForwardEnv: []string{"API_KEY"},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if len(child.ForwardEnv) != 1 || child.ForwardEnv[0] != "API_KEY" {
		t.Errorf("Expected forward_env=[API_KEY] (child replaces), got %v", child.ForwardEnv)
	}
}

func TestProfileInheritanceForwardEnvInherited(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		ForwardEnv: []string{"SSH_AUTH_SOCK", "RUST_BACKTRACE"},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Container: ContainerConfig{
			Image: "child-image",
		},
		// No forward_env — should inherit parent's
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if len(child.ForwardEnv) != 2 {
		t.Fatalf("Expected 2 forward_env inherited from parent, got %d", len(child.ForwardEnv))
	}
}

func TestProfileInheritanceStructMerge(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Limits: &LimitsConfig{
			CPU: CPULimits{Count: "4"},
			Memory: MemoryLimits{
				Limit:   "2GiB",
				Enforce: "hard",
			},
		},
		Tool: &ToolConfig{
			Name:           "claude",
			PermissionMode: "bypass",
		},
		Network: &NetworkConfig{
			Mode: NetworkModeRestricted,
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Limits: &LimitsConfig{
			Memory: MemoryLimits{Limit: "4GiB"},
		},
		Tool: &ToolConfig{
			Name: "aider",
		},
		// No network — inherit parent's
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]

	// Limits: CPU count from parent, memory limit from child, enforce from parent
	if child.Limits.CPU.Count != "4" {
		t.Errorf("Expected CPU count '4' from parent, got %q", child.Limits.CPU.Count)
	}
	if child.Limits.Memory.Limit != "4GiB" {
		t.Errorf("Expected memory limit '4GiB' from child, got %q", child.Limits.Memory.Limit)
	}
	if child.Limits.Memory.Enforce != "hard" {
		t.Errorf("Expected memory enforce 'hard' from parent, got %q", child.Limits.Memory.Enforce)
	}

	// Tool: name from child, permission_mode from parent
	if child.Tool.Name != "aider" {
		t.Errorf("Expected tool name 'aider' from child, got %q", child.Tool.Name)
	}
	if child.Tool.PermissionMode != "bypass" {
		t.Errorf("Expected permission_mode 'bypass' from parent, got %q", child.Tool.PermissionMode)
	}

	// Network: inherited from parent (child didn't define it)
	if child.Network == nil || child.Network.Mode != NetworkModeRestricted {
		t.Error("Expected network mode 'restricted' inherited from parent")
	}
}

func TestProfileInheritanceChain(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["grandparent"] = ProfileConfig{
		Container: ContainerConfig{
			Image: "base-image",
		},
		ForwardEnv: []string{"SSH_AUTH_SOCK"},
		Environment: map[string]string{
			"LEVEL": "grandparent",
			"KEEP":  "yes",
		},
	}
	cfg.Profiles["parent"] = ProfileConfig{
		Inherits: "grandparent",
		Container: ContainerConfig{
			Image: "parent-image",
		},
		Environment: map[string]string{
			"LEVEL": "parent",
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Environment: map[string]string{
			"LEVEL": "child",
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	// Image from parent (not grandparent), since parent overrode it
	if child.Container.Image != "parent-image" {
		t.Errorf("Expected image 'parent-image', got %q", child.Container.Image)
	}
	// LEVEL from child
	if child.Environment["LEVEL"] != "child" {
		t.Errorf("Expected LEVEL=child, got %q", child.Environment["LEVEL"])
	}
	// KEEP from grandparent (through parent)
	if child.Environment["KEEP"] != "yes" {
		t.Errorf("Expected KEEP=yes from grandparent, got %q", child.Environment["KEEP"])
	}
	// ForwardEnv from grandparent (not overridden)
	if len(child.ForwardEnv) != 1 || child.ForwardEnv[0] != "SSH_AUTH_SOCK" {
		t.Errorf("Expected forward_env=[SSH_AUTH_SOCK] from grandparent, got %v", child.ForwardEnv)
	}
}

func TestProfileInheritanceCycleDetected(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["a"] = ProfileConfig{Inherits: "b"}
	cfg.Profiles["b"] = ProfileConfig{Inherits: "a"}

	err := cfg.ResolveProfileInheritance()
	if err == nil {
		t.Fatal("Expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("Expected error mentioning 'cycle', got: %v", err)
	}
}

func TestProfileInheritanceSelfCycle(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["self"] = ProfileConfig{Inherits: "self"}

	err := cfg.ResolveProfileInheritance()
	if err == nil {
		t.Fatal("Expected self-cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("Expected error mentioning 'cycle', got: %v", err)
	}
}

func TestProfileInheritanceMissingParent(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["child"] = ProfileConfig{Inherits: "nonexistent"}

	err := cfg.ResolveProfileInheritance()
	if err == nil {
		t.Fatal("Expected missing parent error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected error mentioning 'not found', got: %v", err)
	}
}

func TestProfileInheritanceCrossLevel(t *testing.T) {
	// Simulate a user-level profile and a project-level profile
	tmpDir := t.TempDir()

	// Create "user-level" profile dir
	userConfigDir := filepath.Join(tmpDir, "user-config")
	userProfileDir := filepath.Join(userConfigDir, "profiles", "base-rust")
	if err := os.MkdirAll(userProfileDir, 0o755); err != nil {
		t.Fatalf("Failed to create user profile dir: %v", err)
	}
	userProfileContent := `
forward_env = ["RUST_BACKTRACE"]

[container]
image = "coi-rust"

[environment]
EDITOR = "vim"
`
	if err := os.WriteFile(filepath.Join(userProfileDir, "config.toml"), []byte(userProfileContent), 0o644); err != nil {
		t.Fatalf("Failed to write user profile: %v", err)
	}

	// Create "project-level" profile dir
	projConfigDir := filepath.Join(tmpDir, "project", ".coi")
	projProfileDir := filepath.Join(projConfigDir, "profiles", "my-rust")
	if err := os.MkdirAll(projProfileDir, 0o755); err != nil {
		t.Fatalf("Failed to create project profile dir: %v", err)
	}
	projProfileContent := `
inherits = "base-rust"

[container]
image = "coi-rust-custom"

[environment]
MY_VAR = "hello"
`
	if err := os.WriteFile(filepath.Join(projProfileDir, "config.toml"), []byte(projProfileContent), 0o644); err != nil {
		t.Fatalf("Failed to write project profile: %v", err)
	}

	// Load profiles from both levels
	cfg := GetDefaultConfig()
	if err := loadProfileDirectories(cfg, userConfigDir); err != nil {
		t.Fatalf("loadProfileDirectories(user) failed: %v", err)
	}
	if err := loadProfileDirectories(cfg, projConfigDir); err != nil {
		t.Fatalf("loadProfileDirectories(project) failed: %v", err)
	}

	// Resolve inheritance
	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["my-rust"]
	if child.Container.Image != "coi-rust-custom" {
		t.Errorf("Expected image 'coi-rust-custom', got %q", child.Container.Image)
	}
	if child.Environment["EDITOR"] != "vim" {
		t.Errorf("Expected EDITOR=vim from parent, got %q", child.Environment["EDITOR"])
	}
	if child.Environment["MY_VAR"] != "hello" {
		t.Errorf("Expected MY_VAR=hello, got %q", child.Environment["MY_VAR"])
	}
	if len(child.ForwardEnv) != 1 || child.ForwardEnv[0] != "RUST_BACKTRACE" {
		t.Errorf("Expected forward_env from parent, got %v", child.ForwardEnv)
	}
}

func TestProfileInheritanceMaxDepth(t *testing.T) {
	cfg := GetDefaultConfig()

	// Create a chain of 12 profiles: p0 → p1 → ... → p11
	for i := 0; i <= 11; i++ {
		name := fmt.Sprintf("p%d", i)
		p := ProfileConfig{Container: ContainerConfig{Image: fmt.Sprintf("img-%d", i)}}
		if i > 0 {
			p.Inherits = fmt.Sprintf("p%d", i-1)
		}
		cfg.Profiles[name] = p
	}

	err := cfg.ResolveProfileInheritance()
	if err == nil {
		t.Fatal("Expected max depth error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum depth") {
		t.Errorf("Expected error mentioning 'maximum depth', got: %v", err)
	}
}

func TestProfileInheritanceSourcePreserved(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Container: ContainerConfig{Image: "parent-image"},
		Source:    "/home/user/.coi/profiles/parent/config.toml",
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Source:   "/home/user/.coi/profiles/child/config.toml",
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if child.Source != "/home/user/.coi/profiles/child/config.toml" {
		t.Errorf("Expected child source preserved, got %q", child.Source)
	}
}

func TestProfileInheritanceInheritsPreserved(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{Container: ContainerConfig{Image: "parent-image"}}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if child.Inherits != "parent" {
		t.Errorf("Expected Inherits preserved as 'parent' after resolution, got %q", child.Inherits)
	}
}

func TestProfileInheritanceBuildMerge(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Container: ContainerConfig{
			Build: BuildConfig{
				Base:   "coi",
				Script: "/path/to/build.sh",
			},
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Container: ContainerConfig{
			Build: BuildConfig{
				Script: "/path/to/child-build.sh",
			},
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	if child.Container.Build.Base != "coi" {
		t.Errorf("Expected build base 'coi' from parent, got %q", child.Container.Build.Base)
	}
	if child.Container.Build.Script != "/path/to/child-build.sh" {
		t.Errorf("Expected build script from child, got %q", child.Container.Build.Script)
	}
}

func TestProfileInheritanceNoInherits(t *testing.T) {
	// Profiles without inherits should not be affected
	cfg := GetDefaultConfig()
	cfg.Profiles["standalone"] = ProfileConfig{
		Container: ContainerConfig{Image: "my-image"},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	p := cfg.Profiles["standalone"]
	if p.Container.Image != "my-image" {
		t.Errorf("Expected image 'my-image', got %q", p.Container.Image)
	}
}

func TestProfileInheritanceSecurityPathsMerge(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Security: &SecurityConfig{
			AdditionalProtectedPaths: []string{"/parent/secret"},
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Security: &SecurityConfig{
			AdditionalProtectedPaths: []string{"/child/secret"},
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	paths := child.Security.AdditionalProtectedPaths
	// Should contain BOTH parent and child paths (additive merge)
	hasParent, hasChild := false, false
	for _, p := range paths {
		if p == "/parent/secret" {
			hasParent = true
		}
		if p == "/child/secret" {
			hasChild = true
		}
	}
	if !hasParent || !hasChild {
		t.Errorf("Expected both /parent/secret and /child/secret, got %v", paths)
	}
}

func TestProfileInheritanceSecurityPathsDedup(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Security: &SecurityConfig{
			AdditionalProtectedPaths: []string{"/shared/path", "/parent/only"},
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits: "parent",
		Security: &SecurityConfig{
			AdditionalProtectedPaths: []string{"/shared/path", "/child/only"},
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	child := cfg.Profiles["child"]
	paths := child.Security.AdditionalProtectedPaths
	// /shared/path should appear only once
	count := 0
	for _, p := range paths {
		if p == "/shared/path" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected /shared/path exactly once, got %d times in %v", count, paths)
	}
	if len(paths) != 3 {
		t.Errorf("Expected 3 unique paths, got %d: %v", len(paths), paths)
	}
}

func TestSynthesizeDefaultProfileIsolation(t *testing.T) {
	cfg := GetDefaultConfig()

	// Record original values
	origCPUCount := cfg.Limits.CPU.Count
	origToolName := cfg.Tool.Name
	origNetMode := cfg.Network.Mode
	origSecurityDisable := cfg.Security.DisableProtection

	// Synthesize default profile
	profile := synthesizeDefaultProfile(cfg)

	// Mutate the profile's struct pointers
	profile.Limits.CPU.Count = "99"
	profile.Tool.Name = "mutated-tool"
	profile.Network.Mode = "mutated-mode"
	profile.Security.DisableProtection = true

	// Original config must NOT be affected
	if cfg.Limits.CPU.Count != origCPUCount {
		t.Errorf("Config.Limits.CPU.Count mutated: expected %q, got %q", origCPUCount, cfg.Limits.CPU.Count)
	}
	if cfg.Tool.Name != origToolName {
		t.Errorf("Config.Tool.Name mutated: expected %q, got %q", origToolName, cfg.Tool.Name)
	}
	if cfg.Network.Mode != origNetMode {
		t.Errorf("Config.Network.Mode mutated: expected %q, got %q", origNetMode, cfg.Network.Mode)
	}
	if cfg.Security.DisableProtection != origSecurityDisable {
		t.Errorf("Config.Security.DisableProtection mutated: expected %v, got %v", origSecurityDisable, cfg.Security.DisableProtection)
	}
}

func TestSynthesizeDefaultProfileSliceIsolation(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Defaults.ForwardEnv = []string{"FOO", "BAR"}
	cfg.Mounts.Default = []MountEntry{{Host: "/src", Container: "/dst"}}
	cfg.Security.AdditionalProtectedPaths = []string{"/secret"}

	profile := synthesizeDefaultProfile(cfg)

	// Mutate existing elements to detect shared backing arrays
	profile.ForwardEnv[0] = "MUTATED"
	profile.Mounts[0].Host = "/mutated"
	profile.Security.AdditionalProtectedPaths[0] = "/evil"

	// Original config elements must NOT be affected
	if cfg.Defaults.ForwardEnv[0] != "FOO" {
		t.Errorf("Config.Defaults.ForwardEnv[0] mutated: expected %q, got %q", "FOO", cfg.Defaults.ForwardEnv[0])
	}
	if cfg.Mounts.Default[0].Host != "/src" {
		t.Errorf("Config.Mounts.Default[0].Host mutated: expected %q, got %q", "/src", cfg.Mounts.Default[0].Host)
	}
	if cfg.Security.AdditionalProtectedPaths[0] != "/secret" {
		t.Errorf("Config.Security.AdditionalProtectedPaths[0] mutated: expected %q, got %q", "/secret", cfg.Security.AdditionalProtectedPaths[0])
	}

	// Appending should also not affect the original slices
	profile.ForwardEnv = append(profile.ForwardEnv, "EXTRA")
	profile.Mounts = append(profile.Mounts, MountEntry{Host: "/new", Container: "/new"})
	profile.Security.AdditionalProtectedPaths = append(profile.Security.AdditionalProtectedPaths, "/extra")

	if len(cfg.Defaults.ForwardEnv) != 2 {
		t.Errorf("Config.Defaults.ForwardEnv length mutated: expected 2 items, got %d", len(cfg.Defaults.ForwardEnv))
	}
	if len(cfg.Mounts.Default) != 1 {
		t.Errorf("Config.Mounts.Default length mutated: expected 1 item, got %d", len(cfg.Mounts.Default))
	}
	if len(cfg.Security.AdditionalProtectedPaths) != 1 {
		t.Errorf("Config.Security.AdditionalProtectedPaths length mutated: expected 1 item, got %d", len(cfg.Security.AdditionalProtectedPaths))
	}
}

func TestProfileInheritanceParentUnchanged(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Profiles["parent"] = ProfileConfig{
		Container: ContainerConfig{Image: "parent-image"},
		Environment: map[string]string{
			"PARENT_VAR": "value",
		},
	}
	cfg.Profiles["child"] = ProfileConfig{
		Inherits:  "parent",
		Container: ContainerConfig{Image: "child-image"},
		Environment: map[string]string{
			"CHILD_VAR": "value",
		},
	}

	if err := cfg.ResolveProfileInheritance(); err != nil {
		t.Fatalf("ResolveProfileInheritance() failed: %v", err)
	}

	parent := cfg.Profiles["parent"]
	if parent.Container.Image != "parent-image" {
		t.Errorf("Expected parent image unchanged, got %q", parent.Container.Image)
	}
	if _, exists := parent.Environment["CHILD_VAR"]; exists {
		t.Error("Parent should not have child's environment variable")
	}
}
