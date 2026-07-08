package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/monitor"
	"github.com/mensfeld/code-on-incus/internal/nftmonitor"
	"github.com/mensfeld/code-on-incus/internal/session"
)

// runState is the mutable state accumulated across run pipeline phases.
// Each phase reads its inputs from state and writes its outputs back.
type runState struct {
	// After resolve-workspace
	absWorkspace string

	// Run-script mode (coi run with no command): execute <workspace>/coi-run
	// from the workspace mount. The file is required to be executable, so it
	// runs directly and its shebang decides the interpreter.
	runScript bool

	// After validate-env
	containerName  string
	img            string
	effectiveAlias string

	// After launch-container (mount/socket config is parsed + trust-gated and
	// all disk devices are attached by the launch phase's pre-start hook; only
	// the wasRestarted reuse path resolves containerWorkspace later, in
	// configure-container)
	mgr                container.ContainerManager
	wasRestarted       bool
	useShift           bool                       // resolved by the launch phase's UID-mapping pre-start hook
	containerWorkspace string                     // in-container workspace path
	gitWorktree        *session.GitWorktreeLayout // external git dirs for a worktree checkout (#533), nil otherwise
	mountConfig        *session.MountConfig       // trust-gated
	socketConfig       *session.SocketConfig      // trust-gated

	// After apply-network
	socketEnv map[string]string
	tz        string

	// After start-monitoring
	monitorDaemon monitor.MonitorDaemon
	nftDaemon     nftmonitor.NFTMonitorDaemon
}

// validateEnvRunPhase checks Incus availability, allocates a slot, resolves
// the image name, and validates the storage pool and alias.
func (a *App) validateEnvRunPhase(s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "validate-env",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			if !container.Available() {
				return nil, container.IncusNotAvailableError()
			}
			if err := container.CheckMinimumVersion(); err != nil {
				return nil, err
			}
			if warning := container.CheckKernelVersion(); warning != "" {
				fmt.Fprintf(os.Stderr, "%s\n", warning)
			}

			slotNum := a.slot
			var err error
			if slotNum == 0 {
				// Persistent mode: prefer the stopped container from a
				// previous run — plain AllocateSlot treats it as occupying
				// its slot and would silently launch a FRESH container on
				// the next slot (state never persists, slots exhaust).
				if a.persistent {
					if reuse, ok := session.FindReusablePersistentSlot(s.absWorkspace, 10); ok {
						slotNum = reuse
						fmt.Fprintf(os.Stderr, "Reusing persistent container on slot %d\n", slotNum)
					}
				}
				if slotNum == 0 {
					slotNum, err = session.AllocateSlot(s.absWorkspace, 10)
					if err != nil {
						return nil, fmt.Errorf("failed to allocate slot: %w", err)
					}
					fmt.Fprintf(os.Stderr, "Auto-allocated slot %d\n", slotNum)
				}
			}
			s.containerName = session.ContainerName(s.absWorkspace, slotNum)

			img := ResolveImageName(a.cfg)
			if err := AutoBuildIfNeeded(a.cfg, img); err != nil {
				return nil, err
			}
			if err := CheckAndReportStaleBase(a.cfg, img); err != nil {
				return nil, err
			}
			if err := container.ValidateStoragePool(a.cfg.Container.StoragePool); err != nil {
				return nil, err
			}
			s.img = img

			effectiveAlias, err := validateAndPrepareAlias(a.cfg.Container.Alias)
			if err != nil {
				return nil, err
			}
			s.effectiveAlias = effectiveAlias
			return nil, nil
		},
	}
}

