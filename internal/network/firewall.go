package network

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// FirewallManager manages firewalld direct rules for container network isolation
type FirewallManager struct {
	containerIP string
	gatewayIP   string
}

// NewFirewallManager creates a new firewall manager for a container
func NewFirewallManager(containerIP, gatewayIP string) *FirewallManager {
	return &FirewallManager{
		containerIP: containerIP,
		gatewayIP:   gatewayIP,
	}
}

// ApplyRestricted applies restricted mode rules (block RFC1918, allow internet)
func (f *FirewallManager) ApplyRestricted(cfg *config.NetworkConfig) error {
	// Ensure base rules for return traffic are in place
	if err := EnsureBaseRules(); err != nil {
		log.Printf("Warning: failed to ensure base rules: %v", err)
	}

	// Priority 0: Allow gateway (for host communication)
	if f.gatewayIP != "" {
		if err := f.addRule(0, f.containerIP, f.gatewayIP+"/32", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	// Handle local network access
	if config.BoolVal(cfg.AllowLocalNetworkAccess) {
		// Allow all RFC1918 when local network access is enabled
		if err := f.addRule(1, f.containerIP, "10.0.0.0/8", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
		}
		if err := f.addRule(1, f.containerIP, "172.16.0.0/12", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
		}
		if err := f.addRule(1, f.containerIP, "192.168.0.0/16", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
		}
	} else if config.BoolVal(cfg.BlockPrivateNetworks) {
		// Block RFC1918 ranges
		if err := f.addRule(10, f.containerIP, "10.0.0.0/8", "REJECT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
		}
		if err := f.addRule(10, f.containerIP, "172.16.0.0/12", "REJECT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
		}
		if err := f.addRule(10, f.containerIP, "192.168.0.0/16", "REJECT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
		}
	}

	// Block metadata endpoints
	if config.BoolVal(cfg.BlockMetadataEndpoint) {
		if err := f.addRule(10, f.containerIP, "169.254.0.0/16", "REJECT"); err != nil {
			return fmt.Errorf("failed to add metadata block rule: %w", err)
		}
	}

	// Explicitly allow all other traffic (internet)
	// Needed because FORWARD chain policy might be DROP with firewalld
	if err := f.addRule(50, f.containerIP, "0.0.0.0/0", "ACCEPT"); err != nil {
		return fmt.Errorf("failed to add default allow rule: %w", err)
	}

	return nil
}

// ApplyAllowlist applies allowlist mode rules (allow specific IPs, block all else)
func (f *FirewallManager) ApplyAllowlist(cfg *config.NetworkConfig, allowedIPs []string) error {
	// Ensure base rules for return traffic are in place
	if err := EnsureBaseRules(); err != nil {
		log.Printf("Warning: failed to ensure base rules: %v", err)
	}

	// Priority 0: Allow gateway (for host communication and DNS via dnsmasq)
	// DNS works through the bridge's dnsmasq - no public DNS servers allowed
	// to prevent DNS exfiltration attacks
	if f.gatewayIP != "" {
		if err := f.addRule(0, f.containerIP, f.gatewayIP+"/32", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	// Handle local network access
	if config.BoolVal(cfg.AllowLocalNetworkAccess) {
		// Allow all RFC1918 when local network access is enabled
		if err := f.addRule(1, f.containerIP, "10.0.0.0/8", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
		}
		if err := f.addRule(1, f.containerIP, "172.16.0.0/12", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
		}
		if err := f.addRule(1, f.containerIP, "192.168.0.0/16", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 allow rule: %w", err)
		}
	}

	// Priority 1: Allow specific IPs (from resolved domains)
	// Sort for deterministic ordering
	sortedIPs := make([]string, len(allowedIPs))
	copy(sortedIPs, allowedIPs)
	sort.Strings(sortedIPs)

	for _, ip := range sortedIPs {
		dest := ip
		if !strings.Contains(ip, "/") {
			dest = ip + "/32"
		}
		if err := f.addRule(1, f.containerIP, dest, "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add allowlist rule for %s: %w", ip, err)
		}
	}

	// Block RFC1918 and metadata (unless local network access is enabled)
	if !config.BoolVal(cfg.AllowLocalNetworkAccess) {
		if err := f.addRule(10, f.containerIP, "10.0.0.0/8", "REJECT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
		}
		if err := f.addRule(10, f.containerIP, "172.16.0.0/12", "REJECT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
		}
		if err := f.addRule(10, f.containerIP, "192.168.0.0/16", "REJECT"); err != nil {
			return fmt.Errorf("failed to add RFC1918 block rule: %w", err)
		}
		if err := f.addRule(10, f.containerIP, "169.254.0.0/16", "REJECT"); err != nil {
			return fmt.Errorf("failed to add metadata block rule: %w", err)
		}
	}

	// Priority 99: Default deny for allowlist mode
	if err := f.addRule(99, f.containerIP, "0.0.0.0/0", "REJECT"); err != nil {
		return fmt.Errorf("failed to add default deny rule: %w", err)
	}

	return nil
}

// RemoveRules removes all firewall rules for this container's IP
func (f *FirewallManager) RemoveRules() error {
	if f.containerIP == "" {
		return nil
	}

	// List all direct rules
	rules, err := f.listDirectRules()
	if err != nil {
		return fmt.Errorf("failed to list firewall rules: %w", err)
	}

	// Remove rules that match this container's IP
	for _, rule := range rules {
		if strings.Contains(rule, f.containerIP) {
			if err := f.removeRule(rule); err != nil {
				log.Printf("Warning: failed to remove firewall rule: %v", err)
			}
		}
	}

	return nil
}

// EnsureBaseRules adds the base rules needed for container networking
// These rules allow return traffic and must be in place before container-specific rules
func EnsureBaseRules() error {
	// Add conntrack rule for return traffic via firewalld direct rules
	// Priority -1 ensures this runs before all other rules (including our container rules at 0+)
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--direct", "--add-rule",
		"ipv4", "filter", "FORWARD", "-1",
		"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Rule might already exist, that's OK
		if !strings.Contains(string(output), "ALREADY_ENABLED") {
			log.Printf("Warning: failed to add conntrack rule via firewalld: %s", strings.TrimSpace(string(output)))
		}
	}

	return nil
}

// EnsureOpenModeRules adds rules to allow all traffic for a container in open mode
// This is needed because FORWARD chain policy may be DROP
func EnsureOpenModeRules(containerIP string) error {
	// Ensure base conntrack rule exists
	if err := EnsureBaseRules(); err != nil {
		log.Printf("Warning: failed to ensure base rules: %v", err)
	}

	// Add ACCEPT rule for all traffic from this container
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--direct", "--add-rule",
		"ipv4", "filter", "FORWARD", "0",
		"-s", containerIP, "-j", "ACCEPT")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "ALREADY_ENABLED") {
			return fmt.Errorf("failed to add open mode rule: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}

	return nil
}

// RemoveOpenModeRules removes the ACCEPT rules for a container in open mode
func RemoveOpenModeRules(containerIP string) error {
	if containerIP == "" {
		return nil
	}

	// Remove the ACCEPT rule for traffic from this container
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--direct", "--remove-rule",
		"ipv4", "filter", "FORWARD", "0",
		"-s", containerIP, "-j", "ACCEPT")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Rule might not exist, that's OK
		if !strings.Contains(string(output), "NOT_ENABLED") {
			return fmt.Errorf("failed to remove open mode rule: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}

	return nil
}

// DisableIPv6ForContainer disables IPv6 in the container to prevent firewall bypass.
// All network isolation rules are IPv4-only; IPv6 would circumvent them entirely.
func DisableIPv6ForContainer(containerName string) error {
	mgr := container.NewManager(containerName)
	cmd := "sysctl -w net.ipv6.conf.all.disable_ipv6=1 net.ipv6.conf.default.disable_ipv6=1"
	_, err := mgr.ExecCommand(cmd, container.ExecCommandOptions{Capture: true})
	return err
}

// addRule adds a firewall direct rule using firewall-cmd
func (f *FirewallManager) addRule(priority int, source, destination, action string) error {
	// firewall-cmd --direct --add-rule ipv4 filter FORWARD <priority> -s <src> -d <dst> -j <action>
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--direct", "--add-rule",
		"ipv4", "filter", "FORWARD", fmt.Sprintf("%d", priority),
		"-s", source, "-d", destination, "-j", action)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("firewall-cmd failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// listDirectRules lists all direct rules in the FORWARD chain
func (f *FirewallManager) listDirectRules() ([]string, error) {
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--direct", "--get-all-rules")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list rules: %w", err)
	}

	var rules []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "FORWARD") {
			rules = append(rules, line)
		}
	}

	return rules, nil
}

