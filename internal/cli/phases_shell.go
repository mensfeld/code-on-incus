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
	monitorDaemon monitor.MonitorDaemon
	nftDaemon     nftmonitor.NFTMonitorDaemon
}

// resolveWorkspacePhase resolves the absolute workspace path from --workspace
// or alias, overlays the workspace's project config, and applies any alias
// profile/slot inherited from the registry.
func (a *App) resolveWorkspacePhase(cmd *cobra.Command, s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "resolve-workspace",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			absWorkspace, err := filepath.Abs(a.workspace)
			if err != nil {
				return nil, fmt.Errorf("invalid workspace path: %w", err)
			}

			// When no alias is given, overlay the workspace's project config when it
			// differs from CWD (config.Load only reads from CWD).
			if s.aliasArg == "" {
				if cwd, err := os.Getwd(); err == nil {
					if absCWD, err := filepath.Abs(cwd); err == nil && absWorkspace != absCWD {
						if err := a.cfg.OverlayProjectConfig(absWorkspace); err != nil && !os.IsNotExist(err) {
							return nil, fmt.Errorf("failed to load project config from %s: %w", absWorkspace, err)
						}
						container.Configure(a.cfg.Incus.Project, a.cfg.Incus.CodeUser, a.cfg.Incus.CodeUID)
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

				if err := a.cfg.OverlayProjectConfig(absWorkspace); err != nil && !os.IsNotExist(err) {
					return nil, fmt.Errorf("failed to load project config from %s: %w", absWorkspace, err)
				}

				if resolved.Profile != "" && !cmd.Flags().Changed("profile") {
					a.profile = resolved.Profile
					if err := a.cfg.ApplyProfile(a.profile); err != nil {
						return nil, err
					}
				}
				container.Configure(a.cfg.Incus.Project, a.cfg.Incus.CodeUser, a.cfg.Incus.CodeUID)
				if resolved.Slot > 0 && !cmd.Flags().Changed("slot") {
					a.slot = resolved.Slot
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
func (a *App) validateEnvPhase(cmd *cobra.Command, s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "validate-env",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			// Config default, overridden by explicit --tmux flag.
			useTmuxDefault := true
			if a.cfg.Shell.UseTmux != nil {
				useTmuxDefault = *a.cfg.Shell.UseTmux
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
func (a *App) configureSessionPhase(cmd *cobra.Command, s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "configure-session",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			// Override tool from --tool flag.
			if toolFlag != "" {
				a.cfg.Tool.Name = toolFlag
			}
			ti, err := getConfiguredTool(a.cfg)
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
			resumeID := a.resume
			if a.continueSession != "" {
				resumeID = a.continueSession
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
						a.profile = metadata.ProfileName
						if err := a.cfg.ApplyProfile(a.profile); err != nil {
							return nil, fmt.Errorf("failed to apply saved profile '%s': %w", a.profile, err)
						}
						if !cmd.Flags().Changed("persistent") {
							a.persistent = config.BoolVal(a.cfg.Container.Persistent)
						}
						fmt.Fprintf(os.Stderr, "Inherited profile '%s' from session\n", a.profile)
					}
					if !cmd.Flags().Changed("persistent") {
						a.persistent = metadata.Persistent
						if a.persistent {
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
			slotNum := a.slot
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
			img := ResolveImageName(a.imageName, a.cfg)
			if err := AutoBuildIfNeeded(a.cfg, img); err != nil {
				return nil, err
			}
			if err := CheckAndReportStaleBase(a.cfg, img); err != nil {
				return nil, err
			}

			// Build config-derived options.
			networkConfig := a.cfg.Network
			limitsConfig := &a.cfg.Limits
			var protectedPaths []string
			if !a.cfg.Security.DisableProtection {
				protectedPaths = a.cfg.Security.GetEffectiveProtectedPaths()
			}
			protectedPaths = filterWritableGitHooks(protectedPaths, a.cfg)

			var cliConfigPath string
			if configDirName := ti.ConfigDirName(); configDirName != "" {
				cliConfigPath = filepath.Join(homeDir, configDirName)
			}

			resolvedForwardedEnvVars := resolveForwardedEnvVarNames(a.cfg.Defaults.ForwardEnv)
			resolvedTimezone := resolveTimezone(a.cfg)

			mountConfig, err := ParseMountConfig(a.cfg)
			if err != nil {
				return nil, fmt.Errorf("invalid mount configuration: %w", err)
			}
			mountConfig = gateUntrustedMounts(mountConfig, s.absWorkspace)
			if err := session.ValidateMounts(mountConfig); err != nil {
				return nil, fmt.Errorf("mount validation failed: %w", err)
			}

			if a.cfg.Container.Alias != "" {
				if err := alias.ValidateAlias(a.cfg.Container.Alias); err != nil {
					return nil, fmt.Errorf("invalid container alias: %w", err)
				}
			}

			s.setupOpts = session.SetupOptions{
				WorkspacePath:         s.absWorkspace,
				Image:                 img,
				Persistent:            a.persistent,
				ResumeFromID:          resumeID,
				Slot:                  slotNum,
				MountConfig:           mountConfig,
				SessionsDir:           sessionsDir,
				CLIConfigPath:         cliConfigPath,
				Tool:                  ti,
				NetworkConfig:         &networkConfig,
				DisableShift:          a.cfg.Incus.DisableShift,
				LimitsConfig:          limitsConfig,
				IncusProject:          a.cfg.Incus.Project,
				ProtectedPaths:        protectedPaths,
				HostImmutable:         a.cfg.Security.IsHostImmutableEnabled(),
				PreserveWorkspacePath: a.cfg.Paths.PreserveWorkspacePath,
				ForwardSSHAgent:       config.BoolVal(a.cfg.SSH.ForwardAgent),
				ForwardedEnvVars:      resolvedForwardedEnvVars,
				ContextFilePath:       a.cfg.Tool.ContextFile,
				ProfileContextFile:    a.cfg.ProfileContextFile,
				AutoContext:           a.cfg.Tool.AutoContext,
				ContainerName:         containerName,
				Timezone:              resolvedTimezone,
				Alias:                 a.cfg.Container.Alias,
			}
			return nil, nil
		},
	}
}

// setupContainerPhase calls session.Setup, saves early metadata for coi list,
// and registers the alias in the global registry. Its teardown calls
// session.Cleanup so the container and session data are handled correctly on
// all exit paths.
func (a *App) setupContainerPhase(s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "setup-container",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			fmt.Fprintf(os.Stderr, "Setting up session %s...\n", s.sessionID)
			result, err := session.Setup(ctx, s.setupOpts)
			if err != nil {
				return nil, fmt.Errorf("failed to setup session: %w", err)
			}
			s.result = result

			if err := session.SaveMetadataEarly(s.sessionsDir, s.sessionID, result.ContainerName, s.absWorkspace, a.persistent, a.profile); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to save early metadata: %v\n", err)
			}

			if effectiveAlias := a.cfg.Container.Alias; effectiveAlias != "" {
				if reg, err := alias.Load(); err == nil {
					if err := reg.Register(effectiveAlias, s.absWorkspace, a.profile); err != nil {
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
					Persistent:     a.persistent,
					ProfileName:    a.profile,
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
func (a *App) startMonitoringPhase(s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "start-monitoring",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			if !config.BoolVal(a.cfg.Monitoring.Enabled) {
				return nil, nil
			}

			if err := startMonitoringDaemon(ctx, s.result.ContainerName, s.absWorkspace, a.cfg, s.result.Logger, &s.monitorDaemon); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to start monitoring daemon: %v\n", err)
			}

			if config.BoolVal(a.cfg.Monitoring.NFT.Enabled) {
				if err := startNFTMonitoringDaemon(ctx, s.result.ContainerName, a.cfg, s.result.Logger, &s.nftDaemon); err != nil {
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
func (a *App) runToolPhase(s *shellState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "run-tool",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			fmt.Fprintf(os.Stderr, "\nStarting session...\n")
			fmt.Fprintf(os.Stderr, "Session ID: %s\n", s.sessionID)
			fmt.Fprintf(os.Stderr, "Container: %s\n", s.result.ContainerName)
			fmt.Fprintf(os.Stderr, "Workspace: %s\n", s.absWorkspace)

			useResumeFlag := (s.resumeID != "") && a.persistent
			restoreOnly := (s.resumeID != "") && !a.persistent

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
				runErr = a.runCLIInTmux(s.result, s.sessionID, background, useResumeFlag, restoreOnly, s.sessionsDir, s.resumeID, s.toolInstance)
			} else {
				fmt.Fprintf(os.Stderr, "Mode: Direct (no tmux)\n")
				if restoreOnly {
					fmt.Fprintf(os.Stderr, "Resume mode: Restored conversation (auto-detect)\n")
				} else if useResumeFlag {
					fmt.Fprintf(os.Stderr, "Resume mode: Persistent session\n")
				}
				fmt.Fprintf(os.Stderr, "\n")
				runErr = a.runCLI(s.result, s.sessionID, useResumeFlag, restoreOnly, s.sessionsDir, s.resumeID, s.toolInstance)
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
