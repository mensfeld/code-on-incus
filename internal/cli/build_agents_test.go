package cli

import (
	"strings"
	"testing"
)

func TestValidateBuildAgents(t *testing.T) {
	if err := validateBuildAgents(nil); err != nil {
		t.Errorf("empty (all agents) must be valid, got %v", err)
	}
	if err := validateBuildAgents([]string{"claude", "opencode", "pi"}); err != nil {
		t.Errorf("all supported agents must be valid, got %v", err)
	}
	err := validateBuildAgents([]string{"claude", "bogus"})
	if err == nil {
		t.Fatal("an unknown agent must be rejected")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the offending agent, got %v", err)
	}
}
