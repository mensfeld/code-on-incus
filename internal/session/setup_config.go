package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

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
				// Create new file with sandbox settings without going through a shell
				if err := mgr.CreateFile(stateJsonDest, settingsJSON+"\n"); err != nil {
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
