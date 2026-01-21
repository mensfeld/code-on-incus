package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/image"
	"github.com/spf13/cobra"
)

var buildForce bool

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build Incus image for AI coding sessions",
	Long: `Build the coi Incus image for running AI coding tools (Claude Code, Aider, etc.).

The coi image includes:
  - Base development tools
  - Node.js LTS
  - Claude CLI
  - Docker
  - GitHub CLI
  - tmux
  - dummy (test stub for testing)

Examples:
  coi build
  coi build --force
  coi build custom my-image --script setup.sh
`,
	Args: cobra.NoArgs,
	RunE: buildCommand,
}

// buildCustomCmd builds a custom image from a script
var buildCustomCmd = &cobra.Command{
	Use:   "custom <name>",
	Short: "Build a custom image from a user script",
	Long: `Build a custom image from the coi base image using a user-provided build script.

The build script should be a bash script that will be executed as root in the container.

Examples:
  coi build custom my-rust-image --script build-rust.sh
  coi build custom my-image --base coi --script setup.sh
  coi build custom my-image --base images:ubuntu/24.04 --script setup.sh`,
	Args: cobra.ExactArgs(1),
	RunE: buildCustomCommand,
}

func init() {
	buildCmd.Flags().BoolVar(&buildForce, "force", false, "Force rebuild even if image exists")

	// Custom build flags
	buildCustomCmd.Flags().String("script", "", "Path to build script (required)")
	buildCustomCmd.Flags().String("base", "", "Base image to build from (default: coi)")
	buildCustomCmd.Flags().BoolVar(&buildForce, "force", false, "Force rebuild even if image exists")
	_ = buildCustomCmd.MarkFlagRequired("script") // Always succeeds for valid flag names.

	buildCmd.AddCommand(buildCustomCmd)
}

func buildCommand(cmd *cobra.Command, args []string) error {
	// Check if Incus is available
	if !container.Available() {
		return fmt.Errorf("incus is not available - please install Incus and ensure you're in the incus-admin group")
	}

	// Configure build options
	opts := image.BuildOptions{
		Force:       buildForce,
		ImageType:   "coi",
		BaseImage:   image.BaseImage,
		AliasName:   image.CoiAlias,
		Description: "coi image (Docker + build tools + Claude CLI + GitHub CLI)",
		Logger: func(msg string) {
			fmt.Println(msg)
		},
	}

	// Build the image
	fmt.Println("Building coi image...")
	builder := image.NewBuilder(opts)
	result := builder.Build()

	if result.Error != nil {
		return fmt.Errorf("build failed: %w", result.Error)
	}

	if result.Skipped {
		fmt.Printf("\nImage already exists. Use --force to rebuild.\n")
		return nil
	}

	fmt.Printf("\n Image '%s' built successfully!\n", opts.AliasName)
	fmt.Printf("  Version: %s\n", result.VersionAlias)
	fmt.Printf("  Fingerprint: %s\n", result.Fingerprint)
	return nil
}

func buildCustomCommand(cmd *cobra.Command, args []string) error {
	imageName := args[0]
	scriptPath, _ := cmd.Flags().GetString("script")
	baseImage, _ := cmd.Flags().GetString("base")

	// Check if Incus is available
	if !container.Available() {
		return fmt.Errorf("incus is not available - please install Incus and ensure you're in the incus-admin group")
	}

	// Verify script exists
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("build script not found: %s", scriptPath)
	}

	// Default to coi base image
	if baseImage == "" {
		baseImage = image.CoiAlias
	}

	// Configure build options
	opts := image.BuildOptions{
		ImageType:   "custom",
		AliasName:   imageName,
		Description: fmt.Sprintf("Custom image: %s", imageName),
		BaseImage:   baseImage,
		BuildScript: scriptPath,
		Force:       buildForce,
		Logger: func(msg string) {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		},
	}

	// Build the image
	fmt.Fprintf(os.Stderr, "Building custom image '%s' from '%s'...\n", imageName, baseImage)
	builder := image.NewBuilder(opts)
	result := builder.Build()

	if result.Error != nil {
		return fmt.Errorf("build failed: %w", result.Error)
	}

	// Output result as JSON (even if skipped)
	output := map[string]interface{}{
		"alias":   imageName,
		"skipped": result.Skipped,
	}

	if !result.Skipped {
		output["fingerprint"] = result.Fingerprint
	} else {
		fmt.Fprintf(os.Stderr, "\nImage already exists. Use --force to rebuild.\n")
	}

	jsonOutput, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(jsonOutput))

	return nil
}
