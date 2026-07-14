package network

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/mensfeld/code-on-incus/internal/logger"
)

// recordingAllower captures what the proxy installed, and when relative to the
// answer being written.
type recordingAllower struct {
	ips      []string
	ttl      uint32
	calls    int
	failWith error
}

func (r *recordingAllower) AllowDynamicIPs(ips []string, ttl uint32) error {
	r.calls++
	if r.failWith != nil {
		return r.failWith
	}
	r.ips = append(r.ips, ips...)
	r.ttl = ttl
	return nil
}

// captureWriter is a dns.ResponseWriter that records the reply. It also notes
// how many allower calls had happened by the time the reply was written, which
// is what lets us assert the install-before-answer ordering.
type captureWriter struct {
	dns.ResponseWriter
	msg               *dns.Msg
	allower           *recordingAllower
	allowCallsAtWrite int
}

func (c *captureWriter) WriteMsg(m *dns.Msg) error {
	c.msg = m
	if c.allower != nil {
		c.allowCallsAtWrite = c.allower.calls
	}
	return nil
}
func (c *captureWriter) LocalAddr() net.Addr  { return &net.UDPAddr{IP: net.IPv4zero} }
func (c *captureWriter) RemoteAddr() net.Addr { return &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2)} }
func (c *captureWriter) Close() error         { return nil }
func (c *captureWriter) TsigStatus() error    { return nil }
func (c *captureWriter) TsigTimersOnly(bool)  {}
func (c *captureWriter) Hijack()              {}

// newTestProxy builds a proxy whose upstream is a canned answer, so no live
// resolver is involved.
func newTestProxy(t *testing.T, entries []string, allower dynAllower, upstream func(*dns.Msg) (*dns.Msg, error)) *DNSProxy {
	t.Helper()
	policy, err := NewAllowPolicy(entries)
	if err != nil {
		t.Fatalf("NewAllowPolicy: %v", err)
	}
	return &DNSProxy{
		policy:   policy,
		allower:  allower,
		logger:   logger.NewDiscard(),
		exchange: upstream,
		denyLog:  make(map[string]time.Time),
	}
}

// answerWith builds an upstream that returns the given A records for the query.
func answerWith(ttl uint32, ips ...string) func(*dns.Msg) (*dns.Msg, error) {
	return func(req *dns.Msg) (*dns.Msg, error) {
		resp := new(dns.Msg)
		resp.SetReply(req)
		for _, ip := range ips {
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name: req.Question[0].Name, Rrtype: dns.TypeA,
					Class: dns.ClassINET, Ttl: ttl,
				},
				A: net.ParseIP(ip),
			})
		}
		return resp, nil
	}
}

func query(name string, qtype uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	return m
}

// TestDNSProxy_InstallsAddressesBeforeAnswering is the central invariant.
//
// The container may dial an address the instant it receives it, so the firewall
// must already trust that address by the time the answer leaves the proxy. If
// the order were reversed — answer first, install second — we would have
// reintroduced the very race this design exists to eliminate, just with a much
// smaller window.
func TestDNSProxy_InstallsAddressesBeforeAnswering(t *testing.T) {
	allower := &recordingAllower{}
	p := newTestProxy(t, []string{"*.googleapis.com"}, allower,
		answerWith(300, "172.217.112.4", "172.217.113.4"))

	w := &captureWriter{allower: allower}
	p.ServeDNS(w, query("us-central1-aiplatform.googleapis.com", dns.TypeA))

	if w.msg == nil {
		t.Fatal("no response written")
	}
	if w.msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("Rcode = %s, want NOERROR", dns.RcodeToString[w.msg.Rcode])
	}
	if w.allowCallsAtWrite == 0 {
		t.Error("the answer was written before its addresses were installed in the firewall — this is the race the proxy exists to close")
	}

	want := map[string]bool{"172.217.112.4": true, "172.217.113.4": true}
	if len(allower.ips) != len(want) {
		t.Fatalf("installed %v, want %v", allower.ips, want)
	}
	for _, ip := range allower.ips {
		if !want[ip] {
			t.Errorf("unexpected address installed: %s", ip)
		}
	}
	if allower.ttl != 300 {
		t.Errorf("ttl = %d, want 300 (the element timeout is derived from it)", allower.ttl)
	}
}

// TestDNSProxy_RefusesUnlistedNames verifies that a name outside the policy is
// refused and, crucially, that nothing is added to the firewall for it.
func TestDNSProxy_RefusesUnlistedNames(t *testing.T) {
	allower := &recordingAllower{}
	upstreamCalled := false
	p := newTestProxy(t, []string{"api.anthropic.com"}, allower,
		func(req *dns.Msg) (*dns.Msg, error) {
			upstreamCalled = true
			return answerWith(300, "1.2.3.4")(req)
		})

	w := &captureWriter{allower: allower}
	p.ServeDNS(w, query("exfil.evil.com", dns.TypeA))

	if w.msg.Rcode != dns.RcodeRefused {
		t.Errorf("Rcode = %s, want REFUSED", dns.RcodeToString[w.msg.Rcode])
	}
	if upstreamCalled {
		t.Error("a denied name must not be forwarded upstream")
	}
	if allower.calls != 0 {
		t.Errorf("a denied name must not add anything to the firewall, got %d install(s)", allower.calls)
	}
}

