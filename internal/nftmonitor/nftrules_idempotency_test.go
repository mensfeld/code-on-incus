package nftmonitor

import "testing"

// Sample `nft list chain ip filter FORWARD` output carrying a full per-IP LOG
// rule set for 10.0.0.5 (the three prefixes AddRules installs), plus an
// unrelated rule. Mirrors the real prefix format `NFT_<KIND>[<ip>]: `.
const forwardChainWith0005 = `table ip filter {
	chain FORWARD {
		type filter hook forward priority filter; policy accept;
		ip saddr 10.0.0.5 ip daddr 169.254.169.254 log prefix "NFT_SUSPICIOUS[10.0.0.5]: " # handle 41
		ip saddr 10.0.0.5 udp dport 53 log prefix "NFT_DNS[10.0.0.5]: " # handle 42
		ip saddr 10.0.0.5 limit rate 10/second log prefix "NFT_COI[10.0.0.5]: " # handle 43
		ip saddr 10.0.0.9 tcp dport 22 accept # handle 7
	}
}`

func TestMonitorRulesPresentForIP(t *testing.T) {
	tests := []struct {
		name      string
		chainText string
		ip        string
		want      bool
	}{
		{"exact ip present", forwardChainWith0005, "10.0.0.5", true},
		// Prefix-overlap guard: 10.0.0.50 must NOT match a rule for 10.0.0.5.
		{"prefix-overlap ip absent", forwardChainWith0005, "10.0.0.50", false},
		{"unrelated ip absent", forwardChainWith0005, "10.0.0.9", false},
		{"empty ip", forwardChainWith0005, "", false},
		{"empty chain", "", "10.0.0.5", false},
		{"marker-free chain", "chain FORWARD { policy accept; }", "10.0.0.5", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := monitorRulesPresentForIP(tt.chainText, tt.ip); got != tt.want {
				t.Errorf("monitorRulesPresentForIP(_, %q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// A single prefix present (e.g. only NFT_DNS) still counts as "present" so the
// guard never re-inserts on top of a partial set.
func TestMonitorRulesPresentForIP_SinglePrefix(t *testing.T) {
	text := `ip saddr 10.1.2.3 udp dport 53 log prefix "NFT_DNS[10.1.2.3]: " # handle 5`
	if !monitorRulesPresentForIP(text, "10.1.2.3") {
		t.Errorf("expected a lone NFT_DNS[ip] rule to count as present")
	}
}
