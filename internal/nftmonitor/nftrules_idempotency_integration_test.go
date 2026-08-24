package nftmonitor

import (
	"os/exec"
	"strings"
	"testing"
)

// sudoNftAvailable reports whether real nft can be driven via passwordless sudo.
func sudoNftAvailable() bool {
	return exec.Command("sudo", "-n", "nft", "list", "ruleset").Run() == nil
}

// countMonitorRulesForIP counts NFT_COI/NFT_DNS/NFT_SUSPICIOUS LOG rules in
// ip filter FORWARD whose bracketed token is exactly ip.
func countMonitorRulesForIP(t *testing.T, ip string) int {
	t.Helper()
	out, err := exec.Command("sudo", "-n", "nft", "-a", "list", "chain", "ip", "filter", "FORWARD").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		for _, prefix := range []string{"NFT_COI[", "NFT_DNS[", "NFT_SUSPICIOUS["} {
			if strings.Contains(line, prefix+ip+"]") {
				n++
				break
			}
		}
	}
	return n
}

// TestAddRules_Idempotent proves the #696 item-3a guard: running AddRules twice
// for the same container IP must not stack duplicate LOG rules in
// ip filter FORWARD. Uses a TEST-NET-3 IP so it never touches real container
// rules. Requires real nft + passwordless sudo; skips otherwise.
func TestAddRules_Idempotent(t *testing.T) {
	if !sudoNftAvailable() {
		t.Skip("nft with passwordless sudo not available, skipping integration test")
	}

	const testIP = "203.0.113.7" // TEST-NET-3, safe fingerprint
	rm := NewRuleManager(&Config{ContainerIP: testIP, LogDNSQueries: true})

	// Clean slate + guaranteed teardown (RemoveRules errors when nothing matches;
	// that's fine here).
	_ = rm.RemoveRules()
	t.Cleanup(func() { _ = rm.RemoveRules() })

	if err := rm.AddRules(); err != nil {
		t.Fatalf("first AddRules failed: %v", err)
	}
	first := countMonitorRulesForIP(t, testIP)
	if first == 0 {
		t.Fatalf("expected LOG rules for %s after first AddRules, found none", testIP)
	}

	if err := rm.AddRules(); err != nil {
		t.Fatalf("second AddRules failed: %v", err)
	}
	second := countMonitorRulesForIP(t, testIP)
	if second != first {
		t.Errorf("AddRules is not idempotent: %d rules after first call, %d after second (#696 item 3a)", first, second)
	}
}
