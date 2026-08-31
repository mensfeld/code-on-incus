package tool

import (
	"strings"
	"testing"
)

func TestCatSubst(t *testing.T) {
	got := catSubst("/home/code/.coi/runs/abc.prompt")
	want := `"$(cat /home/code/.coi/runs/abc.prompt)"`
	if got != want {
		t.Errorf("catSubst = %q, want %q", got, want)
	}
}

func TestClaudeBuildCommandLaunch_PromptAndSystem(t *testing.T) {
	c := NewClaude().(ToolWithPrompt)
	cmd, err := c.BuildCommandLaunch(LaunchSpec{
		SessionID:        "sid-1",
		PromptFile:       "/run/p",
		SystemPromptFile: "/run/s",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	// Fresh session (no resume) keeps --session-id.
	if !strings.Contains(joined, "--session-id sid-1") {
		t.Errorf("missing --session-id: %q", joined)
	}
	if !strings.Contains(joined, `--append-system-prompt "$(cat /run/s)"`) {
		t.Errorf("missing system prompt: %q", joined)
	}
	// The user prompt must be the trailing positional arg.
	if last := cmd[len(cmd)-1]; last != `"$(cat /run/p)"` {
		t.Errorf("prompt must be the trailing positional, got last=%q in %q", last, joined)
	}
}

func TestClaudeBuildCommandLaunch_NoPrompt(t *testing.T) {
	c := NewClaude().(ToolWithPrompt)
	cmd, err := c.BuildCommandLaunch(LaunchSpec{SessionID: "sid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd, " "), "cat") {
		t.Errorf("no prompt/system files -> no cat subst, got %q", cmd)
	}
}

func TestClaudeBuildCommandLaunch_Resume(t *testing.T) {
	c := NewClaude().(ToolWithPrompt)
	cmd, err := c.BuildCommandLaunch(LaunchSpec{
		Resume:          true,
		ResumeSessionID: "prev-9",
		PromptFile:      "/run/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "--resume prev-9") {
		t.Errorf("resume should carry the prior id: %q", joined)
	}
	if strings.Contains(joined, "--session-id") {
		t.Errorf("resume must not also set --session-id: %q", joined)
	}
}

func TestCodexBuildCommandLaunch_Prompt(t *testing.T) {
	c := NewCodex().(ToolWithPrompt)
	cmd, err := c.BuildCommandLaunch(LaunchSpec{SessionID: "sid-1", PromptFile: "/run/p"})
	if err != nil {
		t.Fatal(err)
	}
	if last := cmd[len(cmd)-1]; last != `"$(cat /run/p)"` {
		t.Errorf("prompt must be the trailing positional, got %q", cmd)
	}
}

func TestCodexBuildCommandLaunch_SystemPromptRejected(t *testing.T) {
	c := NewCodex().(ToolWithPrompt)
	_, err := c.BuildCommandLaunch(LaunchSpec{SystemPromptFile: "/run/s"})
	if err == nil {
		t.Error("codex must reject a system prompt (no such flag)")
	}
}

// opencode intentionally does NOT implement ToolWithPrompt (its interactive
// prompt injection has no clean flag); `coi tool spec` fails loudly for such
// tools when a prompt is requested, so the orchestrator delivers it out-of-band.
func TestOpencodeDoesNotImplementToolWithPrompt(t *testing.T) {
	if _, ok := NewOpencode().(ToolWithPrompt); ok {
		t.Error("opencode should not implement ToolWithPrompt (orchestrator delivers prompt out-of-band)")
	}
}
