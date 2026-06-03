package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/logger"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

// CleanupOptions contains options for cleaning up a session
type CleanupOptions struct {
	ContainerName  string
	SessionID      string    // COI session ID for saving tool config data
	Persistent     bool      // If true, stop but don't delete container
	ProfileName    string    // Profile used for this session (saved in metadata for --resume)
	SessionsDir    string    // e.g., ~/.coi/sessions-claude
	SaveSession    bool      // Whether to save tool config directory
	Workspace      string    // Workspace directory path
	Tool           tool.Tool // AI coding tool being used
	NetworkManager network.NetworkManager
	SessionLogger  *logger.SessionLogger
	Logger         func(string)
}

// Cleanup stops and deletes a container, optionally saving session data
func Cleanup(opts CleanupOptions) error {
	// Default logger
	if opts.Logger == nil {
		opts.Logger = func(msg string) {
			fmt.Fprintf(os.Stderr, "[cleanup] %s\n", msg)
		}
	}

	// Clear host-side immutable bits early, before any container operations.
	// This ensures immutable bits are cleared even if the container is already
	// stopped/deleted, and must happen before container deletion so the host
	// files are writable again.
	if opts.ContainerName != "" {
		RemoveImmutable(opts.ContainerName, opts.Logger)
	}

	if opts.ContainerName == "" {
		opts.Logger("No container to clean up")
		return nil
	}

	mgr := container.NewManager(opts.ContainerName)

	// Check if container exists
	// Containers are always launched as non-ephemeral, so they should exist even when stopped
	exists, err := mgr.Exists()
	if err != nil {
		opts.Logger(fmt.Sprintf("Warning: Could not check container existence: %v", err))
	}

	// Save session data immediately — no pre-delay regardless of persistence mode.
	// saveSessionData handles the mid-shutdown race internally: on a transient SFTP
	// error it waits for the container to fully stop, then retries using incus's
	// direct file-access path (no SFTP) so the save succeeds in every exit scenario.
	if opts.SaveSession && exists && opts.SessionID != "" && opts.SessionsDir != "" && opts.Tool != nil && opts.Tool.ConfigDirName() != "" {
		if err := saveSessionData(mgr, opts.ContainerName, opts.SessionID, opts.Persistent, opts.ProfileName, opts.Workspace, opts.SessionsDir, opts.Tool, opts.Logger); err != nil {
			opts.Logger(fmt.Sprintf("Warning: Failed to save session data: %v", err))
		}
	}

	// Handle container based on persistence mode
	if opts.Persistent {
		// Persistent mode: keep container regardless of whether it is running or stopped.
		if exists {
			opts.Logger("Container kept running - use 'coi attach' to reconnect, 'coi shutdown' to stop, or 'coi kill' to force stop")
		} else {
			opts.Logger("Container was stopped but kept for reuse")
		}
	} else {
		// Non-persistent mode: behavior depends on how user exited.
		// - Container still running (user typed 'exit' or detached): keep it running.
		// - Container stopped (user ran 'sudo poweroff'): delete it.
		if exists {
			// Check if container is stopped, with retries to handle shutdown delay.
			// Poweroff/shutdown can take several seconds to complete.
			running := true
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				running, _ = mgr.Running()
				if !running {
					break
				}
			}

			if running {
				// Container still running - user exited normally, keep it for potential re-attach.
				opts.Logger("Container kept running - use 'coi attach' to reconnect, 'coi shutdown' to stop, or 'coi kill' to force stop")
			} else {
				// Container stopped (user ran 'sudo poweroff') - delete it.
				opts.Logger("Container was stopped, removing...")

				// Clean up network FIRST while container still exists
				// This ensures we can get the container IP to remove firewall rules
				if opts.NetworkManager != nil {
					if err := opts.NetworkManager.Teardown(context.Background(), opts.ContainerName); err != nil {
						opts.Logger(fmt.Sprintf("Warning: Failed to cleanup network: %v", err))
					}
				}

				// Now delete container
				if err := mgr.Delete(true); err != nil {
					opts.Logger(fmt.Sprintf("Warning: Failed to delete container: %v", err))
				} else {
					opts.Logger("Container removed (session data saved for --resume)")
				}
			}
		} else {
			opts.Logger("Container was already removed")
		}
	}

	if opts.SessionLogger != nil {
		if err := opts.SessionLogger.Close(); err != nil && opts.Logger != nil {
			opts.Logger(fmt.Sprintf("Warning: failed to close session log: %v", err))
		}
	}

	return nil
}