// launchContainerRunPhase launches or reuses the container and registers a
// teardown that stops or deletes it on exit.
func (a *App) launchContainerRunPhase(s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "launch-container",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			ensureBridgeTrustedZone()
			fmt.Fprintf(os.Stderr, "Launching container %s from image %s...\n", s.containerName, s.img)

			mgr := container.NewManager(s.containerName)
			containerExists, err := mgr.Exists()
			if err != nil {
				return nil, fmt.Errorf("failed to check if container exists: %w", err)
			}
			s.wasRestarted = containerExists && a.persistent

			// Mount/socket config is parsed and trust-gated BEFORE launch so the
			// pre-start hook below can attach every disk device to the stopped
			// container (all host-side computation; no container required).
			mc, err := ParseMountConfig(a.cfg)
			if err != nil {
				return nil, fmt.Errorf("invalid mount configuration: %w", err)
			}
			sc, err := ParseSocketConfig(a.cfg)
			if err != nil {
				return nil, fmt.Errorf("invalid socket configuration: %w", err)
			}
			// One combined trust gate over mounts + sockets, so the per-source
			// fingerprint matches what `coi trust` recorded.
			s.mountConfig, s.socketConfig = a.gateRunForwarding(mc, sc, s.absWorkspace, s.wasRestarted)

			// Pre-start hook: runs AFTER init but BEFORE first start (fresh
			// launches only; reused persistent containers keep their creation-time
			// devices and mapping). Two jobs:
			//  1. Apply the workspace UID mapping (raw.idmap on a host/code UID
			//     mismatch; Colima/Lima auto-detect) so it takes effect at first
			//     boot (#530).
			//  2. Attach ALL disk devices (workspace, additional mounts, security
			//     + secret masks) to the STOPPED container, exactly like the shell
			//     path does. The devices then materialize at start, so a
			//     filesystem that cannot satisfy security.idmap.isolated (e.g. a
			//     virtiofs workspace under OrbStack/Colima) fails the START — where
			//     the existing isolation fallback disables isolation and retries —
			//     instead of failing a post-start hot-mount with no fallback at
			//     all (issue #534).
			// If the ephemeral isolation fallback recreates the container, this
			// hook re-runs on the fresh container, re-applying mapping and devices.
			s.useShift = !a.cfg.Incus.DisableShift
			logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
			preStart := func() error {
				s.useShift, _ = session.ConfigureUIDMapping(s.containerName, a.cfg.Incus.DisableShift, logFn)
				// Restricted/allowlist disable IPv6 in the container (post-start,
				// via the network manager). Pre-seed an IPv4-only networkd config
				// so the link reaches "configured" and systemd-networkd-wait-online
				// does not hang (#548). Non-fatal.
				if m := a.cfg.Network.Mode; m != "" && m != config.NetworkModeOpen {
					if err := container.ConfigureNetworkdIPv4Only(s.containerName); err != nil {
						logFn(fmt.Sprintf("Warning: networkd IPv4-only config not applied: %v", err))
					}
				}
				// Detect a git worktree checkout (.git is a file → external git dirs).
				// A valid layout forces preserve-path so git's pointers resolve, and its
				// internals are mounted + protected in applyWorkspaceMounts (#533).
				layout, wtErr := session.ResolveGitWorktree(s.absWorkspace)
				if wtErr != nil {
					logFn(fmt.Sprintf("Warning: git worktree not mounted (%v); git commands may fail in the container", wtErr))
				}
				s.gitWorktree = layout
				if layout != nil && workspaceUnderSystemDir(s.absWorkspace) {
					return fmt.Errorf("git worktree workspace %q is under a system directory; cannot preserve its host path to mount git internals safely", s.absWorkspace)
				}
				s.containerWorkspace = a.resolveContainerWorkspacePath(s.absWorkspace, layout != nil)
				return a.applyWorkspaceMounts(mgr, s.containerName, s.absWorkspace, &s.containerWorkspace, s.mountConfig, s.useShift, false, layout)
			}

			s.mgr = mgr
			// The teardown pairs with THIS phase's acquisitions: the pre-start
			// hook applies host-side immutable flags (chattr +i on protected
			// paths inside the workspace), so their removal belongs here — not in
			// a later phase whose teardown never registers if that phase (or this
			// one) fails first. Leaked +i flags make the workspace undeletable
			// (pytest tmpdir cleanup EPERM).
			teardown := func() {
				logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
				session.RemoveImmutable(s.containerName, logFn)
				if !a.persistent {
					fmt.Fprintf(os.Stderr, "Cleaning up container %s...\n", s.containerName)
					_ = s.mgr.Delete(true)
				} else {
					if running, _ := s.mgr.Running(); running {
						fmt.Fprintf(os.Stderr, "Stopping persistent container %s...\n", s.containerName)
						_ = s.mgr.Stop(false)
					}
				}
			}

			if err := launchOrReuseContainer(mgr, s.img, a.cfg.Container.StoragePool, s.containerName, containerExists, a.persistent, preStart); err != nil {
				// No teardown on a failed launch: launchOrReuseContainer already
				// removed the half-created container when it was safe to (never a
				// running one — a concurrent invocation may own it), and the
				// teardown's unconditional delete must not fire on a container
				// that may not be ours. Only the immutable flags the pre-start
				// hook may have applied need releasing here.
				logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
				session.RemoveImmutable(s.containerName, logFn)
				return nil, err
			}

			// From here the container is definitively ours (we created or
			// restarted it) — register the teardown, including alongside an
			// error: the pipeline registers teardowns returned with a failed Run.
			if err := applyContainerAlias(s.effectiveAlias, s.containerName, s.absWorkspace); err != nil {
				return teardown, err
			}

			return teardown, nil
		},
	}
}

