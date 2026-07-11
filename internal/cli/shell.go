package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/monitor"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/nftmonitor"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/terminal"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/spf13/cobra"
)

var (
	debugShell    bool
	background    bool
	containerName string
)

var shellCmd = &cobra.Command{
	Use:   "shell [alias]",
	Short: "Start an interactive AI coding session",
	Long: `Start an interactive AI coding session in a container.

By default, runs Claude Code. Other tools can be configured via the tool.name config option.

Sessions run in tmux by default for monitoring and detach/reattach support.
Set [shell] use_tmux = false in config (or a profile) to run without tmux
(direct mode) — like all config-shaped settings, this has no CLI flag.

Tmux mode (default):
  - Interactive: Automatically attaches to tmux session
  - Background: Runs detached, use 'coi tmux capture' to view output
  - Detach anytime: Ctrl+B d (session keeps running)
  - Reattach: Run 'coi shell' again in same workspace

An optional alias argument launches a session from the registered alias's workspace
and profile, from any directory. Register an alias with [container] alias = "name"
in .coi/config.toml.

Examples:
  coi shell                         # Interactive session in tmux
  coi shell myproject               # Launch session using alias (from any directory)
  coi shell --profile opencode      # Tool/behavior via a profile ([tool] name = "opencode")
  coi shell --background            # Run in background (detached, tmux only)
  coi shell --resume                # Resume latest session (auto)
  coi shell --resume=<session-id>   # Resume specific session (note: = is required)
  coi shell --continue=<session-id> # Same as --resume (alias)
  coi shell --slot 2                # Use specific slot
  coi shell --debug                 # Launch bash for debugging
`,
	Args: cobra.MaximumNArgs(1),
	RunE: app.shellCommand,
}

func init() {
	shellCmd.Flags().BoolVar(&debugShell, "debug", false, "Launch interactive bash instead of AI tool (for debugging)")
	shellCmd.Flags().BoolVar(&background, "background", false, "Run AI tool in background tmux session (detached)")
	shellCmd.Flags().StringVar(&containerName, "container", "", "Use existing container (for testing)")
}

func (a *App) shellCommand(cmd *cobra.Command, args []string) error {
	// Create a context that is cancelled on SIGINT/SIGTERM so session.Setup
	// can abort container provisioning on Ctrl+C instead of leaving orphaned
	// containers.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := &shellState{}
	if len(args) > 0 {
		s.aliasArg = args[0]
	}

	pipeline := &session.Pipeline{}
	defer pipeline.Teardown()

	// Signal handler: explicitly trigger cleanup when SIGINT/SIGTERM arrives
	// while runCLI is blocking on an interactive incus exec.
	//
	// We cannot use ctx.Done() as the "signal received" branch because
	// signal.NotifyContext cancels ctx AND delivers to sigChan at the same
	// time — a select over both is non-deterministic. If ctx.Done() is chosen,
	// the goroutine exits without calling Teardown, leaving cleanup to the
	// deferred call, which won't run until the blocking incus exec returns.
	//
	// Instead we use a dedicated `done` channel closed when shellCommand
	// returns. On signal, cleanup is always called. On normal return, the
	// goroutine exits via the done branch without a second cleanup attempt
	// (pipeline.Teardown is idempotent). signal.Stop ensures no further
	// signals are queued after shellCommand returns.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigChan:
			fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, cleaning up...\n")
			pipeline.Teardown()
		case <-done:
		}
	}()

	return pipeline.Run(ctx,
		a.resolveWorkspacePhase(cmd, s),
		a.validateEnvPhase(cmd, s),
		a.configureSessionPhase(cmd, s),
		a.setupContainerPhase(s),
		a.startMonitoringPhase(s),
		a.runToolPhase(s),
	)
}

