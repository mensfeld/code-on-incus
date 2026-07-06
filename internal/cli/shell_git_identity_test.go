package cli

import (
	"os/exec"
	"testing"
)

func TestResolveHostGitIdentityFromGlobalConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	configPath := t.TempDir() + "/gitconfig"
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)

	if err := exec.Command("git", "config", "--global", "user.name", "Host User").Run(); err != nil {
		t.Fatalf("set user.name: %v", err)
	}
	if err := exec.Command("git", "config", "--global", "user.email", "host@example.com").Run(); err != nil {
		t.Fatalf("set user.email: %v", err)
	}

	got := resolveHostGitIdentity()
	if got.Name != "Host User" || got.Email != "host@example.com" {
		t.Fatalf("identity = %+v", got)
	}
}

func TestResolveHostGitIdentityRequiresNameAndEmail(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	configPath := t.TempDir() + "/gitconfig"
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)

	if err := exec.Command("git", "config", "--global", "user.name", "Host User").Run(); err != nil {
		t.Fatalf("set user.name: %v", err)
	}

	got := resolveHostGitIdentity()
	if got.Name != "" || got.Email != "" {
		t.Fatalf("incomplete identity should be dropped, got %+v", got)
	}
}
