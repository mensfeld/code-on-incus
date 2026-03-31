package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/bedrock"
	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/limits"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

const (
	DefaultImage = "images:ubuntu/22.04"
	CoiImage     = "coi"
)

// SetupOptions contains options for setting up a session
type SetupOptions struct {
	WorkspacePath         string
	Image                 string
	Persistent            bool // Keep container between sessions (don't delete on cleanup)
	ResumeFromID          string
	Slot                  int
	MountConfig           *MountConfig // Multi-mount support
	SessionsDir           string       // e.g., ~/.coi/sessions-claude
	CLIConfigPath         string       // e.g., ~/.claude (host CLI config to copy credentials from)
	Tool                  tool.Tool    // AI coding tool being used
	NetworkConfig         *config.NetworkConfig
	DisableShift          bool                 // Disable UID shifting (for Colima/Lima environments)
	LimitsConfig          *config.LimitsConfig // Resource and time limits
	IncusProject          string               // Incus project name
	ProtectedPaths        []string             // Paths to mount read-only for security (e.g., .git/hooks, .vscode)
	PreserveWorkspacePath bool                 // Mount workspace at same path as host instead of /workspace
	ForwardSSHAgent       bool                 // Forward host SSH agent to container
	ForwardedEnvVars      []string             // Names of host env vars being forwarded (for context file)
	ContextFilePath       string               // Path to custom context .md file on host (overrides tool default)
	Timezone              string               // Resolved IANA timezone name (e.g., "America/New_York"), empty for UTC
	AutoContext           *bool                // Auto-inject sandbox context into tool's native system (default: true)
	Logger                func(string)
	ContainerName         string // Use existing container (for testing) - skips container creation
}

// SetupResult contains the result of setup
type SetupResult struct {
	ContainerName          string
	Manager                *container.Manager
	NetworkManager         *network.Manager
	TimeoutMonitor         *limits.TimeoutMonitor
	HomeDir                string
	RunAsRoot              bool
	Image                  string
	ContainerWorkspacePath string // Path where workspace is mounted inside container (default: /workspace)
	SSHAgentSocketPath     string // Path to SSH agent socket inside container (empty if not forwarded)
	Timezone               string // Resolved timezone applied to the container (empty = UTC)
}

