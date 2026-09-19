package cleanup

import "testing"

func TestMonitorRuleIP(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"coi prefix",
			`ip saddr 10.47.6.5 limit rate 10/second log prefix "NFT_COI[10.47.6.5]: " # handle 43`,
			"10.47.6.5",
		},
		{
			"dns prefix",
			`ip saddr 10.47.6.5 udp dport 53 log prefix "NFT_DNS[10.47.6.5]: " # handle 42`,
			"10.47.6.5",
		},
		{
			"suspicious prefix",
			`ip saddr 10.47.6.20 log prefix "NFT_SUSPICIOUS[10.47.6.20]: " # handle 41`,
			"10.47.6.20",
		},
		{
			"no marker",
			`ip saddr 10.47.6.9 tcp dport 22 accept # handle 7`,
			"",
		},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := monitorRuleIP(tt.line); got != tt.want {
				t.Errorf("monitorRuleIP(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestClassifyMonitorLine(t *testing.T) {
	running := map[string]bool{"10.47.6.20": true} // .20 is live, .2 is not

	tests := []struct {
		name         string
		line         string
		wantHandle   string
		wantOrphaned bool
	}{
		{
			// Exact-IP match: a stale rule for 10.47.6.2 must be orphaned even
			// though 10.47.6.2 is a substring of the live 10.47.6.20 — the old
			// strings.Contains logic wrongly treated it as live (#696 item 3b).
			"stale ip, prefix-overlap with live ip",
			`ip saddr 10.47.6.2 log prefix "NFT_COI[10.47.6.2]: " # handle 50`,
			"50",
			true,
		},
		{
			"live ip is not orphaned",
			`ip saddr 10.47.6.20 log prefix "NFT_DNS[10.47.6.20]: " # handle 51`,
			"51",
			false,
		},
		{
			"non-monitoring line skipped",
			`ip saddr 10.47.6.20 tcp dport 22 accept # handle 7`,
			"",
			false,
		},
		{
			"monitoring line without handle skipped",
			`ip saddr 10.47.6.2 log prefix "NFT_COI[10.47.6.2]: "`,
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle, orphaned := classifyMonitorLine(tt.line, running)
			if handle != tt.wantHandle || orphaned != tt.wantOrphaned {
				t.Errorf("classifyMonitorLine(%q) = (%q, %v), want (%q, %v)",
					tt.line, handle, orphaned, tt.wantHandle, tt.wantOrphaned)
			}
		})
	}
}
