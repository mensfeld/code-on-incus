package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/bedrock"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/timing"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/mensfeld/code-on-incus/internal/vmhost"
)

const (
	DefaultImage = "images:ubuntu/22.04"
	CoiImage     = "coi-default"
)

// SetupOptions contains options for setting up a session
type SetupOptions struct {
	WorkspacePath         string
	SessionName           string // [container] session_name: keys the session identity instead of the workspace path when set
	Image                 string
	StoragePool           string // [container] storage_pool: Incus storage pool for the container (empty = Incus default pool)
	Persistent            bool   // Keep container between sessions (don't delete on cleanup)
	ResumeFromID          string
	Slot                  int
	MountConfig           *MountConfig      // Multi-mount support
	SocketConfig          *SocketConfig     // Forwarded host unix sockets
	CredentialConfig      *CredentialConfig // Configured [[credentials]] entries (catalog + ad-hoc)
	PortConfig            *PortConfig       // Configured [[ports]] entries to publish on the host (#558)
	SessionsDir           string            // e.g., ~/.coi/sessions-claude
	CLIConfigPath         string            // e.g., ~/.claude (host CLI config to copy credentials from)
	Tool                  tool.Tool         // AI coding tool being used
	NetworkConfig         *config.NetworkConfig
	DisableShift          bool                   // Disable UID shifting (for Colima/Lima environments)
	LimitsConfig          *config.LimitsConfig   // Resource and time limits
	IncusProject          string                 // Incus project name
	ProtectedPaths        []string               // Paths to mount read-only for security (e.g., .git/hooks, .vscode)
	Security              *config.SecurityConfig // Security config, so worktree-config expansion honors disable_protection/writable_paths (nil = expand unconditionally)
	SecretPaths           []string               // Workspace-relative globs to MASK (empty read-only mount hides contents) — issue #494
	PreserveWorkspacePath bool                   // Mount workspace at same path as host instead of /workspace
	ForwardSSHAgent       bool                   // Forward host SSH agent to container
	ForwardedEnvVars      []string               // Names of host env vars being forwarded (for context file)
	GitIdentity           GitIdentity            // Resolved host git identity to configure inside the container
	GitReadonly           bool                   // Identity is provided by a read-only ~/.gitconfig mount; skip the in-container git config writes
	ContextFilePath       string                 // Path to custom context .md file on host (overrides tool default)
	ProfileContextFile    string                 // Path to profile context .md file (appended to sandbox context)
	Timezone              string                 // Resolved IANA timezone name (e.g., "America/New_York"), empty for UTC
	AutoContext           *bool                  // Auto-inject sandbox context into tool's native system (default: true)
	ContextJSON           *bool                  // Write ~/SANDBOX_CONTEXT.json for programmatic consumers (default: true)
	ContextJSONFilePath   string                 // Path to custom context .json file on host (overrides the generated JSON)
	HostImmutable         bool                   // Apply chattr +i on host-side protected paths (set by CLI from config)
	Alias                 string                 // Human-friendly alias for this container (set user.coi.alias)
	ReadyTimeout          int                    // Seconds to wait for the container to become ready (<=0 = default 30)
	Logger                func(string)
	ContainerName         string // Use existing container (for testing) - skips container creation
}

// SetupResult contains the result of setup
type SetupResult struct {
	ContainerName          string
	Manager                container.ContainerManager
	NetworkManager         network.NetworkManager
	TimeoutMonitor         *limits.TimeoutMonitor
	Logger                 *logger.SessionLogger
	HomeDir                string
	RunAsRoot              bool
	Image                  string
	ContainerWorkspacePath string            // Path where workspace is mounted inside container (default: /workspace)
	SSHAgentSocketPath     string            // SSH agent socket path in container (empty if not forwarded)
	SocketEnv              map[string]string // env var name -> in-container path for all forwarded sockets
	PortsEnv               map[string]string // env var name -> host port for all published [[ports]]
	PublishedPorts         []PublishedPort   // ports published on the host this session (for the context file)
	Timezone               string            // Resolved timezone applied to the container (empty = UTC)
	HasImmutableProtection bool              // True if host-side immutable attribute was applied to protected paths
}