// Setup initializes a container for a Claude session
// This configures the container with workspace mounting and user setup
//
//nolint:gocyclo // Sequential initialization with many configuration paths
func Setup(opts SetupOptions) (*SetupResult, error) {
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

	// 1.5 Validate Bedrock setup if running in Colima/Lima
	if isColimaOrLimaEnvironment() && opts.CLIConfigPath != "" {
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

	// 3. Determine execution context
	// coi image has the claude user pre-configured, so run as that user
	// Other images don't have this setup, so run as root
	usingCoiImage := image == CoiImage
	result.RunAsRoot = !usingCoiImage
	if result.RunAsRoot {
		result.HomeDir = "/root"
	} else {
		result.HomeDir = "/home/" + container.CodeUser
	}

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
			if opts.Persistent || opts.ContainerName != "" {
				// Reuse running container if: persistent mode OR --container flag specified
				opts.Logger("Container already running, reusing...")
				skipLaunch = true
			} else {
				// ERROR: A running container exists for this slot, but we're not in persistent mode
				// This means AllocateSlot() gave us a slot that's already in use!
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
	// (e.g., via 'sudo shutdown 0' from within). Cleanup will delete if not --persistent.
	if !skipLaunch {
		opts.Logger(fmt.Sprintf("Creating container from %s...", image))
		// Create container without starting it (init)
		if err := container.IncusExec("init", image, result.ContainerName); err != nil {
			return nil, fmt.Errorf("failed to create container: %w", err)
		}

		// Configure UID/GID mapping for bind mounts based on environment
		// When host UID matches container code user UID: Use shift=true (simple, works everywhere)
		// When host UID differs: Use raw.idmap to explicitly map host UID → container code UID
		// Colima/Lima: Disable shift (VM already handles UID mapping via virtiofs)

		// Auto-detect Colima/Lima environment if not explicitly configured
		disableShift := opts.DisableShift
		if !disableShift && isColimaOrLimaEnvironment() {
			disableShift = true
			opts.Logger("Auto-detected Colima/Lima environment - disabling UID shifting")
		}

		useShift := !disableShift
		hostUID := os.Getuid()

		if !disableShift && hostUID != container.CodeUID {
			// Host UID differs from container code user UID — shift=true won't work
			// because it only translates root (UID 0), not arbitrary UIDs.
			// Use raw.idmap to explicitly map host UID → container code UID.
			idmapValue := fmt.Sprintf("both %d %d", hostUID, container.CodeUID)
			opts.Logger(fmt.Sprintf("Host UID %d differs from container code UID %d, using raw.idmap: %s", hostUID, container.CodeUID, idmapValue))
			if err := container.IncusExec("config", "set", result.ContainerName, "raw.idmap", idmapValue); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to set raw.idmap: %v", err))
			}
			useShift = false // Don't use shift=true with raw.idmap
		} else if disableShift {
			if !opts.DisableShift {
				// Was auto-detected, not explicitly configured
				opts.Logger("UID shifting disabled (auto-detected Colima/Lima environment)")
			} else {
				opts.Logger("UID shifting disabled (configured via disable_shift option)")
			}
		}

		// Add disk devices BEFORE starting container
		// Determine container mount path - either /workspace (default) or same as host path
		containerWorkspacePath := "/workspace"
		if opts.PreserveWorkspacePath {
			// Validate that the path doesn't conflict with critical system directories
			cleanPath := filepath.Clean(opts.WorkspacePath)
			disallowedPrefixes := []string{
				"/etc", "/bin", "/sbin", "/usr", "/root", "/boot", "/sys", "/proc", "/dev", "/lib", "/lib64",
			}
			isDisallowed := false
			for _, prefix := range disallowedPrefixes {
				if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/") {
					isDisallowed = true
					break
				}
			}
			if isDisallowed {
				opts.Logger(fmt.Sprintf("Warning: preserve_workspace_path requested for %q conflicts with system directories; using /workspace instead", opts.WorkspacePath))
			} else {
				containerWorkspacePath = cleanPath
				opts.Logger(fmt.Sprintf("Adding workspace mount: %s -> %s (preserving host path)", opts.WorkspacePath, containerWorkspacePath))
			}
		}
		if containerWorkspacePath == "/workspace" && !opts.PreserveWorkspacePath {
			opts.Logger(fmt.Sprintf("Adding workspace mount: %s -> %s", opts.WorkspacePath, containerWorkspacePath))
		}
		result.ContainerWorkspacePath = containerWorkspacePath
		if err := result.Manager.MountDisk("workspace", opts.WorkspacePath, containerWorkspacePath, useShift, false); err != nil {
			return nil, fmt.Errorf("failed to add workspace device: %w", err)
		}

		// Configure /tmp tmpfs size (prevent space exhaustion during builds/operations)
		if opts.LimitsConfig != nil && opts.LimitsConfig.Disk.TmpfsSize != "" {
			if err := result.Manager.SetTmpfsSize(opts.LimitsConfig.Disk.TmpfsSize); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to set /tmp size: %v", err))
			} else {
				opts.Logger(fmt.Sprintf("Set /tmp size to %s", opts.LimitsConfig.Disk.TmpfsSize))
			}
		}

		// Mount all configured directories
		if err := setupMounts(result.Manager, opts.MountConfig, useShift, opts.Logger); err != nil {
			return nil, err
		}

		// Protect security-sensitive paths by mounting read-only (security feature)
		// This must be added after the workspace mount for the overlay to work
		if len(opts.ProtectedPaths) > 0 {
			if err := SetupSecurityMounts(result.Manager, opts.WorkspacePath, containerWorkspacePath, opts.ProtectedPaths, useShift); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to setup security mounts: %v", err))
				// Non-fatal: continue even if protection fails
			} else {
				// Log which paths were actually protected
				protectedPaths := GetProtectedPathsForLogging(opts.WorkspacePath, opts.ProtectedPaths)
				if len(protectedPaths) > 0 {
					opts.Logger(fmt.Sprintf("Protected paths (mounted read-only): %s", strings.Join(protectedPaths, ", ")))
				}
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

		// Block privileged containers — they defeat all isolation
		if err := container.CheckNotPrivileged(result.ContainerName); err != nil {
			return nil, err
		}

		// Now start the container
		opts.Logger("Starting container...")
		if err := result.Manager.Start(); err != nil {
			return nil, fmt.Errorf("failed to start container: %w", err)
		}
	}

	// 6. Wait for ready
	opts.Logger("Waiting for container to be ready...")
	if err := waitForReady(result.Manager, 30, opts.Logger); err != nil {
		return nil, err
	}

	// 6.5. Remap container user UID/GID if configured UID differs from image default (1000)
	// The COI image builds the 'code' user with UID/GID 1000. If code_uid is set to a
	// different value, remap the user inside the container so /etc/passwd, home directory
	// ownership, and file permissions all match the configured UID.
	if !skipLaunch && usingCoiImage && container.CodeUID != 1000 {
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

	// 6.6. Setup SSH agent forwarding (proxy device must be added to running container)
	if opts.ForwardSSHAgent {
		socketPath, err := setupSSHAgentForwarding(result.Manager, result.ContainerName, opts.Logger)
		if err != nil {
			opts.Logger(fmt.Sprintf("Warning: SSH agent forwarding failed: %v", err))
		} else if socketPath != "" {
			result.SSHAgentSocketPath = socketPath
		}
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
				result.ContainerName,
				duration,
				config.BoolVal(opts.LimitsConfig.Runtime.AutoStop),
				config.BoolVal(opts.LimitsConfig.Runtime.StopGraceful),
				opts.IncusProject,
				opts.Logger,
			)
			result.TimeoutMonitor.Start()
		}
	}

	// 8. Setup network isolation (after container is running and has IP)
	if opts.NetworkConfig != nil {
		result.NetworkManager = network.NewManager(opts.NetworkConfig)
		if err := result.NetworkManager.SetupForContainer(context.Background(), result.ContainerName); err != nil {
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

// waitForReady waits for container to be ready
func waitForReady(mgr *container.Manager, maxRetries int, logger func(string)) error {
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

		time.Sleep(1 * time.Second)
		if i%5 == 0 && i > 0 {
			logger(fmt.Sprintf("Still waiting... (%ds)", i))
		}
	}

	return fmt.Errorf("container failed to become ready after %d seconds", maxRetries)
}
