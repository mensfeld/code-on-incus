package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeToolBasics(t *testing.T) {
	tool := NewClaude()

	if tool.Name() != "claude" {
		t.Errorf("Expected name 'claude', got '%s'", tool.Name())
	}

	if tool.Binary() != "claude" {
		t.Errorf("Expected binary 'claude', got '%s'", tool.Binary())
	}

	if tool.ConfigDirName() != ".claude" {
		t.Errorf("Expected config dir '.claude', got '%s'", tool.ConfigDirName())
	}

	if tool.SessionsDirName() != "sessions-claude" {
		t.Errorf("Expected sessions dir 'sessions-claude', got '%s'", tool.SessionsDirName())
	}
}

func TestClaudeBuildCommand_NewSession(t *testing.T) {
	tool := NewClaude()
	sessionID := "test-session-123"

	cmd := tool.BuildCommand(sessionID, false, "")

	expected := []string{"claude", "--verbose", "--permission-mode", "bypassPermissions", "--session-id", "test-session-123"}

	if len(cmd) != len(expected) {
		t.Fatalf("Expected %d args, got %d: %v", len(expected), len(cmd), cmd)
	}

	for i, arg := range expected {
		if cmd[i] != arg {
			t.Errorf("Arg[%d]: expected '%s', got '%s'", i, arg, cmd[i])
		}
	}
}

func TestClaudeBuildCommand_ResumeWithID(t *testing.T) {
	tool := NewClaude()
	resumeSessionID := "cli-session-456"

	cmd := tool.BuildCommand("", true, resumeSessionID)

	// Should contain --resume with the session ID
	if !contains(cmd, "--resume") {
		t.Errorf("Expected command to contain '--resume', got: %v", cmd)
	}

	if !contains(cmd, resumeSessionID) {
		t.Errorf("Expected command to contain '%s', got: %v", resumeSessionID, cmd)
	}

	// Should still have permission flags
	if !contains(cmd, "--permission-mode") {
		t.Errorf("Expected command to contain '--permission-mode', got: %v", cmd)
	}

	if !contains(cmd, "bypassPermissions") {
		t.Errorf("Expected command to contain 'bypassPermissions', got: %v", cmd)
	}
}

func TestClaudeBuildCommand_ResumeWithoutID(t *testing.T) {
	tool := NewClaude()

	cmd := tool.BuildCommand("", true, "")

	// Should contain --resume without a specific ID
	if !contains(cmd, "--resume") {
		t.Errorf("Expected command to contain '--resume', got: %v", cmd)
	}

	// Should have exactly one --resume (not followed by an ID)
	resumeIdx := indexOf(cmd, "--resume")
	if resumeIdx == -1 {
		t.Fatal("--resume not found in command")
	}

	// If there's a next arg, it should be a flag (starting with -) not a session ID
	if resumeIdx+1 < len(cmd) {
		nextArg := cmd[resumeIdx+1]
		if !strings.HasPrefix(nextArg, "-") && nextArg != "claude" {
			t.Errorf("Expected --resume to be standalone, but next arg is '%s'", nextArg)
		}
	}
}

func TestClaudeDiscoverSessionID_ValidSession(t *testing.T) {
	tool := NewClaude()

	// Create temporary directory structure
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects", "-workspace")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create a .jsonl file (Claude session file)
	sessionID := "test-session-abc123"
	sessionFile := filepath.Join(projectsDir, sessionID+".jsonl")
	if err := os.WriteFile(sessionFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}

	// Test discovery
	discovered := tool.DiscoverSessionID(tmpDir)
	if discovered != sessionID {
		t.Errorf("Expected session ID '%s', got '%s'", sessionID, discovered)
	}
}

func TestClaudeDiscoverSessionID_NoSession(t *testing.T) {
	tool := NewClaude()

	// Create empty directory structure
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects", "-workspace")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Test discovery with no session files
	discovered := tool.DiscoverSessionID(tmpDir)
	if discovered != "" {
		t.Errorf("Expected empty session ID, got '%s'", discovered)
	}
}

