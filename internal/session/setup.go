package session

import (
	"context"
	"encoding/json"
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

// isColimaOrLimaEnvironment detects if we're running inside a Colima or Lima VM
// These VMs use virtiofs for mounting host directories and already handle UID mapping
// at the VM level, making Incus's shift=true unnecessary and problematic
func isColimaOrLimaEnvironment() bool {
	// Check for virtiofs mounts which are characteristic of Lima/Colima
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	// Lima mounts host directories via virtiofs (e.g., "mount0 on /Users/... type virtiofs")
	// Colima uses Lima under the hood, so same detection applies
	mounts := string(data)
	if strings.Contains(mounts, "virtiofs") {
		return true
	}

	// Additional check: Lima typically runs as the "lima" user
	if user := os.Getenv("USER"); user == "lima" {
		return true
	}

	return false
}

// buildJSONFromSettings converts a settings map to a properly escaped JSON string
// Uses json.Marshal to ensure proper escaping and avoid command injection
func buildJSONFromSettings(settings map[string]interface{}) (string, error) {
	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to marshal settings: %w", err)
	}
	return string(jsonBytes), nil
}

// setupMounts mounts all configured directories to the container
func setupMounts(mgr *container.Manager, mountConfig *MountConfig, useShift bool, logger func(string)) error {
	if mountConfig == nil || len(mountConfig.Mounts) == 0 {
		return nil
	}

	for _, mount := range mountConfig.Mounts {
		// Create host directory if it doesn't exist
		if err := os.MkdirAll(mount.HostPath, 0o755); err != nil {
			return fmt.Errorf("failed to create mount directory '%s': %w", mount.HostPath, err)
		}

		logger(fmt.Sprintf("Adding mount: %s -> %s", mount.HostPath, mount.ContainerPath))

		// Apply shift setting (all mounts use same shift for now)
		if err := mgr.MountDisk(mount.DeviceName, mount.HostPath, mount.ContainerPath, useShift, false); err != nil {
			return fmt.Errorf("failed to add mount '%s': %w", mount.DeviceName, err)
		}
	}

	return nil
}

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
		if err := injectContextFile(result.Manager, ctxInfo, opts.ContextFilePath, result.HomeDir, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to inject context file: %v", err))
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

// restoreSessionData restores tool config directory from a saved session
// Used when resuming a non-persistent session (container was deleted and recreated)
func restoreSessionData(mgr *container.Manager, resumeID, homeDir, sessionsDir string, t tool.Tool, logger func(string)) error {
	configDirName := t.ConfigDirName()
	sourceConfigDir := filepath.Join(sessionsDir, resumeID, configDirName)

	// Check if directory exists
	if info, err := os.Stat(sourceConfigDir); err != nil || !info.IsDir() {
		return fmt.Errorf("no saved session data found for %s", resumeID)
	}

	logger(fmt.Sprintf("Restoring session data from %s", resumeID))

	// Push config directory to container
	// PushDirectory extracts the parent from the path and pushes to create the directory there
	// So we pass the full destination path where the config dir should end up
	destConfigPath := filepath.Join(homeDir, configDirName)
	if err := mgr.PushDirectory(sourceConfigDir, destConfigPath); err != nil {
		return fmt.Errorf("failed to push %s directory: %w", configDirName, err)
	}

	// Fix ownership if running as non-root user
	if homeDir != "/root" {
		statePath := destConfigPath
		if err := mgr.Chown(statePath, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set ownership: %w", err)
		}
	}

	logger("Session data restored successfully")
	return nil
}

// injectCredentials copies essential config files and sandbox settings from host to container when resuming.
// This ensures fresh authentication while preserving the session conversation history.
// Uses the ToolWithConfigDirFiles interface so each tool declares its own files and layout.
func injectCredentials(mgr *container.Manager, hostCLIConfigPath, homeDir string, tcf tool.ToolWithConfigDirFiles, logger func(string)) error {
	logger("Injecting fresh credentials and config for session resume...")

	configDirName := tcf.ConfigDirName()
	stateDir := filepath.Join(homeDir, configDirName)

	// Copy essential config files from host — only those that exist on host
	for _, filename := range tcf.EssentialConfigFiles() {
		if hostCLIConfigPath == "" {
			break
		}
		srcPath := filepath.Join(hostCLIConfigPath, filename)
		if _, err := os.Stat(srcPath); err == nil {
			destPath := filepath.Join(stateDir, filename)
			logger(fmt.Sprintf("  - Refreshing %s", filename))
			if err := mgr.PushFile(srcPath, destPath); err != nil {
				logger(fmt.Sprintf("  - Warning: Failed to copy %s: %v", filename, err))
			}
		}
	}

	// Inject sandbox settings into the tool's sandbox target file
	sandboxSettings := tcf.GetSandboxSettings()
	if len(sandboxSettings) > 0 {
		sandboxTarget := tcf.SandboxSettingsFileName()
		settingsPath := filepath.Join(stateDir, sandboxTarget)
		logger(fmt.Sprintf("Refreshing sandbox settings in %s...", sandboxTarget))

		settingsJSON, err := buildJSONFromSettings(sandboxSettings)
		if err != nil {
			logger(fmt.Sprintf("Warning: Failed to build JSON from settings: %v", err))
		} else {
			// Check if sandbox target file exists in container
			checkCmd := fmt.Sprintf("test -f %s && echo exists || echo missing", settingsPath)
			checkResult, err := mgr.ExecCommand(checkCmd, container.ExecCommandOptions{Capture: true})

			if err != nil || strings.TrimSpace(checkResult) == "missing" {
				// File doesn't exist, create it with sandbox settings
				logger(fmt.Sprintf("%s not found in container, creating with sandbox settings", sandboxTarget))
				settingsBytes, err := json.MarshalIndent(sandboxSettings, "", "  ")
				if err != nil {
					logger(fmt.Sprintf("Warning: Failed to marshal sandbox settings: %v", err))
				} else if err := mgr.CreateFile(settingsPath, string(settingsBytes)+"\n"); err != nil {
					logger(fmt.Sprintf("Warning: Failed to create %s: %v", sandboxTarget, err))
				}
			} else {
				// File exists, merge sandbox settings into it
				escapedJSON := strings.ReplaceAll(settingsJSON, "'", "'\"'\"'")
				injectCmd := fmt.Sprintf(
					`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); updates=json.loads('"'"'%s'"'"'); [d.setdefault(k,{}).update(v) if isinstance(v,dict) and isinstance(d.get(k),dict) else d.__setitem__(k,v) for k,v in updates.items()]; f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
					settingsPath,
					escapedJSON,
				)
				if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
					logger(fmt.Sprintf("Warning: Failed to inject settings into %s: %v", sandboxTarget, err))
				}
			}
		}
	}

	// Copy and refresh tool state config file (e.g., .claude.json) — only if tool uses one
	stateConfigFilename := tcf.StateConfigFileName()
	if stateConfigFilename != "" && hostCLIConfigPath != "" {
		stateConfigPath := filepath.Join(filepath.Dir(hostCLIConfigPath), stateConfigFilename)

		if _, err := os.Stat(stateConfigPath); err == nil {
			logger(fmt.Sprintf("Copying %s for session resume...", stateConfigFilename))
			stateJsonDest := filepath.Join(homeDir, stateConfigFilename)
			if err := mgr.PushFile(stateConfigPath, stateJsonDest); err != nil {
				logger(fmt.Sprintf("Warning: Failed to copy %s: %v", stateConfigFilename, err))
			} else if len(sandboxSettings) > 0 {
				// Inject sandbox settings into state config too
				logger(fmt.Sprintf("Injecting sandbox settings into %s...", stateConfigFilename))
				settingsJSON, err := buildJSONFromSettings(sandboxSettings)
				if err != nil {
					logger(fmt.Sprintf("Warning: Failed to build JSON from settings: %v", err))
				} else {
					escapedJSON := strings.ReplaceAll(settingsJSON, "'", "'\"'\"'")
					injectCmd := fmt.Sprintf(
						`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); updates=json.loads('"'"'%s'"'"'); [d.setdefault(k,{}).update(v) if isinstance(v,dict) and isinstance(d.get(k),dict) else d.__setitem__(k,v) for k,v in updates.items()]; f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
						stateJsonDest,
						escapedJSON,
					)
					if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
						logger(fmt.Sprintf("Warning: Failed to inject settings into %s: %v", stateConfigFilename, err))
					}
				}
			}
		}
	}

	// Fix ownership recursively (matching setupCLIConfig pattern)
	if homeDir != "/root" {
		chownCmd := fmt.Sprintf("chown -R %d:%d %s", container.CodeUID, container.CodeUID, stateDir)
		if _, err := mgr.ExecCommand(chownCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			logger(fmt.Sprintf("Warning: Failed to set %s directory ownership: %v", configDirName, err))
		}

		// Also fix state config file ownership if it was copied
		if stateConfigFilename != "" && hostCLIConfigPath != "" {
			stateJsonDest := filepath.Join(homeDir, stateConfigFilename)
			if err := mgr.Chown(stateJsonDest, container.CodeUID, container.CodeUID); err != nil {
				logger(fmt.Sprintf("Warning: Failed to set %s ownership: %v", stateConfigFilename, err))
			}
		}
	}

	logger("Credentials and config injected successfully")
	return nil
}