// getConfiguredTool returns the tool to use based on config
func getConfiguredTool(cfg *config.Config) (tool.Tool, error) {
	toolName := cfg.Tool.Name
	if toolName == "" {
		toolName = "claude" // Default to claude if not configured
	}

	t, err := tool.Get(toolName)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool '%s': %w", toolName, err)
	}

	// Set effort level if the tool supports it (Claude-specific)
	if twel, ok := t.(tool.ToolWithEffortLevel); ok {
		effortLevel := cfg.Tool.Claude.EffortLevel
		// If not configured, the tool's GetSandboxSettings will use its default
		if effortLevel != "" {
			twel.SetEffortLevel(effortLevel)
		}
	}

	// Set permission mode if the tool supports it
	if twpm, ok := t.(tool.ToolWithPermissionMode); ok {
		if cfg.Tool.PermissionMode != "" {
			twpm.SetPermissionMode(cfg.Tool.PermissionMode)
		}
	}

	return t, nil
}

// resolveGitIdentity picks the commit identity to install in the container, in
// precedence order:
//
//  1. an explicit [git] name/email from trusted-scope config (the sanitizer
//     strips these from untrusted project config), so a user can pin a container
//     identity distinct from their host git config;
//  2. otherwise, when seed_host_identity is enabled (the default), the user's
//     trusted global git config (user.name/user.email);
//  3. otherwise nothing — SetupGitIdentityGuard's user.useConfigOnly=true stays
//     the fail-closed boundary and the tool must set an identity itself.
//
// It never reads project-local git config, so an untrusted checkout cannot
// choose the author identity that will be installed in the container.
func resolveGitIdentity(gitCfg *config.GitConfig) session.GitIdentity {
	if gitCfg != nil {
		explicit := session.GitIdentity{Name: gitCfg.Name, Email: gitCfg.Email}
		if explicit.Complete() {
			return explicit
		}
	}
	if !gitCfg.IsSeedHostIdentityEnabled() {
		return session.GitIdentity{}
	}
	identity := session.GitIdentity{
		Name:  hostGlobalGitConfig("user.name"),
		Email: hostGlobalGitConfig("user.email"),
	}
	if !identity.Complete() {
		return session.GitIdentity{}
	}
	return identity
}

