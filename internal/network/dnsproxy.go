package network

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/mensfeld/code-on-incus/internal/logger"
)

// DNSProxy is the resolver COI puts in front of an allowlisted container, and
// the reason allowlist mode is exact rather than approximate.
//
// The old design resolved allowed_domains on the *host*, pinned the resulting
// addresses into the firewall, and hoped the container's own resolver would
// later return the same ones. For anything behind a large rotating frontend —
// every Google API endpoint, most CDNs — it does not. The container is handed an
// address the host never saw, the firewall rejects it, and the agent dies
// mid-task with a connection error that heals on its own a few seconds later
// when a retry happens to land on an address that *was* pinned.
//
// Here, the answer and the firewall rule come from the same event. A query for
// an allowlisted name is resolved, its A records are installed in the
// container's dynamic nft set, and only then is the answer written back. The
// container cannot be told about an address that is not already permitted, so
// the race has nowhere to live.
//
// Queries for names outside the policy are REFUSED and logged — which also makes
// this a useful signal about what an agent is reaching for.
type DNSProxy struct {
	policy    *AllowPolicy
	allower   dynAllower
	logger    *logger.SessionLogger
	upstreams []string
	client    *dns.Client

	udpSrv *dns.Server
	tcpSrv *dns.Server
	port   int

	// exchange forwards a query upstream. It is a field so tests can drive
	// ServeDNS without a live resolver; production wiring is exchangeUpstream.
	exchange func(*dns.Msg) (*dns.Msg, error)

	// denyLog rate-limits the "denied" log line per name, so an agent in a retry
	// loop cannot flood the session log.
	denyMu  sync.Mutex
	denyLog map[string]time.Time
}

// denyLogInterval is how often the same denied name is logged.
const denyLogInterval = time.Minute

// NewDNSProxy builds a proxy enforcing policy, installing learned addresses via
// allower. Upstream nameservers are read from the host's /etc/resolv.conf.
func NewDNSProxy(policy *AllowPolicy, allower dynAllower, log *logger.SessionLogger) (*DNSProxy, error) {
	conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, fmt.Errorf("failed to read /etc/resolv.conf for upstream nameservers: %w", err)
	}
	if len(conf.Servers) == 0 {
		return nil, fmt.Errorf("no upstream nameservers found in /etc/resolv.conf")
	}

	upstreams := make([]string, 0, len(conf.Servers))
	for _, s := range conf.Servers {
		upstreams = append(upstreams, net.JoinHostPort(s, conf.Port))
	}

	p := &DNSProxy{
		policy:    policy,
		allower:   allower,
		logger:    log,
		upstreams: upstreams,
		client:    &dns.Client{Timeout: 5 * time.Second},
		denyLog:   make(map[string]time.Time),
	}
	p.exchange = p.exchangeUpstream
	return p, nil
}

// Start binds the proxy to listenIP on a free port and serves UDP and TCP.
// The chosen port is returned by Port() and is what the DNAT redirect targets.
//
// The proxy's own upstream queries originate from the host, not from the
// container, so they do not match the container-scoped DNAT rule and cannot loop
// back into the proxy.
func (p *DNSProxy) Start(listenIP string) error {
	pc, err := net.ListenPacket("udp", net.JoinHostPort(listenIP, "0"))
	if err != nil {
		return fmt.Errorf("failed to bind DNS proxy UDP socket on %s: %w", listenIP, err)
	}
	udpAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	if !ok {
		pc.Close()
		return fmt.Errorf("unexpected UDP listener address type %T", pc.LocalAddr())
	}
	p.port = udpAddr.Port

	ln, err := net.Listen("tcp", net.JoinHostPort(listenIP, strconv.Itoa(p.port)))
	if err != nil {
		pc.Close()
		return fmt.Errorf("failed to bind DNS proxy TCP socket on %s:%d: %w", listenIP, p.port, err)
	}

	p.udpSrv = &dns.Server{PacketConn: pc, Handler: p}
	p.tcpSrv = &dns.Server{Listener: ln, Handler: p}

	go func() {
		if err := p.udpSrv.ActivateAndServe(); err != nil {
			p.logger.Errorf("DNS proxy (UDP) stopped: %v", err)
		}
	}()
	go func() {
		if err := p.tcpSrv.ActivateAndServe(); err != nil {
			p.logger.Errorf("DNS proxy (TCP) stopped: %v", err)
		}
	}()

	return nil
}