func TestClaudeDiscoverSessionID_NonExistentDir(t *testing.T) {
	tool := NewClaude()

	// Test discovery with non-existent directory
	discovered := tool.DiscoverSessionID("/nonexistent/path")
	if discovered != "" {
		t.Errorf("Expected empty session ID for non-existent path, got '%s'", discovered)
	}
}

func TestClaudeGetSandboxSettings(t *testing.T) {
	tool := NewClaude()

	settings := tool.GetSandboxSettings()

	// Check required settings
	if settings["allowDangerouslySkipPermissions"] != true {
		t.Error("Expected allowDangerouslySkipPermissions to be true")
	}

	if settings["bypassPermissionsModeAccepted"] != true {
		t.Error("Expected bypassPermissionsModeAccepted to be true")
	}

	// Check permissions map
	permissions, ok := settings["permissions"].(map[string]string)
	if !ok {
		t.Fatal("Expected permissions to be map[string]string")
	}

	if permissions["defaultMode"] != "bypassPermissions" {
		t.Errorf("Expected defaultMode 'bypassPermissions', got '%s'", permissions["defaultMode"])
	}

	// Check effortLevel defaults to "medium"
	if settings["effortLevel"] != "medium" {
		t.Errorf("Expected default effortLevel 'medium', got '%v'", settings["effortLevel"])
	}

	// Check effort acceptance flags
	if settings["effortLevelAccepted"] != true {
		t.Error("Expected effortLevelAccepted to be true")
	}
	if settings["hasSeenEffortPrompt"] != true {
		t.Error("Expected hasSeenEffortPrompt to be true")
	}
	if settings["effortCalloutDismissed"] != true {
		t.Error("Expected effortCalloutDismissed to be true")
	}

	// Check env section has CLAUDE_CODE_EFFORT_LEVEL
	env, ok := settings["env"].(map[string]string)
	if !ok {
		t.Fatal("Expected env to be map[string]string")
	}
	if env["CLAUDE_CODE_EFFORT_LEVEL"] != "medium" {
		t.Errorf("Expected CLAUDE_CODE_EFFORT_LEVEL 'medium', got '%s'", env["CLAUDE_CODE_EFFORT_LEVEL"])
	}
}

func TestClaudeSetEffortLevel(t *testing.T) {
	tool := NewClaude()

	// Cast to ToolWithEffortLevel
	twel, ok := tool.(ToolWithEffortLevel)
	if !ok {
		t.Fatal("Claude tool should implement ToolWithEffortLevel")
	}

	// Test setting to "high"
	twel.SetEffortLevel("high")
	settings := tool.GetSandboxSettings()
	if settings["effortLevel"] != "high" {
		t.Errorf("Expected effortLevel 'high', got '%v'", settings["effortLevel"])
	}

	// Test setting to "low"
	twel.SetEffortLevel("low")
	settings = tool.GetSandboxSettings()
	if settings["effortLevel"] != "low" {
		t.Errorf("Expected effortLevel 'low', got '%v'", settings["effortLevel"])
	}

	// Test setting to "medium"
	twel.SetEffortLevel("medium")
	settings = tool.GetSandboxSettings()
	if settings["effortLevel"] != "medium" {
		t.Errorf("Expected effortLevel 'medium', got '%v'", settings["effortLevel"])
	}
}

func TestClaudeEffortLevelDefault(t *testing.T) {
	// When effortLevel is not set, GetSandboxSettings should default to "medium"
	tool := NewClaude()
	settings := tool.GetSandboxSettings()

	if settings["effortLevel"] != "medium" {
		t.Errorf("Expected default effortLevel 'medium', got '%v'", settings["effortLevel"])
	}
}