// TestDNSProxy_FailsQueryWhenInstallFails verifies the fail-closed path: if the
// address cannot be installed, the answer must not be handed over. Returning it
// anyway would give the container an address the firewall rejects — a silent
// connection failure, which is precisely the symptom we are fixing.
func TestDNSProxy_FailsQueryWhenInstallFails(t *testing.T) {
	allower := &recordingAllower{failWith: errors.New("nft is on fire")}
	p := newTestProxy(t, []string{"api.anthropic.com"}, allower, answerWith(300, "160.79.104.10"))

	w := &captureWriter{allower: allower}
	p.ServeDNS(w, query("api.anthropic.com", dns.TypeA))

	if w.msg.Rcode != dns.RcodeServerFailure {
		t.Errorf("Rcode = %s, want SERVFAIL when the address cannot be allowed",
			dns.RcodeToString[w.msg.Rcode])
	}
	if len(w.msg.Answer) != 0 {
		t.Error("no answer may be returned when its addresses could not be allowed")
	}
}

// TestDNSProxy_AAAAReturnsNoData verifies that AAAA queries for allowed names are
// answered NODATA. The container's IPv6 egress is dropped at the host veth, so a
// AAAA answer is a dead end that clients stall on before falling back to A.
func TestDNSProxy_AAAAReturnsNoData(t *testing.T) {
	allower := &recordingAllower{}
	upstreamCalled := false
	p := newTestProxy(t, []string{"api.anthropic.com"}, allower,
		func(req *dns.Msg) (*dns.Msg, error) {
			upstreamCalled = true
			return answerWith(300, "1.2.3.4")(req)
		})

	w := &captureWriter{allower: allower}
	p.ServeDNS(w, query("api.anthropic.com", dns.TypeAAAA))

	if w.msg.Rcode != dns.RcodeSuccess {
		t.Errorf("Rcode = %s, want NOERROR (NODATA)", dns.RcodeToString[w.msg.Rcode])
	}
	if len(w.msg.Answer) != 0 {
		t.Error("AAAA must be answered NODATA, not with records")
	}
	if upstreamCalled {
		t.Error("AAAA should be short-circuited, not forwarded")
	}
}

// TestDNSProxy_InstallsCNAMEChainTargets verifies that addresses reached through
// a CNAME are installed. api.anthropic.com is fronted by a CDN, so the address
// the container ends up dialling is the chain's terminal A record, not something
// derivable from the queried name.
func TestDNSProxy_InstallsCNAMEChainTargets(t *testing.T) {
	allower := &recordingAllower{}
	p := newTestProxy(t, []string{"api.anthropic.com"}, allower, func(req *dns.Msg) (*dns.Msg, error) {
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Answer = append(resp.Answer,
			&dns.CNAME{
				Hdr:    dns.RR_Header{Name: dns.Fqdn("api.anthropic.com"), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
				Target: dns.Fqdn("edge.cdn.example"),
			},
			&dns.A{
				Hdr: dns.RR_Header{Name: dns.Fqdn("edge.cdn.example"), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("160.79.104.10"),
			},
		)
		return resp, nil
	})

	w := &captureWriter{allower: allower}
	p.ServeDNS(w, query("api.anthropic.com", dns.TypeA))

	if len(allower.ips) != 1 || allower.ips[0] != "160.79.104.10" {
		t.Errorf("installed %v, want the CNAME chain's terminal address", allower.ips)
	}
}

func TestElementTimeout_ClampsToGraceBounds(t *testing.T) {
	// A zero TTL (Cloudflare serves api.anthropic.com this way) must not produce
	// an element that expires immediately — or mid-request. The grace period is
	// the floor.
	if got := elementTimeout(0); got != dnsElementGrace {
		t.Errorf("elementTimeout(0) = %v, want a full %v grace period", got, dnsElementGrace)
	}
	// A normal TTL gets the grace period added, so an address outlives the record
	// that taught us about it and a rotation cannot strand an open connection.
	if got, want := elementTimeout(300), 300*time.Second+dnsElementGrace; got != want {
		t.Errorf("elementTimeout(300) = %v, want %v", got, want)
	}
	// An absurd TTL is capped so a stale address cannot linger indefinitely.
	if got := elementTimeout(365 * 24 * 3600); got != dnsElementMaxTimeout {
		t.Errorf("elementTimeout(1y) = %v, want the %v cap", got, dnsElementMaxTimeout)
	}
}
