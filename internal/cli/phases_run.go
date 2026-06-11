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
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/session"
)

// runState is the mutable state accumulated across run pipeline phases.
// Each phase reads its inputs from state and writes its outputs back.
type runState struct {
	// After resolve-workspace
	absWorkspace string

	// After validate-env
	slotNum       int
	containerName string
	img           string
	effectiveAlias string

	// After launch-container
	mgr          container.ContainerManager
	wasRestarted bool

	// After configure-container
	containerWorkspace string

	// After apply-network
	sshAgentSocketPath string
	networkMgr         network.NetworkManager
	tz                 string
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
				slotNum, err = session.AllocateSlot(s.absWorkspace, 10)
				if err != nil {
					return nil, fmt.Errorf("failed to allocate slot: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Auto-allocated slot %d\n", slotNum)
			}
			s.slotNum = slotNum
			s.containerName = session.ContainerName(s.absWorkspace, slotNum)

			img := ResolveImageName(a.imageName, a.cfg)
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

			if err := launchOrReuseContainer(mgr, s.img, a.cfg.Container.StoragePool, containerExists, a.persistent); err != nil {
				return nil, err
			}

			if err := applyContainerAlias(s.effectiveAlias, s.containerName, s.absWorkspace); err != nil {
				return nil, err
			}

			s.mgr = mgr
			s.wasRestarted = containerExists && a.persistent

			teardown := func() {
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
			return teardown, nil
		},
	}
}

// configureContainerRunPhase applies resource limits, waits for the container
// to be ready, remaps UID/GID if needed, mounts the workspace and security
// paths, and registers an immutable-removal teardown.
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
				if err := remapContainerUserIfNeeded(s.mgr, false); err != nil {
					return nil, err
				}
			}

			s.containerWorkspace = a.resolveContainerWorkspacePath(s.absWorkspace)
			useShift := !a.cfg.Incus.DisableShift
			if err := a.applyWorkspaceMounts(s.mgr, s.containerName, s.absWorkspace, &s.containerWorkspace, useShift, s.wasRestarted); err != nil {
				return nil, err
			}

			teardown := func() {
				logFn := func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) }
				session.RemoveImmutable(s.containerName, logFn)
			}
			return teardown, nil
		},
	}
}

// applyNetworkRunPhase forwards the SSH agent, installs network isolation
// rules, configures the timezone, and trusts mise config. Its teardown removes
// the firewall rules before the container is deleted.
func (a *App) applyNetworkRunPhase(s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "apply-network",
		RunFn: func(_ context.Context) (session.Teardown, error) {
			socketPath, err := a.applySSHAgentForwarding(s.mgr, s.containerName)
			if err != nil {
				return nil, err
			}
			s.sshAgentSocketPath = socketPath

			nm, err := a.applyNetworkIsolation(s.containerName)
			if err != nil {
				return nil, err
			}
			s.networkMgr = nm

			s.tz = a.applyContainerTimezone(s.mgr)

			session.SetupMiseTrust(s.mgr, s.containerWorkspace, func(msg string) {
				fmt.Fprintf(os.Stderr, "%s\n", msg)
			})

			if nm == nil {
				return nil, nil
			}
			teardown := func() {
				if err := nm.Teardown(context.Background(), s.containerName); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to teardown network isolation: %v\n", err)
				}
			}
			return teardown, nil
		},
	}
}

// runCommandPhase starts the optional timeout monitor, executes the user's
// command inside the container via incus exec, and returns any exit-code error.
func (a *App) runCommandPhase(ctx context.Context, args []string, s *runState) session.Phase {
	return session.PhaseFunc{
		PhaseName: "run-command",
		RunFn: func(_ context.Context) (session.Teardown, error) {
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

			fmt.Fprintf(os.Stderr, "Executing: %s\n", strings.Join(args, " "))

			incusArgs := []string{
				"exec", s.containerName, "--user", fmt.Sprintf("%d", container.CodeUID),
				"--group", fmt.Sprintf("%d", container.CodeUID), "--cwd", s.containerWorkspace,
			}
			incusArgs = a.appendEnvArgs(incusArgs, s.tz, s.sshAgentSocketPath)
			incusArgs = append(incusArgs, "--")
			incusArgs = append(incusArgs, args...)

			output, err := container.IncusOutputWithArgs(incusArgs...)
			if output != "" {
				fmt.Print(output)
			}

			if timeoutMon != nil {
				timeoutMon.Stop()
			}

			if err != nil {
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
