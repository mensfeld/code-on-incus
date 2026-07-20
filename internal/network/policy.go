package network

import (
	"fmt"
	"net"
	"strings"
)

// AllowPolicy is the compiled form of [network] allowed_domains.
//
// It splits the configured entries into the two things allowlist mode treats
// differently:
//
//   - Literal addresses (raw IPv4, CIDR) go straight into the container's static
//     nft set. They involve no name resolution at all.
//   - Names are resolved once, on the host. Their addresses go into the dynamic
//     nft set AND into the container's /etc/hosts. With DNS egress blocked, that
//     hosts file is the container's only route from a name to an address — so the
//     two sides cannot disagree, and the container cannot reach an address the
//     firewall has not already been given.
type AllowPolicy struct {
	staticCIDRs []string
	exact       map[string]bool
	names       []string
}

// NewAllowPolicy compiles allowed_domains entries into an AllowPolicy.
//
// Accepted entry forms:
//
//	1.2.3.4              raw IPv4 address
//	10.0.0.0/8           IPv4 CIDR
//	api.example.com      exact hostname
//
// Wildcards are rejected — see the error text below for why, and for what to
// write instead.
func NewAllowPolicy(entries []string) (*AllowPolicy, error) {
	p := &AllowPolicy{exact: make(map[string]bool)}

	for _, raw := range entries {
		entry := strings.ToLower(strings.TrimSpace(raw))
		entry = strings.TrimSuffix(entry, ".")
		if entry == "" {
			continue
		}

		switch {
		case strings.Contains(entry, "/"):
			ip, ipNet, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q in allowed_domains: %w", raw, err)
			}
			// To4() alone is not enough: it returns non-nil for IPv4-mapped IPv6
			// networks such as ::ffff:0:0/96, which ParseCIDR normalises to
			// 0.0.0.0/0. A 4-byte net.IP is the only reliable IPv4 signal.
			if ip.To4() == nil || len(ipNet.IP) != net.IPv4len {
				return nil, fmt.Errorf("%q is an IPv6 CIDR; allowed_domains is IPv4-only", raw)
			}
			p.staticCIDRs = append(p.staticCIDRs, ipNet.String())

		case net.ParseIP(entry) != nil:
			ip := net.ParseIP(entry)
			if ip.To4() == nil {
				return nil, fmt.Errorf("%q is an IPv6 address; allowed_domains is IPv4-only", raw)
			}
			p.staticCIDRs = append(p.staticCIDRs, ip.To4().String()+"/32")

		case strings.Contains(entry, "*"):
			// Wildcards cannot be honoured, so they are rejected rather than
			// quietly mishandled.
			//
			// Allowlist mode resolves each name up front and writes the answer into
			// the container's /etc/hosts, which — with DNS blocked — is the only
			// place the container can get an address from. A wildcard has no answer
			// to write: you cannot know which subdomains will be asked for, so there
			// is nothing to put in the file and nothing to put in the firewall.
			// Supporting one would require a live resolver running for the whole life
			// of the container, which is exactly the moving part this design removes.
			//
			// The previous implementation pretended otherwise: it stripped the "*."
			// and resolved the BASE domain, whose addresses have no overlap at all
			// with the subdomains actually dialled (googleapis.com resolves to
			// 142.250.130.x; us-central1-aiplatform.googleapis.com to 172.217.112-119.4).
			// The result was a firewall that permitted a set of addresses nothing
			// would ever connect to, and a user who believed they were covered.
			// Failing loudly is strictly better than that.
			return nil, fmt.Errorf(
				"wildcard %q in allowed_domains is not supported: allowlist mode resolves each name up front, "+
					"so it cannot know which subdomains a wildcard will cover. List the exact hostnames "+
					"(for example \"us-central1-aiplatform.googleapis.com\", \"oauth2.googleapis.com\"), "+
					"or allow the provider's published IP ranges as CIDRs (for example \"142.250.0.0/15\")", raw)

		default:
			p.exact[entry] = true
			p.names = append(p.names, entry)
		}
	}

	return p, nil
}

// StaticCIDRs returns the literal address entries, in CIDR form, for the static
// nft set. Raw IPs come back as /32.
func (p *AllowPolicy) StaticCIDRs() []string { return p.staticCIDRs }

// Names returns the hostname entries. These are resolved on the host; the answers
// go into both the container's dynamic nft set and its /etc/hosts.
func (p *AllowPolicy) Names() []string { return p.names }