// setupCLIConfig copies tool config directory and injects sandbox settings
func setupCLIConfig(mgr *container.Manager, hostCLIConfigPath, homeDir string, tcf tool.ToolWithConfigDirFiles, logger func(string)) error {
	configDirName := tcf.ConfigDirName()
	stateDir := filepath.Join(homeDir, configDirName)

	// Create config directory in container
	logger(fmt.Sprintf("Creating %s directory in container...", configDirName))
	mkdirCmd := fmt.Sprintf("mkdir -p %s", stateDir)
	if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", configDirName, err)
	}

	essentialFiles := tcf.EssentialConfigFiles()
	sandboxTarget := tcf.SandboxSettingsFileName()

	logger(fmt.Sprintf("Copying essential CLI config files from %s", hostCLIConfigPath))
	for _, filename := range essentialFiles {
		srcPath := filepath.Join(hostCLIConfigPath, filename)
		if _, err := os.Stat(srcPath); err == nil {
			destPath := filepath.Join(stateDir, filename)
			logger(fmt.Sprintf("  - Copying %s", filename))
			if err := mgr.PushFile(srcPath, destPath); err != nil {
				logger(fmt.Sprintf("  - Warning: Failed to copy %s: %v", filename, err))
			}
		} else {
			logger(fmt.Sprintf("  - Skipping %s (not found)", filename))
		}
	}

	// Get sandbox settings from tool and merge into sandbox target file if needed
	sandboxSettings := tcf.GetSandboxSettings()
	if len(sandboxSettings) > 0 {
		settingsPath := filepath.Join(stateDir, sandboxTarget)
		logger(fmt.Sprintf("Merging sandbox settings into %s...", sandboxTarget))
		settingsJSON, err := buildJSONFromSettings(sandboxSettings)
		if err != nil {
			logger(fmt.Sprintf("Warning: Failed to build JSON from settings: %v", err))
		} else {
			// Check if sandbox target file exists in container
			checkCmd := fmt.Sprintf("test -f %s && echo exists || echo missing", settingsPath)
			checkResult, err := mgr.ExecCommand(checkCmd, container.ExecCommandOptions{Capture: true})

			if err != nil || strings.TrimSpace(checkResult) == "missing" {
				// File doesn't exist, create it with sandbox settings
				logger(fmt.Sprintf("%s not found in container, creating with sandbox settings", sandboxTarget))
				settingsBytes, err := json.MarshalIndent(sandboxSettings, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal sandbox settings: %w", err)
				}
				if err := mgr.CreateFile(settingsPath, string(settingsBytes)+"\n"); err != nil {
					return fmt.Errorf("failed to create %s: %w", sandboxTarget, err)
				}
			} else {
				// File exists, merge sandbox settings into it
				logger(fmt.Sprintf("Merging sandbox settings into existing %s", sandboxTarget))
				// Properly escape the JSON string for shell command
				escapedJSON := strings.ReplaceAll(settingsJSON, "'", "'\"'\"'")
				injectCmd := fmt.Sprintf(
					`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); updates=json.loads('"'"'%s'"'"'); [d.setdefault(k,{}).update(v) if isinstance(v,dict) and isinstance(d.get(k),dict) else d.__setitem__(k,v) for k,v in updates.items()]; f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
					settingsPath,
					escapedJSON,
				)
				if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
					logger(fmt.Sprintf("Warning: Failed to inject settings into %s: %v", sandboxTarget, err))
				} else {
					logger(fmt.Sprintf("Successfully merged sandbox settings into %s", sandboxTarget))
				}
			}
		}
		logger(fmt.Sprintf("%s config copied and sandbox settings merged into %s", tcf.Name(), sandboxTarget))
	} else {
		logger(fmt.Sprintf("%s config copied (no sandbox settings needed)", tcf.Name()))
	}

	// Copy and modify tool state config file (e.g., .claude.json)
	// This is a sibling file next to the config directory — only some tools use it
	stateConfigFilename := tcf.StateConfigFileName()
	if stateConfigFilename == "" {
		// Fix ownership of config directory and return
		if homeDir != "/root" {
			chownCmd := fmt.Sprintf("chown -R %d:%d %s", container.CodeUID, container.CodeUID, stateDir)
			if _, err := mgr.ExecCommand(chownCmd, container.ExecCommandOptions{Capture: true}); err != nil {
				return fmt.Errorf("failed to set %s directory ownership: %w", configDirName, err)
			}
		}
		return nil
	}

	stateConfigPath := filepath.Join(filepath.Dir(hostCLIConfigPath), stateConfigFilename)
	logger(fmt.Sprintf("Checking for %s at: %s", stateConfigFilename, stateConfigPath))

	if info, err := os.Stat(stateConfigPath); err == nil {
		logger(fmt.Sprintf("Found %s (size: %d bytes), copying to container...", stateConfigFilename, info.Size()))
		stateJsonDest := filepath.Join(homeDir, stateConfigFilename)

		// Push the file to container
		if err := mgr.PushFile(stateConfigPath, stateJsonDest); err != nil {
			return fmt.Errorf("failed to copy %s: %w", stateConfigFilename, err)
		}
		logger(fmt.Sprintf("%s copied to %s", stateConfigFilename, stateJsonDest))

		// Inject sandbox settings if tool provides them
		if len(sandboxSettings) > 0 {
			logger(fmt.Sprintf("Injecting sandbox settings into %s...", stateConfigFilename))
			settingsJSON, err := buildJSONFromSettings(sandboxSettings)
			if err != nil {
				logger(fmt.Sprintf("Warning: Failed to build JSON from settings: %v", err))
			} else {
				// Properly escape the JSON string for shell command
				escapedJSON := strings.ReplaceAll(settingsJSON, "'", "'\"'\"'")
				injectCmd := fmt.Sprintf(
					`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); updates=json.loads('"'"'%s'"'"'); [d.setdefault(k,{}).update(v) if isinstance(v,dict) and isinstance(d.get(k),dict) else d.__setitem__(k,v) for k,v in updates.items()]; f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
					stateJsonDest,
					escapedJSON,
				)
				if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
					logger(fmt.Sprintf("Warning: Failed to inject settings into %s: %v", stateConfigFilename, err))
				} else {
					logger(fmt.Sprintf("Successfully injected sandbox settings into %s", stateConfigFilename))
				}
			}
		}

		// Fix ownership if running as non-root user
		if homeDir != "/root" {
			if err := mgr.Chown(stateJsonDest, container.CodeUID, container.CodeUID); err != nil {
				return fmt.Errorf("failed to set %s ownership: %w", stateConfigFilename, err)
			}
		}

		// Fix ownership of entire config directory recursively
		if homeDir != "/root" {
			logger(fmt.Sprintf("Fixing ownership of entire %s directory to %d:%d", configDirName, container.CodeUID, container.CodeUID))
			chownCmd := fmt.Sprintf("chown -R %d:%d %s", container.CodeUID, container.CodeUID, stateDir)
			if _, err := mgr.ExecCommand(chownCmd, container.ExecCommandOptions{Capture: true}); err != nil {
				return fmt.Errorf("failed to set %s directory ownership: %w", configDirName, err)
			}
		}

		logger(fmt.Sprintf("%s setup complete", stateConfigFilename))
	} else if os.IsNotExist(err) {
		// Host file doesn't exist, but we may still need to create it in the container
		// to inject sandbox settings (e.g., effort level for Claude)
		if len(sandboxSettings) > 0 {
			logger(fmt.Sprintf("%s not found on host, creating in container with sandbox settings...", stateConfigFilename))
			stateJsonDest := filepath.Join(homeDir, stateConfigFilename)

			settingsJSON, err := buildJSONFromSettings(sandboxSettings)
			if err != nil {
				logger(fmt.Sprintf("Warning: Failed to build JSON from settings: %v", err))
			} else {
				// Create new file with sandbox settings
				createCmd := fmt.Sprintf("echo '%s' > %s", settingsJSON, stateJsonDest)
				if _, err := mgr.ExecCommand(createCmd, container.ExecCommandOptions{Capture: true}); err != nil {
					logger(fmt.Sprintf("Warning: Failed to create %s: %v", stateConfigFilename, err))
				} else {
					logger(fmt.Sprintf("Created %s with sandbox settings", stateConfigFilename))
				}

				// Fix ownership if running as non-root user
				if homeDir != "/root" {
					if err := mgr.Chown(stateJsonDest, container.CodeUID, container.CodeUID); err != nil {
						logger(fmt.Sprintf("Warning: Failed to set %s ownership: %v", stateConfigFilename, err))
					}
				}
			}
		} else {
			logger(fmt.Sprintf("%s not found at %s and no sandbox settings needed, skipping", stateConfigFilename, stateConfigPath))
		}
	} else {
		return fmt.Errorf("failed to check %s: %w", stateConfigFilename, err)
	}

	return nil
}

