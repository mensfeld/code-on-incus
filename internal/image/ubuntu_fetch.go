package image

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"

	"github.com/mensfeld/code-on-incus/internal/container"
)

const (
	canonicalSimplestreamsURL = "https://cloud-images.ubuntu.com/releases/streams/v1/com.ubuntu.cloud:released:download.json"
	canonicalBaseURL          = "https://cloud-images.ubuntu.com/"
)

type canonicalStreams struct {
	Products map[string]canonicalProduct `json:"products"`
}

type canonicalProduct struct {
	Versions map[string]canonicalVersion `json:"versions"`
}

type canonicalVersion struct {
	Items map[string]canonicalItem `json:"items"`
}

type canonicalItem struct {
	Ftype  string `json:"ftype"`
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// EnsureLocalUbuntuImage downloads an Ubuntu image directly from Canonical's CDN
// and imports it into Incus as a local alias. This bypasses Incus's simplestreams
// client, which is incompatible with cloud-images.ubuntu.com. Returns the local
// alias name suitable for use with incus launch.
func EnsureLocalUbuntuImage(version string, logger func(string)) (string, error) {
	arch := canonicalArch()
	localAlias := fmt.Sprintf("coi-ubuntu-%s", version)

	exists, err := container.ImageExists(localAlias)
	if err != nil {
		return "", fmt.Errorf("failed to check image: %w", err)
	}
	if exists {
		logger(fmt.Sprintf("Using cached Ubuntu %s image (%s)", version, localAlias))
		return localAlias, nil
	}

	logger(fmt.Sprintf("Fetching Ubuntu %s image index from cloud-images.ubuntu.com...", version))
	lxdURL, squashfsURL, err := fetchUbuntuImageURLs(version, arch)
	if err != nil {
		return "", fmt.Errorf("failed to locate Ubuntu %s image: %w", version, err)
	}

	// Download metadata tarball (small — a few hundred bytes)
	lxdFile, err := downloadToTemp(lxdURL)
	if err != nil {
		return "", fmt.Errorf("failed to download Ubuntu metadata: %w", err)
	}
	defer os.Remove(lxdFile)

	// Download rootfs squashfs (large — several hundred MB)
	logger(fmt.Sprintf("Downloading Ubuntu %s rootfs (~450 MB, this may take a few minutes)...", version))
	squashfsFile, err := downloadToTemp(squashfsURL)
	if err != nil {
		return "", fmt.Errorf("failed to download Ubuntu rootfs: %w", err)
	}
	defer os.Remove(squashfsFile)

	logger(fmt.Sprintf("Importing Ubuntu %s into Incus as '%s'...", version, localAlias))
	if err := container.ImportImage(lxdFile, squashfsFile, localAlias); err != nil {
		return "", fmt.Errorf("failed to import Ubuntu image: %w", err)
	}

	logger(fmt.Sprintf("Ubuntu %s image ready as local alias '%s'", version, localAlias))
	return localAlias, nil
}

func fetchUbuntuImageURLs(version, arch string) (lxdURL, squashfsURL string, err error) {
	resp, err := http.Get(canonicalSimplestreamsURL)
	if err != nil {
		return "", "", fmt.Errorf("could not reach cloud-images.ubuntu.com: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("simplestreams returned HTTP %d", resp.StatusCode)
	}

	var streams canonicalStreams
	if err := json.NewDecoder(resp.Body).Decode(&streams); err != nil {
		return "", "", fmt.Errorf("failed to parse simplestreams: %w", err)
	}

	productKey := fmt.Sprintf("com.ubuntu.cloud:server:%s:%s", version, arch)
	product, ok := streams.Products[productKey]
	if !ok {
		return "", "", fmt.Errorf("Ubuntu %s (%s) not found in Canonical simplestreams", version, arch)
	}

	// Pick the latest version by sorted key
	keys := make([]string, 0, len(product.Versions))
	for k := range product.Versions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	latest := product.Versions[keys[len(keys)-1]]

	for _, item := range latest.Items {
		switch item.Ftype {
		case "lxd.tar.xz":
			lxdURL = canonicalBaseURL + item.Path
		case "squashfs":
			squashfsURL = canonicalBaseURL + item.Path
		}
	}

	if lxdURL == "" || squashfsURL == "" {
		return "", "", fmt.Errorf("Ubuntu %s (%s): lxd.tar.xz or squashfs not found in latest release", version, arch)
	}

	return lxdURL, squashfsURL, nil
}

func downloadToTemp(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp("", "coi-ubuntu-*.tmp")
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write failed: %w", err)
	}
	tmp.Close()

	return tmp.Name(), nil
}

// canonicalArch maps Go's GOARCH to the architecture name used in Canonical's
// simplestreams product keys.
func canonicalArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "386":
		return "i386"
	case "ppc64le":
		return "ppc64el"
	case "s390x":
		return "s390x"
	default:
		return "amd64"
	}
}
