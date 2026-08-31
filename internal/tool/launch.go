package tool

// LaunchSpec carries the session-specific dynamics an external orchestrator
// supplies for a headless (non-interactive) tool launch. The tool owns the
// translation into its CLI; the caller owns only these dynamics.
//
// PromptFile and SystemPromptFile are IN-CONTAINER paths to files holding the
// initial user prompt and an optional system prompt. Passing files (not the
// text) keeps arbitrary prompt content off the launch command line — the
// headless launcher runs the tool from a script file, and the tool references
// the prompt via a `"$(cat <file>)"` substitution the shell expands at launch,
// so prompts with quotes/newlines can't corrupt the command.
type LaunchSpec struct {
	SessionID        string
	Resume           bool
	ResumeSessionID  string
	PromptFile       string // in-container path; empty = no initial prompt
	SystemPromptFile string // in-container path; empty = no system prompt
}

// ToolWithPrompt is implemented by tools that can be launched non-interactively
// with an initial prompt (and optionally a system prompt) embedded in the
// launch command. Tools that don't implement it fall back to BuildCommand plus
// out-of-band prompt delivery (the headless launcher pastes the prompt into the
// tmux pane after the tool starts).
type ToolWithPrompt interface {
	Tool
	// BuildCommandLaunch returns the full argv for a headless launch, embedding
	// the prompt/system-prompt per the tool's CLI. It returns an error when the
	// spec requests something the tool can't express (e.g. a system prompt on a
	// tool without one) so the caller fails loudly instead of dropping it.
	BuildCommandLaunch(spec LaunchSpec) ([]string, error)
}

// catSubst returns a `"$(cat <path>)"` token whose value the container shell
// expands to the file's contents at launch. path is a launcher-controlled file
// (~/.coi/runs/<id>.*) with no shell metacharacters, so the arbitrary prompt
// text lives only in the file, never on the command line. Only valid inside the
// headless script-file launch, where there are no nested quoting layers.
func catSubst(path string) string {
	return `"$(cat ` + path + `)"`
}
