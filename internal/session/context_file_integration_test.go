package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// skipUnlessContextFileTestable skips the test if integration prerequisites are missing.
func skipUnlessContextFileTestable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}
	exists, err := container.ImageExists("coi")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}
}

// launchContextTestContainer creates and starts a container, registering cleanup.
func launchContextTestContainer(t *testing.T, name string) *container.Manager {
	t.Helper()
	mgr := container.NewManager(name)

	t.Cleanup(func() {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	})

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}

	if err := mgr.Launch("coi", false); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}

	// Wait for container to be ready
	for i := 0; i < 30; i++ {
		if _, err := mgr.ExecCommand("echo ready", container.ExecCommandOptions{Capture: true}); err == nil {
			return mgr
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("Container failed to become ready")
	return nil
}

// TestContextFile_DefaultInjection verifies that ~/SANDBOX_CONTEXT.md is created
// inside the container with default template content when no custom file is provided.
func TestContextFile_DefaultInjection(t *testing.T) {
	skipUnlessContextFileTestable(t)

	containerName := "coi-test-ctx-default"
	mgr := launchContextTestContainer(t, containerName)

	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[context] %s", msg) }

	ctxInfo := tool.ContextInfo{
		WorkspacePath:     "/workspace",
		HomeDir:           homeDir,
		Persistent:        false,
		NetworkMode:       "restricted",
		SSHAgentForwarded: false,
		RunAsRoot:         false,
	}

	// Inject the context file
	err := injectContextFile(mgr, ctxInfo, "", homeDir, logger)
	if err != nil {
		t.Fatalf("injectContextFile failed: %v", err)
	}

	// Verify file exists
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")
	exists, err := mgr.FileExists(destPath)
	if err != nil {
		t.Fatalf("Failed to check if context file exists: %v", err)
	}
	if !exists {
		t.Fatalf("Expected %s to exist in container", destPath)
	}

	// Read the file and verify content
	user := container.CodeUID
	content, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read context file: %v", err)
	}

	// Verify key template values were rendered
	checks := []struct {
		name   string
		substr string
	}{
		{"header", "COI Sandbox Environment"},
		{"workspace path", "/workspace"},
		{"home dir", homeDir},
		{"ephemeral mode", "Ephemeral"},
		{"restricted network", "Restricted"},
		{"ssh not available", "Not available"},
		{"github cli not auth", "Not authenticated"},
		{"non-root user", "Non-root user"},
		{"OS info", "Ubuntu"},
		{"mise section", "Runtime Manager (mise)"},
		{"python in mise table", "Python 3"},
		{"pnpm in mise table", "pnpm"},
		{"troubleshooting section", "Troubleshooting"},
		{"limitations section", "Limitations"},
	}

	for _, check := range checks {
		if !strings.Contains(content, check.substr) {
			t.Errorf("Context file missing %s (expected substring %q)", check.name, check.substr)
		}
	}

	// Verify ownership (should be owned by code user, not root)
	statOut, err := mgr.ExecCommand("stat -c '%u:%g' "+destPath, container.ExecCommandOptions{Capture: true})
	if err != nil {
		t.Fatalf("Failed to stat context file: %v", err)
	}
	expectedOwnership := strings.TrimSpace(statOut)
	if expectedOwnership != "1000:1000" {
		t.Errorf("Expected ownership 1000:1000, got %s", expectedOwnership)
	}

	t.Logf("Context file content:\n%s", content)
}

