package network

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/logger"
)

// linesWithComment returns the nft rule lines carrying this container's coi-<ip>
// comment, so assertions bind to THIS test's rules rather than any leftover or
// concurrent container's rules in the shared chain.
func linesWithComment(rules, containerIP string) []string {
	comment := `comment "coi-` + containerIP + `"`
	var out []string
	for _, line := range strings.Split(rules, "\n") {
		if strings.Contains(line, comment) {
			out = append(out, line)
		}
	}
	return out
}

// TestAllowlistModeAllowedPorts_Integration verifies that allowed_ports in
// allowlist mode scopes the set-accept rules to those destination ports: an
// allowlisted CIDR becomes reachable on 80/443 only, carrying a `th dport` match.
func TestAllowlistModeAllowedPorts_Integration(t *testing.T) {
	skipUnlessAllowlistReady(t)

	const testCIDR = "203.0.113.0/24" // TEST-NET-3 (RFC 5737)

	containerName := "coi-allowlist-ports-test"
	mgr := container.NewManager(containerName)

	t.Cleanup(func() { cleanupTestContainer(t, containerName) })

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}
	if err := mgr.Launch("coi-default", false, ""); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}
	time.Sleep(3 * time.Second)

	containerIP, err := GetContainerIP(containerName)
	if err != nil {
		t.Fatalf("Failed to get container IP: %v", err)
	}

	netCfg := &config.NetworkConfig{
		Mode:           config.NetworkModeAllowlist,
		AllowedDomains: []string{testCIDR},
		AllowedPorts:   []int{80, 443},
	}
	netMgr := NewManager(netCfg, logger.NewDiscard())
	if err := netMgr.SetupForContainer(context.Background(), containerName); err != nil {
		t.Fatalf("SetupForContainer failed: %v", err)
	}

	output, err := runNFTCommand("-a", "list", "chain", "ip", "coi", "forward")
	if err != nil {
		t.Fatalf("Failed to list nft chain: %v", err)
	}
	rules := string(output)
	t.Logf("nft chain:\n%s", rules)

	// Bind to the set-accept line for this container that carries the port match.
	var acceptLine string
	for _, line := range linesWithComment(rules, containerIP) {
		if strings.Contains(line, "@coi_s_") && strings.Contains(line, "accept") && strings.Contains(line, "dport") {
			acceptLine = line
			break
		}
	}
	if acceptLine == "" {
		t.Fatalf("expected a port-scoped static-set accept rule with a dport match:\n%s", rules)
	}
	if !strings.Contains(acceptLine, "443") || !strings.Contains(acceptLine, "80") {
		t.Errorf("expected dport 80/443 on the allowlist accept rule, got:\n%s", acceptLine)
	}

	if err := netMgr.Teardown(context.Background(), containerName); err != nil {
		t.Errorf("Teardown failed: %v", err)
	}
	verifyTeardownRemovesRules(t, containerIP)
}

