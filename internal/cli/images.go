package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/image"
	"github.com/spf13/cobra"
)

var showAll bool

// imageCmd is the parent command for all image operations
var imageCmd = &cobra.Command{
	Use:   "image",
	Short: "Manage Incus images",
	Long:  `Operations for listing, publishing, deleting, and managing container images.`,
}

// Legacy imagesCmd for backwards compatibility (coi images)
var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List available Incus images (alias for 'image list')",
	Long: `List available Incus images for use with --image flag.

Shows both built COI images and available remote images.

Examples:
  coi images              # List COI images only
  coi images --all        # List all local images
`,
	RunE: imageListCommand,
}

// imageListCmd lists available images
var imageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available images",
	Long: `List available Incus images with optional filtering.

Examples:
  coi image list                           # List COI images
  coi image list --all                     # List all local images
  coi image list --prefix claudeyard-      # List images starting with prefix
  coi image list --format json             # Output as JSON`,
	RunE: imageListCommand,
}

// imagePublishCmd publishes a container as an image
var imagePublishCmd = &cobra.Command{
	Use:   "publish <container> <alias>",
	Short: "Publish a stopped container as an image",
	Long: `Publish a container as an image with the given alias.

Example:
  coi image publish my-container my-image --description "Custom build with Python 3.11"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		containerName := args[0]
		aliasName := args[1]

		description, _ := cmd.Flags().GetString("description")

		// Publish container
		fingerprint, err := container.PublishContainer(containerName, aliasName, description)
		if err != nil {
			return exitError(1, fmt.Sprintf("failed to publish container: %v", err))
		}

		// Output as JSON
		result := map[string]string{
			"fingerprint": fingerprint,
			"alias":       aliasName,
		}
		jsonOutput, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(jsonOutput))

		return nil
	},
}

// imageDeleteCmd deletes an image
var imageDeleteCmd = &cobra.Command{
	Use:   "delete <alias>",
	Short: "Delete an image by alias",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		aliasName := args[0]

		if err := container.DeleteImage(aliasName); err != nil {
			return exitError(1, fmt.Sprintf("failed to delete image: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Image %s deleted\n", aliasName)
		return nil
	},
}

// imageExistsCmd checks if an image exists
var imageExistsCmd = &cobra.Command{
	Use:   "exists <alias>",
	Short: "Check if an image exists",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		aliasName := args[0]

		exists, err := container.ImageExists(aliasName)
		if err != nil {
			return exitError(1, fmt.Sprintf("failed to check image: %v", err))
		}

		if !exists {
			return exitError(1, "")
		}

		return nil
	},
}

// imageCleanupCmd cleans up old image versions
var imageCleanupCmd = &cobra.Command{
	Use:   "cleanup <prefix>",
	Short: "Delete old image versions, keeping only N most recent",
	Long: `Delete old versions of images matching a prefix, keeping only the N most recent.

Image aliases must follow format: prefix-YYYYMMDD-HHMMSS

Example:
  # Keep only the 3 most recent versions of node-42 images
  coi image cleanup claudeyard-node-42- --keep 3`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prefix := args[0]
		keepCount, _ := cmd.Flags().GetInt("keep")

		if keepCount <= 0 {
			return exitError(2, "--keep must be > 0")
		}

		deleted, kept, err := image.Cleanup(prefix, keepCount)
		if err != nil {
			return exitError(1, fmt.Sprintf("cleanup failed: %v", err))
		}

		fmt.Fprintf(os.Stderr, "Cleanup complete:\n")
		if len(deleted) > 0 {
			fmt.Fprintf(os.Stderr, "\nDeleted %d old version(s):\n", len(deleted))
			for _, alias := range deleted {
				fmt.Fprintf(os.Stderr, "  - %s\n", alias)
			}
		}
		if len(kept) > 0 {
			fmt.Fprintf(os.Stderr, "\nKept %d recent version(s):\n", len(kept))
			for _, alias := range kept {
				fmt.Fprintf(os.Stderr, "  - %s\n", alias)
			}
		}

		return nil
	},
}

func init() {
	// Add flags to list command
	imageListCmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all local images, not just COI images")
	imageListCmd.Flags().String("prefix", "", "Filter images by alias prefix")
	imageListCmd.Flags().String("format", "table", "Output format: table or json")

	// Add flags to legacy images command
	imagesCmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all local images, not just COI images")

	// Add flags to publish command
	imagePublishCmd.Flags().String("description", "", "Image description")

	// Add flags to cleanup command
	imageCleanupCmd.Flags().Int("keep", 0, "Number of versions to keep (required)")
	_ = imageCleanupCmd.MarkFlagRequired("keep") // Always succeeds for valid flag names.

	// Add subcommands to image command
	imageCmd.AddCommand(imageListCmd)
	imageCmd.AddCommand(imagePublishCmd)
	imageCmd.AddCommand(imageDeleteCmd)
	imageCmd.AddCommand(imageExistsCmd)
	imageCmd.AddCommand(imageCleanupCmd)
}

func imageListCommand(cmd *cobra.Command, args []string) error {
	// Check if Incus is available
	if !container.Available() {
		return fmt.Errorf("incus is not available - please install Incus and ensure you're in the incus-admin group")
	}

	format, _ := cmd.Flags().GetString("format")
	prefix, _ := cmd.Flags().GetString("prefix")

	// If format is JSON, output structured data
	if format == "json" {
		images, err := image.ListAllImages(prefix)
		if err != nil {
			return fmt.Errorf("failed to list images: %w", err)
		}

		jsonOutput, _ := json.MarshalIndent(images, "", "  ")
		fmt.Println(string(jsonOutput))
		return nil
	}

	// Table format (human-readable)
	if prefix != "" {
		// List with prefix filter
		images, err := image.ListAllImages(prefix)
		if err != nil {
			return fmt.Errorf("failed to list images: %w", err)
		}

		if len(images) == 0 {
			fmt.Printf("No images found with prefix '%s'\n", prefix)
			return nil
		}

		fmt.Printf("Images with prefix '%s':\n\n", prefix)
		fmt.Printf("%-40s %-20s %s\n", "ALIAS", "SIZE", "CREATED")
		fmt.Println(strings.Repeat("-", 80))
		for _, img := range images {
			for _, alias := range img.Aliases {
				sizeFormatted := formatSize(fmt.Sprintf("%d", img.Size))
				createdFormatted := img.CreatedAt.Format("2006-01-02 15:04")
				fmt.Printf("%-40s %-20s %s\n", alias, sizeFormatted, createdFormatted)
			}
		}
		return nil
	}

	// Default listing (COI images + optional all)
	fmt.Println("Available Images:")
	fmt.Println()

	// Check COI images
	coiImages := []struct {
		alias       string
		description string
		buildCmd    string
	}{
		{"coi", "coi image (Claude CLI, Node.js, Docker, GitHub CLI, tmux)", "coi build"},
	}

	fmt.Println("COI Images:")
	for _, img := range coiImages {
		exists, err := container.ImageExists(img.alias)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s - error checking: %v\n", img.alias, err)
			continue
		}

		if exists {
			fmt.Printf("  ✓ %s\n", img.alias)
			fmt.Printf("    %s\n", img.description)
		} else {
			fmt.Printf("  ✗ %s (not built)\n", img.alias)
			fmt.Printf("    %s\n", img.description)
			fmt.Printf("    Build with: %s\n", img.buildCmd)
		}
		fmt.Println()
	}

	if showAll {
		fmt.Println("All Local Images:")
		if err := listAllImages(); err != nil {
			return err
		}
	} else {
		fmt.Println("Tip: Use --all to see all local images")
	}

	fmt.Println()
	fmt.Println("Remote Images:")
	fmt.Println("  You can use any image from images.linuxcontainers.org:")
	fmt.Println("  - ubuntu:22.04, ubuntu:24.04")
	fmt.Println("  - debian:12, debian:11")
	fmt.Println("  - alpine:3.19")
	fmt.Println()
	fmt.Println("  Example: coi shell --image ubuntu:24.04")
	fmt.Println()
	fmt.Println("Custom Images:")
	fmt.Println("  Build your own: coi build custom --script setup.sh my-image")
	fmt.Println()

	return nil
}

// listAllImages lists all local Incus images
func listAllImages() error {
	mgr := container.NewManager("temp")

	// Get list of images using incus image list with sg wrapper
	output, err := mgr.ExecHostCommand("sg incus-admin -c 'incus image list --format=csv -c l,s,u'", true)
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		fmt.Println("  No local images found")
		return nil
	}

	fmt.Printf("  %-30s %-15s %s\n", "ALIAS", "SIZE", "UPLOAD DATE")
	fmt.Println("  " + strings.Repeat("-", 70))

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		alias := parts[0]
		size := parts[1]
		uploadDate := parts[2]

		// Format size (convert bytes to human readable)
		sizeFormatted := formatSize(size)

		fmt.Printf("  %-30s %-15s %s\n", alias, sizeFormatted, uploadDate)
	}

	return nil
}

// formatSize converts byte string to human readable
func formatSize(sizeStr string) string {
	// Size is in bytes as string, convert to MB/GB
	var bytes int64
	_, _ = fmt.Sscanf(sizeStr, "%d", &bytes) // Ignore error, default to 0 if parse fails

	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.1fGB", float64(bytes)/(1024*1024*1024))
}