func hostGlobalGitConfig(key string) string {
	out, err := exec.Command("git", "config", "--global", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildCLICommand builds the CLI command string to execute in the container.
// It handles debug shell mode, session ID discovery, tool command building, and dummy mode override.
func buildCLICommand(sessionID string, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string, t tool.Tool) string {
	if debugShell {
		return "bash"
	}

	// Determine resume mode and CLI session ID
	var cliSessionID string
	if useResumeFlag || restoreOnly {
		// Try to discover the tool's internal session ID from saved state
		// The exact discovery mechanism is tool-specific (e.g. some tools read
		// config files, others use environment variables) and may return ""
		// if no previous session can be found (start fresh).
		var sessionStatePath string
		if configDir := t.ConfigDirName(); configDir != "" {
			sessionStatePath = filepath.Join(sessionsDir, resumeID, configDir)
		} else {
			sessionStatePath = filepath.Join(sessionsDir, resumeID)
		}
		cliSessionID = t.DiscoverSessionID(sessionStatePath)
	}

	// Build command using tool abstraction
	// This handles tool-specific flags (--verbose, --permission-mode, etc.)
	cmd := t.BuildCommand(sessionID, useResumeFlag || restoreOnly, cliSessionID)

	// Handle dummy mode override (for testing)
	if os.Getenv("COI_USE_DUMMY") == "1" {
		if len(cmd) > 0 {
			cmd[0] = "dummy"
		}
		fmt.Fprintf(os.Stderr, "Using dummy (test stub) for faster testing\n")
	}

	return strings.Join(cmd, " ")
}

// buildContainerEnv constructs the environment variables map and user pointer for container execution.
// It sets HOME, TERM (sanitized), IS_SANDBOX, merges config environment, resolves forward_env, and
// runs env_commands (host commands whose trimmed stdout is injected). A failing env_command is fatal.
func (a *App) buildContainerEnv(result *session.SetupResult) (map[string]string, *int, error) {
	// Exec as the UID setup resolved AND recorded as user.coi.uid — the same
	// authority coi tmux / coi attach read back, so the session's tmux socket
	// and its consumers agree by construction (#588).
	user := result.ExecUID
	userPtr := &user

	containerEnv := map[string]string{
		"HOME":       result.HomeDir,
		"TERM":       terminal.SanitizeTerm(os.Getenv("TERM")),
		"IS_SANDBOX": "1",
	}

	// Set env vars for every forwarded socket (SSH_AUTH_SOCK for the SSH agent,
	// plus any configured [[sockets]] entries with an `env` name).
	for env, path := range result.SocketEnv {
		containerEnv[env] = path
	}

	// Set TZ if timezone was configured
	if result.Timezone != "" {
		containerEnv["TZ"] = result.Timezone
	}

	// Apply static environment from config (defaults.environment + profile environment)
	for k, v := range a.cfg.Defaults.Environment {
		containerEnv[k] = v
	}

	// Resolve forward_env from config, deduplicate, then look up host values
	for _, name := range a.cfg.Defaults.ForwardEnv {
		if val, ok := os.LookupEnv(name); ok {
			containerEnv[name] = val
		} else {
			fmt.Fprintf(os.Stderr, "Warning: forward_env variable %q is not set on host, skipping\n", name)
		}
	}

	// Command-sourced env vars (highest precedence — freshly minted per session).
	// Applied last so a minted value wins over static environment/forward_env.
	envCommandValues, err := a.resolveEnvCommands()
	if err != nil {
		return nil, nil, err
	}
	for k, v := range envCommandValues {
		containerEnv[k] = v
	}

	// Sanitize TERM if user explicitly provided it via config
	if userTerm, exists := containerEnv["TERM"]; exists {
		containerEnv["TERM"] = terminal.SanitizeTerm(userTerm)
	}

	return containerEnv, userPtr, nil
}

// resolveForwardedEnvVarNames returns the subset of env var names that are actually
// set on the host. This is used to inform the context file about what's available.
func resolveForwardedEnvVarNames(names []string) []string {
	var resolved []string
	for _, name := range names {
		if _, ok := os.LookupEnv(name); ok {
			resolved = append(resolved, name)
		}
	}
	return resolved
}

// ensureTmuxServer starts the tmux server and polls until it is ready (up to 2 seconds).
// This is critical in CI and for newly started containers where the tmux server might not be running yet.
func ensureTmuxServer(mgr container.ContainerExecution, userPtr *int) {
	serverStartCmd := "tmux start-server 2>/dev/null || true; sleep 0.1"
	serverOpts := container.ExecCommandOptions{
		Capture: true,
		User:    userPtr,
	}
	_, _ = mgr.ExecCommand(serverStartCmd, serverOpts) // Best-effort server start.

	// Poll to ensure server is ready (up to 2 seconds)
	for i := 0; i < 20; i++ {
		checkServerCmd := "tmux list-sessions 2>&1 | grep -v 'no server running' || true"
		_, err := mgr.ExecCommand(checkServerCmd, serverOpts)
		if err == nil {
			break // Server is ready
		}
		_, _ = mgr.ExecCommand("sleep 0.1", serverOpts) // Best-effort sleep.
	}
}

// mergeToolEnv adds tool-specific environment variables (if the tool implements ToolWithContainerEnv).
func mergeToolEnv(env map[string]string, t tool.Tool, workspacePath string) {
	if twce, ok := t.(tool.ToolWithContainerEnv); ok {
		for k, v := range twce.GetContainerEnv(workspacePath) {
			env[k] = v
		}
	}
}

// runPreLaunch executes any pre-launch commands for the tool inside the container.
// Commands are run via ExecArgs (no shell interpretation) to prevent injection.
// Returns an error if any pre-launch command fails.
func runPreLaunch(mgr container.ContainerManager, t tool.Tool, opts container.ExecCommandOptions) error {
	pl, ok := t.(tool.ToolWithPreLaunch)
	if !ok {
		return nil
	}
	for _, argv := range pl.PreLaunch() {
		if len(argv) == 0 {
			continue
		}
		if err := mgr.ExecArgs(argv, opts); err != nil {
			return fmt.Errorf("pre-launch command %v failed: %w", argv, err)
		}
	}
	return nil
}

// runCLI executes the CLI tool in the container interactively
func (a *App) runCLI(result *session.SetupResult, sessionID string, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string, t tool.Tool) error {
	cmdToRun := buildCLICommand(sessionID, useResumeFlag, restoreOnly, sessionsDir, resumeID, t)
	containerEnv, userPtr, err := a.buildContainerEnv(result)
	if err != nil {
		return err
	}

	workspacePath := result.ContainerWorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace" // Fallback for backwards compatibility
	}
	mergeToolEnv(containerEnv, t, workspacePath)
	opts := container.ExecCommandOptions{
		User:        userPtr,
		Cwd:         workspacePath,
		Env:         containerEnv,
		Interactive: true, // Attach stdin/stdout/stderr for interactive session
	}

	// Run pre-launch commands (e.g., symlink creation) before the tool starts
	if err := runPreLaunch(result.Manager, t, container.ExecCommandOptions{
		User:    userPtr,
		Cwd:     workspacePath,
		Env:     containerEnv,
		Capture: true,
	}); err != nil {
		return err
	}

	_, err = result.Manager.ExecCommand(cmdToRun, opts)
	return err
}

// sortedEnvKeys returns env's keys in lexicographic order so that command
// strings built from env are deterministic (and unit-testable).
func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildTmuxNewSessionCmd builds the shell command that creates a detached tmux
// session. Forwarded variables are passed via tmux's `-e KEY=VALUE` flags
// rather than as `export` statements, so they don't appear in `ps` output and
// are not part of the inherited shell environment.
func buildTmuxNewSessionCmd(sessionName, workspacePath, cliCmd string, env map[string]string) string {
	var envFlags strings.Builder
	for _, k := range sortedEnvKeys(env) {
		envFlags.WriteString(" -e ")
		envFlags.WriteString(shellQuote(k + "=" + env[k]))
	}
	return fmt.Sprintf(
		"tmux new-session -d -s %s%s -c %s \"bash -c 'trap : INT; %s; exec bash'\"",
		shellQuote(sessionName),
		envFlags.String(),
		shellQuote(workspacePath),
		cliCmd,
	)
}

// buildTmuxSetEnvironmentCmds returns one `tmux set-environment` command per
// entry in env, scoped to the given session so values don't leak across other
// tmux sessions sharing the same server. Running these after `new-session`
// makes the variables available to any window or pane created later (which
// would otherwise inherit the tmux server's near-empty environment).
func buildTmuxSetEnvironmentCmds(sessionName string, env map[string]string) []string {
	cmds := make([]string, 0, len(env))
	for _, k := range sortedEnvKeys(env) {
		cmds = append(cmds, fmt.Sprintf(
			"tmux set-environment -t %s %s %s",
			shellQuote(sessionName),
			shellQuote(k),
			shellQuote(env[k]),
		))
	}
	return cmds
}

// runCLIInTmux executes CLI tool in a tmux session for background/monitoring support
func (a *App) runCLIInTmux(result *session.SetupResult, sessionID string, detached bool, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string, t tool.Tool) error {
	tmuxSessionName := fmt.Sprintf("coi-%s", result.ContainerName)

	// Get workspace path (with fallback for backwards compatibility)
	workspacePath := result.ContainerWorkspacePath
	if workspacePath == "" {
		workspacePath = "/workspace"
	}

	cliCmd := buildCLICommand(sessionID, useResumeFlag, restoreOnly, sessionsDir, resumeID, t)
	containerEnv, userPtr, err := a.buildContainerEnv(result)
	if err != nil {
		return err
	}
	mergeToolEnv(containerEnv, t, workspacePath)

	// Run pre-launch commands (e.g., symlink creation) before the tool starts
	if err := runPreLaunch(result.Manager, t, container.ExecCommandOptions{
		User:    userPtr,
		Cwd:     workspacePath,
		Env:     containerEnv,
		Capture: true,
	}); err != nil {
		return err
	}

	// Ensure tmux server is running first (critical for CI and new containers)
	ensureTmuxServer(result.Manager, userPtr)

	// applyTmuxEnv populates the session's update-environment so windows and
	// panes opened later inherit the forwarded variables. Idempotent: safe to
	// call on a session that was already populated.
	applyTmuxEnv := func() {
		for _, cmd := range buildTmuxSetEnvironmentCmds(tmuxSessionName, containerEnv) {
			if _, err := result.Manager.ExecCommand(cmd, container.ExecCommandOptions{
				Capture: true,
				User:    userPtr,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to set tmux session env (%s): %v\n", cmd, err)
			}
		}
	}

	// Check if tmux session already exists
	checkSessionCmd := fmt.Sprintf("tmux has-session -t %s 2>/dev/null", tmuxSessionName)
	_, err = result.Manager.ExecCommand(checkSessionCmd, container.ExecCommandOptions{
		Capture: true,
		User:    userPtr,
	})

	if err == nil {
		// Re-apply forwarded env so panes opened from now on inherit it,
		// even if the session was created by an older binary that didn't
		// populate the update-environment.
		applyTmuxEnv()

		// Session exists - attach or send command
		if detached {
			// Send command to existing session
			sendCmd := fmt.Sprintf("tmux send-keys -t %s %q Enter", tmuxSessionName, cliCmd)
			_, err := result.Manager.ExecCommand(sendCmd, container.ExecCommandOptions{
				Capture: true,
				User:    userPtr,
			})
			if err != nil {
				return fmt.Errorf("failed to send command to existing tmux session: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Sent command to existing tmux session: %s\n", tmuxSessionName)
			fmt.Fprintf(os.Stderr, "Use 'coi tmux capture %s' to view output\n", result.ContainerName)
			return nil
		} else {
			// Attach to existing session
			fmt.Fprintf(os.Stderr, "Attaching to existing tmux session: %s\n", tmuxSessionName)
			attachCmd := fmt.Sprintf("tmux attach -t %s", tmuxSessionName)
			opts := container.ExecCommandOptions{
				User:        userPtr,
				Cwd:         workspacePath,
				Interactive: true,
			}
			_, err := result.Manager.ExecCommand(attachCmd, opts)
			return err
		}
	}

	// Create new tmux session
	// When claude exits, fall back to bash so user can still interact
	// User can then: exit (leaves container running), Ctrl+b d (detach), or sudo shutdown 0 (stop)
	// Use trap to prevent bash from exiting on SIGINT while allowing Ctrl+C to work in claude
	if detached {
		// Background mode: create detached session
		createCmd := buildTmuxNewSessionCmd(tmuxSessionName, workspacePath, cliCmd, containerEnv)
		opts := container.ExecCommandOptions{
			Capture: true,
			User:    userPtr,
		}
		_, err := result.Manager.ExecCommand(createCmd, opts)
		if err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
		applyTmuxEnv()

		fmt.Fprintf(os.Stderr, "Created background tmux session: %s\n", tmuxSessionName)
		fmt.Fprintf(os.Stderr, "Use 'coi tmux capture %s' to view output\n", result.ContainerName)
		fmt.Fprintf(os.Stderr, "Use 'coi tmux send %s \"<command>\"' to send commands\n", result.ContainerName)
		return nil
	} else {
		// Interactive mode: create detached session, then attach
		// This ensures tmux server owns the session, not the incus exec process
		// When we detach, only the attach process exits, not the session
		// trap : INT prevents bash from exiting on Ctrl+C, exec bash replaces (no nested shells)

		// Check if session already exists (it was checked above but may have been
		// created by another process in the meantime)
		checkCmd := fmt.Sprintf("tmux has-session -t %s 2>/dev/null", tmuxSessionName)
		checkOpts := container.ExecCommandOptions{
			User:    userPtr,
			Capture: true,
		}
		_, checkErr := result.Manager.ExecCommand(checkCmd, checkOpts)

		// Create detached session if it doesn't exist
		if checkErr != nil {
			createCmd := buildTmuxNewSessionCmd(tmuxSessionName, workspacePath, cliCmd, containerEnv)
			createOpts := container.ExecCommandOptions{
				User:    userPtr,
				Cwd:     workspacePath,
				Capture: true,
			}
			if _, err := result.Manager.ExecCommand(createCmd, createOpts); err != nil {
				return fmt.Errorf("failed to create tmux session: %w", err)
			}

			// Poll until tmux reports the session is ready (up to 3s).
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				_, pollErr := result.Manager.ExecCommand(checkCmd, checkOpts)
				if pollErr == nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
		applyTmuxEnv()

		// Attach to the session
		attachCmd := fmt.Sprintf("tmux attach -t %s", tmuxSessionName)
		attachOpts := container.ExecCommandOptions{
			User:        userPtr,
			Cwd:         workspacePath,
			Interactive: true,
			Env:         containerEnv,
		}
		_, err := result.Manager.ExecCommand(attachCmd, attachOpts)
		return err
	}
}

// resolveTimezone determines the timezone to apply to the container.
// Returns an IANA timezone name (e.g., "America/New_York") or "" for UTC/undetected.
func resolveTimezone(cfg *config.Config) string {
	// Use config
	switch strings.ToLower(cfg.Timezone.Mode) {
	case "host", "":
		return detectHostTimezone()
	case "fixed":
		if container.ValidateTimezone(cfg.Timezone.Name) {
			return cfg.Timezone.Name
		}
		fmt.Fprintf(os.Stderr, "Warning: invalid timezone.name %q in config, falling back to UTC\n", cfg.Timezone.Name)
		return ""
	case "utc":
		return ""
	default:
		fmt.Fprintf(os.Stderr, "Warning: unknown timezone.mode %q, falling back to host detection\n", cfg.Timezone.Mode)
		return detectHostTimezone()
	}
}

// detectHostTimezone wraps container.DetectHostTimezone with warning output
func detectHostTimezone() string {
	tz, err := container.DetectHostTimezone()
	if err != nil || tz == "" {
		fmt.Fprintf(os.Stderr, "Warning: could not detect host timezone, container will use UTC\n")
		return ""
	}
	return tz
}

// startMonitoringDaemon starts the background monitoring daemon
func startMonitoringDaemon(ctx context.Context, containerName, workspacePath string, cfg *config.Config, allowedCIDRs []string, log *logger.SessionLogger, daemon *monitor.MonitorDaemon) error {
	// Get home directory for audit log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	auditLogPath := filepath.Join(homeDir, ".coi", "audit", containerName+".jsonl")

	// Create daemon config
	daemonCfg := monitor.DaemonConfig{
		ContainerName:             containerName,
		WorkspacePath:             workspacePath,
		PollInterval:              time.Duration(cfg.Monitoring.PollIntervalSec) * time.Second,
		AuditLogPath:              auditLogPath,
		AllowedCIDRs:              allowedCIDRs,
		AllowedDomains:            cfg.Network.AllowedDomains,
		GTFOBinsDir:               cfg.Detection.GTFOBinsDir,
		SigmaDir:                  cfg.Detection.SigmaDir,
		FileReadThresholdMB:       cfg.Monitoring.FileReadThresholdMB,
		FileReadRateMBPerSec:      cfg.Monitoring.FileReadRateMBPerSec,
		ProcessCountThreshold:     cfg.Monitoring.ProcessCountThreshold,
		ProcessSpawnRateThreshold: config.IntVal(cfg.Monitoring.ProcessSpawnRateThreshold),
		AutoPauseOnHigh:           config.BoolVal(cfg.Monitoring.AutoPauseOnHigh),
		AutoKillOnCritical:        config.BoolVal(cfg.Monitoring.AutoKillOnCritical),
		OnThreat: func(threat monitor.ThreatEvent) {
			log.Printf("[monitor] threat detected: %s severity=%s", threat.Title, threat.Level)
		},
		OnError: func(err error) {
			// Generic label: this callback now carries both collection errors and
			// the responder's kill-path cleanup warnings (which are self-describing).
			log.Errorf("[monitor] error: %v", err)
		},
		OnAction: func(action, message string) {
			// Record the security action durably in the session log, and surface
			// it on the terminal. OnAction fires ONLY on auto-pause/kill — both of
			// which freeze or end the session — so unlike the recurring background
			// diagnostics that motivated issue #372 this one-shot banner does not
			// corrupt a live TUI, and for the pause path it carries the only
			// on-screen "coi unfreeze" recovery hint (the session would otherwise
			// just appear to hang).
			log.Errorf("[security] %s", message)
			fmt.Fprintf(os.Stderr, "\n\n*** SECURITY: %s ***\n\n", message)
		},
	}

	// Start daemon
	d, err := monitor.StartDaemon(ctx, daemonCfg)
	if err != nil {
		return err
	}

	*daemon = d
	fmt.Fprintf(os.Stderr, "[security] Process/filesystem monitoring started (audit log: %s)\n", auditLogPath)
	return nil
}

// startNFTMonitoringDaemon starts the nftables network monitoring daemon
func startNFTMonitoringDaemon(ctx context.Context, containerName string, cfg *config.Config, allowedCIDRs []string, log *logger.SessionLogger, daemon *nftmonitor.NFTMonitorDaemon) error {
	// Route the nft monitor's COI_NFT_DEBUG diagnostics to the session log
	// instead of the user's attached terminal (issue #372 class).
	nftmonitor.SetLogger(log)

	// Get container IP. 30 retries (matching restricted-mode network setup's
	// DHCP wait): in open mode nothing upstream waits for the lease, and the
	// run pipeline reaches this phase seconds after launch — 3 retries (~2 s)
	// intermittently missed the IP under load, silently skipping nft
	// monitoring with only a stderr warning.
	containerIP, err := network.GetContainerIPWithRetries(containerName, 30)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}

	// Get gateway IP
	gatewayIP, err := network.GetContainerGatewayIP(containerName)
	if err != nil {
		// Non-fatal - we can still monitor without gateway IP check
		gatewayIP = ""
	}

	// Get home directory for audit log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	auditLogPath := filepath.Join(homeDir, ".coi", "audit", containerName+"-nft.jsonl")

	// Create NFT daemon config
	nftCfg := nftmonitor.Config{
		ContainerName:      containerName,
		ContainerIP:        containerIP,
		AllowedCIDRs:       allowedCIDRs,
		GatewayIP:          gatewayIP,
		AuditLogPath:       auditLogPath,
		RateLimitPerSecond: cfg.Monitoring.NFT.RateLimitPerSecond,
		DNSQueryThreshold:  cfg.Monitoring.NFT.DNSQueryThreshold,
		LogDNSQueries:      config.BoolVal(cfg.Monitoring.NFT.LogDNSQueries),
		LimaHost:           cfg.Monitoring.NFT.LimaHost,
		OnThreat: func(threat nftmonitor.ThreatEvent) {
			log.Printf("[nft] threat detected: %s severity=%s", threat.Title, threat.Level)
		},
		OnAction: func(action, message string) {
			// Record the security action durably in the session log, and surface
			// it on the terminal. OnAction fires ONLY on auto-pause/kill — both of
			// which freeze or end the session — so unlike the recurring background
			// diagnostics that motivated issue #372 this one-shot banner does not
			// corrupt a live TUI, and for the pause path it carries the only
			// on-screen "coi unfreeze" recovery hint (the session would otherwise
			// just appear to hang).
			log.Errorf("[security] %s", message)
			fmt.Fprintf(os.Stderr, "\n\n*** SECURITY: %s ***\n\n", message)
		},
		OnError: func(err error) {
			log.Errorf("[nft] error: %v", err)
		},
	}

	// Start daemon
	d, err := nftmonitor.StartDaemon(ctx, nftCfg)
	if err != nil {
		return err
	}

	*daemon = d
	fmt.Fprintf(os.Stderr, "[security] NFT network monitoring started (audit log: %s)\n", auditLogPath)
	return nil
}

func resolveDomainsToHostCIDRs(domains []string) []string {
	if len(domains) == 0 {
		return nil
	}
	resolver := network.NewResolver(&network.IPCache{})
	resolved, err := resolver.ResolveAll(domains)
	if err != nil {
		// Route through the network session logger, not os.Stderr — in a coi
		// shell os.Stderr is the user's tmux terminal (issue #372). The resolver
		// already logs each failed domain to the session log; this records the
		// monitoring-phase context alongside it.
		network.Warnf("Warning: some allowed domains failed to resolve for monitoring: %v", err)
	}
	if len(resolved) == 0 {
		return nil
	}
	var cidrs []string
	for _, ips := range resolved {
		for _, ip := range ips {
			if strings.Contains(ip, "/") {
				cidrs = append(cidrs, ip)
			} else {
				cidrs = append(cidrs, ip+"/32")
			}
		}
	}
	return cidrs
}