// injectContextFile creates ~/SANDBOX_CONTEXT.md inside the container.
// If customPath is provided, it reads the file from the host and uses its content.
// Otherwise, it renders the default embedded template with dynamic environment info.
func injectContextFile(mgr *container.Manager, info tool.ContextInfo, customPath, homeDir string, logger func(string)) error {
	destPath := filepath.Join(homeDir, "SANDBOX_CONTEXT.md")

	var content string
	if customPath != "" {
		data, err := os.ReadFile(customPath)
		if err != nil {
			return fmt.Errorf("failed to read custom context file %s: %w", customPath, err)
		}
		content = string(data)
		logger(fmt.Sprintf("Using custom context file: %s", customPath))
	} else {
		content = tool.RenderContextFileContent(info)
		logger("Injecting default sandbox context file")
	}

	// Create the file
	if err := mgr.CreateFile(destPath, content); err != nil {
		return fmt.Errorf("failed to create context file %s: %w", destPath, err)
	}

	// Fix ownership if running as non-root user
	if homeDir != "/root" {
		if err := mgr.Chown(destPath, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set context file ownership: %w", err)
		}
	}

	logger(fmt.Sprintf("Context file injected at %s", destPath))
	return nil
}

// setupSSHAgentForwarding configures an Incus proxy device to forward the host's
// SSH agent socket into the container. This allows git operations inside the
// container to use the host's SSH keys without copying them.
// Returns the container socket path if forwarding was successfully configured,
// or an empty string if forwarding was skipped (no SSH_AUTH_SOCK, invalid socket).
func setupSSHAgentForwarding(mgr *container.Manager, containerName string, logger func(string)) (string, error) {
	hostSocket := filepath.Clean(os.Getenv("SSH_AUTH_SOCK"))
	if hostSocket == "." || hostSocket == "" {
		logger("SSH agent forwarding skipped: SSH_AUTH_SOCK not set on host")
		return "", nil
	}

	// Verify the socket exists
	if _, err := os.Stat(hostSocket); err != nil {
		logger(fmt.Sprintf("SSH agent forwarding skipped: socket %s not accessible: %v", hostSocket, err))
		return "", nil
	}

	containerSocket := "/tmp/ssh-agent.sock"

	// Remove existing device if present (socket path may have changed between sessions)
	_ = mgr.RemoveDevice("ssh-agent")

	logger(fmt.Sprintf("Forwarding SSH agent: %s -> %s", hostSocket, containerSocket))

	if err := addSSHAgentProxyDevice(mgr, hostSocket, containerSocket); err != nil {
		return "", fmt.Errorf("failed to add SSH agent proxy device: %w", err)
	}

	// Verify the socket appears inside the container. Incus proxy devices can
	// take a moment to create the listen socket, especially on freshly-started
	// containers. If the socket doesn't appear, retry with a remove+re-add
	// which works around an Incus race condition.
	if waitForContainerSocket(mgr, containerSocket, 5*time.Second) {
		return containerSocket, nil
	}

	logger("SSH agent socket not yet available, retrying device setup...")
	_ = mgr.RemoveDevice("ssh-agent")

	if err := addSSHAgentProxyDevice(mgr, hostSocket, containerSocket); err != nil {
		return "", fmt.Errorf("failed to re-add SSH agent proxy device: %w", err)
	}

	if !waitForContainerSocket(mgr, containerSocket, 5*time.Second) {
		logger("Warning: SSH agent socket did not appear in container after retry")
		return "", nil
	}

	return containerSocket, nil
}

