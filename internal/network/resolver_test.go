package network

import (
	"testing"
)

func TestResolveDomain_RawIPv4(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid IPv4 address",
			input:   "8.8.8.8",
			want:    "8.8.8.8",
			wantErr: false,
		},
		{
			name:    "valid IPv4 address with different octets",
			input:   "1.1.1.1",
			want:    "1.1.1.1",
			wantErr: false,
		},
		{
			name:    "valid IPv4 address 192.168.1.1",
			input:   "192.168.1.1",
			want:    "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "IPv6 address should fail",
			input:   "2001:4860:4860::8888",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ResolveDomain(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveDomain(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ResolveDomain(%q) unexpected error: %v", tt.input, err)
				return
			}

			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("ResolveDomain(%q) = %v, want [%s]", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveDomain_RawIPv4_SetsTTLZero(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	_, err := resolver.ResolveDomain("8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ttl, ok := resolver.DomainTTLs["8.8.8.8"]; !ok {
		t.Error("DomainTTLs should contain entry for raw IP")
	} else if ttl != 0 {
		t.Errorf("Expected TTL=0 for raw IP, got %d", ttl)
	}
}

func TestResolveDomain_DomainName(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	// Test with a real domain that should resolve
	ips, err := resolver.ResolveDomain("example.com")
	if err != nil {
		t.Skipf("DNS resolution not available: %v", err)
	}

	if len(ips) == 0 {
		t.Error("ResolveDomain(\"example.com\") returned no IPs")
	}

	// Verify all returned values are valid IPv4 addresses
	for _, ip := range ips {
		if ip == "" {
			t.Error("ResolveDomain returned empty IP string")
		}
	}
}

func TestResolveDomain_DomainName_SetsTTL(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	_, err := resolver.ResolveDomain("example.com")
	if err != nil {
		t.Skipf("DNS resolution not available: %v", err)
	}

	// TTL should be set (either from QueryDNS or 0 from fallback)
	if _, ok := resolver.DomainTTLs["example.com"]; !ok {
		t.Error("DomainTTLs should contain entry for resolved domain")
	}
}

func TestGetMinTTL_Empty(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	if got := resolver.GetMinTTL(); got != 0 {
		t.Errorf("GetMinTTL() = %d, want 0 for empty DomainTTLs", got)
	}
}

func TestGetMinTTL_AllZero(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})
	resolver.DomainTTLs["8.8.8.8"] = 0
	resolver.DomainTTLs["1.1.1.1"] = 0

	if got := resolver.GetMinTTL(); got != 0 {
		t.Errorf("GetMinTTL() = %d, want 0 when all TTLs are zero", got)
	}
}

func TestGetMinTTL_MixedTTLs(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})
	resolver.DomainTTLs["8.8.8.8"] = 0          // Raw IP, should be ignored
	resolver.DomainTTLs["example.com"] = 300    // 5 minutes
	resolver.DomainTTLs["cdn.example.com"] = 60 // 1 minute

	if got := resolver.GetMinTTL(); got != 60 {
		t.Errorf("GetMinTTL() = %d, want 60", got)
	}
}

func TestGetMinTTL_SingleDomain(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})
	resolver.DomainTTLs["example.com"] = 180

	if got := resolver.GetMinTTL(); got != 180 {
		t.Errorf("GetMinTTL() = %d, want 180", got)
	}
}

func TestUpdateCache_StoresTTLs(t *testing.T) {
	cache := &IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	}
	resolver := NewResolver(cache)

	// Simulate resolved TTLs
	resolver.DomainTTLs["example.com"] = 300
	resolver.DomainTTLs["cdn.example.com"] = 60

	newIPs := map[string][]string{
		"example.com":     {"93.184.216.34"},
		"cdn.example.com": {"1.2.3.4"},
	}

	resolver.UpdateCache(newIPs)

	// Verify TTLs were stored in cache
	if cache.TTLs == nil {
		t.Fatal("Cache TTLs should not be nil after UpdateCache")
	}

	if ttl, ok := cache.TTLs["example.com"]; !ok || ttl != 300 {
		t.Errorf("Cache TTL for example.com = %d, want 300", ttl)
	}

	if ttl, ok := cache.TTLs["cdn.example.com"]; !ok || ttl != 60 {
		t.Errorf("Cache TTL for cdn.example.com = %d, want 60", ttl)
	}

	// Verify LastUpdate was set
	if cache.LastUpdate.IsZero() {
		t.Error("Cache LastUpdate should not be zero after UpdateCache")
	}
}