// Setup initializes a container for a Claude session
// This configures the container with workspace mounting and user setup
//
//nolint:gocyclo // Sequential initialization with many configuration paths
func Setup(ctx context.Context, opts SetupOptions) (*SetupResult, error) {
	result := &SetupResult{}

	// Default logger
	if opts.Logger == nil {
		opts.Logger = func(msg string) {
			fmt.Fprintf(os.Stderr, "[setup] %s\n", msg)
		}
	}

	// 1. Generate or use existing container name
	var containerName string
	if opts.ContainerName != "" {
		// Use existing container (for testing)
		containerName = opts.ContainerName
		opts.Logger(fmt.Sprintf("Using existing container: %s", containerName))
	} else {
		// Generate new container name
		containerName = ContainerName(opts.WorkspacePath, opts.SessionName, opts.Slot)
		opts.Logger(fmt.Sprintf("Container name: %s", containerName))
	}
	result.ContainerName = containerName
	result.Manager = container.NewManager(containerName)

	hostHome, _ := os.UserHomeDir() // empty string on failure; logger.New handles it
	result.Logger = logger.New(containerName, hostHome)
	if w := result.Logger.InitWarning(); w != "" {
		opts.Logger(fmt.Sprintf("Warning: %s", w))
	}
	// Route the network package's diagnostics (incl. the background IP-refresh
	// goroutine) to the session log files instead of stderr, which in a coi
	// shell is the user's tmux terminal (issue #372).
	network.SetLogger(result.Logger)

	// 1.5 Validate Bedrock setup if running in Colima/Lima
	if vmhost.Detect().HandlesUIDMapping() && opts.CLIConfigPath != "" {
		settingsPath := filepath.Join(opts.CLIConfigPath, "settings.json")
		isConfigured, err := bedrock.IsBedrockConfigured(settingsPath)
		if err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to check Bedrock configuration: %v", err))
		} else if isConfigured {
			opts.Logger("Detected AWS Bedrock configuration, validating setup...")

			// Validate Bedrock setup
			validationResult := bedrock.ValidateColimaBedrockSetup()

			// Check if .aws is mounted
			if opts.MountConfig != nil {
				var mountPaths []string
				for _, mount := range opts.MountConfig.Mounts {
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
						opts.Logger(fmt.Sprintf("⚠️  %s", issue.Message))
					}
				}
			}
		}
	}

	// Autofix: make sure the Incus bridge has iptables FORWARD ACCEPT rules
	// before any container is started. Without them, containers cannot get IPs
	// via DHCP when the FORWARD chain policy is DROP (e.g. when Docker is running).
	if changed, bridgeName, err := network.EnsureBridgeInTrustedZone(); err != nil {
		opts.Logger(fmt.Sprintf("Warning: could not ensure bridge forwarding rules: %v", err))
	} else if changed {
		opts.Logger(fmt.Sprintf("Added iptables FORWARD rules for %s (was missing — containers could not get IPs)", bridgeName))
	}

	// 2. Determine image
	image := opts.Image
	if image == "" {
		image = CoiImage
	}
	result.Image = image

	// Check if image exists
	exists, err := container.ImageExists(image)
	if err != nil {
		return nil, fmt.Errorf("failed to check image: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("image '%s' not found - run 'coi build' first", image)
	}

	// 3. Provisional execution context — finalized at step 6.4 once the
	// container is up and we can probe it for the `code` user.
	//
	// Historically this was a literal string match against the coi-default
	// alias. That breaks custom images built FROM coi-default (they have
	// the `code` user inherited from the base layer but a different alias,
	// so the match returned false and the session was forced to root). We
	// now defer the final decision until after the container boots and
	// probe it directly.
	result.HomeDir = "/home/" + container.CodeUser

	// 4. Check if container already exists
	var skipLaunch bool

	exists, err = result.Manager.Exists()
	if err != nil {
		return nil, fmt.Errorf("failed to check if container exists: %w", err)
	}

	// An explicitly named container (--container) is attached to, never
	// created — so it must already exist. Without this guard a missing name
	// silently skips both the reuse branch (exists is false) and the
	// creation branch (skipLaunch is true), then fails with a misleading
	// "container not ready" only after the full ready_timeout wait.
	if opts.ContainerName != "" {
		if !exists {
			return nil, fmt.Errorf("container '%s' not found - omit --container to launch a new container for this workspace", opts.ContainerName)
		}
		skipLaunch = true
		opts.Logger("Using existing container, skipping creation...")
	}

	if exists {
		// Check if container is currently running
		running, err := result.Manager.Running()
		if err != nil {
			return nil, fmt.Errorf("failed to check if container is running: %w", err)
		}

		if running {
			// Container is running - this is an active session!
			if opts.Persistent || opts.ContainerName != "" || opts.ResumeFromID != "" {
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
				if opts.ContainerName == "" {
					if src := result.Manager.GetWorkspaceSource(); src == "" {
						opts.Logger("Warning: could not determine the running container's workspace source; skipping the workspace-match check")
					} else if !SameWorkspaceSource(src, opts.WorkspacePath) {
						return nil, fmt.Errorf(
							"container %s is running with a different workspace mounted (%s); "+
								"stop that session first (coi shutdown %s) or launch from that workspace",
							containerName, src, containerName)
					}
				}
				opts.Logger("Container already running, reusing...")
				// Strip the previous session's port devices NOW, before the
				// port preflight below bind-probes: their live forkproxy
				// listeners would otherwise make the session collide with its
				// own ports (pinned entries hard-fail, pool numbers drift).
				RemoveStalePortDevices(result.Manager, opts.Logger)
				// Deliberately do NOT reconcile protect-* devices here. This
				// branch reuses an already-RUNNING container, so (a) there is no
				// Start(), hence no "Missing source path" start-validation to
				// prevent — the #610 wedge only bites the stopped branch below —
				// and (b) hot-removing a protect-* device from a live container
				// would DROP a read-only protection mid-session: a source that
				// went missing while still mounted keeps writes blocked, but
				// RemoveDevice would reopen it to a possibly-malicious agent.
				// Reconciliation happens on the next stopped->start cycle.
				skipLaunch = true
			} else {
				// A running container exists for this slot but we're not resuming or in
				// persistent mode — AllocateSlot() should have avoided this slot.
				return nil, fmt.Errorf("slot %d is already in use by a running container %s - this should not happen (bug in slot allocation)", opts.Slot, containerName)
			}
		} else {
			// Container exists but is stopped
			if opts.Persistent || opts.ContainerName != "" {
				// Restart the stopped container
				// This includes: persistent containers OR containers specified via --container flag
				if err := restartStoppedContainer(result, &opts, containerName); err != nil {
					return nil, err
				}
				skipLaunch = true
			} else {
				// Delete the stopped leftover container
				opts.Logger("Found stopped leftover container from previous session, deleting...")
				if err := result.Manager.Delete(true); err != nil {
					return nil, fmt.Errorf("failed to delete leftover container: %w", err)
				}
				// Brief pause to let Incus fully delete
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// 4.6. Defense-in-depth: gate untrusted out-of-workspace mounts, untrusted
	// forwarded sockets, untrusted ad-hoc credential entries, AND untrusted
	// port publications at the single chokepoint every caller passes through.
	// This deliberately runs on the REUSE paths too: sockets, credentials
	// (resume), and ports are re-applied from the current config every
	// session, so gating only at creation would let an untrusted repo config
	// smuggle them onto a reused container. Mount devices are the exception —
	// they persist from creation and can't be re-gated here, so on reuse we
	// warn instead. Idempotent with the CLI-level gate: on the normal CLI
	// flow these are already filtered, so this drops nothing.
	gatedMC, droppedM, gatedSC, droppedS, gatedCC, droppedC, gatedPC, droppedP := FilterTrusted(opts.MountConfig, opts.SocketConfig, opts.CredentialConfig, opts.PortConfig, opts.WorkspacePath)
	if skipLaunch && len(droppedM) > 0 {
		opts.Logger(fmt.Sprintf(
			"Warning: %d untrusted mount(s) remain attached from when this container was created; recreate it (coi kill + relaunch) to apply mount-trust changes",
			len(droppedM),
		))
	} else {
		for _, m := range droppedM {
			opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted mount from %s: %s -> %s (resolves outside the workspace; run 'coi trust' or set %s=1)",
				m.SourcePath, m.HostPath, m.ContainerPath, TrustEnvVar,
			))
		}
	}
	for _, s := range droppedS {
		opts.Logger(fmt.Sprintf(
			"Warning: ignoring untrusted socket from %s: %s -> %s (run 'coi trust' or set %s=1)",
			s.SourcePath, s.HostPath, s.ContainerPath, TrustEnvVar,
		))
	}
	for _, c := range droppedC {
		opts.Logger(fmt.Sprintf(
			"Warning: ignoring untrusted credential entry from %s: %s -> %s (run 'coi trust' or set %s=1)",
			c.SourcePath, c.HostPath, c.ContainerPath, TrustEnvVar,
		))
	}
	for _, p := range droppedP {
		opts.Logger(fmt.Sprintf(
			"Warning: ignoring untrusted %s from %s (a repo declaring host listeners can squat localhost ports; run 'coi trust' or set %s=1)",
			DescribeDroppedPort(p), p.SourcePath, TrustEnvVar,
		))
	}
	opts.MountConfig = gatedMC
	opts.SocketConfig = gatedSC
	opts.CredentialConfig = gatedCC
	opts.PortConfig = gatedPC

	// 4.7. Preflight the port plan BEFORE any container is created (fresh
	// path) and AFTER stale port devices were stripped (reuse path, step 4):
	// pinned host ports that are already taken abort here with a clear
	// error, and auto/pool ports get their final numbers (busy ones skipped
	// forward within the slot block). See ResolvePorts.
	resolvedPorts, err := ResolvePorts(opts.PortConfig, opts.WorkspacePath, opts.SessionName, opts.Slot)
	if err != nil {
		return nil, fmt.Errorf("port preflight failed: %w", err)
	}

	// 5. Create and configure container (but don't start yet if we need to add devices)
	// Always launch as non-ephemeral so we can save session data even if container is stopped
	// (e.g., via 'sudo shutdown 0' from within). Cleanup will delete unless persistent mode is configured.
	if !skipLaunch {
		if err := createAndStartContainer(result, &opts, image, containerName); err != nil {
			return nil, err
		}
	}

	// Set/update alias metadata on container (for running-container lookup).
	// This runs for both new and reused containers so alias changes are propagated.
	if opts.Alias != "" {
		if err := container.IncusExec("config", "set", result.ContainerName,
			fmt.Sprintf("user.coi.alias=%s", opts.Alias)); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to set alias metadata: %v", err))
		}
	}

	// 6. Wait for ready
	readyTimeout := opts.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = 30
	}
	if err := WaitForReady(ctx, result.Manager, readyTimeout, opts.Logger); err != nil {
		return nil, AnnotateReadyTimeout(err, opts.LimitsConfig)
	}

	// 6.1. Configure Docker bridge CIDR to prevent IP conflicts with the host
	// network or other containers. Only applied to newly launched containers.
	if !skipLaunch {
		if err := ConfigureDockerDaemon(result.Manager, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to configure Docker daemon: %v", err))
		}

		// Size /tmp from [limits.disk] tmpfs_size (prevents space exhaustion in
		// big builds). Applied POST-start: it installs a systemd tmp.mount unit,
		// which the running container's init mounts immediately and again at
		// every subsequent boot (#733). Non-fatal.
		if opts.LimitsConfig != nil && opts.LimitsConfig.Disk.TmpfsSize != "" {
			if err := result.Manager.SetTmpfsSize(opts.LimitsConfig.Disk.TmpfsSize); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to set /tmp size: %v", err))
			} else {
				opts.Logger(fmt.Sprintf("Set /tmp size to %s", opts.LimitsConfig.Disk.TmpfsSize))
			}
		}
	}

	// 6.4. Finalize execution context by probing the container for the
	// `code` user. This replaces the old literal-alias match, which
	// forced custom images built FROM coi-default (a valid and common
	// pattern) to run as root. We now trust the image: if it has a
	// `code` user, we use it; otherwise we fall back to root.
	hasCodeUser, err := DetectCodeUser(result.Manager, container.CodeUser)
	if err != nil {
		opts.Logger(fmt.Sprintf("Warning: could not probe container for %s user: %v — falling back to root", container.CodeUser, err))
		hasCodeUser = false
	}
	result.RunAsRoot = !hasCodeUser
	if result.RunAsRoot {
		result.HomeDir = "/root"
	} else {
		result.HomeDir = "/home/" + container.CodeUser
	}

	// 6.5. Remap container user UID/GID if configured UID differs from image default (1000)
	// The COI image builds the 'code' user with UID/GID 1000. If code_uid is set to a
	// different value, remap the user inside the container so /etc/passwd, home directory
	// ownership, and file permissions all match the configured UID.
	if !skipLaunch && hasCodeUser && container.CodeUID != 1000 {
		opts.Logger(fmt.Sprintf("Remapping user %s from UID 1000 to %d...", container.CodeUser, container.CodeUID))
		remapCmd := fmt.Sprintf(
			"groupmod -g %d %s && usermod -u %d -g %d %s",
			container.CodeUID, container.CodeUser,
			container.CodeUID, container.CodeUID, container.CodeUser,
		)
		if _, err := result.Manager.ExecCommand(remapCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			// usermod -u, even without -m, walks the home directory chowning
			// files it finds owned by the old UID — that walk hits any
			// read-only mount already living under /home/<code> (protected
			// paths, [[mounts]] entries; disk devices attach pre-start per
			// #534) and usermod exits non-zero (E_HOMEDIR) despite the passwd
			// update having already been committed. Don't trust the exit code
			// alone: probe whether the account really did move to the target
			// UID before treating this as fatal. groupmod ran first in the
			// chain and usermod commits uid+gid in the same passwd write, so
			// a confirmed UID implies the full remap landed.
			actualUID, _, probeErr := probeCodeUser(result.Manager, container.CodeUser)
			if probeErr != nil || actualUID != container.CodeUID {
				return nil, fmt.Errorf("failed to remap user %s to UID %d: %w", container.CodeUser, container.CodeUID, err)
			}
			opts.Logger(fmt.Sprintf("Warning: UID/GID remap for %s succeeded but usermod's home-directory ownership walk hit an unwritable path: %v", container.CodeUser, err))
		}
		// The home-ownership sweep is best-effort, separately from the remap
		// itself (mirrors the coi run path, #534): a read-only mount under
		// /home/<code> makes chown -R exit non-zero after fixing everything it
		// could, which must not abort setup. Keeping it OUT of the fatal &&
		// chain also means it still runs when usermod exited non-zero above —
		// fused, the chain would skip it and leave writable home files owned
		// by the old UID (the code user unable to write its own dotfiles).
		chownCmd := fmt.Sprintf("chown -R %s:%s /home/%s", container.CodeUser, container.CodeUser, container.CodeUser)
		if _, err := result.Manager.ExecCommand(chownCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			opts.Logger(fmt.Sprintf("Warning: could not chown all of /home/%s after UID remap (a read-only mount under it is expected to fail): %v", container.CodeUser, err))
		}
	}

	// 6.6. Forward host sockets (proxy devices must be added to a running
	// container). The SSH agent (opts.ForwardSSHAgent) is a built-in entry that
	// reads the live $SSH_AUTH_SOCK; configured [[sockets]] follow (already
	// trust-filtered above).
	result.SocketEnv = ForwardConfiguredSockets(result.Manager, opts.SocketConfig, opts.ForwardSSHAgent, opts.Logger)
	result.SSHAgentSocketPath = result.SocketEnv["SSH_AUTH_SOCK"]

	// 6.6.05. Publish configured [ports] on the host (proxy devices, like
	// sockets but pointing the other way): agent-started services become
	// reachable as localhost:<port>, with the mapping exported to the
	// session env (COI_PORTS / COI_PORT_<NAME>) and the sandbox context
	// file (#558). The plan was trust-gated and resolved at step 4.6/4.7
	// (after stale devices were stripped on reuse), so this is a plain add.
	result.PublishedPorts, result.PortsEnv = PublishResolvedPorts(result.Manager, resolvedPorts, opts.Logger)

	// 6.6.1. Prevent git from guessing commit identity from the container user.
	// Setting user.useConfigOnly=true forces git to refuse commits until
	// user.name and user.email are explicitly configured, which ensures AI
	// tools discover and set the real developer identity.
	//
	// git.readonly: instead of writing ~/.gitconfig in-container (which the agent
	// could overwrite), mount the identity read-only at result.HomeDir/.gitconfig —
	// using the home resolved just above, so it is correct for both a code-user and
	// a run-as-root container. This is fail-CLOSED: if the user asked to lock the
	// identity and we cannot, the session aborts rather than silently handing back a
	// writable one. With no resolvable identity there is nothing to lock, so fall
	// through to the normal guard (which still refuses commits until one is set).
	if opts.GitReadonly && opts.GitIdentity.Complete() {
		if err := SetupGitIdentityReadonly(result.Manager, result.HomeDir, opts.GitIdentity); err != nil {
			return nil, fmt.Errorf("git.readonly: could not lock the commit identity read-only: %w", err)
		}
		opts.Logger("Git identity locked read-only (git.readonly): " + result.HomeDir + "/.gitconfig cannot be changed in-container")
	} else {
		if opts.GitReadonly {
			opts.Logger("Warning: git.readonly is set but no identity is resolvable — set [git] name/email (or enable seed_host_identity); nothing to lock")
		}
		SetupGitIdentityGuard(result.Manager, result.HomeDir, opts.Logger)
		SetupGitIdentity(result.Manager, result.HomeDir, opts.GitIdentity, opts.Logger)
	}

	// 6.6.2. Suppress Claude Code auto-mode prompt via managed settings.
	// Only applies when the tool is Claude Code — the managed settings path
	// (/etc/claude-code/managed-settings.json) is Claude-specific.
	if opts.Tool != nil && opts.Tool.Name() == "claude" {
		SetupClaudeManagedSettings(result.Manager, opts.Logger)
	}

	// 6.7. Configure timezone inside container
	// Always set result.Timezone so the TZ env var is applied even if the
	// filesystem configuration fails (some programs only check TZ).
	result.Timezone = opts.Timezone
	if opts.Timezone != "" {
		opts.Logger(fmt.Sprintf("Setting container timezone to %s...", opts.Timezone))
		tzCmd := fmt.Sprintf(
			"ln -sf /usr/share/zoneinfo/%s /etc/localtime && echo %s > /etc/timezone",
			opts.Timezone, opts.Timezone,
		)
		if _, err := result.Manager.ExecCommand(tzCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to set timezone: %v", err))
		}
	} else {
		// Explicitly reset to UTC — important for persistent containers that may
		// have had a different timezone applied in a previous session.
		resetCmd := "ln -sf /usr/share/zoneinfo/UTC /etc/localtime && echo UTC > /etc/timezone"
		if _, err := result.Manager.ExecCommand(resetCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to reset timezone to UTC: %v", err))
		}
	}

	// 7. Start timeout monitor if max_duration is configured
	if opts.LimitsConfig != nil && opts.LimitsConfig.Runtime.MaxDuration != "" {
		duration, err := limits.ParseDuration(opts.LimitsConfig.Runtime.MaxDuration)
		if err != nil {
			return nil, fmt.Errorf("invalid max_duration: %w", err)
		}
		if duration > 0 {
			result.TimeoutMonitor = limits.NewTimeoutMonitor(
				ctx,
				result.ContainerName,
				duration,
				config.BoolVal(opts.LimitsConfig.Runtime.AutoStop),
				config.BoolVal(opts.LimitsConfig.Runtime.StopGraceful),
				opts.IncusProject,
				result.Logger,
			)
			result.TimeoutMonitor.Start()
		}
	}

	// 8. Setup network isolation (after container is running and has IP)
	if opts.NetworkConfig != nil {
		result.NetworkManager = network.NewManager(opts.NetworkConfig, result.Logger)
		if err := result.NetworkManager.SetupForContainer(ctx, result.ContainerName); err != nil {
			return nil, fmt.Errorf("failed to setup network isolation: %w", err)
		}
	}

	// 9. When resuming: restore session data if container was recreated, then inject credentials
	// Skip if tool uses ENV-based auth (no config directory)
	if opts.ResumeFromID != "" && opts.Tool != nil && opts.Tool.ConfigDirName() != "" {
		// If we launched a new container (not reusing persistent one), restore config from saved session
		if !skipLaunch && opts.SessionsDir != "" {
			if err := restoreSessionData(result.Manager, opts.ResumeFromID, result.HomeDir, opts.SessionsDir, opts.Tool, opts.Logger); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Could not restore session data: %v", err))
			}
		}

		// Always inject fresh credentials/sandbox settings when resuming
		if tcf, ok := opts.Tool.(tool.ToolWithConfigDirFiles); ok {
			if opts.CLIConfigPath != "" || tcf.AlwaysSetupConfig() {
				if err := injectCredentials(result.Manager, opts.CLIConfigPath, result.HomeDir, tcf, opts.Logger); err != nil {
					opts.Logger(fmt.Sprintf("Warning: Could not inject credentials: %v", err))
				}
			}
		}
	}

	// 9.5 Refresh/copy configured [[credentials]] entries (catalog bundles and
	// ad-hoc). Independent of which Tool is selected. Re-run on every resume
	// (idempotent) so a rotated host credential stays in sync; on a fresh
	// session it's applied once at step 11 alongside CLI tool config.
	if opts.ResumeFromID != "" && opts.CredentialConfig != nil && len(opts.CredentialConfig.Entries) > 0 {
		opts.Logger("Refreshing configured credentials...")
		if err := setupCredentials(result.Manager, result.HomeDir, opts.CredentialConfig.Entries, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Could not refresh credentials: %v", err))
		}
	}

	// 10. Workspace and configured mounts are already mounted (added before container start in step 5)
	if skipLaunch {
		opts.Logger("Reusing existing workspace and mount configurations")
	}

	// 10.2 Auto-trust mise config files in the workspace so mise doesn't
	// prompt or error when the workspace contains mise.toml / .tool-versions.
	SetupMiseTrust(result.Manager, result.ContainerWorkspacePath, opts.Logger)

	// 10.5 Set auto-context path for config-based tools (must happen before setupCLIConfig
	// so the path is included in GetSandboxSettings output)
	if opts.Tool != nil && config.BoolVal(opts.AutoContext) {
		if acp, ok := opts.Tool.(tool.ToolWithAutoContextPath); ok {
			acp.SetAutoContextPath(filepath.Join(result.HomeDir, "SANDBOX_CONTEXT.md"))
		}
	}

	// 11. Setup CLI tool config (skip if resuming - config already restored)
	if opts.Tool != nil {
		if tcf, ok := opts.Tool.(tool.ToolWithConfigDirFiles); ok {
			if opts.CLIConfigPath != "" && opts.ResumeFromID == "" {
				_, statErr := os.Stat(opts.CLIConfigPath)
				hostDirExists := statErr == nil

				if hostDirExists || tcf.AlwaysSetupConfig() {
					if !skipLaunch {
						opts.Logger(fmt.Sprintf("Setting up %s config...", opts.Tool.Name()))
						if err := setupCLIConfig(result.Manager, opts.CLIConfigPath, result.HomeDir, tcf, opts.Logger); err != nil {
							opts.Logger(fmt.Sprintf("Warning: Failed to setup %s config: %v", opts.Tool.Name(), err))
						}
					} else {
						opts.Logger(fmt.Sprintf("Reusing existing %s config (persistent container)", opts.Tool.Name()))
					}
				} else if statErr != nil && !os.IsNotExist(statErr) {
					return nil, fmt.Errorf("failed to check %s config directory: %w", opts.Tool.Name(), statErr)
				}
			} else if opts.ResumeFromID != "" {
				opts.Logger(fmt.Sprintf("Resuming session - using restored %s config", opts.Tool.Name()))
			}
		} else if opts.Tool.ConfigDirName() == "" {
			opts.Logger(fmt.Sprintf("Tool %s uses ENV-based auth, skipping config setup", opts.Tool.Name()))
		}
	}

	// 11.1 Persist the tool's resolved env as container-level environment.* so
	// EVERY exec — coi's own launch and an external `coi container exec` alike —
	// inherits the profile's tool config (#744). Runs even when skipLaunch is
	// true (reused/persistent containers), unlike the settings.json injection
	// above, so a per-workflow model/effort change actually takes effect.
	if opts.Tool != nil {
		applyToolContainerEnv(ctx, result.ContainerName, result.ContainerWorkspacePath, opts.Tool, opts.Logger)
	}

	// 11.5 Setup configured [[credentials]] entries (skip if resuming - the
	// refresh above already handled it; skip on container reuse - persists
	// from creation, matching how step 11 handles the builtin tool config).
	if !skipLaunch && opts.ResumeFromID == "" && opts.CredentialConfig != nil && len(opts.CredentialConfig.Entries) > 0 {
		opts.Logger("Setting up configured credentials...")
		if err := setupCredentials(result.Manager, result.HomeDir, opts.CredentialConfig.Entries, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to setup credentials: %v", err))
		}
	}

	// 12. Inject sandbox context file (~/SANDBOX_CONTEXT.md)
	// This runs for both new and resumed sessions so dynamic info stays current.
	// The file is tool-agnostic — any AI tool can be configured to read it.
	contextContent := injectSandboxContext(result, opts)

	// 13. Inject auto-context file for tools that support it (e.g., Claude's ~/.claude/CLAUDE.md)
	// This writes sandbox context into the tool's native auto-load file so it's available at session start.
	if opts.Tool != nil && config.BoolVal(opts.AutoContext) && contextContent != "" {
		if acf, ok := opts.Tool.(tool.ToolWithAutoContextFile); ok {
			if err := injectAutoContextFile(result.Manager, acf, contextContent, result.HomeDir, opts.Logger); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to inject auto-context file: %v", err))
			}
		}
	}

	opts.Logger("Container setup complete!")
	return result, nil
}

