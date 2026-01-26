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
			wantRuleCount:         5, // 1 allow + 3 RFC1918 + 1 metadata
			wantContains: []string{
				"egress action=allow",
				"egress action=reject destination=10.0.0.0/8",
				"egress action=reject destination=172.16.0.0/12",
				"egress action=reject destination=192.168.0.0/16",
				"egress action=reject destination=169.254.0.0/16",
			},
		},
		{
			name:                  "block only private networks",
			blockPrivateNetworks:  true,
			blockMetadataEndpoint: false,
			wantRuleCount:         4, // 1 allow + 3 RFC1918
			wantContains: []string{
				"egress action=allow",
				"egress action=reject destination=10.0.0.0/8",
			},
		},
		{
			name:                  "block only metadata",
			blockPrivateNetworks:  false,
			blockMetadataEndpoint: true,
			wantRuleCount:         2, // 1 allow + 1 metadata
			wantContains: []string{
				"egress action=allow",
				"egress action=reject destination=169.254.0.0/16",
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

	// Should have: 1 default-deny + 4 RFC1918/metadata blocks + 3 allowed IPs = 8 rules
	expectedMinRules := 8
	if len(rules) < expectedMinRules {
		t.Errorf("buildAllowlistRules() returned %d rules, want at least %d", len(rules), expectedMinRules)
	}

	// Check for default-deny rule
	foundDefaultDeny := false
	for _, rule := range rules {
		if rule == "egress action=reject destination=0.0.0.0/0" {
			foundDefaultDeny = true
			break
		}
	}
	if !foundDefaultDeny {
		t.Error("buildAllowlistRules() missing default-deny rule")
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
	// 1 default-deny + 4 RFC1918/metadata blocks = 5 rules
	expectedRules := 5
	if len(rules) != expectedRules {
		t.Errorf("buildAllowlistRules() with empty domains returned %d rules, want %d", len(rules), expectedRules)
	}
}