// Port is the port the proxy is listening on.
func (p *DNSProxy) Port() int { return p.port }

// Stop shuts both listeners down.
func (p *DNSProxy) Stop() {
	if p.udpSrv != nil {
		_ = p.udpSrv.Shutdown()
	}
	if p.tcpSrv != nil {
		_ = p.tcpSrv.Shutdown()
	}
}

// ServeDNS handles one query from the container.
func (p *DNSProxy) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	if len(req.Question) == 0 {
		p.respond(w, req, dns.RcodeFormatError)
		return
	}
	q := req.Question[0]
	name := q.Name

	if !p.policy.Allows(name) {
		p.logDenied(name)
		p.respond(w, req, dns.RcodeRefused)
		return
	}

	// The container is IPv4-only and all of its IPv6 egress is dropped at the
	// host veth, so an AAAA answer is a dead end that clients will stall on
	// before falling back. Answer NODATA and let them go straight to A.
	if q.Qtype == dns.TypeAAAA {
		p.respond(w, req, dns.RcodeSuccess)
		return
	}

	resp, err := p.exchange(req)
	if err != nil {
		p.logger.Errorf("DNS proxy: upstream lookup for %s failed: %v", name, err)
		p.respond(w, req, dns.RcodeServerFailure)
		return
	}

	// Install every address we are about to hand back, before we hand it back.
	// Any A record in the answer counts, including those reached through a CNAME
	// chain — we are the resolver, so we see the whole chain and the container
	// will dial whatever it ends at.
	if ips, ttl := extractAnswerIPs(resp); len(ips) > 0 {
		if err := p.allower.AllowDynamicIPs(ips, ttl); err != nil {
			// Returning the answer now would hand the container an address the
			// firewall does not trust — exactly the failure this design exists to
			// prevent. Fail the query instead so the client retries.
			p.logger.Errorf("DNS proxy: failed to allow %v for %s: %v", ips, name, err)
			p.respond(w, req, dns.RcodeServerFailure)
			return
		}
	}

	resp.SetReply(req)
	if err := w.WriteMsg(resp); err != nil {
		p.logger.Errorf("DNS proxy: failed to write response for %s: %v", name, err)
	}
}

// exchangeUpstream forwards a query upstream, trying each nameserver in turn and
// retrying over TCP if the answer comes back truncated.
func (p *DNSProxy) exchangeUpstream(req *dns.Msg) (*dns.Msg, error) {
	out := req.Copy()
	out.Id = dns.Id()

	var lastErr error
	for _, server := range p.upstreams {
		resp, _, err := p.client.Exchange(out, server)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.Truncated {
			tcp := &dns.Client{Net: "tcp", Timeout: p.client.Timeout}
			if tcpResp, _, tcpErr := tcp.Exchange(out, server); tcpErr == nil {
				return tcpResp, nil
			}
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no upstream nameservers available")
	}
	return nil, lastErr
}

// extractAnswerIPs pulls the A records out of a response, with the minimum TTL
// across them (which drives the nft element timeout).
func extractAnswerIPs(resp *dns.Msg) ([]string, uint32) {
	var ips []string
	var minTTL uint32
	first := true

	for _, rr := range resp.Answer {
		a, ok := rr.(*dns.A)
		if !ok {
			continue
		}
		ips = append(ips, a.A.String())
		ttl := rr.Header().Ttl
		if first || ttl < minTTL {
			minTTL = ttl
			first = false
		}
	}
	return ips, minTTL
}

// respond writes a bare reply carrying rcode.
func (p *DNSProxy) respond(w dns.ResponseWriter, req *dns.Msg, rcode int) {
	m := new(dns.Msg)
	m.SetRcode(req, rcode)
	if err := w.WriteMsg(m); err != nil {
		p.logger.Errorf("DNS proxy: failed to write rcode %d response: %v", rcode, err)
	}
}

// logDenied records a blocked lookup, at most once per name per denyLogInterval.
func (p *DNSProxy) logDenied(name string) {
	p.denyMu.Lock()
	last, seen := p.denyLog[name]
	now := time.Now()
	if seen && now.Sub(last) < denyLogInterval {
		p.denyMu.Unlock()
		return
	}
	p.denyLog[name] = now
	p.denyMu.Unlock()

	p.logger.Printf("DNS denied: %s (not in allowed_domains)", name)
}
