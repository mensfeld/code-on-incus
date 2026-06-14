package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/logger"
)

// errNftNotAvailable is the user-facing error when nft is unavailable
const errNftNotAvailable = `nft is not available or passwordless sudo is not configured

Network isolation in restricted/allowlist modes requires nftables (nft).

To fix this:
  1. Install nftables: sudo apt install nftables
  2. Configure passwordless sudo for nft:
     echo "$USER ALL=(ALL) NOPASSWD: /usr/sbin/nft" | sudo tee /etc/sudoers.d/coi-nft
     sudo chmod 0440 /etc/sudoers.d/coi-nft

Alternatively, run with unrestricted network access by setting open mode in
your config file (.coi/config.toml in your workspace, or the profile's
config.toml):

  [network]
  mode = "open"`

// Manager provides high-level network isolation management for containers
type Manager struct {
	config        *config.NetworkConfig
	nft           nftRuler
	resolver      *Resolver
	cacheManager  *CacheManager
	containerName string
	containerIP   string
	logger        *logger.SessionLogger

	// iptables fallback (when FORWARD DROP is set and bridge rules are missing)
	iptablesBridgeName string

	// Refresher lifecycle (for allowlist mode)
	refreshCtx    context.Context
	refreshCancel context.CancelFunc
}

// NewManager creates a new network manager with the specified configuration
func NewManager(cfg *config.NetworkConfig, log *logger.SessionLogger) *Manager {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}

	return &Manager{
		config:       cfg,
		cacheManager: NewCacheManager(homeDir),
		logger:       log,
	}
}

// SetupForContainer configures network isolation for a container.
// It always removes the temporary boot-block rule installed by ApplyBootBlockRule
// once proper isolation rules are in place (or, for open mode, unconditionally —
// the user has opted into unrestricted access). On error in restricted/allowlist
// mode the boot block is intentionally left in place so the container stays
// blocked until the caller tears everything down.
func (m *Manager) SetupForContainer(ctx context.Context, containerName string) error {
	m.containerName = containerName

	// Handle different network modes
	switch m.config.Mode {
	case config.NetworkModeOpen:
		m.logger.Println("Network mode: open (no restrictions)")
		// Add ACCEPT rules via nft so traffic flows even when FORWARD policy is DROP
		if NftAvailable() {
			containerIP, err := GetContainerIP(containerName)
			if err != nil {
				m.logger.Errorf("Warning: could not get container IP for open mode rules: %v", err)
			} else {
				m.containerIP = containerIP
				m.nft = NewNftManager(containerIP, "")
				if err := EnsureOpenModeRules(containerIP); err != nil {
					m.logger.Errorf("Warning: could not add open mode rules: %v", err)
				}
			}
		} else if NeedsIptablesFallback() {
			bridgeName, err := GetIncusBridgeName()
			if err != nil {
				m.logger.Errorf("Warning: could not get bridge name for iptables fallback: %v", err)
			} else {
				if err := EnsureIptablesBridgeRules(bridgeName); err != nil {
					m.logger.Errorf("Warning: could not add iptables bridge rules: %v", err)
				} else {
					m.iptablesBridgeName = bridgeName
					m.logger.Printf("iptables fallback: added FORWARD ACCEPT rules for bridge %s (FORWARD policy is DROP, nft not available)", bridgeName)
				}
			}
		}
		// Open mode always lifts the boot block — errors above are non-fatal
		// and the user has explicitly opted into unrestricted network access.
		m.removeBootBlock(containerName)
		return nil

	case config.NetworkModeRestricted:
		if err := m.setupRestricted(ctx, containerName); err != nil {
			return err // boot block stays: container remains isolated on error
		}

	case config.NetworkModeAllowlist:
		if err := m.setupAllowlist(ctx, containerName); err != nil {
			return err // boot block stays: container remains isolated on error
		}

	default:
		return fmt.Errorf("unknown network mode: %s", m.config.Mode)
	}

	// Restricted or allowlist rules are now in place — lift the boot block.
	m.removeBootBlock(containerName)
	return nil
}

// removeBootBlock removes the temporary boot-block nft rule for containerName,
// logging a warning if removal fails (non-fatal).
func (m *Manager) removeBootBlock(containerName string) {
	if err := RemoveBootBlockRule(containerName); err != nil {
		m.logger.Errorf("Warning: failed to remove boot block rule for %s: %v", containerName, err)
	}
}

