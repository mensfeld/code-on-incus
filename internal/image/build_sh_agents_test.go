package image

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/tool"
)

// extractShellFunc returns the body of a `name() { ... }` function from build.sh,
// assuming the closing brace sits at column 0 (the style build.sh uses).
func extractShellFunc(t *testing.T, script, name string) string {
	t.Helper()
	start := strings.Index(script, name+"() {")
	if start < 0 {
		t.Fatalf("build.sh: function %s() not found", name)
	}
	rest := script[start:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("build.sh: could not find end of %s()", name)
	}
	return rest[:end]
}

// Finding 3 (registry <-> build.sh drift): the agent list is hardcoded in build.sh
// twice — the install_selected_agents case arms and the ${COI_AGENTS:-...} default —
// parallel to the Go tool registry. Nothing at compile time keeps them in sync, so a
// newly-registered agent could pass host-side validateBuildAgents yet hit build.sh's
// `*` arm and install nothing (silent exit-127 footgun). This test makes the registry
// the enforced single source of truth: CI fails the moment the three diverge.
func TestBuildScriptAgentsMatchRegistry(t *testing.T) {
	script := string(embeddedCoiBuildScript)
	fn := extractShellFunc(t, script, "install_selected_agents")

	want := tool.ListSupported() // already sorted
	if len(want) == 0 {
		t.Fatal("tool.ListSupported() is empty")
	}

	// (a) case arms: `claude) install_claude_cli ;;` etc. Skip the "") and *) arms.
	caseArm := regexp.MustCompile(`(?m)^\s*([a-z0-9_-]+)\)\s+install_`)
	got := caseArm.FindAllStringSubmatch(fn, -1)
	arms := make([]string, 0, len(got))
	for _, m := range got {
		arms = append(arms, m[1])
	}
	sort.Strings(arms)
	if strings.Join(arms, ",") != strings.Join(want, ",") {
		t.Errorf("install_selected_agents case arms %v do not match tool.ListSupported() %v; "+
			"add/remove a case arm (and an install_<agent> function) to match the registry", arms, want)
	}

	// (b) default fallback literal: ${COI_AGENTS:-claude opencode pi}
	def := regexp.MustCompile(`COI_AGENTS:-([^}]*)}`).FindStringSubmatch(fn)
	if def == nil {
		t.Fatal("install_selected_agents: could not find the ${COI_AGENTS:-...} default literal")
	}
	defAgents := strings.Fields(strings.TrimSpace(def[1]))
	sort.Strings(defAgents)
	if strings.Join(defAgents, ",") != strings.Join(want, ",") {
		t.Errorf("COI_AGENTS default %v does not match tool.ListSupported() %v; "+
			"the unset-installs-all default must list exactly the supported agents", defAgents, want)
	}
}

// Finding 4 (call-site coverage): the hermetic bash test sources only
// install_selected_agents, so a revert of main() to the old unconditional
// install_claude_cli/install_opencode/install_pi calls would leave every test
// green while the selector goes dead. Assert main() dispatches through
// install_selected_agents and does NOT call the per-agent installers directly.
func TestBuildScriptMainDispatchesThroughSelector(t *testing.T) {
	main := extractShellFunc(t, string(embeddedCoiBuildScript), "main")

	if !strings.Contains(main, "install_selected_agents") {
		t.Error("main() must call install_selected_agents (agent selection is bypassed otherwise)")
	}
	for _, direct := range []string{"install_claude_cli", "install_opencode", "install_pi"} {
		if strings.Contains(main, direct) {
			t.Errorf("main() calls %s directly; per-agent installers must only be reached via "+
				"install_selected_agents so COI_AGENTS is honored", direct)
		}
	}
}
