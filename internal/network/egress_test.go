package network

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateDNSServers(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"empty", nil, []string{}, false},
		{"single", []string{"192.168.1.2"}, []string{"192.168.1.2"}, false},
		{"trims and dedups", []string{" 8.8.8.8 ", "8.8.8.8"}, []string{"8.8.8.8"}, false},
		{"multiple order preserved", []string{"192.168.1.2", "1.1.1.1"}, []string{"192.168.1.2", "1.1.1.1"}, false},
		{"rejects hostname", []string{"pihole.local"}, nil, true},
		{"rejects ipv6", []string{"2001:4860:4860::8888"}, nil, true},
		{"rejects garbage", []string{"not-an-ip"}, nil, true},
		{"rejects cidr", []string{"192.168.1.0/24"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDNSServers(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateAllowedPorts(t *testing.T) {
	tests := []struct {
		name    string
		in      []int
		want    []int
		wantErr bool
	}{
		{"empty", nil, []int{}, false},
		{"sorts and dedups", []int{443, 80, 443}, []int{80, 443}, false},
		{"boundaries ok", []int{1, 65535}, []int{1, 65535}, false},
		{"rejects zero", []int{0}, nil, true},
		{"rejects negative", []int{-1}, nil, true},
		{"rejects too large", []int{65536}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAllowedPorts(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPortSetTokens(t *testing.T) {
	tests := []struct {
		in   []int
		want []string
	}{
		{[]int{443}, []string{"{", "443", "}"}},
		{[]int{80, 443}, []string{"{", "80,", "443", "}"}},
		{[]int{53, 80, 443}, []string{"{", "53,", "80,", "443", "}"}},
	}
	for _, tt := range tests {
		if got := portSetTokens(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("portSetTokens(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestL4PortMatchNoPortsIsHistoric pins that with no ports the emitted match is
// byte-identical to the original hard-coded clause, so existing allowlist/restricted
// configs produce exactly the same nft rules as before this change.
func TestL4PortMatchNoPortsIsHistoric(t *testing.T) {
	got := l4PortMatch(nil)
	want := []string{"meta", "l4proto", "{", "tcp,", "udp", "}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("l4PortMatch(nil) = %v, want historic %v", got, want)
	}
}

func TestL4PortMatchWithPorts(t *testing.T) {
	got := l4PortMatch([]int{80, 443})
	want := []string{"meta", "l4proto", "{", "tcp,", "udp", "}", "th", "dport", "{", "80,", "443", "}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("l4PortMatch = %v, want %v", got, want)
	}
	// The port constraint must sit AFTER the l4proto clause, or nft rejects the rule.
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "l4proto { tcp, udp } th dport") {
		t.Errorf("port match not ordered after l4proto clause: %q", joined)
	}
}