// setupRestricted configures restricted mode using nftables
func (m *Manager) setupRestricted(ctx context.Context, containerName string) error {
	m.logger.Println("Network mode: restricted (blocking local/internal networks)")

	// Check if nft is available
	if !NftAvailable() {
		return fmt.Errorf("%s", errNftNotAvailable)
	}

	// Get container IP
	containerIP, err := GetContainerIP(containerName)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}
	m.containerIP = containerIP
	m.logger.Printf("Container IP: %s", containerIP)

	// Get gateway IP
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		m.logger.Errorf("Warning: Could not auto-detect gateway IP: %v", err)
	} else {
		m.logger.Printf("Gateway IP: %s", gatewayIP)
	}

	// Disable IPv6 inside the container (defence-in-depth; reversible by
	// in-container root, so the host-side drop below is the enforced boundary).
	if err := DisableIPv6ForContainer(containerName); err != nil {
		m.logger.Errorf("Warning: failed to disable IPv6 in container: %v", err)
	}

	// Enforce the IPv6 egress boundary on the host: drop all forwarded IPv6 from
	// the container's veth. The COI filter table is IPv4-only, so without this an
	// agent that re-enables IPv6 escapes the firewall entirely. Fail closed — if
	// the drop cannot be installed the boot block stays in place and setup aborts.
	if err := ApplyIPv6BlockForContainer(containerName); err != nil {
		return fmt.Errorf("failed to enforce IPv6 egress block: %w", err)
	}

	// Create nft manager
	m.nft = NewNftManager(containerIP, gatewayIP)

	// Apply restricted mode rules
	if err := m.nft.ApplyRestricted(m.config); err != nil {
		return fmt.Errorf("failed to apply nft rules: %w", err)
	}

	m.logger.Printf("nft rules applied for container %s", containerName)

	// Log what is blocked
	if config.BoolVal(m.config.BlockPrivateNetworks) {
		m.logger.Println("  Blocking private networks (RFC1918)")
	}
	if config.BoolVal(m.config.BlockMetadataEndpoint) {
		m.logger.Println("  Blocking cloud metadata endpoints")
	}

	return nil
}

// setupAllowlist configures allowlist mode with DNS resolution and refresh
func (m *Manager) setupAllowlist(ctx context.Context, containerName string) error {
	m.logger.Println("Network mode: allowlist (domain-based filtering)")

	// Check if nft is available
	if !NftAvailable() {
		return fmt.Errorf("%s", errNftNotAvailable)
	}

	// Validate configuration
	if len(m.config.AllowedDomains) == 0 {
		return fmt.Errorf("allowlist mode requires at least one allowed domain")
	}

	// Get container IP
	containerIP, err := GetContainerIP(containerName)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}
	m.containerIP = containerIP
	m.logger.Printf("Container IP: %s", containerIP)

	// Get gateway IP
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		m.logger.Errorf("Warning: Could not auto-detect gateway IP: %v", err)
	} else {
		m.logger.Printf("Gateway IP: %s", gatewayIP)
	}

	// Disable IPv6 inside the container (defence-in-depth; reversible by
	// in-container root, so the host-side drop below is the enforced boundary).
	if err := DisableIPv6ForContainer(containerName); err != nil {
		m.logger.Errorf("Warning: failed to disable IPv6 in container: %v", err)
	}

	// Enforce the IPv6 egress boundary on the host: drop all forwarded IPv6 from
	// the container's veth. The COI filter table is IPv4-only, so without this an
	// agent that re-enables IPv6 escapes the allowlist entirely. Fail closed — if
	// the drop cannot be installed the boot block stays in place and setup aborts.
	if err := ApplyIPv6BlockForContainer(containerName); err != nil {
		return fmt.Errorf("failed to enforce IPv6 egress block: %w", err)
	}

	// Create nft manager
	m.nft = NewNftManager(containerIP, gatewayIP)

	// Load IP cache
	cache, err := m.cacheManager.Load(containerName)
	if err != nil {
		m.logger.Errorf("Warning: Failed to load cache: %v", err)
		cache = &IPCache{
			Domains:    make(map[string][]string),
			TTLs:       make(map[string]uint32),
			LastUpdate: time.Time{},
		}
	}

	// Initialize resolver with cache
	m.resolver = NewResolver(cache)

	// Resolve domains
	m.logger.Printf("Resolving %d allowed domains...", len(m.config.AllowedDomains))
	domainIPs, err := m.resolver.ResolveAll(m.config.AllowedDomains)
	if err != nil && len(domainIPs) == 0 {
		return fmt.Errorf("failed to resolve any allowed domains: %w", err)
	}

	// Log resolution results
	totalIPs := countIPs(domainIPs)
	m.logger.Printf("Resolved %d domains to %d IPs", len(domainIPs), totalIPs)
	for domain, ips := range domainIPs {
		m.logger.Printf("  %s -> %d IPs", domain, len(ips))
	}

	// Save resolved IPs to cache
	m.resolver.UpdateCache(domainIPs)
	if err := m.cacheManager.Save(containerName, m.resolver.GetCache()); err != nil {
		m.logger.Errorf("Warning: Failed to save cache: %v", err)
	}

	// Collect all unique IPs from resolved domains
	allowedIPs := collectUniqueIPs(domainIPs)

	// Apply allowlist mode rules
	if err := m.nft.ApplyAllowlist(m.config, allowedIPs); err != nil {
		return fmt.Errorf("failed to apply nft rules: %w", err)
	}

	m.logger.Printf("nft rules applied for container %s", containerName)
	m.logger.Println("  Allowing only specified domains")
	m.logger.Println("  Blocking all RFC1918 private networks")
	m.logger.Println("  Blocking cloud metadata endpoints")

	// Compute initial refresh interval from DNS TTLs
	minTTL := m.resolver.GetMinTTL()

	// Start background refresher with TTL-aware interval
	m.startRefresher(ctx, minTTL)

	return nil
}

