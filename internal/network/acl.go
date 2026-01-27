package network

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// ErrACLNotSupported is returned when the network doesn't support ACLs
// This typically happens with standard bridge networks (non-OVN)
var ErrACLNotSupported = fmt.Errorf("network ACLs not supported")

// ACLManager manages Incus network ACLs for container isolation
type ACLManager struct{}

// Create creates a new network ACL with the specified rules
func (m *ACLManager) Create(name string, cfg *config.NetworkConfig, containerName string) error {
	// First, check if ACL already exists and delete it
	// This handles cases where ACL wasn't cleaned up properly
	_ = m.Delete(name) // Ignore error if ACL doesn't exist

	// Create the ACL
	if err := container.IncusExecQuiet("network", "acl", "create", name); err != nil {
		return fmt.Errorf("failed to create ACL %s: %w", name, err)
	}

	// Auto-detect gateway IP for established connection rules
	gatewayIP, err := getContainerGatewayIP(containerName)
	if err != nil {
		log.Printf("Warning: Could not auto-detect gateway IP for ACL: %v", err)
	}

	// Build and add egress rules
	rules := buildACLRules(cfg, gatewayIP)
	for _, rule := range rules {
		// Parse rule into parts for the incus command
		// Rule format: "egress reject destination=10.0.0.0/8"
		parts := strings.Fields(rule)
		if len(parts) < 2 {
			return fmt.Errorf("invalid ACL rule format: %s", rule)
		}

		// Build command: incus network acl rule add <name> <direction> <action> <key=value>...
		args := []string{"network", "acl", "rule", "add", name}
		args = append(args, parts...)

		if err := container.IncusExecQuiet(args...); err != nil {
			// If rule addition fails, clean up the ACL
			_ = m.Delete(name)
			return fmt.Errorf("failed to add ACL rule %s: %w", rule, err)
		}
	}

	// Add ingress allow-all rule to allow response traffic
	if err := container.IncusExecQuiet("network", "acl", "rule", "add", name, "ingress", "action=allow"); err != nil {
		_ = m.Delete(name)
		return fmt.Errorf("failed to add ingress allow rule: %w", err)
	}

	return nil
}

// ApplyToContainer applies the ACL to a container's network interface
func (m *ACLManager) ApplyToContainer(containerName, aclName string) error {
	// Get the network name from the default profile (most containers use this)
	profileOutput, err := container.IncusOutput("profile", "device", "show", "default")
	if err != nil {
		return fmt.Errorf("failed to get default profile devices: %w", err)
	}

	// Parse the network name from profile
	var networkName string
	lines := strings.Split(profileOutput, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "eth0:" {
			// Look for network: line in the following lines
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				if strings.Contains(lines[j], "network:") {
					parts := strings.Split(lines[j], ":")
					if len(parts) >= 2 {
						networkName = strings.TrimSpace(parts[1])
						break
					}
				}
			}
			break
		}
	}

	if networkName == "" {
		return fmt.Errorf("could not determine network name from default profile")
	}

	// Check if the network supports ACLs before attempting to apply
	// ACLs require OVN networks; standard bridge networks don't support security.acls
	if supported, err := m.networkSupportsACLs(networkName); err != nil {
		return fmt.Errorf("failed to check ACL support: %w", err)
	} else if !supported {
		return ErrACLNotSupported
	}

	// Step 1: Override the eth0 device from profile to container level
	// This copies all properties from the profile's eth0 device
	err = container.IncusExec("config", "device", "override", containerName, "eth0")
	if err != nil {
		return fmt.Errorf("failed to override eth0 device: %w", err)
	}

	// Step 2: Set the security.acls property on the now-overridden device
	err = container.IncusExec("config", "device", "set", containerName, "eth0",
		"security.acls", aclName)
	if err != nil {
		// Check if this is an ACL not supported error
		if strings.Contains(err.Error(), "Invalid device option") ||
			strings.Contains(err.Error(), "security.acls") {
			return ErrACLNotSupported
		}
		return fmt.Errorf("failed to set ACL property: %w", err)
	}

	return nil
}

