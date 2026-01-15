package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mensfeld/claude-on-incus/internal/config"
	"github.com/mensfeld/claude-on-incus/internal/container"
	"github.com/mensfeld/claude-on-incus/internal/network"
	"github.com/mensfeld/claude-on-incus/internal/tool"
)

const (
	DefaultImage = "images:ubuntu/22.04"
	CoiImage     = "coi"
)

// buildJSONFromSettings converts a settings map to a properly escaped JSON string
// Uses json.Marshal to ensure proper escaping and avoid command injection
func buildJSONFromSettings(settings map[string]interface{}) (string, error) {
	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to marshal settings: %w", err)
	}
	return string(jsonBytes), nil
}

// SetupOptions contains options for setting up a session
type SetupOptions struct {
	WorkspacePath string
	Image         string
	Persistent    bool // Keep container between sessions (don't delete on cleanup)
	ResumeFromID  string
	Slot          int
	StoragePath   string
	SessionsDir   string    // e.g., ~/.coi/sessions-claude
	CLIConfigPath string    // e.g., ~/.claude (host CLI config to copy credentials from)
	Tool          tool.Tool // AI coding tool being used
	NetworkConfig *config.NetworkConfig
	Logger        func(string)
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
			if err := os.MkdirAll(opts.StoragePath, 0o755); err != nil {
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
	// Skip if tool uses ENV-based auth (no config directory)
	if opts.ResumeFromID != "" && opts.Tool != nil && opts.Tool.ConfigDirName() != "" {
		// If we launched a new container (not reusing persistent one), restore config from saved session
		if !skipLaunch && opts.SessionsDir != "" {
			if err := restoreSessionData(result.Manager, opts.ResumeFromID, result.HomeDir, opts.SessionsDir, opts.Tool, opts.Logger); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Could not restore session data: %v", err))
			}
		}

		// Always inject fresh credentials when resuming (whether persistent container or restored session)
		if opts.CLIConfigPath != "" {
			if err := injectCredentials(result.Manager, opts.CLIConfigPath, result.HomeDir, opts.Tool, opts.Logger); err != nil {
				opts.Logger(fmt.Sprintf("Warning: Could not inject credentials: %v", err))
			}
		}
	}

	// 8. Workspace and storage are already mounted (added before container start in step 5)
	if skipLaunch {
		opts.Logger("Reusing existing workspace and storage mounts")
	}

	// 10. Setup CLI tool config (skip if resuming - config already restored)
	// Skip entirely if tool uses ENV-based auth (ConfigDirName returns "")
	if opts.Tool != nil && opts.Tool.ConfigDirName() != "" {
		if opts.CLIConfigPath != "" && opts.ResumeFromID == "" {
			// Check if host config directory exists
			if _, err := os.Stat(opts.CLIConfigPath); err == nil {
				// Copy and inject settings (but only if NOT resuming)
				// Only run on first launch, not when restarting persistent container
				if !skipLaunch {
					opts.Logger(fmt.Sprintf("Setting up %s config...", opts.Tool.Name()))
					if err := setupCLIConfig(result.Manager, opts.CLIConfigPath, result.HomeDir, opts.Tool, opts.Logger); err != nil {
						opts.Logger(fmt.Sprintf("Warning: Failed to setup %s config: %v", opts.Tool.Name(), err))
					}
				} else {
					opts.Logger(fmt.Sprintf("Reusing existing %s config (persistent container)", opts.Tool.Name()))
				}
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to check %s config directory: %w", opts.Tool.Name(), err)
			}
		} else if opts.ResumeFromID != "" {
			opts.Logger(fmt.Sprintf("Resuming session - using restored %s config", opts.Tool.Name()))
		}
	} else if opts.Tool != nil {
		opts.Logger(fmt.Sprintf("Tool %s uses ENV-based auth, skipping config setup", opts.Tool.Name()))
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

// injectCredentials copies credentials and essential config from host to container when resuming
// This ensures fresh authentication while preserving the session conversation history
func injectCredentials(mgr *container.Manager, hostCLIConfigPath, homeDir string, t tool.Tool, logger func(string)) error {
	logger("Injecting fresh credentials and config for session resume...")

	configDirName := t.ConfigDirName()

	// Copy .credentials.json from host to container
	credentialsPath := filepath.Join(hostCLIConfigPath, ".credentials.json")
	if _, err := os.Stat(credentialsPath); err != nil {
		return fmt.Errorf("credentials file not found: %w", err)
	}

	destCredentials := filepath.Join(homeDir, configDirName, ".credentials.json")
	if err := mgr.PushFile(credentialsPath, destCredentials); err != nil {
		return fmt.Errorf("failed to push credentials: %w", err)
	}

	// Fix ownership if running as non-root user
	if homeDir != "/root" {
		if err := mgr.Chown(destCredentials, container.CodeUID, container.CodeUID); err != nil {
			return fmt.Errorf("failed to set credentials ownership: %w", err)
		}
	}

	// Get sandbox settings from tool
	sandboxSettings := t.GetSandboxSettings()
	if len(sandboxSettings) > 0 {
		// Get the state config filename (e.g., ".claude.json" or ".aider.json")
		stateConfigFilename := fmt.Sprintf(".%s.json", t.Name())
		stateConfigPath := filepath.Join(filepath.Dir(hostCLIConfigPath), stateConfigFilename)

		if _, err := os.Stat(stateConfigPath); err == nil {
			logger(fmt.Sprintf("Copying %s for session resume...", stateConfigFilename))
			stateJsonDest := filepath.Join(homeDir, stateConfigFilename)
			if err := mgr.PushFile(stateConfigPath, stateJsonDest); err != nil {
				logger(fmt.Sprintf("Warning: Failed to copy %s: %v", stateConfigFilename, err))
			} else {
				// Inject sandbox settings using tool's GetSandboxSettings()
				logger(fmt.Sprintf("Injecting sandbox settings into %s...", stateConfigFilename))
				settingsJSON, err := buildJSONFromSettings(sandboxSettings)
				if err != nil {
					logger(fmt.Sprintf("Warning: Failed to build JSON from settings: %v", err))
				} else {
					// Properly escape the JSON string for shell command
					escapedJSON := strings.ReplaceAll(settingsJSON, "'", "'\"'\"'")
					injectCmd := fmt.Sprintf(
						`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); updates=json.loads('"'"'%s'"'"'); d.update(updates); f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
						stateJsonDest,
						escapedJSON,
					)
					if _, err := mgr.ExecCommand(injectCmd, container.ExecCommandOptions{Capture: true}); err != nil {
						logger(fmt.Sprintf("Warning: Failed to inject settings into %s: %v", stateConfigFilename, err))
					}
				}

				// Fix ownership if running as non-root user
				if homeDir != "/root" {
					if err := mgr.Chown(stateJsonDest, container.CodeUID, container.CodeUID); err != nil {
						logger(fmt.Sprintf("Warning: Failed to set %s ownership: %v", stateConfigFilename, err))
					}
				}
			}
		}
	}

	logger("Credentials and config injected successfully")
	return nil
}

// setupCLIConfig copies tool config directory and injects sandbox settings
func setupCLIConfig(mgr *container.Manager, hostCLIConfigPath, homeDir string, t tool.Tool, logger func(string)) error {
	configDirName := t.ConfigDirName()
	stateDir := filepath.Join(homeDir, configDirName)

	// Create config directory in container
	logger(fmt.Sprintf("Creating %s directory in container...", configDirName))
	mkdirCmd := fmt.Sprintf("mkdir -p %s", stateDir)
	if _, err := mgr.ExecCommand(mkdirCmd, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", configDirName, err)
	}

	// Copy only essential files from config directory (skip debug logs with permission issues)
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

	// Get sandbox settings from tool and create/update settings.json if needed
	sandboxSettings := t.GetSandboxSettings()
	if len(sandboxSettings) > 0 {
		settingsPath := filepath.Join(stateDir, "settings.json")
		// Build JSON from settings map using helper with pretty printing
		settingsBytes, err := json.MarshalIndent(sandboxSettings, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to build JSON from sandbox settings: %w", err)
		}

		if err := mgr.CreateFile(settingsPath, string(settingsBytes)+"\n"); err != nil {
			return fmt.Errorf("failed to create settings.json: %w", err)
		}
		logger(fmt.Sprintf("%s config copied and sandbox settings injected in settings.json", t.Name()))
	} else {
		logger(fmt.Sprintf("%s config copied (no sandbox settings needed)", t.Name()))
	}

	// Copy and modify tool state config file (e.g., .claude.json, .aider.json)
	// This is a sibling file to the config directory
	stateConfigFilename := fmt.Sprintf(".%s.json", t.Name())
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
					`python3 -c 'import json; f=open("%s","r+"); d=json.load(f); updates=json.loads('"'"'%s'"'"'); d.update(updates); f.seek(0); json.dump(d,f,indent=2); f.truncate()'`,
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
			logger(fmt.Sprintf("Fixing ownership of %s to %d:%d", stateConfigFilename, container.CodeUID, container.CodeUID))
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
		logger(fmt.Sprintf("Warning: %s not found at %s, skipping", stateConfigFilename, stateConfigPath))
	} else {
		return fmt.Errorf("failed to check %s: %w", stateConfigFilename, err)
	}

	return nil
}