// dockerDaemonJSON is the daemon.json written to new containers.
// It merges two concerns:
//   - "group": "code" — preserves the base-image setting that gives the
//     non-root `code` user access to /var/run/docker.sock (also enforced
//     via a systemd socket drop-in written in build.sh). Omitting this
//     regresses non-root Docker access after a container reboot.
//   - "bip" / "default-address-pools" — avoid Docker bridge IP conflicts.
//     Docker's built-in pool (172.17–172.29) overlaps with many corporate
//     VPNs and cloud subnets. The chosen ranges (172.30.x, 172.31.x) sit
//     at the far end of RFC 1918's 172.16.0.0/12 block where conflicts are
//     rare in practice.
const dockerDaemonJSON = `{
  "group": "code",
  "bip": "172.30.0.1/24",
  "default-address-pools": [
    {"base": "172.31.0.0/16", "size": 24}
  ]
}`

// ConfigureDockerDaemon writes /etc/docker/daemon.json inside the container
// to configure bridge CIDRs that don't overlap with the host network.
func ConfigureDockerDaemon(mgr container.ContainerExecution, logFn func(string)) error {
	cmd := fmt.Sprintf(
		"mkdir -p /etc/docker && printf '%%s' %s > /etc/docker/daemon.json",
		shellEscape(dockerDaemonJSON),
	)
	if _, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("write /etc/docker/daemon.json: %w", err)
	}
	logFn("Configured Docker bridge CIDRs (172.30.0.0/24, 172.31.0.0/16)")
	return nil
}

