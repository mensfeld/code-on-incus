package cli

import "testing"

// Deterministic integration coverage for the [tool.claude] effort_level wiring,
// the companion to the model coverage in model_config_test.go (and reusing its
// sandboxEnvForProjectConfig helper). It drives the real chain — project
// config.toml → config resolution → getConfiguredTool → GetSandboxSettings —
// and asserts CLAUDE_CODE_EFFORT_LEVEL, the env var that actually locks the
// effort level for the session, lands in the sandbox settings.
//
// This replaces tests/shell/ephemeral/effort_level_ephemeral.py, which (a) was
// vacuous — it matched marker strings that also appear in the echoed command the
// terminal monitor captures, so the assertions passed regardless of the real
// result — and (b) exercised the ephemeral settings.json-injection path, which
// is skipped in the CI dummy-mode flow, so a non-vacuous version would fail
// there anyway.

func TestToolClaudeEffort_InjectedFromConfig(t *testing.T) {
	env := sandboxEnvForProjectConfig(t,
		"[tool]\nname = \"claude\"\n\n[tool.claude]\neffort_level = \"high\"\n")
	if env["CLAUDE_CODE_EFFORT_LEVEL"] != "high" {
		t.Errorf("expected CLAUDE_CODE_EFFORT_LEVEL=high in sandbox env, got %q (env=%v)",
			env["CLAUDE_CODE_EFFORT_LEVEL"], env)
	}
}

func TestToolClaudeEffort_AbsentWhenUnset(t *testing.T) {
	env := sandboxEnvForProjectConfig(t, "[tool]\nname = \"claude\"\n")
	if v, ok := env["CLAUDE_CODE_EFFORT_LEVEL"]; ok {
		t.Errorf("expected no CLAUDE_CODE_EFFORT_LEVEL when effort_level is unset, got %q", v)
	}
}

func TestToolClaudeEffort_Variants(t *testing.T) {
	for _, level := range []string{"low", "medium", "high", "xhigh", "max", "auto"} {
		env := sandboxEnvForProjectConfig(t,
			"[tool.claude]\neffort_level = \""+level+"\"\n")
		if env["CLAUDE_CODE_EFFORT_LEVEL"] != level {
			t.Errorf("effort_level=%q: expected CLAUDE_CODE_EFFORT_LEVEL=%q, got %q",
				level, level, env["CLAUDE_CODE_EFFORT_LEVEL"])
		}
	}
}
