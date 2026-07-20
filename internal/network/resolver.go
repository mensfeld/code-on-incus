package network

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Resolver handles DNS resolution with caching and fallback
type Resolver struct {
	cache      *IPCache
	DomainTTLs map[string]uint32
}

// NewResolver creates a new resolver with a cache
func NewResolver(cache *IPCache) *Resolver {
	return &Resolver{
		cache:      cache,
		DomainTTLs: make(map[string]uint32),
	}
}

// ResolveDomain resolves a single domain to IPv4 addresses.
// It tries QueryDNS first for TTL information, falling back to net.LookupIP.
// If the input is already an IPv4 address, it returns it directly with TTL=0.
func (r *Resolver) ResolveDomain(domain string) ([]string, error) {
	// Check if input is already an IP address
	if ip := net.ParseIP(domain); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			r.DomainTTLs[domain] = 0
			return []string{ipv4.String()}, nil
		}
		return nil, fmt.Errorf("%s is not a valid IPv4 address", domain)
	}

	// CIDR passthrough — normalize host bits and skip DNS
	if strings.Contains(domain, "/") {
		_, ipNet, err := net.ParseCIDR(domain)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", domain, err)
		}
		// len check is required — To4() returns non-nil for IPv4-mapped IPv6 addresses
		// like ::ffff:0:0/96, which net.ParseCIDR normalises to 0.0.0.0/0. A 4-byte
		// net.IP is the only reliable signal that the network is a native IPv4 CIDR.
		if len(ipNet.IP) != net.IPv4len {
			return nil, fmt.Errorf("%q is an IPv6 CIDR; only IPv4 is supported", domain)
		}
		r.DomainTTLs[domain] = 0
		return []string{ipNet.String()}, nil
	}

	// A wildcard has no meaningful resolution and must never be passed here.
	//
	// This function used to "support" one by stripping the "*." and resolving the
	// BASE domain, which is where the original bug lived: googleapis.com resolves
	// to 142.250.130.x, while us-central1-aiplatform.googleapis.com — the address
	// the container actually dials — is 172.217.112-119.4. Not one address in
	// common. The allowlist was populated with addresses nothing would ever connect
	// to, and nothing said so. AllowPolicy now rejects wildcards up front; this
	// guard makes sure no other caller can quietly reintroduce the old behaviour.
	if strings.Contains(domain, "*") {
		return nil, fmt.Errorf("cannot resolve wildcard %q: list exact hostnames or a CIDR instead", domain)
	}

	// Try TTL-aware DNS query first
	result, err := QueryDNS(domain)
	if err == nil && len(result.IPs) > 0 {
		r.DomainTTLs[domain] = result.TTL
		logInfof("  %s: resolved %d IPs (TTL: %ds)", domain, len(result.IPs), result.TTL)
		return result.IPs, nil
	}

	// Fall back to standard resolver. This is an informational degradation
	// notice, not a failure — resolution continues below — so it is logged at
	// info level (stdout log) rather than as a warning.
	if err != nil {
		logInfof("  %s: TTL-aware DNS failed (%v), falling back to standard resolver", domain, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs, lookupErr := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	if lookupErr != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", domain, lookupErr)
	}

	ips := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if ipv4 := addr.To4(); ipv4 != nil {
			ips = append(ips, ipv4.String())
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPv4 addresses found for %s", domain)
	}

	// TTL=0 indicates unknown (fallback path)
	r.DomainTTLs[domain] = 0

	return ips, nil
}

// ResolveAll resolves all domains to IPs with caching fallback
func (r *Resolver) ResolveAll(domains []string) (map[string][]string, error) {
	results := make(map[string][]string)
	hasError := false
	resolvedCount := 0

	for _, domain := range domains {
		ips, err := r.ResolveDomain(domain)
		if err != nil {
			logWarnf("Warning: Failed to resolve %s: %v", domain, err)
			hasError = true

			// Use cached IPs if available
			if cached, ok := r.cache.Domains[domain]; ok && len(cached) > 0 {
				logInfof("Using cached IPs for %s: %v", domain, cached)
				results[domain] = cached
				// Preserve cached TTL if available
				if cachedTTL, ok := r.cache.TTLs[domain]; ok {
					r.DomainTTLs[domain] = cachedTTL
				}
				resolvedCount++
				continue
			}

			// Skip domain if no cache available
			logWarnf("Warning: No cached IPs available for %s, skipping", domain)
			continue
		}

		results[domain] = ips
		resolvedCount++
	}

	// If we couldn't resolve any domains and have no cache, return error
	if resolvedCount == 0 {
		return nil, fmt.Errorf("failed to resolve any domains")
	}

	// Return results with partial error indication
	if hasError {
		return results, fmt.Errorf("some domains failed to resolve (using cached IPs where available)")
	}

	return results, nil
}

// GetMinTTL returns the minimum TTL across all resolved domains.
// Domains with TTL=0 (unknown/IP addresses) are ignored.
// Returns 0 if no TTL information is available.
func (r *Resolver) GetMinTTL() uint32 {
	var minTTL uint32
	first := true

	for _, ttl := range r.DomainTTLs {
		if ttl == 0 {
			continue // Skip unknown TTLs (raw IPs or fallback resolution)
		}
		if first || ttl < minTTL {
			minTTL = ttl
			first = false
		}
	}

	return minTTL
}

// UpdateCache updates the cache with new IPs and TTLs
func (r *Resolver) UpdateCache(newIPs map[string][]string) {
	r.cache.Domains = newIPs
	r.cache.TTLs = make(map[string]uint32, len(r.DomainTTLs))
	for domain, ttl := range r.DomainTTLs {
		r.cache.TTLs[domain] = ttl
	}
	r.cache.LastUpdate = time.Now()
}

// GetCache returns the current cache
func (r *Resolver) GetCache() *IPCache {
	return r.cache
}
