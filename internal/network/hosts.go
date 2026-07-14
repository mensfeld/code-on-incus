package network

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// Allowlist mode makes the container's name resolution deterministic instead of
// live: COI resolves the allowed domains on the host, installs those addresses in
// the firewall, and writes the SAME addresses into the container's /etc/hosts.
// All DNS egress is then blocked (see the port-53 rules in nft_filter.go), so the
// hosts file COI wrote is the container's only way to turn a name into an address.
//
// This is what kills the original bug at the root. The old design resolved on the
// host, pinned the result, and left the container to resolve the same name
// independently — and for any domain behind a rotating pool of frontend addresses
// (every Google API endpoint) the container routinely got a different member of
// the pool, one the firewall had never seen. The two sides disagreed, the packet
// hit the default reject, and the agent died mid-task with "Unable to connect".
//
// Here there is only one source of truth. The container cannot resolve a name to
// an address the firewall does not already trust, because there is nowhere else
// for an address to come from. Nothing has to stay running for that to hold: the
// hosts file and the nft set are both already in place and they already agree, so
// the guarantee survives `coi` exiting, the user detaching from tmux, or the
// process being killed. Stale-but-consistent is a working container; the thing
// that broke was fresh-but-divergent.
//
// An agent with root inside the container can of course overwrite /etc/hosts. That
// costs it its own name resolution and gains it nothing — the firewall is still
// the boundary, and an address it invents is an address the firewall rejects. The
// failure is closed and self-inflicted.
const (
	hostsBeginMarker = "# BEGIN coi allowlist (managed by coi — edits will be overwritten)"
	hostsEndMarker   = "# END coi allowlist"
)

// renderHostsBlock builds the managed block for the container's /etc/hosts.
// One line per address, so a name backed by several frontend addresses gives the
// resolver all of them to choose between — exactly the set the firewall allows.
// Pure, so it can be tested without a container.
func renderHostsBlock(domainIPs map[string][]string) string {
	domains := make([]string, 0, len(domainIPs))
	for domain := range domainIPs {
		domains = append(domains, domain)
	}
	sort.Strings(domains) // deterministic output: no spurious rewrites

	var b strings.Builder
	b.WriteString(hostsBeginMarker)
	b.WriteString("\n")
	for _, domain := range domains {
		ips := append([]string(nil), domainIPs[domain]...)
		sort.Strings(ips)
		for _, ip := range ips {
			fmt.Fprintf(&b, "%s\t%s\n", ip, domain)
		}
	}
	b.WriteString(hostsEndMarker)
	return b.String()
}

// WriteAllowlistHosts replaces the COI-managed block in the container's
// /etc/hosts with the given name→address mappings, leaving everything outside the
// markers (localhost, the container's own name) untouched.
//
// The file is rewritten in place via `cat >` rather than `mv` so the inode,
// ownership and permissions survive — some resolvers hold the file open, and
// swapping the inode under them is how you get a container that resolves nothing
// until it is restarted.
func WriteAllowlistHosts(containerName string, domainIPs map[string][]string) error {
	block := renderHostsBlock(domainIPs)

	// The heredoc is quoted ('COI_HOSTS_EOF') so the shell performs no expansion
	// on the content — a domain is not a shell word and must not be treated as one.
	script := fmt.Sprintf(`set -e
tmp=$(mktemp)
sed '/^%s/,/^%s/d' /etc/hosts > "$tmp"
cat >> "$tmp" <<'COI_HOSTS_EOF'
%s
COI_HOSTS_EOF
cat "$tmp" > /etc/hosts
rm -f "$tmp"
`, regexpEscapeForSed(hostsBeginMarker), regexpEscapeForSed(hostsEndMarker), block)

	mgr := container.NewManager(containerName)
	if _, err := mgr.ExecCommand(script, container.ExecCommandOptions{Capture: true}); err != nil {
		return fmt.Errorf("failed to write allowlist hosts entries: %w", err)
	}
	return nil
}

// regexpEscapeForSed escapes the characters that are special in a sed basic
// regular expression. The markers contain "(" and ")" and an em dash; only the
// BRE metacharacters need escaping, but escaping conservatively keeps the sed
// address stable if the markers are ever reworded.
func regexpEscapeForSed(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`.[]*^$\/`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
