package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// headlessResult is the machine-readable handle set a headless launch prints
// (with --json) so an orchestrator can attach/capture/send and wait on the run.
type headlessResult struct {
	Container   string `json:"container"`
	TmuxSession string `json:"tmux_session"`
	SessionID   string `json:"session_id"`
	ExitFile    string `json:"exit_file"` // in-container path the sentinel is written to
}

// headlessRunsDir is where per-run files (prompt, system prompt, launch script,
// exit sentinel) live inside the container, under the tool user's home.
func headlessRunsDir(homeDir string) string {
	return filepath.Join(homeDir, ".coi", "runs")
}

// buildHeadlessScript is the bash body run in the tmux pane for a headless
// launch: run the tool, record its exit code to the sentinel file, then drop to
// an interactive shell so the caller can still attach after the run finishes.
// toolCmd is the already-joined command (it may contain `"$(cat <file>)"`
// substitutions, which is exactly why it runs from a script file rather than
// through the nested-quote tmux wrapper the interactive path uses).
func buildHeadlessScript(toolCmd, workspacePath, exitFile string) string {
	return fmt.Sprintf(
		"trap : INT\ncd %s\n%s\necho $? > %s\nexec bash\n",
		shellQuote(workspacePath),
		toolCmd,
		shellQuote(exitFile),
	)
}

// buildHeadlessTmuxCmd builds the `tmux new-session` command that runs the
// launch script detached. Env is passed via `-e KEY=VALUE` (like the
// interactive path) so it never appears in `ps`. The pane command is a bare
// `bash <script>` — a simple path, no nested quoting — so the script's own
// content (including prompt substitutions) is what carries the complexity.
func buildHeadlessTmuxCmd(sessionName, workspacePath, scriptPath string, env map[string]string) string {
	var envFlags strings.Builder
	for _, k := range sortedEnvKeys(env) {
		envFlags.WriteString(" -e ")
		envFlags.WriteString(shellQuote(k + "=" + env[k]))
	}
	return fmt.Sprintf(
		"tmux new-session -d -s %s%s -c %s %s",
		shellQuote(sessionName),
		envFlags.String(),
		shellQuote(workspacePath),
		shellQuote("bash "+scriptPath),
	)
}

// launchHeadless drives a non-interactive tool run for an external orchestrator
// (#746): coi owns the tool translation (command, session id, resume, model,
// permission, env) and the caller supplies only the dynamics (prompt, system
// prompt, resume mode). It launches into the detached tmux session the caller
// can `coi tmux` attach/capture/send against, records a completion sentinel, and
// prints machine-readable handles.
func (a *App) launchHeadless(s *shellState) error {
	t := s.toolInstance
	mgr := s.result.Manager
	homeDir := s.result.HomeDir
	workspacePath := s.result.ContainerWorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace"
	}
	runsDir := headlessRunsDir(homeDir)
	tmuxSession := fmt.Sprintf("coi-%s", s.result.ContainerName)

	// The tool runs as the code user; write run files owned by it.
	uid := container.CodeUID
	if _, err := mgr.ExecCommand(fmt.Sprintf("mkdir -p %s && chown %d:%d %s",
		shellQuote(runsDir), uid, uid, shellQuote(runsDir)),
		container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create runs dir: %w", err)
	}

	// Stage prompt / system prompt as in-container files so their (arbitrary)
	// content never touches the launch command line.
	promptPath, err := a.stageHeadlessFile(mgr, a.promptFile, filepath.Join(runsDir, s.sessionID+".prompt"), uid)
	if err != nil {
		return err
	}
	systemPromptPath, err := a.stageHeadlessFile(mgr, a.systemPromptFile, filepath.Join(runsDir, s.sessionID+".sys"), uid)
	if err != nil {
		return err
	}

	// Resolve resume as continue-or-fresh: if resume was requested but no prior
	// session exists, start fresh instead of failing.
	spec := tool.LaunchSpec{
		SessionID:        s.sessionID,
		PromptFile:       promptPath,
		SystemPromptFile: systemPromptPath,
	}
	if s.resumeID != "" {
		if prior := discoverResumeSessionID(t, s.sessionsDir, s.resumeID); prior != "" {
			spec.Resume = true
			spec.ResumeSessionID = prior
		}
	}

	toolCmd, paste, err := a.buildHeadlessToolCmd(t, spec)
	if err != nil {
		return err
	}

	// Write the launch script and record the exit-sentinel path.
	exitFile := filepath.Join(runsDir, s.sessionID+".exit")
	scriptPath := filepath.Join(runsDir, s.sessionID+".sh")
	script := buildHeadlessScript(toolCmd, workspacePath, exitFile)
	if err := mgr.CreateFileWithOwner(scriptPath, script, uid, uid, "0755"); err != nil {
		return fmt.Errorf("failed to write launch script: %w", err)
	}

	// Env: same resolution the interactive launch uses (forwarded vars + tool
	// container env). #744 also applies model/effort at the container level, so
	// this is belt-and-suspenders for the pane.
	containerEnv, userPtr, err := a.buildContainerEnv(s.result)
	if err != nil {
		return err
	}
	mergeToolEnv(containerEnv, t, workspacePath)

	if err := runPreLaunch(mgr, t, container.ExecCommandOptions{
		User: userPtr, Cwd: workspacePath, Env: containerEnv, Capture: true,
	}); err != nil {
		return err
	}
	ensureTmuxServer(mgr, userPtr)

	launchCmd := buildHeadlessTmuxCmd(tmuxSession, workspacePath, scriptPath, containerEnv)
	if _, err := mgr.ExecCommand(launchCmd, container.ExecCommandOptions{Capture: true, User: userPtr}); err != nil {
		return fmt.Errorf("failed to start headless tmux session: %w", err)
	}
	for _, cmd := range buildTmuxSetEnvironmentCmds(tmuxSession, containerEnv) {
		if _, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true, User: userPtr}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to set tmux env: %v\n", err)
		}
	}

	// Tools without native prompt embedding (opencode) get the prompt pasted
	// into the pane once the TUI has had a moment to come up.
	if paste && promptPath != "" {
		a.pasteHeadlessPrompt(mgr, userPtr, tmuxSession, promptPath)
	}

	res := headlessResult{
		Container:   s.result.ContainerName,
		TmuxSession: tmuxSession,
		SessionID:   s.sessionID,
		ExitFile:    exitFile,
	}
	return a.emitHeadlessResult(res)
}