// shellEscape single-quotes a string for safe interpolation in a shell command.
func shellEscape(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// ErrNotReady is the sentinel wrapped by WaitForReady's timeout error, so
// callers can tell "the window expired" apart from cancellation or probe
// errors (e.g. to attach cause hints — see AnnotateReadyTimeout).
var ErrNotReady = errors.New("container failed to become ready")

// WaitForReady waits for the container to be ready: running AND able to
// execute a command. It probes once per second for up to maxRetries seconds
// and honors ctx cancellation between probes (a SIGINT-cancelled context
// stops the wait immediately instead of sleeping out the window against a
// container the signal handler already tore down). This is the single
// readiness chokepoint for every wait-for-container path (shell, run, health
// probes) — private copies of this loop drift, as coi run's no-sleep variant
// proved.
func WaitForReady(ctx context.Context, mgr container.ContainerManager, maxRetries int, logger func(string)) error {
	defer timing.Start(timing.CatStep, "wait-for-ready")()
	logger("Waiting for container to be ready...")
	for i := 0; i < maxRetries; i++ {
		running, err := mgr.Running()
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}

		if running {
			// Additional check: try to execute a simple command
			_, err := mgr.ExecCommand("echo ready", container.ExecCommandOptions{Capture: true})
			if err == nil {
				return nil
			}
		}

		// No sleep after the final probe — it would delay the error for
		// nothing. The last iteration falls straight through to the timeout.
		if i == maxRetries-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
		if (i+1)%5 == 0 {
			logger(fmt.Sprintf("Still waiting... (%ds)", i+1))
		}
	}

	return fmt.Errorf("%w after %d seconds", ErrNotReady, maxRetries)
}

