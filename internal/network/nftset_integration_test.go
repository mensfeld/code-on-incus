package network

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

// These tests drive the real nft binary. They do not need Incus or a live
// container: every rule is keyed on a source address, so a synthetic address
// exercises the exact code path a real container would take.
const testContainerIP = "10.99.99.99"

const testGatewayIP = "10.99.99.1"

func skipUnlessNft(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft not found, skipping nft integration test")
	}
	if err := exec.Command("sudo", "-n", "nft", "list", "tables").Run(); err != nil {
		t.Skip("passwordless sudo for nft unavailable, skipping nft integration test")
	}
}

func nftDump(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("sudo", "-n", "nft", "list", "table", "ip", "coi").CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// TestAllowlistSets_Lifecycle exercises the set-based allowlist end to end
// against the real kernel: rules reference the sets, static and dynamic elements
// land in the right set, and teardown removes both rules and sets.
func TestAllowlistSets_Lifecycle(t *testing.T) {
	skipUnlessNft(t)

	f := NewNftManager(testContainerIP, testGatewayIP)
	t.Cleanup(func() { _ = f.RemoveRules() })

	cfg := &config.NetworkConfig{Mode: config.NetworkModeAllowlist}
	if err := f.ApplyAllowlist(cfg, []string{"8.8.8.8/32", "192.0.2.0/24"}); err != nil {
		t.Fatalf("ApplyAllowlist: %v", err)
	}

	dump := nftDump(t)

	// The rules must name the sets, not individual addresses. That is what makes
	// them stable for the container's lifetime and lets updates be pure element
	// operations, with no window where a stale reject shadows a fresh accept.
	for _, want := range []string{
		"@" + staticSetName(testContainerIP),
		"@" + dynamicSetName(testContainerIP),
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("expected a rule referencing %s\n%s", want, dump)
		}
	}

	// Literal entries land in the static set.
	for _, want := range []string{"8.8.8.8", "192.0.2.0/24"} {
		if !strings.Contains(dump, want) {
			t.Errorf("expected static element %s in the ruleset\n%s", want, dump)
		}
	}

	// Default deny must still be the last word.
	if !strings.Contains(dump, "reject") {
		t.Errorf("expected a default reject rule\n%s", dump)
	}

	// A DNS-learned address goes into the dynamic set, with a timeout.
	if err := f.AllowDynamicIPs([]string{"172.217.112.4"}, 300); err != nil {
		t.Fatalf("AllowDynamicIPs: %v", err)
	}
	dump = nftDump(t)
	if !strings.Contains(dump, "172.217.112.4") {
		t.Errorf("expected the DNS-learned address in the dynamic set\n%s", dump)
	}
	if !strings.Contains(dump, "timeout") {
		t.Errorf("dynamic elements must carry a timeout so a rotated-out address expires\n%s", dump)
	}

	// Teardown must remove the rules *and* the sets. The kernel refuses to drop a
	// set that a rule still references, so getting this order wrong leaks sets
	// across container restarts.
	if err := f.RemoveRules(); err != nil {
		t.Fatalf("RemoveRules: %v", err)
	}
	dump = nftDump(t)
	if strings.Contains(dump, testContainerIP) {
		t.Errorf("rules for %s survived teardown\n%s", testContainerIP, dump)
	}
	for _, set := range []string{staticSetName(testContainerIP), dynamicSetName(testContainerIP)} {
		if strings.Contains(dump, set) {
			t.Errorf("set %s survived teardown\n%s", set, dump)
		}
	}
}

// TestAllowDynamicIPs_IsIdempotent verifies that re-adding an address refreshes
// its timeout rather than failing. The DNS proxy calls this on every answer, so
// a second query for a name it already resolved must not error.
func TestAllowDynamicIPs_IsIdempotent(t *testing.T) {
	skipUnlessNft(t)

	f := NewNftManager(testContainerIP, testGatewayIP)
	t.Cleanup(func() { _ = f.RemoveRules() })

	cfg := &config.NetworkConfig{Mode: config.NetworkModeAllowlist}
	if err := f.ApplyAllowlist(cfg, nil); err != nil {
		t.Fatalf("ApplyAllowlist: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := f.AllowDynamicIPs([]string{"172.217.112.4"}, 300); err != nil {
			t.Fatalf("AllowDynamicIPs call %d: %v", i+1, err)
		}
	}

	// The hot-path cache means only the first call should have reached nft; the
	// element still has nearly all of its life left on calls 2 and 3.
	if got := len(f.dynSeenSnapshot()); got != 1 {
		t.Errorf("expected 1 tracked dynamic address, got %d", got)
	}
	if !strings.Contains(nftDump(t), "172.217.112.4") {
		t.Error("the address should be present after repeated adds")
	}
}

// TestDNSIntercept_Lifecycle verifies the DNAT redirect that makes the allowlist
// enforceable rather than merely cooperative: it catches a container's DNS no
// matter which nameserver the container aims at.
func TestDNSIntercept_Lifecycle(t *testing.T) {
	skipUnlessNft(t)

	t.Cleanup(func() { _ = RemoveDNSIntercept(testContainerIP) })

	if err := EnsureDNSIntercept(testContainerIP, testGatewayIP, 15353); err != nil {
		t.Fatalf("EnsureDNSIntercept: %v", err)
	}
	// Idempotent — setup may run more than once.
	if err := EnsureDNSIntercept(testContainerIP, testGatewayIP, 15353); err != nil {
		t.Fatalf("EnsureDNSIntercept (second call): %v", err)
	}

	out, err := exec.Command("sudo", "-n", "nft", "list", "table", "ip", coiNatTable).CombinedOutput()
	if err != nil {
		t.Fatalf("listing %s: %v", coiNatTable, err)
	}
	dump := string(out)

	if strings.Count(dump, dnsInterceptComment(testContainerIP)) != 1 {
		t.Errorf("expected exactly one intercept rule after two calls\n%s", dump)
	}
	if !strings.Contains(dump, "dnat to 10.99.99.1:15353") {
		t.Errorf("expected the DNAT redirect to the COI resolver\n%s", dump)
	}
	// The redirect must not be scoped to port 53 on a particular nameserver — the
	// whole point is that it catches an agent pointing resolv.conf anywhere.
	if !strings.Contains(dump, "dport 53") {
		t.Errorf("expected the redirect to match destination port 53\n%s", dump)
	}

	if err := RemoveDNSIntercept(testContainerIP); err != nil {
		t.Fatalf("RemoveDNSIntercept: %v", err)
	}
	out, _ = exec.Command("sudo", "-n", "nft", "list", "table", "ip", coiNatTable).CombinedOutput()
	if strings.Contains(string(out), dnsInterceptComment(testContainerIP)) {
		t.Errorf("intercept rule survived teardown\n%s", out)
	}
}
