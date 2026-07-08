package image

import "testing"

func TestAgentEnv(t *testing.T) {
	if env := agentEnv(nil); env != nil {
		t.Errorf("nil agents should yield nil env (all agents), got %v", env)
	}
	if env := agentEnv([]string{}); env != nil {
		t.Errorf("empty agents should yield nil env (all agents), got %v", env)
	}
	env := agentEnv([]string{"claude", "pi"})
	if env["COI_AGENTS"] != "claude,pi" {
		t.Errorf("COI_AGENTS = %q, want %q", env["COI_AGENTS"], "claude,pi")
	}
}
