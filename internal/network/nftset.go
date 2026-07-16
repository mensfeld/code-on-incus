package network

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Allowlist mode keeps a container's permitted destinations in two nft sets
// rather than in one rule per address:
//
//	coi_s_<ip>  static  — literal IPs and CIDRs from allowed_domains. Permanent.
//	coi_d_<ip>  dynamic — addresses learned from DNS answers COI itself served.
//	                      Each element carries a timeout.
//
// Four rules reference the sets (an L4 rule and a rate-limited ICMP rule per
// set) and never change for the life of the container. Updating the allowlist
// is then an element operation, not a rule rewrite.
//
// This is what closes the fail-closed window the old ReplaceAllowlist
// documented: it appended fresh accept rules *behind* the still-present default
// reject, so a newly-resolved address stayed unreachable until every stale
// handle had been deleted, one sudo-nft exec at a time. With sets there are no
// rules to swap — an added element takes effect atomically, immediately.
//
// Dynamic elements carry a timeout rather than being replaced wholesale so that
// an address rotating out of DNS keeps working for a grace period. Large cloud
// frontends (every Google API endpoint, for instance) round-robin over a pool
// far bigger than any single answer, so a strict replace would keep evicting
// addresses that are still perfectly live and still in use by an open session.
const (
	// dnsElementGrace is added to an answer's TTL to derive the element timeout,
	// so an address outlives the DNS record that taught us about it. It is also
	// the effective floor: a TTL of 0 — which is what Cloudflare serves for
	// api.anthropic.com — still yields a full grace period rather than an element
	// that expires the moment it is installed.
	dnsElementGrace = 10 * time.Minute
	// dnsElementMaxTimeout caps how long a stale address can linger.
	dnsElementMaxTimeout = 12 * time.Hour
)

// nftIdent converts a container IP into a legal nft set identifier.
func nftIdent(containerIP string) string {
	return strings.ReplaceAll(containerIP, ".", "_")
}

func staticSetName(containerIP string) string  { return "coi_s_" + nftIdent(containerIP) }
func dynamicSetName(containerIP string) string { return "coi_d_" + nftIdent(containerIP) }

// elementTimeout derives an element timeout from a DNS answer's TTL: the TTL
// plus a grace period, capped. The grace is what absorbs frontend rotation — an
// address that drops out of DNS keeps working long enough that a request in
// flight, or a session that is about to reconnect, does not simply fail.
func elementTimeout(ttl uint32) time.Duration {
	d := time.Duration(ttl)*time.Second + dnsElementGrace
	if d > dnsElementMaxTimeout {
		d = dnsElementMaxTimeout
	}
	return d
}

// ensureAllowlistSets creates the container's static and dynamic sets.
// `nft add set` is idempotent, so this is safe to call repeatedly.
func (f *NftManager) ensureAllowlistSets() error {
	if err := ensureCOITableAndChain(); err != nil {
		return err
	}
	// The static set holds CIDRs, so it needs `flags interval`. The dynamic set
	// holds /32s with per-element expiry, so it needs `flags timeout`. They are
	// kept separate deliberately: combining interval and timeout flags in one
	// set is poorly supported across kernel versions.
	if _, err := runNFTCommand("add", "set", "ip", "coi", staticSetName(f.containerIP),
		"{", "type", "ipv4_addr", ";", "flags", "interval", ";", "}"); err != nil {
		return fmt.Errorf("failed to create static allowlist set: %w", err)
	}
	if _, err := runNFTCommand("add", "set", "ip", "coi", dynamicSetName(f.containerIP),
		"{", "type", "ipv4_addr", ";", "flags", "timeout", ";", "}"); err != nil {
		return fmt.Errorf("failed to create dynamic allowlist set: %w", err)
	}
	return nil
}

// addSetElements adds elements to one of the container's sets. For the dynamic
// set, re-adding an element that is already present updates its timeout rather
// than failing, which is what makes refresh idempotent and cheap.
func (f *NftManager) addSetElements(setName string, elements []string) error {
	if len(elements) == 0 {
		return nil
	}
	args := []string{"add", "element", "ip", "coi", setName, "{", strings.Join(elements, ", "), "}"}
	if _, err := runNFTCommand(args...); err != nil {
		return fmt.Errorf("failed to add elements to %s: %w", setName, err)
	}
	return nil
}