// removeRule removes a specific firewall direct rule
func (f *FirewallManager) removeRule(rule string) error {
	// Parse rule: "ipv4 filter FORWARD 10 -s 10.47.62.50 -d 10.0.0.0/8 -j REJECT"
	parts := strings.Fields(rule)
	if len(parts) < 4 {
		return fmt.Errorf("invalid rule format: %s", rule)
	}

	// Build remove command
	args := []string{"-n", "firewall-cmd", "--direct", "--remove-rule"}
	args = append(args, parts...)

	cmd := exec.Command("sudo", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove rule: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// GetContainerIP retrieves the IPv4 address of a container from Incus
// It retries for up to 30 seconds waiting for DHCP to assign an IP
func GetContainerIP(containerName string) (string, error) {
	return GetContainerIPWithRetries(containerName, 30)
}

// GetContainerIPFast retrieves the container IP with minimal retries
// Use this when the container should already have an IP assigned
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

		// Wait before retrying
		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}

	return "", fmt.Errorf("timeout waiting for container IP after %d seconds: %w", maxRetries, lastErr)
}

// getContainerIPOnce attempts to get the container IP once without retrying
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
			// Look for eth0 IPv4 address
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

// FirewallInstalled checks if the firewall-cmd binary is installed
func FirewallInstalled() bool {
	_, err := exec.LookPath("firewall-cmd")
	return err == nil
}