func TestClaudeToolConfigDirFiles(t *testing.T) {
	tool := NewClaude()
	tcf, ok := tool.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("ClaudeTool does not implement ToolWithConfigDirFiles")
	}

	// EssentialConfigFiles
	files := tcf.EssentialConfigFiles()
	expected := []string{".credentials.json", "config.yml", "settings.json", "CLAUDE.md"}
	if len(files) != len(expected) {
		t.Fatalf("EssentialConfigFiles() = %v, want %v", files, expected)
	}
	for i, f := range files {
		if f != expected[i] {
			t.Errorf("EssentialConfigFiles()[%d] = %q, want %q", i, f, expected[i])
		}
	}

	// SandboxSettingsFileName
	if tcf.SandboxSettingsFileName() != "settings.json" {
		t.Errorf("SandboxSettingsFileName() = %q, want %q", tcf.SandboxSettingsFileName(), "settings.json")
	}

	// StateConfigFileName
	if tcf.StateConfigFileName() != ".claude.json" {
		t.Errorf("StateConfigFileName() = %q, want %q", tcf.StateConfigFileName(), ".claude.json")
	}

	// AlwaysSetupConfig
	if tcf.AlwaysSetupConfig() {
		t.Error("AlwaysSetupConfig() = true, want false")
	}
}

func TestRegistryGet_Claude(t *testing.T) {
	tool, err := Get("claude")
	if err != nil {
		t.Fatalf("Expected to get claude tool, got error: %v", err)
	}

	if tool.Name() != "claude" {
		t.Errorf("Expected tool name 'claude', got '%s'", tool.Name())
	}
}

func TestRegistryGet_Unknown(t *testing.T) {
	_, err := Get("unknown-tool")
	if err == nil {
		t.Error("Expected error for unknown tool, got nil")
	}

	expectedMsg := "unknown tool"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}
}

func TestRegistryGetDefault(t *testing.T) {
	tool := GetDefault()

	if tool == nil {
		t.Fatal("Expected tool, got nil")
	}

	if tool.Name() != "claude" {
		t.Errorf("Expected default tool to be 'claude', got '%s'", tool.Name())
	}
}

func TestClaudeSetPermissionMode_Interface(t *testing.T) {
	tool := NewClaude()

	// Verify ClaudeTool implements ToolWithPermissionMode
	twpm, ok := tool.(ToolWithPermissionMode)
	if !ok {
		t.Fatal("Claude tool should implement ToolWithPermissionMode")
	}

	// Verify the method works without panic
	twpm.SetPermissionMode("interactive")
}

func TestClaudeBuildCommand_InteractiveMode(t *testing.T) {
	ct := &ClaudeTool{permissionMode: "interactive"}
	sessionID := "test-session-123"

	cmd := ct.BuildCommand(sessionID, false, "")

	// Should have --verbose and --session-id but NOT --permission-mode/bypassPermissions
	if !contains(cmd, "--verbose") {
		t.Error("Expected command to contain '--verbose'")
	}
	if !contains(cmd, "--session-id") {
		t.Error("Expected command to contain '--session-id'")
	}
	if !contains(cmd, sessionID) {
		t.Errorf("Expected command to contain '%s'", sessionID)
	}
	if contains(cmd, "--permission-mode") {
		t.Error("Expected command NOT to contain '--permission-mode' in interactive mode")
	}
	if contains(cmd, "bypassPermissions") {
		t.Error("Expected command NOT to contain 'bypassPermissions' in interactive mode")
	}
}

func TestClaudeBuildCommand_BypassModeExplicit(t *testing.T) {
	ct := &ClaudeTool{permissionMode: "bypass"}
	sessionID := "test-session-123"

	cmd := ct.BuildCommand(sessionID, false, "")

	// Explicit "bypass" should behave like default (include --permission-mode bypassPermissions)
	if !contains(cmd, "--permission-mode") {
		t.Error("Expected command to contain '--permission-mode' in bypass mode")
	}
	if !contains(cmd, "bypassPermissions") {
		t.Error("Expected command to contain 'bypassPermissions' in bypass mode")
	}
}