// AddStaticIPs installs the literal address entries from allowed_domains.
// These never expire — the user named them explicitly.
func (f *NftManager) AddStaticIPs(cidrs []string) error {
	if f.containerIP == "" || len(cidrs) == 0 {
		return nil
	}
	sorted := append([]string(nil), cidrs...)
	sort.Strings(sorted) // deterministic, for tests and for readable nft output
	return f.addSetElements(staticSetName(f.containerIP), sorted)
}

// AllowDynamicIPs adds DNS-learned addresses to the dynamic set with a timeout
// derived from the answer's TTL. It returns only once the kernel has the
// addresses, so a caller that writes them into the container's /etc/hosts
// afterwards knows the firewall already trusts what it is about to hand over.
//
// The lock is deliberately held across the nft call. Setup installs the initial
// answers while the background refresher may already be re-resolving on its own
// goroutine, so two calls that touch the same address can overlap. An earlier
// version recorded the addresses in dynSeen, released the lock, and only then
// shelled out to nft — which let a concurrent caller see them as "already
// installed", skip the nft call, and proceed while the first exec was still in
// flight. The container could then be handed an address the kernel set did not
// contain yet: the original bug, reproduced with a smaller window. Serialising
// here costs one exec's latency and buys the invariant the whole design rests on.
//
// dynSeen is what keeps this cheap: it is a record of what has already been
// installed, so re-syncing a name whose elements still have most of their life
// left costs no exec at all.
func (f *NftManager) AllowDynamicIPs(ips []string, ttl uint32) error {
	if f.containerIP == "" || len(ips) == 0 {
		return nil
	}

	timeout := elementTimeout(ttl)

	f.dynMu.Lock()
	defer f.dynMu.Unlock()

	if f.dynSeen == nil {
		f.dynSeen = make(map[string]time.Time)
	}
	now := time.Now()

	// Drop addresses whose kernel elements have already expired. Without this the
	// map only ever grows — one entry per address ever resolved — which for an
	// agent working through many wildcard subdomains is an unbounded leak.
	for ip, expiry := range f.dynSeen {
		if now.After(expiry) {
			delete(f.dynSeen, ip)
		}
	}

	var stale []string
	for _, ip := range ips {
		expiry, known := f.dynSeen[ip]
		// Refresh once an element is past half its life. Re-adding resets the
		// kernel timeout, so a live address never expires while still in use.
		if !known || now.After(expiry.Add(-timeout/2)) {
			stale = append(stale, ip)
		}
	}
	if len(stale) == 0 {
		return nil
	}

	secs := int(timeout.Seconds())
	elements := make([]string, 0, len(stale))
	for _, ip := range stale {
		elements = append(elements, fmt.Sprintf("%s timeout %ds", ip, secs))
	}

	if err := f.addSetElements(dynamicSetName(f.containerIP), elements); err != nil {
		return err
	}

	// Record only after the kernel has them, so a failed exec cannot leave an
	// address marked as installed when it is not.
	for _, ip := range stale {
		f.dynSeen[ip] = now.Add(timeout)
	}
	return nil
}

// removeAllowlistSetsForIP deletes a container's allowlist sets.
//
// Rules referencing a set must be gone first or the kernel refuses with EBUSY,
// so callers delete rules before calling this. A set that was never created (a
// container in open or restricted mode) is not an error during teardown.
func removeAllowlistSetsForIP(containerIP string) error {
	if containerIP == "" {
		return nil
	}
	var firstErr error
	for _, name := range []string{staticSetName(containerIP), dynamicSetName(containerIP)} {
		if _, err := runNFTCommand("delete", "set", "ip", "coi", name); err != nil {
			if isNftNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			logWarnf("Warning: failed to delete nft set %s: %v", name, err)
		}
	}
	return firstErr
}

// isNftNotFound reports whether an nft error means "it isn't there", which is
// success as far as teardown is concerned.
func isNftNotFound(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "No such file") || strings.Contains(msg, "does not exist")
}

// dynSeenSnapshot returns the dynamic addresses this manager believes it has
// installed, for diagnostics and tests.
func (f *NftManager) dynSeenSnapshot() map[string]time.Time {
	f.dynMu.Lock()
	defer f.dynMu.Unlock()
	out := make(map[string]time.Time, len(f.dynSeen))
	for k, v := range f.dynSeen {
		out[k] = v
	}
	return out
}

// dynAllower is the slice of NftManager the resolver/refresher path needs to
// install DNS-learned addresses. Keeping it narrow lets the Manager's sync path
// be tested with a stub in place of real nft.
type dynAllower interface {
	AllowDynamicIPs(ips []string, ttl uint32) error
}

var _ dynAllower = (*NftManager)(nil)