// configureContainerRunPhase applies resource limits, waits for the container
// to be ready, and remaps the code user's UID/GID if configured. Disk devices
// (workspace, additional mounts, security + secret overlays) and the immutable
// flags are the launch phase's job now — attached by its pre-start hook,
// released by its teardown (#534); only the wasRestarted reuse path resolves
// its existing workspace mount path here.
func (a *App) configureContainerRunPhase(s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "configure-container",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			if !s.wasRestarted {
				limitsConfig := &a.cfg.Limits
				if hasAnyLimits(limitsConfig) {
					fmt.Fprintf(os.Stderr, "Applying resource limits...\n")
					applyOpts := limits.ApplyOptions{
						ContainerName: s.containerName,
						CPU: limits.CPULimits{
							Count:     limitsConfig.CPU.Count,
							Allowance: limitsConfig.CPU.Allowance,
							Priority:  limitsConfig.CPU.Priority,
						},
						Memory: limits.MemoryLimits{
							Limit:   limitsConfig.Memory.Limit,
							Enforce: limitsConfig.Memory.Enforce,
							Swap:    limitsConfig.Memory.Swap,
						},
						Disk: limits.DiskLimits{
							Read:     limitsConfig.Disk.Read,
							Write:    limitsConfig.Disk.Write,
							Max:      limitsConfig.Disk.Max,
							Priority: limitsConfig.Disk.Priority,
						},
						Runtime: limits.RuntimeLimits{
							MaxProcesses: limitsConfig.Runtime.MaxProcesses,
						},
						Project: a.cfg.Incus.Project,
					}
					if err := limits.ApplyResourceLimits(applyOpts); err != nil {
						return nil, fmt.Errorf("failed to apply resource limits: %w", err)
					}
				}
			}

			fmt.Fprintf(os.Stderr, "Waiting for container to be ready...\n")
			if err := waitForContainer(s.mgr, 30); err != nil {
				return nil, err
			}

			if !s.wasRestarted {
				logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
				if err := session.ConfigureDockerDaemon(s.mgr, logFn); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to configure Docker daemon: %v\n", err)
				}
				if err := remapContainerUserIfNeeded(s.mgr, s.wasRestarted); err != nil {
					return nil, err
				}
			}

			// Disk devices (workspace, additional mounts, security + secret masks)
			// and the UID mapping were applied by the launch phase's pre-start
			// hook, BEFORE first start — so raw.idmap takes effect at boot (#530)
			// and a device on an idmap-incompatible filesystem (virtiofs) fails
			// the start where the isolation fallback covers it (#534). Only a
			// reused persistent container has work left here: resolve its existing
			// workspace mount path from the container config.
			if s.wasRestarted {
				// Reuse: devices (incl. any worktree mounts) persist from creation, so
				// applyWorkspaceMounts returns early without remounting; layout is nil.
				if err := a.applyWorkspaceMounts(s.mgr, s.containerName, s.absWorkspace, &s.containerWorkspace, s.mountConfig, !a.cfg.Incus.DisableShift, true, nil); err != nil {
					return nil, err
				}
			}

			// No teardown: the immutable-flag removal moved to the launch phase's
			// teardown, alongside the pre-start hook that now applies them —
			// so it runs even when a later phase (this one included) fails
			// before registering anything.
			return nil, nil
		},
	}
}

