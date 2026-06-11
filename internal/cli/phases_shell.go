package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/alias"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/monitor"
	"github.com/mensfeld/code-on-incus/internal/nftmonitor"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/spf13/cobra"
)

// shellState is the mutable state accumulated across shell pipeline phases.
// Each phase reads its inputs from state and writes its outputs back.
type shellState struct {
	aliasArg string // positional argument from cobra args[0], if given

	// After resolve-workspace
	absWorkspace string

	// After validate-env
	useTmux bool // determined from config + explicit --tmux flag

	// After configure-session
	sessionID    string
	resumeID     string
	slotNum      int
	toolInstance tool.Tool
	sessionsDir  string
	setupOpts    session.SetupOptions // fully built options for session.Setup

	// After setup-container
	result *session.SetupResult

	// After start-monitoring
	monitorDaemon *monitor.Daemon
	nftDaemon     *nftmonitor.Daemon
}

// resolveWorkspacePhase resolves the absolute workspace path from --workspace
// or alias, overlays the workspace's project config, and applies any alias
// profile/slot inherited from the registry.
func resolveWorkspacePhase(cmd *cobra.Command, s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "resolve-workspace",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			absWorkspace, err := filepath.Abs(app.workspace)
			if err != nil {
				return nil, fmt.Errorf("invalid workspace path: %w", err)
			}

			// When no alias is given, overlay the workspace's project config when it
			// differs from CWD (config.Load only reads from CWD).
			if s.aliasArg == "" {
				if cwd, err := os.Getwd(); err == nil {
					if absCWD, err := filepath.Abs(cwd); err == nil && absWorkspace != absCWD {
						if err := app.cfg.OverlayProjectConfig(absWorkspace); err != nil && !os.IsNotExist(err) {
							return nil, fmt.Errorf("failed to load project config from %s: %w", absWorkspace, err)
						}
						container.Configure(app.cfg.Incus.Project, app.cfg.Incus.CodeUser, app.cfg.Incus.CodeUID)
					}
				}
			}

			// Alias resolution overrides workspace and may apply a saved profile/slot.
			if s.aliasArg != "" {
				resolved, err := alias.ResolveAliasForLaunch(s.aliasArg)
				if err != nil {
					return nil, err
				}
				absWorkspace = resolved.Workspace

				if err := app.cfg.OverlayProjectConfig(absWorkspace); err != nil && !os.IsNotExist(err) {
					return nil, fmt.Errorf("failed to load project config from %s: %w", absWorkspace, err)
				}

				if resolved.Profile != "" && !cmd.Flags().Changed("profile") {
					app.profile = resolved.Profile
					if err := app.cfg.ApplyProfile(app.profile); err != nil {
						return nil, err
					}
				}
				container.Configure(app.cfg.Incus.Project, app.cfg.Incus.CodeUser, app.cfg.Incus.CodeUID)
				if resolved.Slot > 0 && !cmd.Flags().Changed("slot") {
					app.slot = resolved.Slot
				}
			}

			s.absWorkspace = absWorkspace
			return nil, nil
		},
	}
}

// validateEnvPhase determines the effective tmux mode (config default vs
// explicit flag) and verifies that Incus is available and at the minimum
// required version.
func validateEnvPhase(cmd *cobra.Command, s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "validate-env",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			// Config default, overridden by explicit --tmux flag.
			useTmuxDefault := true
			if app.cfg.Shell.UseTmux != nil {
				useTmuxDefault = *app.cfg.Shell.UseTmux
			}
			if cmd.Flags().Changed("tmux") {
				s.useTmux = useTmux // cobra already set the package-level var
			} else {
				s.useTmux = useTmuxDefault
			}

			if !container.Available() {
				return nil, container.IncusNotAvailableError()
			}
			if err := container.CheckMinimumVersion(); err != nil {
				return nil, err
			}
			if warning := container.CheckKernelVersion(); warning != "" {
				fmt.Fprintf(os.Stderr, "%s\n", warning)
			}
			return nil, nil
		},
	}
}

