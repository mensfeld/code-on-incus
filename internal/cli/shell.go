package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mensfeld/claude-on-incus/internal/config"
	"github.com/mensfeld/claude-on-incus/internal/container"
	"github.com/mensfeld/claude-on-incus/internal/session"
	"github.com/spf13/cobra"
)

var (
	debugShell bool
	background bool
	useTmux    bool
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive Claude session",
	Long: `Start an interactive Claude Code session in a container (always runs in tmux).

All sessions run in tmux for monitoring and detach/reattach support:
  - Interactive: Automatically attaches to tmux session
  - Background: Runs detached, use 'coi tmux capture' to view output
  - Detach anytime: Ctrl+B d (session keeps running)
  - Reattach: Run 'coi shell' again in same workspace

Examples:
  coi shell                         # Interactive session in tmux
  coi shell --background            # Run in background (detached)
  coi shell --resume                # Resume latest session (auto)
  coi shell --resume=<session-id>   # Resume specific session (note: = is required)
  coi shell --continue=<session-id> # Same as --resume (alias)
  coi shell --slot 2                # Use specific slot
  coi shell --debug                 # Launch bash for debugging
`,
	RunE: shellCommand,
}

func init() {
	shellCmd.Flags().BoolVar(&debugShell, "debug", false, "Launch interactive bash instead of Claude (for debugging)")
	shellCmd.Flags().BoolVar(&background, "background", false, "Run Claude in background tmux session (detached)")
	shellCmd.Flags().BoolVar(&useTmux, "tmux", true, "Use tmux for session management (default true)")
}