// applyNetworkRunPhase forwards the SSH agent and any configured [[sockets]],
// installs network isolation rules, configures the timezone, and trusts mise
// config. Its teardown removes the firewall rules before the container is deleted.
func (a *App) applyNetworkRunPhase(s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "apply-network",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			s.socketEnv = a.applyForwardSockets(s.mgr, s.socketConfig)

			nm, err := a.applyNetworkIsolation(ctx, s.containerName)
			if err != nil {
				return nil, err
			}

			s.tz = a.applyContainerTimezone(s.mgr)

			session.SetupMiseTrust(s.mgr, s.containerWorkspace, func(msg string) {
				fmt.Fprintf(os.Stderr, "%s\n", msg)
			})

			if nm == nil {
				return nil, nil
			}
			teardown := func() {
				// Use a fresh background context for teardown: the run context
				// may already be cancelled (SIGINT), but we must still remove
				// the nftables rules before the container is deleted.
				if err := nm.Teardown(context.Background(), s.containerName); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to teardown network isolation: %v\n", err)
				}
			}
			return teardown, nil
		},
	}
}

// startMonitoringRunPhase starts the security monitoring daemons for the run
// container via the shared startSessionMonitoring helper (same watchers as an
// agent session). Threat events land in the audit log via the responder;
// diagnostics use a discard logger (run has no session log file).
func (a *App) startMonitoringRunPhase(s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "start-monitoring",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			teardown := a.startSessionMonitoring(ctx, s.containerName, s.absWorkspace, logger.NewDiscard(), &s.monitorDaemon, &s.nftDaemon)
			return teardown, nil
		},
	}
}

// runCommandPhase starts the optional timeout monitor, executes the user's
// command inside the container via incus exec, and returns any exit-code error.
func (a *App) runCommandPhase(args []string, s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "run-command",
		RunFn: func(ctx context.Context) (session.Teardown, error) {
			var timeoutMon *limits.TimeoutMonitor
			if a.cfg.Limits.Runtime.MaxDuration != "" {
				maxDur, _ := limits.ParseDuration(a.cfg.Limits.Runtime.MaxDuration)
				autoStop := config.BoolVal(a.cfg.Limits.Runtime.AutoStop)
				if a.cfg.Limits.Runtime.AutoStop == nil {
					autoStop = true
				}
				stopGraceful := config.BoolVal(a.cfg.Limits.Runtime.StopGraceful)
				runLog := logger.NewDiscard()
				timeoutMon = limits.NewTimeoutMonitor(ctx, s.containerName, maxDur, autoStop, stopGraceful, a.cfg.Incus.Project, runLog)
				timeoutMon.Start()
			}
			defer func() {
				if timeoutMon != nil {
					timeoutMon.Stop()
				}
			}()

			// Run-script mode: execute the script straight from the workspace
			// mount — it comes from the host, nothing is pushed. It runs
			// directly, so the shebang decides the interpreter.
			execArgs := args
			if s.runScript {
				execArgs = []string{s.containerWorkspace + "/" + runScriptName}
				fmt.Fprintf(os.Stderr, "Running workspace run script: %s\n", runScriptName)
			} else {
				fmt.Fprintf(os.Stderr, "Executing: %s\n", strings.Join(args, " "))
			}

			incusArgs := []string{
				"exec", s.containerName, "--user", fmt.Sprintf("%d", container.CodeUID),
				"--group", fmt.Sprintf("%d", container.CodeUID), "--cwd", s.containerWorkspace,
			}
			incusArgs, err := a.appendEnvArgs(incusArgs, s.tz, s.socketEnv)
			if err != nil {
				return nil, err
			}
			incusArgs = append(incusArgs, "--")
			incusArgs = append(incusArgs, execArgs...)

			// Streamed exec: output appears live (a long build isn't silent
			// until completion) and stdin is connected for piped input.
			if err := container.IncusExecStreamedContext(ctx, incusArgs...); err != nil {
				if exitErr, ok := err.(*container.ExitError); ok {
					fmt.Fprintf(os.Stderr, "\nCommand exited with code %d\n", exitErr.ExitCode)
					return nil, &ExitCodeError{Code: exitErr.ExitCode}
				}
				return nil, fmt.Errorf("command failed: %w", err)
			}

			fmt.Fprintf(os.Stderr, "\nCommand completed successfully\n")
			return nil, nil
		},
	}
}
