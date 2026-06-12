package network

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// NftManager manages nftables rules for container network isolation.
// Rules live in the dedicated "ip coi" table, forward chain, keeping COI
// rules isolated from Docker, ufw, and other tools.
type NftManager struct {
	containerIP string
	gatewayIP   string
}

// NewNftManager creates a new nft manager for a container
func NewNftManager(containerIP, gatewayIP string) *NftManager {
	return &NftManager{
		containerIP: containerIP,
		gatewayIP:   gatewayIP,
	}
}

// ApplyRestricted applies restricted mode rules (block RFC1918, allow internet)
func (f *NftManager) ApplyRestricted(cfg *config.NetworkConfig) error {
	if err := EnsureBaseRules(); err != nil {
		log.Printf("Warning: failed to ensure base rules: %v", err)
	}

	if f.gatewayIP != "" {
		if err := f.addRule(f.containerIP, f.gatewayIP+"/32", "accept"); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	if config.BoolVal(cfg.AllowLocalNetworkAccess) {
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			if err := f.addRule(f.containerIP, cidr, "accept"); err != nil {
				return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
			}
		}
	} else if config.BoolVal(cfg.BlockPrivateNetworks) {
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			if err := f.addRule(f.containerIP, cidr, "reject"); err != nil {
				return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
			}
		}
	}

	if config.BoolVal(cfg.BlockMetadataEndpoint) {
		if err := f.addRule(f.containerIP, "169.254.0.0/16", "reject"); err != nil {
			return fmt.Errorf("failed to add metadata block rule: %w", err)
		}
	}

	// Allow all remaining internet traffic (needed when FORWARD policy is DROP)
	if err := f.addRule(f.containerIP, "0.0.0.0/0", "accept"); err != nil {
		return fmt.Errorf("failed to add default allow rule: %w", err)
	}

	return nil
}

// ApplyAllowlist applies allowlist mode rules (allow specific IPs, block all else)
func (f *NftManager) ApplyAllowlist(cfg *config.NetworkConfig, allowedIPs []string) error {
	if err := EnsureBaseRules(); err != nil {
		log.Printf("Warning: failed to ensure base rules: %v", err)
	}

	if f.gatewayIP != "" {
		if err := f.addRule(f.containerIP, f.gatewayIP+"/32", "accept"); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	if config.BoolVal(cfg.AllowLocalNetworkAccess) {
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			if err := f.addRule(f.containerIP, cidr, "accept"); err != nil {
				return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
			}
		}
	}

	// Sort IPs for deterministic rule ordering
	sortedIPs := make([]string, len(allowedIPs))
	copy(sortedIPs, allowedIPs)
	sort.Strings(sortedIPs)

	for _, ip := range sortedIPs {
		dest := ip
		if !strings.Contains(ip, "/") {
			dest = ip + "/32"
		}
		if err := f.addRule(f.containerIP, dest, "accept"); err != nil {
			return fmt.Errorf("failed to add allowlist rule for %s: %w", ip, err)
		}
	}

	if !config.BoolVal(cfg.AllowLocalNetworkAccess) {
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"} {
			if err := f.addRule(f.containerIP, cidr, "reject"); err != nil {
				return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
			}
		}
	}

	// Default deny for allowlist mode
	if err := f.addRule(f.containerIP, "0.0.0.0/0", "reject"); err != nil {
		return fmt.Errorf("failed to add default deny rule: %w", err)
	}

	return nil
}

// RemoveRules removes all nftables rules for this container's IP
func (f *NftManager) RemoveRules() error {
	if f.containerIP == "" {
		return nil
	}
	return deleteNFTRulesByComment("coi-" + f.containerIP)
}