func shellCommand(cmd *cobra.Command, args []string) error {
	// Validate no unexpected positional arguments
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument '%s' - did you mean --resume=%s? (note: use = when specifying session ID)", args[0], args[0])
	}

	// Get absolute workspace path
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	// Check if Incus is available
	if !container.Available() {
		return fmt.Errorf("incus is not available - please install Incus and ensure you're in the incus-admin group")
	}

	// Get sessions directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	sessionsDir := filepath.Join(homeDir, ".coi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	// Handle resume flag (--resume or --continue)
	resumeID := resume
	if continueSession != "" {
		resumeID = continueSession // --continue takes precedence if both are provided
	}

	// Check if resume/continue flag was explicitly set
	resumeFlagSet := cmd.Flags().Changed("resume") || cmd.Flags().Changed("continue")

	// Auto-detect if flag was set but value is empty or "auto"
	if resumeFlagSet && (resumeID == "" || resumeID == "auto") {
		// Auto-detect latest for workspace (only looks at sessions from the same workspace)
		resumeID, err = session.GetLatestSessionForWorkspace(sessionsDir, absWorkspace)
		if err != nil {
			return fmt.Errorf("no previous session to resume for this workspace: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Auto-detected session: %s\n", resumeID)
	} else if resumeID != "" {
		// Validate that the explicitly provided session exists
		if !session.SessionExists(sessionsDir, resumeID) {
			return fmt.Errorf("session '%s' not found - check available sessions with: coi list --all", resumeID)
		}
		fmt.Fprintf(os.Stderr, "Resuming session: %s\n", resumeID)
	}

	// When resuming, inherit persistent flag from the original session
	// unless it was explicitly overridden by the user
	if resumeID != "" {
		metadataPath := filepath.Join(sessionsDir, resumeID, "metadata.json")
		if metadata, err := session.LoadSessionMetadata(metadataPath); err == nil {
			// Inherit persistent flag if not explicitly set by user
			if !cmd.Flags().Changed("persistent") {
				persistent = metadata.Persistent
				if persistent {
					fmt.Fprintf(os.Stderr, "Inherited persistent mode from session\n")
				}
			}
		}
	}

	// Generate or use session ID
	var sessionID string
	if resumeID != "" {
		sessionID = resumeID // Reuse the same session ID when resuming
	} else {
		sessionID, err = session.GenerateSessionID()
		if err != nil {
			return err
		}
	}

	// Allocate slot - always check for availability and auto-increment if needed
	slotNum := slot
	if slotNum == 0 {
		// No slot specified, find first available
		slotNum, err = session.AllocateSlot(absWorkspace, 10)
		if err != nil {
			return fmt.Errorf("failed to allocate slot: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Auto-allocated slot %d\n", slotNum)
	} else {
		// Slot specified, but check if it's available
		// If not, find next available slot starting from the specified one
		available, err := session.IsSlotAvailable(absWorkspace, slotNum)
		if err != nil {
			return fmt.Errorf("failed to check slot availability: %w", err)
		}

		if !available {
			// Slot is occupied, find next available starting from slot+1
			originalSlot := slotNum
			slotNum, err = session.AllocateSlotFrom(absWorkspace, slotNum+1, 10)
			if err != nil {
				return fmt.Errorf("slot %d is occupied and failed to find next available slot: %w", originalSlot, err)
			}
			fmt.Fprintf(os.Stderr, "Slot %d is occupied, using slot %d instead\n", originalSlot, slotNum)
		}
	}

	// Prepare network configuration
	networkConfig := cfg.Network // Copy from loaded config
	// Override network mode from flag if specified
	if networkMode != "" {
		networkConfig.Mode = config.NetworkMode(networkMode)
	}

	// Setup session
	setupOpts := session.SetupOptions{
		WorkspacePath:    absWorkspace,
		Image:            imageName,
		Persistent:       persistent,
		ResumeFromID:     resumeID,
		Slot:             slotNum,
		SessionsDir:      sessionsDir,
		CLIConfigPath:    filepath.Join(homeDir, ".claude"),
		NetworkConfig:    &networkConfig,
	}

	if storage != "" {
		setupOpts.StoragePath = storage
	}

	fmt.Fprintf(os.Stderr, "Setting up session %s...\n", sessionID)
	result, err := session.Setup(setupOpts)
	if err != nil {
		return fmt.Errorf("failed to setup session: %w", err)
	}

	// Save metadata early so coi list shows correct persistent/ephemeral status
	if err := session.SaveMetadataEarly(sessionsDir, sessionID, result.ContainerName, absWorkspace, persistent); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save early metadata: %v\n", err)
	}

	// Setup cleanup on exit
	defer func() {
		fmt.Fprintf(os.Stderr, "\nCleaning up session...\n")
		cleanupOpts := session.CleanupOptions{
			ContainerName:  result.ContainerName,
			SessionID:      sessionID,
			Persistent:     persistent,
			SessionsDir:    sessionsDir,
			SaveSession:    true, // Always save session data
			Workspace:      absWorkspace,
			NetworkManager: result.NetworkManager,
		}
		if err := session.Cleanup(cleanupOpts); err != nil {
			fmt.Fprintf(os.Stderr, "Cleanup error: %v\n", err)
		}
	}()

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, cleaning up...\n")
		os.Exit(0) // Defer will run
	}()

	// Run Claude CLI
	fmt.Fprintf(os.Stderr, "\nStarting Claude session...\n")
	fmt.Fprintf(os.Stderr, "Session ID: %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "Container: %s\n", result.ContainerName)
	fmt.Fprintf(os.Stderr, "Workspace: %s\n", absWorkspace)

	// Determine resume mode
	// The difference is:
	// - Persistent: container is reused, .claude stays in container, pass --resume to Claude
	// - Ephemeral: container is recreated, we restore .claude dir, let Claude auto-detect session
	//
	// For persistent containers resuming: pass --resume flag with our session ID
	// For ephemeral containers resuming: just restore .claude, Claude will auto-detect from restored data
	useResumeFlag := (resumeID != "") && persistent
	restoreOnly := (resumeID != "") && !persistent

	// Choose execution mode
	if useTmux {
		if background {
			fmt.Fprintf(os.Stderr, "Mode: Background (tmux)\n")
		} else {
			fmt.Fprintf(os.Stderr, "Mode: Interactive (tmux)\n")
		}
		if restoreOnly {
			fmt.Fprintf(os.Stderr, "Resume mode: Restored conversation (auto-detect)\n")
		} else if useResumeFlag {
			fmt.Fprintf(os.Stderr, "Resume mode: Persistent session\n")
		}
		fmt.Fprintf(os.Stderr, "\n")
		err = runCLIInTmux(result, sessionID, background, useResumeFlag, restoreOnly, sessionsDir, resumeID)
	} else {
		fmt.Fprintf(os.Stderr, "Mode: Direct (no tmux)\n")
		if restoreOnly {
			fmt.Fprintf(os.Stderr, "Resume mode: Restored conversation (auto-detect)\n")
		} else if useResumeFlag {
			fmt.Fprintf(os.Stderr, "Resume mode: Persistent session\n")
		}
		fmt.Fprintf(os.Stderr, "\n")
		err = runCLI(result, sessionID, useResumeFlag, restoreOnly, sessionsDir, resumeID)
	}

	// Handle expected exit conditions gracefully
	if err != nil {
		errStr := err.Error()
		// Exit status 130 means interrupted by SIGINT (Ctrl+C) - this is normal
		if errStr == "exit status 130" {
			return nil
		}
		// Container shutdown from within (sudo shutdown 0) causes exec to fail
		// This can manifest as various errors depending on timing
		if strings.Contains(errStr, "Failed to retrieve PID") ||
			strings.Contains(errStr, "server exited") ||
			strings.Contains(errStr, "connection reset") ||
			errStr == "exit status 1" {
			// Don't print anything - cleanup will show appropriate message
			return nil
		}
	}

	return err
}

