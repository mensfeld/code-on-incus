package image

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mensfeld/claude-on-incus/internal/container"
)

const (
	BaseImage      = "images:ubuntu/22.04"
	CoiAlias       = "coi"
	BuildContainer = "coi-build"
)

// BuildOptions contains options for building an image
type BuildOptions struct {
	ImageType   string // "coi" or "custom"
	AliasName   string
	Description string
	BaseImage   string
	Force       bool
	BuildScript string // For custom images
	Logger      func(string)
}

// BuildResult contains the result of an image build
type BuildResult struct {
	Success      bool
	Skipped      bool
	VersionAlias string
	Fingerprint  string
	Error        error
}

// Builder handles Incus image building
type Builder struct {
	opts BuildOptions
	mgr  *container.Manager
}

// NewBuilder creates a new Builder instance
func NewBuilder(opts BuildOptions) *Builder {
	if opts.Logger == nil {
		opts.Logger = func(msg string) {
			fmt.Fprintf(os.Stderr, "[build] %s\n", msg)
		}
	}

	return &Builder{
		opts: opts,
		mgr:  container.NewManager(BuildContainer),
	}
}

// Build executes the image build process
func (b *Builder) Build() *BuildResult {
	result := &BuildResult{}

	// Check if image already exists
	if !b.opts.Force {
		exists, err := container.ImageExists(b.opts.AliasName)
		if err != nil {
			result.Error = fmt.Errorf("failed to check image: %w", err)
			return result
		}
		if exists {
			b.opts.Logger(fmt.Sprintf("Image '%s' already exists. Use --force to rebuild.", b.opts.AliasName))
			result.Skipped = true
			return result
		}
	}

	// Generate version alias
	result.VersionAlias = fmt.Sprintf("%s-%s", b.opts.AliasName, time.Now().Format("20060102-150405"))
	b.opts.Logger(fmt.Sprintf("Building Incus image '%s'...", result.VersionAlias))

	// Execute build steps
	if err := b.launchBuildContainer(); err != nil {
		result.Error = err
		b.cleanup()
		return result
	}

	if err := b.waitForNetwork(); err != nil {
		result.Error = err
		b.cleanup()
		return result
	}

	// Run build steps (implemented by specific image types)
	if err := b.runBuildSteps(); err != nil {
		result.Error = err
		b.cleanup()
		return result
	}

	// Create image
	fingerprint, err := b.createImage(result.VersionAlias)
	if err != nil {
		result.Error = err
		b.cleanup()
		return result
	}
	result.Fingerprint = fingerprint

	// Cleanup build container
	b.cleanup()

	// Update alias
	if err := b.updateAlias(result.VersionAlias, b.opts.AliasName); err != nil {
		result.Error = err
		return result
	}

	b.opts.Logger(fmt.Sprintf("Image '%s' built successfully! (version: %s)", b.opts.AliasName, result.VersionAlias))
	result.Success = true
	return result
}

// launchBuildContainer launches the build container from base image
func (b *Builder) launchBuildContainer() error {
	b.opts.Logger(fmt.Sprintf("Launching build container from %s...", b.opts.BaseImage))

	if err := b.mgr.Launch(b.opts.BaseImage, false); err != nil {
		return fmt.Errorf("failed to launch build container: %w", err)
	}

	// Wait for container to start
	time.Sleep(3 * time.Second)
	return nil
}