// ReplaceAllowlist atomically replaces the allowlist rules for this container.
// It snapshots the existing rule handles first, appends the new rules, then
// deletes only the old handles. The container therefore always has an active
// rule set during the transition — there is never a window where all rules are
// absent and the chain's default-accept policy would pass all traffic through.
func (f *NftManager) ReplaceAllowlist(cfg *config.NetworkConfig, allowedIPs []string) error {
	if f.containerIP == "" {
		return nil
	}

	// Capture current handles before appending new rules so we can surgically
	// remove them afterwards without touching the freshly-added entries.
	oldHandles, err := nftGetHandlesByComment("coi-" + f.containerIP)
	if err != nil {
		return fmt.Errorf("failed to list current nft rules: %w", err)
	}

	if err := f.ApplyAllowlist(cfg, allowedIPs); err != nil {
		return err
	}

	// Remove only the old handles. New rules are already in the chain, so the
	// container is never left without a filtering rule set.
	for _, h := range oldHandles {
		if _, delErr := runNFTCommand("delete", "rule", "ip", "coi", "forward", "handle", h); delErr != nil {
			log.Printf("Warning: failed to delete stale nft rule handle %s: %v", h, delErr)
		}
	}
	return nil
}

// EnsureBaseRules creates the ip coi table/chain and adds the shared conntrack rule.
func EnsureBaseRules() error {
	if err := ensureCOITableAndChain(); err != nil {
		return err
	}
	exists, err := nftRuleExistsWithComment("coi-base")
	if err != nil || exists {
		return err
	}
	_, err = runNFTCommand("add", "rule", "ip", "coi", "forward",
		"ct", "state", "established,related", "accept",
		"comment", `"coi-base"`)
	return err
}

// EnsureOpenModeRules adds an ACCEPT rule for all traffic from a container in open mode.
func EnsureOpenModeRules(containerIP string) error {
	if err := EnsureBaseRules(); err != nil {
		log.Printf("Warning: failed to ensure base rules: %v", err)
	}
	exists, err := nftRuleExistsWithComment("coi-" + containerIP)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = runNFTCommand("add", "rule", "ip", "coi", "forward",
		"ip", "saddr", containerIP,
		"accept",
		"comment", fmt.Sprintf(`"coi-%s"`, containerIP))
	if err != nil {
		return fmt.Errorf("failed to add open mode rule: %w", err)
	}
	return nil
}

// RemoveOpenModeRules removes the ACCEPT rules for a container in open mode.
func RemoveOpenModeRules(containerIP string) error {
	if containerIP == "" {
		return nil
	}
	return deleteNFTRulesByComment("coi-" + containerIP)
}

// DisableIPv6ForContainer disables IPv6 in the container to prevent firewall bypass.
// All network isolation rules are IPv4-only; IPv6 would circumvent them entirely.
func DisableIPv6ForContainer(containerName string) error {
	mgr := container.NewManager(containerName)
	cmd := "sysctl -w net.ipv6.conf.all.disable_ipv6=1 net.ipv6.conf.default.disable_ipv6=1"
	_, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true})
	return err
}

// addRule appends a nftables rule to ip coi forward for this container.
// Rules are appended in call order, so callers must invoke addRule from most-specific
// to least-specific (gateway → allowlist → RFC1918 block → default).
func (f *NftManager) addRule(source, destination, action string) error {
	args := []string{
		"add", "rule", "ip", "coi", "forward",
		"ip", "saddr", source,
	}
	// 0.0.0.0/0 matches all destinations — omit the daddr match for cleaner nft syntax
	if destination != "0.0.0.0/0" {
		args = append(args, "ip", "daddr", destination)
	}
	args = append(args, action, "comment", fmt.Sprintf(`"coi-%s"`, f.containerIP))
	if _, err := runNFTCommand(args...); err != nil {
		return fmt.Errorf("nft rule add failed: %w", err)
	}
	return nil
}