// TestContextFile_CustomFile verifies that a user-provided custom context file
// is used instead of the default template when context_file is set in config.
func TestContextFile_CustomFile(t *testing.T) {
	skipUnlessContextFileTestable(t)

	containerName := "coi-test-ctx-custom"
	mgr := launchContextTestContainer(t, containerName)

	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[context] %s", msg) }

	// Create a custom context file on the host
	customContent := "# My Custom Sandbox\n\nThis is a custom context file for testing.\nWorkspace: /my/project\n"
	customFile := filepath.Join(t.TempDir(), "custom-context.md")
	if err := os.WriteFile(customFile, []byte(customContent), 0o644); err != nil {
		t.Fatalf("Failed to write custom context file: %v", err)
	}

	ctxInfo := tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
	}

	// Inject with custom file path
	err := injectContextFile(mgr, ctxInfo, customFile, homeDir, logger)
	if err != nil {
		t.Fatalf("injectContextFile with custom file failed: %v", err)
	}

	// Verify file exists
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")
	exists, err := mgr.FileExists(destPath)
	if err != nil {
		t.Fatalf("Failed to check if context file exists: %v", err)
	}
	if !exists {
		t.Fatalf("Expected %s to exist in container", destPath)
	}

	// Read and verify it's the custom content, not the default template
	user := container.CodeUID
	content, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read context file: %v", err)
	}

	if !strings.Contains(content, "My Custom Sandbox") {
		t.Errorf("Expected custom content, got:\n%s", content)
	}
	if !strings.Contains(content, "custom context file for testing") {
		t.Errorf("Expected custom content body, got:\n%s", content)
	}

	// Should NOT contain default template content
	if strings.Contains(content, "COI Sandbox Environment") {
		t.Error("Custom file should not contain default template header")
	}

	t.Logf("Custom context file content:\n%s", content)
}

// TestContextFile_CustomFileNotFound verifies that injectContextFile returns
// an error when the custom file path doesn't exist.
func TestContextFile_CustomFileNotFound(t *testing.T) {
	skipUnlessContextFileTestable(t)

	containerName := "coi-test-ctx-notfound"
	mgr := launchContextTestContainer(t, containerName)

	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[context] %s", msg) }

	ctxInfo := tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
	}

	// Try to inject with non-existent file
	err := injectContextFile(mgr, ctxInfo, "/tmp/nonexistent-context-file-12345.md", homeDir, logger)
	if err == nil {
		t.Fatal("Expected error when custom file doesn't exist, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read custom context file") {
		t.Errorf("Expected 'failed to read custom context file' error, got: %v", err)
	}
}

// TestContextFile_Regeneration verifies that the context file is regenerated
// on each call (important for resumed sessions with updated dynamic info).
func TestContextFile_Regeneration(t *testing.T) {
	skipUnlessContextFileTestable(t)

	containerName := "coi-test-ctx-regen"
	mgr := launchContextTestContainer(t, containerName)

	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[context] %s", msg) }
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")
	user := container.CodeUID

	// First injection: ephemeral, restricted
	ctxInfo1 := tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
		Persistent:    false,
		NetworkMode:   "restricted",
	}
	if err := injectContextFile(mgr, ctxInfo1, "", homeDir, logger); err != nil {
		t.Fatalf("First injectContextFile failed: %v", err)
	}

	content1, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read first context file: %v", err)
	}
	if !strings.Contains(content1, "Ephemeral") {
		t.Error("First injection should contain 'Ephemeral'")
	}
	if !strings.Contains(content1, "Restricted") {
		t.Error("First injection should contain 'Restricted'")
	}

	// Second injection: persistent, open (simulating resumed session with changed config)
	ctxInfo2 := tool.ContextInfo{
		WorkspacePath:     "/workspace",
		HomeDir:           homeDir,
		Persistent:        true,
		NetworkMode:       "open",
		SSHAgentForwarded: true,
	}
	if err := injectContextFile(mgr, ctxInfo2, "", homeDir, logger); err != nil {
		t.Fatalf("Second injectContextFile failed: %v", err)
	}

	content2, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read second context file: %v", err)
	}
	if !strings.Contains(content2, "Persistent") {
		t.Error("Second injection should contain 'Persistent'")
	}
	if !strings.Contains(content2, "Open") {
		t.Error("Second injection should contain 'Open'")
	}
	if !strings.Contains(content2, "Forwarded from host") {
		t.Error("Second injection should contain 'Forwarded from host'")
	}

	// Should no longer contain the old values
	if strings.Contains(content2, "Ephemeral") {
		t.Error("Second injection should not contain 'Ephemeral' from first run")
	}
}