func TestClaudeGetSandboxSettings_InteractiveMode(t *testing.T) {
	ct := &ClaudeTool{permissionMode: "interactive"}

	settings := ct.GetSandboxSettings()

	// Should NOT have bypass permission keys
	if _, ok := settings["allowDangerouslySkipPermissions"]; ok {
		t.Error("Expected no 'allowDangerouslySkipPermissions' key in interactive mode")
	}
	if _, ok := settings["bypassPermissionsModeAccepted"]; ok {
		t.Error("Expected no 'bypassPermissionsModeAccepted' key in interactive mode")
	}
	if _, ok := settings["permissions"]; ok {
		t.Error("Expected no 'permissions' key in interactive mode")
	}

	// Should still have effort level keys
	if settings["effortLevel"] != "medium" {
		t.Errorf("Expected effortLevel 'medium', got '%v'", settings["effortLevel"])
	}
	if settings["effortLevelAccepted"] != true {
		t.Error("Expected effortLevelAccepted to be true")
	}
	if settings["hasSeenEffortPrompt"] != true {
		t.Error("Expected hasSeenEffortPrompt to be true")
	}
	if settings["effortCalloutDismissed"] != true {
		t.Error("Expected effortCalloutDismissed to be true")
	}

	env, ok := settings["env"].(map[string]string)
	if !ok {
		t.Fatal("Expected env to be map[string]string")
	}
	if env["CLAUDE_CODE_EFFORT_LEVEL"] != "medium" {
		t.Errorf("Expected CLAUDE_CODE_EFFORT_LEVEL 'medium', got '%s'", env["CLAUDE_CODE_EFFORT_LEVEL"])
	}
}

func TestClaudeGetSandboxSettings_BypassModeDefault(t *testing.T) {
	ct := &ClaudeTool{} // Empty permissionMode = default bypass

	settings := ct.GetSandboxSettings()

	// Should have bypass permission keys (default behavior)
	if settings["allowDangerouslySkipPermissions"] != true {
		t.Error("Expected allowDangerouslySkipPermissions to be true")
	}
	if settings["bypassPermissionsModeAccepted"] != true {
		t.Error("Expected bypassPermissionsModeAccepted to be true")
	}

	permissions, ok := settings["permissions"].(map[string]string)
	if !ok {
		t.Fatal("Expected permissions to be map[string]string")
	}
	if permissions["defaultMode"] != "bypassPermissions" {
		t.Errorf("Expected defaultMode 'bypassPermissions', got '%s'", permissions["defaultMode"])
	}

	// Should also have effort level keys
	if settings["effortLevel"] != "medium" {
		t.Errorf("Expected effortLevel 'medium', got '%v'", settings["effortLevel"])
	}
}

func TestClaudeBuildCommand_ResumeInteractiveMode(t *testing.T) {
	ct := &ClaudeTool{permissionMode: "interactive"}
	resumeSessionID := "cli-session-456"

	cmd := ct.BuildCommand("", true, resumeSessionID)

	// Should have --resume with session ID
	if !contains(cmd, "--resume") {
		t.Error("Expected command to contain '--resume'")
	}
	if !contains(cmd, resumeSessionID) {
		t.Errorf("Expected command to contain '%s'", resumeSessionID)
	}

	// Should NOT have permission flags
	if contains(cmd, "--permission-mode") {
		t.Error("Expected command NOT to contain '--permission-mode' in interactive mode")
	}
	if contains(cmd, "bypassPermissions") {
		t.Error("Expected command NOT to contain 'bypassPermissions' in interactive mode")
	}

	// Should still have --verbose
	if !contains(cmd, "--verbose") {
		t.Error("Expected command to contain '--verbose'")
	}
}

func TestClaudeTool_AutoContextFile(t *testing.T) {
	tool := NewClaude()
	acf, ok := tool.(ToolWithAutoContextFile)
	if !ok {
		t.Fatal("ClaudeTool should implement ToolWithAutoContextFile")
	}

	path := acf.AutoContextFile()
	if path != ".claude/CLAUDE.md" {
		t.Errorf("AutoContextFile() = %q, want %q", path, ".claude/CLAUDE.md")
	}
}

