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
	Veths               []string // Orphaned veth interfaces (no master bridge)
	NftRules            []string // Orphaned nft rules (for non-existent container IPs)
	NFTMonitorRules     []string // Orphaned nft monitoring rules (NFT_COI/NFT_DNS/NFT_SUSPICIOUS)
	IptablesBridgeRules []string // Orphaned iptables coi-bridge-forward rules (no coi containers running)
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

// DetectOrphanedNftRules finds container IPs in the ip coi forward chain
// that don't belong to any running container.
func DetectOrphanedNftRules() ([]string, error) {
	ipHandles, err := network.ListCOIFilterRuleIPs()
	if err != nil || len(ipHandles) == 0 {
		return nil, err
	}

	containerIPs, err := getRunningContainerIPs()
	if err != nil {
		return nil, fmt.Errorf("failed to get container IPs: %w", err)
	}

	running := make(map[string]bool, len(containerIPs))
	for _, ip := range containerIPs {
		running[ip] = true
	}

	var orphaned []string
	for ip := range ipHandles {
		if !running[ip] {
			orphaned = append(orphaned, ip)
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

// CleanupOrphanedNftRules removes all ip coi forward rules for the given container IPs.
// The rules slice contains container IP addresses (as returned by DetectOrphanedNftRules).
func CleanupOrphanedNftRules(rules []string, logger func(string)) (int, error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	cleaned := 0
	for _, ip := range rules {
		logger(fmt.Sprintf("Removing orphaned nft rules for container IP: %s", ip))
		if err := network.DeleteCOIFilterRulesForIP(ip); err != nil {
			logger(fmt.Sprintf("  Warning: Failed to remove rules for %s: %v", ip, err))
			continue
		}
		cleaned++
	}
	return cleaned, nil
}

// DetectOrphanedNFTMonitorRules finds nft monitoring rules for IPs that don't belong to any running container
// These are rules with prefixes: NFT_COI[ip], NFT_DNS[ip], NFT_SUSPICIOUS[ip]
func DetectOrphanedNFTMonitorRules() ([]string, error) {
	// List all rules in the FORWARD chain with handles
	// Note: -a must come before the command (nft -a list chain...)
	cmd := exec.Command("sudo", "-n", "nft", "-a", "list", "chain", "ip", "filter", "FORWARD")
	output, err := cmd.Output()
	if err != nil {
		// Chain might not exist, which is fine
		return nil, nil
	}

	// Get all running container IPs
	containerIPs, err := getRunningContainerIPs()
	if err != nil {
		return nil, fmt.Errorf("failed to get container IPs: %w", err)
	}

	var orphaned []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Look for our monitoring rule prefixes
		if !strings.Contains(line, "NFT_COI[") &&
			!strings.Contains(line, "NFT_DNS[") &&
			!strings.Contains(line, "NFT_SUSPICIOUS[") {
			continue
		}

		// Extract handle for cleanup
		handleIdx := strings.Index(line, "# handle ")
		if handleIdx == -1 {
			continue
		}

		// Check if any running container IP is referenced in this rule
		isOrphaned := true
		for _, ip := range containerIPs {
			if strings.Contains(line, ip) {
				isOrphaned = false
				break
			}
		}

		if isOrphaned {
			// Store the handle number for cleanup
			handlePart := line[handleIdx+9:] // skip "# handle "
			if spaceIdx := strings.Index(handlePart, " "); spaceIdx != -1 {
				handlePart = handlePart[:spaceIdx]
			}
			handlePart = strings.TrimSpace(handlePart)
			orphaned = append(orphaned, handlePart)
		}
	}

	return orphaned, nil
}

// CleanupOrphanedNFTMonitorRules removes orphaned nft monitoring rules by handle
func CleanupOrphanedNFTMonitorRules(handles []string, logger func(string)) (int, error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	cleaned := 0
	for _, handle := range handles {
		logger(fmt.Sprintf("Removing orphaned nft monitoring rule handle: %s", handle))

		cmd := exec.Command("sudo", "-n", "nft", "delete", "rule", "ip", "filter", "FORWARD", "handle", handle)
		if err := cmd.Run(); err != nil {
			logger(fmt.Sprintf("  Warning: Failed to remove rule handle %s: %v", handle, err))
			continue
		}
		cleaned++
	}

	return cleaned, nil
}

// DetectOrphanedIptablesBridgeRules finds coi-bridge-forward iptables rules
// that are left over when no coi containers are running
func DetectOrphanedIptablesBridgeRules() ([]string, error) {
	if !network.IptablesAvailable() {
		return nil, nil
	}

	// Check if any coi containers are running; if yes, rules are not orphaned
	containerIPs, err := getRunningContainerIPs()
	if err != nil {
		return nil, fmt.Errorf("failed to get container IPs: %w", err)
	}
	if len(containerIPs) > 0 {
		return nil, nil
	}

	// List FORWARD chain rules and look for our comment tag
	cmd := exec.Command("sudo", "-n", "iptables", "-S", "FORWARD")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil // iptables might not be accessible
	}

	var orphaned []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "coi-bridge-forward") {
			orphaned = append(orphaned, line)
		}
	}

	return orphaned, nil
}

