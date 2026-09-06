package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mensfeld/code-on-incus/internal/alias"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/terminal"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/spf13/cobra"
)

var (
	attachWithBash bool
	attachSlot     int
)

var attachCmd = &cobra.Command{
	Use:   "attach [container-name]",
	Short: "Attach to a running AI coding session",
	Long: `Attach to a running AI coding session in a container.

If no container name is provided, lists all running sessions.
If only one session is running, attaches to it automatically.

Examples:
  coi attach                    # List sessions or auto-attach if only one
  coi attach claude-abc123-1    # Attach to specific session
  coi attach --slot=1           # Attach to slot 1 for current workspace
  coi attach --bash             # Attach to bash shell instead of tmux session
  coi attach coi-123 --bash     # Attach to specific container with bash
  coi attach --tool codex       # Start codex in the running container (overrides [tool] name)`,
	RunE: app.attachCommand,
}

func init() {
	attachCmd.Flags().BoolVar(&attachWithBash, "bash", false, "Attach to bash shell instead of tmux session")
	attachCmd.Flags().IntVar(&attachSlot, "slot", 0, "Slot number to attach to (requires workspace context)")
	attachCmd.Flags().StringVar(&toolOverride, "tool", "", "Start this AI tool in the running container instead of attaching to tmux (claude, codex, opencode, pi, omp)")
}

func (a *App) attachCommand(cmd *cobra.Command, args []string) error {
	// A profile-carried session_name changes which container this workspace
	// resolves to, so the operational commands apply the same [defaults]
	// profile fallback the launch used (error-tolerantly, per #607).
	a.applyDefaultProfileForOps(cmd)

	// --tool and --bash are mutually exclusive: one starts an AI tool, the other
	// drops to a shell.
	if toolOverride != "" && attachWithBash {
		return &ExitCodeError{Code: 2, Message: "--tool and --bash cannot be combined"}
	}

	var targetContainer string

	// If --slot is provided, calculate container name from workspace and slot
	if attachSlot > 0 {
		// Resolve workspace path
		workspacePath, err := filepath.Abs(a.workspace)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace path: %w", err)
		}

		// Calculate container name for this workspace+slot
		targetContainer = session.ContainerName(workspacePath, a.sessionName(), attachSlot)

		// Verify it exists and is running
		mgr := container.NewManager(targetContainer)
		running, err := mgr.Running()
		if err != nil || !running {
			return fmt.Errorf("container %s not found or not running", targetContainer)
		}

		fmt.Printf("Attaching to %s (slot %d)...\n", targetContainer, attachSlot)
	} else {
		// List all running containers with configured prefix
		prefix := regexp.QuoteMeta(session.GetContainerPrefix())
		containers, err := container.ListContainers(prefix + ".*")
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		// If container name provided, use it (with alias resolution)
		if len(args) > 0 {
			targetContainer = args[0]
			if resolved, err := alias.ResolveAliasForRunning(targetContainer); err == nil {
				targetContainer = resolved
			} else if !alias.IsContainerName(targetContainer) {
				return err
			}
			// Verify it exists and is running
			found := false
			for _, c := range containers {
				if c == targetContainer {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("container %s not found or not running", targetContainer)
			}
		} else if len(containers) == 0 {
			// No container name provided and no sessions running
			fmt.Println("No active sessions")
			return nil
		} else if len(containers) == 1 {
			// Auto-attach if only one session
			targetContainer = containers[0]
			fmt.Printf("Attaching to %s...\n", targetContainer)
		} else {
			// Multiple sessions - show list
			fmt.Println("Active sessions:")
			for i, c := range containers {
				mgr := container.NewManager(c)
				running, err := mgr.Running()
				if err != nil || !running {
					continue
				}
				fmt.Printf("  %d. %s\n", i+1, c)
			}
			fmt.Printf("\nUse: coi attach <container-name>\n")
			return nil
		}
	}

	// Attach to container (a specific tool, bash, or the tmux session)
	if toolOverride != "" {
		t, err := a.resolveConfiguredTool()
		if err != nil {
			return &ExitCodeError{Code: 2, Message: err.Error()}
		}
		return a.attachToContainerWithTool(targetContainer, t)
	}
	if attachWithBash {
		return attachToContainerWithBash(targetContainer)
	}
	return attachToContainer(targetContainer)
}