func TestClaudeTool_EssentialConfigFiles_IncludesCLAUDEMD(t *testing.T) {
	tool := NewClaude()
	tcf, ok := tool.(ToolWithConfigDirFiles)
	if !ok {
		t.Fatal("ClaudeTool does not implement ToolWithConfigDirFiles")
	}

	files := tcf.EssentialConfigFiles()
	found := false
	for _, f := range files {
		if f == "CLAUDE.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("EssentialConfigFiles() = %v, does not include 'CLAUDE.md'", files)
	}
}

func TestRenderContextFileContent(t *testing.T) {
	info := ContextInfo{
		WorkspacePath:     "/workspace",
		HomeDir:           "/home/code",
		Persistent:        false,
		NetworkMode:       "restricted",
		SSHAgentForwarded: true,
		RunAsRoot:         false,
		Architecture:      "amd64",
		ProtectedPaths:    []string{".git/hooks", ".vscode"},
	}

	content := RenderContextFileContent(info)

	// Check that key sections are present
	checks := []struct {
		name   string
		substr string
	}{
		{"workspace path", "/workspace"},
		{"home dir", "/home/code"},
		{"ephemeral mode", "Ephemeral"},
		{"restricted network", "Restricted"},
		{"network limitation", "local/private networks are blocked"},
		{"ssh forwarded", "Forwarded from host"},
		{"non-root user", "Non-root user"},
		{"COI header", "COI Sandbox Environment"},
		{"full root access", "Full root access"},
		{"docker available", "Docker is available"},
		{"OS info", "Ubuntu"},
		{"architecture", "amd64"},
		{"docker row", "Docker-in-Docker"},
		{"troubleshooting section", "Troubleshooting"},
		{"limitations section", "Limitations"},
		{"protected paths", ".git/hooks, .vscode"},
		{"ephemeral warning", "System-level changes"},
	}

	for _, check := range checks {
		if !strings.Contains(content, check.substr) {
			t.Errorf("RenderContextFileContent() missing %s (expected substring %q)", check.name, check.substr)
		}
	}
}

func TestRenderContextFileContent_Persistent(t *testing.T) {
	info := ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       "/root",
		Persistent:    true,
		NetworkMode:   "open",
		RunAsRoot:     true,
	}

	content := RenderContextFileContent(info)

	if !strings.Contains(content, "Persistent") {
		t.Error("Expected 'Persistent' in content for persistent mode")
	}
	if !strings.Contains(content, "Open") {
		t.Error("Expected 'Open' in content for open network mode")
	}
	if !strings.Contains(content, "Root user") {
		t.Error("Expected 'Root user' in content for root mode")
	}
	// Persistent mode should NOT warn about system-level changes being lost
	if strings.Contains(content, "System-level changes") {
		t.Error("Persistent mode should not contain ephemeral warning about system-level changes")
	}
}

func TestRenderContextFileContent_AllNetworkModes(t *testing.T) {
	tests := []struct {
		mode              string
		expected          string
		hasLimitation     bool
		limitationContain string
	}{
		{"restricted", "Restricted", true, "local/private networks are blocked"},
		{"open", "Open", false, ""},
		{"allowlist", "Allowlist", true, "pre-approved domains"},
		{"", "Default", false, ""},
	}

	for _, tt := range tests {
		info := ContextInfo{
			WorkspacePath: "/workspace",
			HomeDir:       "/home/code",
			NetworkMode:   tt.mode,
		}
		content := RenderContextFileContent(info)
		if !strings.Contains(content, tt.expected) {
			t.Errorf("NetworkMode %q: expected content to contain %q", tt.mode, tt.expected)
		}
		if tt.hasLimitation {
			if !strings.Contains(content, tt.limitationContain) {
				t.Errorf("NetworkMode %q: expected limitation containing %q", tt.mode, tt.limitationContain)
			}
		}
	}
}

func TestRenderContextFileContent_NoProtectedPaths(t *testing.T) {
	info := ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       "/home/code",
	}
	content := RenderContextFileContent(info)
	if strings.Contains(content, "Protected paths") {
		t.Error("Should not contain protected paths section when none configured")
	}
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
