package image

import "testing"

func TestAgentEnv(t *testing.T) {
	if env := agentEnv(nil); env != nil {
		t.Errorf("nil agents should yield nil env (default agent set), got %v", env)
	}
	if env := agentEnv([]string{}); env != nil {
		t.Errorf("empty agents should yield nil env (default agent set), got %v", env)
	}
	env := agentEnv([]string{"claude", "pi"})
	if env["COI_AGENTS"] != "claude,pi" {
		t.Errorf("COI_AGENTS = %q, want %q", env["COI_AGENTS"], "claude,pi")
	}
}

// Finding 4 (Env wiring): prove the builder actually threads the agent selection
// into the exec options used to run build.sh — i.e. runBuildScriptResolved does not
// silently drop the Env. Testable without launching a container.
func TestBuildScriptExecOpts(t *testing.T) {
	// Selection present -> COI_AGENTS delivered to the script.
	b := NewBuilder(BuildOptions{Agents: []string{"claude", "pi"}})
	opts := b.buildScriptExecOpts()
	if opts.Env["COI_AGENTS"] != "claude,pi" {
		t.Errorf("exec opts COI_AGENTS = %q, want %q", opts.Env["COI_AGENTS"], "claude,pi")
	}
	if opts.Capture {
		t.Error("build script exec should stream (Capture=false), not capture")
	}

	// No selection -> no COI_AGENTS, so build.sh keeps its default agent set.
	b = NewBuilder(BuildOptions{})
	if opts := b.buildScriptExecOpts(); opts.Env != nil {
		t.Errorf("empty agents should leave Env nil (COI_AGENTS unset), got %v", opts.Env)
	}
}
