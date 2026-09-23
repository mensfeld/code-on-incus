package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mensfeld/code-on-incus/internal/bedrock"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/mensfeld/code-on-incus/internal/vmhost"
)

// setupState is the shared state threaded through the Setup pipeline. Each
// phase reads and mutates it in place; splitting Setup's former ~600-line body
// into phase methods keeps them small without changing the sequential data
// flow (result is built up field by field; the handful of intermediate values
// the monolith kept as locals — the resolved image, the reuse decision, the
// probed user, the port plan — live here instead).
type setupState struct {
	opts   SetupOptions
	result *SetupResult

	containerName string
	image         string
	skipLaunch    bool
	hasCodeUser   bool
	resolvedPorts []PublishedPort
}

// phase adapts a setupState method to the Phase interface.
func phase(name string, fn func(context.Context) (Teardown, error)) Phase {
	return PhaseFunc{PhaseName: name, RunFn: fn}
}

// 1. Generate or use existing container name, create the manager and session
// logger, and route the network package's diagnostics to the session log.
func (st *setupState) phaseResolveName(_ context.Context) (Teardown, error) {
	var containerName string
	if st.opts.ContainerName != "" {
		// Use existing container (for testing)
		containerName = st.opts.ContainerName
		st.opts.Logger(fmt.Sprintf("Using existing container: %s", containerName))
	} else {
		// Generate new container name
		containerName = ContainerName(st.opts.WorkspacePath, st.opts.SessionName, st.opts.Slot)
		st.opts.Logger(fmt.Sprintf("Container name: %s", containerName))
	}
	st.containerName = containerName
	st.result.ContainerName = containerName
	st.result.Manager = container.NewManager(containerName)

	hostHome, _ := os.UserHomeDir() // empty string on failure; logger.New handles it
	st.result.Logger = logger.New(containerName, hostHome)
	if w := st.result.Logger.InitWarning(); w != "" {
		st.opts.Logger(fmt.Sprintf("Warning: %s", w))
	}
	// Route the network package's diagnostics (incl. the background IP-refresh
	// goroutine) to the session log files instead of stderr, which in a coi
	// shell is the user's tmux terminal (issue #372).
	network.SetLogger(st.result.Logger)
	return nil, nil
}

// 1.5 Validate Bedrock setup if running in Colima/Lima.
func (st *setupState) phaseValidateBedrock(_ context.Context) (Teardown, error) {
	if vmhost.Detect().HandlesUIDMapping() && st.opts.CLIConfigPath != "" {
		settingsPath := filepath.Join(st.opts.CLIConfigPath, "settings.json")
		isConfigured, err := bedrock.IsBedrockConfigured(settingsPath)
		if err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Failed to check Bedrock configuration: %v", err))
		} else if isConfigured {
			st.opts.Logger("Detected AWS Bedrock configuration, validating setup...")

			// Validate Bedrock setup
			validationResult := bedrock.ValidateColimaBedrockSetup()

			// Check if .aws is mounted
			if st.opts.MountConfig != nil {
				var mountPaths []string
				for _, mount := range st.opts.MountConfig.Mounts {
					mountPaths = append(mountPaths, mount.HostPath)
				}
				if mountIssue := bedrock.CheckMountConfiguration(mountPaths); mountIssue != nil {
					validationResult.Issues = append(validationResult.Issues, *mountIssue)
				}
			}

			// If there are errors, fail with helpful message
			if validationResult.HasErrors() {
				return nil, fmt.Errorf("%s", validationResult.FormatError())
			}

			// Log warnings but continue
			if len(validationResult.Issues) > 0 {
				for _, issue := range validationResult.Issues {
					if issue.Severity == "warning" {
						st.opts.Logger(fmt.Sprintf("⚠️  %s", issue.Message))
					}
				}
			}
		}
	}
	return nil, nil
}

// Autofix: make sure the Incus bridge has iptables FORWARD ACCEPT rules before
// any container is started. Without them, containers cannot get IPs via DHCP
// when the FORWARD chain policy is DROP (e.g. when Docker is running).
func (st *setupState) phaseEnsureBridge(_ context.Context) (Teardown, error) {
	if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
		st.opts.Logger(fmt.Sprintf("Warning: could not ensure bridge forwarding rules: %v", err))
	} else if changed {
		st.opts.Logger(fmt.Sprintf("Added iptables FORWARD rules for %s (was missing — containers could not get IPs)", bridgeName))
	}
	return nil, nil
}

