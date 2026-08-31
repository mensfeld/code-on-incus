package session

import (
	"reflect"
	"testing"
)

func TestPlanToolContainerEnv_SetsDesired(t *testing.T) {
	plan := planToolContainerEnv("", map[string]string{
		"ANTHROPIC_MODEL":          "opus",
		"CLAUDE_CODE_EFFORT_LEVEL": "high",
	})
	if !reflect.DeepEqual(plan.setKeys, []string{"ANTHROPIC_MODEL", "CLAUDE_CODE_EFFORT_LEVEL"}) {
		t.Errorf("setKeys = %v, want sorted [ANTHROPIC_MODEL CLAUDE_CODE_EFFORT_LEVEL]", plan.setKeys)
	}
	if plan.set["ANTHROPIC_MODEL"] != "opus" || plan.set["CLAUDE_CODE_EFFORT_LEVEL"] != "high" {
		t.Errorf("set = %v", plan.set)
	}
	if plan.marker != "ANTHROPIC_MODEL,CLAUDE_CODE_EFFORT_LEVEL" {
		t.Errorf("marker = %q", plan.marker)
	}
	if len(plan.unset) != 0 {
		t.Errorf("unset = %v, want none", plan.unset)
	}
}

// A profile switch (opus+high -> just sonnet) must unset the key the previous
// profile set but the new one no longer wants — the core reuse-staleness fix.
func TestPlanToolContainerEnv_ReconcilesStaleKeys(t *testing.T) {
	prev := "ANTHROPIC_MODEL,CLAUDE_CODE_EFFORT_LEVEL"
	plan := planToolContainerEnv(prev, map[string]string{"ANTHROPIC_MODEL": "sonnet"})

	if !reflect.DeepEqual(plan.setKeys, []string{"ANTHROPIC_MODEL"}) {
		t.Errorf("setKeys = %v, want [ANTHROPIC_MODEL]", plan.setKeys)
	}
	if plan.set["ANTHROPIC_MODEL"] != "sonnet" {
		t.Errorf("model not updated: %v", plan.set)
	}
	if !reflect.DeepEqual(plan.unset, []string{"CLAUDE_CODE_EFFORT_LEVEL"}) {
		t.Errorf("unset = %v, want [CLAUDE_CODE_EFFORT_LEVEL]", plan.unset)
	}
	if plan.marker != "ANTHROPIC_MODEL" {
		t.Errorf("marker = %q, want ANTHROPIC_MODEL", plan.marker)
	}
}

// Nothing desired but keys set before -> unset them all and clear the marker.
func TestPlanToolContainerEnv_ClearsWhenEmpty(t *testing.T) {
	plan := planToolContainerEnv("ANTHROPIC_MODEL", map[string]string{})
	if len(plan.setKeys) != 0 {
		t.Errorf("setKeys = %v, want none", plan.setKeys)
	}
	if !reflect.DeepEqual(plan.unset, []string{"ANTHROPIC_MODEL"}) {
		t.Errorf("unset = %v, want [ANTHROPIC_MODEL]", plan.unset)
	}
	if plan.marker != "" {
		t.Errorf("marker = %q, want empty", plan.marker)
	}
}

// An unsafe value (newline) is skipped, not set — and because it isn't applied,
// a previously-set key of the same name is reconciled away.
func TestPlanToolContainerEnv_SkipsUnsafeValue(t *testing.T) {
	plan := planToolContainerEnv("ANTHROPIC_MODEL", map[string]string{
		"ANTHROPIC_MODEL": "opus\nenvironment.EVIL=1",
	})
	if len(plan.setKeys) != 0 {
		t.Errorf("setKeys = %v, want none (unsafe skipped)", plan.setKeys)
	}
	if !reflect.DeepEqual(plan.skipped, []string{"ANTHROPIC_MODEL"}) {
		t.Errorf("skipped = %v, want [ANTHROPIC_MODEL]", plan.skipped)
	}
	if !reflect.DeepEqual(plan.unset, []string{"ANTHROPIC_MODEL"}) {
		t.Errorf("unset = %v, want [ANTHROPIC_MODEL] (unsafe means no longer set)", plan.unset)
	}
}

func TestValidContainerEnvValue(t *testing.T) {
	for _, v := range []string{"opus", "claude-opus-4-8", "high", "/workspace/.local/share", ""} {
		if !validContainerEnvValue(v) {
			t.Errorf("validContainerEnvValue(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"a\nb", "a\rb", "a\x00b"} {
		if validContainerEnvValue(v) {
			t.Errorf("validContainerEnvValue(%q) = true, want false", v)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"A", []string{"A"}},
		{"A,B", []string{"A", "B"}},
		{" A , ,B ", []string{"A", "B"}},
	}
	for _, tc := range cases {
		if got := splitCSV(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
