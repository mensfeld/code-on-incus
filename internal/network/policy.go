package network

import (
	"fmt"
	"net"
	"strings"
)

// AllowPolicy is the compiled form of [network] allowed_domains.
//
// It splits the configured entries into the two things the firewall treats
// very differently:
//
//   - Literal addresses (raw IPv4, CIDR) are installed directly into the
//     container's static nft set. They need no DNS at all.
//   - Names (exact hostnames and "*." wildcards) are enforced at resolution
//     time by DNSProxy: a name that matches the policy has its answer's A
//     records added to the container's dynamic nft set before the answer is
//     handed back, so the container can only ever dial an address the firewall
//     already trusts.
//
// Matching a name at query time — rather than resolving it up front and pinning
// the result — is what makes wildcards work. The old resolve-and-pin path
// handled "*.example.com" by resolving the *base* domain, which for a large
// CDN or cloud frontend returns an address set with no overlap at all with the
// subdomains actually being dialled.
type AllowPolicy struct {
	staticCIDRs []string
	exact       map[string]bool
	// suffixes hold the "." + base form of each wildcard, so a HasSuffix test
	// matches subdomains at any depth without matching a sibling like
	// "notexample.com".
	suffixes []string
	names    []string
}

// NewAllowPolicy compiles allowed_domains entries into an AllowPolicy.
//
// Accepted entry forms:
//
//	1.2.3.4              raw IPv4 address
//	10.0.0.0/8           IPv4 CIDR
//	api.example.com      exact hostname
//	*.example.com        example.com and any subdomain of it, at any depth
//
// A "*" anywhere other than as a leading "*." is rejected. Entries like
// "*-aiplatform.googleapis.com" look plausible but are not wildcards; the old
// resolver passed them to DNS verbatim, where they failed to resolve and were
// then silently dropped from the allowlist, leaving the user with a firewall
// that quietly did not cover what they asked for. Failing loudly is the point.
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

		case strings.HasPrefix(entry, "*."):
			base := entry[2:]
			if base == "" || strings.Contains(base, "*") {
				return nil, fmt.Errorf("invalid wildcard %q in allowed_domains: expected the form \"*.example.com\"", raw)
			}
			// The wildcard covers its own base too: "*.example.com" allows
			// "example.com". This matches how dnsmasq and most firewall
			// allowlists read a wildcard, and is what users expect.
			p.exact[base] = true
			p.suffixes = append(p.suffixes, "."+base)
			p.names = append(p.names, base)

		case strings.Contains(entry, "*"):
			return nil, fmt.Errorf(
				"invalid entry %q in allowed_domains: a wildcard must be written as a leading \"*.\" label "+
					"(for example \"*.googleapis.com\"); partial-label wildcards are not supported", raw)

		default:
			p.exact[entry] = true
			p.names = append(p.names, entry)
		}
	}

	return p, nil
}

// Allows reports whether qname is permitted by the policy. The name may carry a
// trailing dot and any casing; both are normalised.
func (p *AllowPolicy) Allows(qname string) bool {
	name := strings.ToLower(strings.TrimSuffix(qname, "."))
	if name == "" {
		return false
	}
	if p.exact[name] {
		return true
	}
	for _, suffix := range p.suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// StaticCIDRs returns the literal address entries, in CIDR form, for the static
// nft set. Raw IPs come back as /32.
func (p *AllowPolicy) StaticCIDRs() []string { return p.staticCIDRs }

// Names returns the name entries (wildcards reduced to their base domain).
// DNSProxy enforces these; the refresher also resolves them to prewarm the
// dynamic set so the container is not blocked on a cold cache at boot.
func (p *AllowPolicy) Names() []string { return p.names }

// HasNames reports whether any name entry exists. When false the allowlist is
// purely address-based and the DNS proxy has nothing to enforce.
func (p *AllowPolicy) HasNames() bool { return len(p.names) > 0 }