// 2. Determine image and verify it exists. 3. Set the provisional execution
// context (finalized in phaseDetectUser once the container is up).
func (st *setupState) phaseResolveImage(_ context.Context) (Teardown, error) {
	image := st.opts.Image
	if image == "" {
		image = CoiImage
	}
	st.result.Image = image
	st.image = image

	// Check if image exists
	exists, err := container.ImageExists(image)
	if err != nil {
		return nil, fmt.Errorf("failed to check image: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("image '%s' not found - run 'coi build' first", image)
	}

	// Provisional execution context — finalized at phaseDetectUser once the
	// container is up and we can probe it for the `code` user.
	//
	// Historically this was a literal string match against the coi-default
	// alias. That breaks custom images built FROM coi-default (they have
	// the `code` user inherited from the base layer but a different alias,
	// so the match returned false and the session was forced to root). We
	// now defer the final decision until after the container boots and
	// probe it directly.
	st.result.HomeDir = "/home/" + container.CodeUser
	return nil, nil
}

// 4. Check if the container already exists and decide whether to reuse,
// restart, or delete-and-recreate it; sets skipLaunch accordingly.
func (st *setupState) phaseReconcileExisting(_ context.Context) (Teardown, error) {
	exists, err := st.result.Manager.Exists()
	if err != nil {
		return nil, fmt.Errorf("failed to check if container exists: %w", err)
	}

	// An explicitly named container (--container) is attached to, never
	// created — so it must already exist. Without this guard a missing name
	// silently skips both the reuse branch (exists is false) and the
	// creation branch (skipLaunch is true), then fails with a misleading
	// "container not ready" only after the full ready_timeout wait.
	if st.opts.ContainerName != "" {
		if !exists {
			return nil, fmt.Errorf("container '%s' not found - omit --container to launch a new container for this workspace", st.opts.ContainerName)
		}
		st.skipLaunch = true
		st.opts.Logger("Using existing container, skipping creation...")
	}

	if exists {
		// Check if container is currently running
		running, err := st.result.Manager.Running()
		if err != nil {
			return nil, fmt.Errorf("failed to check if container is running: %w", err)
		}

		if running {
			// Container is running - this is an active session!
			if st.opts.Persistent || st.opts.ContainerName != "" || st.opts.ResumeFromID != "" {
				// Reuse running container if: persistent mode, --container flag, or explicit resume.
				// The resume case covers post-reboot Incus stateful restore: a non-persistent
				// container may be Running because Incus restored it; --resume should reuse it.
				// A RUNNING container cannot be remounted safely, so refuse
				// to attach when it still has a DIFFERENT workspace mounted —
				// the normal hazard for a named session (session_name) whose
				// previous checkout's session is still live. Attaching would
				// silently hand this launch the other checkout's files.
				// An EXPLICIT --container is exempt: naming the container is
				// the user saying "attach to that container as it is",
				// wherever it was created from (the documented testing flow).
				if st.opts.ContainerName == "" {
					if src := st.result.Manager.GetWorkspaceSource(); src == "" {
						st.opts.Logger("Warning: could not determine the running container's workspace source; skipping the workspace-match check")
					} else if !SameWorkspaceSource(src, st.opts.WorkspacePath) {
						return nil, fmt.Errorf(
							"container %s is running with a different workspace mounted (%s); "+
								"stop that session first (coi shutdown %s) or launch from that workspace",
							st.containerName, src, st.containerName)
					}
				}
				st.opts.Logger("Container already running, reusing...")
				// A RUNNING container's kernel-surface settings cannot be
				// changed (nesting is rejected, the deny list is racy), so
				// only surface a config mismatch instead of applying it. Skip
				// for an explicit --container attach: that means "use it as it
				// is", and its config may legitimately come from an Incus
				// profile we neither manage nor should second-guess.
				if st.opts.ContainerName == "" {
					warnHardeningMismatch(st.result.ContainerName, hardeningPolicyFrom(&st.opts), st.opts.Logger)
				}
				// Strip the previous session's port devices NOW, before the
				// port preflight below bind-probes: their live forkproxy
				// listeners would otherwise make the session collide with its
				// own ports (pinned entries hard-fail, pool numbers drift).
				RemoveStalePortDevices(st.result.Manager, st.opts.Logger)
				// Deliberately do NOT reconcile protect-* devices here. This
				// branch reuses an already-RUNNING container, so (a) there is no
				// Start(), hence no "Missing source path" start-validation to
				// prevent — the #610 wedge only bites the stopped branch below —
				// and (b) hot-removing a protect-* device from a live container
				// would DROP a read-only protection mid-session: a source that
				// went missing while still mounted keeps writes blocked, but
				// RemoveDevice would reopen it to a possibly-malicious agent.
				// Reconciliation happens on the next stopped->start cycle.
				st.skipLaunch = true
			} else {
				// A running container exists for this slot but we're not resuming or in
				// persistent mode — AllocateSlot() should have avoided this slot.
				return nil, fmt.Errorf("slot %d is already in use by a running container %s - this should not happen (bug in slot allocation)", st.opts.Slot, st.containerName)
			}
		} else {
			// Container exists but is stopped
			if st.opts.Persistent || st.opts.ContainerName != "" {
				// Restart the stopped container
				// This includes: persistent containers OR containers specified via --container flag
				if err := restartStoppedContainer(st.result, &st.opts, st.containerName); err != nil {
					return nil, err
				}
				st.skipLaunch = true
			} else {
				// Delete the stopped leftover container
				st.opts.Logger("Found stopped leftover container from previous session, deleting...")
				if err := st.result.Manager.Delete(true); err != nil {
					return nil, fmt.Errorf("failed to delete leftover container: %w", err)
				}
				// Brief pause to let Incus fully delete
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
	return nil, nil
}

// 4.6 Defense-in-depth: gate untrusted out-of-workspace mounts, forwarded
// sockets, ad-hoc credential entries, and port publications at the single
// chokepoint every caller passes through. Runs on reuse paths too.
func (st *setupState) phaseFilterTrusted(_ context.Context) (Teardown, error) {
	gatedMC, droppedM, gatedSC, droppedS, gatedCC, droppedC, gatedPC, droppedP := FilterTrusted(st.opts.MountConfig, st.opts.SocketConfig, st.opts.CredentialConfig, st.opts.PortConfig, st.opts.WorkspacePath)
	if st.skipLaunch && len(droppedM) > 0 {
		st.opts.Logger(fmt.Sprintf(
			"Warning: %d untrusted mount(s) remain attached from when this container was created; recreate it (coi kill + relaunch) to apply mount-trust changes",
			len(droppedM),
		))
	} else {
		for _, m := range droppedM {
			st.opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted mount from %s: %s -> %s (resolves outside the workspace; run 'coi trust' or set %s=1)",
				m.SourcePath, m.HostPath, m.ContainerPath, TrustEnvVar,
			))
		}
	}
	for _, s := range droppedS {
		st.opts.Logger(fmt.Sprintf(
			"Warning: ignoring untrusted socket from %s: %s -> %s (run 'coi trust' or set %s=1)",
			s.SourcePath, s.HostPath, s.ContainerPath, TrustEnvVar,
		))
	}
	for _, c := range droppedC {
		st.opts.Logger(fmt.Sprintf(
			"Warning: ignoring untrusted credential entry from %s: %s -> %s (run 'coi trust' or set %s=1)",
			c.SourcePath, c.HostPath, c.ContainerPath, TrustEnvVar,
		))
	}
	for _, p := range droppedP {
		st.opts.Logger(fmt.Sprintf(
			"Warning: ignoring untrusted %s from %s (a repo declaring host listeners can squat localhost ports; run 'coi trust' or set %s=1)",
			DescribeDroppedPort(p), p.SourcePath, TrustEnvVar,
		))
	}
	st.opts.MountConfig = gatedMC
	st.opts.SocketConfig = gatedSC
	st.opts.CredentialConfig = gatedCC
	st.opts.PortConfig = gatedPC

	// On reuse, [[mounts]] devices persist from creation and are never re-added
	// (like mount-trust above), so a changed per-mount `shift` would otherwise
	// be a silent no-op. Warn when an attached mount device's shift no longer
	// matches the current config (#604).
	if st.skipLaunch && st.opts.MountConfig != nil {
		WarnMountShiftDrift(st.result.ContainerName, st.opts.MountConfig.Mounts, st.opts.Logger)
	}
	return nil, nil
}

// 4.7 Preflight the port plan BEFORE any container is created (fresh path) and
// AFTER stale port devices were stripped (reuse path).
func (st *setupState) phasePreflightPorts(_ context.Context) (Teardown, error) {
	resolvedPorts, err := ResolvePorts(st.opts.PortConfig, st.opts.WorkspacePath, st.opts.SessionName, st.opts.Slot)
	if err != nil {
		return nil, fmt.Errorf("port preflight failed: %w", err)
	}
	st.resolvedPorts = resolvedPorts
	return nil, nil
}

// 5. Create and configure the container (unless reusing), and set/update alias
// metadata for running-container lookup.
func (st *setupState) phaseCreateContainer(_ context.Context) (Teardown, error) {
	// Always launch as non-ephemeral so we can save session data even if container is stopped
	// (e.g., via 'sudo shutdown 0' from within). Cleanup will delete unless persistent mode is configured.
	if !st.skipLaunch {
		if err := createAndStartContainer(st.result, &st.opts, st.image, st.containerName); err != nil {
			return nil, err
		}
	}

	// Set/update alias metadata on container (for running-container lookup).
	// This runs for both new and reused containers so alias changes are propagated.
	if st.opts.Alias != "" {
		if err := container.IncusExec("config", "set", st.result.ContainerName,
			fmt.Sprintf("user.coi.alias=%s", st.opts.Alias)); err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Failed to set alias metadata: %v", err))
		}
	}
	return nil, nil
}

// 6. Wait for the container to become ready.
func (st *setupState) phaseWaitReady(ctx context.Context) (Teardown, error) {
	readyTimeout := st.opts.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = 30
	}
	if err := WaitForReady(ctx, st.result.Manager, readyTimeout, st.opts.Logger); err != nil {
		return nil, AnnotateReadyTimeout(err, st.opts.LimitsConfig)
	}
	return nil, nil
}

