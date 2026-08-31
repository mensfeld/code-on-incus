package cli

import (
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

func TestBuildToolSpecCommand_ClaudeNative(t *testing.T) {
	argv, err := buildToolSpecCommand(tool.NewClaude(), tool.LaunchSpec{
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
}

func TestBuildToolSpecCommand_DummyOverride(t *testing.T) {
	t.Setenv("COI_USE_DUMMY", "1")
	argv, err := buildToolSpecCommand(tool.NewClaude(), tool.LaunchSpec{SessionID: "sid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) == 0 || argv[0] != "dummy" {
		t.Errorf("dummy override should replace the binary, got: %v", argv)
	}
}

func TestBuildToolSpecCommand_CodexPrompt(t *testing.T) {
	// Codex embeds the prompt as a trailing positional and has no system prompt.
	argv, err := buildToolSpecCommand(tool.NewCodex(), tool.LaunchSpec{
		SessionID: "sid", PromptFile: "/run/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if argv[len(argv)-1] != `"$(cat /run/p)"` {
		t.Errorf("codex prompt must be trailing cat-subst: %v", argv)
	}
	if _, err := buildToolSpecCommand(tool.NewCodex(), tool.LaunchSpec{
		SessionID: "sid", SystemPromptFile: "/run/s",
	}); err == nil {
		t.Error("codex must reject a system prompt (no such flag)")
	}
}

func TestBuildToolSpecCommand_ToolWithoutPromptRejected(t *testing.T) {
	// opencode doesn't implement ToolWithPrompt: a requested prompt can't be
	// embedded in argv, so spec fails loudly instead of dropping it.
	if _, err := buildToolSpecCommand(tool.NewOpencode(), tool.LaunchSpec{
		SessionID: "sid", PromptFile: "/run/p",
	}); err == nil {
		t.Error("a prompt on a tool without embedding support must error")
	}
	// With no prompt, the base command is fine.
	argv, err := buildToolSpecCommand(tool.NewOpencode(), tool.LaunchSpec{SessionID: "sid"})
	if err != nil {
		t.Fatalf("no-prompt launch for opencode should succeed: %v", err)
	}
	if len(argv) == 0 {
		t.Error("expected a base command for opencode")
	}
}
