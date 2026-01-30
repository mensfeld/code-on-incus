package network

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// Manager provides high-level network isolation management for containers
type Manager struct {
	config        *config.NetworkConfig
	firewall      *FirewallManager
	resolver      *Resolver
	cacheManager  *CacheManager
	containerName string
	containerIP   string // Stored for cleanup

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
		firewall:     NewFirewallManager(),
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

// setupRestricted configures restricted mode (blocks RFC1918 and metadata)
func (m *Manager) setupRestricted(ctx context.Context, containerName string) error {
	log.Println("Network mode: restricted (blocking local/internal networks)")

	// Check firewalld availability
	if err := CheckFirewalldAvailable(); err != nil {
		return fmt.Errorf("restricted mode requires firewalld: %w\n\nTo fix this, either:\n  1. Install and start firewalld:\n     Linux: sudo apt install firewalld && sudo systemctl enable --now firewalld\n     Note: macOS does not support firewalld - use --network=open instead\n  2. Run with unrestricted network access: coi shell --network=open", err)
	}

	// Wait for container to get an IP address
	containerIP, err := m.waitForContainerIP(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}
	m.containerIP = containerIP
	log.Printf("Container IP: %s", containerIP)

	// Get gateway IP for host communication
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		log.Printf("Warning: Could not auto-detect gateway IP: %v", err)
	} else {
		log.Printf("Gateway IP: %s", gatewayIP)
	}

	// Apply firewall rules
	if err := m.firewall.ApplyRestricted(containerIP, gatewayIP, m.config); err != nil {
		return fmt.Errorf("failed to apply firewall rules: %w", err)
	}

	// Log what is blocked
	if m.config.BlockPrivateNetworks {
		log.Println("  Blocking private networks (RFC1918)")
	}
	if m.config.BlockMetadataEndpoint {
		log.Println("  Blocking cloud metadata endpoints")
	}

	return nil
}

// setupAllowlist configures allowlist mode with DNS resolution and refresh
func (m *Manager) setupAllowlist(ctx context.Context, containerName string) error {
	log.Println("Network mode: allowlist (domain-based filtering)")

	// Check firewalld availability
	if err := CheckFirewalldAvailable(); err != nil {
		return fmt.Errorf("allowlist mode requires firewalld: %w\n\nTo fix this, either:\n  1. Install and start firewalld:\n     Linux: sudo apt install firewalld && sudo systemctl enable --now firewalld\n     Note: macOS does not support firewalld - use --network=open instead\n  2. Run with unrestricted network access: coi shell --network=open", err)
	}

	// Validate configuration
	if len(m.config.AllowedDomains) == 0 {
		return fmt.Errorf("allowlist mode requires at least one allowed domain")
	}

	// Wait for container to get an IP address
	containerIP, err := m.waitForContainerIP(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}
	m.containerIP = containerIP
	log.Printf("Container IP: %s", containerIP)

	// Get gateway IP for host communication
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		log.Printf("Warning: Could not auto-detect gateway IP: %v", err)
	} else {
		log.Printf("Gateway IP: %s", gatewayIP)
	}

	// Load IP cache
	cache, err := m.cacheManager.Load(containerName)
	if err != nil {
		log.Printf("Warning: Failed to load cache: %v", err)
		cache = &IPCache{
			Domains:    make(map[string][]string),
			LastUpdate: time.Time{},
		}
	}

	// Initialize resolver with cache
	m.resolver = NewResolver(cache)

	// Resolve domains
	log.Printf("Resolving %d allowed domains...", len(m.config.AllowedDomains))
	domainIPs, err := m.resolver.ResolveAll(m.config.AllowedDomains)
	if err != nil && len(domainIPs) == 0 {
		return fmt.Errorf("failed to resolve any allowed domains: %w", err)
	}

	// Add gateway IP to domainIPs map
	if gatewayIP != "" {
		if net.ParseIP(gatewayIP) != nil {
			domainIPs["__internal_gateway__"] = []string{gatewayIP}
			log.Printf("Auto-detected gateway IP: %s", gatewayIP)
		}
	}

	// Log resolution results
	totalIPs := countIPs(domainIPs)
	log.Printf("Resolved %d domains to %d IPs", len(domainIPs), totalIPs)
	for domain, ips := range domainIPs {
		log.Printf("  %s -> %d IPs", domain, len(ips))
	}

	// Save resolved IPs to cache
	m.resolver.UpdateCache(domainIPs)
	if err := m.cacheManager.Save(containerName, m.resolver.GetCache()); err != nil {
		log.Printf("Warning: Failed to save cache: %v", err)
	}

	// Apply firewall rules
	if err := m.firewall.ApplyAllowlist(containerIP, gatewayIP, m.config, domainIPs); err != nil {
		return fmt.Errorf("failed to apply firewall rules: %w", err)
	}

	log.Println("  Allowing only specified domains")
	log.Println("  Blocking all RFC1918 private networks")
	log.Println("  Blocking cloud metadata endpoints")

	// Start background refresher
	m.startRefresher(ctx, gatewayIP)

	return nil
}

