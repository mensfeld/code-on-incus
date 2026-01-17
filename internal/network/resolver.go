package network

import (
	"context"
	"fmt"
	"log"
	"net"
	"reflect"
	"sort"
	"time"
)

// Resolver handles DNS resolution with caching and fallback
type Resolver struct {
	cache *IPCache
}

// NewResolver creates a new resolver with a cache
func NewResolver(cache *IPCache) *Resolver {
	return &Resolver{cache: cache}
}

// ResolveDomain resolves a single domain to IPv4 addresses
// If the input is already an IPv4 address, it returns it directly
func (r *Resolver) ResolveDomain(domain string) ([]string, error) {
	// Check if input is already an IP address
	if ip := net.ParseIP(domain); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return []string{ipv4.String()}, nil
		}
		return nil, fmt.Errorf("%s is not a valid IPv4 address", domain)
	}

	// Resolve domain name to IPs
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", domain, err)
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
			log.Printf("Warning: Failed to resolve %s: %v", domain, err)
			hasError = true

			// Use cached IPs if available
			if cached, ok := r.cache.Domains[domain]; ok && len(cached) > 0 {
				log.Printf("Using cached IPs for %s: %v", domain, cached)
				results[domain] = cached
				resolvedCount++
				continue
			}

			// Skip domain if no cache available
			log.Printf("Warning: No cached IPs available for %s, skipping", domain)
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

// IPsUnchanged checks if resolved IPs differ from cache
func (r *Resolver) IPsUnchanged(newIPs map[string][]string) bool {
	// Quick check: different number of domains
	if len(newIPs) != len(r.cache.Domains) {
		return false
	}

	// Check each domain
	for domain, newDomainIPs := range newIPs {
		cachedIPs, exists := r.cache.Domains[domain]
		if !exists {
			return false // New domain
		}

		// Sort both slices for comparison
		sortedNew := make([]string, len(newDomainIPs))
		copy(sortedNew, newDomainIPs)
		sort.Strings(sortedNew)

		sortedCached := make([]string, len(cachedIPs))
		copy(sortedCached, cachedIPs)
		sort.Strings(sortedCached)

		// Compare sorted slices
		if !reflect.DeepEqual(sortedNew, sortedCached) {
			return false
		}
	}

	return true
}

// UpdateCache updates the cache with new IPs
func (r *Resolver) UpdateCache(newIPs map[string][]string) {
	r.cache.Domains = newIPs
	r.cache.LastUpdate = time.Now()
}

// GetCache returns the current cache
func (r *Resolver) GetCache() *IPCache {
	return r.cache
}
