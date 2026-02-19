package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
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
	useTmux       bool
	containerName string
	toolFlag      string
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive AI coding session",
	Long: `Start an interactive AI coding session in a container (always runs in tmux).

By default, runs Claude Code. Other tools can be configured via the tool.name config option.

All sessions run in tmux for monitoring and detach/reattach support:
  - Interactive: Automatically attaches to tmux session
  - Background: Runs detached, use 'coi tmux capture' to view output
  - Detach anytime: Ctrl+B d (session keeps running)
  - Reattach: Run 'coi shell' again in same workspace

Examples:
  coi shell                         # Interactive session in tmux
  coi shell --tool opencode         # Use opencode instead of configured tool
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
	shellCmd.Flags().BoolVar(&debugShell, "debug", false, "Launch interactive bash instead of AI tool (for debugging)")
	shellCmd.Flags().BoolVar(&background, "background", false, "Run AI tool in background tmux session (detached)")
	shellCmd.Flags().BoolVar(&useTmux, "tmux", true, "Use tmux for session management (default true)")
	shellCmd.Flags().StringVar(&containerName, "container", "", "Use existing container (for testing)")
	shellCmd.Flags().StringVar(&toolFlag, "tool", "", "Override AI tool (e.g. claude, opencode, aider)")
}

//nolint:gocyclo // Sequential initialization with many configuration paths
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

	// Get configured tool (needed to determine tool-specific sessions directory)
	// --tool flag overrides whatever is in .coi.toml or global config
	if toolFlag != "" {
		cfg.Tool.Name = toolFlag
	}
	toolInstance, err := getConfiguredTool(cfg)
	if err != nil {
		return err
	}

	// Get sessions directory (tool-specific: sessions-claude, sessions-aider, etc.)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	baseDir := filepath.Join(homeDir, ".coi")
	sessionsDir := session.GetSessionsDir(baseDir, toolInstance)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	// Handle resume flag (--resume or --continue)
	resumeID := resume
	if continueSession != "" {
		resumeID = continueSession // --continue takes precedence if both are provided
	}

	// Check if resume/continue flag was explicitly set
	resumeFlagSet := cmd.Flags().Changed("resume") || cmd.Flags().Changed("continue")

	// Check if tool uses workspace-based sessions (like opencode stores in .opencode/)
	// These tools don't need COI session tracking - their data is in the workspace
	isWorkspaceSessionTool := false
	if _, ok := toolInstance.(tool.ToolWithHomeConfigFile); ok {
		// File-based tools like opencode store sessions in workspace, not ~/.coi/sessions-*
		// Check for .opencode/ or similar in workspace
		workspaceSessionDir := filepath.Join(absWorkspace, ".opencode")
		if info, err := os.Stat(workspaceSessionDir); err == nil && info.IsDir() {
			isWorkspaceSessionTool = true
		}
	}

	// Auto-detect if flag was set but value is empty or "auto"
	if resumeFlagSet && (resumeID == "" || resumeID == "auto") {
		if isWorkspaceSessionTool {
			// For workspace-session tools, use a synthetic session ID
			// The actual session data is in the workspace directory
			resumeID = "workspace-session"
			fmt.Fprintf(os.Stderr, "Resuming %s session from workspace\n", toolInstance.Name())
		} else {
			// Auto-detect latest for workspace (only looks at sessions from the same workspace)
			resumeID, err = session.GetLatestSessionForWorkspace(sessionsDir, absWorkspace)
			if err != nil {
				return fmt.Errorf("no previous session to resume for this workspace: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Auto-detected session: %s\n", resumeID)
		}
	} else if resumeID != "" && !isWorkspaceSessionTool {
		// Validate that the explicitly provided session exists (skip for workspace-session tools)
		if !session.SessionExists(sessionsDir, resumeID) {
			return fmt.Errorf("session '%s' not found - check available sessions with: coi list --all", resumeID)
		}
		fmt.Fprintf(os.Stderr, "Resuming session: %s\n", resumeID)
	}

	// When resuming, inherit persistent flag from the original session
	// unless it was explicitly overridden by the user
	// Skip for workspace-session tools (they don't have COI metadata files)
	if resumeID != "" && !isWorkspaceSessionTool {
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

	// Determine CLI config path based on tool
	// For file-based tools (ToolWithHomeConfigFile), point at the single config file.
	// For directory-based tools (ConfigDirName != ""), point at the config directory.
	// For ENV-based tools (both return ""), leave empty.
	var cliConfigPath string
	if twh, ok := toolInstance.(tool.ToolWithHomeConfigFile); ok {
		// File-based config (e.g., ~/.opencode.json)
		cliConfigPath = filepath.Join(homeDir, twh.HomeConfigFileName())
	} else if configDirName := toolInstance.ConfigDirName(); configDirName != "" {
		// Directory-based config (e.g., ~/.claude/)
		cliConfigPath = filepath.Join(homeDir, configDirName)
	}

	// Merge limits configuration from config file and CLI flags
	limitsConfig := mergeLimitsConfig(cmd)

	// Determine protected paths for security mounts
	// Use config's protected paths unless disabled via flag or config
	var protectedPaths []string
	if !writableGitHooks && !cfg.Security.DisableProtection {
		protectedPaths = cfg.Security.GetEffectiveProtectedPaths()
	}

	// Setup session
	setupOpts := session.SetupOptions{
		WorkspacePath:  absWorkspace,
		Image:          imageName,
		Persistent:     persistent,
		ResumeFromID:   resumeID,
		Slot:           slotNum,
		SessionsDir:    sessionsDir,
		CLIConfigPath:  cliConfigPath,
		Tool:           toolInstance,
		NetworkConfig:  &networkConfig,
		DisableShift:   cfg.Incus.DisableShift,
		LimitsConfig:   limitsConfig,
		IncusProject:   cfg.Incus.Project,
		ProtectedPaths: protectedPaths,
		ContainerName:  containerName,
	}

	// Parse and validate mount configuration
	mountConfig, err := ParseMountConfig(cfg, mountPairs)
	if err != nil {
		return fmt.Errorf("invalid mount configuration: %w", err)
	}

	// Validate no nested mounts
	if err := session.ValidateMounts(mountConfig); err != nil {
		return fmt.Errorf("mount validation failed: %w", err)
	}

	setupOpts.MountConfig = mountConfig

	fmt.Fprintf(os.Stderr, "Setting up session %s...\n", sessionID)
	result, err := session.Setup(setupOpts)
	if err != nil {
		return fmt.Errorf("failed to setup session: %w", err)
	}

	// Save metadata early so coi list shows correct persistent/ephemeral status
	if err := session.SaveMetadataEarly(sessionsDir, sessionID, result.ContainerName, absWorkspace, persistent); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save early metadata: %v\n", err)
	}

	// Start monitoring daemons if enabled (via config or --monitor flag)
	var monitorDaemon *monitor.Daemon
	var nftDaemon *nftmonitor.Daemon
	monitoringEnabled := cfg.Monitoring.Enabled || enableMonitoring
	if monitoringEnabled {
		// Override config settings when --monitor flag is used
		if enableMonitoring {
			cfg.Monitoring.Enabled = true
			cfg.Monitoring.AutoKillOnCritical = true
			cfg.Monitoring.AutoPauseOnHigh = true
		}
		// Start traditional monitoring (process/filesystem)
		if err := startMonitoringDaemon(result.ContainerName, absWorkspace, cfg, &monitorDaemon); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to start monitoring daemon: %v\n", err)
			// Don't fail the session if monitoring fails
		}

		// Start nftables monitoring (network only)
		if cfg.Monitoring.NFT.Enabled {
			if err := startNFTMonitoringDaemon(result.ContainerName, cfg, &nftDaemon); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to start NFT monitoring: %v\n", err)
				// Don't fail the session if NFT monitoring fails
			}
		}
	}

	// Setup cleanup on exit
	defer func() {
		fmt.Fprintf(os.Stderr, "\nCleaning up session...\n")

		// Stop monitoring daemons if they were started
		if monitorDaemon != nil {
			if err := monitorDaemon.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to stop monitoring daemon: %v\n", err)
			}
		}
		if nftDaemon != nil {
			if err := nftDaemon.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to stop NFT monitoring: %v\n", err)
			}
		}

		// Stop timeout monitor if it was started
		if result.TimeoutMonitor != nil {
			result.TimeoutMonitor.Stop()
		}

		cleanupOpts := session.CleanupOptions{
			ContainerName:  result.ContainerName,
			SessionID:      sessionID,
			Persistent:     persistent,
			SessionsDir:    sessionsDir,
			SaveSession:    true, // Always save session data
			Workspace:      absWorkspace,
			Tool:           toolInstance,
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

	// Run CLI tool
	fmt.Fprintf(os.Stderr, "\nStarting session...\n")
	fmt.Fprintf(os.Stderr, "Session ID: %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "Container: %s\n", result.ContainerName)
	fmt.Fprintf(os.Stderr, "Workspace: %s\n", absWorkspace)

	// Determine resume mode
	// The difference is:
	// - Persistent: container is reused, tool config stays in container, pass --resume flag
	// - Ephemeral: container is recreated, we restore config dir, tool auto-detects session
	//
	// For persistent containers resuming: pass --resume flag with tool's session ID
	// For ephemeral containers resuming: just restore config, tool will auto-detect from restored data
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
		err = runCLIInTmux(result, sessionID, background, useResumeFlag, restoreOnly, sessionsDir, resumeID, toolInstance)
	} else {
		fmt.Fprintf(os.Stderr, "Mode: Direct (no tmux)\n")
		if restoreOnly {
			fmt.Fprintf(os.Stderr, "Resume mode: Restored conversation (auto-detect)\n")
		} else if useResumeFlag {
			fmt.Fprintf(os.Stderr, "Resume mode: Persistent session\n")
		}
		fmt.Fprintf(os.Stderr, "\n")
		err = runCLI(result, sessionID, useResumeFlag, restoreOnly, sessionsDir, resumeID, toolInstance)
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

	return t, nil
}

// runCLI executes the CLI tool in the container interactively
func runCLI(result *session.SetupResult, sessionID string, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string, t tool.Tool) error {
	// Build command - either bash for debugging or CLI tool
	var cmdToRun string
	if debugShell {
		// Debug mode: launch interactive bash
		cmdToRun = "bash"
	} else {
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
		if getEnvValue("COI_USE_DUMMY") == "1" {
			if len(cmd) > 0 {
				cmd[0] = "dummy"
			}
			fmt.Fprintf(os.Stderr, "Using dummy (test stub) for faster testing\n")
		}

		cmdToRun = strings.Join(cmd, " ")
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
		"TERM":       terminal.SanitizeTerm(os.Getenv("TERM")), // Use sanitized terminal type
		"IS_SANDBOX": "1",                                      // Always set sandbox mode
	}

	// Merge user-provided --env vars
	for _, e := range envVars {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			containerEnv[parts[0]] = parts[1]
		}
	}

	// Sanitize TERM if user explicitly provided it via -e flag
	if userTerm, exists := containerEnv["TERM"]; exists {
		containerEnv["TERM"] = terminal.SanitizeTerm(userTerm)
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
func runCLIInTmux(result *session.SetupResult, sessionID string, detached bool, useResumeFlag, restoreOnly bool, sessionsDir, resumeID string, t tool.Tool) error {
	tmuxSessionName := fmt.Sprintf("coi-%s", result.ContainerName)

	// Build CLI command
	var cliCmd string
	if debugShell {
		// Debug mode: launch interactive bash
		cliCmd = "bash"
	} else {
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
		if getEnvValue("COI_USE_DUMMY") == "1" {
			if len(cmd) > 0 {
				cmd[0] = "dummy"
			}
			fmt.Fprintf(os.Stderr, "Using dummy (test stub) for faster testing\n")
		}

		cliCmd = strings.Join(cmd, " ")
	}

	// Build environment variables
	user := container.CodeUID
	if result.RunAsRoot {
		user = 0
	}
	userPtr := &user

	// Get TERM with fallback
	termEnv := terminal.SanitizeTerm(os.Getenv("TERM"))

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

	// Sanitize TERM if user explicitly provided it via -e flag
	if userTerm, exists := containerEnv["TERM"]; exists {
		containerEnv["TERM"] = terminal.SanitizeTerm(userTerm)
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
	_, _ = result.Manager.ExecCommand(serverStartCmd, serverOpts) // Best-effort server start.

	// Poll to ensure server is ready (up to 2 seconds)
	for i := 0; i < 20; i++ {
		checkServerCmd := "tmux list-sessions 2>&1 | grep -v 'no server running' || true"
		_, err := result.Manager.ExecCommand(checkServerCmd, serverOpts)
		if err == nil {
			break // Server is ready
		}
		_, _ = result.Manager.ExecCommand("sleep 0.1", serverOpts) // Best-effort sleep.
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
		_, _ = result.Manager.ExecCommand(serverStartCmd, serverOpts) // Best-effort server start.

		// Poll to ensure server is ready (up to 2 seconds)
		// This prevents race conditions in CI where the server takes time to initialize
		for i := 0; i < 20; i++ {
			checkServerCmd := "tmux list-sessions 2>&1 | grep -v 'no server running' || true"
			_, err := result.Manager.ExecCommand(checkServerCmd, serverOpts)
			if err == nil {
				break // Server is ready (even if no sessions exist)
			}
			// Wait 100ms before next attempt
			_, _ = result.Manager.ExecCommand("sleep 0.1", serverOpts) // Best-effort sleep.
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

// startMonitoringDaemon starts the background monitoring daemon
func startMonitoringDaemon(containerName, workspacePath string, cfg *config.Config, daemon **monitor.Daemon) error {
	// Get home directory for audit log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	auditLogPath := filepath.Join(homeDir, ".coi", "audit", containerName+".jsonl")

	// Get allowed CIDRs from network config
	allowedCIDRs := []string{}
	// TODO: Convert allowed domains to CIDRs if in allowlist mode

	// Create daemon config
	daemonCfg := monitor.DaemonConfig{
		ContainerName:        containerName,
		WorkspacePath:        workspacePath,
		PollInterval:         time.Duration(cfg.Monitoring.PollIntervalSec) * time.Second,
		AuditLogPath:         auditLogPath,
		AllowedCIDRs:         allowedCIDRs,
		AllowedDomains:       cfg.Network.AllowedDomains,
		FileReadThresholdMB:  cfg.Monitoring.FileReadThresholdMB,
		FileReadRateMBPerSec: cfg.Monitoring.FileReadRateMBPerSec,
		AutoPauseOnHigh:      cfg.Monitoring.AutoPauseOnHigh,
		AutoKillOnCritical:   cfg.Monitoring.AutoKillOnCritical,
		OnThreat: func(threat monitor.ThreatEvent) {
			// Display alert in terminal
			fmt.Fprint(os.Stderr, monitor.FormatThreatAlert(threat))
		},
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "[monitor] Error: %v\n", err)
		},
	}

	// Start daemon
	ctx := context.Background()
	d, err := monitor.StartDaemon(ctx, daemonCfg)
	if err != nil {
		return err
	}

	*daemon = d
	fmt.Fprintf(os.Stderr, "Monitoring daemon started (audit log: %s)\n", auditLogPath)
	return nil
}

// startNFTMonitoringDaemon starts the nftables network monitoring daemon
func startNFTMonitoringDaemon(containerName string, cfg *config.Config, daemon **nftmonitor.Daemon) error {
	// Get container IP
	containerIP, err := network.GetContainerIPWithRetries(containerName, 3)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}

	// Get gateway IP
	gatewayIP, err := network.GetContainerGatewayIP(containerName)
	if err != nil {
		// Non-fatal - we can still monitor without gateway IP check
		fmt.Fprintf(os.Stderr, "Warning: Failed to get gateway IP: %v\n", err)
		gatewayIP = ""
	}

	// Get home directory for audit log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	auditLogPath := filepath.Join(homeDir, ".coi", "audit", containerName+"-nft.jsonl")

	// Get allowed CIDRs from network config
	allowedCIDRs := []string{}
	// TODO: Convert allowed domains to CIDRs if in allowlist mode

	// Create NFT daemon config
	nftCfg := nftmonitor.Config{
		ContainerName:      containerName,
		ContainerIP:        containerIP,
		AllowedCIDRs:       allowedCIDRs,
		GatewayIP:          gatewayIP,
		AuditLogPath:       auditLogPath,
		RateLimitPerSecond: cfg.Monitoring.NFT.RateLimitPerSecond,
		DNSQueryThreshold:  cfg.Monitoring.NFT.DNSQueryThreshold,
		LogDNSQueries:      cfg.Monitoring.NFT.LogDNSQueries,
		LimaHost:           cfg.Monitoring.NFT.LimaHost,
		OnThreat: func(threat nftmonitor.ThreatEvent) {
			// Display alert in terminal using existing formatter
			monitorThreat := monitor.ThreatEvent{
				Timestamp:   threat.Timestamp,
				Level:       monitor.ThreatLevel(threat.Level),
				Category:    threat.Category,
				Title:       threat.Title,
				Description: threat.Description,
				Evidence:    threat.Evidence,
			}
			fmt.Fprint(os.Stderr, monitor.FormatThreatAlert(monitorThreat))
		},
	}

	// Start daemon
	ctx := context.Background()
	d, err := nftmonitor.StartDaemon(ctx, nftCfg)
	if err != nil {
		return err
	}

	*daemon = d
	fmt.Fprintf(os.Stderr, "NFT monitoring started (container IP: %s, audit log: %s)\n", containerIP, auditLogPath)
	return nil
}