// waitForNetwork waits for network connectivity in container
func (b *Builder) waitForNetwork() error {
	b.opts.Logger("Waiting for network...")

	dnsFixed := false
	maxAttempts := 180 // 3 minutes - increased for slower CI environments
	for i := 0; i < maxAttempts; i++ {
		// Try TCP connection (works even when ICMP/ping is blocked in CI)
		// Using /dev/tcp bash feature to test HTTP connectivity without curl
		_, err := b.mgr.ExecCommand("timeout 3 bash -c 'exec 3<>/dev/tcp/archive.ubuntu.com/80 && echo connected >&3' 2>/dev/null", container.ExecCommandOptions{
			Capture: true,
		})
		if err == nil {
			b.opts.Logger(fmt.Sprintf("Network ready (HTTP) after %d seconds", i+1))
			if dnsFixed {
				b.logDNSFixWarning()
			}
			return nil
		}

		// Fallback to ping (works in most environments but not GitHub Actions)
		_, pingErr := b.mgr.ExecCommand("ping -c 1 -W 2 archive.ubuntu.com", container.ExecCommandOptions{
			Capture: true,
		})
		if pingErr == nil {
			b.opts.Logger(fmt.Sprintf("Network ready (ICMP) after %d seconds", i+1))
			if dnsFixed {
				b.logDNSFixWarning()
			}
			return nil
		}

		// After 10 seconds, check if this is a DNS issue and auto-fix
		if i == 10 && !dnsFixed {
			if b.tryFixDNS() {
				dnsFixed = true
				// Give the new DNS config a moment to take effect
				time.Sleep(2 * time.Second)
				continue
			}
		}

		// Log progress every 30 seconds with diagnostic info
		if i > 0 && i%30 == 0 {
			b.opts.Logger(fmt.Sprintf("Still waiting for network... (%d/%d seconds)", i, maxAttempts))

			// Get IP address info for debugging
			ipOutput, _ := b.mgr.ExecCommand("ip addr show eth0 | grep inet || ip addr show", container.ExecCommandOptions{
				Capture: true,
			})
			b.opts.Logger(fmt.Sprintf("Container IP info: %s", ipOutput))

			// Check if DNS resolution works
			dnsOutput, _ := b.mgr.ExecCommand("cat /etc/resolv.conf", container.ExecCommandOptions{
				Capture: true,
			})
			b.opts.Logger(fmt.Sprintf("DNS config: %s", dnsOutput))
		}

		time.Sleep(1 * time.Second)
	}

	// Final diagnostic before failing
	b.opts.Logger("Network timeout - gathering diagnostic info...")
	ipOutput, _ := b.mgr.ExecCommand("ip addr show", container.ExecCommandOptions{Capture: true})
	b.opts.Logger(fmt.Sprintf("Final IP addresses:\n%s", ipOutput))

	routeOutput, _ := b.mgr.ExecCommand("ip route show", container.ExecCommandOptions{Capture: true})
	b.opts.Logger(fmt.Sprintf("Final routes:\n%s", routeOutput))

	return fmt.Errorf("network timeout after %d seconds", maxAttempts)
}

// tryFixDNS attempts to automatically fix DNS misconfiguration
// Returns true if a fix was applied
func (b *Builder) tryFixDNS() bool {
	// Test if we can reach an IP directly (Google DNS on port 53)
	_, ipErr := b.mgr.ExecCommand("timeout 3 bash -c 'exec 3<>/dev/tcp/8.8.8.8/53' 2>/dev/null", container.ExecCommandOptions{
		Capture: true,
	})

	if ipErr != nil {
		// Can't reach external IPs - this is a general network issue, not DNS-specific
		return false
	}

	// We can reach IPs but not hostnames - this is a DNS issue
	// Check for the common systemd-resolved stub resolver issue (127.0.0.53)
	resolvConf, _ := b.mgr.ExecCommand("cat /etc/resolv.conf 2>/dev/null", container.ExecCommandOptions{Capture: true})

	hasStubResolver := strings.Contains(resolvConf, "127.0.0.53")
	hasEmptyDNS := strings.TrimSpace(resolvConf) == "" || !strings.Contains(resolvConf, "nameserver")

	if hasStubResolver || hasEmptyDNS {
		b.opts.Logger("Detected DNS misconfiguration, applying automatic fix...")

		// Inject working DNS servers
		// First, remove resolv.conf if it's a symlink (common with systemd-resolved)
		_, _ = b.mgr.ExecCommand("rm -f /etc/resolv.conf 2>/dev/null", container.ExecCommandOptions{Capture: true})

		// Write a working resolv.conf with public DNS servers
		_, err := b.mgr.ExecCommand(`cat > /etc/resolv.conf << 'EOF'
# Auto-configured by coi build due to DNS misconfiguration
nameserver 8.8.8.8
nameserver 8.8.4.4
nameserver 1.1.1.1
EOF`, container.ExecCommandOptions{Capture: true})
		if err != nil {
			b.opts.Logger(fmt.Sprintf("Failed to fix DNS: %v", err))
			return false
		}

		b.opts.Logger("DNS configuration fixed (using 8.8.8.8, 8.8.4.4, 1.1.1.1)")
		return true
	}

	return false
}

