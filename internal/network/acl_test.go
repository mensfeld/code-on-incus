package network

import (
	"errors"
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

func TestBuildACLRules_Restricted(t *testing.T) {
	tests := []struct {
		name                  string
		blockPrivateNetworks  bool
		blockMetadataEndpoint bool
		wantRuleCount         int
		wantContains          []string
	}{
		{
			name:                  "block both private networks and metadata",
			blockPrivateNetworks:  true,
			blockMetadataEndpoint: true,
			wantRuleCount:         5, // 3 RFC1918 + 1 metadata + 1 allow
			wantContains: []string{
				"egress action=reject destination=10.0.0.0/8",
				"egress action=reject destination=172.16.0.0/12",
				"egress action=reject destination=192.168.0.0/16",
				"egress action=reject destination=169.254.0.0/16",
				"egress action=allow",
			},
		},
		{
			name:                  "block only private networks",
			blockPrivateNetworks:  true,
			blockMetadataEndpoint: false,
			wantRuleCount:         4, // 3 RFC1918 + 1 allow
			wantContains: []string{
				"egress action=reject destination=10.0.0.0/8",
				"egress action=allow",
			},
		},
		{
			name:                  "block only metadata",
			blockPrivateNetworks:  false,
			blockMetadataEndpoint: true,
			wantRuleCount:         2, // 1 metadata + 1 allow
			wantContains: []string{
				"egress action=reject destination=169.254.0.0/16",
				"egress action=allow",
			},
		},
		{
			name:                  "block nothing",
			blockPrivateNetworks:  false,
			blockMetadataEndpoint: false,
			wantRuleCount:         1, // just allow
			wantContains: []string{
				"egress action=allow",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NetworkConfig{
				Mode:                  config.NetworkModeRestricted,
				BlockPrivateNetworks:  tt.blockPrivateNetworks,
				BlockMetadataEndpoint: tt.blockMetadataEndpoint,
			}

			rules := buildACLRules(cfg)

			if len(rules) != tt.wantRuleCount {
				t.Errorf("buildACLRules() returned %d rules, want %d", len(rules), tt.wantRuleCount)
			}

			for _, want := range tt.wantContains {
				found := false
				for _, rule := range rules {
					if rule == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("buildACLRules() missing expected rule: %s", want)
				}
			}
		})
	}
}

func TestBuildACLRules_OpenMode(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode:                  config.NetworkModeOpen,
		BlockPrivateNetworks:  true,
		BlockMetadataEndpoint: true,
	}

	rules := buildACLRules(cfg)

	// Open mode should return no rules regardless of block settings
	if len(rules) != 0 {
		t.Errorf("buildACLRules() for open mode returned %d rules, want 0", len(rules))
	}
}

func TestBuildAllowlistRules(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode: config.NetworkModeAllowlist,
	}

	domainIPs := map[string][]string{
		"api.example.com": {"1.2.3.4", "5.6.7.8"},
		"cdn.example.com": {"10.20.30.40"},
	}

	rules := buildAllowlistRules(cfg, domainIPs)

	// Should have: 3 allowed IPs + 4 RFC1918/metadata blocks = 7 rules (no catch-all)
	expectedRules := 7
	if len(rules) != expectedRules {
		t.Errorf("buildAllowlistRules() returned %d rules, want %d", len(rules), expectedRules)
	}

	// Check for allowed IPs
	wantAllowed := []string{
		"egress action=allow destination=1.2.3.4/32",
		"egress action=allow destination=5.6.7.8/32",
		"egress action=allow destination=10.20.30.40/32",
	}

	for _, want := range wantAllowed {
		found := false
		for _, rule := range rules {
			if rule == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("buildAllowlistRules() missing expected allow rule: %s", want)
		}
	}

	// Check for RFC1918 blocks
	wantBlocks := []string{
		"egress action=reject destination=10.0.0.0/8",
		"egress action=reject destination=172.16.0.0/12",
		"egress action=reject destination=192.168.0.0/16",
		"egress action=reject destination=169.254.0.0/16",
	}

	for _, want := range wantBlocks {
		found := false
		for _, rule := range rules {
			if rule == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("buildAllowlistRules() missing expected block rule: %s", want)
		}
	}
}

