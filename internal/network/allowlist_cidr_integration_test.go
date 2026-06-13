package network

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/logger"
)

// TestAllowlistModeCIDRRule_Integration verifies that a CIDR entry in
// allowed_domains (e.g. "203.0.113.0/24") produces an nftables rule with the
// CIDR intact — not mangled into "203.0.113.0/24/32" — and that the rule is
// removed cleanly on teardown.
func TestAllowlistModeCIDRRule_Integration(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}
	if !NftAvailable() {
		t.Skip("nft not available, skipping integration test")
	}
	exists, err := container.ImageExists("coi-default")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	// 203.0.113.0/24 is TEST-NET-3 (RFC 5737) — documentation-only, never routed.
	const testCIDR = "203.0.113.0/24"

	containerName := "coi-allowlist-cidr-test"
	mgr := container.NewManager(containerName)

	t.Cleanup(func() {
		cleanupTestContainer(t, containerName)
	})

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
	t.Logf("Container IP: %s", containerIP)

	boolFalse := false
	netCfg := &config.NetworkConfig{
		Mode:                  config.NetworkModeAllowlist,
		BlockPrivateNetworks:  &boolFalse,
		BlockMetadataEndpoint: &boolFalse,
		AllowedDomains:        []string{testCIDR},
	}
	netMgr := NewManager(netCfg, logger.NewDiscard())

	if err := netMgr.SetupForContainer(context.Background(), containerName); err != nil {
		t.Fatalf("SetupForContainer failed: %v", err)
	}

	// Query the actual nftables chain and look for the CIDR rule.
	output, err := runNFTCommand("-a", "list", "chain", "ip", "coi", "forward")
	if err != nil {
		t.Fatalf("Failed to list nft chain: %v", err)
	}
	rules := string(output)
	t.Logf("nft chain output:\n%s", rules)

	// The CIDR must appear verbatim in the ruleset.
	if !strings.Contains(rules, testCIDR) {
		t.Errorf("Expected CIDR %q to appear in nft rules, but it did not.\nRules:\n%s", testCIDR, rules)
	}

	// Guard against the regression: appending /32 to a CIDR produces "203.0.113.0/24/32".
	mangled := testCIDR + "/32"
	if strings.Contains(rules, mangled) {
		t.Errorf("Regression: found mangled CIDR %q in nft rules — /32 was incorrectly appended to a CIDR.\nRules:\n%s", mangled, rules)
	}

	// Teardown must remove the rules for this container.
	if err := netMgr.Teardown(context.Background(), containerName); err != nil {
		t.Errorf("Teardown failed: %v", err)
	}

	output, err = runNFTCommand("-a", "list", "chain", "ip", "coi", "forward")
	if err == nil {
		remaining := string(output)
		comment := `comment "coi-` + containerIP + `"`
		if strings.Contains(remaining, comment) {
			t.Errorf("Rules for container IP %s still present after teardown", containerIP)
		}
	}

	_ = mgr.Stop(true)
	_ = mgr.Delete(true)
}

// TestAllowlistModeWildcardDomain_Integration verifies that a wildcard domain
// entry (e.g. "*.example.com") resolves the base domain and installs /32 rules
// for its IPs — not a literal "*.example.com" string that would corrupt nftables.
func TestAllowlistModeWildcardDomain_Integration(t *testing.T) {
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skip("incus not found, skipping integration test")
	}
	if !container.Available() {
		t.Skip("incus daemon not running, skipping integration test")
	}
	if !NftAvailable() {
		t.Skip("nft not available, skipping integration test")
	}
	exists, err := container.ImageExists("coi-default")
	if err != nil || !exists {
		t.Skip("coi image not found, skipping integration test (run 'coi build' first)")
	}

	containerName := "coi-allowlist-wildcard-test"
	mgr := container.NewManager(containerName)

	t.Cleanup(func() {
		cleanupTestContainer(t, containerName)
	})

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
	t.Logf("Container IP: %s", containerIP)

	boolFalse := false
	netCfg := &config.NetworkConfig{
		Mode:                  config.NetworkModeAllowlist,
		BlockPrivateNetworks:  &boolFalse,
		BlockMetadataEndpoint: &boolFalse,
		AllowedDomains:        []string{"*.example.com"},
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
	t.Logf("nft chain output:\n%s", rules)

	// The literal wildcard string must never appear in nftables — it is not a valid IP/CIDR.
	if strings.Contains(rules, "*.example.com") {
		t.Errorf("Literal wildcard string \"*.example.com\" appeared in nft rules — it should have been resolved to IPs")
	}

	// There should be at least one rule for this container (the base domain resolved to IPs).
	comment := `comment "coi-` + containerIP + `"`
	if !strings.Contains(rules, comment) {
		t.Errorf("Expected at least one rule with comment %q, but found none.\nRules:\n%s", comment, rules)
	}

	if err := netMgr.Teardown(context.Background(), containerName); err != nil {
		t.Errorf("Teardown failed: %v", err)
	}

	_ = mgr.Stop(true)
	_ = mgr.Delete(true)
}
