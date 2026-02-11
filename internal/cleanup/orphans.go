package cleanup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/network"
)

// OrphanedResources holds information about orphaned system resources
type OrphanedResources struct {
	Veths                 []string // Orphaned veth interfaces (no master bridge)
	FirewallRules         []string // Orphaned firewall rules (for non-existent container IPs)
	FirewalldZoneBindings []string // Orphaned firewalld zone bindings (veths in zones but not on system)
}

// DetectOrphanedVeths finds veth interfaces that have no master bridge
// These are typically left over from improperly cleaned up containers
func DetectOrphanedVeths() ([]string, error) {
	var orphaned []string

	// Read all network interfaces from /sys/class/net
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("failed to read network interfaces: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()

		// Only check veth interfaces
		if !strings.HasPrefix(name, "veth") {
			continue
		}

		// Check if this veth has a master (bridge)
		masterPath := fmt.Sprintf("/sys/class/net/%s/master", name)
		if _, err := os.Stat(masterPath); os.IsNotExist(err) {
			// No master symlink = orphaned veth
			orphaned = append(orphaned, name)
		}
	}

	return orphaned, nil
}

// DetectOrphanedFirewallRules finds firewall rules for IPs that don't belong to any running container
func DetectOrphanedFirewallRules() ([]string, error) {
	if !network.FirewallAvailable() {
		return nil, nil
	}

	// Get all direct rules
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--direct", "--get-all-rules")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list firewall rules: %w", err)
	}

	// Get all running container IPs
	containerIPs, err := getRunningContainerIPs()
	if err != nil {
		return nil, fmt.Errorf("failed to get container IPs: %w", err)
	}

	var orphaned []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		rule := strings.TrimSpace(scanner.Text())
		if rule == "" || !strings.Contains(rule, "FORWARD") {
			continue
		}

		// Skip the base conntrack rule
		if strings.Contains(rule, "conntrack") {
			continue
		}

		// Check if this rule references a container IP that no longer exists
		isOrphaned := true
		for _, ip := range containerIPs {
			if strings.Contains(rule, ip) {
				isOrphaned = false
				break
			}
		}

		// Only consider rules with container-like IPs (10.x.x.x pattern) as potentially orphaned
		if isOrphaned && strings.Contains(rule, "10.") {
			orphaned = append(orphaned, rule)
		}
	}

	return orphaned, nil
}

// getRunningContainerIPs returns IPs of all running containers
func getRunningContainerIPs() ([]string, error) {
	output, err := container.IncusOutput("list", "--format=json")
	if err != nil {
		return nil, err
	}

	var containers []struct {
		Name  string `json:"name"`
		State struct {
			Status  string `json:"status"`
			Network map[string]struct {
				Addresses []struct {
					Family  string `json:"family"`
					Address string `json:"address"`
				} `json:"addresses"`
			} `json:"network"`
		} `json:"state"`
	}

	if err := parseJSON(output, &containers); err != nil {
		return nil, err
	}

	var ips []string
	for _, c := range containers {
		if c.State.Status != "Running" {
			continue
		}
		if eth0, ok := c.State.Network["eth0"]; ok {
			for _, addr := range eth0.Addresses {
				if addr.Family == "inet" {
					ips = append(ips, addr.Address)
				}
			}
		}
	}

	return ips, nil
}

// parseJSON is a helper to parse JSON output
func parseJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}

// CleanupOrphanedVeths removes orphaned veth interfaces
// Returns the number of veths cleaned up and any error
func CleanupOrphanedVeths(veths []string, logger func(string)) (int, error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	cleaned := 0
	for _, veth := range veths {
		logger(fmt.Sprintf("Removing orphaned veth: %s", veth))
		cmd := exec.Command("sudo", "-n", "ip", "link", "delete", veth)
		if err := cmd.Run(); err != nil {
			logger(fmt.Sprintf("  Warning: Failed to remove %s: %v", veth, err))
			continue
		}
		cleaned++
	}

	return cleaned, nil
}

// CleanupOrphanedFirewallRules removes orphaned firewall rules
// Returns the number of rules cleaned up and any error
func CleanupOrphanedFirewallRules(rules []string, logger func(string)) (int, error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	cleaned := 0
	for _, rule := range rules {
		logger(fmt.Sprintf("Removing orphaned firewall rule: %s", rule))

		// Parse rule and remove it
		parts := strings.Fields(rule)
		if len(parts) < 4 {
			continue
		}

		args := []string{"-n", "firewall-cmd", "--direct", "--remove-rule"}
		args = append(args, parts...)

		cmd := exec.Command("sudo", args...)
		if err := cmd.Run(); err != nil {
			logger(fmt.Sprintf("  Warning: Failed to remove rule: %v", err))
			continue
		}
		cleaned++
	}

	return cleaned, nil
}

// DetectAll detects all orphaned resources
func DetectAll() (*OrphanedResources, error) {
	result := &OrphanedResources{}

	veths, err := DetectOrphanedVeths()
	if err != nil {
		return nil, fmt.Errorf("failed to detect orphaned veths: %w", err)
	}
	result.Veths = veths

	rules, err := DetectOrphanedFirewallRules()
	if err != nil {
		// Non-fatal - firewall might not be available
		log.Printf("Warning: Could not check firewall rules: %v", err)
	}
	result.FirewallRules = rules

	zoneBindings, err := network.DetectOrphanedFirewalldZoneBindings()
	if err != nil {
		// Non-fatal - firewalld might not be available
		log.Printf("Warning: Could not check firewalld zone bindings: %v", err)
	}
	result.FirewalldZoneBindings = zoneBindings

	return result, nil
}

// CleanupAll cleans up all orphaned resources
func CleanupAll(logger func(string)) (vethsCleaned, rulesCleaned, zoneBindingsCleaned int, err error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	orphans, err := DetectAll()
	if err != nil {
		return 0, 0, 0, err
	}

	if len(orphans.Veths) > 0 {
		vethsCleaned, _ = CleanupOrphanedVeths(orphans.Veths, logger)
	}

	if len(orphans.FirewallRules) > 0 {
		rulesCleaned, _ = CleanupOrphanedFirewallRules(orphans.FirewallRules, logger)
	}

	if len(orphans.FirewalldZoneBindings) > 0 {
		zoneBindingsCleaned, _ = network.CleanupOrphanedFirewalldZoneBindings(orphans.FirewalldZoneBindings, logger)
	}

	return vethsCleaned, rulesCleaned, zoneBindingsCleaned, nil
}

// HasOrphans returns true if there are any orphaned resources
func HasOrphans() bool {
	orphans, err := DetectAll()
	if err != nil {
		return false
	}
	return len(orphans.Veths) > 0 || len(orphans.FirewallRules) > 0 || len(orphans.FirewalldZoneBindings) > 0
}

// CleanupOrphanedFirewalldZoneBindings removes orphaned veth interfaces from firewalld zones
// This is a wrapper around network.CleanupOrphanedFirewalldZoneBindings
func CleanupOrphanedFirewalldZoneBindings(veths []string, logger func(string)) (int, error) {
	return network.CleanupOrphanedFirewalldZoneBindings(veths, logger)
}
