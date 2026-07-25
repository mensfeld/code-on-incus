package network

import (
	"testing"

	"github.com/mensfeld/code-on-incus/internal/config"
)

func TestClassifyHostIP(t *testing.T) {
	cases := map[string]hostIPClass{
		"1.2.3.4":         hostPublic,
		"8.8.8.8":         hostPublic,
		"10.0.0.5":        hostPrivate,
		"172.16.0.1":      hostPrivate,
		"192.168.1.1":     hostPrivate,
		"169.254.169.254": hostMetadata,
		"169.254.0.1":     hostMetadata,
	}
	for ip, want := range cases {
		if got := classifyHostIP(ip); got != want {
			t.Errorf("classifyHostIP(%s) = %d, want %d", ip, got, want)
		}
	}
}

func TestCheckHostReachable(t *testing.T) {
	const noLocal, withLocal = false, true

	// allowlist, allow_local_network_access=false: only public reachable
	// (RFC1918/metadata hard-blocked).
	if err := checkHostReachable(config.NetworkModeAllowlist, noLocal, "1.2.3.4"); err != nil {
		t.Errorf("allowlist public should be reachable: %v", err)
	}
	if err := checkHostReachable(config.NetworkModeAllowlist, noLocal, "10.0.0.5"); err == nil {
		t.Error("allowlist private must be refused without allow_local_network_access")
	}
	if err := checkHostReachable(config.NetworkModeAllowlist, noLocal, "169.254.169.254"); err == nil {
		t.Error("allowlist metadata must be refused")
	}

	// allowlist, allow_local_network_access=true: RFC1918 becomes reachable
	// (nft installs an RFC1918 accept), so a private host entry must be allowed —
	// regression test for mensfeld/code-on-incus#605 (pbarnes-tibco).
	if err := checkHostReachable(config.NetworkModeAllowlist, withLocal, "192.168.1.50"); err != nil {
		t.Errorf("allowlist private WITH allow_local_network_access should be reachable: %v", err)
	}
	if err := checkHostReachable(config.NetworkModeAllowlist, withLocal, "10.0.0.5"); err != nil {
		t.Errorf("allowlist private WITH allow_local_network_access should be reachable: %v", err)
	}
	// ...but metadata stays refused even with local access on (allowlist installs
	// an RFC1918 accept, never a metadata one, so it is a genuine SSRF dead-name).
	if err := checkHostReachable(config.NetworkModeAllowlist, withLocal, "169.254.169.254"); err == nil {
		t.Error("allowlist metadata must be refused even with allow_local_network_access")
	}

	// restricted: public + private reachable (private via targeted allow), metadata refused.
	if err := checkHostReachable(config.NetworkModeRestricted, noLocal, "10.0.0.5"); err != nil {
		t.Errorf("restricted private should be reachable: %v", err)
	}
	if err := checkHostReachable(config.NetworkModeRestricted, noLocal, "169.254.169.254"); err == nil {
		t.Error("restricted metadata must be refused")
	}
	// open: anything goes.
	for _, ip := range []string{"10.0.0.5", "169.254.169.254", "1.2.3.4"} {
		if err := checkHostReachable(config.NetworkModeOpen, noLocal, ip); err != nil {
			t.Errorf("open should allow %s: %v", ip, err)
		}
	}
}

func TestRenderAndParseUserHostsBlock_RoundTrip(t *testing.T) {
	entries := []config.HostEntry{
		{IP: "10.0.0.5", Hostnames: []string{"db.internal", "cache.internal"}},
		{IP: "1.2.3.4", Hostnames: []string{"api.example"}},
	}
	block := renderUserHostsBlock(entries)

	// Parse it back out of a realistic /etc/hosts, with lines around the block.
	hostsFile := "127.0.0.1 localhost\n" + block + "\n1.1.1.1 other\n"
	got := parseUserHostsBlock(hostsFile)
	if len(got) != 2 {
		t.Fatalf("round-trip parsed %d entries, want 2: %+v", len(got), got)
	}
	// render sorts by IP: "1.2.3.4" < "10.0.0.5" (string order)
	if got[0].IP != "1.2.3.4" || got[1].IP != "10.0.0.5" {
		t.Errorf("unexpected IPs/order: %+v", got)
	}
	if len(got[1].Hostnames) != 2 {
		t.Errorf("db entry should keep both hostnames: %+v", got[1])
	}
}

func TestParseUserHostsBlock_NoBlock(t *testing.T) {
	if got := parseUserHostsBlock("127.0.0.1 localhost\n1.1.1.1 x\n"); len(got) != 0 {
		t.Errorf("a file with no coi block must parse to nothing, got %+v", got)
	}
}

func TestMergeHostEntry(t *testing.T) {
	// Same IP → union hostnames (no duplicate).
	base := []config.HostEntry{{IP: "10.0.0.5", Hostnames: []string{"db"}}}
	m := mergeHostEntry(base, config.HostEntry{IP: "10.0.0.5", Hostnames: []string{"db", "cache"}})
	if len(m) != 1 || len(m[0].Hostnames) != 2 {
		t.Errorf("same-IP merge should union hostnames, got %+v", m)
	}

	// New IP → append.
	base2 := []config.HostEntry{{IP: "10.0.0.5", Hostnames: []string{"db"}}}
	m2 := mergeHostEntry(base2, config.HostEntry{IP: "1.2.3.4", Hostnames: []string{"api"}})
	if len(m2) != 2 {
		t.Errorf("new-IP merge should append, got %+v", m2)
	}
}