// 6.1 Configure the Docker bridge CIDR to prevent IP conflicts.
func (st *setupState) phaseConfigureDockerBridge(_ context.Context) (Teardown, error) {
	// Written whenever Docker is enabled — on reuse too, not only fresh
	// launches: a persistent container created with docker=false and later
	// flipped to docker=true has no daemon.json yet. The write is idempotent.
	// Skipped for an explicit --container attach ("use it as it is").
	if st.opts.ContainerName == "" && hardeningPolicyFrom(&st.opts).DockerEnabled() {
		if err := ConfigureDockerDaemon(st.result.Manager, st.opts.Logger); err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Failed to configure Docker daemon: %v", err))
		}
	}
	return nil, nil
}

// 6 (cont). Size /tmp to prevent space exhaustion in big builds (POST-start,
// fresh launches only).
func (st *setupState) phaseSizeTmpfs(_ context.Context) (Teardown, error) {
	if !st.skipLaunch {
		ApplyTmpfsSizing(st.result.Manager, st.opts.LimitsConfig, st.opts.Logger)
	}
	return nil, nil
}

// 6.4 Finalize the execution context by probing the container for the `code`
// user; fall back to root when it has none.
func (st *setupState) phaseDetectUser(_ context.Context) (Teardown, error) {
	hasCodeUser, err := DetectCodeUser(st.result.Manager, container.CodeUser)
	if err != nil {
		st.opts.Logger(fmt.Sprintf("Warning: could not probe container for %s user: %v — falling back to root", container.CodeUser, err))
		hasCodeUser = false
	}
	st.hasCodeUser = hasCodeUser
	st.result.RunAsRoot = !hasCodeUser
	if st.result.RunAsRoot {
		st.result.HomeDir = "/root"
	} else {
		st.result.HomeDir = "/home/" + container.CodeUser
	}
	return nil, nil
}