// getEnvValue checks for an env var in --env flags first, then os.Getenv
func getEnvValue(key string) string {
	// Check --env flags first
	for _, e := range envVars {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 && parts[0] == key {
			return parts[1]
		}
	}
	// Fall back to os.Getenv
	return os.Getenv(key)
}

// runCLI executes the CLI tool in the container interactively
func runCLI(result *session.SetupResult, sessionID string, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string) error {
	// Determine which CLI binary to use (real or dummy)
	cliBinary := "claude"
	if getEnvValue("COI_USE_DUMMY") == "1" {
		cliBinary = "dummy"
		fmt.Fprintf(os.Stderr, "Using dummy (test stub) for faster testing\n")
	}

	// Build command - either bash for debugging or CLI tool
	var cmdToRun string
	if debugShell {
		// Debug mode: launch interactive bash
		cmdToRun = "bash"
	} else {
		// Always use permission-mode bypassPermissions to skip all prompts
		permissionFlags := "--permission-mode bypassPermissions "

		// Build session flag:
		// - useResumeFlag or restoreOnly: use --resume with CLI's session ID
		// - neither: use --session-id for new sessions
		var sessionArg string
		if useResumeFlag || restoreOnly {
			// Resume mode: find CLI's session ID from saved data
			cliSessionID := session.GetCLISessionID(sessionsDir, resumeID)
			if cliSessionID != "" {
				sessionArg = fmt.Sprintf(" --resume %s", cliSessionID)
			} else {
				// Fallback to auto-detect if we can't find the session ID
				sessionArg = " --resume"
			}
		} else {
			sessionArg = fmt.Sprintf(" --session-id %s", sessionID)
		}

		cmdToRun = fmt.Sprintf("%s --verbose %s%s", cliBinary, permissionFlags, sessionArg)
	}

	// Execute in container
	user := container.CodeUID
	if result.RunAsRoot {
		user = 0
	}

	userPtr := &user

	// Build environment variables
	containerEnv := map[string]string{
		"HOME":       result.HomeDir,
		"TERM":       os.Getenv("TERM"), // Preserve terminal type
		"IS_SANDBOX": "1",               // Always set sandbox mode
	}

	// Merge user-provided --env vars
	for _, e := range envVars {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			containerEnv[parts[0]] = parts[1]
		}
	}

	opts := container.ExecCommandOptions{
		User:        userPtr,
		Cwd:         "/workspace",
		Env:         containerEnv,
		Interactive: true, // Attach stdin/stdout/stderr for interactive session
	}

	_, err := result.Manager.ExecCommand(cmdToRun, opts)
	return err
}

