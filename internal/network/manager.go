package network

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// Manager provides high-level network isolation management for containers
type Manager struct {
	config        *config.NetworkConfig
	acl           *ACLManager
	resolver      *Resolver
	cacheManager  *CacheManager
	containerName string
	aclName       string

	// Refresher lifecycle (for allowlist mode)
	refreshCtx    context.Context
	refreshCancel context.CancelFunc
}

// NewManager creates a new network manager with the specified configuration
func NewManager(cfg *config.NetworkConfig) *Manager {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}

	return &Manager{
		config:       cfg,
		acl:          &ACLManager{},
		cacheManager: NewCacheManager(homeDir),
	}
}

// SetupForContainer configures network isolation for a container
func (m *Manager) SetupForContainer(ctx context.Context, containerName string) error {
	m.containerName = containerName

	// Handle different network modes
	switch m.config.Mode {
	case config.NetworkModeOpen:
		log.Println("Network mode: open (no restrictions)")
		return nil

	case config.NetworkModeRestricted:
		return m.setupRestricted(ctx, containerName)

	case config.NetworkModeAllowlist:
		return m.setupAllowlist(ctx, containerName)

	default:
		return fmt.Errorf("unknown network mode: %s", m.config.Mode)
	}
}

// setupRestricted configures restricted mode (existing behavior)
func (m *Manager) setupRestricted(ctx context.Context, containerName string) error {
	log.Println("Network mode: restricted (blocking local/internal networks)")

	// Generate ACL name
	m.aclName = fmt.Sprintf("coi-%s-restricted", containerName)

	// 1. Create ACL with block rules
	if err := m.acl.Create(m.aclName, m.config); err != nil {
		return fmt.Errorf("failed to create network ACL: %w", err)
	}

	// 2. Apply ACL to container
	if err := m.acl.ApplyToContainer(containerName, m.aclName); err != nil {
		// If applying fails, clean up the ACL
		_ = m.acl.Delete(m.aclName)
		return fmt.Errorf("failed to apply network ACL: %w", err)
	}

	log.Printf("Network ACL '%s' applied successfully", m.aclName)

	// Log what is blocked
	if m.config.BlockPrivateNetworks {
		log.Println("  ✓ Blocking private networks (RFC1918)")
	}
	if m.config.BlockMetadataEndpoint {
		log.Println("  ✓ Blocking cloud metadata endpoints")
	}

	return nil
}

// setupAllowlist configures allowlist mode with DNS resolution and refresh
func (m *Manager) setupAllowlist(ctx context.Context, containerName string) error {
	log.Println("Network mode: allowlist (domain-based filtering)")
	m.aclName = fmt.Sprintf("coi-%s-allowlist", containerName)

	// Validate configuration
	if len(m.config.AllowedDomains) == 0 {
		return fmt.Errorf("allowlist mode requires at least one allowed domain")
	}

	// 1. Load IP cache
	cache, err := m.cacheManager.Load(containerName)
	if err != nil {
		log.Printf("Warning: Failed to load cache: %v", err)
		// Initialize empty cache
		cache = &IPCache{
			Domains:    make(map[string][]string),
			LastUpdate: time.Time{},
		}
	}

	// 2. Initialize resolver with cache
	m.resolver = NewResolver(cache)

	// 3. Resolve domains
	log.Printf("Resolving %d allowed domains...", len(m.config.AllowedDomains))
	domainIPs, err := m.resolver.ResolveAll(m.config.AllowedDomains)
	if err != nil && len(domainIPs) == 0 {
		return fmt.Errorf("failed to resolve any allowed domains: %w", err)
	}

	// Log resolution results
	totalIPs := countIPs(domainIPs)
	log.Printf("Resolved %d domains to %d IPs", len(domainIPs), totalIPs)
	for domain, ips := range domainIPs {
		log.Printf("  • %s → %d IPs", domain, len(ips))
	}

	// 4. Save resolved IPs to cache
	m.resolver.UpdateCache(domainIPs)
	if err := m.cacheManager.Save(containerName, m.resolver.GetCache()); err != nil {
		log.Printf("Warning: Failed to save cache: %v", err)
	}

	// 5. Create ACL with resolved IPs
	if err := m.acl.CreateAllowlist(m.aclName, m.config, domainIPs); err != nil {
		return fmt.Errorf("failed to create allowlist ACL: %w", err)
	}

	// 6. Apply to container
	if err := m.acl.ApplyToContainer(containerName, m.aclName); err != nil {
		_ = m.acl.Delete(m.aclName)
		return fmt.Errorf("failed to apply allowlist ACL: %w", err)
	}

	log.Printf("Network ACL '%s' applied successfully", m.aclName)
	log.Println("  ✓ Allowing only specified domains")
	log.Println("  ✓ Blocking all RFC1918 private networks")
	log.Println("  ✓ Blocking cloud metadata endpoints")

	// 7. Start background refresher
	m.startRefresher(ctx)

	return nil
}