// saveSessionData saves the tool config directory from the container
func saveSessionData(mgr container.ContainerManager, containerName string, sessionID string, persistent bool, profileName string, workspace string, sessionsDir string, t tool.Tool, logger func(string)) error {
	// Determine home directory
	// For coi images, we always use /home/code
	// For other images, we use /root
	// Since we currently only support coi images, always use /home/code
	homeDir := "/home/" + container.CodeUser

	configDirName := t.ConfigDirName()
	stateDir := filepath.Join(homeDir, configDirName)

	// Create local session directory
	localSessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(localSessionDir, 0o755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	logger(fmt.Sprintf("Saving session data to %s", localSessionDir))

	// Remove old config directory if it exists (when resuming)
	localConfigDir := filepath.Join(localSessionDir, configDirName)
	if _, err := os.Stat(localConfigDir); err == nil {
		logger("Removing old session data before saving new state")
		if err := os.RemoveAll(localConfigDir); err != nil {
			return fmt.Errorf("failed to remove old %s directory: %w", configDirName, err)
		}
	}

	// Pull config directory from container.
	//
	// On the normal exit path the container is running and SFTP is healthy —
	// the first attempt succeeds immediately.
	//
	// On the poweroff path, cleanup may start while the container's SFTP subsystem
	// is still mid-teardown (sshd killed by init, container not yet fully stopped).
	// On a transient SFTP/connection error we wait up to 5 s for the container to
	// fully stop, then retry. Once stopped, incus switches to direct file access
	// (no SFTP), so the retry reliably succeeds.
	var pullErr error
	for attempt := range 3 {
		if attempt > 0 {
			// SFTP failed — wait for the container to fully stop so incus
			// uses direct file access (not SFTP) on the next attempt.
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				if running, _ := mgr.Running(); !running {
					break
				}
			}
		}
		pullErr = mgr.PullDirectory(stateDir, localConfigDir)
		if pullErr == nil {
			break
		}
		msg := strings.ToLower(pullErr.Error())
		if strings.Contains(msg, "does not exist") ||
			strings.Contains(msg, "not found") ||
			strings.Contains(msg, "no such file") {
			logger(fmt.Sprintf("No %s directory found in container", configDirName))
			return nil
		}
		if !strings.Contains(msg, "sftp") &&
			!strings.Contains(msg, "unexpected eof") &&
			!strings.Contains(msg, "bad file descriptor") &&
			!strings.Contains(msg, "server unexpectedly closed") &&
			!strings.Contains(msg, "error receiving version packet") {
			break
		}
	}
	if pullErr != nil {
		return fmt.Errorf("failed to pull %s directory: %w", configDirName, pullErr)
	}

	// Save metadata
	metadata := SessionMetadata{
		SessionID:     sessionID,
		ContainerName: containerName,
		Persistent:    persistent,
		ProfileName:   profileName,
		Workspace:     workspace,
		SavedAt:       getCurrentTime(),
	}

	metadataPath := filepath.Join(localSessionDir, "metadata.json")
	if err := saveMetadata(metadataPath, metadata); err != nil {
		// Non-fatal - session data is already saved
		logger(fmt.Sprintf("Warning: Failed to save metadata: %v", err))
	}

	logger("Session data saved successfully")
	return nil
}

// SessionMetadata contains information about a saved session
type SessionMetadata struct {
	SessionID     string `json:"session_id"`
	ContainerName string `json:"container_name"`
	Persistent    bool   `json:"persistent"`
	ProfileName   string `json:"profile_name"`
	Workspace     string `json:"workspace"`
	SavedAt       string `json:"saved_at"`
}

// saveMetadata saves session metadata to a JSON file
func saveMetadata(path string, metadata SessionMetadata) error {
	// Simple JSON marshaling
	content := fmt.Sprintf(`{
  "session_id": "%s",
  "container_name": "%s",
  "persistent": %t,
  "profile_name": "%s",
  "workspace": "%s",
  "saved_at": "%s"
}
`, metadata.SessionID, metadata.ContainerName, metadata.Persistent, metadata.ProfileName, metadata.Workspace, metadata.SavedAt)

	return os.WriteFile(path, []byte(content), 0o644)
}

// getCurrentTime returns current time in RFC3339 format
func getCurrentTime() string {
	return time.Now().Format(time.RFC3339)
}

// SaveMetadataEarly saves session metadata at session start so coi list can show correct status
func SaveMetadataEarly(sessionsDir, sessionID, containerName, workspace string, persistent bool, profileName string) error {
	// Create session directory if it doesn't exist
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	metadata := SessionMetadata{
		SessionID:     sessionID,
		ContainerName: containerName,
		Persistent:    persistent,
		ProfileName:   profileName,
		Workspace:     workspace,
		SavedAt:       getCurrentTime(),
	}

	metadataPath := filepath.Join(sessionDir, "metadata.json")
	return saveMetadata(metadataPath, metadata)
}

// SessionExists checks if a session with the given ID exists and is valid
func SessionExists(sessionsDir, sessionID string) bool {
	metadataPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
	info, err := os.Stat(metadataPath)
	return err == nil && !info.IsDir()
}

