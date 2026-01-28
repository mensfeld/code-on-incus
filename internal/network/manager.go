package network

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// errACLNotSupportedMessage is the user-facing error message when ACLs are not supported.
// Extracted as a constant to avoid duplication between setupRestricted and setupAllowlist.
const errACLNotSupportedMessage = `network ACLs not supported: your Incus network does not support security.acls

This feature requires an OVN (Open Virtual Network) network. Your current network
uses a standard bridge which doesn't support egress filtering via ACLs.

To fix this, choose one of these options:

  1. Run with unrestricted network access:
     coi shell --network=open

  2. Set up an OVN network in Incus (recommended for production):
     - Install OVN: sudo apt install ovn-host ovn-central
     - Create OVN network: incus network create ovn-net --type=ovn
     - Update default profile to use the OVN network

For more information, see: https://linuxcontainers.org/incus/docs/main/howto/network_ovn/`

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

	// Ensure host can route to OVN networks (if applicable)
	// This allows users to access container services from their host
	if err := ensureHostRoute(containerName); err != nil {
		// Log warning but don't fail - routing is a convenience feature
		log.Printf("Warning: Could not configure host routing: %v", err)
	}

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
	if err := m.acl.Create(m.aclName, m.config, containerName); err != nil {
		return fmt.Errorf("failed to create network ACL: %w", err)
	}

	// 2. Apply ACL to container
	if err := m.acl.ApplyToContainer(containerName, m.aclName); err != nil {
		// Clean up the ACL
		_ = m.acl.Delete(m.aclName)

		// Check if ACLs are not supported (non-OVN network)
		if errors.Is(err, ErrACLNotSupported) {
			return fmt.Errorf("%s", errACLNotSupportedMessage)
		}

		return fmt.Errorf("failed to apply network ACL: %w", err)
	}

	log.Printf("Network ACL '%s' applied successfully", m.aclName)

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

	// 3a. Auto-detect and add gateway IP (required for routing)
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		log.Printf("Warning: Could not auto-detect gateway IP: %v", err)
		log.Println("You may need to manually add the gateway IP to allowed_domains")
	} else {
		// Validate gateway IP before adding
		if net.ParseIP(gatewayIP) == nil {
			log.Printf("Warning: Invalid gateway IP detected: %s", gatewayIP)
		} else {
			// Add gateway IP to domainIPs map with internal key
			domainIPs["__internal_gateway__"] = []string{gatewayIP}
			log.Printf("Auto-detected gateway IP: %s", gatewayIP)
		}
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

		// Check if ACLs are not supported (non-OVN network)
		if errors.Is(err, ErrACLNotSupported) {
			return fmt.Errorf("%s", errACLNotSupportedMessage)
		}

		return fmt.Errorf("failed to apply allowlist ACL: %w", err)
	}

	log.Printf("Network ACL '%s' applied successfully", m.aclName)
	log.Println("  Allowing only specified domains")
	log.Println("  Blocking all RFC1918 private networks")
	log.Println("  Blocking cloud metadata endpoints")

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

	// Re-add gateway IP (required for routing, must persist across refreshes)
	gatewayIP, err := getContainerGatewayIP(m.containerName)
	if err != nil {
		log.Printf("Warning: Could not re-detect gateway IP during refresh: %v", err)
	} else {
		// Validate gateway IP before adding
		if net.ParseIP(gatewayIP) == nil {
			log.Printf("Warning: Invalid gateway IP detected during refresh: %s", gatewayIP)
		} else {
			newIPs["__internal_gateway__"] = []string{gatewayIP}
			log.Printf("Re-added gateway IP during refresh: %s", gatewayIP)
		}
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
		// With the existence check in Delete(), this only happens if the ACL exists but
		// deletion failed due to unexpected errors (e.g., permissions, network issues).
		// The normal "ACL still attached" case is prevented by deleting the container first.
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

// ensureHostRoute ensures the host can route to OVN networks
// This allows users to access container services (web servers, databases, etc.) from their host
func ensureHostRoute(containerName string) error {
	// Get the network name for this container
	networkName, err := getContainerNetworkName(containerName)
	if err != nil {
		return fmt.Errorf("failed to get network name: %w", err)
	}

	// Check if this is an OVN network
	isOVN, err := isOVNNetwork(networkName)
	if err != nil {
		return fmt.Errorf("failed to check network type: %w", err)
	}
	if !isOVN {
		// Not an OVN network, no routing needed
		return nil
	}

	// Get OVN network configuration
	subnet, err := getNetworkSubnet(networkName)
	if err != nil {
		return fmt.Errorf("failed to get OVN subnet: %w", err)
	}

	uplinkBridge, err := getOVNUplinkBridge(networkName)
	if err != nil {
		return fmt.Errorf("failed to get OVN uplink bridge: %w", err)
	}

	ovnUplinkIP, err := getOVNUplinkIP(networkName)
	if err != nil {
		return fmt.Errorf("failed to get OVN uplink IP: %w", err)
	}

	// Check if route already exists
	if routeExists(subnet, ovnUplinkIP) {
		log.Printf("Host route already configured: %s via %s", subnet, ovnUplinkIP)
		return nil
	}

	// Try to add route
	if err := addRoute(subnet, ovnUplinkIP, uplinkBridge); err != nil {
		// Provide helpful message if we can't add route
		log.Printf("ℹ️  OVN Network Routing")
		log.Printf("")
		log.Printf("Your container is on an OVN network (%s). To access services running", subnet)
		log.Printf("in the container from your host machine (web servers, databases, etc.),")
		log.Printf("you need to add a route. This is independent of the network mode.")
		log.Printf("")
		log.Printf("Run this command to enable host-to-container connectivity:")
		log.Printf("  sudo ip route add %s via %s dev %s", subnet, ovnUplinkIP, uplinkBridge)
		log.Printf("")
		log.Printf("The route persists until reboot. COI automatically checks and re-adds it")
		log.Printf("when starting containers. For fully automatic setup after reboot, either:")
		log.Printf("  1. Configure passwordless sudo for 'ip route' (see README)")
		log.Printf("  2. Add the route to your network configuration (netplan/systemd)")
		log.Printf("")
		return nil // Don't fail container startup
	}

	log.Printf("✓ OVN host route configured: %s via %s", subnet, ovnUplinkIP)
	log.Printf("  Container services are accessible from your host machine")
	return nil
}

// getContainerNetworkName retrieves the network name from the default profile
func getContainerNetworkName(containerName string) (string, error) {
	profileOutput, err := container.IncusOutput("profile", "device", "show", "default")
	if err != nil {
		return "", fmt.Errorf("failed to get default profile: %w", err)
	}

	lines := strings.Split(profileOutput, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "eth0:" {
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				if strings.Contains(lines[j], "network:") {
					parts := strings.Split(lines[j], ":")
					if len(parts) >= 2 {
						return strings.TrimSpace(parts[1]), nil
					}
				}
			}
			break
		}
	}

	return "", fmt.Errorf("could not determine network name from profile")
}

