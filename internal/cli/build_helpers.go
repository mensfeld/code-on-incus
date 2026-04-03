package cli

import (
	"fmt"
	"os"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/image"
)

// resolveBuildScript returns the path to the build script to use.
// If build.Script is set, it verifies the file exists.
// If build.Commands are set, it creates a temporary script.
// Script takes precedence over commands.
// Returns the path, a cleanup function, and any error.
func resolveBuildScript(build *config.BuildConfig) (string, func(), error) {
	noop := func() {}

	// Script takes precedence over commands
	if build.Script != "" {
		scriptPath := build.Script
		if _, err := os.Stat(scriptPath); err != nil {
			return "", noop, fmt.Errorf("build script not found: %s", scriptPath)
		}
		return scriptPath, noop, nil
	}

	// Generate temporary script from commands
	if len(build.Commands) > 0 {
		tmpFile, err := os.CreateTemp("", "coi-build-*.sh")
		if err != nil {
			return "", noop, fmt.Errorf("failed to create temporary build script: %w", err)
		}

		// Write shebang and commands
		content := "#!/bin/bash\nset -e\n"
		for _, cmd := range build.Commands {
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

// ResolveImageName returns the effective image name using: CLI flag > config defaults.image > CoiAlias
func ResolveImageName(flagValue string, cfg *config.Config) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.Defaults.Image != "" {
		return cfg.Defaults.Image
	}
	return image.CoiAlias
}

// AutoBuildIfNeeded checks if the image is missing and returns an error with instructions.
// Returns nil if the image already exists.
func AutoBuildIfNeeded(cfg *config.Config, imageName string) error {
	// Check if image exists
	exists, err := container.ImageExists(imageName)
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}
	if exists {
		return nil
	}

	// Image doesn't exist — return error with instructions
	if imageName == image.CoiAlias {
		return fmt.Errorf("image '%s' not found — run 'coi build' first", imageName)
	}
	return fmt.Errorf("image '%s' not found — run 'coi build --profile <profile>' first", imageName)
}