// logDNSFixWarning logs a warning about the DNS misconfiguration and how to permanently fix it
func (b *Builder) logDNSFixWarning() {
	b.opts.Logger("")
	b.opts.Logger("WARNING: DNS misconfiguration detected (systemd-resolved stub at 127.0.0.53).")
	b.opts.Logger("Auto-fixed for this build. The resulting image uses static DNS (8.8.8.8, 8.8.4.4, 1.1.1.1).")
	b.opts.Logger("To fix your Incus network for other containers, run:")
	b.opts.Logger("  incus network set incusbr0 dns.mode managed")
	b.opts.Logger("")
}

// runBuildSteps executes the build steps based on image type
func (b *Builder) runBuildSteps() error {
	switch b.opts.ImageType {
	case "coi":
		return b.buildCoi()
	case "custom":
		return b.buildCustom()
	default:
		return fmt.Errorf("unknown image type: %s", b.opts.ImageType)
	}
}

// buildCoi implements coi image build steps using external script
func (b *Builder) buildCoi() error {
	return b.runBuildScript("scripts/build/coi.sh")
}

// runBuildScript executes a build script from the scripts directory
func (b *Builder) runBuildScript(scriptPath string) error {
	// Find script - try relative to cwd first, then relative to executable
	if _, err := os.Stat(scriptPath); err != nil {
		// Try to find relative to executable
		execPath, _ := os.Executable()
		if execPath != "" {
			altPath := fmt.Sprintf("%s/../%s", execPath, scriptPath)
			if _, err := os.Stat(altPath); err == nil {
				scriptPath = altPath
			}
		}
	}

	// Verify script exists
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("build script not found: %s (run from project root)", scriptPath)
	}

	b.opts.Logger(fmt.Sprintf("Using build script: %s", scriptPath))

	// Push dummy to /tmp (required for build scripts)
	dummyPath := "testdata/dummy/dummy"
	if _, err := os.Stat(dummyPath); err != nil {
		return fmt.Errorf("dummy not found at %s (run from project root)", dummyPath)
	}
	b.opts.Logger("Pushing dummy to container...")
	if err := b.mgr.PushFile(dummyPath, "/tmp/dummy"); err != nil {
		return fmt.Errorf("failed to push dummy: %w", err)
	}

	// Push build script to container
	b.opts.Logger("Pushing build script to container...")
	if err := b.mgr.PushFile(scriptPath, "/tmp/build.sh"); err != nil {
		return fmt.Errorf("failed to push build script: %w", err)
	}

	// Make executable
	if _, err := b.mgr.ExecCommand("chmod +x /tmp/build.sh", container.ExecCommandOptions{}); err != nil {
		return fmt.Errorf("failed to chmod build script: %w", err)
	}

	// Execute script
	b.opts.Logger("Executing build script...")
	if _, err := b.mgr.ExecCommand("/tmp/build.sh", container.ExecCommandOptions{Capture: false}); err != nil {
		return fmt.Errorf("build script failed: %w", err)
	}

	b.opts.Logger("Build script completed successfully")
	return nil
}

