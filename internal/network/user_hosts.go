package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// hostIPClass classifies a host-entry address for reachability decisions.
type hostIPClass int

const (
	hostPublic   hostIPClass = iota // routable internet address
	hostPrivate                     // RFC1918 (10/8, 172.16/12, 192.168/16) or ULA
	hostMetadata                    // 169.254.0.0/16 link-local, incl. the cloud metadata endpoint
)

// classifyHostIP buckets an address for the per-mode reachability rules. The IP
// is assumed already validated as a parseable IPv4 (config.ValidateNetworkHosts).
func classifyHostIP(ipStr string) hostIPClass {
	ip := net.ParseIP(ipStr)
	switch {
	case ip == nil:
		return hostPublic // unreachable in practice — validation rejects non-IPs
	case ip.IsLinkLocalUnicast():
		return hostMetadata
	case ip.IsPrivate():
		return hostPrivate
	default:
		return hostPublic
	}
}

// checkHostReachable reports whether a host entry's address CAN be made reachable
// under the given mode. It refuses addresses the mode hard-blocks, so we never
// write a /etc/hosts name that resolves to a permanently-unreachable address.
func checkHostReachable(mode config.NetworkMode, ipStr string) error {
	switch classifyHostIP(ipStr) {
	case hostMetadata:
		// Refused in every enforcing mode: pointing a name at 169.254.0.0/16
		// (cloud metadata) is a credential-theft SSRF vector.
		if mode == config.NetworkModeRestricted || mode == config.NetworkModeAllowlist {
			return fmt.Errorf(
				"network.hosts: %s is in the link-local/metadata range (169.254.0.0/16), refused in %s mode "+
					"(cloud-metadata SSRF)", ipStr, mode)
		}
	case hostPrivate:
		// allowlist hard-blocks RFC1918 (the H1 rule) before any allow, so a host
		// entry there is a dead name — refuse it. restricted CAN reach it via a
		// targeted rule (applied below).
		if mode == config.NetworkModeAllowlist {
			return fmt.Errorf(
				"network.hosts: %s is a private (RFC1918) address, which allowlist mode always blocks — "+
					"a host entry there can never be reached; use restricted or open mode for a private target", ipStr)
		}
	}
	return nil
}

// ApplyUserHosts writes the static host entries ([[network.hosts]] / `coi hosts`,
// #605) into the container's /etc/hosts and makes each address reachable under the
// active network mode. Reachability is validated for EVERY entry up front, so a
// refused entry aborts before any /etc/hosts or firewall change. Works on a
// running container. An empty slice clears the managed block.
func ApplyUserHosts(containerName string, mode config.NetworkMode, entries []config.HostEntry) error {
	if len(entries) == 0 {
		return WriteUserHosts(containerName, nil)
	}
	if mode == "" {
		mode = config.NetworkModeRestricted // the config default
	}

	// 1. Fail before touching anything if any entry can't be made reachable.
	for _, e := range entries {
		if err := checkHostReachable(mode, e.IP); err != nil {
			return err
		}
	}

	// 2. Firewall reachability (open mode needs none — nothing is blocked there).
	if mode == config.NetworkModeRestricted || mode == config.NetworkModeAllowlist {
		containerIP, err := GetContainerIP(containerName)
		if err != nil {
			return fmt.Errorf("failed to get container IP for host reachability: %w", err)
		}
		for _, e := range entries {
			if err := applyHostFirewall(mode, containerIP, e); err != nil {
				return err
			}
		}
	}

	// 3. Write /etc/hosts (all modes).
	return WriteUserHosts(containerName, entries)
}

// ReadUserHosts returns the host entries currently in the container's COI
// user-hosts /etc/hosts block (empty if none). Used by `coi hosts list/add/remove`
// to read-modify-write the block on a running container.
func ReadUserHosts(containerName string) ([]config.HostEntry, error) {
	mgr := container.NewManager(containerName)
	out, err := mgr.ExecCommand("cat /etc/hosts", container.ExecCommandOptions{Capture: true})
	if err != nil {
		return nil, fmt.Errorf("failed to read /etc/hosts: %w", err)
	}
	return parseUserHostsBlock(out), nil
}

// parseUserHostsBlock extracts the entries between the user-hosts markers. Pure,
// so it is unit-testable without a container.
func parseUserHostsBlock(hostsFile string) []config.HostEntry {
	var entries []config.HostEntry
	inBlock := false
	for _, line := range strings.Split(hostsFile, "\n") {
		switch {
		case strings.HasPrefix(line, userHostsBeginMarker):
			inBlock = true
		case strings.HasPrefix(line, userHostsEndMarker):
			inBlock = false
		case inBlock:
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				entries = append(entries, config.HostEntry{IP: fields[0], Hostnames: fields[1:]})
			}
		}
	}
	return entries
}