// 6.5 Remap the container user UID/GID if the configured UID differs from the
// image default (1000).
func (st *setupState) phaseRemapUID(_ context.Context) (Teardown, error) {
	if !st.skipLaunch && st.hasCodeUser && container.CodeUID != 1000 {
		if err := remapContainerUser(st.result, st.opts); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// 6.6 Forward host sockets (SSH agent built-in entry plus configured
// [[sockets]]) to the running container.
func (st *setupState) phaseForwardSockets(_ context.Context) (Teardown, error) {
	st.result.SocketEnv = ForwardConfiguredSockets(st.result.Manager, st.opts.SocketConfig, st.opts.ForwardSSHAgent, st.opts.Logger)
	st.result.SSHAgentSocketPath = st.result.SocketEnv["SSH_AUTH_SOCK"]
	return nil, nil
}

// 6.6.05 Publish configured [ports] on the host (proxy devices).
func (st *setupState) phasePublishPorts(_ context.Context) (Teardown, error) {
	st.result.PublishedPorts, st.result.PortsEnv = PublishResolvedPorts(st.result.Manager, st.resolvedPorts, st.opts.Logger)
	return nil, nil
}

// 6.6.1 Prevent git from guessing a commit identity from the container user
// (and, under git.readonly, mount the identity read-only).
func (st *setupState) phaseConfigureGit(ctx context.Context) (Teardown, error) {
	if err := configureGitIdentity(ctx, st.result, st.opts); err != nil {
		return nil, err
	}
	return nil, nil
}

// 6.6.2 Claude managed settings: auto-mode prompt suppression and
// includeCoAuthoredBy handling. No-op for other tools.
func (st *setupState) phaseClaudeSettings(_ context.Context) (Teardown, error) {
	if st.opts.Tool != nil && st.opts.Tool.Name() == "claude" {
		SetupClaudeManagedSettings(st.result.Manager,
			shouldSuppressClaudeAutoMode(st.opts.Tool.Name(), st.opts.PermissionMode),
			st.opts.GitStripAttribution, st.opts.Logger)
	}
	return nil, nil
}

// 6.7 Configure the timezone inside the container (or reset to UTC).
func (st *setupState) phaseConfigureTimezone(_ context.Context) (Teardown, error) {
	// Always set result.Timezone so the TZ env var is applied even if the
	// filesystem configuration fails (some programs only check TZ).
	st.result.Timezone = st.opts.Timezone
	if st.opts.Timezone != "" {
		st.opts.Logger(fmt.Sprintf("Setting container timezone to %s...", st.opts.Timezone))
		tzCmd := fmt.Sprintf(
			"ln -sf /usr/share/zoneinfo/%s /etc/localtime && echo %s > /etc/timezone",
			st.opts.Timezone, st.opts.Timezone,
		)
		if _, err := st.result.Manager.ExecCommand(tzCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Failed to set timezone: %v", err))
		}
	} else {
		// Explicitly reset to UTC — important for persistent containers that may
		// have had a different timezone applied in a previous session.
		resetCmd := "ln -sf /usr/share/zoneinfo/UTC /etc/localtime && echo UTC > /etc/timezone"
		if _, err := st.result.Manager.ExecCommand(resetCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Failed to reset timezone to UTC: %v", err))
		}
	}
	return nil, nil
}

// 7. Start the timeout monitor if max_duration is configured.
func (st *setupState) phaseStartTimeoutMonitor(ctx context.Context) (Teardown, error) {
	if st.opts.LimitsConfig != nil && st.opts.LimitsConfig.Runtime.MaxDuration != "" {
		duration, err := limits.ParseDuration(st.opts.LimitsConfig.Runtime.MaxDuration)
		if err != nil {
			return nil, fmt.Errorf("invalid max_duration: %w", err)
		}
		if duration > 0 {
			st.result.TimeoutMonitor = limits.NewTimeoutMonitor(
				ctx,
				st.result.ContainerName,
				duration,
				config.BoolVal(st.opts.LimitsConfig.Runtime.AutoStop),
				config.BoolVal(st.opts.LimitsConfig.Runtime.StopGraceful),
				st.opts.IncusProject,
				st.result.Logger,
			)
			st.result.TimeoutMonitor.Start()
		}
	}
	return nil, nil
}

// 8. Setup network isolation (after the container is running and has an IP).
func (st *setupState) phaseSetupNetwork(ctx context.Context) (Teardown, error) {
	if st.opts.NetworkConfig != nil {
		st.result.NetworkManager = network.NewManager(st.opts.NetworkConfig, st.result.Logger)
		if err := st.result.NetworkManager.SetupForContainer(ctx, st.result.ContainerName); err != nil {
			return nil, fmt.Errorf("failed to setup network isolation: %w", err)
		}
	}
	return nil, nil
}

// 9 / 9.5 When resuming: restore session data if the container was recreated,
// inject fresh credentials, and refresh configured [[credentials]].
func (st *setupState) phaseResumeRestore(_ context.Context) (Teardown, error) {
	// Skip if tool uses ENV-based auth (no config directory)
	if st.opts.ResumeFromID != "" && st.opts.Tool != nil && st.opts.Tool.ConfigDirName() != "" {
		// If we launched a new container (not reusing persistent one), restore config from saved session
		if !st.skipLaunch && st.opts.SessionsDir != "" {
			if err := restoreSessionData(st.result.Manager, st.opts.ResumeFromID, st.result.HomeDir, st.opts.SessionsDir, st.opts.Tool, st.opts.Logger); err != nil {
				st.opts.Logger(fmt.Sprintf("Warning: Could not restore session data: %v", err))
			}
		}

		// Always inject fresh credentials/sandbox settings when resuming
		if tcf, ok := st.opts.Tool.(tool.ToolWithConfigDirFiles); ok {
			if st.opts.CLIConfigPath != "" || tcf.AlwaysSetupConfig() {
				if err := injectCredentials(st.result.Manager, st.opts.CLIConfigPath, st.result.HomeDir, tcf, st.opts.Logger); err != nil {
					st.opts.Logger(fmt.Sprintf("Warning: Could not inject credentials: %v", err))
				}
			}
		}
	}

	// Refresh/copy configured [[credentials]] entries (catalog bundles and
	// ad-hoc). Independent of which Tool is selected. Re-run on every resume
	// (idempotent) so a rotated host credential stays in sync; on a fresh
	// session it's applied once at phaseSetupCredentials alongside CLI tool config.
	if st.opts.ResumeFromID != "" && st.opts.CredentialConfig != nil && len(st.opts.CredentialConfig.Entries) > 0 {
		st.opts.Logger("Refreshing configured credentials...")
		if err := setupCredentials(st.result.Manager, st.result.HomeDir, st.opts.CredentialConfig.Entries, st.opts.Logger); err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Could not refresh credentials: %v", err))
		}
	}
	return nil, nil
}

// 10 / 10.2 / 10.5 Note reused mounts, auto-trust workspace mise config, and
// set the auto-context path for config-based tools (before setupCLIConfig).
func (st *setupState) phaseMountsAndContextPath(_ context.Context) (Teardown, error) {
	// Workspace and configured mounts are already mounted (added before container start in phaseCreateContainer)
	if st.skipLaunch {
		st.opts.Logger("Reusing existing workspace and mount configurations")
	}

	// Auto-trust mise config files in the workspace so mise doesn't
	// prompt or error when the workspace contains mise.toml / .tool-versions.
	SetupMiseTrust(st.result.Manager, st.result.ContainerWorkspacePath, st.opts.Logger)

	// Set auto-context path for config-based tools (must happen before setupCLIConfig
	// so the path is included in GetSandboxSettings output)
	if st.opts.Tool != nil && config.BoolVal(st.opts.AutoContext) {
		if acp, ok := st.opts.Tool.(tool.ToolWithAutoContextPath); ok {
			acp.SetAutoContextPath(filepath.Join(st.result.HomeDir, "SANDBOX_CONTEXT.md"))
		}
	}
	return nil, nil
}

// 11. Setup CLI tool config (skip if resuming — config already restored).
func (st *setupState) phaseSetupCLIConfig(_ context.Context) (Teardown, error) {
	if st.opts.Tool != nil {
		if tcf, ok := st.opts.Tool.(tool.ToolWithConfigDirFiles); ok {
			if st.opts.CLIConfigPath != "" && st.opts.ResumeFromID == "" {
				_, statErr := os.Stat(st.opts.CLIConfigPath)
				hostDirExists := statErr == nil

				if hostDirExists || tcf.AlwaysSetupConfig() {
					switch {
					case !st.skipLaunch:
						st.opts.Logger(fmt.Sprintf("Setting up %s config...", st.opts.Tool.Name()))
						if err := setupCLIConfig(st.result.Manager, st.opts.CLIConfigPath, st.result.HomeDir, tcf, st.opts.Logger); err != nil {
							st.opts.Logger(fmt.Sprintf("Warning: Failed to setup %s config: %v", st.opts.Tool.Name(), err))
						}
					case !toolConfigSeeded(st.result.Manager, st.result.HomeDir, tcf):
						// Persistent reuse with a tool coi hasn't seeded in this
						// container yet — e.g. a profile that shares [container]
						// session_name but sets a different [tool] name, so you
						// re-enter the same container (code, packages, state) with
						// another tool (#708 follow-up). Keyed on the tool's own
						// essential config files (not dir existence/content, which
						// the base image and agent installers pre-populate). Seed
						// its config once so it authenticates, without touching the
						// original tool's config or conversation history.
						st.opts.Logger(fmt.Sprintf("Setting up %s config in reused container (first use of this tool here)...", st.opts.Tool.Name()))
						if err := setupCLIConfig(st.result.Manager, st.opts.CLIConfigPath, st.result.HomeDir, tcf, st.opts.Logger); err != nil {
							st.opts.Logger(fmt.Sprintf("Warning: Failed to setup %s config: %v", st.opts.Tool.Name(), err))
						}
					default:
						st.opts.Logger(fmt.Sprintf("Reusing existing %s config (persistent container)", st.opts.Tool.Name()))
					}
				} else if statErr != nil && !os.IsNotExist(statErr) {
					return nil, fmt.Errorf("failed to check %s config directory: %w", st.opts.Tool.Name(), statErr)
				}
			} else if st.opts.ResumeFromID != "" {
				st.opts.Logger(fmt.Sprintf("Resuming session - using restored %s config", st.opts.Tool.Name()))
			}
		} else if st.opts.Tool.ConfigDirName() == "" {
			st.opts.Logger(fmt.Sprintf("Tool %s uses ENV-based auth, skipping config setup", st.opts.Tool.Name()))
		}
	}
	return nil, nil
}

// 11.1 Persist the tool's resolved env as container-level environment.* so
// every exec inherits the profile's tool config (#744).
func (st *setupState) phaseApplyToolEnv(ctx context.Context) (Teardown, error) {
	if st.opts.Tool != nil {
		applyToolContainerEnv(ctx, st.result.ContainerName, st.result.ContainerWorkspacePath, st.opts.Tool, st.opts.Logger)
	}
	return nil, nil
}

// 11.5 Setup configured [[credentials]] entries (fresh, non-resume launches).
func (st *setupState) phaseSetupCredentials(_ context.Context) (Teardown, error) {
	if !st.skipLaunch && st.opts.ResumeFromID == "" && st.opts.CredentialConfig != nil && len(st.opts.CredentialConfig.Entries) > 0 {
		st.opts.Logger("Setting up configured credentials...")
		if err := setupCredentials(st.result.Manager, st.result.HomeDir, st.opts.CredentialConfig.Entries, st.opts.Logger); err != nil {
			st.opts.Logger(fmt.Sprintf("Warning: Failed to setup credentials: %v", err))
		}
	}
	return nil, nil
}

// 12 / 13 Inject the sandbox context file (~/SANDBOX_CONTEXT.md) and, for tools
// that support it, the native auto-context file (e.g. Claude's ~/.claude/CLAUDE.md).
func (st *setupState) phaseInjectContext(_ context.Context) (Teardown, error) {
	// Runs for both new and resumed sessions so dynamic info stays current.
	contextContent := injectSandboxContext(st.result, st.opts)

	if st.opts.Tool != nil && config.BoolVal(st.opts.AutoContext) && contextContent != "" {
		if acf, ok := st.opts.Tool.(tool.ToolWithAutoContextFile); ok {
			if err := injectAutoContextFile(st.result.Manager, acf, contextContent, st.result.HomeDir, st.opts.Logger); err != nil {
				st.opts.Logger(fmt.Sprintf("Warning: Failed to inject auto-context file: %v", err))
			}
		}
	}
	return nil, nil
}