// configureSessionPhase assembles all session options: chooses the tool,
// determines the sessions directory, resolves the resume ID, allocates a
// slot, and constructs the SetupOptions for the container.
//
//nolint:gocyclo // Many configuration paths — sequential by design
func configureSessionPhase(cmd *cobra.Command, s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "configure-session",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			// Override tool from --tool flag.
			if toolFlag != "" {
				app.cfg.Tool.Name = toolFlag
			}
			ti, err := getConfiguredTool(app.cfg)
			if err != nil {
				return nil, err
			}
			s.toolInstance = ti

			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}
			baseDir := filepath.Join(homeDir, ".coi")
			sessionsDir := session.GetSessionsDir(baseDir, ti)
			if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create sessions directory: %w", err)
			}
			s.sessionsDir = sessionsDir

			// Resolve the resume/continue ID.
			resumeID := app.resume
			if app.continueSession != "" {
				resumeID = app.continueSession
			}
			resumeFlagSet := cmd.Flags().Changed("resume") || cmd.Flags().Changed("continue")

			isWorkspaceSessionTool := false
			if ti.Name() == "opencode" {
				workspaceSessionDir := filepath.Join(s.absWorkspace, ".opencode")
				if info, err := os.Stat(workspaceSessionDir); err == nil && info.IsDir() {
					isWorkspaceSessionTool = true
				}
			}

			if resumeFlagSet && (resumeID == "" || resumeID == "auto") {
				if isWorkspaceSessionTool {
					resumeID = "workspace-session"
					fmt.Fprintf(os.Stderr, "Resuming %s session from workspace\n", ti.Name())
				} else {
					resumeID, err = session.GetLatestSessionForWorkspace(sessionsDir, s.absWorkspace)
					if err != nil {
						return nil, fmt.Errorf("no previous session to resume for this workspace: %w", err)
					}
					fmt.Fprintf(os.Stderr, "Auto-detected session: %s\n", resumeID)
				}
			} else if resumeID != "" && !isWorkspaceSessionTool {
				if !session.SessionExists(sessionsDir, resumeID) {
					return nil, fmt.Errorf("session '%s' not found - check available sessions with: coi list --all", resumeID)
				}
				fmt.Fprintf(os.Stderr, "Resuming session: %s\n", resumeID)
			}
			s.resumeID = resumeID

			// Inherit persistent flag, profile, and slot from session metadata.
			var resumeSlot int
			if resumeID != "" && !isWorkspaceSessionTool {
				metadataPath := filepath.Join(sessionsDir, resumeID, "metadata.json")
				if metadata, err := session.LoadSessionMetadata(metadataPath); err == nil {
					if !cmd.Flags().Changed("profile") && metadata.ProfileName != "" {
						app.profile = metadata.ProfileName
						if err := app.cfg.ApplyProfile(app.profile); err != nil {
							return nil, fmt.Errorf("failed to apply saved profile '%s': %w", app.profile, err)
						}
						if !cmd.Flags().Changed("persistent") {
							app.persistent = config.BoolVal(app.cfg.Container.Persistent)
						}
						fmt.Fprintf(os.Stderr, "Inherited profile '%s' from session\n", app.profile)
					}
					if !cmd.Flags().Changed("persistent") {
						app.persistent = metadata.Persistent
						if app.persistent {
							fmt.Fprintf(os.Stderr, "Inherited persistent mode from session\n")
						}
					}
					if !cmd.Flags().Changed("slot") {
						if _, origSlot, err := session.ParseContainerName(metadata.ContainerName); err == nil {
							resumeSlot = origSlot
						}
					}
				}
			}

			// Generate or reuse session ID.
			if resumeID != "" {
				s.sessionID = resumeID
			} else {
				id, err := session.GenerateSessionID()
				if err != nil {
					return nil, err
				}
				s.sessionID = id
			}

			// Allocate slot.
			slotNum := app.slot
			if resumeSlot > 0 && slotNum == 0 {
				slotNum = resumeSlot
				fmt.Fprintf(os.Stderr, "Reusing original slot %d from session\n", slotNum)
			} else if slotNum == 0 {
				slotNum, err = session.AllocateSlot(s.absWorkspace, 10)
				if err != nil {
					return nil, fmt.Errorf("failed to allocate slot: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Auto-allocated slot %d\n", slotNum)
			} else {
				available, err := session.IsSlotAvailable(s.absWorkspace, slotNum)
				if err != nil {
					return nil, fmt.Errorf("failed to check slot availability: %w", err)
				}
				if !available {
					orig := slotNum
					slotNum, err = session.AllocateSlotFrom(s.absWorkspace, slotNum+1, 10)
					if err != nil {
						return nil, fmt.Errorf("slot %d is occupied and failed to find next available slot: %w", orig, err)
					}
					fmt.Fprintf(os.Stderr, "Slot %d is occupied, using slot %d instead\n", orig, slotNum)
				}
			}
			s.slotNum = slotNum

			// Resolve image and auto-build if needed.
			img := ResolveImageName(app.imageName, app.cfg)
			if err := AutoBuildIfNeeded(app.cfg, img); err != nil {
				return nil, err
			}
			if err := CheckAndReportStaleBase(app.cfg, img); err != nil {
				return nil, err
			}

			// Build config-derived options.
			networkConfig := app.cfg.Network
			limitsConfig := &app.cfg.Limits
			var protectedPaths []string
			if !app.cfg.Security.DisableProtection {
				protectedPaths = app.cfg.Security.GetEffectiveProtectedPaths()
			}
			protectedPaths = filterWritableGitHooks(protectedPaths, app.cfg)

			var cliConfigPath string
			if configDirName := ti.ConfigDirName(); configDirName != "" {
				cliConfigPath = filepath.Join(homeDir, configDirName)
			}

			resolvedForwardedEnvVars := resolveForwardedEnvVarNames(app.cfg.Defaults.ForwardEnv)
			resolvedTimezone := resolveTimezone(app.cfg)

			mountConfig, err := ParseMountConfig(app.cfg)
			if err != nil {
				return nil, fmt.Errorf("invalid mount configuration: %w", err)
			}
			if err := session.ValidateMounts(mountConfig); err != nil {
				return nil, fmt.Errorf("mount validation failed: %w", err)
			}

			if app.cfg.Container.Alias != "" {
				if err := alias.ValidateAlias(app.cfg.Container.Alias); err != nil {
					return nil, fmt.Errorf("invalid container alias: %w", err)
				}
			}

			s.setupOpts = session.SetupOptions{
				WorkspacePath:         s.absWorkspace,
				Image:                 img,
				Persistent:            app.persistent,
				ResumeFromID:          resumeID,
				Slot:                  slotNum,
				MountConfig:           mountConfig,
				SessionsDir:           sessionsDir,
				CLIConfigPath:         cliConfigPath,
				Tool:                  ti,
				NetworkConfig:         &networkConfig,
				DisableShift:          app.cfg.Incus.DisableShift,
				LimitsConfig:          limitsConfig,
				IncusProject:          app.cfg.Incus.Project,
				ProtectedPaths:        protectedPaths,
				HostImmutable:         app.cfg.Security.IsHostImmutableEnabled(),
				PreserveWorkspacePath: app.cfg.Paths.PreserveWorkspacePath,
				ForwardSSHAgent:       config.BoolVal(app.cfg.SSH.ForwardAgent),
				ForwardedEnvVars:      resolvedForwardedEnvVars,
				ContextFilePath:       app.cfg.Tool.ContextFile,
				ProfileContextFile:    app.cfg.ProfileContextFile,
				AutoContext:           app.cfg.Tool.AutoContext,
				ContainerName:         containerName,
				Timezone:              resolvedTimezone,
				Alias:                 app.cfg.Container.Alias,
			}
			return nil, nil
		},
	}
}

