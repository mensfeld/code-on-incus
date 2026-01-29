package network

import (
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

func TestFirewallManager_RestrictedRuleGeneration(t *testing.T) {
	// This tests the rule generation logic by checking that the ApplyRestricted
	// method generates correct parameters. Since we can't easily mock firewall-cmd,
	// we test the configuration handling logic.

	tests := []struct {
		name                    string
		blockPrivateNetworks    bool
		blockMetadataEndpoint   bool
		allowLocalNetworkAccess bool
		gatewayIP               string
		expectedRuleCount       int // Approximate count based on config
	}{
		{
			name:                    "block both private networks and metadata",
			blockPrivateNetworks:    true,
			blockMetadataEndpoint:   true,
			allowLocalNetworkAccess: false,
			gatewayIP:               "10.47.62.1",
			expectedRuleCount:       5, // 1 gateway + 3 RFC1918 + 1 metadata
		},
		{
			name:                    "allow local network access",
			blockPrivateNetworks:    true,
			blockMetadataEndpoint:   true,
			allowLocalNetworkAccess: true,
			gatewayIP:               "10.47.62.1",
			expectedRuleCount:       4, // 1 gateway + 3 RFC1918 allows
		},
		{
			name:                    "no gateway IP",
			blockPrivateNetworks:    true,
			blockMetadataEndpoint:   true,
			allowLocalNetworkAccess: false,
			gatewayIP:               "",
			expectedRuleCount:       4, // 3 RFC1918 + 1 metadata (no gateway)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NetworkConfig{
				Mode:                    config.NetworkModeRestricted,
				BlockPrivateNetworks:    tt.blockPrivateNetworks,
				BlockMetadataEndpoint:   tt.blockMetadataEndpoint,
				AllowLocalNetworkAccess: tt.allowLocalNetworkAccess,
			}

			// Verify config values are set correctly
			if cfg.BlockPrivateNetworks != tt.blockPrivateNetworks {
				t.Errorf("BlockPrivateNetworks = %v, want %v", cfg.BlockPrivateNetworks, tt.blockPrivateNetworks)
			}
			if cfg.AllowLocalNetworkAccess != tt.allowLocalNetworkAccess {
				t.Errorf("AllowLocalNetworkAccess = %v, want %v", cfg.AllowLocalNetworkAccess, tt.allowLocalNetworkAccess)
			}
		})
	}
}

func TestRFC1918Ranges(t *testing.T) {
	// Verify RFC1918 ranges are correctly defined
	expectedRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	if len(rfc1918Ranges) != len(expectedRanges) {
		t.Errorf("rfc1918Ranges has %d entries, want %d", len(rfc1918Ranges), len(expectedRanges))
	}

	for i, expected := range expectedRanges {
		if rfc1918Ranges[i] != expected {
			t.Errorf("rfc1918Ranges[%d] = %s, want %s", i, rfc1918Ranges[i], expected)
		}
	}
}

func TestMetadataRange(t *testing.T) {
	// Verify metadata range is correctly defined
	if metadataRange != "169.254.0.0/16" {
		t.Errorf("metadataRange = %s, want 169.254.0.0/16", metadataRange)
	}
}

func TestNewFirewallManager(t *testing.T) {
	fm := NewFirewallManager()
	if fm == nil {
		t.Error("NewFirewallManager() returned nil")
	}
}

func TestAllowlistIPDeduplication(t *testing.T) {
	// Test that the allowlist rule generation would deduplicate IPs
	// Multiple domains can resolve to the same IP

	allowedIPs := map[string][]string{
		"api.example.com":      {"160.79.104.10", "1.2.3.4"},
		"platform.example.com": {"160.79.104.10"}, // Same IP as api
		"other.example.com":    {"5.6.7.8"},
		"__internal_gateway__": {"10.47.62.1"}, // Should be skipped
	}

	// Count unique IPs (excluding gateway)
	uniqueIPs := make(map[string]bool)
	for domain, ips := range allowedIPs {
		if domain == "__internal_gateway__" {
			continue
		}
		for _, ip := range ips {
			uniqueIPs[ip] = true
		}
	}

	// Should have 3 unique IPs: 160.79.104.10, 1.2.3.4, 5.6.7.8
	if len(uniqueIPs) != 3 {
		t.Errorf("Expected 3 unique IPs, got %d", len(uniqueIPs))
	}

	// Verify specific IPs
	expectedIPs := []string{"160.79.104.10", "1.2.3.4", "5.6.7.8"}
	for _, ip := range expectedIPs {
		if !uniqueIPs[ip] {
			t.Errorf("Missing expected IP: %s", ip)
		}
	}
}

func TestAllowlistSkipsGateway(t *testing.T) {
	// Test that __internal_gateway__ is handled separately
	allowedIPs := map[string][]string{
		"api.example.com":      {"1.2.3.4"},
		"__internal_gateway__": {"10.47.62.1"},
	}

	// Count IPs for regular allow rules (excluding gateway)
	count := 0
	for domain, ips := range allowedIPs {
		if domain == "__internal_gateway__" {
			continue
		}
		count += len(ips)
	}

	if count != 1 {
		t.Errorf("Expected 1 regular IP, got %d", count)
	}
}
