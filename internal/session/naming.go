package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// GetContainerPrefix returns the container prefix to use.
// Checks COI_CONTAINER_PREFIX environment variable first, defaults to "coi-".
// This allows tests to use a different prefix (e.g., "coi-test-") to avoid
// interfering with user's active sessions.
func GetContainerPrefix() string {
	if prefix := os.Getenv("COI_CONTAINER_PREFIX"); prefix != "" {
		return prefix
	}
	return "coi-"
}

// WorkspaceHash generates a short hash from workspace path
// Returns first 8 characters of SHA256 hash
func WorkspaceHash(workspacePath string) string {
	// Normalize path (resolve symlinks, make absolute)
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		absPath = workspacePath
	}

	hash := sha256.Sum256([]byte(absPath))
	return fmt.Sprintf("%x", hash)[:8]
}

// ContainerName generates a container name from workspace and slot
// Format: <prefix><workspace-hash>-<slot> where prefix defaults to "coi-"
// Can be customized via COI_CONTAINER_PREFIX environment variable
func ContainerName(workspacePath string, slot int) string {
	hash := WorkspaceHash(workspacePath)
	prefix := GetContainerPrefix()
	return fmt.Sprintf("%s%s-%d", prefix, hash, slot)
}

// AllocateSlot finds the next available slot for a workspace
// Returns the slot number (1, 2, 3, ...) or 0 if no slots available
func AllocateSlot(workspacePath string, maxSlots int) (int, error) {
	if maxSlots == 0 {
		maxSlots = 10 // Default max 10 parallel sessions
	}

	hash := WorkspaceHash(workspacePath)
	prefix := fmt.Sprintf("%s%s-", GetContainerPrefix(), hash)

	// Get all containers matching our workspace
	output, err := container.IncusOutput("list", "--format=json")
	if err != nil {
		return 0, err
	}

	// Parse running containers using proper JSON parsing
	runningSlots := make(map[int]bool)
	re := regexp.MustCompile(fmt.Sprintf(`^%s(\d+)$`, regexp.QuoteMeta(prefix)))

	// Parse JSON array of containers
	var containers []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		// Fallback: if JSON parsing fails, try regex on raw output
		nameMatches := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`).FindAllStringSubmatch(output, -1)
		for _, match := range nameMatches {
			if len(match) > 1 {
				containerName := match[1]
				if matches := re.FindStringSubmatch(containerName); len(matches) > 1 {
					if slotNum, err := strconv.Atoi(matches[1]); err == nil {
						runningSlots[slotNum] = true
					}
				}
			}
		}
	} else {
		for _, c := range containers {
			if matches := re.FindStringSubmatch(c.Name); len(matches) > 1 {
				if slotNum, err := strconv.Atoi(matches[1]); err == nil {
					runningSlots[slotNum] = true
				}
			}
		}
	}

	// Find first available slot
	for slot := 1; slot <= maxSlots; slot++ {
		if !runningSlots[slot] {
			return slot, nil
		}
	}

	return 0, fmt.Errorf("all %d slots are in use", maxSlots)
}

// AllocateSlotFrom finds the next available slot starting from a specific slot number
// Returns the slot number or error if no slots available
func AllocateSlotFrom(workspacePath string, startSlot, maxSlots int) (int, error) {
	if maxSlots == 0 {
		maxSlots = 10 // Default max 10 parallel sessions
	}

	hash := WorkspaceHash(workspacePath)
	prefix := fmt.Sprintf("%s%s-", GetContainerPrefix(), hash)

	// Get all containers matching our workspace
	output, err := container.IncusOutput("list", "--format=json")
	if err != nil {
		return 0, err
	}

	// Parse running containers using proper JSON parsing
	runningSlots := make(map[int]bool)
	re := regexp.MustCompile(fmt.Sprintf(`^%s(\d+)$`, regexp.QuoteMeta(prefix)))

	// Parse JSON array of containers
	var containers []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		// Fallback: if JSON parsing fails, try regex on raw output
		nameMatches := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`).FindAllStringSubmatch(output, -1)
		for _, match := range nameMatches {
			if len(match) > 1 {
				containerName := match[1]
				if matches := re.FindStringSubmatch(containerName); len(matches) > 1 {
					if slotNum, err := strconv.Atoi(matches[1]); err == nil {
						runningSlots[slotNum] = true
					}
				}
			}
		}
	} else {
		for _, c := range containers {
			if matches := re.FindStringSubmatch(c.Name); len(matches) > 1 {
				if slotNum, err := strconv.Atoi(matches[1]); err == nil {
					runningSlots[slotNum] = true
				}
			}
		}
	}

	// Find first available slot starting from startSlot
	for slot := startSlot; slot <= maxSlots; slot++ {
		if !runningSlots[slot] {
			return slot, nil
		}
	}

	return 0, fmt.Errorf("no available slots from %d to %d", startSlot, maxSlots)
}

// IsSlotAvailable checks if a specific slot is available
func IsSlotAvailable(workspacePath string, slot int) (bool, error) {
	containerName := ContainerName(workspacePath, slot)
	running, err := container.ContainerRunning(containerName)
	if err != nil {
		return false, err
	}
	return !running, nil
}

// ParseContainerName extracts workspace hash and slot from container name
// Returns (hash, slot, error)
func ParseContainerName(containerName string) (string, int, error) {
	prefix := regexp.QuoteMeta(GetContainerPrefix())
	re := regexp.MustCompile(fmt.Sprintf(`^%s([a-f0-9]{8})-(\d+)$`, prefix))
	matches := re.FindStringSubmatch(containerName)
	if len(matches) != 3 {
		return "", 0, fmt.Errorf("invalid container name format: %s", containerName)
	}

	hash := matches[1]
	slot, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", 0, fmt.Errorf("invalid slot number in container name: %s", containerName)
	}

	return hash, slot, nil
}

// ListWorkspaceSessions lists all sessions for a workspace
// Returns map of slot -> container name
func ListWorkspaceSessions(workspacePath string) (map[int]string, error) {
	hash := WorkspaceHash(workspacePath)
	prefix := fmt.Sprintf("%s%s-", GetContainerPrefix(), hash)

	output, err := container.IncusOutput("list", "--format=json")
	if err != nil {
		return nil, err
	}

	sessions := make(map[int]string)
	re := regexp.MustCompile(fmt.Sprintf(`^%s(\d+)$`, regexp.QuoteMeta(prefix)))

	// Parse JSON array of containers
	var containers []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		// Fallback: if JSON parsing fails, try regex on raw output
		nameMatches := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`).FindAllStringSubmatch(output, -1)
		for _, match := range nameMatches {
			if len(match) > 1 {
				containerName := match[1]
				if matches := re.FindStringSubmatch(containerName); len(matches) > 1 {
					if slotNum, err := strconv.Atoi(matches[1]); err == nil {
						sessions[slotNum] = containerName
					}
				}
			}
		}
	} else {
		for _, c := range containers {
			if matches := re.FindStringSubmatch(c.Name); len(matches) > 1 {
				if slotNum, err := strconv.Atoi(matches[1]); err == nil {
					sessions[slotNum] = c.Name
				}
			}
		}
	}

	return sessions, nil
}