// FirewallAvailable checks if firewalld is available and running
func FirewallAvailable() bool {
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--state")
	err := cmd.Run()
	return err == nil
}

// UfwInstalled checks if the ufw binary is installed
func UfwInstalled() bool {
	_, err := exec.LookPath("ufw")
	return err == nil
}

// UfwActive checks if ufw is active. It first tries systemctl (no sudo needed),
// then falls back to parsing 'sudo -n ufw status' output.
func UfwActive() bool {
	// Prefer systemctl — works without sudo
	if err := exec.Command("systemctl", "is-active", "--quiet", "ufw").Run(); err == nil {
		return true
	}

	// Fallback: try ufw status directly (requires passwordless sudo)
	out, err := exec.Command("sudo", "-n", "ufw", "status").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: active")
}

// MasqueradeEnabled checks if masquerade is enabled in the default firewalld zone
func MasqueradeEnabled() bool {
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--query-masquerade")
	return cmd.Run() == nil
}

// BridgeInTrustedZone checks if the Incus bridge interface is in the firewalld trusted zone.
// Returns (inZone, bridgeName, error). An error is returned when the bridge name cannot be
// determined or when firewall-cmd fails unexpectedly (not just "interface not in zone").
// firewall-cmd --query-interface exits 0 when in zone, 1 when not in zone, and other
// codes for real errors.
func BridgeInTrustedZone() (bool, string, error) {
	bridgeName, err := GetIncusBridgeName()
	if err != nil {
		return false, "", fmt.Errorf("could not determine bridge name: %w", err)
	}

	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--zone=trusted", "--query-interface="+bridgeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Exit code 1 means "not in zone" — that's a valid answer, not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, bridgeName, nil
		}
		return false, bridgeName, fmt.Errorf("firewall-cmd query failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return true, bridgeName, nil
}

