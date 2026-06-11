package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/image"
	"github.com/spf13/cobra"
)

var buildForce bool

var buildCompression string

var buildAll bool

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build Incus image for AI coding sessions",
	Long: `Build an Incus image using a profile's build configuration.

By default, builds the "coi-default" image using the built-in default profile.
Use --profile to build from a custom profile's [container.build] section.
Use --all to build images for all profiles that have a build configuration.

Examples:
  coi build
  coi build --force
  coi build --profile rust-dev
  coi build --all
  coi build --all --force
`,
	Args: cobra.NoArgs,
	RunE: buildCommand,
}

func init() {
	buildCmd.Flags().BoolVarP(&buildForce, "force", "f", false, "Force rebuild even if image exists")
	buildCmd.Flags().StringVar(&buildCompression, "compression", "", "Compression algorithm (e.g., none, gzip, xz; see Incus docs for all options)")
	buildCmd.Flags().BoolVar(&buildAll, "all", false, "Build images for all profiles that have a build configuration")
}

func buildCommand(cmd *cobra.Command, args []string) error {
	// Check if Incus is available
	if !container.Available() {
		return container.IncusNotAvailableError()
	}

	// Check minimum Incus version
	if err := container.CheckMinimumVersion(); err != nil {
		return err
	}

	// Warn if kernel is too old (non-blocking)
	if warning := container.CheckKernelVersion(); warning != "" {
		fmt.Fprintf(os.Stderr, "%s\n", warning)
	}

	if buildAll {
		return buildAllProfiles()
	}

	// Determine which profile to use
	profileName := app.profile // from --profile flag
	if profileName == "" {
		profileName = "default"
	}

	p := app.cfg.GetProfile(profileName)
	if p == nil {
		return fmt.Errorf("profile '%s' not found", profileName)
	}

	imageName := p.Container.Image
	if imageName == "" {
		imageName = image.CoiAlias
	}

	// Resolve the effective build container storage pool using the same
	// precedence as ApplyProfile: start with the global container storage
	// pool and let the selected profile override it only when it sets a
	// non-empty storage_pool value. Validate before any container work so a
	// missing pool fails loud and early.
	buildPool := app.cfg.Container.StoragePool
	if p.Container.StoragePool != "" {
		buildPool = p.Container.StoragePool
	}
	if err := container.ValidateStoragePool(buildPool); err != nil {
		return err
	}

	// For coi-default image: always use the embedded build script
	if imageName == image.CoiAlias {
		// Allow overriding the base image via [container.build] base in profile/config.
		coiBaseImage := image.BaseImage
		if p.Container.Build.Base != "" {
			coiBaseImage = p.Container.Build.Base
		}

		opts := image.BuildOptions{
			Force:       buildForce,
			ImageType:   "coi",
			BaseImage:   coiBaseImage,
			AliasName:   image.CoiAlias,
			Description: "coi image (Docker + build tools + Claude CLI + GitHub CLI)",
			Compression: buildCompression,
			StoragePool: buildPool,
			Logger: func(msg string) {
				fmt.Fprintf(os.Stderr, "%s\n", msg)
			},
		}

		fmt.Fprintf(os.Stderr, "Building image '%s' from profile '%s'...\n", imageName, profileName)
		builder := image.NewBuilder(opts)
		result := builder.Build()

		if result.Error != nil {
			return fmt.Errorf("build failed: %w", result.Error)
		}

		if result.Skipped {
			fmt.Fprintf(os.Stderr, "\nImage '%s' already exists. Use --force to rebuild.\n", opts.AliasName)
			return nil
		}

		fmt.Fprintf(os.Stderr, "\nImage '%s' built successfully!\n", opts.AliasName)
		fmt.Fprintf(os.Stderr, "  Version: %s\n", result.VersionAlias)
		fmt.Fprintf(os.Stderr, "  Fingerprint: %s\n", result.Fingerprint)
		return nil
	}

	// Custom profile build: requires [container.build] section
	if !p.Container.Build.HasBuildConfig() {
		return fmt.Errorf("profile '%s' has no [container.build] section — add a build script or commands to the profile", profileName)
	}

	// Resolve build script
	scriptPath, cleanup, err := resolveBuildScript(&p.Container.Build)
	if err != nil {
		return err
	}
	defer cleanup()

	// Determine base image
	baseImage := p.Container.Build.Base
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
		StoragePool: buildPool,
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

// buildAllProfiles builds images for all profiles that have a build configuration.
// The "default" profile (coi-default) is always built first; other profiles are
// processed in alphabetical order. Profiles without a [container.build] section
// are silently skipped. Errors are collected and reported together at the end.
func buildAllProfiles() error {
	// Collect profile names: "default" first, then the rest alphabetically.
	others := make([]string, 0, len(app.cfg.Profiles))
	for name := range app.cfg.Profiles {
		if name != "default" {
			others = append(others, name)
		}
	}
	sort.Strings(others)
	names := append([]string{"default"}, others...)

	var built, skipped, errored int
	var buildErrors []string

	for _, profileName := range names {
		p := app.cfg.GetProfile(profileName)
		if p == nil {
			continue
		}

		imageName := p.Container.Image
		if imageName == "" {
			imageName = image.CoiAlias
		}

		// Skip profiles that neither map to coi-default nor define a build config.
		if imageName != image.CoiAlias && !p.Container.Build.HasBuildConfig() {
			continue
		}

		buildPool := app.cfg.Container.StoragePool
		if p.Container.StoragePool != "" {
			buildPool = p.Container.StoragePool
		}
		if err := container.ValidateStoragePool(buildPool); err != nil {
			buildErrors = append(buildErrors, fmt.Sprintf("  profile '%s': %v", profileName, err))
			errored++
			continue
		}

		fmt.Fprintf(os.Stderr, "\n[%s] Building image '%s'...\n", profileName, imageName)

		if imageName == image.CoiAlias {
			coiBaseImage := image.BaseImage
			if p.Container.Build.Base != "" {
				coiBaseImage = p.Container.Build.Base
			}
			opts := image.BuildOptions{
				Force:       buildForce,
				ImageType:   "coi",
				BaseImage:   coiBaseImage,
				AliasName:   image.CoiAlias,
				Description: "coi image (Docker + build tools + Claude CLI + GitHub CLI)",
				Compression: buildCompression,
				StoragePool: buildPool,
				Logger:      func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) },
			}
			result := image.NewBuilder(opts).Build()
			if result.Error != nil {
				buildErrors = append(buildErrors, fmt.Sprintf("  profile '%s': %v", profileName, result.Error))
				errored++
				continue
			}
			if result.Skipped {
				fmt.Fprintf(os.Stderr, "[%s] Image '%s' already exists (use --force to rebuild).\n", profileName, imageName)
				skipped++
				continue
			}
			fmt.Fprintf(os.Stderr, "[%s] Image '%s' built successfully.\n", profileName, imageName)
			built++
		} else {
			scriptPath, cleanup, err := resolveBuildScript(&p.Container.Build)
			if err != nil {
				buildErrors = append(buildErrors, fmt.Sprintf("  profile '%s': %v", profileName, err))
				errored++
				continue
			}
			defer cleanup()

			baseImage := p.Container.Build.Base
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
				StoragePool: buildPool,
				Logger:      func(msg string) { fmt.Fprintf(os.Stderr, "%s\n", msg) },
			}
			result := image.NewBuilder(opts).Build()
			if result.Error != nil {
				buildErrors = append(buildErrors, fmt.Sprintf("  profile '%s': %v", profileName, result.Error))
				errored++
				continue
			}
			if result.Skipped {
				fmt.Fprintf(os.Stderr, "[%s] Image '%s' already exists (use --force to rebuild).\n", profileName, imageName)
				skipped++
				continue
			}
			fmt.Fprintf(os.Stderr, "[%s] Image '%s' built successfully.\n", profileName, imageName)
			built++
		}
	}

	fmt.Fprintf(os.Stderr, "\n--- Build summary ---\n")
	fmt.Fprintf(os.Stderr, "  Built:   %d\n", built)
	fmt.Fprintf(os.Stderr, "  Skipped: %d\n", skipped)
	fmt.Fprintf(os.Stderr, "  Failed:  %d\n", errored)

	if len(buildErrors) > 0 {
		return fmt.Errorf("%d build(s) failed:\n%s", errored, strings.Join(buildErrors, "\n"))
	}
	return nil
}