// startRefresher starts the background IP refresh goroutine
func (m *Manager) startRefresher(ctx context.Context) {
	if m.config.RefreshIntervalMinutes <= 0 {
		log.Println("IP refresh disabled (refresh_interval_minutes <= 0)")
		return
	}

	m.refreshCtx, m.refreshCancel = context.WithCancel(ctx)

	interval := time.Duration(m.config.RefreshIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)

	log.Printf("Starting IP refresh every %d minutes", m.config.RefreshIntervalMinutes)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Println("IP refresh: checking for updated IPs...")
				if err := m.refreshAllowedIPs(); err != nil {
					log.Printf("Warning: IP refresh failed: %v", err)
				}

			case <-m.refreshCtx.Done():
				log.Println("IP refresher stopped")
				return
			}
		}
	}()
}

// stopRefresher stops the background refresher goroutine
func (m *Manager) stopRefresher() {
	if m.refreshCancel != nil {
		m.refreshCancel()
		m.refreshCancel = nil
	}
}

// refreshAllowedIPs refreshes domain IPs and updates ACL if changed
func (m *Manager) refreshAllowedIPs() error {
	// Resolve all domains again
	newIPs, err := m.resolver.ResolveAll(m.config.AllowedDomains)
	if err != nil && len(newIPs) == 0 {
		return fmt.Errorf("failed to resolve any domains")
	}

	// Check if anything changed
	if m.resolver.IPsUnchanged(newIPs) {
		log.Println("IP refresh: no changes detected")
		return nil
	}

	// Update ACL with new IPs
	totalIPs := countIPs(newIPs)
	log.Printf("IP refresh: updating ACL with %d IPs", totalIPs)

	if err := m.acl.RecreateWithNewIPs(m.aclName, m.config, newIPs, m.containerName); err != nil {
		return fmt.Errorf("failed to update ACL: %w", err)
	}

	// Update cache
	m.resolver.UpdateCache(newIPs)
	if err := m.cacheManager.Save(m.containerName, m.resolver.GetCache()); err != nil {
		log.Printf("Warning: Failed to save cache: %v", err)
	}

	log.Printf("IP refresh: successfully updated ACL")
	return nil
}

// countIPs counts total IPs across all domains
func countIPs(domainIPs map[string][]string) int {
	count := 0
	for _, ips := range domainIPs {
		count += len(ips)
	}
	return count
}

// Teardown removes network isolation for a container
func (m *Manager) Teardown(ctx context.Context, containerName string) error {
	// Stop background refresher if running (for allowlist mode)
	m.stopRefresher()

	// Nothing to clean up in open mode
	if m.config.Mode == config.NetworkModeOpen {
		return nil
	}

	// Remove ACL
	aclName := m.aclName
	if aclName == "" {
		// Fallback if aclName wasn't set - try both modes
		if m.config.Mode == config.NetworkModeAllowlist {
			aclName = fmt.Sprintf("coi-%s-allowlist", containerName)
		} else {
			aclName = fmt.Sprintf("coi-%s-restricted", containerName)
		}
	}

	if err := m.acl.Delete(aclName); err != nil {
		// Don't fail teardown if ACL deletion fails
		// The ACL might have been already removed or never created
		log.Printf("Warning: failed to delete network ACL '%s': %v", aclName, err)
		return nil
	}

	log.Printf("Network ACL '%s' removed", aclName)
	return nil
}

// GetMode returns the current network mode
func (m *Manager) GetMode() config.NetworkMode {
	return m.config.Mode
}