func TestErrACLNotSupported(t *testing.T) {
	// Test that ErrACLNotSupported can be checked with errors.Is
	err := ErrACLNotSupported

	if !errors.Is(err, ErrACLNotSupported) {
		t.Error("errors.Is(ErrACLNotSupported, ErrACLNotSupported) should return true")
	}

	// Test error message
	expectedMsg := "network ACLs not supported"
	if err.Error() != expectedMsg {
		t.Errorf("ErrACLNotSupported.Error() = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestBuildAllowlistRules_EmptyDomains(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode: config.NetworkModeAllowlist,
	}

	domainIPs := map[string][]string{}

	rules := buildAllowlistRules(cfg, domainIPs)

	// Should still have the blocking rules even with no allowed domains
	// 4 RFC1918/metadata blocks (no catch-all)
	expectedRules := 4
	if len(rules) != expectedRules {
		t.Errorf("buildAllowlistRules() with empty domains returned %d rules, want %d", len(rules), expectedRules)
	}
}

// TestBuildACLRules_RuleOrdering tests that REJECT rules come before ALLOW rules
// This is critical for OVN which evaluates rules in order
func TestBuildACLRules_RuleOrdering(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode:                  config.NetworkModeRestricted,
		BlockPrivateNetworks:  true,
		BlockMetadataEndpoint: true,
	}

	rules := buildACLRules(cfg)

	// Find indices of first reject and first allow
	firstRejectIdx := -1
	firstAllowIdx := -1

	for i, rule := range rules {
		if firstRejectIdx == -1 && (rule == "egress action=reject destination=10.0.0.0/8" ||
			rule == "egress action=reject destination=172.16.0.0/12" ||
			rule == "egress action=reject destination=192.168.0.0/16" ||
			rule == "egress action=reject destination=169.254.0.0/16") {
			firstRejectIdx = i
		}
		if firstAllowIdx == -1 && rule == "egress action=allow" {
			firstAllowIdx = i
		}
	}

	if firstRejectIdx == -1 {
		t.Error("No reject rules found in restricted mode")
	}
	if firstAllowIdx == -1 {
		t.Error("No allow rule found in restricted mode")
	}

	// CRITICAL: Reject rules must come BEFORE allow rules
	// This was the bug - OVN evaluates rules in order
	if firstRejectIdx > firstAllowIdx {
		t.Errorf("Rule ordering bug: REJECT rules (index %d) come after ALLOW rules (index %d). "+
			"OVN evaluates rules in order, so REJECT must come first.",
			firstRejectIdx, firstAllowIdx)
	}
}

// TestBuildAllowlistRules_RuleOrdering tests that ALLOW rules come before REJECT rules
// This is critical for allowlist mode to work correctly
func TestBuildAllowlistRules_RuleOrdering(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode: config.NetworkModeAllowlist,
	}

	domainIPs := map[string][]string{
		"api.example.com": {"1.2.3.4"},
	}

	rules := buildAllowlistRules(cfg, domainIPs)

	// Find indices of first allow and first reject
	firstAllowIdx := -1
	firstRejectIdx := -1

	for i, rule := range rules {
		if firstAllowIdx == -1 && rule == "egress action=allow destination=1.2.3.4/32" {
			firstAllowIdx = i
		}
		if firstRejectIdx == -1 && (rule == "egress action=reject destination=10.0.0.0/8" ||
			rule == "egress action=reject destination=172.16.0.0/12" ||
			rule == "egress action=reject destination=192.168.0.0/16" ||
			rule == "egress action=reject destination=169.254.0.0/16") {
			firstRejectIdx = i
		}
	}

	if firstAllowIdx == -1 {
		t.Error("No allow rules found in allowlist mode")
	}
	if firstRejectIdx == -1 {
		t.Error("No reject rules found in allowlist mode")
	}

	// CRITICAL: Allow rules must come BEFORE reject rules in allowlist mode
	// This ensures allowed IPs can be reached before they're blocked by RFC1918 rules
	if firstAllowIdx > firstRejectIdx {
		t.Errorf("Rule ordering bug: ALLOW rules (index %d) come after REJECT rules (index %d). "+
			"OVN evaluates rules in order, so ALLOW must come first in allowlist mode.",
			firstAllowIdx, firstRejectIdx)
	}
}

// TestBuildAllowlistRules_IPDeduplication tests that duplicate IPs are handled correctly
// Multiple domains can resolve to the same IP (e.g., api.anthropic.com and platform.anthropic.com)
func TestBuildAllowlistRules_IPDeduplication(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode: config.NetworkModeAllowlist,
	}

	// Simulate multiple domains resolving to same IP
	domainIPs := map[string][]string{
		"api.example.com":      {"160.79.104.10"},
		"platform.example.com": {"160.79.104.10"}, // Same IP as api
		"other.example.com":    {"1.2.3.4"},
	}

	rules := buildAllowlistRules(cfg, domainIPs)

	// Count how many times the duplicate IP appears in rules
	duplicateIPCount := 0
	targetRule := "egress action=allow destination=160.79.104.10/32"
	uniqueIPCount := 0
	uniqueRule := "egress action=allow destination=1.2.3.4/32"

	for _, rule := range rules {
		if rule == targetRule {
			duplicateIPCount++
		}
		if rule == uniqueRule {
			uniqueIPCount++
		}
	}

	// Should appear exactly once, not twice
	if duplicateIPCount != 1 {
		t.Errorf("IP deduplication failed: found %d rules for 160.79.104.10, want 1", duplicateIPCount)
		t.Logf("Rules: %v", rules)
	}

	// Unique IP should also be present
	if uniqueIPCount != 1 {
		t.Errorf("Unique IP missing: found %d rules for 1.2.3.4, want 1", uniqueIPCount)
		t.Logf("Rules: %v", rules)
	}
}

// TestBuildAllowlistRules_NoCatchAllReject verifies we don't have an explicit catch-all reject
// OVN applies implicit default-deny when ACLs are attached to a NIC, so we don't need
// an explicit 0.0.0.0/0 reject rule (which would interfere with OVN routing)
func TestBuildAllowlistRules_NoCatchAllReject(t *testing.T) {
	cfg := &config.NetworkConfig{
		Mode: config.NetworkModeAllowlist,
	}

	domainIPs := map[string][]string{
		"api.example.com": {"1.2.3.4"},
	}

	rules := buildAllowlistRules(cfg, domainIPs)

	// Should NOT have a catch-all reject rule
	for _, rule := range rules {
		if rule == "egress action=reject destination=0.0.0.0/0" {
			t.Error("Found catch-all reject rule which interferes with OVN routing")
			t.Logf("Rules: %v", rules)
		}
	}
}