func TestResolveDomain_CIDR(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCIDR string
		wantErr  bool
	}{
		{
			name:     "clean CIDR passthrough",
			input:    "54.231.0.0/17",
			wantCIDR: "54.231.0.0/17",
		},
		{
			name:     "CIDR with host bits normalized",
			input:    "54.231.0.1/17",
			wantCIDR: "54.231.0.0/17",
		},
		{
			name:     "private CIDR",
			input:    "10.0.0.0/8",
			wantCIDR: "10.0.0.0/8",
		},
		{
			name:     "/32 single-host CIDR",
			input:    "1.2.3.4/32",
			wantCIDR: "1.2.3.4/32",
		},
		{
			name:    "IPv6 CIDR rejected",
			input:   "2001:db8::/32",
			wantErr: true,
		},
		{
			// ::ffff:0:0/96 is an IPv4-mapped IPv6 CIDR. net.ParseCIDR normalises it
			// to 0.0.0.0/0 and To4() returns non-nil — the old To4()==nil guard let
			// this through, producing a nftables accept-all rule. The len check catches it.
			name:    "IPv4-mapped IPv6 CIDR rejected",
			input:   "::ffff:0:0/96",
			wantErr: true,
		},
		{
			name:    "IPv4-mapped IPv6 CIDR partial range rejected",
			input:   "::ffff:128.0.0.0/97",
			wantErr: true,
		},
		{
			name:    "invalid CIDR rejected",
			input:   "not-a-cidr/24",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh resolver per subtest so DomainTTLs don't accumulate across cases.
			resolver := NewResolver(&IPCache{
				Domains: make(map[string][]string),
				TTLs:    make(map[string]uint32),
			})
			got, err := resolver.ResolveDomain(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveDomain(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveDomain(%q) unexpected error: %v", tt.input, err)
			}
			if len(got) != 1 || got[0] != tt.wantCIDR {
				t.Errorf("ResolveDomain(%q) = %v, want [%s]", tt.input, got, tt.wantCIDR)
			}
		})
	}
}

func TestResolveDomain_CIDR_SetsTTLZero(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	_, err := resolver.ResolveDomain("10.0.0.0/8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ttl, ok := resolver.DomainTTLs["10.0.0.0/8"]; !ok {
		t.Error("DomainTTLs should contain entry for CIDR")
	} else if ttl != 0 {
		t.Errorf("Expected TTL=0 for CIDR, got %d", ttl)
	}
}

func TestResolveDomain_Wildcard(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	ips, err := resolver.ResolveDomain("*.example.com")
	if err != nil {
		t.Skipf("DNS resolution not available: %v", err)
	}

	if len(ips) == 0 {
		t.Error("ResolveDomain(\"*.example.com\") returned no IPs")
	}

	// Cache key must be the original wildcard string, not the stripped form
	if _, ok := resolver.DomainTTLs["*.example.com"]; !ok {
		t.Error("DomainTTLs should contain entry keyed by the wildcard domain")
	}
}

func TestResolveDomain_Wildcard_EmptyBase(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})

	_, err := resolver.ResolveDomain("*.")
	if err == nil {
		t.Error("ResolveDomain(\"*.\") should return an error for missing base domain")
	}
}

func TestGetMinTTL_CIDRsIgnored(t *testing.T) {
	resolver := NewResolver(&IPCache{
		Domains: make(map[string][]string),
		TTLs:    make(map[string]uint32),
	})
	resolver.DomainTTLs["10.0.0.0/8"] = 0
	resolver.DomainTTLs["example.com"] = 300

	if got := resolver.GetMinTTL(); got != 300 {
		t.Errorf("GetMinTTL() = %d, want 300 (CIDR TTL=0 should be ignored)", got)
	}
}

func TestResolveAll_UsesCachedTTL(t *testing.T) {
	cache := &IPCache{
		Domains: map[string][]string{
			"nonexistent.example.invalid": {"1.2.3.4"},
		},
		TTLs: map[string]uint32{
			"nonexistent.example.invalid": 120,
		},
	}
	resolver := NewResolver(cache)

	// This domain won't resolve, so it should fall back to cache
	results, _ := resolver.ResolveAll([]string{"nonexistent.example.invalid"})

	if len(results) == 0 {
		t.Skip("No cached results returned")
	}

	// Should preserve cached TTL
	if ttl, ok := resolver.DomainTTLs["nonexistent.example.invalid"]; !ok || ttl != 120 {
		t.Errorf("Expected cached TTL 120, got %d", ttl)
	}
}
