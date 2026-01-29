package network

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// FirewallManager manages firewalld direct rules for container network isolation
type FirewallManager struct {
	// No state needed - rules are identified by container IP
}

// RFC1918 private network ranges
var rfc1918Ranges = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

// Metadata endpoint range (cloud provider metadata services)
const metadataRange = "169.254.0.0/16"

// NewFirewallManager creates a new FirewallManager
func NewFirewallManager() *FirewallManager {
	return &FirewallManager{}
}

// ApplyRestricted applies restricted mode firewall rules for a container
// Restricted mode: blocks RFC1918 and metadata, allows all other traffic
func (f *FirewallManager) ApplyRestricted(containerIP, gatewayIP string, cfg *config.NetworkConfig) error {
	log.Printf("Applying restricted mode firewall rules for container IP %s", containerIP)

	// Priority 0: Allow gateway (for host communication)
	if gatewayIP != "" {
		if err := f.addRule(containerIP, gatewayIP+"/32", "ACCEPT", 0); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	// Handle local network access
	if cfg.AllowLocalNetworkAccess {
		// Allow all RFC1918 ranges
		for _, cidr := range rfc1918Ranges {
			if err := f.addRule(containerIP, cidr, "ACCEPT", 1); err != nil {
				return fmt.Errorf("failed to add local network allow rule for %s: %w", cidr, err)
			}
		}
	} else {
		// Priority 10: Block RFC1918 ranges
		if cfg.BlockPrivateNetworks {
			for _, cidr := range rfc1918Ranges {
				if err := f.addRule(containerIP, cidr, "REJECT", 10); err != nil {
					return fmt.Errorf("failed to add RFC1918 block rule for %s: %w", cidr, err)
				}
			}
		}
	}

	// Priority 10: Block metadata endpoint
	if cfg.BlockMetadataEndpoint {
		if err := f.addRule(containerIP, metadataRange, "REJECT", 10); err != nil {
			return fmt.Errorf("failed to add metadata block rule: %w", err)
		}
	}

	// No default rule needed - default forwarding allows all other traffic

	log.Printf("Restricted mode firewall rules applied for %s", containerIP)
	return nil
}

// ApplyAllowlist applies allowlist mode firewall rules for a container
// Allowlist mode: allows only specific IPs, blocks everything else
func (f *FirewallManager) ApplyAllowlist(containerIP, gatewayIP string, cfg *config.NetworkConfig, allowedIPs map[string][]string) error {
	log.Printf("Applying allowlist mode firewall rules for container IP %s", containerIP)

	// Priority 0: Allow gateway (for host communication)
	if gatewayIP != "" {
		if err := f.addRule(containerIP, gatewayIP+"/32", "ACCEPT", 0); err != nil {
			return fmt.Errorf("failed to add gateway allow rule: %w", err)
		}
	}

	// Handle local network access
	if cfg.AllowLocalNetworkAccess {
		// Allow all RFC1918 ranges
		for _, cidr := range rfc1918Ranges {
			if err := f.addRule(containerIP, cidr, "ACCEPT", 1); err != nil {
				return fmt.Errorf("failed to add local network allow rule for %s: %w", cidr, err)
			}
		}
	}

	// Priority 1: Allow specific IPs from resolved domains
	// Deduplicate IPs across all domains
	uniqueIPs := make(map[string]bool)
	for domain, ips := range allowedIPs {
		if domain == "__internal_gateway__" {
			continue // Skip gateway IP - handled separately
		}
		for _, ip := range ips {
			uniqueIPs[ip] = true
		}
	}

	// Sort IPs for deterministic ordering
	sortedIPs := make([]string, 0, len(uniqueIPs))
	for ip := range uniqueIPs {
		sortedIPs = append(sortedIPs, ip)
	}
	sort.Strings(sortedIPs)

	for _, ip := range sortedIPs {
		if err := f.addRule(containerIP, ip+"/32", "ACCEPT", 1); err != nil {
			return fmt.Errorf("failed to add allowlist rule for %s: %w", ip, err)
		}
	}

	// Priority 10: Block RFC1918 (safety, even if some IPs were explicitly allowed above)
	if !cfg.AllowLocalNetworkAccess {
		for _, cidr := range rfc1918Ranges {
			if err := f.addRule(containerIP, cidr, "REJECT", 10); err != nil {
				return fmt.Errorf("failed to add RFC1918 block rule for %s: %w", cidr, err)
			}
		}

		// Block metadata
		if err := f.addRule(containerIP, metadataRange, "REJECT", 10); err != nil {
			return fmt.Errorf("failed to add metadata block rule: %w", err)
		}
	}

	// Priority 99: Default deny for allowlist mode
	if err := f.addRule(containerIP, "0.0.0.0/0", "REJECT", 99); err != nil {
		return fmt.Errorf("failed to add default deny rule: %w", err)
	}

	log.Printf("Allowlist mode firewall rules applied for %s", containerIP)
	return nil
}

// RemoveContainerRules removes all firewall rules for a container
func (f *FirewallManager) RemoveContainerRules(containerIP string) error {
	if containerIP == "" {
		return nil
	}

	log.Printf("Removing firewall rules for container IP %s", containerIP)

	// List all direct rules and find ones matching this container IP
	rules, err := f.listDirectRules()
	if err != nil {
		return fmt.Errorf("failed to list firewall rules: %w", err)
	}

	// Remove rules that contain this container IP as source
	removedCount := 0
	for _, rule := range rules {
		if strings.Contains(rule, "-s "+containerIP) {
			if err := f.removeRule(rule); err != nil {
				log.Printf("Warning: failed to remove rule: %v", err)
			} else {
				removedCount++
			}
		}
	}

	log.Printf("Removed %d firewall rules for %s", removedCount, containerIP)
	return nil
}

// addRule adds a firewalld direct rule
// Priority determines rule order: lower numbers = higher priority (evaluated first)
func (f *FirewallManager) addRule(sourceIP, destination, action string, priority int) error {
	// firewall-cmd --direct --add-rule ipv4 filter FORWARD <priority> -s <source> -d <dest> -j <action>
	cmd := exec.Command("firewall-cmd", "--direct", "--add-rule", "ipv4", "filter", "FORWARD",
		fmt.Sprintf("%d", priority),
		"-s", sourceIP,
		"-d", destination,
		"-j", action)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("firewall-cmd failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// listDirectRules lists all direct rules from firewalld
func (f *FirewallManager) listDirectRules() ([]string, error) {
	cmd := exec.Command("firewall-cmd", "--direct", "--get-all-rules")
	output, err := cmd.Output()
	if err != nil {
		// If firewalld isn't running or no rules, return empty list
		return []string{}, nil
	}

	var rules []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			rules = append(rules, line)
		}
	}

	return rules, nil
}

// removeRule removes a firewalld direct rule
func (f *FirewallManager) removeRule(rule string) error {
	// Parse the rule format: "ipv4 filter FORWARD <priority> -s <source> -d <dest> -j <action>"
	parts := strings.Fields(rule)
	if len(parts) < 4 {
		return fmt.Errorf("invalid rule format: %s", rule)
	}

	// Build the remove command with the same parameters
	args := append([]string{"--direct", "--remove-rule"}, parts...)
	cmd := exec.Command("firewall-cmd", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("firewall-cmd remove failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// GetContainerIP retrieves the IPv4 address of a container from Incus
func GetContainerIP(containerName string) (string, error) {
	output, err := container.IncusOutput("list", containerName, "--format", "json")
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
					Scope   string `json:"scope"`
				} `json:"addresses"`
			} `json:"network"`
		} `json:"state"`
	}

	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return "", fmt.Errorf("failed to parse container info: %w", err)
	}

	for _, c := range containers {
		if c.Name != containerName {
			continue
		}

		// Check eth0 first, then any other interface
		interfaces := []string{"eth0"}
		for iface := range c.State.Network {
			if iface != "eth0" && iface != "lo" {
				interfaces = append(interfaces, iface)
			}
		}

		for _, iface := range interfaces {
			if netInfo, ok := c.State.Network[iface]; ok {
				for _, addr := range netInfo.Addresses {
					if addr.Family == "inet" && addr.Scope == "global" {
						return addr.Address, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no IPv4 address found for container %s", containerName)
}

// CheckFirewalldAvailable checks if firewalld is running and accessible
func CheckFirewalldAvailable() error {
	// Check if firewall-cmd exists
	_, err := exec.LookPath("firewall-cmd")
	if err != nil {
		return fmt.Errorf("firewalld not installed (firewall-cmd not found)")
	}

	cmd := exec.Command("firewall-cmd", "--state")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("firewalld not available: %s", strings.TrimSpace(string(output)))
	}

	state := strings.TrimSpace(string(output))
	if state != "running" {
		return fmt.Errorf("firewalld is not running (state: %s)", state)
	}

	return nil
}