// TestContextFile_GitHubCLIAuthenticated verifies that the context file reflects
// GitHub CLI authentication status when GH_TOKEN or GITHUB_TOKEN is forwarded.
func TestContextFile_GitHubCLIAuthenticated(t *testing.T) {
	skipUnlessContextFileTestable(t)

	containerName := "coi-test-ctx-ghauth"
	mgr := launchContextTestContainer(t, containerName)

	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[context] %s", msg) }
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")
	user := container.CodeUID

	// Test with GH CLI authenticated
	ctxInfo := tool.ContextInfo{
		WorkspacePath:      "/workspace",
		HomeDir:            homeDir,
		GHCLIAuthenticated: true,
	}
	if err := injectContextFile(mgr, ctxInfo, "", homeDir, logger); err != nil {
		t.Fatalf("injectContextFile failed: %v", err)
	}

	content, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read context file: %v", err)
	}

	if !strings.Contains(content, "Authenticated via forwarded token") {
		t.Error("Context file should indicate GitHub CLI is authenticated")
	}
	if strings.Contains(content, "Not authenticated") {
		t.Error("Context file should not say 'Not authenticated' when GH CLI is authenticated")
	}

	// Test without GH CLI authenticated
	ctxInfo2 := tool.ContextInfo{
		WorkspacePath:      "/workspace",
		HomeDir:            homeDir,
		GHCLIAuthenticated: false,
	}
	if err := injectContextFile(mgr, ctxInfo2, "", homeDir, logger); err != nil {
		t.Fatalf("injectContextFile (unauthenticated) failed: %v", err)
	}

	content2, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read context file: %v", err)
	}

	if !strings.Contains(content2, "Not authenticated") {
		t.Error("Context file should indicate GitHub CLI is not authenticated")
	}

	t.Logf("Authenticated context file content:\n%s", content)
}

// TestContextFile_ForwardedEnvVars verifies that forwarded environment variable
// names appear in the context file when provided.
func TestContextFile_ForwardedEnvVars(t *testing.T) {
	skipUnlessContextFileTestable(t)

	containerName := "coi-test-ctx-envvars"
	mgr := launchContextTestContainer(t, containerName)

	homeDir := "/home/" + container.CodeUser
	logger := func(msg string) { t.Logf("[context] %s", msg) }
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")
	user := container.CodeUID

	// Test with forwarded env vars
	ctxInfo := tool.ContextInfo{
		WorkspacePath:    "/workspace",
		HomeDir:          homeDir,
		ForwardedEnvVars: []string{"ANTHROPIC_API_KEY", "GH_TOKEN", "CUSTOM_VAR"},
	}
	if err := injectContextFile(mgr, ctxInfo, "", homeDir, logger); err != nil {
		t.Fatalf("injectContextFile failed: %v", err)
	}

	content, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read context file: %v", err)
	}

	checks := []struct {
		name   string
		substr string
	}{
		{"forwarded env section", "Forwarded Environment Variables"},
		{"anthropic key listed", "ANTHROPIC_API_KEY"},
		{"gh token listed", "GH_TOKEN"},
		{"custom var listed", "CUSTOM_VAR"},
	}
	for _, check := range checks {
		if !strings.Contains(content, check.substr) {
			t.Errorf("Context file missing %s (expected substring %q)", check.name, check.substr)
		}
	}

	// Test without forwarded env vars — section should not appear
	ctxInfo2 := tool.ContextInfo{
		WorkspacePath: "/workspace",
		HomeDir:       homeDir,
	}
	if err := injectContextFile(mgr, ctxInfo2, "", homeDir, logger); err != nil {
		t.Fatalf("injectContextFile (no env vars) failed: %v", err)
	}

	content2, err := mgr.ExecCommand("cat "+destPath, container.ExecCommandOptions{
		Capture: true,
		User:    &user,
	})
	if err != nil {
		t.Fatalf("Failed to read context file: %v", err)
	}

	if strings.Contains(content2, "Forwarded Environment Variables") {
		t.Error("Context file should not contain forwarded env vars section when none are forwarded")
	}

	t.Logf("Forwarded env vars context file content:\n%s", content)
}