// AnnotateReadyTimeout appends a cause hint to a WaitForReady timeout when
// disk I/O limits were active during boot: a low configured rate (e.g.
// read = "100kB") throttles the container's root disk while it is still
// booting and can starve startup past the readiness window — a failure mode
// that otherwise presents as a bare, misleading timeout. Errors other than
// the ErrNotReady timeout (cancellation, probe failures) pass through
// unchanged.
func AnnotateReadyTimeout(err error, limitsCfg *config.LimitsConfig) error {
	if err == nil || limitsCfg == nil || !errors.Is(err, ErrNotReady) {
		return err
	}
	var active []string
	if limitsCfg.Disk.Read != "" {
		active = append(active, "read="+limitsCfg.Disk.Read)
	}
	if limitsCfg.Disk.Write != "" {
		active = append(active, "write="+limitsCfg.Disk.Write)
	}
	if limitsCfg.Disk.Max != "" {
		active = append(active, "max="+limitsCfg.Disk.Max)
	}
	if len(active) == 0 {
		return err
	}
	return fmt.Errorf("%w (note: [limits.disk] %s was active while the container booted — a low disk I/O rate can starve startup past the readiness window)",
		err, strings.Join(active, " "))
}

// DetectCodeUser returns true if the named user account exists inside
// the running container. It is used to decide whether to run sessions
// as `code` or fall back to root — replacing the old broken heuristic
// that matched the image alias literally against "coi-default" and
// misclassified every custom image built from it as a root image.
//
// Implemented on probeCodeUser (shared with ResolveCodeUID): "user not
// present" is recognized by `id`'s own stderr, while incus-level exec
// failures return an error so the caller can decide whether to warn or
// fall back. See probeCodeUser for the argv-injection defence notes.
func DetectCodeUser(mgr container.ContainerExecution, codeUser string) (bool, error) {
	_, exists, err := probeCodeUser(mgr, codeUser)
	return exists, err
}

// codeUserMissing reports whether a probe error means `id` itself ran and
// said the account doesn't exist — as opposed to an incus-level failure
// (daemon unreachable, container stopped mid-race, permission denied,
// missing binary) that ALSO surfaces as *container.ExitError from the CLI
// exec path, with the same non-zero exit code. The stderr text is the only
// reliable discriminator: `id` (GNU coreutils and busybox alike) says
// "no such user"/"unknown user", incus's own failures say "Error: ...".
// Only a genuine no-such-user may fall back to root — misclassifying an
// infra failure would silently misdirect callers to root's tmux socket,
// the exact #588 failure mode.
func codeUserMissing(err error) bool {
	var exitErr *container.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	stderr := strings.ToLower(exitErr.Stderr)
	return strings.Contains(stderr, "no such user") || strings.Contains(stderr, "unknown user")
}

