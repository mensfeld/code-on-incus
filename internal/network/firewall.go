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
	// Priority 0: Allow gateway (for host communication)
	if f.gatewayIP != "" {
		if err := f.addRule(0, f.containerIP, f.gatewayIP+"/32", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	// Handle local network access
	if cfg.AllowLocalNetworkAccess {
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
	} else if cfg.BlockPrivateNetworks {
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
	if cfg.BlockMetadataEndpoint {
		if err := f.addRule(10, f.containerIP, "169.254.0.0/16", "REJECT"); err != nil {
			return fmt.Errorf("failed to add metadata block rule: %w", err)
		}
	}

	// No default deny rule - allow all other traffic (internet)
	return nil
}

// ApplyAllowlist applies allowlist mode rules (allow specific IPs, block all else)
func (f *FirewallManager) ApplyAllowlist(cfg *config.NetworkConfig, allowedIPs []string) error {
	// Priority 0: Allow gateway (for host communication)
	if f.gatewayIP != "" {
		if err := f.addRule(0, f.containerIP, f.gatewayIP+"/32", "ACCEPT"); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	// Handle local network access
	if cfg.AllowLocalNetworkAccess {
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
	if !cfg.AllowLocalNetworkAccess {
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
	const maxRetries = 30
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

// FirewallAvailable checks if firewalld is available and running
func FirewallAvailable() bool {
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--state")
	err := cmd.Run()
	return err == nil
}