// TestRestrictedModeDNSPinAndPortCap_Integration exercises the user's real target
// setup: restricted mode with a pinned resolver (dns_servers) and an egress port
// cap (allowed_ports). It asserts the pinned resolver is accepted on :53, all
// other :53 is rejected, the internet egress is capped to the allowed ports, and
// a default-deny closes the chain — then that teardown removes everything.
func TestRestrictedModeDNSPinAndPortCap_Integration(t *testing.T) {
	skipUnlessAllowlistReady(t)

	// TEST-NET-3 (RFC 5737): documentation-only, never routed — a safe, unique
	// stand-in for a pinned Pi-hole address.
	const pinnedDNS = "203.0.113.53"

	containerName := "coi-restricted-dns-ports-test"
	mgr := container.NewManager(containerName)

	t.Cleanup(func() { cleanupTestContainer(t, containerName) })

	if exists, _ := mgr.Exists(); exists {
		_ = mgr.Stop(true)
		_ = mgr.Delete(true)
	}
	if err := mgr.Launch("coi-default", false, ""); err != nil {
		t.Fatalf("Failed to launch container: %v", err)
	}
	time.Sleep(3 * time.Second)

	containerIP, err := GetContainerIP(containerName)
	if err != nil {
		t.Fatalf("Failed to get container IP: %v", err)
	}

	netCfg := &config.NetworkConfig{
		Mode:         config.NetworkModeRestricted,
		DNSServers:   []string{pinnedDNS},
		AllowedPorts: []int{80, 443},
	}
	netMgr := NewManager(netCfg, logger.NewDiscard())
	if err := netMgr.SetupForContainer(context.Background(), containerName); err != nil {
		t.Fatalf("SetupForContainer failed: %v", err)
	}

	output, err := runNFTCommand("-a", "list", "chain", "ip", "coi", "forward")
	if err != nil {
		t.Fatalf("Failed to list nft chain: %v", err)
	}
	rules := string(output)
	t.Logf("nft chain:\n%s", rules)

	lines := linesWithComment(rules, containerIP)
	if len(lines) == 0 {
		t.Fatalf("no rules found for container %s:\n%s", containerIP, rules)
	}

	var (
		dnsPinAccept bool // accept :53 to the pinned resolver
		dnsReject    bool // reject :53 to everything else
		portCap      bool // port-capped internet accept
		defaultDeny  bool // final catch-all reject
	)
	for _, line := range lines {
		hasDport53 := strings.Contains(line, "dport 53")
		switch {
		case hasDport53 && strings.Contains(line, pinnedDNS) && strings.Contains(line, "accept"):
			dnsPinAccept = true
		case hasDport53 && strings.Contains(line, "reject"):
			dnsReject = true
		case strings.Contains(line, "dport") && strings.Contains(line, "443") &&
			strings.Contains(line, "80") && strings.Contains(line, "accept"):
			portCap = true
		case strings.Contains(line, "reject") && !hasDport53 && !strings.Contains(line, "/"):
			// The default deny is a bare `ip saddr <ip> ... reject` with no daddr and
			// no dport 53 — distinguishing it from the RFC1918 and DNS rejects.
			defaultDeny = true
		}
	}

	if !dnsPinAccept {
		t.Errorf("expected an accept rule for pinned resolver %s on :53:\n%s", pinnedDNS, rules)
	}
	if !dnsReject {
		t.Errorf("expected a reject rule for :53 to non-pinned resolvers:\n%s", rules)
	}
	if !portCap {
		t.Errorf("expected a port-capped internet accept (dport 80/443):\n%s", rules)
	}
	if !defaultDeny {
		t.Errorf("expected a default-deny reject closing the chain:\n%s", rules)
	}

	if err := netMgr.Teardown(context.Background(), containerName); err != nil {
		t.Errorf("Teardown failed: %v", err)
	}
	verifyTeardownRemovesRules(t, containerIP)
}

// TestAllowlistModeRejectsDNSServers_Integration verifies that combining
// dns_servers with allowlist mode is refused: allowlist mode blocks all DNS by
// design, so re-opening :53 to a pinned resolver would reintroduce exactly the
// host/container divergence the mode prevents. No container launch is required —
// the guard fires before any container state is touched.
func TestAllowlistModeRejectsDNSServers_Integration(t *testing.T) {
	if !NftAvailable() {
		t.Skip("nft not available, skipping integration test")
	}

	netCfg := &config.NetworkConfig{
		Mode:           config.NetworkModeAllowlist,
		AllowedDomains: []string{"203.0.113.0/24"},
		DNSServers:     []string{"203.0.113.53"},
	}
	netMgr := NewManager(netCfg, logger.NewDiscard())

	err := netMgr.SetupForContainer(context.Background(), "coi-nonexistent-dnspin-guard")
	if err == nil {
		t.Fatal("expected SetupForContainer to fail when dns_servers is set in allowlist mode")
	}
	if !strings.Contains(err.Error(), "dns_servers") || !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("expected a dns_servers/allowlist incompatibility error, got: %v", err)
	}
}