// addSSHAgentProxyDevice adds the proxy device for SSH agent forwarding.
func addSSHAgentProxyDevice(mgr *container.Manager, hostSocket, containerSocket string) error {
	return mgr.AddProxyDevice(
		"ssh-agent",
		fmt.Sprintf("unix:%s", hostSocket),
		fmt.Sprintf("unix:%s", containerSocket),
		container.CodeUID,
		container.CodeUID,
	)
}

// waitForContainerSocket polls for a Unix socket to appear inside the container.
func waitForContainerSocket(mgr *container.Manager, socketPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	checkCmd := fmt.Sprintf("test -S %s", socketPath)
	for time.Now().Before(deadline) {
		if _, err := mgr.ExecCommand(checkCmd, container.ExecCommandOptions{Capture: true}); err == nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// hasLimits checks if any limits are configured
func hasLimits(cfg *config.LimitsConfig) bool {
	if cfg == nil {
		return false
	}

	// Check if any limit is set (non-empty strings or non-zero integers)
	return cfg.CPU.Count != "" ||
		cfg.CPU.Allowance != "" ||
		cfg.CPU.Priority != 0 ||
		cfg.Memory.Limit != "" ||
		cfg.Memory.Enforce != "" ||
		cfg.Memory.Swap != "" ||
		cfg.Disk.Read != "" ||
		cfg.Disk.Write != "" ||
		cfg.Disk.Max != "" ||
		cfg.Disk.Priority != 0 ||
		cfg.Runtime.MaxProcesses != 0
}
