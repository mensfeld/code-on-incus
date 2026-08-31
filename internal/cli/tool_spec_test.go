package cli

import (
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

func TestBuildToolSpecCommand_ClaudeNative(t *testing.T) {
	argv, prompt, err := buildToolSpecCommand(tool.NewClaude(), tool.LaunchSpec{
		SessionID: "sid", PromptFile: "/run/p", SystemPromptFile: "/run/s",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--session-id sid") {
		t.Errorf("missing --session-id: %q", joined)
	}
	// System prompt embedded via --append-system-prompt "$(cat …)".
	if !strings.Contains(joined, `--append-system-prompt "$(cat /run/s)"`) {
		t.Errorf("missing system prompt: %q", joined)
	}
	// Initial prompt is the trailing positional, staged via "$(cat …)".
	if argv[len(argv)-1] != `"$(cat /run/p)"` {
		t.Errorf("prompt must be trailing cat-subst: %q", joined)
	}
	// Claude embeds the prompt, so there's no out-of-band prompt.
	if prompt != "" {
		t.Errorf("embedding tool must not surface an out-of-band prompt, got %q", prompt)
	}
}

func TestBuildToolSpecCommand_DummyOverride(t *testing.T) {
	t.Setenv("COI_USE_DUMMY", "1")
	argv, _, err := buildToolSpecCommand(tool.NewClaude(), tool.LaunchSpec{SessionID: "sid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) == 0 || argv[0] != "dummy" {
		t.Errorf("dummy override should replace the binary, got: %v", argv)
	}
}

func TestBuildToolSpecCommand_CodexPrompt(t *testing.T) {
	// Codex embeds the prompt as a trailing positional and has no system prompt.
	argv, prompt, err := buildToolSpecCommand(tool.NewCodex(), tool.LaunchSpec{
		SessionID: "sid", PromptFile: "/run/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if argv[len(argv)-1] != `"$(cat /run/p)"` {
		t.Errorf("codex prompt must be trailing cat-subst: %v", argv)
	}
	if prompt != "" {
		t.Errorf("codex embeds the prompt; no out-of-band prompt expected, got %q", prompt)
	}
	if _, _, err := buildToolSpecCommand(tool.NewCodex(), tool.LaunchSpec{
		SessionID: "sid", SystemPromptFile: "/run/s",
	}); err == nil {
		t.Error("codex must reject a system prompt (no such flag)")
	}
}

func TestBuildToolSpecCommand_ToolWithoutPromptOutOfBand(t *testing.T) {
	// opencode doesn't implement ToolWithPrompt: the prompt can't ride in argv,
	// so it comes back as the out-of-band prompt (the staged file path), not in
	// the command and not dropped.
	argv, prompt, err := buildToolSpecCommand(tool.NewOpencode(), tool.LaunchSpec{
		SessionID: "sid", PromptFile: "/run/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) == 0 {
		t.Error("expected a base command for opencode")
	}
	if strings.Contains(strings.Join(argv, " "), "$(cat") {
		t.Errorf("opencode must not embed the prompt in argv: %v", argv)
	}
	if prompt != "/run/p" {
		t.Errorf("expected out-of-band prompt path /run/p, got %q", prompt)
	}
	// A system prompt on a tool without one is still rejected loudly.
	if _, _, err := buildToolSpecCommand(tool.NewOpencode(), tool.LaunchSpec{
		SessionID: "sid", SystemPromptFile: "/run/s",
	}); err == nil {
		t.Error("a system prompt on a tool without support must error")
	}
	// With no prompt, the base command is fine and there's nothing out-of-band.
	argv, prompt, err = buildToolSpecCommand(tool.NewOpencode(), tool.LaunchSpec{SessionID: "sid"})
	if err != nil {
		t.Fatalf("no-prompt launch for opencode should succeed: %v", err)
	}
	if len(argv) == 0 || prompt != "" {
		t.Errorf("no-prompt opencode: want base cmd and empty prompt, got argv=%v prompt=%q", argv, prompt)
	}
}
