package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/image"
)

// BuildFromConfigResult contains the result of a config-driven build
type BuildFromConfigResult struct {
	Built       bool   // True if a build was performed
	Skipped     bool   // True if image already existed
	ImageName   string // The image alias that was built
	Fingerprint string // Fingerprint of the built image
}

// BuildFromConfig builds a custom image using the [build] section from config.
// It requires both cfg.Build to have script/commands AND cfg.Defaults.Image to be set
// to a custom name (not "coi").
func BuildFromConfig(cfg *config.Config, force bool, compression string) (*BuildFromConfigResult, error) {
	result := &BuildFromConfigResult{}

	if !cfg.Build.HasBuildConfig() {
		return nil, fmt.Errorf("no [build] configuration found in config")
	}

	imageName := cfg.Defaults.Image
	if imageName == "" || imageName == "coi" {
		return nil, fmt.Errorf("defaults.image must be set to a custom image name when using [build] config")
	}
	result.ImageName = imageName

	// Determine base image
	baseImage := cfg.Build.Base
	if baseImage == "" {
		baseImage = image.CoiAlias
	}

	// Determine build script path
	scriptPath, cleanup, err := resolveBuildScript(cfg)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Configure build options
	opts := image.BuildOptions{
		ImageType:   "custom",
		AliasName:   imageName,
		Description: fmt.Sprintf("Custom image (%s)", imageName),
		BaseImage:   baseImage,
		BuildScript: scriptPath,
		Force:       force,
		Compression: compression,
		Logger: func(msg string) {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		},
	}

	// Build the image
	fmt.Fprintf(os.Stderr, "Building custom image '%s' from '%s'...\n", imageName, baseImage)
	builder := image.NewBuilder(opts)
	buildResult := builder.Build()

	if buildResult.Error != nil {
		return nil, fmt.Errorf("build failed: %w", buildResult.Error)
	}

	if buildResult.Skipped {
		result.Skipped = true
		return result, nil
	}

	result.Built = true
	result.Fingerprint = buildResult.Fingerprint
	return result, nil
}

// resolveBuildScript returns the path to the build script to use.
// If cfg.Build.Script is set, it verifies the file exists.
// If cfg.Build.Commands are set, it creates a temporary script.
// Script takes precedence over commands.
// Returns the path, a cleanup function, and any error.
func resolveBuildScript(cfg *config.Config) (string, func(), error) {
	noop := func() {}

	// Script takes precedence over commands
	if cfg.Build.Script != "" {
		scriptPath := cfg.Build.Script
		if _, err := os.Stat(scriptPath); err != nil {
			return "", noop, fmt.Errorf("build script not found: %s", scriptPath)
		}
		return scriptPath, noop, nil
	}

	// Generate temporary script from commands
	if len(cfg.Build.Commands) > 0 {
		tmpFile, err := os.CreateTemp("", "coi-build-*.sh")
		if err != nil {
			return "", noop, fmt.Errorf("failed to create temporary build script: %w", err)
		}

		// Write shebang and commands
		content := "#!/bin/bash\nset -e\n"
		for _, cmd := range cfg.Build.Commands {
			content += cmd + "\n"
		}

		if _, err := tmpFile.WriteString(content); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return "", noop, fmt.Errorf("failed to write temporary build script: %w", err)
		}
		tmpFile.Close()

		if err := os.Chmod(tmpFile.Name(), 0o755); err != nil {
			os.Remove(tmpFile.Name())
			return "", noop, fmt.Errorf("failed to make temporary build script executable: %w", err)
		}

		cleanup := func() { os.Remove(tmpFile.Name()) }
		return tmpFile.Name(), cleanup, nil
	}

	return "", noop, fmt.Errorf("no build script or commands configured")
}

// ResolveImageName returns the effective image name using: CLI flag > config defaults.image > "coi"
func ResolveImageName(flagValue string, cfg *config.Config) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.Defaults.Image != "" {
		return cfg.Defaults.Image
	}
	return "coi"
}

// CanBuildFromConfig returns true if the config has build configuration and a custom image name
func CanBuildFromConfig(cfg *config.Config) bool {
	return cfg.Build.HasBuildConfig() &&
		cfg.Defaults.Image != "" &&
		cfg.Defaults.Image != "coi"
}

// AutoBuildIfNeeded checks if the image is missing and builds it from config if possible.
// Returns nil if the image already exists or was built successfully.
// Returns an error if the image is missing and can't be built.
func AutoBuildIfNeeded(cfg *config.Config, imageName string) error {
	// Check if image exists
	exists, err := container.ImageExists(imageName)
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}
	if exists {
		return nil
	}

	// Image doesn't exist - try to build from config
	if CanBuildFromConfig(cfg) && cfg.Defaults.Image == imageName {
		fmt.Fprintf(os.Stderr, "Image '%s' not found. Building from config...\n", imageName)
		result, err := BuildFromConfig(cfg, false, "")
		if err != nil {
			return fmt.Errorf("auto-build failed: %w", err)
		}
		if result.Built {
			fmt.Fprintf(os.Stderr, "Image '%s' built successfully (fingerprint: %s)\n", imageName, result.Fingerprint)
		}
		return nil
	}

	// No build config available - return the standard error
	// Check if user might have a build script in .coi/ that they haven't configured
	coiDir := filepath.Join(".", ".coi")
	if _, err := os.Stat(coiDir); err == nil {
		return fmt.Errorf("image '%s' not found - add a [build] section to .coi/config.toml or run 'coi build custom %s --script <script>' first", imageName, imageName)
	}
	return fmt.Errorf("image '%s' not found - run 'coi build %s' first", imageName, imageName)
}
