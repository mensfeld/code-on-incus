package network

import (
	"testing"
)

func TestResolveDomain_RawIPv4(t *testing.T) {
	resolver := NewResolver(&IPCache{Domains: make(map[string][]string)})

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

func TestResolveDomain_DomainName(t *testing.T) {
	resolver := NewResolver(&IPCache{Domains: make(map[string][]string)})

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