// attachToContainerWithTool starts an AI tool in an already-running container
// (the analog of --bash starting a shell), overriding the tool the session was
// created with (#708). It first seeds that tool's CLI config/credentials +
// context + model env — so, e.g., starting codex on a claude-created container
// still authenticates — then execs the tool's launch command interactively as a
// fresh session. This is separate from the session's original tmux window.
func (a *App) attachToContainerWithTool(containerName string, t tool.Tool) error {
	mgr := container.NewManager(containerName)
	workspacePath := mgr.GetWorkspacePath()

	// homeDir drives config/context seeding; the exec run-user is resolved via
	// tmuxExecUser below (the #588 tmux-socket-owner resolution).
	_, homeDir, err := toolSpecUserHome(mgr)
	if err != nil {
		return err
	}

	// Seed the tool's config/creds/context/model env into the live container.
	hostHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve host home directory: %w", err)
	}
	var cliConfigPath string
	if dirName := t.ConfigDirName(); dirName != "" {
		cliConfigPath = filepath.Join(hostHome, dirName)
	}
	seedResult := &session.SetupResult{
		Manager:                mgr,
		ContainerName:          containerName,
		ContainerWorkspacePath: workspacePath,
		HomeDir:                homeDir,
	}
	seedOpts := session.SetupOptions{
		Tool:                t,
		CLIConfigPath:       cliConfigPath,
		AutoContext:         a.cfg.Tool.AutoContext,
		ContextJSON:         a.cfg.Tool.ContextJSON,
		ContextFilePath:     a.cfg.Tool.ContextFile,
		ContextJSONFilePath: a.cfg.Tool.ContextJSONFile,
		ProfileContextFile:  a.cfg.ProfileContextFile,
		NetworkConfig:       &a.cfg.Network,
		LimitsConfig:        &a.cfg.Limits,
		Persistent:          a.persistent,
		ForwardedEnvVars:    resolveForwardedEnvVarNames(a.cfg.Defaults.ForwardEnv),
		Logger:              stderrLogFn,
	}
	if err := session.SeedToolConfigForRun(context.Background(), seedResult, seedOpts); err != nil {
		return err
	}

	// Build the tool's launch command for a fresh ad-hoc session.
	sessionID, err := session.GenerateSessionID()
	if err != nil {
		return fmt.Errorf("failed to generate session id: %w", err)
	}
	argv := t.BuildCommand(sessionID, false, "")
	if os.Getenv("COI_USE_DUMMY") == "1" && len(argv) > 0 {
		argv[0] = "dummy"
	}

	env := map[string]string{"TERM": terminal.SanitizeTerm(os.Getenv("TERM"))}
	mergeToolEnv(env, t, workspacePath)

	// Run as the container's ACTUAL code user (config-derived CodeUID can
	// misstate it, #588 — same resolution as tmux attach / --bash).
	user, err := tmuxExecUser(mgr)
	if err != nil {
		return err
	}
	opts := container.ExecCommandOptions{
		User:        user,
		Cwd:         workspacePath,
		Interactive: true,
		Env:         env,
	}

	fmt.Fprintf(os.Stderr, "Starting %s in %s...\n", t.Name(), containerName)
	if err := mgr.ExecArgs(argv, opts); err != nil {
		errStr := err.Error()
		// Signal exits on container shutdown / force-kill / Ctrl+C are expected.
		if errStr == "exit status 143" || errStr == "exit status 137" || errStr == "exit status 130" {
			return nil
		}
		return fmt.Errorf("failed to start %s in container: %w", t.Name(), err)
	}
	return nil
}

func attachToContainer(containerName string) error {
	// Calculate the tmux session name (consistent with shell command)
	tmuxSessionName := fmt.Sprintf("coi-%s", containerName)

	// Use container manager for proper user/environment handling
	// Direct command execution without bash -c wrapper for better terminal handling
	mgr := container.NewManager(containerName)

	// Get TERM with fallback (same as shell command)
	termEnv := terminal.SanitizeTerm(os.Getenv("TERM"))

	// Get workspace path from container's device config
	workspacePath := mgr.GetWorkspacePath()

	// Attach as the container's ACTUAL code UID, not the config-derived one:
	// the tmux socket lives at /tmp/tmux-<uid> of whoever created the
	// session, which config can misstate after remaps or for root-session
	// images (#588 — same resolution as the coi tmux helpers).
	user, err := tmuxExecUser(mgr)
	if err != nil {
		return err
	}
	opts := container.ExecCommandOptions{
		User:        user,
		Cwd:         workspacePath,
		Interactive: true,
		Env: map[string]string{
			"TERM": termEnv,
		},
	}

	// Use ExecArgs instead of ExecCommand to avoid bash -c wrapper
	// tmux attach needs direct terminal access
	commandArgs := []string{"tmux", "attach", "-t", tmuxSessionName}
	err = mgr.ExecArgs(commandArgs, opts)
	if err != nil {
		errStr := err.Error()
		// Exit status 143 = SIGTERM (128+15), happens when container shuts down
		// Exit status 137 = SIGKILL (128+9), happens on force kill
		// Exit status 130 = SIGINT (128+2), happens on Ctrl+C
		if errStr == "exit status 143" || errStr == "exit status 137" || errStr == "exit status 130" {
			return nil
		}
		// tmux attach failed - likely no session exists
		// Suggest using --bash to get a shell
		fmt.Fprintf(os.Stderr, "\nNo tmux session found in container.\n")
		fmt.Fprintf(os.Stderr, "The container is still running. To get a shell, use:\n")
		fmt.Fprintf(os.Stderr, "  coi attach %s --bash\n", containerName)
		return nil
	}

	return nil
}

func attachToContainerWithBash(containerName string) error {
	// Use container manager for proper user/environment handling
	mgr := container.NewManager(containerName)

	// Get workspace path from container's device config
	workspacePath := mgr.GetWorkspacePath()

	// Execute bash as the container's actual code user (same resolution as
	// tmux attach above — config-derived CodeUID can misstate it, #588)
	user, err := tmuxExecUser(mgr)
	if err != nil {
		return err
	}
	opts := container.ExecCommandOptions{
		User:        user,
		Cwd:         workspacePath,
		Interactive: true,
	}

	_, err = mgr.ExecCommand("exec bash", opts)
	if err != nil {
		// Handle expected exit conditions gracefully
		errStr := err.Error()
		// Exit status 143 = SIGTERM (128+15), happens when container shuts down
		// Exit status 137 = SIGKILL (128+9), happens on force kill
		// Exit status 130 = SIGINT (128+2), happens on Ctrl+C
		if errStr == "exit status 143" || errStr == "exit status 137" || errStr == "exit status 130" {
			return nil
		}
		return fmt.Errorf("failed to attach to container: %w", err)
	}

	return nil
}