// EnsureBridgeInTrustedZone makes sure the Incus bridge is a member of the
// firewalld trusted zone, adding it if necessary. It is safe to call at the
// start of any command path that needs container networking to work.
//
// Return values:
//   - changed: true whenever the permanent add step succeeded in this call,
//     regardless of whether the subsequent reload succeeded. This way a
//     reload failure does not hide the fact that the host state was
//     mutated. false when firewalld is unavailable or when the bridge was
//     already in the zone.
//   - bridgeName: the detected Incus bridge name (empty string when firewalld
//     is unavailable or the bridge name could not be determined).
//   - err: non-nil only on real failures (bridge detection error or
//     firewall-cmd failure while adding/reloading). firewalld not being
//     available is explicitly NOT an error — callers that require firewalld
//     should check that separately via FirewallAvailable().
//
// The function is idempotent: calling it repeatedly is safe, and a no-op on
// already-healthy systems.
func EnsureBridgeInTrustedZone() (changed bool, bridgeName string, err error) {
	// firewalld not available → nothing to do. Not an error.
	if !FirewallAvailable() {
		return false, "", nil
	}

	inZone, name, err := BridgeInTrustedZone()
	if err != nil {
		return false, name, err
	}
	if inZone {
		return false, name, nil
	}

	// Add the bridge to the trusted zone, persistently, and reload firewalld
	// so the running config picks up the change.
	addCmd := exec.Command("sudo", "-n", "firewall-cmd", "--zone=trusted", "--add-interface="+name, "--permanent")
	if output, err := addCmd.CombinedOutput(); err != nil {
		return false, name, fmt.Errorf("firewall-cmd --add-interface failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// The permanent add already mutated the host state, so from here on
	// changed=true regardless of whether reload succeeds. Returning
	// changed=false on a reload failure would hide a real change from the
	// caller (and suppress the "Added …" log), while the bridge is already
	// listed in the permanent config.
	reloadCmd := exec.Command("sudo", "-n", "firewall-cmd", "--reload")
	if output, err := reloadCmd.CombinedOutput(); err != nil {
		return true, name, fmt.Errorf("firewall-cmd --reload failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return true, name, nil
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

	// First line looks like: "Chain FORWARD (policy DROP)"
	lines := strings.SplitN(string(output), "\n", 2)
	if len(lines) == 0 {
		return false
	}

	return strings.Contains(lines[0], "policy DROP")
}

// NeedsIptablesFallback returns true when firewalld is not available but
// the FORWARD chain policy is DROP (typically caused by Docker) and iptables
// is available as a fallback
func NeedsIptablesFallback() bool {
	return !FirewallAvailable() && ForwardPolicyIsDrop() && IptablesAvailable()
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

// RemoveIptablesBridgeRules removes the coi-bridge-forward iptables rules for
// the given bridge. Tolerant if rules are already absent.
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
// for the given bridge name
func IptablesBridgeRulesExist(bridgeName string) bool {
	inRule := iptablesRuleExists("FORWARD",
		"-i", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward")
	outRule := iptablesRuleExists("FORWARD",
		"-o", bridgeName, "-j", "ACCEPT", "-m", "comment", "--comment", "coi-bridge-forward")
	return inRule && outRule
}

// iptablesRuleExists checks if a specific iptables rule exists using iptables -C
func iptablesRuleExists(chain string, ruleSpec ...string) bool {
	args := append([]string{"-n", "iptables", "-C", chain}, ruleSpec...)
	cmd := exec.Command("sudo", args...)
	return cmd.Run() == nil
}

// IsDockerRunning checks if the Docker daemon is running
func IsDockerRunning() bool {
	// Try systemctl first
	cmd := exec.Command("systemctl", "is-active", "--quiet", "docker")
	if cmd.Run() == nil {
		return true
	}

	// Fall back to checking for the process
	cmd = exec.Command("pgrep", "-x", "dockerd")
	return cmd.Run() == nil
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

// RemoveVethFromFirewalldZone removes a veth interface from its firewalld zone
// This cleans up stale zone bindings after container deletion
func RemoveVethFromFirewalldZone(vethName string) error {
	if vethName == "" {
		return nil
	}

	if !FirewallAvailable() {
		return nil
	}

	// Try to remove from common zones (public, trusted)
	// firewall-cmd returns success if interface wasn't in the zone
	zones := []string{"public", "trusted"}
	for _, zone := range zones {
		cmd := exec.Command("sudo", "-n", "firewall-cmd", "--zone="+zone, "--remove-interface="+vethName)
		// Ignore errors - interface might not be in this zone
		_ = cmd.Run()
	}

	return nil
}

// DetectOrphanedFirewalldZoneBindings finds veth interfaces registered in firewalld
// that no longer exist on the system
func DetectOrphanedFirewalldZoneBindings() ([]string, error) {
	if !FirewallAvailable() {
		return nil, nil
	}

	// Get all veths registered in firewalld by parsing nft output
	cmd := exec.Command("sudo", "-n", "nft", "list", "table", "inet", "firewalld")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list firewalld table: %w", err)
	}

	// Extract unique veth names from the output
	vethInFirewalld := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		// Look for patterns like: iifname "veth01059070"
		if strings.Contains(line, "veth") {
			// Extract veth name using simple parsing
			for _, part := range strings.Fields(line) {
				part = strings.Trim(part, "\"")
				if strings.HasPrefix(part, "veth") && len(part) > 4 {
					vethInFirewalld[part] = true
				}
			}
		}
	}

	// Get veths that actually exist on the system
	existingVeths := make(map[string]bool)
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("failed to read network interfaces: %w", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "veth") {
			existingVeths[entry.Name()] = true
		}
	}

	// Find orphaned veths (in firewalld but not on system)
	var orphaned []string
	for veth := range vethInFirewalld {
		if !existingVeths[veth] {
			orphaned = append(orphaned, veth)
		}
	}

	return orphaned, nil
}

// CleanupOrphanedFirewalldZoneBindings removes orphaned veth interfaces from firewalld zones
func CleanupOrphanedFirewalldZoneBindings(veths []string, logger func(string)) (int, error) {
	if logger == nil {
		logger = func(msg string) { log.Printf("%s", msg) }
	}

	cleaned := 0
	for _, veth := range veths {
		logger(fmt.Sprintf("Removing orphaned firewalld zone binding: %s", veth))
		if err := RemoveVethFromFirewalldZone(veth); err != nil {
			logger(fmt.Sprintf("  Warning: Failed to remove %s: %v", veth, err))
			continue
		}
		cleaned++
	}

	// Note: We intentionally do NOT reload firewalld here.
	// The --remove-interface command already applies the change at runtime,
	// and reloading firewalld would wipe out Docker's dynamically-added nft rules.

	return cleaned, nil
}