// ensureCOITableAndChain creates the ip coi table and forward chain if absent.
// The chain hooks into the forward path at priority 10, which runs after the
// nftmonitor chain at priority 0 so traffic is logged before being filtered.
func ensureCOITableAndChain() error {
	if _, err := runNFTCommand("add", "table", "ip", "coi"); err != nil {
		return fmt.Errorf("failed to create nft table ip coi: %w", err)
	}
	// nft add chain with hook spec fails if the chain already exists — check first.
	if _, err := runNFTCommand("list", "chain", "ip", "coi", "forward"); err == nil {
		return nil
	}
	_, err := runNFTCommand("add", "chain", "ip", "coi", "forward",
		"{", "type", "filter", "hook", "forward", "priority", "10", ";", "policy", "accept", ";", "}")
	if err != nil {
		return fmt.Errorf("failed to create nft chain ip coi forward: %w", err)
	}
	return nil
}

// nftRuleExistsWithComment returns true if any rule in ip coi forward carries the given comment.
func nftRuleExistsWithComment(comment string) (bool, error) {
	output, err := runNFTCommand("list", "chain", "ip", "coi", "forward")
	if err != nil {
		return false, nil // chain doesn't exist yet
	}
	return strings.Contains(string(output), fmt.Sprintf(`comment "%s"`, comment)), nil
}

// nftGetHandlesByComment returns the handles of rules in ip coi forward that match comment.
func nftGetHandlesByComment(comment string) ([]string, error) {
	output, err := runNFTCommand("-a", "list", "chain", "ip", "coi", "forward")
	if err != nil {
		return nil, nil
	}
	var handles []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, fmt.Sprintf(`comment "%s"`, comment)) {
			if h := extractNFTHandle(line); h != "" {
				handles = append(handles, h)
			}
		}
	}
	return handles, nil
}

// deleteNFTRulesByComment removes all rules in ip coi forward that carry the given comment.
func deleteNFTRulesByComment(comment string) error {
	handles, err := nftGetHandlesByComment(comment)
	if err != nil {
		return err
	}
	for _, h := range handles {
		if _, delErr := runNFTCommand("delete", "rule", "ip", "coi", "forward", "handle", h); delErr != nil {
			log.Printf("Warning: failed to delete nft rule handle %s: %v", h, delErr)
		}
	}
	return nil
}

// ListCOIFilterRuleIPs returns a map of container IP → rule handles from ip coi forward.
// Used by orphan detection to find rules whose containers are no longer running.
func ListCOIFilterRuleIPs() (map[string][]string, error) {
	output, err := runNFTCommand("-a", "list", "chain", "ip", "coi", "forward")
	if err != nil {
		return nil, nil
	}
	result := make(map[string][]string)
	for _, line := range strings.Split(string(output), "\n") {
		const prefix = `comment "coi-`
		idx := strings.Index(line, prefix)
		if idx == -1 {
			continue
		}
		rest := line[idx+len(prefix):]
		end := strings.Index(rest, `"`)
		if end == -1 {
			continue
		}
		ip := rest[:end]
		if ip == "base" {
			continue
		}
		if h := extractNFTHandle(line); h != "" {
			result[ip] = append(result[ip], h)
		}
	}
	return result, nil
}

// DeleteCOIFilterRulesForIP removes all ip coi forward rules for the given container IP.
func DeleteCOIFilterRulesForIP(ip string) error {
	return deleteNFTRulesByComment("coi-" + ip)
}

// GetContainerIP retrieves the IPv4 address of a container from Incus
// It retries for up to 30 seconds waiting for DHCP to assign an IP
func GetContainerIP(containerName string) (string, error) {
	return GetContainerIPWithRetries(containerName, 30)
}

// GetContainerIPFast retrieves the container IP with minimal retries
func GetContainerIPFast(containerName string) (string, error) {
	return GetContainerIPWithRetries(containerName, 3)
}

