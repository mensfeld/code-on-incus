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
  mode = "open"

If you have deliberately declined passwordless sudo (use_sudo = false), note
that restricted/allowlist modes cannot be enforced without it — use open mode.`

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

	// Allowlist mode: the compiled policy, and the resolver that enforces it by
	// installing each answer's addresses before handing the answer back.
	policy   *AllowPolicy
	dnsProxy *DNSProxy

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

	// Route the package's resolver/nft diagnostics (which log via the package
	// logger, including from package-level helpers and the background refresher)
	// to this Manager's logger. This makes the caller's choice authoritative: a
	// discard logger (coi run / ConfigureContainer) actually silences network
	// output instead of falling back to stderr, and a session file logger keeps
	// it off the tmux terminal — issue #372. session.Setup also calls SetLogger
	// earlier to cover the pre-Manager boot rules; that is the same logger, so
	// the two stay consistent.
	SetLogger(log)

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
		if NftUsable(m.config) {
			containerIP, err := GetContainerIP(containerName)
			if err != nil {
				m.logger.Errorf("Warning: could not get container IP for open mode rules: %v", err)
			} else {
				m.containerIP = containerIP
				m.nft = NewNftManager(containerIP, "")
				m.purgeStaleRulesForIP(containerIP)
				if err := EnsureOpenModeRules(containerIP); err != nil {
					m.logger.Errorf("Warning: could not add open mode rules: %v", err)
				}
			}
		} else if m.config.SudoAllowed() && NeedsIptablesFallback() {
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

// purgeStaleRulesForIP removes any coi-<IP> rules left in the forward chain by a
// previous container that held this IP. Incus DHCP recycles leases, so a new
// container frequently reuses a prior one's address; if that prior container did
// not tear down cleanly (kill -9, OOM, host crash, or a teardown that could not
// resolve the already-deleted container's IP), its IP-keyed rules are orphaned.
// Because the forward chain is evaluated first-match-wins, an inherited blanket
// ACCEPT would let a restricted/allowlist successor bypass its filter entirely.
// Reset-then-apply: purge the IP's rules before installing this container's
// policy. Best-effort — deleteNFTRulesByComment retries internally and the rule
// comment is matched exactly, so coi-base and coi-boot-<name> are never touched.
func (m *Manager) purgeStaleRulesForIP(containerIP string) {
	if containerIP == "" {
		return
	}
	if err := DeleteCOIFilterRulesForIP(containerIP); err != nil {
		m.logger.Errorf("Warning: failed to purge stale nft rules for %s: %v", containerIP, err)
	}
}

// setupRestricted configures restricted mode using nftables
func (m *Manager) setupRestricted(ctx context.Context, containerName string) error {
	m.logger.Println("Network mode: restricted (blocking local/internal networks)")

	// Check if nft is available (and that sudo is permitted by config)
	if !NftUsable(m.config) {
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
	m.purgeStaleRulesForIP(containerIP)

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

// setupAllowlist configures allowlist mode.
//
// The allowlist is enforced at DNS resolution time: COI becomes the container's
// resolver (transparently — see dnsintercept.go), and every address it hands
// back for an allowed name is installed in the container's nft set before the
// answer is returned. The container therefore cannot learn about an address the
// firewall does not already trust, which is what makes the mode exact.
//
// Literal IP/CIDR entries in allowed_domains skip DNS entirely and go straight
// into the static set.
func (m *Manager) setupAllowlist(ctx context.Context, containerName string) error {
	m.logger.Println("Network mode: allowlist (DNS-enforced domain filtering)")

	// Check if nft is available (and that sudo is permitted by config)
	if !NftUsable(m.config) {
		return fmt.Errorf("%s", errNftNotAvailable)
	}

	// Validate configuration
	if len(m.config.AllowedDomains) == 0 {
		return fmt.Errorf("allowlist mode requires at least one allowed domain")
	}

	policy, err := NewAllowPolicy(m.config.AllowedDomains)
	if err != nil {
		return fmt.Errorf("invalid allowed_domains: %w", err)
	}
	m.policy = policy

	// Get container IP
	containerIP, err := GetContainerIP(containerName)
	if err != nil {
		return fmt.Errorf("failed to get container IP: %w", err)
	}
	m.containerIP = containerIP
	m.logger.Printf("Container IP: %s", containerIP)

	// The gateway address is where the DNS proxy listens and where the intercept
	// rule redirects to, so allowlist mode cannot proceed without it. Failing
	// here is fail-closed: the boot block stays in place and the container has no
	// egress at all.
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		return fmt.Errorf("failed to determine gateway IP (required for DNS-enforced allowlist): %w", err)
	}
	m.logger.Printf("Gateway IP: %s", gatewayIP)

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
	m.purgeStaleRulesForIP(containerIP)

	// Install the rules and the static (literal IP/CIDR) entries. The rules name
	// the container's sets, not individual addresses, so they are stable for the
	// life of the container.
	staticCIDRs := policy.StaticCIDRs()
	if err := m.nft.ApplyAllowlist(m.config, staticCIDRs); err != nil {
		return fmt.Errorf("failed to apply nft rules: %w", err)
	}
	m.logger.Printf("nft rules applied for container %s (%d literal address entries, %d domain patterns)",
		containerName, len(staticCIDRs), len(policy.Names()))

	// Stand up the resolver and put it in front of the container. Both steps are
	// fatal on failure: without them the container would be left with an
	// allowlist that only ever contains its literal entries, and every domain
	// would silently fail to resolve.
	if err := m.startDNSProxy(gatewayIP, containerIP); err != nil {
		return err
	}

	m.logger.Println("  Allowing only allowlisted domains (enforced at DNS resolution)")
	m.logger.Println("  Blocking all RFC1918 private networks")
	m.logger.Println("  Blocking cloud metadata endpoints")

	// Prewarm the dynamic set by resolving the configured names up front, so a
	// container that somehow bypasses the proxy — or one whose first connection
	// races its own first query — is not starting from an empty set. This is a
	// safety net, not the enforcement path.
	minTTL := m.prewarmDynamicSet(containerName)

	// Keep the background refresher running as a second safety net: it re-adds
	// the configured names' addresses, which refreshes their set-element timeouts
	// even for a container sitting idle.
	m.startRefresher(ctx, minTTL)

	return nil
}

// startDNSProxy binds the DNS proxy to the gateway address and redirects the
// container's port-53 traffic to it.
func (m *Manager) startDNSProxy(gatewayIP, containerIP string) error {
	proxy, err := NewDNSProxy(m.policy, m.nftSetAllower(), m.logger)
	if err != nil {
		return fmt.Errorf("failed to create DNS proxy: %w", err)
	}
	if err := proxy.Start(gatewayIP); err != nil {
		return fmt.Errorf("failed to start DNS proxy: %w", err)
	}
	m.dnsProxy = proxy
	m.logger.Printf("DNS proxy listening on %s:%d", gatewayIP, proxy.Port())

	if err := EnsureDNSIntercept(containerIP, gatewayIP, proxy.Port()); err != nil {
		proxy.Stop()
		m.dnsProxy = nil
		return fmt.Errorf("failed to redirect container DNS to the COI resolver: %w", err)
	}
	m.logger.Printf("DNS intercept installed: %s:53 -> %s:%d", containerIP, gatewayIP, proxy.Port())
	return nil
}

// nftSetAllower exposes the nft manager's dynamic-set writer to the DNS proxy.
// Returns nil when the nft manager is a test stub that cannot install elements.
func (m *Manager) nftSetAllower() dynAllower {
	if a, ok := m.nft.(dynAllower); ok {
		return a
	}
	return noopAllower{}
}

// noopAllower stands in when the nft layer cannot install set elements (tests).
type noopAllower struct{}

func (noopAllower) AllowDynamicIPs([]string, uint32) error { return nil }

// prewarmDynamicSet resolves the policy's name entries once and seeds their
// addresses into the dynamic set. Returns the minimum TTL seen, for scheduling
// the refresher. Failures are warnings: the DNS proxy is the enforcement path,
// and it will install addresses as the container asks for them.
func (m *Manager) prewarmDynamicSet(containerName string) uint32 {
	names := m.policy.Names()
	if len(names) == 0 {
		return 0
	}

	cache, err := m.cacheManager.Load(containerName)
	if err != nil {
		m.logger.Errorf("Warning: Failed to load cache: %v", err)
		cache = &IPCache{
			Domains:    make(map[string][]string),
			TTLs:       make(map[string]uint32),
			LastUpdate: time.Time{},
		}
	}
	m.resolver = NewResolver(cache)

	m.logger.Printf("Prewarming allowlist with %d domains...", len(names))
	domainIPs, err := m.resolver.ResolveAll(names)
	if err != nil && len(domainIPs) == 0 {
		m.logger.Errorf("Warning: prewarm resolved no domains: %v", err)
		return 0
	}

	m.installResolved(domainIPs)

	m.resolver.UpdateCache(domainIPs)
	if err := m.cacheManager.Save(containerName, m.resolver.GetCache()); err != nil {
		m.logger.Errorf("Warning: Failed to save cache: %v", err)
	}

	return m.resolver.GetMinTTL()
}

// installResolved adds resolved addresses to the dynamic set, using each
// domain's own TTL to derive its element timeout.
func (m *Manager) installResolved(domainIPs map[string][]string) {
	allower := m.nftSetAllower()
	for domain, ips := range domainIPs {
		if len(ips) == 0 {
			continue
		}
		ttl := m.resolver.DomainTTLs[domain]
		if err := allower.AllowDynamicIPs(ips, ttl); err != nil {
			m.logger.Errorf("Warning: failed to allow %d addresses for %s: %v", len(ips), domain, err)
			continue
		}
		m.logger.Printf("  %s -> %d addresses", domain, len(ips))
	}
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

// refreshAllowedIPs re-resolves the policy's name entries and re-adds their
// addresses to the dynamic set. Returns the minimum TTL for rescheduling.
//
// This is a safety net, not the enforcement path — DNSProxy installs addresses
// as the container asks for them. The refresher exists so that an idle
// container's set elements keep having their timeouts refreshed, and so a
// container that bypasses the proxy still finds the configured domains reachable.
//
// Unlike the old implementation there is no "did anything change?" short-circuit
// and no rule rewrite. Re-adding an element that is already present simply
// refreshes its kernel timeout, and AllowDynamicIPs skips the nft call entirely
// for elements that still have most of their life left — so an unchanged
// allowlist costs nothing.
func (m *Manager) refreshAllowedIPs() (uint32, error) {
	if m.policy == nil || m.resolver == nil {
		return 0, nil
	}
	names := m.policy.Names()
	if len(names) == 0 {
		return 0, nil
	}

	newIPs, err := m.resolver.ResolveAll(names)
	if err != nil && len(newIPs) == 0 {
		return 0, fmt.Errorf("failed to resolve any domains")
	}

	m.installResolved(newIPs)

	m.resolver.UpdateCache(newIPs)
	if err := m.cacheManager.Save(m.containerName, m.resolver.GetCache()); err != nil {
		m.logger.Errorf("Warning: Failed to save cache: %v", err)
	}

	return m.resolver.GetMinTTL(), nil
}

// Teardown removes network isolation for a container
func (m *Manager) Teardown(ctx context.Context, containerName string) error {
	// Stop background refresher if running (for allowlist mode)
	m.stopRefresher()

	// Stop the DNS proxy and drop the intercept rule that pointed at it. Do this
	// before the filter rules come down: the intercept redirects the container's
	// DNS to a listener that is about to disappear, and a stale DNAT rule would
	// black-hole DNS for whatever gets this IP next.
	if m.dnsProxy != nil {
		m.dnsProxy.Stop()
		m.dnsProxy = nil
	}
	if m.containerIP != "" {
		if err := RemoveDNSIntercept(m.containerIP); err != nil {
			m.logger.Errorf("Warning: failed to remove DNS intercept rule: %v", err)
		}
	}

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
		if !NftUsable(m.config) && m.iptablesBridgeName == "" {
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
