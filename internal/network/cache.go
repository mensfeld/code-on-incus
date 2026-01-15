package network

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// IPCache stores resolved domain IPs with timestamp
type IPCache struct {
	Domains    map[string][]string `json:"domains"`
	LastUpdate time.Time           `json:"last_update"`
}

// CacheManager handles persistent IP cache storage
type CacheManager struct {
	cacheDir string
}

// NewCacheManager creates a new cache manager
func NewCacheManager(baseDir string) *CacheManager {
	return &CacheManager{
		cacheDir: filepath.Join(baseDir, ".coi", "network-cache"),
	}
}

// Load reads the IP cache for a container
func (c *CacheManager) Load(containerName string) (*IPCache, error) {
	cachePath := filepath.Join(c.cacheDir, fmt.Sprintf("%s.json", containerName))

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty cache if file doesn't exist
			return &IPCache{
				Domains:    make(map[string][]string),
				LastUpdate: time.Time{},
			}, nil
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache IPCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse cache file: %w", err)
	}

	// Initialize domains map if nil
	if cache.Domains == nil {
		cache.Domains = make(map[string][]string)
	}

	return &cache, nil
}

// Save writes the IP cache for a container
func (c *CacheManager) Save(containerName string, cache *IPCache) error {
	// Ensure cache directory exists
	if err := os.MkdirAll(c.cacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cachePath := filepath.Join(c.cacheDir, fmt.Sprintf("%s.json", containerName))

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// Delete removes the cache file for a container
func (c *CacheManager) Delete(containerName string) error {
	cachePath := filepath.Join(c.cacheDir, fmt.Sprintf("%s.json", containerName))

	if err := os.Remove(cachePath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	return nil
}