// GetContainerIPWithRetries retrieves the IPv4 address with configurable retry count
func GetContainerIPWithRetries(containerName string, maxRetries int) (string, error) {
	const retryDelay = time.Second

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		ip, err := getContainerIPOnce(containerName)
		if err == nil {
			return ip, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	return "", fmt.Errorf("timeout waiting for container IP after %d seconds: %w", maxRetries, lastErr)
}

func getContainerIPOnce(containerName string) (string, error) {
	output, err := container.IncusOutput("list", containerName, "--format=json")
	if err != nil {
		return "", fmt.Errorf("failed to get container info: %w", err)
	}

	var containers []struct {
		Name  string `json:"name"`
		State struct {
			Network map[string]struct {
				Addresses []struct {
					Family  string `json:"family"`
					Address string `json:"address"`
				} `json:"addresses"`
			} `json:"network"`
		} `json:"state"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return "", fmt.Errorf("failed to parse container info: %w", err)
	}

	for _, c := range containers {
		if c.Name == containerName {
			if eth0, ok := c.State.Network["eth0"]; ok {
				for _, addr := range eth0.Addresses {
					if addr.Family == "inet" {
						return addr.Address, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("no IPv4 address found for container %s", containerName)
}

// NftInstalled checks if the nft binary is installed
func NftInstalled() bool {
	_, err := exec.LookPath("nft")
	return err == nil
}

// NftAvailable checks if nft is available and passwordless sudo is configured
func NftAvailable() bool {
	cmd := exec.Command("sudo", "-n", "nft", "list", "tables")
	return cmd.Run() == nil
}

// UfwInstalled checks if the ufw binary is installed
func UfwInstalled() bool {
	_, err := exec.LookPath("ufw")
	return err == nil
}

// UfwActive checks if ufw is active.
func UfwActive() bool {
	if err := exec.Command("systemctl", "is-active", "--quiet", "ufw").Run(); err == nil {
		return true
	}
	out, err := exec.Command("sudo", "-n", "ufw", "status").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: active")
}

// MasqueradeEnabled checks if the Incus bridge has NAT (masquerade) enabled.
// Incus manages masquerade natively via its bridge config; no extra firewall daemon needed.
// ipv4.nat defaults to true for Incus-managed bridges; an unset/empty value
// means the default (enabled), so only an explicit "false" means disabled.
func MasqueradeEnabled() bool {
	bridgeName, err := GetIncusBridgeName()
	if err != nil {
		return false
	}
	output, err := container.IncusOutput("network", "get", bridgeName, "ipv4.nat")
	if err != nil {
		return false
	}
	val := strings.TrimSpace(output)
	return val != "false" // empty string = default = enabled
}

// BridgeInTrustedZone checks whether the Incus bridge has iptables FORWARD ACCEPT
// rules in place (the nft-based replacement for iptables bridge forwarding rules).
// Returns (inZone, bridgeName, error).
func BridgeInTrustedZone() (bool, string, error) {
	bridgeName, err := GetIncusBridgeName()
	if err != nil {
		return false, "", fmt.Errorf("could not determine bridge name: %w", err)
	}
	return IptablesBridgeRulesExist(bridgeName), bridgeName, nil
}

// EnsureBridgeInTrustedZone ensures the Incus bridge has iptables FORWARD ACCEPT
// rules so containers can obtain IPs via DHCP even when the FORWARD policy is DROP.
// Returns (changed, bridgeName, error).
func EnsureBridgeInTrustedZone() (bool, string, error) {
	bridgeName, err := GetIncusBridgeName()
	if err != nil {
		return false, "", fmt.Errorf("could not determine bridge name: %w", err)
	}
	if IptablesBridgeRulesExist(bridgeName) {
		return false, bridgeName, nil
	}
	if err := EnsureIptablesBridgeRules(bridgeName); err != nil {
		return false, bridgeName, err
	}
	return true, bridgeName, nil
}

// IptablesAvailable checks if the iptables binary is installed
func IptablesAvailable() bool {
	_, err := exec.LookPath("iptables")
	return err == nil
}

// ForwardPolicyIsDrop checks if the iptables FORWARD chain policy is DROP
func ForwardPolicyIsDrop() bool {
	cmd := exec.Command("sudo", "-n", "iptables", "-L", "FORWARD", "-n")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	lines := strings.SplitN(string(output), "\n", 2)
	if len(lines) == 0 {
		return false
	}
	return strings.Contains(lines[0], "policy DROP")
}

// NeedsIptablesFallback returns true when nft is not available but
// the FORWARD chain policy is DROP and iptables is available as a fallback
func NeedsIptablesFallback() bool {
	return !NftAvailable() && ForwardPolicyIsDrop() && IptablesAvailable()
}

// GetIncusBridgeName extracts the bridge/network name from the Incus default profile
func GetIncusBridgeName() (string, error) {
	profileOutput, err := container.IncusOutput("profile", "device", "show", "default")
	if err != nil {
		return "", fmt.Errorf("failed to get default profile: %w", err)
	}

	lines := strings.Split(profileOutput, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "eth0:" {
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				if strings.Contains(lines[j], "network:") {
					parts := strings.Split(lines[j], ":")
					if len(parts) >= 2 {
						return strings.TrimSpace(parts[1]), nil
					}
				}
			}
			break
		}
	}
	return "", fmt.Errorf("could not determine network/bridge name from default profile")
}

// EnsureIptablesBridgeRules idempotently adds FORWARD ACCEPT rules for the
// given bridge interface. The rules are tagged with a comment so they can be
// identified for cleanup.
func EnsureIptablesBridgeRules(bridgeName string) error {
	rules := [][]string{
		{"-i", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward"},
		{"-o", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward"},
	}

	for _, ruleSpec := range rules {
		if iptablesRuleExists("FORWARD", ruleSpec...) {
			continue
		}
		args := append([]string{"-n", "iptables", "-I", "FORWARD"}, ruleSpec...)
		cmd := exec.Command("sudo", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to add iptables rule: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

// RemoveIptablesBridgeRules removes the coi-bridge-forward iptables rules.
func RemoveIptablesBridgeRules(bridgeName string) error {
	rules := [][]string{
		{"-i", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward"},
		{"-o", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward"},
	}

	for _, ruleSpec := range rules {
		if !iptablesRuleExists("FORWARD", ruleSpec...) {
			continue
		}
		args := append([]string{"-n", "iptables", "-D", "FORWARD"}, ruleSpec...)
		cmd := exec.Command("sudo", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to remove iptables rule: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

// IptablesBridgeRulesExist checks whether the coi-bridge-forward rules exist
func IptablesBridgeRulesExist(bridgeName string) bool {
	inRule := iptablesRuleExists("FORWARD",
		"-i", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward")
	outRule := iptablesRuleExists("FORWARD",
		"-o", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward")
	return inRule && outRule
}

func iptablesRuleExists(chain string, ruleSpec ...string) bool {
	args := append([]string{"-n", "iptables", "-C", chain}, ruleSpec...)
	cmd := exec.Command("sudo", args...)
	return cmd.Run() == nil
}

// IsDockerRunning checks if the Docker daemon is running
func IsDockerRunning() bool {
	if exec.Command("systemctl", "is-active", "--quiet", "docker").Run() == nil {
		return true
	}
	return exec.Command("pgrep", "-x", "dockerd").Run() == nil
}

// GetContainerVethName retrieves the host-side veth interface name for a container
func GetContainerVethName(containerName string) (string, error) {
	output, err := container.IncusOutput("list", containerName, "--format=json")
	if err != nil {
		return "", fmt.Errorf("failed to get container info: %w", err)
	}

	var containers []struct {
		Name  string `json:"name"`
		State struct {
			Network map[string]struct {
				HostName string `json:"host_name"`
			} `json:"network"`
		} `json:"state"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return "", fmt.Errorf("failed to parse container info: %w", err)
	}

	for _, c := range containers {
		if c.Name == containerName {
			if eth0, ok := c.State.Network["eth0"]; ok {
				if eth0.HostName != "" {
					return eth0.HostName, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no veth interface found for container %s", containerName)
}