// collectUniqueIPs extracts all unique IPs from domain resolution map
func collectUniqueIPs(domainIPs map[string][]string) []string {
	uniqueIPs := make(map[string]bool)
	for _, ips := range domainIPs {
		for _, ip := range ips {
			uniqueIPs[ip] = true
		}
	}

	result := make([]string, 0, len(uniqueIPs))
	for ip := range uniqueIPs {
		result = append(result, ip)
	}
	return result
}

// computeRefreshInterval determines the refresh interval based on DNS TTL and config cap.
// The configured refresh_interval_minutes acts as a maximum cap.
// If minTTL is 0 (unknown), the config interval is used as-is.
func (m *Manager) computeRefreshInterval(minTTL uint32) time.Duration {
	configInterval := time.Duration(m.config.RefreshIntervalMinutes) * time.Minute

	if minTTL == 0 {
		return configInterval
	}

	ttlInterval := time.Duration(minTTL) * time.Second

	if ttlInterval < configInterval {
		return ttlInterval
	}

	return configInterval
}

// startRefresher starts the background IP refresh goroutine with TTL-aware scheduling.
// It uses time.Timer instead of time.Ticker to allow dynamic rescheduling after each refresh.
func (m *Manager) startRefresher(ctx context.Context, initialMinTTL uint32) {
	if m.config.RefreshIntervalMinutes <= 0 {
		m.logger.Println("IP refresh disabled (refresh_interval_minutes <= 0)")
		return
	}

	m.refreshCtx, m.refreshCancel = context.WithCancel(ctx)

	interval := m.computeRefreshInterval(initialMinTTL)
	timer := time.NewTimer(interval)

	m.logger.Printf("Starting IP refresh (interval: %s, TTL-based: %v)", interval, initialMinTTL > 0)

	go func() {
		defer timer.Stop()

		for {
			select {
			case <-timer.C:
				m.logger.Println("IP refresh: checking for updated IPs...")
				newMinTTL, err := m.refreshAllowedIPs()
				if err != nil {
					m.logger.Errorf("Warning: IP refresh failed: %v", err)
				}

				// Recompute interval from new TTLs
				nextInterval := m.computeRefreshInterval(newMinTTL)
				m.logger.Printf("IP refresh: next check in %s", nextInterval)
				timer.Reset(nextInterval)

			case <-m.refreshCtx.Done():
				m.logger.Println("IP refresher stopped")
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

// refreshAllowedIPs refreshes domain IPs and updates nft rules if changed.
// Returns the minimum TTL from the new resolution for rescheduling.
func (m *Manager) refreshAllowedIPs() (uint32, error) {
	// Resolve all domains again
	newIPs, err := m.resolver.ResolveAll(m.config.AllowedDomains)
	if err != nil && len(newIPs) == 0 {
		return 0, fmt.Errorf("failed to resolve any domains")
	}

	newMinTTL := m.resolver.GetMinTTL()

	// Check if anything changed
	if m.resolver.IPsUnchanged(newIPs) {
		m.logger.Println("IP refresh: no changes detected")
		return newMinTTL, nil
	}

	// Update nft rules with new IPs
	totalIPs := countIPs(newIPs)
	m.logger.Printf("IP refresh: updating nft rules with %d IPs", totalIPs)

	// Replace rules atomically: snapshot old handles, append new rules, then
	// delete only the old handles. This avoids any window where all rules are
	// absent and the chain's default-accept policy would pass all traffic.
	allowedIPs := collectUniqueIPs(newIPs)
	if err := m.nft.ReplaceAllowlist(m.config, allowedIPs); err != nil {
		return newMinTTL, fmt.Errorf("failed to update nft rules: %w", err)
	}

	// Update cache
	m.resolver.UpdateCache(newIPs)
	if err := m.cacheManager.Save(m.containerName, m.resolver.GetCache()); err != nil {
		m.logger.Errorf("Warning: Failed to save cache: %v", err)
	}

	m.logger.Printf("IP refresh: successfully updated nft rules")
	return newMinTTL, nil
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

	// Clean up iptables bridge rules if we added them
	if m.iptablesBridgeName != "" {
		// Check if other coi containers are still running before removing
		output, err := container.IncusOutput("list", "--format=json")
		hasOtherContainers := true // conservative default
		if err == nil {
			hasOtherContainers = otherContainersRunning(output, containerName)
		}

		if !hasOtherContainers {
			if err := RemoveIptablesBridgeRules(m.iptablesBridgeName); err != nil {
				m.logger.Errorf("Warning: failed to remove iptables bridge rules: %v", err)
			} else {
				m.logger.Printf("iptables fallback: removed FORWARD ACCEPT rules for bridge %s", m.iptablesBridgeName)
			}
		} else {
			m.logger.Printf("iptables fallback: skipping rule removal, other containers still running")
		}
	}

	// For open mode, also clean up nft ACCEPT rules created by EnsureOpenModeRules()
	if m.config.Mode == config.NetworkModeOpen {
		if !NftAvailable() && m.iptablesBridgeName == "" {
			return nil // No nft and no iptables fallback, no rules to clean up
		}

		// Use cached container IP if available (set during SetupForContainer)
		// Only try to get from container if not cached
		if m.containerIP == "" {
			containerIP, err := GetContainerIP(containerName)
			if err != nil {
				return nil // Container might be already deleted, and IP wasn't cached
			}
			m.containerIP = containerIP
		}

		// Create nft manager if not already created
		if m.nft == nil {
			m.nft = NewNftManager(m.containerIP, "")
		}
	}

	// Remove nft rules for ALL modes
	if m.nft != nil {
		if err := m.nft.RemoveRules(); err != nil {
			m.logger.Errorf("Warning: failed to remove nft rules: %v", err)
		} else {
			m.logger.Printf("nft rules removed for container %s", containerName)
		}
	}

	// Remove the host-side IPv6 egress block (idempotent — only present for
	// restricted/allowlist modes, but safe to call for all modes).
	if err := RemoveIPv6BlockForContainer(containerName); err != nil {
		m.logger.Errorf("Warning: failed to remove IPv6 block rule for %s: %v", containerName, err)
	}

	// Clean up any residual boot-block rule (idempotent — no-op if already removed
	// by SetupForContainer, but handles the case where setup failed mid-way).
	m.removeBootBlock(containerName)

	return nil
}

// GetMode returns the current network mode
func (m *Manager) GetMode() config.NetworkMode {
	return m.config.Mode
}

// GetContainerGatewayIP exports the gateway IP detection for external use
func GetContainerGatewayIP(containerName string) (string, error) {
	return getContainerGatewayIP(containerName)
}

// getContainerGatewayIP auto-detects the gateway IP for a container's network
func getContainerGatewayIP(containerName string) (string, error) {
	networkName, err := GetIncusBridgeName()
	if err != nil {
		return "", err
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

// otherContainersRunning parses the JSON output of `incus list --format=json`
// and reports whether any container other than excludeName is currently Running.
// On JSON parse failure it returns true (conservative: keep bridge rules).
func otherContainersRunning(jsonOutput, excludeName string) bool {
	var containers []struct {
		Name  string `json:"name"`
		State struct {
			Status string `json:"status"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &containers); err != nil {
		return true // conservative: can't confirm no other containers, keep rules
	}
	for _, c := range containers {
		if c.Name != excludeName && c.State.Status == "Running" {
			return true
		}
	}
	return false
}
