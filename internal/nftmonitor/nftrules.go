package nftmonitor

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RuleManager manages nftables LOG rules for container monitoring
type RuleManager struct {
	config      *Config
	ruleHandles []string // Store rule handles for cleanup
}

// NewRuleManager creates a new nftables rule manager
func NewRuleManager(cfg *Config) *RuleManager {
	return &RuleManager{
		config:      cfg,
		ruleHandles: []string{},
	}
}

// AddRules adds nftables LOG rules for the container
func (rm *RuleManager) AddRules() error {
	// Rule 1: High priority - Always log suspicious traffic (no rate limit)
	// Metadata endpoint, RFC1918, suspicious ports
	if err := rm.addSuspiciousTrafficRule(); err != nil {
		return fmt.Errorf("failed to add suspicious traffic rule: %w", err)
	}

	// Rule 2: Medium priority - Always log DNS queries (if enabled)
	if rm.config.LogDNSQueries {
		if err := rm.addDNSRule(); err != nil {
			return fmt.Errorf("failed to add DNS rule: %w", err)
		}
	}

	// Rule 3: Low priority - Rate-limited logging for all other traffic
	if err := rm.addGeneralTrafficRule(); err != nil {
		return fmt.Errorf("failed to add general traffic rule: %w", err)
	}

	return nil
}

// RemoveRules removes all nftables LOG rules added by this manager
func (rm *RuleManager) RemoveRules() error {
	// List all rules with handles in FORWARD chain
	output, err := rm.runNFTCommand("list", "chain", "ip", "filter", "FORWARD", "-a")
	if err != nil {
		return fmt.Errorf("failed to list rules: %w", err)
	}

	// Find and delete all rules with our log prefix
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if strings.Contains(line, fmt.Sprintf("NFT_COI[%s]", rm.config.ContainerIP)) ||
			strings.Contains(line, fmt.Sprintf("NFT_DNS[%s]", rm.config.ContainerIP)) ||
			strings.Contains(line, fmt.Sprintf("NFT_SUSPICIOUS[%s]", rm.config.ContainerIP)) {
			// Extract handle number from line like: "... # handle 123"
			if handle := extractHandle(line); handle != "" {
				if err := rm.deleteRuleByHandle(handle); err != nil {
					return fmt.Errorf("failed to delete rule handle %s: %w", handle, err)
				}
			}
		}
	}

	return nil
}

// addSuspiciousTrafficRule adds high-priority rule for suspicious destinations
func (rm *RuleManager) addSuspiciousTrafficRule() error {
	// Metadata endpoint + RFC1918 addresses + suspicious ports
	ruleArgs := []string{
		"add", "rule", "ip", "filter", "FORWARD",
		"ip", "saddr", rm.config.ContainerIP,
		"ip", "daddr", "{", "169.254.169.254", ",", "10.0.0.0/8", ",", "172.16.0.0/12", ",", "192.168.0.0/16", "}",
		"log", "prefix", fmt.Sprintf("NFT_SUSPICIOUS[%s]: ", rm.config.ContainerIP),
		"level", "info",
		"counter",
	}

	_, err := rm.runNFTCommand(ruleArgs...)
	if err != nil {
		return err
	}

	// Add rule for suspicious ports
	suspiciousPorts := []string{"4444", "5555", "1234", "31337", "12345", "6666-6697"}
	portsArgs := []string{
		"add", "rule", "ip", "filter", "FORWARD",
		"ip", "saddr", rm.config.ContainerIP,
		"tcp", "dport", "{",
	}
	portsArgs = append(portsArgs, strings.Join(suspiciousPorts, ","))
	portsArgs = append(portsArgs, "}", "log", "prefix", fmt.Sprintf("NFT_SUSPICIOUS[%s]: ", rm.config.ContainerIP),
		"level", "info", "counter")

	_, err = rm.runNFTCommand(portsArgs...)
	return err
}

// addDNSRule adds medium-priority rule for DNS queries
func (rm *RuleManager) addDNSRule() error {
	ruleArgs := []string{
		"add", "rule", "ip", "filter", "FORWARD",
		"ip", "saddr", rm.config.ContainerIP,
		"udp", "dport", "53",
		"log", "prefix", fmt.Sprintf("NFT_DNS[%s]: ", rm.config.ContainerIP),
		"level", "info",
		"counter",
	}

	_, err := rm.runNFTCommand(ruleArgs...)
	return err
}

// addGeneralTrafficRule adds low-priority rate-limited rule for all traffic
func (rm *RuleManager) addGeneralTrafficRule() error {
	rateLimit := rm.config.RateLimitPerSecond
	if rateLimit <= 0 {
		rateLimit = 100 // Default
	}

	ruleArgs := []string{
		"add", "rule", "ip", "filter", "FORWARD",
		"ip", "saddr", rm.config.ContainerIP,
		"limit", "rate", fmt.Sprintf("%d/second", rateLimit),
		"log", "prefix", fmt.Sprintf("NFT_COI[%s]: ", rm.config.ContainerIP),
		"level", "info",
		"counter",
	}

	_, err := rm.runNFTCommand(ruleArgs...)
	return err
}

// deleteRuleByHandle deletes a rule by its handle number
func (rm *RuleManager) deleteRuleByHandle(handle string) error {
	_, err := rm.runNFTCommand("delete", "rule", "ip", "filter", "FORWARD", "handle", handle)
	return err
}

// runNFTCommand executes an nft command with proper sudo/lima handling
func (rm *RuleManager) runNFTCommand(args ...string) ([]byte, error) {
	var cmd *exec.Cmd

	if rm.config.LimaHost != "" {
		// Run inside Lima VM
		cmdArgs := append([]string{"shell", rm.config.LimaHost, "sudo", "nft"}, args...)
		cmd = exec.Command("limactl", cmdArgs...)
	} else {
		// Native Linux - use sudo
		cmdArgs := append([]string{"nft"}, args...)
		cmd = exec.Command("sudo", cmdArgs...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("nft command failed: %w (output: %s)", err, string(output))
	}

	return output, nil
}

// extractHandle extracts the handle number from a nft rule line
// Example: "... # handle 123" -> "123"
func extractHandle(line string) string {
	parts := strings.Split(line, "# handle ")
	if len(parts) < 2 {
		return ""
	}
	handleStr := strings.TrimSpace(parts[1])
	// Handle might have additional text after it
	if idx := strings.Index(handleStr, " "); idx != -1 {
		handleStr = handleStr[:idx]
	}
	// Validate it's a number
	if _, err := strconv.Atoi(handleStr); err != nil {
		return ""
	}
	return handleStr
}
