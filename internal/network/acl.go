package network

import (
	"fmt"
	"strings"

	"github.com/mensfeld/claude-on-incus/internal/config"
	"github.com/mensfeld/claude-on-incus/internal/container"
)

// ACLManager manages Incus network ACLs for container isolation
type ACLManager struct{}

// Create creates a new network ACL with the specified rules
func (m *ACLManager) Create(name string, cfg *config.NetworkConfig) error {
	// First, check if ACL already exists and delete it
	// This handles cases where ACL wasn't cleaned up properly
	_ = m.Delete(name) // Ignore error if ACL doesn't exist

	// Create the ACL
	if err := container.IncusExecQuiet("network", "acl", "create", name); err != nil {
		return fmt.Errorf("failed to create ACL %s: %w", name, err)
	}

	// Build and add rules
	rules := buildACLRules(cfg)
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
		return fmt.Errorf("failed to set ACL property: %w", err)
	}

	return nil
}

// Delete removes a network ACL
func (m *ACLManager) Delete(name string) error {
	// Delete ACL (use quiet to suppress error if doesn't exist)
	return container.IncusExecQuiet("network", "acl", "delete", name)
}

// buildACLRules generates ACL rules based on network configuration
func buildACLRules(cfg *config.NetworkConfig) []string {
	rules := []string{}

	// In restricted mode, block local networks
	if cfg.Mode == config.NetworkModeRestricted {
		// First, add allow rules for all traffic (lower priority)
		// This ensures non-blocked traffic is explicitly allowed
		rules = append(rules, "egress action=allow")

		// Then add reject rules for specific ranges (higher priority, evaluated first)
		// Block private ranges (RFC1918)
		if cfg.BlockPrivateNetworks {
			rules = append(rules, "egress action=reject destination=10.0.0.0/8")
			rules = append(rules, "egress action=reject destination=172.16.0.0/12")
			rules = append(rules, "egress action=reject destination=192.168.0.0/16")
		}

		// Block cloud metadata endpoints
		if cfg.BlockMetadataEndpoint {
			rules = append(rules, "egress action=reject destination=169.254.0.0/16")
		}
	}

	return rules
}