// networkSupportsACLs checks if the given network supports ACLs
// ACLs are only supported on OVN networks, not standard bridge networks
func (m *ACLManager) networkSupportsACLs(networkName string) (bool, error) {
	// Get network configuration to check its type
	output, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return false, fmt.Errorf("failed to get network info: %w", err)
	}

	// Parse the output to find the network type
	// Looking for "type: ovn" or "type: bridge"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "type:") {
			networkType := strings.TrimSpace(strings.TrimPrefix(line, "type:"))
			// Only OVN networks support security.acls on NIC devices
			return networkType == "ovn", nil
		}
	}

	// If we can't determine the type, assume ACLs are not supported
	return false, nil
}

// Delete removes a network ACL
func (m *ACLManager) Delete(name string) error {
	// Delete ACL (use quiet to suppress error if doesn't exist)
	return container.IncusExecQuiet("network", "acl", "delete", name)
}

// CreateAllowlist creates ACL for allowlist mode with resolved IPs
func (m *ACLManager) CreateAllowlist(name string, cfg *config.NetworkConfig, domainIPs map[string][]string) error {
	// First, delete existing ACL if present
	_ = m.Delete(name)

	// Create the ACL
	if err := container.IncusExecQuiet("network", "acl", "create", name); err != nil {
		return fmt.Errorf("failed to create ACL %s: %w", name, err)
	}

	// Build rules for allowlist mode
	rules := buildAllowlistRules(cfg, domainIPs)

	// Add egress rules
	for _, rule := range rules {
		parts := strings.Fields(rule)
		if len(parts) < 2 {
			_ = m.Delete(name)
			return fmt.Errorf("invalid ACL rule format: %s", rule)
		}

		args := []string{"network", "acl", "rule", "add", name}
		args = append(args, parts...)

		if err := container.IncusExecQuiet(args...); err != nil {
			_ = m.Delete(name)
			return fmt.Errorf("failed to add ACL rule %s: %w", rule, err)
		}
	}

	// Add ingress allow-all rule to allow response traffic
	if err := container.IncusExecQuiet("network", "acl", "rule", "add", name, "ingress", "action=allow"); err != nil {
		_ = m.Delete(name)
		return fmt.Errorf("failed to add ingress allow rule: %w", err)
	}

	return nil
}

// RecreateWithNewIPs updates ACL with new IP list (full recreation)
func (m *ACLManager) RecreateWithNewIPs(name string, cfg *config.NetworkConfig, domainIPs map[string][]string, containerName string) error {
	// Delete existing ACL
	if err := m.Delete(name); err != nil {
		return fmt.Errorf("failed to delete old ACL: %w", err)
	}

	// Create new ACL with updated IPs
	if err := m.CreateAllowlist(name, cfg, domainIPs); err != nil {
		return fmt.Errorf("failed to create new ACL: %w", err)
	}

	// Reapply to container
	if err := m.ApplyToContainer(containerName, name); err != nil {
		return fmt.Errorf("failed to reapply ACL: %w", err)
	}

	return nil
}

// buildACLRules generates ACL rules based on network configuration
func buildACLRules(cfg *config.NetworkConfig, gatewayIP string) []string {
	rules := []string{}

	// In restricted mode, block local networks
	if cfg.Mode == config.NetworkModeRestricted {
		// IMPORTANT: OVN evaluates rules in order they're added
		// We must add ALLOW rules for established connections FIRST, then REJECT, then general ALLOW

		// Allow traffic to host/local network (FIRST - highest priority)
		// This allows response traffic from container back to host/local network
		// Note: Incus OVN ACLs don't support connection tracking, so we allow all traffic to these destinations
		if cfg.AllowLocalNetworkAccess {
			// Allow to entire local network (useful for tmux across machines)
			// When enabled, RFC1918 blocking is disabled to allow full local network access
			rules = append(rules, "egress action=allow destination=10.0.0.0/8")
			rules = append(rules, "egress action=allow destination=172.16.0.0/12")
			rules = append(rules, "egress action=allow destination=192.168.0.0/16")
		} else {
			// Default: only allow to gateway IP (host only)
			if gatewayIP != "" {
				rules = append(rules, fmt.Sprintf("egress action=allow destination=%s/32", gatewayIP))
			}

			// Block rest of private ranges (RFC1918) if configured
			if cfg.BlockPrivateNetworks {
				rules = append(rules, "egress action=reject destination=10.0.0.0/8")
				rules = append(rules, "egress action=reject destination=172.16.0.0/12")
				rules = append(rules, "egress action=reject destination=192.168.0.0/16")
			}
		}

		// Block cloud metadata endpoints
		if cfg.BlockMetadataEndpoint {
			rules = append(rules, "egress action=reject destination=169.254.0.0/16")
		}

		// Allow all other traffic (added last, lowest priority)
		rules = append(rules, "egress action=allow")
	}

	return rules
}

