package cli

import (
	"strings"
	"testing"
)

func TestBuildTmuxNewSessionCmd_PassesEnvViaDashE(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN": "ghp_secret",
		"JIRA_USER":    "alice",
	}
	got := buildTmuxNewSessionCmd("coi-x", "/workspace", "claude --verbose", env)

	for _, want := range []string{
		"-e 'GITHUB_TOKEN=ghp_secret'",
		"-e 'JIRA_USER=alice'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in command, got: %s", want, got)
		}
	}
	if strings.Contains(got, "export GITHUB_TOKEN") || strings.Contains(got, "export JIRA_USER") {
		t.Errorf("env must not be inlined as `export` statements (would leak to ps and not propagate to new windows): %s", got)
	}
	if !strings.Contains(got, "tmux new-session -d -s coi-x") {
		t.Errorf("expected detached new-session for coi-x, got: %s", got)
	}
	if !strings.Contains(got, "-c /workspace") {
		t.Errorf("expected workspace path -c flag, got: %s", got)
	}
	if !strings.Contains(got, "claude --verbose") {
		t.Errorf("expected cliCmd in payload, got: %s", got)
	}
	if !strings.Contains(got, "trap : INT;") {
		t.Errorf("expected SIGINT trap to be preserved, got: %s", got)
	}
	if !strings.Contains(got, "exec bash") {
		t.Errorf("expected exec bash fallback to be preserved, got: %s", got)
	}
}

func TestBuildTmuxNewSessionCmd_DeterministicOrder(t *testing.T) {
	env := map[string]string{"BBB": "2", "AAA": "1", "CCC": "3"}
	got := buildTmuxNewSessionCmd("s", "/w", "cmd", env)
	a := strings.Index(got, "AAA=")
	b := strings.Index(got, "BBB=")
	c := strings.Index(got, "CCC=")
	if a < 0 || b < 0 || c < 0 || !(a < b && b < c) {
		t.Errorf("expected env flags in lexicographic order, got: %s", got)
	}
}

func TestBuildTmuxNewSessionCmd_QuotesAwkwardValues(t *testing.T) {
	env := map[string]string{
		"WITH_SPACE":  "hello world",
		"WITH_QUOTE":  "it's risky",
		"WITH_DOLLAR": "$HOME",
	}
	got := buildTmuxNewSessionCmd("s", "/w", "cmd", env)

	// Spaces stay inside the quoted token.
	if !strings.Contains(got, "-e 'WITH_SPACE=hello world'") {
		t.Errorf("space value not single-quoted as one token, got: %s", got)
	}
	// Embedded single quote is escaped via the standard '"'"' trick.
	if !strings.Contains(got, `-e 'WITH_QUOTE=it'"'"'s risky'`) {
		t.Errorf("embedded single quote not escaped, got: %s", got)
	}
	// $HOME must not be exposed to shell expansion -- it must stay inside single quotes.
	if !strings.Contains(got, "-e 'WITH_DOLLAR=$HOME'") {
		t.Errorf("dollar sign not protected by single quotes, got: %s", got)
	}
}

func TestBuildTmuxNewSessionCmd_EmptyEnv(t *testing.T) {
	got := buildTmuxNewSessionCmd("s", "/w", "cmd", nil)
	if strings.Contains(got, " -e ") {
		t.Errorf("expected no -e flags for nil env, got: %s", got)
	}
	if !strings.Contains(got, "tmux new-session -d -s s") {
		t.Errorf("expected new-session header, got: %s", got)
	}
}

func TestBuildTmuxSetEnvironmentCmds_OnePerVar(t *testing.T) {
	env := map[string]string{"FOO": "1", "BAR": "two words"}
	got := buildTmuxSetEnvironmentCmds("coi-x", env)

	if len(got) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(got), got)
	}
	// Sorted: BAR before FOO.
	if got[0] != "tmux set-environment -g -t coi-x 'BAR' 'two words'" {
		t.Errorf("unexpected first command: %q", got[0])
	}
	if got[1] != "tmux set-environment -g -t coi-x 'FOO' '1'" {
		t.Errorf("unexpected second command: %q", got[1])
	}
}

func TestBuildTmuxSetEnvironmentCmds_EmptyEnv(t *testing.T) {
	if got := buildTmuxSetEnvironmentCmds("s", nil); len(got) != 0 {
		t.Errorf("expected no commands for nil env, got: %v", got)
	}
}