// waitForContainerIP waits for the container to get an IP address
func (m *Manager) waitForContainerIP(ctx context.Context, containerName string) (string, error) {
	// Try up to 60 times with 1 second delay (60 seconds total)
	// CI environments with parallel tests may have slower DHCP
	maxAttempts := 60
	for i := 0; i < maxAttempts; i++ {
		ip, err := GetContainerIP(containerName)
		if err == nil && ip != "" {
			return ip, nil
		}

		if i > 0 && i%10 == 0 {
			log.Printf("Still waiting for container IP... (%d/%d seconds)", i, maxAttempts)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1 * time.Second):
			// Continue waiting
		}
	}

	return "", fmt.Errorf("timeout waiting for container IP")
}

// startRefresher starts the background IP refresh goroutine
func (m *Manager) startRefresher(ctx context.Context, gatewayIP string) {
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
				if err := m.refreshAllowedIPs(gatewayIP); err != nil {
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

// refreshAllowedIPs refreshes domain IPs and updates firewall rules if changed
func (m *Manager) refreshAllowedIPs(gatewayIP string) error {
	// Resolve all domains again
	newIPs, err := m.resolver.ResolveAll(m.config.AllowedDomains)
	if err != nil && len(newIPs) == 0 {
		return fmt.Errorf("failed to resolve any domains")
	}

	// Re-add gateway IP
	if gatewayIP != "" {
		if net.ParseIP(gatewayIP) != nil {
			newIPs["__internal_gateway__"] = []string{gatewayIP}
		}
	}

	// Check if anything changed
	if m.resolver.IPsUnchanged(newIPs) {
		log.Println("IP refresh: no changes detected")
		return nil
	}

	// Update firewall rules with new IPs
	totalIPs := countIPs(newIPs)
	log.Printf("IP refresh: updating firewall rules with %d IPs", totalIPs)

	// Remove old rules and apply new ones
	if err := m.firewall.RemoveContainerRules(m.containerIP); err != nil {
		log.Printf("Warning: failed to remove old rules: %v", err)
	}

	if err := m.firewall.ApplyAllowlist(m.containerIP, gatewayIP, m.config, newIPs); err != nil {
		return fmt.Errorf("failed to apply updated firewall rules: %w", err)
	}

	// Update cache
	m.resolver.UpdateCache(newIPs)
	if err := m.cacheManager.Save(m.containerName, m.resolver.GetCache()); err != nil {
		log.Printf("Warning: Failed to save cache: %v", err)
	}

	log.Printf("IP refresh: successfully updated firewall rules")
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

	// Get container IP if we don't have it stored
	containerIP := m.containerIP
	if containerIP == "" {
		ip, err := GetContainerIP(containerName)
		if err != nil {
			// Container might already be stopped/deleted
			log.Printf("Warning: could not get container IP for cleanup: %v", err)
			return nil
		}
		containerIP = ip
	}

	// Remove firewall rules
	if err := m.firewall.RemoveContainerRules(containerIP); err != nil {
		log.Printf("Warning: failed to remove firewall rules: %v", err)
		return nil
	}

	log.Printf("Firewall rules removed for %s", containerIP)
	return nil
}

// GetMode returns the current network mode
func (m *Manager) GetMode() config.NetworkMode {
	return m.config.Mode
}

// getContainerGatewayIP auto-detects the gateway IP for a container's network
func getContainerGatewayIP(containerName string) (string, error) {
	// Get container's network configuration from default profile
	profileOutput, err := container.IncusOutput("profile", "device", "show", "default")
	if err != nil {
		return "", fmt.Errorf("failed to get default profile: %w", err)
	}

	// Parse network name from profile (eth0 device)
	var networkName string
	lines := strings.Split(profileOutput, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "eth0:" {
			// Look for network: line
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				if strings.Contains(lines[j], "network:") {
					parts := strings.Split(lines[j], ":")
					if len(parts) >= 2 {
						networkName = strings.TrimSpace(parts[1])
						break
					}
				}
			}
			break
		}
	}

	if networkName == "" {
		return "", fmt.Errorf("could not determine network name from profile")
	}

	// Get network configuration
	networkOutput, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return "", fmt.Errorf("failed to get network info: %w", err)
	}

	// Parse gateway IP (ipv4.address field)
	for _, line := range strings.Split(networkOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ipv4.address:") {
			addressWithMask := strings.TrimSpace(strings.TrimPrefix(line, "ipv4.address:"))
			// Remove CIDR suffix (e.g., "10.128.178.1/24" -> "10.128.178.1")
			gatewayIP := addressWithMask
			if idx := strings.Index(addressWithMask, "/"); idx != -1 {
				gatewayIP = addressWithMask[:idx]
			}

			// Validate that we extracted a valid IPv4 address
			if net.ParseIP(gatewayIP) == nil {
				return "", fmt.Errorf("invalid IPv4 address extracted: %s", gatewayIP)
			}

			return gatewayIP, nil
		}
	}

	return "", fmt.Errorf("could not find ipv4.address in network %s", networkName)
}
