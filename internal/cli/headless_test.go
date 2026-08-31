package cli

import (
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

func TestBuildHeadlessScript(t *testing.T) {
	got := buildHeadlessScript(`claude --session-id x "$(cat /run/p)"`, "/workspace", "/home/code/.coi/runs/x.exit")
	for _, want := range []string{
		"trap : INT",
		"cd '/workspace'",
		`claude --session-id x "$(cat /run/p)"`, // tool cmd stays raw so the cat subst works
		"echo $? > '/home/code/.coi/runs/x.exit'",
		"exec bash",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("script missing %q:\n%s", want, got)
		}
	}
	// The exit sentinel must be recorded AFTER the tool runs, before exec bash.
	if strings.Index(got, "echo $?") < strings.Index(got, "claude") {
		t.Error("exit sentinel must be written after the tool command")
	}
	if strings.Index(got, "exec bash") < strings.Index(got, "echo $?") {
		t.Error("exec bash must come after the exit sentinel")
	}
}

func TestBuildHeadlessTmuxCmd(t *testing.T) {
	got := buildHeadlessTmuxCmd("coi-abc-1", "/workspace", "/home/code/.coi/runs/x.sh", map[string]string{
		"ANTHROPIC_MODEL": "opus",
		"FOO":             "bar",
	})
	for _, want := range []string{
		"tmux new-session -d -s 'coi-abc-1'",
		"-e 'ANTHROPIC_MODEL=opus'",
		"-e 'FOO=bar'",
		"-c '/workspace'",
		"'bash /home/code/.coi/runs/x.sh'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tmux cmd missing %q:\n%s", want, got)
		}
	}
	// Env keys are emitted sorted (deterministic).
	if strings.Index(got, "ANTHROPIC_MODEL") > strings.Index(got, "FOO") {
		t.Errorf("env flags must be sorted: %s", got)
	}
}

func TestBuildHeadlessToolCmd_ClaudeNative(t *testing.T) {
	a := &App{}
	cmd, paste, err := a.buildHeadlessToolCmd(tool.NewClaude(), tool.LaunchSpec{
		SessionID: "sid", PromptFile: "/run/p", SystemPromptFile: "/run/s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if paste {
		t.Error("claude embeds the prompt natively; paste must be false")
	}
	if !strings.Contains(cmd, `--append-system-prompt "$(cat /run/s)"`) {
		t.Errorf("missing system prompt: %s", cmd)
	}
	if !strings.HasSuffix(cmd, `"$(cat /run/p)"`) {
		t.Errorf("prompt must be trailing: %s", cmd)
	}
}

func TestBuildHeadlessToolCmd_DummyOverride(t *testing.T) {
	t.Setenv("COI_USE_DUMMY", "1")
	a := &App{}
	cmd, _, err := a.buildHeadlessToolCmd(tool.NewClaude(), tool.LaunchSpec{SessionID: "sid"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cmd, "dummy ") {
		t.Errorf("dummy override should replace the binary, got: %s", cmd)
	}
}

func TestBuildHeadlessToolCmd_OpencodePastesPrompt(t *testing.T) {
	a := &App{}
	// opencode doesn't implement ToolWithPrompt -> prompt must be pasted, and a
	// system prompt is rejected loudly.
	_, paste, err := a.buildHeadlessToolCmd(tool.NewOpencode(), tool.LaunchSpec{SessionID: "sid", PromptFile: "/run/p"})
	if err != nil {
		t.Fatal(err)
	}
	if !paste {
		t.Error("opencode should signal paste=true for the initial prompt")
	}
	if _, _, err := a.buildHeadlessToolCmd(tool.NewOpencode(), tool.LaunchSpec{SystemPromptFile: "/run/s"}); err == nil {
		t.Error("a system prompt on a tool without support must error")
	}
}

func TestParseExitSentinel(t *testing.T) {
	if st := parseExitSentinel("sid", "", false); st.State != "running" || st.ExitCode != nil {
		t.Errorf("absent sentinel -> running/no-code, got %+v", st)
	}
	st := parseExitSentinel("sid", "0\n", true)
	if st.State != "done" || st.ExitCode == nil || *st.ExitCode != 0 {
		t.Errorf("exit 0 -> done/0, got %+v", st)
	}
	st = parseExitSentinel("sid", "42", true)
	if st.State != "done" || *st.ExitCode != 42 {
		t.Errorf("exit 42 -> done/42, got %+v", st)
	}
	st = parseExitSentinel("sid", "garbage", true)
	if st.State != "done" || *st.ExitCode != -1 {
		t.Errorf("unparseable but present -> done/-1, got %+v", st)
	}
}