// buildAllowlistRules generates allowlist ACL rules with default-deny
func buildAllowlistRules(cfg *config.NetworkConfig, domainIPs map[string][]string) []string {
	rules := []string{}

	// Extract gateway IP for established connection rule
	var gatewayIP string
	if ips, ok := domainIPs["__internal_gateway__"]; ok && len(ips) > 0 {
		gatewayIP = ips[0]
	}

	// Deduplicate IPs across all domains (multiple domains can resolve to same IP)
	// Exclude __internal_gateway__ from regular allow rules (it's only for established connections)
	uniqueIPs := make(map[string]bool)
	for domain, ips := range domainIPs {
		if domain == "__internal_gateway__" {
			continue // Skip gateway IP - it's only used for established connection rule
		}
		for _, ip := range ips {
			uniqueIPs[ip] = true
		}
	}

	// IMPORTANT: Rules are evaluated in order they're added in OVN
	// We must add ALLOW rules BEFORE REJECT rules

	// Step 0: Allow traffic to host/local network (FIRST - highest priority)
	// This allows response traffic from container back to host/local network
	// Note: Incus OVN ACLs don't support connection tracking, so we allow all traffic to these destinations
	if cfg.AllowLocalNetworkAccess {
		// Allow to entire local network (useful for tmux across machines)
		// When enabled, RFC1918 blocking is disabled to allow full local network access
		rules = append(rules, "egress action=allow destination=10.0.0.0/8")
		rules = append(rules, "egress action=allow destination=172.16.0.0/12")
		rules = append(rules, "egress action=allow destination=192.168.0.0/16")
	} else if gatewayIP != "" {
		// Default: only allow to gateway IP (host only)
		rules = append(rules, fmt.Sprintf("egress action=allow destination=%s/32", gatewayIP))
	}

	// Step 1: Allow specific IPs from resolved domains (added first, highest priority)
	// Sort IPs for deterministic ordering (makes debugging and testing easier)
	sortedIPs := make([]string, 0, len(uniqueIPs))
	for ip := range uniqueIPs {
		sortedIPs = append(sortedIPs, ip)
	}
	sort.Strings(sortedIPs)

	for _, ip := range sortedIPs {
		// Use /32 for single IP precision
		rules = append(rules, fmt.Sprintf("egress action=allow destination=%s/32", ip))
	}

	// Step 2: Block RFC1918 and metadata (but specific IPs/ranges were already allowed above)
	// Skip if allow_local_network_access is enabled (already allowed all local networks)
	if !cfg.AllowLocalNetworkAccess {
		rules = append(rules, "egress action=reject destination=10.0.0.0/8")
		rules = append(rules, "egress action=reject destination=172.16.0.0/12")
		rules = append(rules, "egress action=reject destination=192.168.0.0/16")
		rules = append(rules, "egress action=reject destination=169.254.0.0/16")
	}

	// Note: We don't add a catch-all reject (0.0.0.0/0) because OVN's ACL system
	// applies implicit default-deny when ACLs are attached to a NIC. Adding an
	// explicit 0.0.0.0/0 reject interferes with OVN's internal routing.

	return rules
}