// buildCustom runs a custom build script
func (b *Builder) buildCustom() error {
	if b.opts.BuildScript == "" {
		return fmt.Errorf("build script required for custom images")
	}

	b.opts.Logger("Running custom build script...")

	// Read script content from file
	scriptBytes, err := os.ReadFile(b.opts.BuildScript)
	if err != nil {
		return fmt.Errorf("failed to read build script: %w", err)
	}

	// Push dummy to /tmp (required for test build scripts)
	dummyPath := "testdata/dummy/dummy"
	if _, err := os.Stat(dummyPath); err == nil {
		// Only push if dummy exists (optional for custom builds)
		b.opts.Logger("Pushing dummy to container...")
		if err := b.mgr.PushFile(dummyPath, "/tmp/dummy"); err != nil {
			return fmt.Errorf("failed to push dummy: %w", err)
		}
	}

	// Push script to container
	b.opts.Logger(fmt.Sprintf("Uploading build script from %s...", b.opts.BuildScript))
	if err := b.mgr.PushFile(b.opts.BuildScript, "/tmp/build.sh"); err != nil {
		return fmt.Errorf("failed to push build script: %w", err)
	}

	// Make executable
	if _, err := b.mgr.ExecCommand("chmod +x /tmp/build.sh", container.ExecCommandOptions{}); err != nil {
		return err
	}

	// Execute script as root
	b.opts.Logger(fmt.Sprintf("Executing build script (%d bytes)...", len(scriptBytes)))
	if _, err := b.mgr.ExecCommand("/tmp/build.sh", container.ExecCommandOptions{Capture: false}); err != nil {
		return fmt.Errorf("custom build script failed: %w", err)
	}

	b.opts.Logger("Custom build script completed successfully")
	return nil
}

// createImage publishes the container as an image
func (b *Builder) createImage(versionAlias string) (string, error) {
	b.opts.Logger("Stopping container for imaging...")
	if err := b.mgr.Stop(true); err != nil {
		return "", fmt.Errorf("failed to stop container: %w", err)
	}

	b.opts.Logger(fmt.Sprintf("Creating image '%s'...", versionAlias))

	// Publish container as image
	_, err := container.IncusOutput(
		"publish", BuildContainer,
		"--alias", versionAlias,
		fmt.Sprintf("description=%s", b.opts.Description),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create image: %w", err)
	}

	// Get fingerprint
	fingerprint, err := getImageFingerprint(versionAlias)
	if err != nil {
		return "", err
	}

	return fingerprint, nil
}

// cleanup removes the build container
func (b *Builder) cleanup() {
	b.opts.Logger("Cleaning up build container...")
	// Only stop if container is running (avoids spurious error messages)
	if running, _ := b.mgr.Running(); running {
		_ = b.mgr.Stop(true) // Best effort cleanup
	}
	_ = b.mgr.Delete(true) // Best effort cleanup
}

// updateAlias updates the main alias to point to the new image
func (b *Builder) updateAlias(versionAlias, mainAlias string) error {
	b.opts.Logger(fmt.Sprintf("Updating alias '%s' to point to new image...", mainAlias))

	fingerprint, err := getImageFingerprint(versionAlias)
	if err != nil {
		return err
	}

	// Delete old alias if it exists
	if exists, _ := container.ImageExists(mainAlias); exists {
		_ = container.IncusExec("image", "alias", "delete", mainAlias) // Best effort
	}

	// Create new alias
	if err := container.IncusExec("image", "alias", "create", mainAlias, fingerprint); err != nil {
		return fmt.Errorf("failed to create alias: %w", err)
	}

	return nil
}

// getImageFingerprint gets the fingerprint of an image by alias
func getImageFingerprint(alias string) (string, error) {
	output, err := container.IncusOutput("image", "list", alias, "--project", "default", "--format=json")
	if err != nil {
		return "", err
	}

	var images []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &images); err != nil {
		return "", err
	}

	for _, img := range images {
		if aliases, ok := img["aliases"].([]interface{}); ok {
			for _, a := range aliases {
				if aliasMap, ok := a.(map[string]interface{}); ok {
					if name, ok := aliasMap["name"].(string); ok && name == alias {
						if fingerprint, ok := img["fingerprint"].(string); ok {
							return fingerprint, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("image not found: %s", alias)
}