// setupContainerPhase calls session.Setup, saves early metadata for coi list,
// and registers the alias in the global registry. Its teardown calls
// session.Cleanup so the container and session data are handled correctly on
// all exit paths.
func setupContainerPhase(s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "setup-container",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			fmt.Fprintf(os.Stderr, "Setting up session %s...\n", s.sessionID)
			result, err := session.Setup(ctx, s.setupOpts)
			if err != nil {
				return nil, fmt.Errorf("failed to setup session: %w", err)
			}
			s.result = result

			if err := session.SaveMetadataEarly(s.sessionsDir, s.sessionID, result.ContainerName, s.absWorkspace, app.persistent, app.profile); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to save early metadata: %v\n", err)
			}

			if effectiveAlias := app.cfg.Container.Alias; effectiveAlias != "" {
				if reg, err := alias.Load(); err == nil {
					if err := reg.Register(effectiveAlias, s.absWorkspace, app.profile); err != nil {
						return nil, fmt.Errorf("alias conflict: %w", err)
					}
					if err := reg.Save(); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Failed to save alias registry: %v\n", err)
					}
				}
			}

			teardown := func() {
				fmt.Fprintf(os.Stderr, "\nCleaning up session...\n")
				if s.result.TimeoutMonitor != nil {
					s.result.TimeoutMonitor.Stop()
				}
				cleanupOpts := session.CleanupOptions{
					ContainerName:  s.result.ContainerName,
					SessionID:      s.sessionID,
					Persistent:     app.persistent,
					ProfileName:    app.profile,
					SessionsDir:    s.sessionsDir,
					SaveSession:    true,
					Workspace:      s.absWorkspace,
					Tool:           s.toolInstance,
					NetworkManager: s.result.NetworkManager,
					SessionLogger:  s.result.Logger,
				}
				if err := session.Cleanup(cleanupOpts); err != nil {
					fmt.Fprintf(os.Stderr, "Cleanup error: %v\n", err)
				}
			}
			return teardown, nil
		},
	}
}

