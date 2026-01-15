package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mensfeld/claude-on-incus/internal/config"
	"github.com/mensfeld/claude-on-incus/internal/container"
	"github.com/mensfeld/claude-on-incus/internal/session"
	"github.com/spf13/cobra"
)

var (
	listAll    bool
	listFormat string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List active containers and saved sessions",
	Long: `List active claude-on-incus containers and saved sessions.

By default, shows only active containers. Use --all to also show saved sessions.

Examples:
  coi list
  coi list --all
`,
	RunE: listCommand,
}

func init() {
	listCmd.Flags().BoolVar(&listAll, "all", false, "Show saved sessions in addition to active containers")
	listCmd.Flags().StringVar(&listFormat, "format", "text", "Output format: text or json")
}

func listCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate format value
	if listFormat != "text" && listFormat != "json" {
		return fmt.Errorf("invalid format '%s': must be 'text' or 'json'", listFormat)
	}

	// Get configured tool to determine tool-specific sessions directory
	toolInstance, err := getConfiguredTool(cfg)
	if err != nil {
		return err
	}

	// Get tool-specific sessions directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	baseDir := filepath.Join(homeDir, ".coi")
	sessionsDir := session.GetSessionsDir(baseDir, toolInstance)

	// List active containers
	containers, err := listActiveContainers()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Build maps of container name -> workspace and container name -> persistent from saved sessions
	// We search for metadata.json files directly (not using listSavedSessions which requires .claude dir)
	// because metadata is saved early at session start, before .claude directory exists
	containerWorkspaces := make(map[string]string)
	containerPersistent := make(map[string]bool)
	if entries, err := os.ReadDir(sessionsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			metadataPath := filepath.Join(sessionsDir, entry.Name(), "metadata.json")
			if data, err := os.ReadFile(metadataPath); err == nil {
				var metadata session.SessionMetadata
				if err := json.Unmarshal(data, &metadata); err == nil && metadata.ContainerName != "" {
					containerWorkspaces[metadata.ContainerName] = metadata.Workspace
					containerPersistent[metadata.ContainerName] = metadata.Persistent
				}
			}
		}
	}

	// Get saved sessions if --all
	var sessions []SessionInfo
	if listAll {
		sessions, err = listSavedSessions(sessionsDir)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}
	}

	// Route to formatter
	if listFormat == "json" {
		return outputJSON(containers, sessions, containerWorkspaces, containerPersistent)
	}

	return outputText(containers, sessions, containerWorkspaces, containerPersistent)
}

// ContainerInfo holds information about a container
type ContainerInfo struct {
	Name      string
	Status    string
	CreatedAt string
	Image     string
}

// SessionInfo holds information about a saved session
type SessionInfo struct {
	ID        string
	SavedAt   string
	Workspace string
}

// listActiveContainers lists all active claude-on-incus containers
func listActiveContainers() ([]ContainerInfo, error) {
	// Use the configured container prefix (respects COI_CONTAINER_PREFIX env var)
	prefix := session.GetContainerPrefix()
	pattern := fmt.Sprintf("^%s", prefix)

	output, err := container.IncusOutput("list", pattern, "--format=json")
	if err != nil {
		return nil, err
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return nil, err
	}

	var result []ContainerInfo
	for _, c := range containers {
		name, _ := c["name"].(string)            // Type assertion, default to "" if fails
		status, _ := c["status"].(string)        // Type assertion, default to "" if fails
		createdAt, _ := c["created_at"].(string) // Type assertion, default to "" if fails

		// Get image info
		config, _ := c["config"].(map[string]interface{}) // Type assertion
		image, _ := config["image.description"].(string)  // Type assertion

		// Parse created_at time
		createdTime := ""
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			createdTime = t.Format("2006-01-02 15:04:05")
		}

		result = append(result, ContainerInfo{
			Name:      name,
			Status:    status,
			CreatedAt: createdTime,
			Image:     image,
		})
	}

	return result, nil
}

// listSavedSessions lists all saved sessions
func listSavedSessions(sessionsDir string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, err
	}

	var result []SessionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sessionID := entry.Name()

		// Check if it has a .claude directory
		statePath := filepath.Join(sessionsDir, sessionID, ".claude")
		if info, err := os.Stat(statePath); err != nil || !info.IsDir() {
			continue
		}

		// Try to read metadata
		metadataPath := filepath.Join(sessionsDir, sessionID, "metadata.json")
		savedAt := ""
		workspace := ""

		if data, err := os.ReadFile(metadataPath); err == nil {
			var metadata session.SessionMetadata
			if err := json.Unmarshal(data, &metadata); err == nil {
				savedAt = metadata.SavedAt
				workspace = metadata.Workspace
			}
		}

		// Get directory modification time as fallback
		if savedAt == "" {
			if info, err := entry.Info(); err == nil {
				savedAt = info.ModTime().Format("2006-01-02 15:04:05")
			}
		}

		result = append(result, SessionInfo{
			ID:        sessionID,
			SavedAt:   savedAt,
			Workspace: workspace,
		})
	}

	return result, nil
}

// outputJSON formats container and session data as JSON
func outputJSON(containers []ContainerInfo, sessions []SessionInfo,
	workspaces map[string]string, persistent map[string]bool,
) error {
	// Enrich container data
	enrichedContainers := make([]map[string]interface{}, 0, len(containers))
	for _, c := range containers {
		item := map[string]interface{}{
			"name":       c.Name,
			"status":     c.Status,
			"created_at": c.CreatedAt,
			"image":      c.Image,
			"persistent": persistent[c.Name],
		}
		if ws, ok := workspaces[c.Name]; ok {
			item["workspace"] = ws
		}
		enrichedContainers = append(enrichedContainers, item)
	}

	// Build output structure
	output := map[string]interface{}{
		"active_containers": enrichedContainers,
	}

	if len(sessions) > 0 {
		output["saved_sessions"] = sessions
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(jsonData))
	return nil
}

// outputText formats container and session data as human-readable text
func outputText(containers []ContainerInfo, sessions []SessionInfo,
	workspaces map[string]string, persistent map[string]bool,
) error {
	// Active Containers section
	fmt.Println("Active Containers:")
	fmt.Println("------------------")

	if len(containers) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, c := range containers {
			// Show container name with mode indicator from session metadata
			// (not from Incus state, since all containers are now created as persistent in Incus)
			if persistent[c.Name] {
				fmt.Printf("  %s (persistent)\n", c.Name)
			} else {
				fmt.Printf("  %s (ephemeral)\n", c.Name)
			}
			fmt.Printf("    Status: %s\n", c.Status)
			fmt.Printf("    Created: %s\n", c.CreatedAt)
			if c.Image != "" {
				fmt.Printf("    Image: %s\n", c.Image)
			}
			// Show workspace if we have it from session metadata
			if workspace, ok := workspaces[c.Name]; ok && workspace != "" {
				fmt.Printf("    Workspace: %s\n", workspace)
			}
		}
	}

	// Saved Sessions section (only with --all)
	if sessions != nil {
		fmt.Println("\nSaved Sessions:")
		fmt.Println("---------------")

		if len(sessions) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, s := range sessions {
				fmt.Printf("  %s\n", s.ID)
				fmt.Printf("    Saved: %s\n", s.SavedAt)
				if s.Workspace != "" {
					fmt.Printf("    Workspace: %s\n", s.Workspace)
				}
			}
		}
	}

	return nil
}