// probeCodeUser is the ONE code-user probe shared by DetectCodeUser and
// ResolveCodeUID (so their error taxonomies cannot drift): it runs
// `id -u <codeUser>` in the container and returns (uid, true, nil) when the
// account exists, (0, false, nil) when `id` reports it missing, and an error
// for anything else — including incus-level exec failures, which are
// distinguished from "no such user" by stderr (see codeUserMissing).
//
// codeUser is passed as a raw argv entry to `id` rather than interpolated
// into a shell string — defence-in-depth against a maliciously crafted
// [incus] code_user config value: `id` receives it as a single argument and
// reports "no such user"; the shell never sees it.
func probeCodeUser(mgr container.ContainerExecution, codeUser string) (int, bool, error) {
	out, err := mgr.ExecArgsCapture(
		[]string{"id", "-u", codeUser},
		container.ExecCommandOptions{Capture: true},
	)
	if err != nil {
		if codeUserMissing(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	uid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, false, fmt.Errorf("unexpected `id -u %s` output %q: %w", codeUser, out, err)
	}
	return uid, true, nil
}

// ResolveCodeUID returns the UID the container's code user ACTUALLY has,
// probed from the container itself, or root (0) when the account doesn't
// exist — images without a code user run their sessions, and therefore
// their tmux server, as root. Callers that must talk to a session's
// per-user resources (e.g. the tmux socket at /tmp/tmux-<uid>, #588) need
// this rather than the config-derived container.CodeUID: after an
// in-container remap (remapContainerUserIfNeeded / [incus] code_uid) the
// two can differ. Note the probe uses the CURRENT config's code_user NAME,
// so it resolves cross-config UID variance but not a container created
// under a different [incus] code_user name (custom-image corner case —
// such containers probe as "no user" and resolve to root).
func ResolveCodeUID(mgr container.ContainerExecution, codeUser string) (int, error) {
	uid, exists, err := probeCodeUser(mgr, codeUser)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil // no code user: sessions run as root
	}
	return uid, nil
}

// restartStoppedContainer reconciles and restarts a stopped, reusable container
// (persistent or explicit --container). It re-runs the same workspace-mount and
// security-device setup a fresh launch uses (issue #610) and applies the boot
// network block; it mutates result (workspace path, immutable flag) and
// opts.ProtectedPaths in place. Extracted verbatim from Setup.
func restartStoppedContainer(result *SetupResult, opts *SetupOptions, containerName string) error {
	opts.Logger("Starting existing container...")
	// Strip stale port devices while STOPPED: they would re-bind
	// their old host ports at start (colliding with the preflight
	// below, or failing the start outright if another process took
	// a port meanwhile). The current plan is re-published later.
	RemoveStalePortDevices(result.Manager, opts.Logger)
	// Reconcile the workspace-sourced security devices against the
	// CURRENT workspace BEFORE start (issue #610). A protect-*/mask-*/
	// gitc-* device attached at first launch keeps its original host
	// source; if that source was removed while the container was stopped,
	// Incus rejects the container at start-validation with "Missing
	// source path" and no fresh coi invocation self-heals it. Strip those
	// devices and re-run the SAME security setup a fresh launch uses, so
	// materialization / symlink-rejection / type handling all come from
	// one place and protection matches the current workspace.
	reuseCWP := result.Manager.GetWorkspacePath()
	result.ContainerWorkspacePath = reuseCWP
	reuseLayout, reuseWtErr := ResolveGitWorktree(opts.WorkspacePath)
	if reuseWtErr != nil {
		// The layout also feeds the shift decision below; losing it
		// silently would drop the common dir's vote (#683).
		opts.Logger(fmt.Sprintf("Warning: git worktree not resolved (%v); its git dirs are skipped by the UID-mapping check and git commands may fail in the container", reuseWtErr))
	}
	reuseWritableHooks := !containsGitHooksPath(opts.ProtectedPaths)
	StripSecurityDevices(result.Manager, opts.Logger)
	// Decide the shift flag the same way a fresh launch does (issue
	// #685). The old `!opts.DisableShift` ignored both cases that turn
	// shift off at first launch — a host/code UID mismatch and a
	// Colima/Lima guest that maps UIDs itself — so every reuse re-added
	// the protect-* devices with shift=true. On a container the #678
	// fallback had already converted to raw.idmap that re-armed the
	// exact configuration whose start failure caused the conversion,
	// once per session. ResolveReuseUIDMapping additionally converts
	// creation-time shift=true devices when the decision is raw.idmap
	// (#683 — a pre-upgrade container on OrbStack ≥2.2.2 never hits
	// the start failure the reactive fallback keys on).
	//
	// The decision deliberately sees the UNGATED mount config: on
	// reuse, mount devices persist from creation regardless of
	// current trust (the 4.6 gate below only warns), so the
	// currently-declared mounts are the closest available stand-in
	// for the devices actually attached. Trust-gating the vote
	// would be both unsafe and pointless here: a mount whose trust
	// was revoked after creation is still attached and its
	// filesystem still matters, while the only influence any path
	// has on the vote is flipping toward raw.idmap — whose value
	// derives from host/code UIDs, never from the path — so an
	// untrusted entry cannot inject anything.
	reuseSources := MountSources(opts.WorkspacePath, opts.MountConfig, WorktreeSources(reuseLayout)...)
	reuseUseShift := ResolveReuseUIDMapping(containerName, reuseSources, opts.DisableShift, opts.Logger)
	// A named session (session_name) can be reused from a different
	// workspace location than the container was created with — the
	// persisted workspace device then points at the old source and
	// must be replaced before the security mounts derive their
	// overlays from the container-side workspace path.
	// An EXPLICIT --container is exempt: it means "enter that
	// container as it is" — rebinding its workspace to whatever
	// directory the caller happens to be in would both break the
	// testing flow and silently mount an unintended directory
	// (e.g. $HOME) read-write into the container.
	if opts.ContainerName == "" {
		if cwp, moved, remountErr := RemountMovedWorkspace(result.Manager, opts.WorkspacePath, opts.PreserveWorkspacePath, reuseLayout, reuseUseShift, opts.Logger); remountErr != nil {
			return remountErr
		} else if moved {
			reuseCWP = cwp
			result.ContainerWorkspacePath = cwp
		}
	}
	reusePaths, reuseImmutable, reuseErr := applySessionSecurity(result.Manager, *opts, reuseCWP, reuseUseShift, reuseLayout, reuseWritableHooks, containerName)
	opts.ProtectedPaths = reusePaths
	if reuseImmutable {
		result.HasImmutableProtection = true
	}
	if reuseErr != nil {
		return reuseErr
	}
	// Reuse gets the same start fallbacks as a fresh launch and as
	// run's persistent reuse (internal/cli/run.go): a persistent
	// container may carry security.idmap.isolated, and its disk devices
	// only materialize at start, so an idmap-incompatible mount fails
	// right here with nothing to catch it (#685).
	if err := container.StartWithIsolationFallback(result.ContainerName); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	// Block network immediately: a previous session's AI agent may have
	// planted startup scripts (systemd units, cron jobs, shell hooks) that
	// would otherwise phone home during the boot window before
	// SetupForContainer installs proper isolation rules.
	if opts.NetworkConfig != nil {
		if err := network.ApplyBootBlockRule(result.ContainerName); err != nil {
			// Fail closed in restricted/allowlist mode: rather than let
			// the container run unblocked during the boot window, stop it
			// and abort. Open mode opts into unrestricted egress.
			if opts.NetworkConfig.Mode != config.NetworkModeOpen {
				_ = result.Manager.Stop(true)
				return fmt.Errorf("boot network block failed in %s mode; stopped container to avoid an unprotected boot window: %w", opts.NetworkConfig.Mode, err)
			}
			opts.Logger(fmt.Sprintf("Warning: boot block not applied (open mode): %v", err))
		} else {
			opts.Logger("Boot network block applied (lifted after isolation rules are set up)")
		}
	}
	return nil
}

