package network

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/logger"
)

// TestDNSProxy_ResolvedAddressLandsInFirewall is the whole point of the design,
// verified against the real kernel and a real upstream resolver.
//
// It resolves a name through the proxy exactly as a container would, then checks
// that the address it was handed is present in the container's nft set. The old
// resolve-and-pin design could not make this guarantee: it resolved on the host,
// pinned the result, and left the container free to resolve the same name to a
// different address moments later — which is what made allowlist mode flap
// against rotating cloud frontends.
func TestDNSProxy_ResolvedAddressLandsInFirewall(t *testing.T) {
	skipUnlessNft(t)

	// Needs a working upstream resolver.
	if _, err := net.LookupHost("api.anthropic.com"); err != nil {
		t.Skip("no live DNS available, skipping")
	}

	f := NewNftManager(testContainerIP, testGatewayIP)
	t.Cleanup(func() { _ = f.RemoveRules() })

	cfg := &config.NetworkConfig{Mode: config.NetworkModeAllowlist}
	if err := f.ApplyAllowlist(cfg, nil); err != nil {
		t.Fatalf("ApplyAllowlist: %v", err)
	}

	policy, err := NewAllowPolicy([]string{"api.anthropic.com", "*.googleapis.com"})
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}

	proxy, err := NewDNSProxy(policy, f, logger.NewDiscard())
	if err != nil {
		t.Fatalf("NewDNSProxy: %v", err)
	}
	if err := proxy.Start("127.0.0.1"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(proxy.Stop)

	client := &dns.Client{Timeout: 10 * time.Second}
	proxyAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(proxy.Port()))

	ask := func(name string) *dns.Msg {
		t.Helper()
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(name), dns.TypeA)
		resp, _, err := client.Exchange(m, proxyAddr)
		if err != nil {
			t.Fatalf("querying proxy for %s: %v", name, err)
		}
		return resp
	}

	// A wildcard subdomain — the case the old resolver silently got wrong, because
	// it allowlisted the addresses of the *base* domain (googleapis.com) while the
	// container dialled a regional frontend on a completely different network.
	for _, name := range []string{"api.anthropic.com", "us-central1-aiplatform.googleapis.com"} {
		resp := ask(name)
		if resp.Rcode != dns.RcodeSuccess {
			t.Fatalf("%s: Rcode = %s, want NOERROR", name, dns.RcodeToString[resp.Rcode])
		}

		answered, _ := extractAnswerIPs(resp)
		if len(answered) == 0 {
			t.Fatalf("%s: proxy returned no A records", name)
		}

		dump := nftDump(t)
		for _, ip := range answered {
			if !strings.Contains(dump, ip) {
				t.Errorf("%s: proxy handed the container %s but it is not in the firewall set — "+
					"this is exactly the race the DNS-driven allowlist exists to eliminate\n%s",
					name, ip, dump)
			}
		}
		t.Logf("%s -> %v (all present in the nft set)", name, answered)
	}

	// A name outside the policy must be refused, and must add nothing.
	resp := ask("example.com")
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("example.com: Rcode = %s, want REFUSED", dns.RcodeToString[resp.Rcode])
	}
}
