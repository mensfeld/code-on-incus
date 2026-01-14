package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mensfeld/claude-on-incus/internal/config"
	"github.com/mensfeld/claude-on-incus/internal/container"
	"github.com/mensfeld/claude-on-incus/internal/network"
)

const (
	DefaultImage = "images:ubuntu/22.04"
	CoiImage     = "coi"
)

// SetupOptions contains options for setting up a session
type SetupOptions struct {
	WorkspacePath    string
	Image            string
	Persistent       bool   // Keep container between sessions (don't delete on cleanup)
	ResumeFromID     string
	Slot             int
	StoragePath      string
	SessionsDir      string // e.g., ~/.claude-on-incus/sessions
	CLIConfigPath    string // e.g., ~/.claude (host CLI config to copy credentials from)
	NetworkConfig    *config.NetworkConfig
	Logger           func(string)
}

// SetupResult contains the result of setup
type SetupResult struct {
	ContainerName  string
	Manager        *container.Manager
	NetworkManager *network.Manager
	HomeDir        string
	RunAsRoot      bool
	Image          string
}

// Setup initializes a container for a Claude session
// This configures the container with workspace mounting and user setup
func Setup(opts SetupOptions) (*SetupResult, error) {
	result := &SetupResult{}

	// Default logger
	if opts.Logger == nil {
		opts.Logger = func(msg string) {
			fmt.Fprintf(os.Stderr, "[setup] %s\n", msg)
		}
	}

	// 1. Generate container name
	containerName := ContainerName(opts.WorkspacePath, opts.Slot)
	result.ContainerName = containerName
	result.Manager = container.NewManager(containerName)
	opts.Logger(fmt.Sprintf("Container name: %s", containerName))

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
			if opts.Persistent {
				opts.Logger("Container already running, reusing...")
				skipLaunch = true
			} else {
				// ERROR: A running container exists for this slot, but we're not in persistent mode
				// This means AllocateSlot() gave us a slot that's already in use!
				return nil, fmt.Errorf("slot %d is already in use by a running container %s - this should not happen (bug in slot allocation)", opts.Slot, containerName)
			}
		} else {
			// Container exists but is stopped
			if opts.Persistent {
				// Restart the stopped persistent container
				opts.Logger("Restarting existing persistent container...")
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
		// Local: Use shift=true (kernel idmap support)
		// CI: Use raw.idmap (kernel lacks idmap support, runner UID 1001 → container UID 1000)
		useShift := true
		isCI := os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true"

		if isCI {
			opts.Logger("Configuring UID/GID mapping for CI environment...")
			if err := container.IncusExec("config", "set", result.ContainerName, "raw.idmap", "both 1001 1000"); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Failed to set raw.idmap: %v", err))
			}
			useShift = false // Don't use shift=true with raw.idmap
		}

		// Add disk devices BEFORE starting container
		opts.Logger(fmt.Sprintf("Adding workspace mount: %s", opts.WorkspacePath))
		if err := result.Manager.MountDisk("workspace", opts.WorkspacePath, "/workspace", useShift); err != nil {
			return nil, fmt.Errorf("failed to add workspace device: %w", err)
		}

		// Add storage device if specified
		if opts.StoragePath != "" {
			if err := os.MkdirAll(opts.StoragePath, 0755); err != nil {
				return nil, fmt.Errorf("failed to create storage directory: %w", err)
			}
			opts.Logger(fmt.Sprintf("Adding storage mount: %s", opts.StoragePath))
			if err := result.Manager.MountDisk("storage", opts.StoragePath, "/storage", useShift); err != nil {
				return nil, fmt.Errorf("failed to add storage device: %w", err)
			}
		}

		// Setup network isolation (before starting container)
		if opts.NetworkConfig != nil {
			result.NetworkManager = network.NewManager(opts.NetworkConfig)
			if err := result.NetworkManager.SetupForContainer(context.Background(), result.ContainerName); err != nil {
				return nil, fmt.Errorf("failed to setup network isolation: %w", err)
			}
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

	// 7. When resuming: restore session data if container was recreated, then inject credentials
	if opts.ResumeFromID != "" {
		// If we launched a new container (not reusing persistent one), restore .claude from saved session
		if !skipLaunch && opts.SessionsDir != "" {
			if err := restoreSessionData(result.Manager, opts.ResumeFromID, result.HomeDir, opts.SessionsDir, opts.Logger); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Could not restore session data: %v", err))
			}
		}

		// Always inject fresh credentials when resuming (whether persistent container or restored session)
		if opts.CLIConfigPath != "" {
			if err := injectCredentials(result.Manager, opts.CLIConfigPath, result.HomeDir, opts.Logger); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Could not inject credentials: %v", err))
			}
		}
	}

	// 8. Workspace and storage are already mounted (added before container start in step 5)
	if skipLaunch {
		opts.Logger("Reusing existing workspace and storage mounts")
	}

	// 10. Setup Claude config (skip if resuming - .claude already restored)
	if opts.CLIConfigPath != "" && opts.ResumeFromID == "" {
		// Check if host .claude directory exists
		if _, err := os.Stat(opts.CLIConfigPath); err == nil {
			// Copy and inject settings (but only if NOT resuming)
			// Only run on first launch, not when restarting persistent container
			if !skipLaunch {
				opts.Logger("Setting up Claude config...")
				if err := setupCLIConfig(result.Manager, opts.CLIConfigPath, result.HomeDir, opts.Logger); err != nil {
					opts.Logger(fmt.Sprintf("Warning: Failed to setup Claude config: %v", err))
				}
			} else {
				opts.Logger("Reusing existing Claude config (persistent container)")
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to check Claude config directory: %w", err)
		}
	} else if opts.ResumeFromID != "" {
		opts.Logger("Resuming session - using restored .claude config")
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

// restoreSessionData restores .claude directory from a saved session
// Used when resuming a non-persistent session (container was deleted and recreated)
func restoreSessionData(mgr *container.Manager, resumeID, homeDir, sessionsDir string, logger func(string)) error {
	sourceClaudeDir := filepath.Join(sessionsDir, resumeID, ".claude")

	// Check if directory exists
	if info, err := os.Stat(sourceClaudeDir); err != nil || !info.IsDir() {
		return fmt.Errorf("no saved session data found for %s", resumeID)
	}

	logger(fmt.Sprintf("Restoring session data from %s", resumeID))

	// Push .claude directory to container
	// PushDirectory extracts the parent from the path and pushes to create the directory there
	// So we pass the full destination path where .claude should end up
	destClaudePath := filepath.Join(homeDir, ".claude")
	if err := mgr.PushDirectory(sourceClaudeDir, destClaudePath); err != nil {
		return fmt.Errorf("failed to push .claude directory: %w", err)
	}

	// Fix ownership if running as claude user
	if homeDir != "/root" {
		statePath := destClaudePath
		if err := mgr.Chown(statePath, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set ownership: %w", err)
		}
	}

	logger("Session data restored successfully")
	return nil
}

// injectCredentials copies credentials and essential config from host to container when resuming
// This ensures fresh authentication while preserving the session conversation history
func injectCredentials(mgr *container.Manager, hostCLIConfigPath, homeDir string, logger func(string)) error {
	logger("Injecting fresh credentials and config for session resume...")

	// Copy .credentials.json from host to container
	credentialsPath := filepath.Join(hostCLIConfigPath, ".credentials.json")
	if _, err := os.Stat(credentialsPath); err != nil {
		return fmt.Errorf("credentials file not found: %w", err)
	}

	destCredentials := filepath.Join(homeDir, ".claude", ".credentials.json")
	if err := mgr.PushFile(credentialsPath, destCredentials); err != nil {
		return fmt.Errorf("failed to push credentials: %w", err)
	}

	// Fix ownership if running as claude user
	if homeDir != "/root" {
		if err := mgr.Chown(destCredentials, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set credentials ownership: %w", err)
		}
	}

	// Also copy .claude.json (sibling to .claude directory) if it exists
	// This file contains important config like theme, startup count, etc.
	stateConfigPath := filepath.Join(filepath.Dir(hostCLIConfigPath), ".claude.json")
	if _, err := os.Stat(stateConfigPath); err == nil {
		logger("Copying .claude.json for session resume...")
		claudeJsonDest := filepath.Join(homeDir, ".claude.json")
		if err := mgr.PushFile(stateConfigPath, claudeJsonDest); err != nil {
			logger(fmt.Sprintf("Warning: Failed to copy .claude.json: %v", err))
		} else {
			// Inject sandbox settings into .claude.json
			logger("Injecting sandbox settings into .claude.json...")
			injectCmd := fmt.Sprintf(
				`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); d["allowDangerouslySkipPermissions"]=True; d["bypassPermissionsModeAccepted"]=True; d["permissions"]={"defaultMode":"bypassPermissions"}; f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
				claudeJsonDest,
			)
			if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
				logger(fmt.Sprintf("Warning: Failed to inject settings into .claude.json: %v", err))
			}

			// Fix ownership if running as claude user
			if homeDir != "/root" {
				if err := mgr.Chown(claudeJsonDest, container.CodeUID, container.CodeUID); err != nil {
					logger(fmt.Sprintf("Warning: Failed to set .claude.json ownership: %v", err))
				}
			}
		}
	}

	logger("Credentials and config injected successfully")
	return nil
}

// setupCLIConfig copies .claude directory and injects sandbox settings
func setupCLIConfig(mgr *container.Manager, hostCLIConfigPath, homeDir string, logger func(string)) error {
	stateDir := filepath.Join(homeDir, ".claude")

	// Create .claude directory in container
	logger("Creating .claude directory in container...")
	mkdirCmd := fmt.Sprintf("mkdir -p %s", stateDir)
	if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Copy only essential files from .claude directory (skip debug logs with permission issues)
	essentialFiles := []string{
		".credentials.json",
		"config.yml",
		"settings.json",
	}

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

	// Create or update settings.json with sandbox settings
	settingsPath := filepath.Join(stateDir, "settings.json")
	sandboxSettings := `{
  "includeCoAuthoredBy": false,
  "allowDangerouslySkipPermissions": true,
  "bypassPermissionsModeAccepted": true,
  "permissions": {
    "defaultMode": "bypassPermissions"
  }
}
`
	if err := mgr.CreateFile(settingsPath, sandboxSettings); err != nil {
		return fmt.Errorf("failed to create settings.json: %w", err)
	}

	logger("Claude config copied and sandbox settings injected in settings.json")

	// Copy and modify .claude.json (sibling to .claude directory)
	stateConfigPath := filepath.Join(filepath.Dir(hostCLIConfigPath), ".claude.json")
	logger(fmt.Sprintf("Checking for .claude.json at: %s", stateConfigPath))

	if info, err := os.Stat(stateConfigPath); err == nil {
		logger(fmt.Sprintf("Found .claude.json (size: %d bytes), copying to container...", info.Size()))
		claudeJsonDest := filepath.Join(homeDir, ".claude.json")

		// Push the file to container
		if err := mgr.PushFile(stateConfigPath, claudeJsonDest); err != nil {
			return fmt.Errorf("failed to copy .claude.json: %w", err)
		}
		logger(fmt.Sprintf(".claude.json copied to %s", claudeJsonDest))

		// Inject sandbox settings into .claude.json
		logger("Injecting sandbox settings into .claude.json...")
		injectCmd := fmt.Sprintf(
			`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); d["allowDangerouslySkipPermissions"]=True; d["bypassPermissionsModeAccepted"]=True; d["permissions"]={"defaultMode":"bypassPermissions"}; f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
			claudeJsonDest,
		)
		if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
			logger(fmt.Sprintf("Warning: Failed to inject settings into .claude.json: %v", err))
		} else {
			logger("Successfully injected sandbox settings into .claude.json")
		}

		// Fix ownership if running as claude user
		if homeDir != "/root" {
			logger(fmt.Sprintf("Fixing ownership of .claude.json to %d:%d", container.CodeUID, container.CodeUID))
			if err := mgr.Chown(claudeJsonDest, container.CodeUID, container.CodeUID); err != nil {
				return fmt.Errorf("failed to set .claude.json ownership: %w", err)
			}
		}

		// Fix ownership of entire .claude directory recursively
		if homeDir != "/root" {
			logger(fmt.Sprintf("Fixing ownership of entire .claude directory to %d:%d", container.CodeUID, container.CodeUID))
			chownCmd := fmt.Sprintf("chown -R %d:%d %s", container.CodeUID, container.CodeUID, stateDir)
			if _, err := mgr.ExecCommand(chownCmd, container.ExecCommandOptions{Capture: true}); err != nil {
				return fmt.Errorf("failed to set .claude directory ownership: %w", err)
			}
		}

		logger(".claude.json setup complete")
	} else if os.IsNotExist(err) {
		logger(fmt.Sprintf("Warning: .claude.json not found at %s, skipping", stateConfigPath))
	} else {
		return fmt.Errorf("failed to check .claude.json: %w", err)
	}

	return nil
}
