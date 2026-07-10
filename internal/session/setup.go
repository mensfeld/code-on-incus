package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

const (
	DefaultImage = "images:ubuntu/22.04"
	CoiImage     = "coi-default"
)

// SetupOptions contains options for setting up a session
type SetupOptions struct {
	WorkspacePath         string
	Image                 string
	Persistent            bool // Keep container between sessions (don't delete on cleanup)
	ResumeFromID          string
	Slot                  int
	MountConfig           *MountConfig  // Multi-mount support
	SocketConfig          *SocketConfig // Forwarded host unix sockets
	SessionsDir           string        // e.g., ~/.coi/sessions-claude
	CLIConfigPath         string        // e.g., ~/.claude (host CLI config to copy credentials from)
	Tool                  tool.Tool     // AI coding tool being used
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
	ContextFilePath       string                 // Path to custom context .md file on host (overrides tool default)
	ProfileContextFile    string                 // Path to profile context .md file (appended to sandbox context)
	Timezone              string                 // Resolved IANA timezone name (e.g., "America/New_York"), empty for UTC
	AutoContext           *bool                  // Auto-inject sandbox context into tool's native system (default: true)
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
		containerName = ContainerName(opts.WorkspacePath, opts.Slot)
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

	// If using existing container, skip launch
	if opts.ContainerName != "" {
		skipLaunch = true
		opts.Logger("Using existing container, skipping creation...")
	}

	exists, err = result.Manager.Exists()
	if err != nil {
		return nil, fmt.Errorf("failed to check if container exists: %w", err)
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
				opts.Logger("Container already running, reusing...")
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
				opts.Logger("Starting existing container...")
				if err := result.Manager.Start(); err != nil {
					return nil, fmt.Errorf("failed to start container: %w", err)
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
							return nil, fmt.Errorf("boot network block failed in %s mode; stopped container to avoid an unprotected boot window: %w", opts.NetworkConfig.Mode, err)
						}
						opts.Logger(fmt.Sprintf("Warning: boot block not applied (open mode): %v", err))
					} else {
						opts.Logger("Boot network block applied (lifted after isolation rules are set up)")
					}
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

	// 5. Create and configure container (but don't start yet if we need to add devices)
	// Always launch as non-ephemeral so we can save session data even if container is stopped
	// (e.g., via 'sudo shutdown 0' from within). Cleanup will delete unless persistent mode is configured.
	if !skipLaunch {
		opts.Logger(fmt.Sprintf("Creating container from %s...", image))
		// Create container without starting it (init)
		if err := container.IncusExec("init", image, result.ContainerName); err != nil {
			return nil, fmt.Errorf("failed to create container: %w", err)
		}

		// Configure UID/GID mapping for the workspace bind mount. Shared with
		// the run pipeline via ConfigureUIDMapping so both honor Colima/Lima
		// auto-detection AND set raw.idmap on any host-UID/code-UID mismatch
		// (issue #530).
		// Shell path sets raw.idmap before its own start (below), so the
		// idmapApplied signal is not needed here.
		useShift, _ := ConfigureUIDMapping(result.ContainerName, opts.DisableShift, opts.Logger)

		// Add disk devices BEFORE starting container.
		// Detect a git worktree checkout (.git is a file whose real git internals
		// live outside the workspace). A valid layout forces preserve-path so git's
		// pointers resolve identically host<->container, and its external git dirs
		// are mounted + protected below (issue #533). A pointer that fails the safety
		// guard is not mounted; git fails loudly rather than exposing a mis-pointed dir.
		worktreeLayout, wtErr := ResolveGitWorktree(opts.WorkspacePath)
		if wtErr != nil {
			opts.Logger(fmt.Sprintf("Warning: git worktree not mounted (%v); git commands may fail in the container", wtErr))
		}
		worktreeWritableHooks := !containsGitHooksPath(opts.ProtectedPaths)

		// Determine container mount path - either /workspace (default) or same as host path
		preserveWorkspace := opts.PreserveWorkspacePath || worktreeLayout != nil
		containerWorkspacePath := "/workspace"
		if preserveWorkspace {
			// Validate that the path doesn't conflict with critical system directories.
			if WorkspaceUnderSystemDir(opts.WorkspacePath) {
				if worktreeLayout != nil {
					// Can't preserve the path, so the worktree's git pointers can't
					// resolve and its internals can't be protected — fail closed.
					return nil, fmt.Errorf("git worktree workspace %q is under a system directory; cannot preserve its host path to mount git internals safely", opts.WorkspacePath)
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
			return nil, fmt.Errorf("failed to add workspace device: %w", err)
		}
		// Mount the worktree's external git common dir (read-write, at its host path)
		// so git resolves; its RCE sinks are re-covered read-only after the security
		// mounts below (issue #533).
		if worktreeLayout != nil {
			if err := MountGitWorktreeDirs(result.Manager, worktreeLayout, useShift); err != nil {
				return nil, fmt.Errorf("failed to mount git worktree dirs: %w", err)
			}
			opts.Logger(fmt.Sprintf("Mounted git worktree common dir (read-write): %s", worktreeLayout.CommonDir))
		}

		// Configure /tmp tmpfs size (prevent space exhaustion during builds/operations)
		if opts.LimitsConfig != nil && opts.LimitsConfig.Disk.TmpfsSize != "" {
			if err := result.Manager.SetTmpfsSize(opts.LimitsConfig.Disk.TmpfsSize); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to set /tmp size: %v", err))
			} else {
				opts.Logger(fmt.Sprintf("Set /tmp size to %s", opts.LimitsConfig.Disk.TmpfsSize))
			}
		}

		// Defense-in-depth: gate untrusted out-of-workspace mounts AND untrusted
		// forwarded sockets here, where a freshly-launched container is set up, so
		// any caller of session.Setup that didn't pre-filter is still covered.
		// (On container reuse this block is skipped along with the rest of setup —
		// resources persist from creation; the run/shell reuse paths warn that
		// trust changes need a recreate.) Idempotent with the CLI-level gate: on
		// the normal CLI flow these are already filtered, so this drops nothing.
		gatedMC, droppedM, gatedSC, droppedS := FilterTrusted(opts.MountConfig, opts.SocketConfig, opts.WorkspacePath)
		for _, m := range droppedM {
			opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted mount from %s: %s -> %s (resolves outside the workspace; run 'coi trust' or set %s=1)",
				m.SourcePath, m.HostPath, m.ContainerPath, TrustEnvVar))
		}
		for _, s := range droppedS {
			opts.Logger(fmt.Sprintf(
				"Warning: ignoring untrusted socket from %s: %s -> %s (run 'coi trust' or set %s=1)",
				s.SourcePath, s.HostPath, s.ContainerPath, TrustEnvVar))
		}
		opts.MountConfig = gatedMC
		opts.SocketConfig = gatedSC

		// Mount all configured directories
		if err := setupMounts(result.Manager, opts.MountConfig, useShift, opts.Logger); err != nil {
			return nil, err
		}

		// Protect security-sensitive paths by mounting read-only (security feature).
		// This must be added after the workspace mount for the overlay to work.
		// SetupSecurityMounts expands the dynamic per-worktree git configs internally
		// (issue #545 — the single chokepoint both session paths share) and returns
		// the effective list used for logging + the immutable pass over the same set.
		securityPaths := opts.ProtectedPaths
		if worktreeLayout != nil {
			// Workspace .git is a file; its .git/* defaults can't be protected here
			// (they'd warn "gitdir indirection"). SetupCommonDirProtection covers the
			// real internals; keep the non-.git protections.
			securityPaths = StripGitProtectedPaths(securityPaths)
		}
		effectivePaths, err := SetupSecurityMounts(result.Manager, opts.WorkspacePath, containerWorkspacePath, securityPaths, useShift, opts.Security)
		// Adopt the expanded list as the canonical protected set so downstream
		// consumers (the SANDBOX_CONTEXT.md "Protected paths" listing built from
		// opts.ProtectedPaths below) reflect what was actually mounted, including the
		// per-worktree configs — matching pre-#545 behavior where the caller expanded
		// in place. Returned even on partial error, so the record stays complete.
		opts.ProtectedPaths = effectivePaths
		if err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to setup security mounts: %v", err))
			// Non-fatal: continue even if protection fails
		} else if len(effectivePaths) > 0 {
			// Log which paths were actually protected
			protectedPaths := GetProtectedPathsForLogging(opts.WorkspacePath, effectivePaths)
			if len(protectedPaths) > 0 {
				opts.Logger(fmt.Sprintf("Protected paths (mounted read-only): %s", strings.Join(protectedPaths, ", ")))
			}
		}

		// Extend the read-only protection to the external git common dir (issue #533):
		// the same #474 hook/config lockdown applied to the worktree's real internals,
		// which the workspace-relative pass above cannot reach. Mount-only (the
		// host-immutable belt is deferred; the read-only mount is the primary control).
		if worktreeLayout != nil {
			cProtected, cErr := SetupCommonDirProtection(result.Manager, worktreeLayout.CommonDir, useShift, worktreeWritableHooks, opts.Security)
			if cErr != nil {
				opts.Logger(fmt.Sprintf("Warning: some git common-dir protections not applied: %v", cErr))
			}
			if len(cProtected) > 0 {
				opts.Logger(fmt.Sprintf("Protected git common-dir paths (read-only): %s", strings.Join(cProtected, ", ")))
			}
		}

		// Apply host-side immutable attribute for defense-in-depth. Runs even on a
		// partial mount error, so it is gated on the effective list, not err.
		if len(effectivePaths) > 0 && opts.HostImmutable {
			immutablePaths := ApplyImmutable(opts.WorkspacePath, effectivePaths, containerName, opts.Logger)
			if len(immutablePaths) > 0 {
				result.HasImmutableProtection = true
				opts.Logger(fmt.Sprintf("Host-side immutable protection applied: %s", strings.Join(immutablePaths, ", ")))
			}
		}

		// Mask secret paths (issue #494): mount an empty read-only file/dir over
		// each resolved match so the contained agent can neither read nor modify
		// repo-local secrets. Independent of protected_paths. FAIL CLOSED — if a
		// configured secret cannot be masked we must not launch with it exposed.
		if len(opts.SecretPaths) > 0 {
			masked, skipped, err := SetupSecretMasks(result.Manager, opts.WorkspacePath, containerWorkspacePath, opts.SecretPaths, useShift)
			for _, s := range skipped {
				opts.Logger(fmt.Sprintf("Warning: secret_paths entry %q is NOT masked (missing, or a symlink resolving outside the workspace) — it is not hidden from the agent", s))
			}
			if err != nil {
				return nil, fmt.Errorf("failed to mask secret paths: %w", err)
			}
			if len(masked) > 0 {
				opts.Logger(fmt.Sprintf("Masked secret paths (hidden read-only): %s", strings.Join(masked, ", ")))
			}
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
				return nil, fmt.Errorf("failed to apply resource limits: %w", err)
			}
		}

		// Enable Docker/nested container support (must be set before first boot)
		opts.Logger("Enabling Docker support...")
		if err := container.EnableDockerSupport(result.ContainerName); err != nil {
			return nil, fmt.Errorf("failed to enable Docker support: %w", err)
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
			return nil, fmt.Errorf("failed to disable guest API: %w", err)
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
			return nil, err
		}

		// Now start the container
		opts.Logger("Starting container...")
		if idmapIsolated {
			if err := container.StartWithIsolationFallback(result.ContainerName); err != nil {
				return nil, fmt.Errorf("failed to start container: %w", err)
			}
		} else {
			if err := result.Manager.Start(); err != nil {
				return nil, fmt.Errorf("failed to start container: %w", err)
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
					return nil, fmt.Errorf("boot network block failed in %s mode; stopped container to avoid an unprotected boot window: %w", opts.NetworkConfig.Mode, err)
				}
				opts.Logger(fmt.Sprintf("Warning: boot block not applied (open mode): %v", err))
			} else {
				opts.Logger("Boot network block applied (lifted after isolation rules are set up)")
			}
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
			"groupmod -g %d %s && usermod -u %d -g %d %s && chown -R %s:%s /home/%s",
			container.CodeUID, container.CodeUser,
			container.CodeUID, container.CodeUID, container.CodeUser,
			container.CodeUser, container.CodeUser, container.CodeUser,
		)
		if _, err := result.Manager.ExecCommand(remapCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			return nil, fmt.Errorf("failed to remap user %s to UID %d: %w", container.CodeUser, container.CodeUID, err)
		}
	}

	// 6.6. Forward host sockets (proxy devices must be added to a running
	// container). The SSH agent (opts.ForwardSSHAgent) is a built-in entry that
	// reads the live $SSH_AUTH_SOCK; configured [[sockets]] follow (already
	// trust-filtered above).
	result.SocketEnv = ForwardConfiguredSockets(result.Manager, opts.SocketConfig, opts.ForwardSSHAgent, opts.Logger)
	result.SSHAgentSocketPath = result.SocketEnv["SSH_AUTH_SOCK"]

	// 6.6.1. Prevent git from guessing commit identity from the container user.
	// Setting user.useConfigOnly=true forces git to refuse commits until
	// user.name and user.email are explicitly configured, which ensures AI
	// tools discover and set the real developer identity.
	SetupGitIdentityGuard(result.Manager, result.HomeDir, opts.Logger)
	SetupGitIdentity(result.Manager, result.HomeDir, opts.GitIdentity, opts.Logger)

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

	// 12. Inject sandbox context file (~/SANDBOX_CONTEXT.md)
	// This runs for both new and resumed sessions so dynamic info stays current.
	// The file is tool-agnostic — any AI tool can be configured to read it.
	var contextContent string
	{
		networkMode := ""
		if opts.NetworkConfig != nil {
			networkMode = string(opts.NetworkConfig.Mode)
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
			SSHAgentForwarded:  result.SSHAgentSocketPath != "",
			RunAsRoot:          result.RunAsRoot,
			ProtectedPaths:     opts.ProtectedPaths,
			GHCLIAuthenticated: ghAuthenticated,
			ForwardedEnvVars:   opts.ForwardedEnvVars,
			Timezone:           result.Timezone,
			ExtraMounts:        extraMounts,
			CPULimit:           cpuLimit,
			MemoryLimit:        memoryLimit,
			MaxDuration:        maxDuration,
			ToolName:           toolName,
			ContainerName:      result.ContainerName,
			ProfileContext:     profileContext,
		}
		contextContent = resolveContextContent(ctxInfo, opts.ContextFilePath, opts.Logger)
		if err := injectContextFile(result.Manager, ctxInfo, opts.ContextFilePath, result.HomeDir, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to inject context file: %v", err))
		}
	}

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
// The probe runs `id -u <user>` via ExecArgsCapture, which passes
// codeUser as a raw argv entry to `id` rather than interpolating it
// into a shell string. That is defence-in-depth against a maliciously
// crafted [incus] code_user value in config.toml: even if a user set
// `code_user = "code; rm -rf /"`, `id` would just receive that as a
// single argv and report "no such user" — the shell never sees it.
//
// A *container.ExitError (non-zero exit of `id`) is treated as "user
// not present" and returns (false, nil). Any other error (e.g. incus
// connectivity failure) is surfaced to the caller so it can decide
// whether to warn or fall back.
func DetectCodeUser(mgr container.ContainerExecution, codeUser string) (bool, error) {
	_, err := mgr.ExecArgsCapture(
		[]string{"id", "-u", codeUser},
		container.ExecCommandOptions{Capture: true},
	)
	if err == nil {
		return true, nil
	}
	var exitErr *container.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}