// AddUserHost adds (or, for an existing IP, extends the hostnames of) one host
// entry on a running container: it applies firewall reachability for the address
// under the active mode, then merges the entry into the /etc/hosts user block.
func AddUserHost(containerName string, mode config.NetworkMode, entry config.HostEntry) error {
	if err := config.ValidateNetworkHosts([]config.HostEntry{entry}); err != nil {
		return err
	}
	if mode == "" {
		mode = config.NetworkModeRestricted
	}
	if err := checkHostReachable(mode, entry.IP); err != nil {
		return err
	}
	// Firewall reachability for just this address (open mode needs none).
	if mode == config.NetworkModeRestricted || mode == config.NetworkModeAllowlist {
		containerIP, err := GetContainerIP(containerName)
		if err != nil {
			return fmt.Errorf("failed to get container IP: %w", err)
		}
		if err := applyHostFirewall(mode, containerIP, entry); err != nil {
			return err
		}
	}
	// Merge into the existing block (union hostnames for a repeated IP).
	existing, err := ReadUserHosts(containerName)
	if err != nil {
		return err
	}
	return WriteUserHosts(containerName, mergeHostEntry(existing, entry))
}

// RemoveUserHost drops the given hostnames from the container's user-hosts block
// and rewrites it. Firewall reachability opened for the address is left in place;
// it is session-scoped and torn down when the container stops.
func RemoveUserHost(containerName string, hostnames []string) ([]config.HostEntry, error) {
	existing, err := ReadUserHosts(containerName)
	if err != nil {
		return nil, err
	}
	drop := make(map[string]bool, len(hostnames))
	for _, h := range hostnames {
		drop[h] = true
	}
	var kept []config.HostEntry
	for _, e := range existing {
		var names []string
		for _, n := range e.Hostnames {
			if !drop[n] {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			kept = append(kept, config.HostEntry{IP: e.IP, Hostnames: names})
		}
	}
	if err := WriteUserHosts(containerName, kept); err != nil {
		return nil, err
	}
	return kept, nil
}

// mergeHostEntry returns entries with `add` merged in: if its IP already exists,
// the hostname sets are unioned; otherwise it is appended.
func mergeHostEntry(entries []config.HostEntry, add config.HostEntry) []config.HostEntry {
	for i, e := range entries {
		if e.IP == add.IP {
			seen := make(map[string]bool, len(e.Hostnames))
			for _, n := range e.Hostnames {
				seen[n] = true
			}
			for _, n := range add.Hostnames {
				if !seen[n] {
					entries[i].Hostnames = append(entries[i].Hostnames, n)
				}
			}
			return entries
		}
	}
	return append(entries, add)
}

// applyHostFirewall applies reachability for a single entry under an enforcing
// mode (caller ensures mode is restricted/allowlist and the entry passed
// checkHostReachable).
func applyHostFirewall(mode config.NetworkMode, containerIP string, entry config.HostEntry) error {
	class := classifyHostIP(entry.IP)
	switch {
	case mode == config.NetworkModeAllowlist && class == hostPublic:
		if err := NewNftManager(containerIP, "").AddStaticIPs([]string{entry.IP}); err != nil {
			return fmt.Errorf("failed to allow host %s in allowlist mode: %w", entry.IP, err)
		}
	case mode == config.NetworkModeRestricted && class == hostPrivate:
		if err := insertContainerAcceptRule(containerIP, entry.IP); err != nil {
			return fmt.Errorf("failed to allow private host %s in restricted mode: %w", entry.IP, err)
		}
	}
	return nil
}

// insertContainerAcceptRule PREPENDS an accept rule to the coi forward chain for
// traffic from containerIP to a specific destination, so it is evaluated before
// the mode's RFC1918 reject. Tagged with the same `coi-<ip>` comment the other
// per-container rules use, so it is torn down by the existing cleanup path.
func insertContainerAcceptRule(containerIP, destIP string) error {
	if _, err := runNFTCommand(
		"insert", "rule", "ip", "coi", "forward",
		"ip", "saddr", containerIP,
		"ip", "daddr", destIP+"/32",
		"accept", "comment", fmt.Sprintf(`"coi-%s"`, containerIP),
	); err != nil {
		return fmt.Errorf("nft insert accept rule failed: %w", err)
	}
	return nil
}