// createAndStartContainer creates a fresh container (init), mounts the
// workspace + configured/worktree/security devices, applies limits and the
// pre-boot hardening, starts it, and applies the boot network block. It mutates
// result and opts.ProtectedPaths in place. Extracted verbatim from Setup.
func createAndStartContainer(result *SetupResult, opts *SetupOptions, image, containerName string) error {
	opts.Logger(fmt.Sprintf("Creating container from %s...", image))
	// Create container without starting it (init). Honor the configured
	// storage pool ([container] storage_pool) the same way the run pipeline
	// does via `-s <pool>` — otherwise `coi shell` silently lands on the
	// Incus default pool (#726). An empty pool means "use the default".
	initArgs := []string{"init", image, result.ContainerName}
	if opts.StoragePool != "" {
		initArgs = append(initArgs, "-s", opts.StoragePool)
	}
	if err := container.IncusExec(initArgs...); err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Detect a git worktree checkout (.git is a file whose real git internals
	// live outside the workspace) BEFORE the UID-mapping decision: the common
	// dir is mounted as its own shift-carrying disk device, so its filesystem
	// must vote on that decision too (#683). A valid layout forces
	// preserve-path so git's pointers resolve identically host<->container,
	// and its external git dirs are mounted + protected below (issue #533). A
	// pointer that fails the safety guard is not mounted; git fails loudly
	// rather than exposing a mis-pointed dir.
	worktreeLayout, wtErr := ResolveGitWorktree(opts.WorkspacePath)
	if wtErr != nil {
		opts.Logger(fmt.Sprintf("Warning: git worktree not mounted (%v); git commands may fail in the container", wtErr))
	}
	worktreeWritableHooks := !containsGitHooksPath(opts.ProtectedPaths)

	// Configure UID/GID mapping for the workspace bind mount. Shared with
	// the run pipeline via ConfigureUIDMapping so both honor Colima/Lima
	// auto-detection AND set raw.idmap on any host-UID/code-UID mismatch
	// (issue #530).
	// Shell path sets raw.idmap before its own start (below), so the
	// idmapApplied signal is not needed here.
	useShift, _ := ConfigureUIDMapping(result.ContainerName, MountSources(opts.WorkspacePath, opts.MountConfig, WorktreeSources(worktreeLayout)...), opts.DisableShift, opts.Logger)

	// Determine container mount path - either /workspace (default) or same as host path
	preserveWorkspace := opts.PreserveWorkspacePath || worktreeLayout != nil
	containerWorkspacePath := "/workspace"
	if preserveWorkspace {
		// Validate that the path doesn't conflict with critical system directories.
		if WorkspaceUnderSystemDir(opts.WorkspacePath) {
			if worktreeLayout != nil {
				// Can't preserve the path, so the worktree's git pointers can't
				// resolve and its internals can't be protected — fail closed.
				return fmt.Errorf("git worktree workspace %q is under a system directory; cannot preserve its host path to mount git internals safely", opts.WorkspacePath)
			}
			opts.Logger(fmt.Sprintf("Warning: preserve_workspace_path requested for %q conflicts with system directories; using /workspace instead", opts.WorkspacePath))
		} else {
			containerWorkspacePath = filepath.Clean(opts.WorkspacePath)
			opts.Logger(fmt.Sprintf("Adding workspace mount: %s -> %s (preserving host path)", opts.WorkspacePath, containerWorkspacePath))
		}
	}
	if containerWorkspacePath == "/workspace" && !preserveWorkspace {
		opts.Logger(fmt.Sprintf("Adding workspace mount: %s -> %s", opts.WorkspacePath, containerWorkspacePath))
	}
	result.ContainerWorkspacePath = containerWorkspacePath
	if err := result.Manager.MountDisk("workspace", opts.WorkspacePath, containerWorkspacePath, useShift, false); err != nil {
		return fmt.Errorf("failed to add workspace device: %w", err)
	}
	// Mount the worktree's external git common dir (read-write, at its host path)
	// so git resolves; its RCE sinks are re-covered read-only after the security
	// mounts below (issue #533).
	if worktreeLayout != nil {
		if err := MountGitWorktreeDirs(result.Manager, worktreeLayout, useShift); err != nil {
			return fmt.Errorf("failed to mount git worktree dirs: %w", err)
		}
		opts.Logger(fmt.Sprintf("Mounted git worktree common dir (read-write): %s", worktreeLayout.CommonDir))
	}

	// Mount all configured directories
	if err := setupMounts(result.Manager, opts.MountConfig, useShift, opts.Logger); err != nil {
		return err
	}

	// Protect security-sensitive paths (read-only mounts), extend protection to a
	// worktree's external git common dir, apply the host-immutable belt, and mask
	// secret paths — all via applySessionSecurity, the single implementation shared
	// with the reuse/restart reconcile below (issue #610). Must be added after the
	// workspace mount for the overlays to layer on top.
	effectivePaths, hasImmutable, secErr := applySessionSecurity(result.Manager, *opts, containerWorkspacePath, useShift, worktreeLayout, worktreeWritableHooks, containerName)
	// Adopt the expanded list as the canonical protected set so downstream
	// consumers (the SANDBOX_CONTEXT.md "Protected paths" listing built from
	// opts.ProtectedPaths below) reflect what was actually mounted, including the
	// per-worktree configs.
	opts.ProtectedPaths = effectivePaths
	if hasImmutable {
		result.HasImmutableProtection = true
	}
	if secErr != nil {
		return secErr
	}

	// Apply resource limits before starting (if configured)
	if opts.LimitsConfig != nil && hasLimits(opts.LimitsConfig) {
		opts.Logger("Applying resource limits...")
		applyOpts := limits.ApplyOptions{
			ContainerName: result.ContainerName,
			CPU: limits.CPULimits{
				Count:     opts.LimitsConfig.CPU.Count,
				Allowance: opts.LimitsConfig.CPU.Allowance,
				Priority:  opts.LimitsConfig.CPU.Priority,
			},
			Memory: limits.MemoryLimits{
				Limit:   opts.LimitsConfig.Memory.Limit,
				Enforce: opts.LimitsConfig.Memory.Enforce,
				Swap:    opts.LimitsConfig.Memory.Swap,
			},
			Disk: limits.DiskLimits{
				Read:     opts.LimitsConfig.Disk.Read,
				Write:    opts.LimitsConfig.Disk.Write,
				Max:      opts.LimitsConfig.Disk.Max,
				Priority: opts.LimitsConfig.Disk.Priority,
			},
			Runtime: limits.RuntimeLimits{
				MaxProcesses: opts.LimitsConfig.Runtime.MaxProcesses,
			},
			Project: opts.IncusProject,
		}
		if err := limits.ApplyResourceLimits(applyOpts); err != nil {
			return fmt.Errorf("failed to apply resource limits: %w", err)
		}
	}

	// Enable Docker/nested container support (must be set before first boot)
	opts.Logger("Enabling Docker support...")
	if err := container.EnableDockerSupport(result.ContainerName); err != nil {
		return fmt.Errorf("failed to enable Docker support: %w", err)
	}

	// Isolate UID/GID namespace so each container gets a unique host-side UID
	// range, preventing cross-container file access via shared host UIDs.
	// Non-fatal: some environments (nested containers, CI runners) don't have
	// enough subuid/subgid space. The fallback at start time handles this.
	idmapIsolated := false
	if err := container.IsolateUIDNamespace(result.ContainerName); err != nil {
		opts.Logger(fmt.Sprintf("Warning: UID namespace isolation unavailable: %v", err))
	} else {
		idmapIsolated = true
	}

	// Disable guest API to prevent host topology leaks (FLAWS Finding 3)
	if err := container.DisableGuestAPI(result.ContainerName); err != nil {
		return fmt.Errorf("failed to disable guest API: %w", err)
	}

	// Harden the bridge NIC against egress-isolation bypass: anti-spoof the
	// source IP/MAC (so saddr-keyed nft rules can't be dodged) and isolate the
	// bridge port (so the container can't reach sibling containers at L2).
	// Non-fatal: unmanaged/static/macvlan NICs degrade to nft-only enforcement.
	if err := container.EnableNICSecurity(result.ContainerName); err != nil {
		opts.Logger(fmt.Sprintf("Warning: NIC security hardening not applied: %v", err))
	}

	// For restricted/allowlist modes, disable IPv6 from the kernel's first
	// instant so there is no IPv6 egress window before the host-side ip6 drop
	// is installed. Open mode opts into unrestricted egress, so skip it.
	if opts.NetworkConfig != nil && opts.NetworkConfig.Mode != config.NetworkModeOpen {
		if err := container.DisableIPv6AtBoot(result.ContainerName); err != nil {
			opts.Logger(fmt.Sprintf("Warning: pre-boot IPv6 disable not applied: %v", err))
		}
		// With IPv6 disabled, keep systemd-networkd from wedging on it (#548).
		if err := container.ConfigureNetworkdIPv4Only(result.ContainerName); err != nil {
			opts.Logger(fmt.Sprintf("Warning: networkd IPv4-only config not applied: %v", err))
		}
	}

	// Block privileged containers — they defeat all isolation
	if err := container.CheckNotPrivileged(result.ContainerName); err != nil {
		return err
	}

	// Now start the container
	opts.Logger("Starting container...")
	if idmapIsolated {
		if err := container.StartWithIsolationFallback(result.ContainerName); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	} else {
		// Isolation was never set, so the isolation fallback has nothing to
		// unset — but the #678 idmapped-mount failure is orthogonal to it and
		// hits this branch just the same (#685).
		if err := container.StartWithIdmapFallback(result.ContainerName); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	}
	// Block network immediately after first boot as well: defence-in-depth
	// against a malicious base image that runs something on init.
	if opts.NetworkConfig != nil {
		if err := network.ApplyBootBlockRule(result.ContainerName); err != nil {
			// Fail closed in restricted/allowlist mode: stop the just-started
			// container and abort rather than leave an unprotected boot window.
			// Open mode opts into unrestricted egress.
			if opts.NetworkConfig.Mode != config.NetworkModeOpen {
				_ = result.Manager.Stop(true)
				return fmt.Errorf("boot network block failed in %s mode; stopped container to avoid an unprotected boot window: %w", opts.NetworkConfig.Mode, err)
			}
			opts.Logger(fmt.Sprintf("Warning: boot block not applied (open mode): %v", err))
		} else {
			opts.Logger("Boot network block applied (lifted after isolation rules are set up)")
		}
	}
	return nil
}

// injectSandboxContext builds the tool.ContextInfo from the resolved session
// facts, injects ~/SANDBOX_CONTEXT.md (and the optional .json companion), and
// returns the rendered context content for the auto-context step. Extracted
// verbatim from Setup's phase-12 block.
func injectSandboxContext(result *SetupResult, opts SetupOptions) string {
	networkMode := ""
	var allowedPorts []int
	var dnsServers, allowedDomains []string
	if opts.NetworkConfig != nil {
		networkMode = string(opts.NetworkConfig.Mode)
		allowedPorts = opts.NetworkConfig.AllowedPorts
		dnsServers = opts.NetworkConfig.DNSServers
		allowedDomains = opts.NetworkConfig.AllowedDomains
	}
	// Check if GH_TOKEN or GITHUB_TOKEN is among forwarded env vars
	ghAuthenticated := false
	for _, name := range opts.ForwardedEnvVars {
		if name == "GH_TOKEN" || name == "GITHUB_TOKEN" {
			ghAuthenticated = true
			break
		}
	}

	toolName := "AI coding tool"
	if opts.Tool != nil {
		toolName = opts.Tool.Name()
	}

	var extraMounts []tool.MountInfo
	if opts.MountConfig != nil {
		for _, m := range opts.MountConfig.Mounts {
			extraMounts = append(extraMounts, tool.MountInfo{ContainerPath: m.ContainerPath})
		}
	}

	var cpuLimit, memoryLimit, maxDuration string
	if opts.LimitsConfig != nil {
		cpuLimit = opts.LimitsConfig.CPU.Count
		memoryLimit = opts.LimitsConfig.Memory.Limit
		maxDuration = opts.LimitsConfig.Runtime.MaxDuration
	}

	// Read profile context file content if configured
	var profileContext string
	if opts.ProfileContextFile != "" {
		data, err := os.ReadFile(opts.ProfileContextFile)
		if err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to read profile context file %s: %v", opts.ProfileContextFile, err))
		} else {
			profileContext = string(data)
			opts.Logger(fmt.Sprintf("Loaded profile context from %s", opts.ProfileContextFile))
		}
	}

	ctxInfo := tool.ContextInfo{
		WorkspacePath:      result.ContainerWorkspacePath,
		HomeDir:            result.HomeDir,
		Persistent:         opts.Persistent,
		NetworkMode:        networkMode,
		AllowedPorts:       allowedPorts,
		DNSServers:         dnsServers,
		AllowedDomains:     allowedDomains,
		SSHAgentForwarded:  result.SSHAgentSocketPath != "",
		RunAsRoot:          result.RunAsRoot,
		ProtectedPaths:     opts.ProtectedPaths,
		GHCLIAuthenticated: ghAuthenticated,
		ForwardedEnvVars:   opts.ForwardedEnvVars,
		Timezone:           result.Timezone,
		ExtraMounts:        extraMounts,
		PublishedPorts:     publishedPortInfos(result.PublishedPorts),
		CPULimit:           cpuLimit,
		MemoryLimit:        memoryLimit,
		MaxDuration:        maxDuration,
		ToolName:           toolName,
		ContainerName:      result.ContainerName,
		ProfileContext:     profileContext,
	}
	contextContent := resolveContextContent(ctxInfo, opts.ContextFilePath, opts.Logger)
	if err := injectContextFile(result.Manager, ctxInfo, opts.ContextFilePath, result.HomeDir, opts.Logger); err != nil {
		opts.Logger(fmt.Sprintf("Warning: Failed to inject context file: %v", err))
	}
	// Machine-readable companion for programmatic consumers (#705), enabled
	// by default. Written from ctxInfo (the real facts) unless [tool]
	// context_json_file provides a custom JSON to inject verbatim; disable
	// entirely with context_json = false.
	if config.BoolVal(opts.ContextJSON) {
		if err := injectContextJSONFile(result.Manager, ctxInfo, opts.ContextJSONFilePath, result.HomeDir, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to inject context JSON file: %v", err))
		}
	}
	return contextContent
}