// runCLIInTmux executes CLI tool in a tmux session for background/monitoring support
func runCLIInTmux(result *session.SetupResult, sessionID string, detached bool, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string) error {
	tmuxSessionName := fmt.Sprintf("coi-%s", result.ContainerName)

	// Determine which CLI binary to use (real or dummy)
	cliBinary := "claude"
	if getEnvValue("COI_USE_DUMMY") == "1" {
		cliBinary = "dummy"
		fmt.Fprintf(os.Stderr, "Using dummy (test stub) for faster testing\n")
	}

	// Build CLI command
	var cliCmd string
	if debugShell {
		// Debug mode: launch interactive bash
		cliCmd = "bash"
	} else {
		// Always use permission-mode bypassPermissions
		permissionFlags := "--permission-mode bypassPermissions "

		// Build session flag:
		// - useResumeFlag or restoreOnly: use --resume with CLI's session ID
		// - neither: use --session-id for new sessions
		var sessionArg string
		if useResumeFlag || restoreOnly {
			// Resume mode: find CLI's session ID from saved data
			cliSessionID := session.GetCLISessionID(sessionsDir, resumeID)
			if cliSessionID != "" {
				sessionArg = fmt.Sprintf(" --resume %s", cliSessionID)
			} else {
				// Fallback to auto-detect if we can't find the session ID
				sessionArg = " --resume"
			}
		} else {
			sessionArg = fmt.Sprintf(" --session-id %s", sessionID)
		}

		cliCmd = fmt.Sprintf("%s --verbose %s%s", cliBinary, permissionFlags, sessionArg)
	}

	// Build environment variables
	user := container.CodeUID
	if result.RunAsRoot {
		user = 0
	}
	userPtr := &user

	// Get TERM with fallback
	termEnv := os.Getenv("TERM")
	if termEnv == "" {
		termEnv = "xterm-256color" // Fallback to widely compatible terminal
	}

	containerEnv := map[string]string{
		"HOME":       result.HomeDir,
		"TERM":       termEnv,
		"IS_SANDBOX": "1", // Always set sandbox mode
	}

	// Merge user-provided --env vars
	for _, e := range envVars {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			containerEnv[parts[0]] = parts[1]
		}
	}

	// Build environment export commands for tmux
	envExports := ""
	for k, v := range containerEnv {
		envExports += fmt.Sprintf("export %s=%q; ", k, v)
	}

	// Ensure tmux server is running first (critical for CI and new containers)
	// In constrained CI environments, wait for the server to be ready
	serverStartCmd := "tmux start-server 2>/dev/null || true; sleep 0.1"
	serverOpts := container.ExecCommandOptions{
		Capture: true,
		User:    userPtr,
	}
	result.Manager.ExecCommand(serverStartCmd, serverOpts)

	// Poll to ensure server is ready (up to 2 seconds)
	for i := 0; i < 20; i++ {
		checkServerCmd := "tmux list-sessions 2>&1 | grep -v 'no server running' || true"
		_, err := result.Manager.ExecCommand(checkServerCmd, serverOpts)
		if err == nil {
			break // Server is ready
		}
		result.Manager.ExecCommand("sleep 0.1", serverOpts)
	}

	// Check if tmux session already exists
	checkSessionCmd := fmt.Sprintf("tmux has-session -t %s 2>/dev/null", tmuxSessionName)
	_, err := result.Manager.ExecCommand(checkSessionCmd, container.ExecCommandOptions{
		Capture: true,
		User:    userPtr,
	})

	if err == nil {
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
				Cwd:         "/workspace",
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
		createCmd := fmt.Sprintf(
			"tmux new-session -d -s %s -c /workspace \"bash -c 'trap : INT; %s %s; exec bash'\"",
			tmuxSessionName,
			envExports,
			cliCmd,
		)
		opts := container.ExecCommandOptions{
			Capture: true,
			User:    userPtr,
		}
		_, err := result.Manager.ExecCommand(createCmd, opts)
		if err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Created background tmux session: %s\n", tmuxSessionName)
		fmt.Fprintf(os.Stderr, "Use 'coi tmux capture %s' to view output\n", result.ContainerName)
		fmt.Fprintf(os.Stderr, "Use 'coi tmux send %s \"<command>\"' to send commands\n", result.ContainerName)
		return nil
	} else {
		// Interactive mode: create detached session, then attach
		// This ensures tmux server owns the session, not the incus exec process
		// When we detach, only the attach process exits, not the session
		// trap : INT prevents bash from exiting on Ctrl+C, exec bash replaces (no nested shells)

		// Step 0: Ensure tmux server is running (required for all tmux operations)
		// This is critical in CI and for newly started containers where tmux server
		// might not be running yet. The command is idempotent - if server is already
		// running, it does nothing. We redirect stderr and use || true to ignore errors.
		//
		// In constrained CI environments, we need to wait for the server to be ready.
		// We start the server and then verify it's responsive by polling.
		serverStartCmd := "tmux start-server 2>/dev/null || true; sleep 0.1"
		serverOpts := container.ExecCommandOptions{
			User:    userPtr,
			Capture: true,
		}
		result.Manager.ExecCommand(serverStartCmd, serverOpts)

		// Poll to ensure server is ready (up to 2 seconds)
		// This prevents race conditions in CI where the server takes time to initialize
		for i := 0; i < 20; i++ {
			checkServerCmd := "tmux list-sessions 2>&1 | grep -v 'no server running' || true"
			_, err := result.Manager.ExecCommand(checkServerCmd, serverOpts)
			if err == nil {
				break // Server is ready (even if no sessions exist)
			}
			// Wait 100ms before next attempt
			result.Manager.ExecCommand("sleep 0.1", serverOpts)
		}

		// Step 1: Check if session already exists
		checkCmd := fmt.Sprintf("tmux has-session -t %s 2>/dev/null", tmuxSessionName)
		checkOpts := container.ExecCommandOptions{
			User:    userPtr,
			Capture: true,
		}
		_, checkErr := result.Manager.ExecCommand(checkCmd, checkOpts)

		// Step 2: Create detached session if it doesn't exist
		if checkErr != nil {
			createCmd := fmt.Sprintf(
				"tmux new-session -d -s %s -c /workspace \"bash -c 'trap : INT; %s %s; exec bash'\"",
				tmuxSessionName,
				envExports,
				cliCmd,
			)
			createOpts := container.ExecCommandOptions{
				User:    userPtr,
				Cwd:     "/workspace",
				Capture: true,
			}
			if _, err := result.Manager.ExecCommand(createCmd, createOpts); err != nil {
				return fmt.Errorf("failed to create tmux session: %w", err)
			}

			// Give tmux a moment to fully initialize the session
			time.Sleep(500 * time.Millisecond)
		}

		// Step 3: Attach to the session
		attachCmd := fmt.Sprintf("tmux attach -t %s", tmuxSessionName)
		attachOpts := container.ExecCommandOptions{
			User:        userPtr,
			Cwd:         "/workspace",
			Interactive: true,
			Env:         containerEnv,
		}
		_, err := result.Manager.ExecCommand(attachCmd, attachOpts)
		return err
	}
}