// startMonitoringPhase starts the traditional and NFT monitoring daemons when
// enabled in config. Its teardown stops both daemons.
func startMonitoringPhase(s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "start-monitoring",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			if !config.BoolVal(app.cfg.Monitoring.Enabled) {
				return nil, nil
			}

			if err := startMonitoringDaemon(ctx, s.result.ContainerName, s.absWorkspace, app.cfg, s.result.Logger, &s.monitorDaemon); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to start monitoring daemon: %v\n", err)
			}

			if config.BoolVal(app.cfg.Monitoring.NFT.Enabled) {
				if err := startNFTMonitoringDaemon(ctx, s.result.ContainerName, app.cfg, s.result.Logger, &s.nftDaemon); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to start NFT monitoring: %v\n", err)
				}
			}

			teardown := func() {
				if s.monitorDaemon != nil {
					if err := s.monitorDaemon.Stop(); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Failed to stop monitoring daemon: %v\n", err)
					}
				}
				if s.nftDaemon != nil {
					if err := s.nftDaemon.Stop(); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Failed to stop NFT monitoring: %v\n", err)
					}
				}
			}
			return teardown, nil
		},
	}
}

// runToolPhase executes the AI coding tool (via tmux or direct mode) and
// normalises expected exit conditions (SIGINT, container shutdown) to nil.
func runToolPhase(s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "run-tool",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			fmt.Fprintf(os.Stderr, "\nStarting session...\n")
			fmt.Fprintf(os.Stderr, "Session ID: %s\n", s.sessionID)
			fmt.Fprintf(os.Stderr, "Container: %s\n", s.result.ContainerName)
			fmt.Fprintf(os.Stderr, "Workspace: %s\n", s.absWorkspace)

			useResumeFlag := (s.resumeID != "") && app.persistent
			restoreOnly := (s.resumeID != "") && !app.persistent

			var runErr error
			if s.useTmux {
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
				runErr = runCLIInTmux(s.result, s.sessionID, background, useResumeFlag, restoreOnly, s.sessionsDir, s.resumeID, s.toolInstance)
			} else {
				fmt.Fprintf(os.Stderr, "Mode: Direct (no tmux)\n")
				if restoreOnly {
					fmt.Fprintf(os.Stderr, "Resume mode: Restored conversation (auto-detect)\n")
				} else if useResumeFlag {
					fmt.Fprintf(os.Stderr, "Resume mode: Persistent session\n")
				}
				fmt.Fprintf(os.Stderr, "\n")
				runErr = runCLI(s.result, s.sessionID, useResumeFlag, restoreOnly, s.sessionsDir, s.resumeID, s.toolInstance)
			}

			// Normalise expected exit conditions so the pipeline doesn't log
			// a spurious error and the teardown (cleanup) still runs.
			if runErr != nil {
				errStr := runErr.Error()
				if errStr == "exit status 130" {
					return nil, nil
				}
				if strings.Contains(errStr, "Failed to retrieve PID") ||
					strings.Contains(errStr, "server exited") ||
					strings.Contains(errStr, "connection reset") ||
					errStr == "exit status 1" {
					return nil, nil
				}
			}
			return nil, runErr
		},
	}
}
