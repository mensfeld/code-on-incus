package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	updateCheck   bool
	updateForce   bool
	updateVersion string // hidden flag: skip GitHub query when re-execing under sudo
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update coi to the latest release",
	Long: `Download and install the latest coi release from GitHub.

Queries the GitHub releases API, compares versions, downloads the new binary,
verifies its SHA256 checksum, and replaces the current binary in-place.

If the binary directory is not writable, suggests re-running with sudo.

Examples:
  coi update          # Download and install latest release
  coi update --check  # Only check for updates, don't install
  coi update --force  # Skip confirmation prompt
`,
	RunE: updateCommand,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "Only check for updates, don't install")
	updateCmd.Flags().BoolVarP(&updateForce, "force", "f", false, "Skip confirmation prompt")
	updateCmd.Flags().StringVar(&updateVersion, "version", "", "Install specific version (skips GitHub query)")
	_ = updateCmd.Flags().MarkHidden("version")
}

// githubRelease represents the relevant fields from the GitHub releases API
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset represents a release asset
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func updateCommand(cmd *cobra.Command, args []string) error {
	currentVersion := Version
	isDev := currentVersion == "dev"

	// Determine latest version
	var release *githubRelease
	var latestTag string

	if updateVersion != "" {
		// Version passed via hidden flag (sudo re-exec) — skip GitHub query
		latestTag = updateVersion
	} else {
		// Query GitHub API
		var err error
		release, err = fetchLatestRelease()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		latestTag = release.TagName
	}

	latestVersion := strings.TrimPrefix(latestTag, "v")

	// Show version comparison
	if isDev {
		fmt.Printf("Current version: %s (development build)\n", currentVersion)
		fmt.Printf("Latest release:  v%s\n", latestVersion)
		if !updateForce && !updateCheck {
			fmt.Println("\nWarning: Development builds cannot be version-compared.")
			fmt.Println("Use --force to install the latest release anyway.")
			return nil
		}
	} else {
		fmt.Printf("Current version: v%s\n", currentVersion)
		fmt.Printf("Latest release:  v%s\n", latestVersion)
	}

	// Compare versions
	if !isDev {
		cmp := compareVersions(currentVersion, latestVersion)
		if cmp >= 0 {
			fmt.Println("\nYou are already on the latest version.")
			return nil
		}
	}

	// Check-only mode
	if updateCheck {
		if isDev {
			fmt.Println("\nUpdate available (dev → v" + latestVersion + ")")
		} else {
			fmt.Println("\nUpdate available: v" + currentVersion + " → v" + latestVersion)
		}
		return nil
	}

	// Confirm unless --force
	if !updateForce {
		fmt.Print("\nInstall update? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Resolve current binary path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine binary path: %w", err)
	}
	binaryPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}
	binaryDir := filepath.Dir(binaryPath)

	// Check if directory is writable
	if !isDirWritable(binaryDir) {
		fmt.Printf("\nPermission denied: %s is not writable.\n", binaryDir)
		fmt.Printf("Re-run with: sudo %s update --force --version %s\n", os.Args[0], latestTag)
		return fmt.Errorf("insufficient permissions to update binary in %s", binaryDir)
	}

	// If we don't have release info yet (sudo re-exec path), fetch it
	if release == nil {
		release, err = fetchLatestRelease()
		if err != nil {
			return fmt.Errorf("failed to fetch release info: %w", err)
		}
	}

	// Determine expected asset name
	binaryName := fmt.Sprintf("coi-%s-%s", runtime.GOOS, runtime.GOARCH)

	// Find binary asset URL
	binaryURL := ""
	checksumURL := ""
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			binaryURL = asset.BrowserDownloadURL
		}
		if asset.Name == "checksums.txt" {
			checksumURL = asset.BrowserDownloadURL
		}
	}

	if binaryURL == "" {
		return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, latestTag)
	}

	// Download binary
	fmt.Printf("Downloading %s...\n", binaryName)
	binaryData, err := downloadFile(binaryURL)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}

	// Verify checksum if available
	if checksumURL != "" {
		fmt.Println("Verifying checksum...")
		checksumData, err := downloadFile(checksumURL)
		if err != nil {
			return fmt.Errorf("failed to download checksums: %w", err)
		}

		if err := verifyChecksum(binaryData, checksumData, binaryName); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		fmt.Println("Checksum verified.")
	} else {
		fmt.Println("Warning: No checksums.txt found in release, skipping verification.")
	}

	// Atomic replace: write to temp file in same directory, then rename
	tmpFile, err := os.CreateTemp(binaryDir, ".coi-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on any error
	defer func() {
		// Only remove if it still exists (rename succeeded = no longer at tmpPath)
		if _, err := os.Stat(tmpPath); err == nil {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(binaryData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Preserve original file permissions
	origInfo, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to stat original binary: %w", err)
	}
	if err := os.Chmod(tmpPath, origInfo.Mode()); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Rename temp file to binary path (atomic on same filesystem)
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("\nSuccessfully updated to v%s\n", latestVersion)
	fmt.Printf("Binary: %s\n", binaryPath)
	return nil
}

// fetchLatestRelease queries the GitHub API for the latest release
func fetchLatestRelease() (*githubRelease, error) {
	url := "https://api.github.com/repos/mensfeld/code-on-incus/releases/latest"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if release.TagName == "" {
		return nil, fmt.Errorf("no tag_name in release response")
	}

	return &release, nil
}

// downloadFile downloads a URL and returns the response body as bytes
func downloadFile(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

// verifyChecksum verifies the SHA256 checksum of data against a checksums.txt file
func verifyChecksum(data []byte, checksumFile []byte, binaryName string) error {
	hash := sha256.Sum256(data)
	actualHash := hex.EncodeToString(hash[:])

	// Parse checksums.txt — format: "<hash>  <filename>" or "<hash> <filename>"
	lines := strings.Split(string(checksumFile), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split on whitespace (may be "hash  filename" or "hash filename")
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		if parts[1] == binaryName {
			expectedHash := parts[0]
			if actualHash != expectedHash {
				return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
			}
			return nil
		}
	}

	return fmt.Errorf("no checksum found for %s in checksums.txt", binaryName)
}

// compareVersions compares two semver strings (without "v" prefix)
// Returns -1 if a < b, 0 if a == b, 1 if a > b
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	// Pad to same length
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for len(aParts) < maxLen {
		aParts = append(aParts, "0")
	}
	for len(bParts) < maxLen {
		bParts = append(bParts, "0")
	}

	for i := 0; i < maxLen; i++ {
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])

		// If both parse as numbers, compare numerically
		if aErr == nil && bErr == nil {
			if aNum < bNum {
				return -1
			}
			if aNum > bNum {
				return 1
			}
			continue
		}

		// Fall back to string comparison
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}

	return 0
}

// isDirWritable checks if a directory is writable by attempting to create a temp file
func isDirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".coi-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