// isOVNNetwork checks if a network is of type OVN
func isOVNNetwork(networkName string) (bool, error) {
	output, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return false, fmt.Errorf("failed to get network info: %w", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "type:") {
			networkType := strings.TrimSpace(strings.TrimPrefix(line, "type:"))
			return networkType == "ovn", nil
		}
	}

	return false, nil
}

// getNetworkSubnet gets the IPv4 subnet of a network (e.g., "10.215.220.0/24")
// It parses the ipv4.address (which is gateway IP + CIDR) and converts to network subnet
func getNetworkSubnet(networkName string) (string, error) {
	output, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return "", fmt.Errorf("failed to get network info: %w", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ipv4.address:") {
			addressWithCIDR := strings.TrimSpace(strings.TrimPrefix(line, "ipv4.address:"))

			// Parse the CIDR notation (e.g., "10.128.178.1/24")
			_, ipnet, err := net.ParseCIDR(addressWithCIDR)
			if err != nil {
				return "", fmt.Errorf("failed to parse CIDR %s: %w", addressWithCIDR, err)
			}

			// Return the network address (e.g., "10.128.178.0/24")
			return ipnet.String(), nil
		}
	}

	return "", fmt.Errorf("could not find ipv4.address in network config")
}

// getOVNUplinkBridge gets the uplink bridge name for an OVN network (e.g., "incusbr0")
func getOVNUplinkBridge(networkName string) (string, error) {
	output, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return "", fmt.Errorf("failed to get network info: %w", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "network:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "network:")), nil
		}
	}

	return "", fmt.Errorf("could not find uplink network in OVN config")
}

// getOVNUplinkIP gets the OVN's uplink IP on the bridge (e.g., "10.47.62.100")
// This is the IP the OVN network uses as its gateway on the uplink bridge
func getOVNUplinkIP(networkName string) (string, error) {
	output, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return "", fmt.Errorf("failed to get network info: %w", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "volatile.network.ipv4.address:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "volatile.network.ipv4.address:")), nil
		}
	}

	return "", fmt.Errorf("could not find volatile.network.ipv4.address in OVN config")
}

// routeExists checks if a route already exists on the host
func routeExists(subnet, gateway string) bool {
	// Run 'ip route show' on the host (not inside container)
	cmd := exec.Command("ip", "route", "show")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	// Look for a line like: "10.215.220.0/24 via 10.47.62.100 dev incusbr0"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, subnet) && strings.Contains(line, gateway) {
			return true
		}
	}

	return false
}

// addRoute adds a route to the routing table on the host
func addRoute(subnet, gateway, bridge string) error {
	// Try to add route using ip command on the host (not inside container)
	// This requires either:
	// 1. Running as root/sudo
	// 2. Having CAP_NET_ADMIN capability
	// 3. User having permissions via sudo NOPASSWD for ip command
	cmdStr := fmt.Sprintf("ip route add %s via %s dev %s", subnet, gateway, bridge)

	// Try with sudo first (non-interactive: -n)
	cmd := exec.Command("sudo", "-n", "ip", "route", "add", subnet, "via", gateway, "dev", bridge)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Try without sudo (if running as root or with capabilities)
	cmd = exec.Command("ip", "route", "add", subnet, "via", gateway, "dev", bridge)
	if err := cmd.Run(); err == nil {
		return nil
	}

	return fmt.Errorf("failed to add route (need sudo): %s", cmdStr)
}