// stageHeadlessFile copies a host prompt file into the container at destPath and
// returns destPath; an empty hostPath returns "" (no file, no path emitted).
func (a *App) stageHeadlessFile(mgr container.ContainerManager, hostPath, destPath string, uid int) (string, error) {
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

// buildHeadlessToolCmd returns the joined launch command for the tool and
// whether the initial prompt still needs to be pasted into the pane (true for
// tools that don't implement ToolWithPrompt). It applies the dummy-mode override
// used by tests, mirroring buildCLICommand.
func (a *App) buildHeadlessToolCmd(t tool.Tool, spec tool.LaunchSpec) (cmd string, paste bool, err error) {
	var argv []string
	if twp, ok := t.(tool.ToolWithPrompt); ok {
		argv, err = twp.BuildCommandLaunch(spec)
		if err != nil {
			return "", false, err
		}
	} else {
		if spec.SystemPromptFile != "" {
			return "", false, fmt.Errorf("tool %q has no system-prompt support for headless launch", t.Name())
		}
		argv = t.BuildCommand(spec.SessionID, spec.Resume, spec.ResumeSessionID)
		paste = spec.PromptFile != ""
	}
	if os.Getenv("COI_USE_DUMMY") == "1" && len(argv) > 0 {
		argv[0] = "dummy"
	}
	return strings.Join(argv, " "), paste, nil
}

// pasteHeadlessPrompt delivers the initial prompt to a tool launched without
// native prompt embedding, via tmux's buffer (file-based, so arbitrary content
// is safe) after a short delay for the TUI to become ready.
func (a *App) pasteHeadlessPrompt(mgr container.ContainerManager, userPtr *int, tmuxSession, promptPath string) {
	time.Sleep(2 * time.Second)
	cmds := []string{
		fmt.Sprintf("tmux load-buffer %s", shellQuote(promptPath)),
		fmt.Sprintf("tmux paste-buffer -t %s", shellQuote(tmuxSession)),
		fmt.Sprintf("tmux send-keys -t %s Enter", shellQuote(tmuxSession)),
	}
	for _, c := range cmds {
		if _, err := mgr.ExecCommand(c, container.ExecCommandOptions{Capture: true, User: userPtr}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to paste prompt: %v\n", err)
			return
		}
	}
}

// emitHeadlessResult prints the run handles, as JSON when --json is set.
func (a *App) emitHeadlessResult(res headlessResult) error {
	if a.jsonOutput {
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Fprintf(os.Stderr, "Headless session started.\n")
	fmt.Fprintf(os.Stderr, "  Container:    %s\n", res.Container)
	fmt.Fprintf(os.Stderr, "  Tmux session: %s\n", res.TmuxSession)
	fmt.Fprintf(os.Stderr, "  Session ID:   %s\n", res.SessionID)
	fmt.Fprintf(os.Stderr, "Attach: coi tmux capture %s | Send: coi tmux send %s <text> | Wait: coi tmux status %s --session-id %s --wait\n",
		res.Container, res.Container, res.Container, res.SessionID)
	return nil
}

// discoverResumeSessionID mirrors buildCLICommand's resume discovery: it looks
// up the tool's internal session id from saved state, returning "" when none
// exists (so headless resume degrades to a fresh start).
func discoverResumeSessionID(t tool.Tool, sessionsDir, resumeID string) string {
	var statePath string
	if configDir := t.ConfigDirName(); configDir != "" {
		statePath = filepath.Join(sessionsDir, resumeID, configDir)
	} else {
		statePath = filepath.Join(sessionsDir, resumeID)
	}
	return t.DiscoverSessionID(statePath)
}
