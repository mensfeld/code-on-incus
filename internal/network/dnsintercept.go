package network

import (
	"fmt"
	"strings"
)

// DNS interception redirects every DNS query a container makes to COI's own
// resolver, regardless of which nameserver the container thinks it is talking
// to.
//
// This is what makes the DNS-driven allowlist enforceable rather than merely
// cooperative. Pointing the container's /etc/resolv.conf at COI would be enough
// for a well-behaved client, but in-container root can rewrite resolv.conf, and
// the default allowlist historically shipped with 8.8.8.8 and 1.1.1.1 as literal
// entries — so an agent could simply resolve names COI never saw. A prerouting
// DNAT on port 53 catches all of it: the query is redirected to COI's proxy
// whatever destination the client chose.
//
// Rules live in their own "ip coi_nat" table so COI's NAT never entangles with
// Incus's or Docker's.
const coiNatTable = "coi_nat"

func dnsInterceptComment(containerIP string) string {
	return "coi-dns-" + containerIP
}

// ensureCOINatTableAndChain creates the ip coi_nat table and its prerouting
// chain. The chain hooks dstnat so the redirect happens before routing decides
// the packet is destined off-box.
func ensureCOINatTableAndChain() error {
	if _, err := runNFTCommand("add", "table", "ip", coiNatTable); err != nil {
		return fmt.Errorf("failed to create nft table ip %s: %w", coiNatTable, err)
	}
	// `nft add chain` with a hook spec fails if the chain already exists, so
	// probe first.
	if _, err := runNFTCommand("list", "chain", "ip", coiNatTable, "prerouting"); err == nil {
		return nil
	}
	if _, err := runNFTCommand("add", "chain", "ip", coiNatTable, "prerouting",
		"{", "type", "nat", "hook", "prerouting", "priority", "dstnat", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("failed to create nft chain ip %s prerouting: %w", coiNatTable, err)
	}
	return nil
}

// EnsureDNSIntercept redirects the container's UDP and TCP port-53 traffic to
// COI's DNS proxy listening on gatewayIP:port. Idempotent.
func EnsureDNSIntercept(containerIP, gatewayIP string, port int) error {
	if containerIP == "" || gatewayIP == "" || port == 0 {
		return fmt.Errorf("DNS intercept needs container IP, gateway IP and port (got %q, %q, %d)",
			containerIP, gatewayIP, port)
	}
	if err := ensureCOINatTableAndChain(); err != nil {
		return err
	}

	comment := dnsInterceptComment(containerIP)
	if exists, err := nftRuleExistsInChain(coiNatTable, "prerouting", comment); err == nil && exists {
		return nil
	}

	// `th dport` matches the transport header port for both TCP and UDP, so one
	// rule covers both.
	if _, err := runNFTCommand(
		"add", "rule", "ip", coiNatTable, "prerouting",
		"ip", "saddr", containerIP,
		"meta", "l4proto", "{", "tcp,", "udp", "}",
		"th", "dport", "53",
		"dnat", "to", fmt.Sprintf("%s:%d", gatewayIP, port),
		"comment", fmt.Sprintf("%q", comment),
	); err != nil {
		return fmt.Errorf("failed to add DNS intercept rule for %s: %w", containerIP, err)
	}
	return nil
}

// RemoveDNSIntercept removes the DNAT redirect for a container. Safe to call
// when none was installed.
func RemoveDNSIntercept(containerIP string) error {
	if containerIP == "" {
		return nil
	}
	return deleteNFTRulesInChain(coiNatTable, "prerouting", dnsInterceptComment(containerIP))
}

// nftRuleExistsInChain reports whether a rule carrying comment exists in the
// given table's chain.
func nftRuleExistsInChain(table, chain, comment string) (bool, error) {
	output, err := runNFTCommand("list", "chain", "ip", table, chain)
	if err != nil {
		return false, nil // chain doesn't exist yet
	}
	return strings.Contains(string(output), fmt.Sprintf("comment %q", comment)), nil
}

// deleteNFTRulesInChain removes every rule carrying comment from table/chain.
func deleteNFTRulesInChain(table, chain, comment string) error {
	output, err := runNFTCommand("-a", "list", "chain", "ip", table, chain)
	if err != nil {
		return nil // chain doesn't exist — nothing to remove
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, fmt.Sprintf("comment %q", comment)) {
			continue
		}
		if h := extractNFTHandle(line); h != "" {
			if _, delErr := runNFTCommand("delete", "rule", "ip", table, chain, "handle", h); delErr != nil {
				logWarnf("Warning: failed to delete DNS intercept rule handle %s: %v", h, delErr)
			}
		}
	}
	return nil
}