// CleanupOrphanedIptablesBridgeRules removes orphaned coi-bridge-forward iptables rules
func CleanupOrphanedIptablesBridgeRules(rules []string, logger func(string)) (int, error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	if len(rules) == 0 {
		return 0, nil
	}

	bridgeName, err := network.GetIncusBridgeName()
	if err != nil {
		logger(fmt.Sprintf("Warning: could not get bridge name for iptables cleanup: %v", err))
		return 0, err
	}

	logger(fmt.Sprintf("Removing orphaned iptables bridge rules for %s", bridgeName))
	if err := network.RemoveIptablesBridgeRules(bridgeName); err != nil {
		logger(fmt.Sprintf("  Warning: Failed to remove iptables bridge rules: %v", err))
		return 0, err
	}

	return len(rules), nil
}

// DetectAll detects all orphaned resources
func DetectAll() (*OrphanedResources, error) {
	result := &OrphanedResources{}

	veths, err := DetectOrphanedVeths()
	if err != nil {
		return nil, fmt.Errorf("failed to detect orphaned veths: %w", err)
	}
	result.Veths = veths

	rules, err := DetectOrphanedNftRules()
	if err != nil {
		// Non-fatal - firewall might not be available
		log.Printf("Warning: Could not check nft rules: %v", err)
	}
	result.NftRules = rules

	nftRules, err := DetectOrphanedNFTMonitorRules()
	if err != nil {
		// Non-fatal - nft might not be available
		log.Printf("Warning: Could not check nft monitoring rules: %v", err)
	}
	result.NFTMonitorRules = nftRules

	iptablesBridgeRules, err := DetectOrphanedIptablesBridgeRules()
	if err != nil {
		// Non-fatal - iptables might not be available
		log.Printf("Warning: Could not check iptables bridge rules: %v", err)
	}
	result.IptablesBridgeRules = iptablesBridgeRules

	return result, nil
}

// CleanupAll cleans up all orphaned resources
func CleanupAll(logger func(string)) (vethsCleaned, rulesCleaned, nftRulesCleaned, iptablesBridgeRulesCleaned int, err error) {
	if logger == nil {
		logger = func(msg string) { log.Println(msg) }
	}

	orphans, err := DetectAll()
	if err != nil {
		return 0, 0, 0, 0, err
	}

	if len(orphans.Veths) > 0 {
		vethsCleaned, _ = CleanupOrphanedVeths(orphans.Veths, logger)
	}

	if len(orphans.NftRules) > 0 {
		rulesCleaned, _ = CleanupOrphanedNftRules(orphans.NftRules, logger)
	}

	if len(orphans.NFTMonitorRules) > 0 {
		nftRulesCleaned, _ = CleanupOrphanedNFTMonitorRules(orphans.NFTMonitorRules, logger)
	}

	if len(orphans.IptablesBridgeRules) > 0 {
		iptablesBridgeRulesCleaned, _ = CleanupOrphanedIptablesBridgeRules(orphans.IptablesBridgeRules, logger)
	}

	return vethsCleaned, rulesCleaned, nftRulesCleaned, iptablesBridgeRulesCleaned, nil
}

// HasOrphans returns true if there are any orphaned resources
func HasOrphans() bool {
	orphans, err := DetectAll()
	if err != nil {
		return false
	}
	return len(orphans.Veths) > 0 || len(orphans.NftRules) > 0 || len(orphans.NFTMonitorRules) > 0 || len(orphans.IptablesBridgeRules) > 0
}
