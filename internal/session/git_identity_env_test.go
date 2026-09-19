package session

import (
	"sort"
	"strings"
	"testing"
)

func TestPlanGitIdentityEnv_LockedSetsFourKeys(t *testing.T) {
	id := GitIdentity{Name: testBotName, Email: testBotEmail}
	plan := planGitIdentityEnv("", id, true)

	want := map[string]string{
		"GIT_AUTHOR_NAME":     id.Name,
		"GIT_AUTHOR_EMAIL":    id.Email,
		"GIT_COMMITTER_NAME":  id.Name,
		"GIT_COMMITTER_EMAIL": id.Email,
	}
	if len(plan.set) != len(want) {
		t.Fatalf("set = %v, want %d keys", plan.set, len(want))
	}
	for k, v := range want {
		if plan.set[k] != v {
			t.Errorf("%s = %q, want %q", k, plan.set[k], v)
		}
	}
	// setKeys sorted (deterministic apply order) and reflected in the marker.
	if !sort.StringsAreSorted(plan.setKeys) {
		t.Errorf("setKeys not sorted: %v", plan.setKeys)
	}
	if plan.marker != strings.Join(plan.setKeys, ",") {
		t.Errorf("marker %q != joined setKeys %v", plan.marker, plan.setKeys)
	}
}

func TestPlanGitIdentityEnv_NotLockedOrIncompleteSetsNothing(t *testing.T) {
	id := GitIdentity{Name: "bot", Email: "b@x"}
	if p := planGitIdentityEnv("", id, false); len(p.set) != 0 || p.marker != "" {
		t.Errorf("unlocked must set nothing, got %v marker=%q", p.set, p.marker)
	}
	if p := planGitIdentityEnv("", GitIdentity{Name: "bot"}, true); len(p.set) != 0 {
		t.Errorf("incomplete identity must set nothing, got %v", p.set)
	}
}

// Turning the lock OFF on a reused container must unset exactly the keys the
// previous (locked) setup recorded in the marker.
func TestPlanGitIdentityEnv_UnsetsStaleKeysWhenDisabled(t *testing.T) {
	prev := "GIT_AUTHOR_EMAIL,GIT_AUTHOR_NAME,GIT_COMMITTER_EMAIL,GIT_COMMITTER_NAME"
	plan := planGitIdentityEnv(prev, GitIdentity{Name: "bot", Email: "b@x"}, false)
	if len(plan.set) != 0 {
		t.Fatalf("disabled lock must set nothing, got %v", plan.set)
	}
	want := []string{"GIT_AUTHOR_EMAIL", "GIT_AUTHOR_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_NAME"}
	if strings.Join(plan.unset, ",") != strings.Join(want, ",") {
		t.Errorf("unset = %v, want %v", plan.unset, want)
	}
	if plan.marker != "" {
		t.Errorf("marker must clear when nothing is set, got %q", plan.marker)
	}
}

// An unsafe value (newline) is skipped, not applied.
func TestPlanGitIdentityEnv_SkipsUnsafeValue(t *testing.T) {
	plan := planGitIdentityEnv("", GitIdentity{Name: "bad\nname", Email: "b@x"}, true)
	// The two NAME keys carry the newline and must be skipped; the two EMAIL keys apply.
	if _, ok := plan.set["GIT_AUTHOR_NAME"]; ok {
		t.Error("newline-bearing name must be skipped, not set")
	}
	if plan.set["GIT_AUTHOR_EMAIL"] != "b@x" {
		t.Errorf("safe email should still be set, got %q", plan.set["GIT_AUTHOR_EMAIL"])
	}
	if len(plan.skipped) != 2 {
		t.Errorf("expected 2 skipped keys (both NAME), got %v", plan.skipped)
	}
}
