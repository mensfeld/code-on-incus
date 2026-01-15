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