// ListSavedSessions lists all saved sessions in the sessions directory
func ListSavedSessions(sessionsDir string) ([]string, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var sessions []string
	for _, entry := range entries {
		if entry.IsDir() {
			// Check if it contains a metadata.json file (tool-agnostic indicator)
			metadataPath := filepath.Join(sessionsDir, entry.Name(), "metadata.json")
			if info, err := os.Stat(metadataPath); err == nil && !info.IsDir() {
				sessions = append(sessions, entry.Name())
			}
		}
	}

	return sessions, nil
}

// GetLatestSession returns the most recently saved session ID
func GetLatestSession(sessionsDir string) (string, error) {
	sessions, err := ListSavedSessions(sessionsDir)
	if err != nil {
		return "", err
	}

	if len(sessions) == 0 {
		return "", fmt.Errorf("no saved sessions found")
	}

	// Find the most recent session by reading metadata
	var latestSession string
	var latestTime time.Time

	for _, sessionID := range sessions {
		metadataPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
		metadata, err := LoadSessionMetadata(metadataPath)
		if err != nil {
			continue // Skip sessions without valid metadata
		}

		savedTime, err := time.Parse(time.RFC3339, metadata.SavedAt)
		if err != nil {
			continue
		}

		if latestSession == "" || savedTime.After(latestTime) {
			latestSession = sessionID
			latestTime = savedTime
		}
	}

	if latestSession == "" {
		return "", fmt.Errorf("no valid sessions found")
	}

	return latestSession, nil
}

// GetLatestSessionForWorkspace returns the most recent session ID for a specific workspace
func GetLatestSessionForWorkspace(sessionsDir, workspacePath string) (string, error) {
	sessions, err := ListSavedSessions(sessionsDir)
	if err != nil {
		return "", err
	}

	if len(sessions) == 0 {
		return "", fmt.Errorf("no saved sessions found")
	}

	// Get the workspace hash to match against
	workspaceHash := WorkspaceHash(workspacePath)

	// Find the most recent session for this workspace
	var latestSession string
	var latestTime time.Time

	for _, sessionID := range sessions {
		metadataPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
		metadata, err := LoadSessionMetadata(metadataPath)
		if err != nil {
			continue // Skip sessions without valid metadata
		}

		// Extract workspace hash from container name (format: claude-<hash>-<slot>)
		sessionHash, _, err := ParseContainerName(metadata.ContainerName)
		if err != nil {
			continue
		}

		// Only consider sessions from the same workspace
		if sessionHash != workspaceHash {
			continue
		}

		savedTime, err := time.Parse(time.RFC3339, metadata.SavedAt)
		if err != nil {
			continue
		}

		if latestSession == "" || savedTime.After(latestTime) {
			latestSession = sessionID
			latestTime = savedTime
		}
	}

	if latestSession == "" {
		return "", fmt.Errorf("no saved sessions found for workspace %s", workspacePath)
	}

	return latestSession, nil
}

// LoadSessionMetadata loads session metadata from a JSON file
func LoadSessionMetadata(path string) (*SessionMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var metadata SessionMetadata
	// Simple JSON parsing
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "\"session_id\"") {
			metadata.SessionID = extractJSONValue(line)
		} else if strings.Contains(line, "\"container_name\"") {
			metadata.ContainerName = extractJSONValue(line)
		} else if strings.Contains(line, "\"persistent\"") {
			metadata.Persistent = strings.Contains(line, "true")
		} else if strings.Contains(line, "\"profile_name\"") {
			metadata.ProfileName = extractJSONValue(line)
		} else if strings.Contains(line, "\"workspace\"") {
			metadata.Workspace = extractJSONValue(line)
		} else if strings.Contains(line, "\"saved_at\"") {
			metadata.SavedAt = extractJSONValue(line)
		}
	}

	if metadata.SessionID == "" {
		return nil, fmt.Errorf("invalid metadata: missing session_id")
	}

	return &metadata, nil
}

// extractJSONValue extracts the value from a JSON line like `"key": "value",`
func extractJSONValue(line string) string {
	// Find the value between quotes after the colon
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return ""
	}

	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `",`)
	return value
}

// GetCLISessionID extracts the CLI tool's session ID from a saved coi session.
// CLI tools store sessions in .claude/projects/-workspace/<session-id>.jsonl
// Returns empty string if no session found.
func GetCLISessionID(sessionsDir, coiSessionID string) string {
	projectsDir := filepath.Join(sessionsDir, coiSessionID, ".claude", "projects", "-workspace")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}

	// Look for .jsonl files (session files)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".jsonl") {
			// Extract session ID from filename (remove .jsonl suffix)
			return strings.TrimSuffix(name, ".jsonl")
		}
	}

	return ""
}
