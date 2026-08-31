package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/spf13/cobra"
)

// continueSelfSentinel is the value a bare `--continue` (no `=id`) carries via
// the flag's NoOptDefVal, meaning "resume the --session-id session if it has
// prior state". A distinct sentinel keeps it apart from an explicit
// `--continue=<id>` and from the flag being absent. The `@` prefix can't collide
// with a real coi session id (those are workspace-derived slugs / UUIDs).
const continueSelfSentinel = "@self"

var (
	toolSpecContainer        string
	toolSpecSessionID        string
	toolSpecPromptFile       string
	toolSpecSystemPromptFile string
	toolSpecContinue         string
	toolSpecJSON             bool
)

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Work with the profile's configured AI tool",
	Long: `Inspect the profile's configured AI coding tool without launching an
interactive session.`,
}

var toolSpecCmd = &cobra.Command{
	Use:   "spec",
	Short: "Print a tool's launch command + env for an orchestrator to run (#751)",
	Long: `Print, without executing, the exact command and tool-derived environment for
launching the profile's tool inside an existing container. An external
orchestrator then runs that command through its own container exec + tmux,
owning the terminal, streaming, input, and lifecycle.

coi builds the command from the profile's tool via the tool.Tool abstraction
(session id, resume, model, permission), so it is correct for any supported
tool (claude/codex/…). The prompt (and optional system prompt) are staged into
the container as files and referenced via "$(cat <file>)", keeping arbitrary
prompt content off the command line. Only tool-derived env (model/effort) is
emitted; secrets and auth stay with the caller, which adds its own --env when it
execs.

Tools that can't embed the initial prompt in their launch command (e.g.
opencode) instead get a "prompt" field: the in-container path to the staged
prompt file, for the orchestrator to deliver out-of-band after launch (e.g.
tmux load-buffer + paste-buffer).

  coi tool spec --container <ctr> --session-id <id> --prompt-file <host> \
      [--system-prompt-file <host>] [--continue[=<id>]] --json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return app.toolSpecCommand(cmd)
	},
}

func init() {
	toolSpecCmd.Flags().StringVar(&toolSpecContainer, "container", "", "Existing container to build the launch spec for (required)")
	toolSpecCmd.Flags().StringVar(&toolSpecSessionID, "session-id", "", "Session id for the launch (required; letters, digits, '.', '_', '-', start alphanumeric, max 64)")
	toolSpecCmd.Flags().StringVar(&toolSpecPromptFile, "prompt-file", "", "Host file whose contents are staged as the tool's initial prompt")
	toolSpecCmd.Flags().StringVar(&toolSpecSystemPromptFile, "system-prompt-file", "", "Host file whose contents are staged as the tool's system prompt (tools that support one)")
	toolSpecCmd.Flags().StringVar(&toolSpecContinue, "continue", "", "Resume-or-fresh: resume a session (default: --session-id) if prior state exists, else start fresh. Optional value: --continue=<id>")
	toolSpecCmd.Flags().Lookup("continue").NoOptDefVal = continueSelfSentinel
	toolSpecCmd.Flags().BoolVar(&toolSpecJSON, "json", false, "Print the spec as JSON (the machine-readable form orchestrators consume)")

	toolCmd.AddCommand(toolSpecCmd)
}

// toolSpecResult is the machine-readable spec a `coi tool spec` prints: the
// exact argv to run in the container and the tool-derived env (model/effort)
// only. Secrets/auth are deliberately absent — the caller adds those itself.
//
// Prompt is set only for tools that can't embed the initial prompt in their
// launch command (they don't implement ToolWithPrompt, e.g. opencode). It is the
// in-container path to the staged prompt file — the same file embedding tools
// reference via "$(cat …)" in Command — so the orchestrator delivers it
// out-of-band after launch (e.g. `tmux load-buffer <prompt>` + `paste-buffer`,
// which is file-based and safe for arbitrary content). Empty when the prompt is
// embedded in Command, or when no prompt was given.
type toolSpecResult struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
	Prompt  string            `json:"prompt,omitempty"`
}

// toolSpecCommand builds and prints the launch spec for the profile's tool
// against an existing container (#751). It stages the prompt(s) into the
// container and reuses the same tool.Tool / ToolWithPrompt / ToolWithContainerEnv
// methods the interactive launch uses, but never executes anything — the
// orchestrator owns execution.
func (a *App) toolSpecCommand(cmd *cobra.Command) error {
	if toolSpecContainer == "" {
		return &ExitCodeError{Code: 2, Message: "--container is required"}
	}
	if toolSpecSessionID == "" {
		return &ExitCodeError{Code: 2, Message: "--session-id is required"}
	}
	// The session id is a caller-supplied value that becomes a filename
	// component and is joined into the shell-run launch command (referenced via
	// "$(cat …/<id>.prompt)"); reject anything that isn't a safe token so it
	// can't break or inject the command.
	if err := session.ValidateSessionID(toolSpecSessionID); err != nil {
		return &ExitCodeError{Code: 2, Message: err.Error()}
	}

	// Resolve and validate the continue/resume target up front — before any
	// container or filesystem access — so an unsafe id (it selects a session
	// directory to read) is rejected early. Empty means "no resume".
	resumeID := ""
	if cmd.Flags().Changed("continue") {
		resumeID = toolSpecContinue
		if resumeID == continueSelfSentinel || resumeID == "" {
			resumeID = toolSpecSessionID
		}
		if err := session.ValidateSessionID(resumeID); err != nil {
			return &ExitCodeError{Code: 2, Message: err.Error()}
		}
	}

	t, err := getConfiguredTool(a.cfg)
	if err != nil {
		return err
	}

	mgr := container.NewManager(toolSpecContainer)
	if running, err := mgr.Running(); err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	} else if !running {
		return fmt.Errorf("container %s is not running", toolSpecContainer)
	}

	uid, homeDir := toolSpecUserHome(mgr)
	runsDir := filepath.Join(homeDir, ".coi", "runs")

	// The tool runs as the code user; write run files owned by it.
	if _, err := mgr.ExecCommand(fmt.Sprintf("mkdir -p %s && chown %d:%d %s",
		shellQuote(runsDir), uid, uid, shellQuote(runsDir)),
		container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create runs dir: %w", err)
	}

	// Stage prompt / system prompt as in-container files so their (arbitrary)
	// content never touches the launch command line.
	promptPath, err := a.stageSpecFile(mgr, toolSpecPromptFile, filepath.Join(runsDir, toolSpecSessionID+".prompt"), uid)
	if err != nil {
		return err
	}
	systemPromptPath, err := a.stageSpecFile(mgr, toolSpecSystemPromptFile, filepath.Join(runsDir, toolSpecSessionID+".sys"), uid)
	if err != nil {
		return err
	}

	spec := tool.LaunchSpec{
		SessionID:        toolSpecSessionID,
		PromptFile:       promptPath,
		SystemPromptFile: systemPromptPath,
	}

	// --continue/--resume resolve continue-or-fresh: resume only if prior state
	// for the (already-validated) target session exists, else start fresh.
	if resumeID != "" {
		hostHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		sessionsDir := session.GetSessionsDir(filepath.Join(hostHome, ".coi"), t)
		if prior := discoverResumeSessionID(t, sessionsDir, resumeID); prior != "" {
			spec.Resume = true
			spec.ResumeSessionID = prior
		}
	}

	argv, outOfBandPrompt, err := buildToolSpecCommand(t, spec)
	if err != nil {
		return err
	}

	// Env: tool-derived only (model/effort). Secrets/auth stay with the caller,
	// which adds its own --env when it execs the command.
	env := map[string]string{}
	mergeToolEnv(env, t, toolSpecWorkspacePath)

	return emitToolSpec(toolSpecResult{Command: argv, Env: env, Prompt: outOfBandPrompt})
}

// toolSpecWorkspacePath is the in-container workspace the tool env is computed
// against. The shipped tools' GetContainerEnv ignore it (model/effort don't
// depend on the path); it's a stable default so the spec is deterministic.
const toolSpecWorkspacePath = "/workspace"

// toolSpecUserHome resolves the uid the run files are owned by and the home dir
// the runs directory lives under, for an existing container. Falls back to root
// on images without the code user.
func toolSpecUserHome(mgr container.ContainerManager) (uid int, home string) {
	hasCode, err := session.DetectCodeUser(mgr, container.CodeUser)
	if err != nil || !hasCode {
		return 0, "/root"
	}
	u, err := session.ResolveCodeUID(mgr, container.CodeUser)
	if err != nil {
		return container.CodeUID, "/home/" + container.CodeUser
	}
	return u, "/home/" + container.CodeUser
}

// stageSpecFile copies a host prompt file into the container at destPath and
// returns destPath; an empty hostPath returns "" (no file, no path emitted).
func (a *App) stageSpecFile(mgr container.ContainerManager, hostPath, destPath string, uid int) (string, error) {
	if hostPath == "" {
		return "", nil
	}
	content, err := os.ReadFile(hostPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %q: %w", hostPath, err)
	}
	if err := mgr.CreateFileWithOwner(destPath, string(content), uid, uid, "0600"); err != nil {
		return "", fmt.Errorf("failed to stage prompt file in container: %w", err)
	}
	return destPath, nil
}

// buildToolSpecCommand returns the launch argv for the tool and, when the tool
// can't embed the prompt in argv, the in-container prompt-file path to deliver
// out-of-band (the toolSpecResult.Prompt field). Tools implementing
// ToolWithPrompt (claude/codex) embed the prompt (and system prompt) directly;
// tools without it (opencode) get the base command plus outOfBandPrompt so the
// prompt is surfaced rather than silently dropped. A system prompt on a tool
// without one is still rejected loudly (those tools have no such concept). The
// dummy-mode override used by tests mirrors buildCLICommand.
func buildToolSpecCommand(t tool.Tool, spec tool.LaunchSpec) (argv []string, outOfBandPrompt string, err error) {
	if twp, ok := t.(tool.ToolWithPrompt); ok {
		argv, err = twp.BuildCommandLaunch(spec)
		if err != nil {
			return nil, "", err
		}
	} else {
		if spec.SystemPromptFile != "" {
			return nil, "", fmt.Errorf("tool %q has no system-prompt support for a launch spec", t.Name())
		}
		argv = t.BuildCommand(spec.SessionID, spec.Resume, spec.ResumeSessionID)
		// The prompt can't ride in argv for this tool; hand its staged path back
		// so the orchestrator delivers it out-of-band after launch.
		outOfBandPrompt = spec.PromptFile
	}
	if os.Getenv("COI_USE_DUMMY") == "1" && len(argv) > 0 {
		argv[0] = "dummy"
	}
	return argv, outOfBandPrompt, nil
}

// emitToolSpec prints the spec: JSON on stdout with --json (the orchestrator
// form), otherwise a human-readable summary.
func emitToolSpec(res toolSpecResult) error {
	if toolSpecJSON {
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Println("command:")
	for _, arg := range res.Command {
		fmt.Printf("  %s\n", arg)
	}
	if len(res.Env) > 0 {
		fmt.Println("env:")
		for _, k := range sortedEnvKeys(res.Env) {
			fmt.Printf("  %s=%s\n", k, res.Env[k])
		}
	}
	if res.Prompt != "" {
		fmt.Printf("prompt (deliver out-of-band): %s\n", res.Prompt)
	}
	return nil
}

// discoverResumeSessionID mirrors buildCLICommand's resume discovery: it looks
// up the tool's internal session id from saved state, returning "" when none
// exists (so resume degrades to a fresh start).
func discoverResumeSessionID(t tool.Tool, sessionsDir, resumeID string) string {
	var statePath string
	if configDir := t.ConfigDirName(); configDir != "" {
		statePath = filepath.Join(sessionsDir, resumeID, configDir)
	} else {
		statePath = filepath.Join(sessionsDir, resumeID)
	}
	return t.DiscoverSessionID(statePath)
}
