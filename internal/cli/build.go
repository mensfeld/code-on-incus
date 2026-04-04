package cli

import (
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/image"
	"github.com/spf13/cobra"
)

var buildForce bool

var buildCompression string

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build Incus image for AI coding sessions",
	Long: `Build an Incus image using a profile's build configuration.

By default, builds the "coi-default" image using the built-in default profile.
Use --profile to build from a custom profile's [build] section.

Examples:
  coi build
  coi build --force
  coi build --profile rust-dev
`,
	Args: cobra.NoArgs,
	RunE: buildCommand,
}

func init() {
	buildCmd.Flags().BoolVarP(&buildForce, "force", "f", false, "Force rebuild even if image exists")
	buildCmd.Flags().StringVar(&buildCompression, "compression", "", "Compression algorithm (e.g., none, gzip, xz; see Incus docs for all options)")
}

func buildCommand(cmd *cobra.Command, args []string) error {
	// Check if Incus is available
	if !container.Available() {
		return fmt.Errorf("incus is not available - please install Incus and ensure you're in the incus-admin group")
	}

	// Check minimum Incus version
	if err := container.CheckMinimumVersion(); err != nil {
		return err
	}

	// Warn if kernel is too old (non-blocking)
	if warning := container.CheckKernelVersion(); warning != "" {
		fmt.Fprintf(os.Stderr, "%s\n", warning)
	}

	// Determine which profile to use
	profileName := profile // from --profile flag
	if profileName == "" {
		profileName = "default"
	}

	p := cfg.GetProfile(profileName)
	if p == nil {
		return fmt.Errorf("profile '%s' not found", profileName)
	}

	imageName := p.Image
	if imageName == "" {
		imageName = image.CoiAlias
	}

	// For coi-default image: always use the embedded build script
	if imageName == image.CoiAlias {
		opts := image.BuildOptions{
			Force:       buildForce,
			ImageType:   "coi",
			BaseImage:   image.BaseImage,
			AliasName:   image.CoiAlias,
			Description: "coi image (Docker + build tools + Claude CLI + GitHub CLI)",
			Compression: buildCompression,
			Logger: func(msg string) {
				fmt.Println(msg)
			},
		}

		fmt.Printf("Building image '%s' from profile '%s'...\n", imageName, profileName)
		builder := image.NewBuilder(opts)
		result := builder.Build()

		if result.Error != nil {
			return fmt.Errorf("build failed: %w", result.Error)
		}

		if result.Skipped {
			fmt.Printf("\nImage '%s' already exists. Use --force to rebuild.\n", opts.AliasName)
			return nil
		}

		fmt.Printf("\nImage '%s' built successfully!\n", opts.AliasName)
		fmt.Printf("  Version: %s\n", result.VersionAlias)
		fmt.Printf("  Fingerprint: %s\n", result.Fingerprint)
		return nil
	}

	// Custom profile build: requires [build] section
	if p.Build == nil || !p.Build.HasBuildConfig() {
		return fmt.Errorf("profile '%s' has no [build] section — add a build script or commands to the profile", profileName)
	}

	// Resolve build script
	scriptPath, cleanup, err := resolveBuildScript(p.Build)
	if err != nil {
		return err
	}
	defer cleanup()

	// Determine base image
	baseImage := p.Build.Base
	if baseImage == "" {
		baseImage = image.CoiAlias
	}

	opts := image.BuildOptions{
		ImageType:   "custom",
		AliasName:   imageName,
		Description: fmt.Sprintf("Custom image (%s)", imageName),
		BaseImage:   baseImage,
		BuildScript: scriptPath,
		Force:       buildForce,
		Compression: buildCompression,
		Logger: func(msg string) {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		},
	}

	fmt.Fprintf(os.Stderr, "Building image '%s' from profile '%s' (base: %s)...\n", imageName, profileName, baseImage)
	builder := image.NewBuilder(opts)
	result := builder.Build()

	if result.Error != nil {
		return fmt.Errorf("build failed: %w", result.Error)
	}

	if result.Skipped {
		fmt.Fprintf(os.Stderr, "\nImage '%s' already exists. Use --force to rebuild.\n", imageName)
		return nil
	}

	fmt.Fprintf(os.Stderr, "\nImage '%s' built successfully!\n", imageName)
	fmt.Fprintf(os.Stderr, "  Fingerprint: %s\n", result.Fingerprint)
	return nil
}
