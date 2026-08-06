package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/monitor"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/nftmonitor"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/tool"
	"github.com/mensfeld/code-on-incus/internal/vmhost"
)

// waitProbeReady waits up to 30s for a just-launched probe container via the
// shared session.WaitForReady chokepoint (silent logger — health probes don't
// narrate). The former hand-rolled loops here retried through transient
// ContainerRunning errors; WaitForReady fails fast on them instead, which for
// a probe is the same verdict (an erroring incus is a failed check) delivered
// sooner.
func waitProbeReady(containerName string) bool {
	return session.WaitForReady(context.Background(), container.NewManager(containerName), 30, func(string) {}) == nil
}

// CheckOS reports the operating system information
func CheckOS() HealthCheck {
	// Get OS and architecture
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Try to get more detailed OS info on Linux
	var details string
	var environment string

	if osName == "linux" {
		// Try to read /etc/os-release for distribution info
		if content, err := os.ReadFile("/etc/os-release"); err == nil {
			lines := strings.Split(string(content), "\n")
			var prettyName string
			for _, line := range lines {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					prettyName = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
					break
				}
			}
			if prettyName != "" {
				details = prettyName
			}
		}

		// Detect if running in Colima/Lima VM
		if vmhost.Detect() == vmhost.KindLimaLike {
			environment = "colima"
		}
	} else if osName == "darwin" {
		// Get macOS version
		cmd := exec.Command("sw_vers", "-productVersion")
		if output, err := cmd.Output(); err == nil {
			details = "macOS " + strings.TrimSpace(string(output))
		}
	}

	message := fmt.Sprintf("%s/%s", osName, arch)
	if details != "" {
		message = fmt.Sprintf("%s (%s)", details, arch)
	}
	if environment != "" {
		message += fmt.Sprintf(" [%s]", environment)
	}

	return HealthCheck{
		Name:    "os",
		Status:  StatusOK,
		Message: message,
		Details: map[string]interface{}{
			"os":          osName,
			"arch":        arch,
			"details":     details,
			"environment": environment,
		},
	}
}

// CheckIncus verifies that Incus is available and running
func CheckIncus() HealthCheck {
	// Check if incus binary exists
	if _, err := exec.LookPath("incus"); err != nil {
		return HealthCheck{
			Name:    "incus",
			Status:  StatusFailed,
			Message: "Incus binary not found",
		}
	}

	// Check if Incus is available (daemon running and accessible)
	if !container.Available() {
		return HealthCheck{
			Name:    "incus",
			Status:  StatusFailed,
			Message: "Incus daemon not running or not accessible",
		}
	}

	// Get Incus version
	versionOutput, err := container.IncusOutput("version")
	if err != nil {
		return HealthCheck{
			Name:    "incus",
			Status:  StatusOK,
			Message: "Running (version unknown)",
		}
	}

	return evaluateIncusVersion(versionOutput)
}

// evaluateIncusVersion evaluates the raw `incus version` output and returns
// the appropriate health check result, including minimum version validation.
func evaluateIncusVersion(versionOutput string) HealthCheck {
	versionStr, err := container.ExtractServerVersion(versionOutput)
	if err != nil {
		return HealthCheck{
			Name:    "incus",
			Status:  StatusOK,
			Message: "Running (version unknown)",
		}
	}

	v, err := container.ParseIncusVersion(versionStr)
	if err != nil {
		return HealthCheck{
			Name:    "incus",
			Status:  StatusOK,
			Message: fmt.Sprintf("Running (version %s)", versionStr),
			Details: map[string]interface{}{
				"version": versionStr,
			},
		}
	}

	if !container.MeetsMinimumVersion(v) {
		return HealthCheck{
			Name:    "incus",
			Status:  StatusWarning,
			Message: container.FormatMinVersionError(v),
			Details: map[string]interface{}{
				"version": versionStr,
			},
		}
	}

	return HealthCheck{
		Name:    "incus",
		Status:  StatusOK,
		Message: fmt.Sprintf("Running (version %s)", versionStr),
		Details: map[string]interface{}{
			"version": versionStr,
		},
	}
}

// CheckPermissions verifies user has correct group membership
func CheckPermissions() HealthCheck {
	// On macOS, no group check needed
	if runtime.GOOS == "darwin" {
		return HealthCheck{
			Name:    "permissions",
			Status:  StatusOK,
			Message: "macOS - no group required",
		}
	}

	// Get current user
	currentUser, err := user.Current()
	if err != nil {
		return HealthCheck{
			Name:    "permissions",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not determine current user: %v", err),
		}
	}

	// Use os.Getgroups() (getgroups(2)) to get the actual supplementary GIDs
	// of the current process — not the group database. This correctly reflects
	// whether the running session has picked up a recent usermod change.
	activeGIDs, err := os.Getgroups()
	if err != nil {
		return HealthCheck{
			Name:    "permissions",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not determine active session groups: %v", err),
		}
	}

	// Look for incus-admin group
	incusGroup, err := user.LookupGroup("incus-admin")
	if err != nil {
		return HealthCheck{
			Name:    "permissions",
			Status:  StatusFailed,
			Message: "incus-admin group not found",
		}
	}

	// Check if the group is active in the current session.
	incusGID, _ := strconv.Atoi(incusGroup.Gid)
	for _, gid := range activeGIDs {
		if gid == incusGID {
			return HealthCheck{
				Name:    "permissions",
				Status:  StatusOK,
				Message: "User in incus-admin group",
				Details: map[string]interface{}{
					"user":  currentUser.Username,
					"group": "incus-admin",
				},
			}
		}
	}

	// Not active in session — check if they were added but haven't re-logged in.
	if container.UserInGroupFile(currentUser.Username, "incus-admin") {
		return HealthCheck{
			Name:    "permissions",
			Status:  StatusWarning,
			Message: fmt.Sprintf("User '%s' is in incus-admin group but session not reloaded — log out and back in, or run: newgrp incus-admin", currentUser.Username),
		}
	}

	return HealthCheck{
		Name:    "permissions",
		Status:  StatusFailed,
		Message: fmt.Sprintf("User '%s' not in incus-admin group — run: sudo usermod -aG incus-admin $USER", currentUser.Username),
	}
}

// CheckImage verifies that the default image exists
func CheckImage(imageName string) HealthCheck {
	if imageName == "" {
		imageName = "coi-default"
	}

	exists, err := container.ImageExists(imageName)
	if err != nil {
		return HealthCheck{
			Name:    "image",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not check image: %v", err),
		}
	}

	if !exists {
		return HealthCheck{
			Name:    "image",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Image '%s' not found (run 'coi build')", imageName),
			Details: map[string]interface{}{
				"expected": imageName,
			},
		}
	}

	// Get image fingerprint
	output, err := container.IncusOutput("image", "list", imageName, "--format=csv", "-c", "f")
	fingerprint := ""
	if err == nil && output != "" {
		lines := strings.Split(output, "\n")
		if len(lines) > 0 {
			fingerprint = strings.TrimSpace(lines[0])
			if len(fingerprint) > 12 {
				fingerprint = fingerprint[:12] + "..."
			}
		}
	}

	message := imageName
	if fingerprint != "" {
		message = fmt.Sprintf("%s (fingerprint: %s)", imageName, fingerprint)
	}

	return HealthCheck{
		Name:    "image",
		Status:  StatusOK,
		Message: message,
		Details: map[string]interface{}{
			"alias":       imageName,
			"fingerprint": fingerprint,
		},
	}
}

// CheckNetworkBridge verifies the network bridge is configured
func CheckNetworkBridge() HealthCheck {
	networkName, err := network.GetIncusBridgeName()
	if err != nil {
		return HealthCheck{
			Name:    "network_bridge",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Could not determine bridge name: %v", err),
		}
	}

	// Get network configuration
	networkOutput, err := container.IncusOutput("network", "show", networkName)
	if err != nil {
		return HealthCheck{
			Name:    "network_bridge",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not get network info for %s: %v", networkName, err),
		}
	}

	// Parse IPv4 address
	var ipv4Address string
	for _, line := range strings.Split(networkOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ipv4.address:") {
			ipv4Address = strings.TrimSpace(strings.TrimPrefix(line, "ipv4.address:"))
			break
		}
	}

	if ipv4Address == "" || ipv4Address == "none" {
		return HealthCheck{
			Name:    "network_bridge",
			Status:  StatusFailed,
			Message: fmt.Sprintf("%s has no IPv4 address", networkName),
		}
	}

	return HealthCheck{
		Name:    "network_bridge",
		Status:  StatusOK,
		Message: fmt.Sprintf("%s (%s)", networkName, ipv4Address),
		Details: map[string]interface{}{
			"name": networkName,
			"ipv4": ipv4Address,
		},
	}
}

// CheckIPForwarding verifies IP forwarding is enabled
func CheckIPForwarding() HealthCheck {
	// On macOS, IP forwarding works differently
	if runtime.GOOS == "darwin" {
		return HealthCheck{
			Name:    "ip_forwarding",
			Status:  StatusOK,
			Message: "macOS - managed by Incus",
		}
	}

	// Read /proc/sys/net/ipv4/ip_forward
	content, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return HealthCheck{
			Name:    "ip_forwarding",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not check: %v", err),
		}
	}

	value := strings.TrimSpace(string(content))
	if value == "1" {
		return HealthCheck{
			Name:    "ip_forwarding",
			Status:  StatusOK,
			Message: "Enabled",
		}
	}

	return HealthCheck{
		Name:    "ip_forwarding",
		Status:  StatusWarning,
		Message: "Disabled (may affect container networking)",
	}
}

// CheckNft verifies nft availability and masquerade configuration
func CheckNft(netCfg config.NetworkConfig) HealthCheck {
	mode := netCfg.Mode
	sudoAllowed := netCfg.SudoAllowed()
	installed := network.NftInstalled()
	// NftUsable returns false (without probing sudo) when use_sudo=false — a user
	// who opted out of COI invoking sudo at all.
	available := network.NftUsable(&netCfg)
	masquerade := network.MasqueradeEnabled()
	isColima := vmhost.Detect() == vmhost.KindLimaLike

	details := map[string]interface{}{
		"nft_installed": installed,
		"nft_available": available,
		"masquerade":    masquerade,
		"colima":        isColima,
		"use_sudo":      sudoAllowed,
	}

	if mode == config.NetworkModeOpen {
		return HealthCheck{
			Name:    "nft",
			Status:  StatusOK,
			Message: "nft not required for open mode",
			Details: details,
		}
	}

	// use_sudo=false with a mode that needs nft is a deliberate opt-out paired
	// with an incompatible mode. Surface it as a warning (not a hard failure)
	// pointing at the fix, rather than nagging about configuring sudo.
	if !sudoAllowed {
		return HealthCheck{
			Name:    "nft",
			Status:  StatusWarning,
			Message: fmt.Sprintf("use_sudo=false but network mode is %q, which needs nft — set [network] mode = \"open\" (restricted/allowlist require passwordless sudo)", mode),
			Details: details,
		}
	}

	// Required for restricted/allowlist modes
	if !installed {
		message := fmt.Sprintf("nft not installed (required for %s mode) — install with: sudo apt install nftables", mode)
		if isColima {
			message = "nft not installed — on Colima, set mode = \"open\" in [network] section of your config.toml"
		}
		return HealthCheck{
			Name:    "nft",
			Status:  StatusFailed,
			Message: message,
			Details: details,
		}
	}

	if !available {
		message := "nft installed but passwordless sudo not configured — run: echo \"$USER ALL=(ALL) NOPASSWD: /usr/sbin/nft\" | sudo tee /etc/sudoers.d/coi-nft && sudo chmod 0440 /etc/sudoers.d/coi-nft"
		if isColima {
			message = "nft sudo not configured — on Colima, set mode = \"open\" in [network] section of your config.toml"
		}
		return HealthCheck{
			Name:    "nft",
			Status:  StatusFailed,
			Message: message,
			Details: details,
		}
	}

	if !masquerade {
		return HealthCheck{
			Name:    "nft",
			Status:  StatusWarning,
			Message: "Incus bridge NAT (masquerade) not enabled — containers may not reach the internet. Check with: incus network get incusbr0 ipv4.nat",
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "nft",
		Status:  StatusOK,
		Message: fmt.Sprintf("nft available, Incus bridge NAT enabled (%s mode available)", mode),
		Details: details,
	}
}

// CheckUFWConflict checks if ufw is active with a DROP FORWARD policy that
// would block container traffic. With nft-based COI, ufw and COI can coexist
// as long as bridge forwarding rules are in place.
func CheckUFWConflict() HealthCheck {
	ufwInstalled := network.UfwInstalled()
	ufwActive := ufwInstalled && network.UfwActive()
	forwardDrop := network.ForwardPolicyIsDrop()

	details := map[string]interface{}{
		"ufw_installed":       ufwInstalled,
		"ufw_active":          ufwActive,
		"forward_policy_drop": forwardDrop,
	}

	if !ufwActive {
		return HealthCheck{
			Name:    "ufw_conflict",
			Status:  StatusOK,
			Message: "ufw is not active",
			Details: details,
		}
	}

	if forwardDrop {
		return HealthCheck{
			Name:    "ufw_conflict",
			Status:  StatusWarning,
			Message: "ufw is active and FORWARD policy is DROP — coi will add iptables bridge rules automatically to ensure containers can reach the network",
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "ufw_conflict",
		Status:  StatusOK,
		Message: "ufw is active but FORWARD policy is not DROP — no conflict with COI",
		Details: details,
	}
}

// CheckBridgeForwardRules checks whether the Incus bridge has iptables FORWARD
// ACCEPT rules in place when the FORWARD chain policy is DROP.  When the policy
// is ACCEPT (the common case) no per-bridge rules are needed and the check
// returns OK regardless of whether rules are present.
func CheckBridgeForwardRules() HealthCheck {
	hasRules, bridgeName, err := network.BridgeInTrustedZone()
	if err != nil {
		return HealthCheck{
			Name:    "bridge_forward_rules",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not check bridge forwarding rules: %v", err),
		}
	}

	forwardDrop := network.ForwardPolicyIsDrop()

	details := map[string]interface{}{
		"bridge_name":         bridgeName,
		"has_forward_rules":   hasRules,
		"forward_policy_drop": forwardDrop,
	}

	if !hasRules && forwardDrop {
		return HealthCheck{
			Name:    "bridge_forward_rules",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Bridge %s has no iptables FORWARD rules and FORWARD policy is DROP — rules are added automatically on first container start", bridgeName),
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "bridge_forward_rules",
		Status:  StatusOK,
		Message: fmt.Sprintf("Bridge %s forwarding OK", bridgeName),
		Details: details,
	}
}

// CheckIptablesSudo verifies passwordless sudo for iptables, which COI uses
// for bridge FORWARD rule inspection and management regardless of whether nft
// is also available.
func CheckIptablesSudo() HealthCheck {
	if runtime.GOOS == "darwin" {
		return HealthCheck{
			Name:    "iptables_sudo",
			Status:  StatusOK,
			Message: "macOS — not required",
		}
	}

	if !network.SudoEnabled() {
		return HealthCheck{
			Name:    "iptables_sudo",
			Status:  StatusOK,
			Message: "skipped — [network] use_sudo = false (COI does not invoke sudo)",
		}
	}

	iptablesPath, err := exec.LookPath("iptables")
	if err != nil {
		return HealthCheck{
			Name:    "iptables_sudo",
			Status:  StatusOK,
			Message: "iptables not installed — not required",
		}
	}

	if exec.Command("sudo", "-n", iptablesPath, "-L", "FORWARD", "-n").Run() != nil {
		return HealthCheck{
			Name:    "iptables_sudo",
			Status:  StatusWarning,
			Message: fmt.Sprintf(`Passwordless sudo not configured for iptables — run: echo "$USER ALL=(ALL) NOPASSWD: %s" | sudo tee /etc/sudoers.d/coi-iptables && sudo chmod 0440 /etc/sudoers.d/coi-iptables`, iptablesPath),
		}
	}

	return HealthCheck{
		Name:    "iptables_sudo",
		Status:  StatusOK,
		Message: "Passwordless sudo configured for iptables",
	}
}

// CheckCOIDirectory verifies the COI directory exists and is writable
func CheckCOIDirectory() HealthCheck {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return HealthCheck{
			Name:    "coi_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Could not determine home directory: %v", err),
		}
	}

	coiDir := filepath.Join(homeDir, ".coi")

	// Check if directory exists
	info, err := os.Stat(coiDir)
	if os.IsNotExist(err) {
		return HealthCheck{
			Name:    "coi_directory",
			Status:  StatusWarning,
			Message: fmt.Sprintf("%s does not exist (will be created on first run)", coiDir),
		}
	}
	if err != nil {
		return HealthCheck{
			Name:    "coi_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Could not access %s: %v", coiDir, err),
		}
	}

	if !info.IsDir() {
		return HealthCheck{
			Name:    "coi_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("%s is not a directory", coiDir),
		}
	}

	// Check if writable by creating a temp file
	testFile := filepath.Join(coiDir, ".health-check-test")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		return HealthCheck{
			Name:    "coi_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("%s is not writable", coiDir),
		}
	}
	os.Remove(testFile)

	return HealthCheck{
		Name:    "coi_directory",
		Status:  StatusOK,
		Message: "~/.coi (writable)",
		Details: map[string]interface{}{
			"path": coiDir,
		},
	}
}

// CheckSessionsDirectory verifies the sessions directory exists and is writable
func CheckSessionsDirectory(cfg *config.Config) HealthCheck {
	// Get configured tool to determine sessions directory
	toolName := cfg.Tool.Name
	if toolName == "" {
		toolName = "claude"
	}
	toolInstance, err := tool.Get(toolName)
	if err != nil {
		toolInstance = tool.GetDefault()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return HealthCheck{
			Name:    "sessions_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Could not determine home directory: %v", err),
		}
	}

	baseDir := filepath.Join(homeDir, ".coi")
	sessionsDir := session.GetSessionsDir(baseDir, toolInstance)

	// Check if directory exists
	info, err := os.Stat(sessionsDir)
	if os.IsNotExist(err) {
		return HealthCheck{
			Name:    "sessions_directory",
			Status:  StatusOK,
			Message: fmt.Sprintf("%s (will be created)", filepath.Base(sessionsDir)),
			Details: map[string]interface{}{
				"path": sessionsDir,
			},
		}
	}
	if err != nil {
		return HealthCheck{
			Name:    "sessions_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Could not access %s: %v", sessionsDir, err),
		}
	}

	if !info.IsDir() {
		return HealthCheck{
			Name:    "sessions_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("%s is not a directory", sessionsDir),
		}
	}

	// Check if writable
	testFile := filepath.Join(sessionsDir, ".health-check-test")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		return HealthCheck{
			Name:    "sessions_directory",
			Status:  StatusFailed,
			Message: fmt.Sprintf("%s is not writable", sessionsDir),
		}
	}
	os.Remove(testFile)

	return HealthCheck{
		Name:    "sessions_directory",
		Status:  StatusOK,
		Message: fmt.Sprintf("~/.coi/%s (writable)", filepath.Base(sessionsDir)),
		Details: map[string]interface{}{
			"path": sessionsDir,
		},
	}
}

// CheckConfiguration verifies the configuration is loaded correctly
func CheckConfiguration(cfg *config.Config) HealthCheck {
	if cfg == nil {
		return HealthCheck{
			Name:    "config",
			Status:  StatusFailed,
			Message: "Configuration not loaded",
		}
	}

	// Find which config files exist
	configPaths := config.GetConfigPaths()
	var loadedFrom []string
	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			loadedFrom = append(loadedFrom, path)
		}
	}

	message := "Defaults only (no config files)"
	if len(loadedFrom) > 0 {
		message = loadedFrom[len(loadedFrom)-1] // Show highest priority
	}

	return HealthCheck{
		Name:    "config",
		Status:  StatusOK,
		Message: message,
		Details: map[string]interface{}{
			"loaded_from": loadedFrom,
		},
	}
}

// CheckNetworkMode reports the configured network mode
func CheckNetworkMode(mode config.NetworkMode) HealthCheck {
	if mode == "" {
		mode = config.NetworkModeRestricted
	}

	return HealthCheck{
		Name:    "network_mode",
		Status:  StatusOK,
		Message: string(mode),
		Details: map[string]interface{}{
			"mode": string(mode),
		},
	}
}

// CheckTool reports the configured tool
func CheckTool(toolName string) HealthCheck {
	if toolName == "" {
		toolName = "claude"
	}

	_, err := tool.Get(toolName)
	if err != nil {
		return HealthCheck{
			Name:    "tool",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Unknown tool: %s", toolName),
		}
	}

	return HealthCheck{
		Name:    "tool",
		Status:  StatusOK,
		Message: toolName,
		Details: map[string]interface{}{
			"name": toolName,
		},
	}
}

// CheckDNS verifies DNS resolution is working
func CheckDNS() HealthCheck {
	// Try to resolve a well-known domain
	testDomain := "api.anthropic.com"

	ips, err := net.LookupIP(testDomain)
	if err != nil {
		return HealthCheck{
			Name:    "dns_resolution",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Failed to resolve %s: %v", testDomain, err),
		}
	}

	if len(ips) == 0 {
		return HealthCheck{
			Name:    "dns_resolution",
			Status:  StatusWarning,
			Message: fmt.Sprintf("No IPs found for %s", testDomain),
		}
	}

	return HealthCheck{
		Name:    "dns_resolution",
		Status:  StatusOK,
		Message: fmt.Sprintf("Working (%s -> %d IPs)", testDomain, len(ips)),
		Details: map[string]interface{}{
			"test_domain": testDomain,
			"ip_count":    len(ips),
		},
	}
}

// CheckContainerConnectivity tests internet connectivity from inside a container
func CheckContainerConnectivity(imageName string) HealthCheck {
	// Skip if no image available
	if imageName == "" {
		imageName = "coi-default"
	}

	exists, err := container.ImageExists(imageName)
	if err != nil || !exists {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusWarning,
			Message: "Skipped (image not available)",
		}
	}

	// Create temporary container name
	containerName := fmt.Sprintf("coi-health-check-%d", time.Now().UnixNano())

	// Launch ephemeral container on the Incus default pool — this probe is
	// one-shot and pool routing isn't relevant to what we're checking.
	if err := container.LaunchContainer(imageName, containerName, ""); err != nil {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Failed to launch test container: %v", err),
		}
	}

	// Ensure cleanup on any exit path
	defer func() {
		// Ephemeral containers auto-delete when stopped, but force cleanup just in case
		_ = container.StopContainer(containerName)
		_ = container.DeleteContainer(containerName)
	}()

	// Wait for container to be ready and have network (up to 30 seconds)
	if !waitProbeReady(containerName) {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusFailed,
			Message: "Test container failed to start within timeout",
		}
	}

	// Wait for DHCP to assign an IP (up to 15 seconds)
	var containerIP string
	for i := 0; i < 15; i++ {
		ip, err := network.GetContainerIP(containerName)
		if err == nil && ip != "" {
			containerIP = ip
			break
		}
		time.Sleep(1 * time.Second)
	}

	if containerIP == "" {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusFailed,
			Message: "Container failed to get IP address (DHCP not working)",
		}
	}

	// Apply firewall rules to allow container traffic
	// (FORWARD chain policy may be DROP with Docker)
	var usedIptablesFallback bool
	var iptablesBridgeName string

	if network.NftAvailable() {
		if err := network.EnsureOpenModeRules(containerIP); err != nil {
			return HealthCheck{
				Name:    "container_connectivity",
				Status:  StatusWarning,
				Message: fmt.Sprintf("Failed to apply firewall rules: %v", err),
			}
		}
	} else if network.NeedsIptablesFallback() {
		bridgeName, err := network.GetIncusBridgeName()
		if err != nil {
			return HealthCheck{
				Name:    "container_connectivity",
				Status:  StatusWarning,
				Message: fmt.Sprintf("iptables fallback: could not get bridge name: %v", err),
			}
		}
		if err := network.EnsureIptablesBridgeRules(bridgeName); err != nil {
			return HealthCheck{
				Name:    "container_connectivity",
				Status:  StatusWarning,
				Message: fmt.Sprintf("iptables fallback: failed to add bridge rules: %v", err),
			}
		}
		usedIptablesFallback = true
		iptablesBridgeName = bridgeName
	}

	// Clean up firewall rules on exit
	defer func() {
		if usedIptablesFallback {
			_ = network.RemoveIptablesBridgeRules(iptablesBridgeName)
		} else if network.NftAvailable() {
			_ = network.RemoveOpenModeRules(containerIP)
		}
	}()

	// Give networking additional time to fully stabilize after DHCP
	time.Sleep(3 * time.Second)

	// Test 1: DNS resolution using getent
	dnsOutput, dnsErr := container.IncusOutput("exec", containerName, "--", "getent", "hosts", "api.anthropic.com")

	// Test 2: HTTP connectivity using curl
	httpOutput, httpErr := container.IncusOutput("exec", containerName, "--", "curl", "-s", "--connect-timeout", "10", "-o", "/dev/null", "-w", "%{http_code}", "https://api.anthropic.com")

	// Analyze results
	dnsOK := dnsErr == nil && dnsOutput != ""
	// Accept any HTTP response - getting a response means connectivity works
	// Common responses: 200 (OK), 401/403 (auth required), 404 (not found), 405 (method not allowed)
	httpOK := httpErr == nil && httpOutput != "" && httpOutput != "000"

	details := map[string]interface{}{
		"dns_test":  dnsOK,
		"http_test": httpOK,
	}

	if dnsOK {
		parts := strings.Fields(dnsOutput)
		if len(parts) > 0 {
			details["dns_result"] = parts[0] // First IP
		}
	}
	if httpOK {
		details["http_status"] = httpOutput
	}

	if dnsOK && httpOK {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusOK,
			Message: fmt.Sprintf("DNS and HTTP working (status %s)", httpOutput),
			Details: details,
		}
	}

	if !dnsOK && !httpOK {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusFailed,
			Message: "Both DNS and HTTP failed inside container",
			Details: details,
		}
	}

	if !dnsOK {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusWarning,
			Message: "DNS resolution failed inside container",
			Details: details,
		}
	}

	// DNS OK but HTTP failed - provide specific error message
	if httpErr != nil {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusWarning,
			Message: fmt.Sprintf("HTTP connectivity failed (DNS OK, curl error: %v)", httpErr),
			Details: details,
		}
	}
	return HealthCheck{
		Name:    "container_connectivity",
		Status:  StatusWarning,
		Message: fmt.Sprintf("HTTP connectivity failed (DNS OK, HTTP status: %s)", httpOutput),
		Details: details,
	}
}

// CheckNetworkRestriction tests that restricted network mode properly blocks private networks
func CheckNetworkRestriction(imageName string) HealthCheck {
	// Skip if firewall not available
	if !network.NftAvailable() {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusWarning,
			Message: "Skipped (nft not available)",
		}
	}

	// Skip if no image available
	if imageName == "" {
		imageName = "coi-default"
	}

	exists, err := container.ImageExists(imageName)
	if err != nil || !exists {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusWarning,
			Message: "Skipped (image not available)",
		}
	}

	// Create temporary container name
	containerName := fmt.Sprintf("coi-restriction-check-%d", time.Now().UnixNano())

	// Launch ephemeral container on the Incus default pool — this probe is
	// one-shot and pool routing isn't relevant to what we're checking.
	if err := container.LaunchContainer(imageName, containerName, ""); err != nil {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Failed to launch test container: %v", err),
		}
	}

	// Track if we applied firewall rules (for cleanup)
	var nftManager *network.NftManager

	// Ensure cleanup on any exit path
	defer func() {
		// Remove firewall rules first
		if nftManager != nil {
			_ = nftManager.RemoveRules()
		}
		// Then stop/delete container
		_ = container.StopContainer(containerName)
		_ = container.DeleteContainer(containerName)
	}()

	// Wait for container to be ready and have network (up to 30 seconds)
	if !waitProbeReady(containerName) {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: "Test container failed to start within timeout",
		}
	}

	// Wait for DHCP to assign an IP (up to 15 seconds)
	var containerIP string
	for i := 0; i < 15; i++ {
		ip, err := network.GetContainerIP(containerName)
		if err == nil && ip != "" {
			containerIP = ip
			break
		}
		time.Sleep(1 * time.Second)
	}

	if containerIP == "" {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: "Container failed to get IP address",
		}
	}

	// Get gateway IP for firewall rules from the host-side /proc/<pid>/net/route.
	// This avoids running ip route inside the container, where the output could
	// be spoofed by a compromised binary or bind-mount.
	gatewayIP, gwErr := getContainerGatewayIPFromProc(containerName)

	// Apply restricted mode firewall rules
	nftManager = network.NewNftManager(containerIP, gatewayIP)
	boolTrue := true
	restrictedConfig := &config.NetworkConfig{
		Mode:                  config.NetworkModeRestricted,
		BlockPrivateNetworks:  &boolTrue,
		BlockMetadataEndpoint: &boolTrue,
	}

	if err := nftManager.ApplyRestricted(restrictedConfig); err != nil {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Failed to apply firewall rules: %v", err),
		}
	}

	// Test 1: External internet should be accessible
	httpOutput, httpErr := container.IncusOutput("exec", containerName, "--", "curl", "-s", "--connect-timeout", "5", "-o", "/dev/null", "-w", "%{http_code}", "https://api.anthropic.com")
	externalOK := httpErr == nil && httpOutput != "" && httpOutput != "000"

	// Test 2: RFC1918 private networks should be blocked
	// Try to reach a private IP - we use the gateway but on a different port that won't respond
	// Actually, let's try to reach 10.0.0.1 which should be blocked
	// Using curl with connect-timeout to test if connection is rejected
	_, privateErr := container.IncusOutput("exec", containerName, "--", "curl", "-s", "--connect-timeout", "2", "-o", "/dev/null", "http://10.0.0.1:80")

	// If private network access is blocked, curl should fail with connection refused/rejected
	// Exit code 7 = connection refused, 28 = timeout (both indicate blocking works)
	privateBlocked := privateErr != nil

	// Also test 192.168.0.1
	_, private2Err := container.IncusOutput("exec", containerName, "--", "curl", "-s", "--connect-timeout", "2", "-o", "/dev/null", "http://192.168.0.1:80")
	private2Blocked := private2Err != nil

	// Test 3: the cloud metadata endpoint (169.254.169.254) is the marquee SSRF
	// target block_metadata_endpoint defends — verify it's actually unreachable.
	_, metadataErr := container.IncusOutput("exec", containerName, "--", "curl", "-s", "--connect-timeout", "2", "-o", "/dev/null", "http://169.254.169.254/")
	metadataBlocked := metadataErr != nil

	details := map[string]interface{}{
		"container_ip":        containerIP,
		"gateway_ip":          gatewayIP,
		"external_access":     externalOK,
		"private_blocked":     privateBlocked,
		"private_10_blocked":  privateBlocked,
		"private_192_blocked": private2Blocked,
		"metadata_blocked":    metadataBlocked,
	}
	if gwErr != nil {
		details["gateway_read_error"] = gwErr.Error()
	}

	if externalOK {
		details["external_status"] = httpOutput
	}

	// Evaluate results
	if externalOK && privateBlocked && private2Blocked && metadataBlocked {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusOK,
			Message: "Restricted mode working (external OK; private networks + metadata endpoint blocked)",
			Details: details,
		}
	}

	if !externalOK {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: "Restricted mode broken: external internet not accessible",
			Details: details,
		}
	}

	if !privateBlocked || !private2Blocked {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: "Restricted mode broken: private networks NOT blocked (firewall rules ineffective)",
			Details: details,
		}
	}

	if !metadataBlocked {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: "Restricted mode broken: cloud metadata endpoint (169.254.169.254) NOT blocked (SSRF exposure)",
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "network_restriction",
		Status:  StatusWarning,
		Message: "Restricted mode partially working",
		Details: details,
	}
}

// CheckSecretMasking proves, at runtime, that workspace secret masking
// (`[security] secret_paths`) actually hides a secret from inside the container.
// It plants a decoy secret plus a non-secret marker in a temp workspace, mounts
// it into an ephemeral container, applies the real masking code path
// (session.SetupSecretMasks), then reads both files from inside: the marker must
// be readable (proves the workspace is mounted/readable — the control), and the
// masked secret must read EMPTY (proves masking works). A leak is StatusFailed.
func CheckSecretMasking(imageName string) HealthCheck {
	const name = "secret_masking"
	const secretSentinel = "COI_SECRET_SENTINEL_DO_NOT_LEAK"
	const markerSentinel = "COI_MARKER_SENTINEL"

	if imageName == "" {
		imageName = "coi-default"
	}
	if exists, err := container.ImageExists(imageName); err != nil || !exists {
		return HealthCheck{Name: name, Status: StatusWarning, Message: "Skipped (image not available)"}
	}

	// Temp workspace with a decoy secret + a readable marker (control).
	workspace, err := os.MkdirTemp("", "coi-secret-check-")
	if err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (temp dir: %v)", err)}
	}
	defer os.RemoveAll(workspace)
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte(secretSentinel+"\n"), 0o600); err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (write secret: %v)", err)}
	}
	if err := os.WriteFile(filepath.Join(workspace, "marker.txt"), []byte(markerSentinel+"\n"), 0o644); err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (write marker: %v)", err)}
	}

	containerName := fmt.Sprintf("coi-secret-check-%d", time.Now().UnixNano())
	if err := container.LaunchContainer(imageName, containerName, ""); err != nil {
		return HealthCheck{Name: name, Status: StatusFailed, Message: fmt.Sprintf("Failed to launch test container: %v", err)}
	}
	defer func() {
		_ = container.StopContainer(containerName)
		_ = container.DeleteContainer(containerName)
	}()

	if !waitProbeReady(containerName) {
		return HealthCheck{Name: name, Status: StatusFailed, Message: "Test container failed to start within timeout"}
	}

	mgr := container.NewManager(containerName)
	const containerWorkspace = "/workspace"
	const useShift = true // matches COI's default workspace mount
	if err := mgr.MountDisk("workspace", workspace, containerWorkspace, useShift, false); err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (mount workspace: %v)", err)}
	}

	// Apply the REAL masking code path.
	masked, _, maskErr := session.SetupSecretMasks(mgr, workspace, containerWorkspace, []string{".env"}, useShift)
	if maskErr != nil || len(masked) == 0 {
		return HealthCheck{Name: name, Status: StatusFailed, Message: fmt.Sprintf("Failed to apply secret masks: %v", maskErr)}
	}

	markerOut, _ := container.IncusOutput("exec", containerName, "--", "cat", containerWorkspace+"/marker.txt")
	secretOut, _ := container.IncusOutput("exec", containerName, "--", "cat", containerWorkspace+"/.env")

	controlReadable := strings.Contains(markerOut, markerSentinel)
	secretLeaked := strings.Contains(secretOut, secretSentinel)

	details := map[string]interface{}{
		"masked_paths":     masked,
		"control_readable": controlReadable,
		"secret_leaked":    secretLeaked,
	}

	if secretLeaked {
		return HealthCheck{
			Name:    name,
			Status:  StatusFailed,
			Message: "Secret masking INEFFECTIVE: a masked secret_paths file is readable inside the container",
			Details: details,
		}
	}
	if !controlReadable {
		// The masked file read empty, but so did the control marker — can't
		// distinguish working masking from a broken/unreadable mount. Don't claim
		// success on an inconclusive probe.
		return HealthCheck{
			Name:    name,
			Status:  StatusWarning,
			Message: "Could not verify secret masking (probe workspace not readable inside container)",
			Details: details,
		}
	}
	return HealthCheck{
		Name:    name,
		Status:  StatusOK,
		Message: "Secret masking working (masked secret_paths read empty inside the container)",
		Details: details,
	}
}

// CheckHostCredentialIsolation proves COI's headline guarantee — host
// credentials are not exposed to the container unless explicitly mounted — holds
// at runtime. It plants a decoy in the host home directory, launches a probe
// container with its own (separate) temp workspace, and confirms the decoy is
// NOT readable inside by its host path. If it leaks, the host home has been
// mounted into the container (a containment regression) → StatusFailed.
func CheckHostCredentialIsolation(imageName string) HealthCheck {
	const name = "host_credential_isolation"
	const sentinel = "COI_HOST_CRED_DECOY_DO_NOT_LEAK"

	if imageName == "" {
		imageName = "coi-default"
	}
	if exists, err := container.ImageExists(imageName); err != nil || !exists {
		return HealthCheck{Name: name, Status: StatusWarning, Message: "Skipped (image not available)"}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (home dir: %v)", err)}
	}
	// Decoy in the host home ROOT (not inside .ssh/.aws — non-invasive); if COI
	// ever mounted the host home into a container it would surface here.
	decoyPath := filepath.Join(homeDir, fmt.Sprintf(".coi-hostcred-decoy-%d", time.Now().UnixNano()))
	if err := os.WriteFile(decoyPath, []byte(sentinel+"\n"), 0o600); err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (write decoy: %v)", err)}
	}
	defer os.Remove(decoyPath)

	// Separate temp workspace so the probe doesn't mount the host home.
	workspace, err := os.MkdirTemp("", "coi-hostcred-check-")
	if err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (temp dir: %v)", err)}
	}
	defer os.RemoveAll(workspace)

	containerName := fmt.Sprintf("coi-hostcred-check-%d", time.Now().UnixNano())
	if err := container.LaunchContainer(imageName, containerName, ""); err != nil {
		return HealthCheck{Name: name, Status: StatusFailed, Message: fmt.Sprintf("Failed to launch test container: %v", err)}
	}
	defer func() {
		_ = container.StopContainer(containerName)
		_ = container.DeleteContainer(containerName)
	}()

	if !waitProbeReady(containerName) {
		return HealthCheck{Name: name, Status: StatusFailed, Message: "Test container failed to start within timeout"}
	}

	mgr := container.NewManager(containerName)
	if err := mgr.MountDisk("workspace", workspace, "/workspace", true, false); err != nil {
		return HealthCheck{Name: name, Status: StatusWarning, Message: fmt.Sprintf("Skipped (mount workspace: %v)", err)}
	}

	// Try to read the host decoy by its absolute host path from inside.
	out, _ := container.IncusOutput("exec", containerName, "--", "cat", decoyPath)
	leaked := strings.Contains(out, sentinel)

	details := map[string]interface{}{
		"host_home":    homeDir,
		"decoy_path":   decoyPath,
		"decoy_leaked": leaked,
	}
	if leaked {
		return HealthCheck{
			Name:    name,
			Status:  StatusFailed,
			Message: "Host home is reachable inside the container — host credentials are NOT isolated",
			Details: details,
		}
	}
	return HealthCheck{
		Name:    name,
		Status:  StatusOK,
		Message: "Host credentials isolated (host home not reachable inside the container)",
		Details: details,
	}
}

// CheckDiskSpace checks available disk space in ~/.coi directory
func CheckDiskSpace() HealthCheck {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return HealthCheck{
			Name:    "disk_space",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not determine home directory: %v", err),
		}
	}

	coiDir := filepath.Join(homeDir, ".coi")

	// Use the parent directory if .coi doesn't exist yet
	checkDir := coiDir
	if _, err := os.Stat(coiDir); os.IsNotExist(err) {
		checkDir = homeDir
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(checkDir, &stat); err != nil {
		return HealthCheck{
			Name:    "disk_space",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not check disk space: %v", err),
		}
	}

	// Calculate available space in bytes
	// #nosec G115 - Bsize is always positive on real filesystems
	availableBytes := stat.Bavail * uint64(stat.Bsize)
	availableGB := float64(availableBytes) / (1024 * 1024 * 1024)

	// Warn if less than 5GB available
	if availableGB < 5 {
		return HealthCheck{
			Name:    "disk_space",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Low disk space: %.1f GB available", availableGB),
			Details: map[string]interface{}{
				"available_gb": availableGB,
				"path":         checkDir,
			},
		}
	}

	return HealthCheck{
		Name:    "disk_space",
		Status:  StatusOK,
		Message: fmt.Sprintf("%.1f GB available", availableGB),
		Details: map[string]interface{}{
			"available_gb": availableGB,
			"path":         checkDir,
		},
	}
}

// defaultIncusPool returns the storage pool name used by Incus's "default"
// profile. Used as a fallback when no profile/global config asks for a
// specific pool, so we still check something useful. Goes through
// container.IncusOutput so the configured Incus project is respected.
func defaultIncusPool() string {
	profileOut, err := container.IncusOutput("profile", "show", "default")
	if err != nil {
		return "default"
	}
	for _, line := range strings.Split(profileOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pool:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "pool:"))
		}
	}
	return "default"
}

// poolUsage holds parsed `incus storage info` numbers for a single pool plus
// any error encountered while gathering them.
type poolUsage struct {
	driver   string
	usedGiB  float64
	totalGiB float64
	err      error
}

// gatherPoolUsage queries `incus storage info <pool>` and parses out the
// driver and space-used / total-space lines. Returns an error if the pool is
// missing or the output cannot be parsed. Goes through container.IncusOutput
// so the configured Incus project is respected. Swappable so unit tests can
// exercise CheckIncusStoragePools without an Incus daemon.
var gatherPoolUsage = func(pool string) poolUsage {
	out, err := container.IncusOutput("storage", "info", pool)
	if err != nil {
		return poolUsage{err: err}
	}
	return parsePoolInfo(out)
}

// parsePoolInfo extracts the driver and usage numbers from `incus storage
// info` output.
func parsePoolInfo(out string) poolUsage {
	var u poolUsage
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "driver:"):
			u.driver = strings.TrimSpace(strings.TrimPrefix(line, "driver:"))
		case strings.HasPrefix(line, "space used:"):
			u.usedGiB = parseStorageValueGiB(strings.TrimPrefix(line, "space used:"))
		case strings.HasPrefix(line, "total space:"):
			u.totalGiB = parseStorageValueGiB(strings.TrimPrefix(line, "total space:"))
		}
	}

	if u.totalGiB == 0 {
		u.err = fmt.Errorf("could not parse usage")
	}
	return u
}

// listPoolDrivers returns a pool-name → driver map from `incus storage list
// --format=json` — the structured source of truth for the driver. The text
// scrape in parsePoolInfo stays only as a fallback, so a future Incus
// reshaping either output cannot silently blank the driver. Returns nil when
// the list call fails; swappable so unit tests can run without a daemon.
var listPoolDrivers = func() map[string]string {
	pools, err := container.ListStoragePools()
	if err != nil {
		return nil
	}
	drivers := make(map[string]string, len(pools))
	for _, p := range pools {
		drivers[p.Name] = p.Driver
	}
	return drivers
}

// CheckIncusStoragePools checks Incus storage pool usage for all pools the
// loaded config references (global + every profile), de-duplicated. An empty
// pool name in config falls back to the Incus default profile's pool so we
// always check at least one real pool.
//
// One HealthCheck is returned with per-pool entries in Details. The overall
// status is the worst status across the inspected pools (failed > warning > ok).
// Besides usage thresholds, a pool on the `dir` driver is flagged as a warning:
// it forces `incus init` to re-unpack the whole image on every launch, which
// dominates session startup time (#659).
func CheckIncusStoragePools(pools []string) HealthCheck {
	// De-dupe + replace empty entries with the actual default pool name.
	seen := map[string]bool{}
	var unique []string
	for _, p := range pools {
		if p == "" {
			p = defaultIncusPool()
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		unique = append(unique, p)
	}
	if len(unique) == 0 {
		unique = []string{defaultIncusPool()}
	}

	details := map[string]interface{}{}
	overall := StatusOK
	var messages []string
	worsen := func(s CheckStatus) {
		if s == StatusFailed {
			overall = StatusFailed
		} else if s == StatusWarning && overall == StatusOK {
			overall = StatusWarning
		}
	}

	drivers := listPoolDrivers()

	for _, pool := range unique {
		u := gatherPoolUsage(pool)

		driver := drivers[pool]
		if driver == "" {
			driver = u.driver
		}
		label := pool
		if driver != "" {
			label = fmt.Sprintf("%s (%s)", pool, driver)
		}
		// A `dir` pool has no unpacked image volume to clone from, so every
		// launch re-unpacks the full image tarball (~5-6s per unpacked GB,
		// #659). Copy-on-write drivers make instance creation near-free.
		dirWarning := fmt.Sprintf("%s: 'dir' storage driver re-unpacks the image on every launch — recreate the pool with a copy-on-write driver (zfs/btrfs), e.g. by re-running install.sh", label)

		if u.err != nil {
			details[pool] = map[string]interface{}{
				"driver": driver,
				"status": string(StatusFailed),
				"error":  u.err.Error(),
			}
			messages = append(messages, fmt.Sprintf("%s: missing", pool))
			if driver == "dir" {
				messages = append(messages, dirWarning)
			}
			worsen(StatusFailed)
			continue
		}

		freeGiB := u.totalGiB - u.usedGiB
		usedPct := (u.usedGiB / u.totalGiB) * 100

		var poolStatus CheckStatus
		switch {
		case freeGiB < 2 || usedPct > 90:
			poolStatus = StatusFailed
		case freeGiB < 5 || usedPct > 80:
			poolStatus = StatusWarning
		default:
			poolStatus = StatusOK
		}
		if driver == "dir" && poolStatus == StatusOK {
			poolStatus = StatusWarning
		}
		worsen(poolStatus)

		details[pool] = map[string]interface{}{
			"driver":    driver,
			"used_gib":  u.usedGiB,
			"total_gib": u.totalGiB,
			"free_gib":  freeGiB,
			"used_pct":  usedPct,
			"status":    string(poolStatus),
		}
		messages = append(messages, fmt.Sprintf("%s: %.1f GiB free of %.1f GiB (%.0f%% used)", label, freeGiB, u.totalGiB, usedPct))
		if driver == "dir" {
			messages = append(messages, dirWarning)
		}
	}

	return HealthCheck{
		Name:    "incus_storage_pools",
		Status:  overall,
		Message: strings.Join(messages, "; "),
		Details: details,
	}
}

// parseStorageValueGiB parses a value like "277.69MiB" or "28.57GiB" and
// returns the equivalent in GiB. Returns 0 on parse failure.
func parseStorageValueGiB(s string) float64 {
	s = strings.TrimSpace(s)

	// Find where the numeric part ends and the unit suffix begins.
	// We cannot use fmt.Sscanf("%f%s") because it interprets "1.00EiB"
	// as scientific notation (the 'E') and fails.
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == '-' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0
	}

	var val float64
	if _, err := fmt.Sscanf(s[:i], "%f", &val); err != nil {
		return 0
	}

	unit := strings.TrimSpace(s[i:])
	switch strings.ToLower(unit) {
	case "eib":
		return val * 1024 * 1024 * 1024
	case "tib":
		return val * 1024
	case "gib", "":
		return val
	case "mib":
		return val / 1024
	case "kib":
		return val / (1024 * 1024)
	case "bytes", "b":
		return val / (1024 * 1024 * 1024)
	default:
		return val // unknown unit, assume GiB
	}
}

// CheckActiveContainers counts running COI containers
func CheckActiveContainers() HealthCheck {
	prefix := session.GetContainerPrefix()
	pattern := fmt.Sprintf("^%s", prefix)

	output, err := container.IncusOutput("list", pattern, "--format=json")
	if err != nil {
		return HealthCheck{
			Name:    "active_containers",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not list containers: %v", err),
		}
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return HealthCheck{
			Name:    "active_containers",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not parse container list: %v", err),
		}
	}

	// Count running containers
	running := 0
	for _, c := range containers {
		if status, ok := c["status"].(string); ok && status == "Running" {
			running++
		}
	}

	total := len(containers)
	message := fmt.Sprintf("%d running", running)
	if total > running {
		message = fmt.Sprintf("%d running, %d stopped", running, total-running)
	}
	if total == 0 {
		message = "None"
	}

	return HealthCheck{
		Name:    "active_containers",
		Status:  StatusOK,
		Message: message,
		Details: map[string]interface{}{
			"running": running,
			"total":   total,
		},
	}
}

// CheckSavedSessions counts saved sessions
func CheckSavedSessions(cfg *config.Config) HealthCheck {
	// Get configured tool
	toolName := cfg.Tool.Name
	if toolName == "" {
		toolName = "claude"
	}
	toolInstance, err := tool.Get(toolName)
	if err != nil {
		toolInstance = tool.GetDefault()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return HealthCheck{
			Name:    "saved_sessions",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not determine home directory: %v", err),
		}
	}

	baseDir := filepath.Join(homeDir, ".coi")
	sessionsDir := session.GetSessionsDir(baseDir, toolInstance)

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return HealthCheck{
				Name:    "saved_sessions",
				Status:  StatusOK,
				Message: "None",
				Details: map[string]interface{}{
					"count": 0,
				},
			}
		}
		return HealthCheck{
			Name:    "saved_sessions",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not read sessions directory: %v", err),
		}
	}

	// Count directories (sessions)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}

	message := fmt.Sprintf("%d session(s)", count)
	if count == 0 {
		message = "None"
	}

	return HealthCheck{
		Name:    "saved_sessions",
		Status:  StatusOK,
		Message: message,
		Details: map[string]interface{}{
			"count": count,
			"path":  sessionsDir,
		},
	}
}

// CheckImageAge checks if the COI image is outdated
func CheckImageAge(imageName string) HealthCheck {
	if imageName == "" {
		imageName = "coi-default"
	}

	// Get image info
	output, err := container.IncusOutput("image", "list", imageName, "--format=json")
	if err != nil {
		return HealthCheck{
			Name:    "image_age",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not get image info: %v", err),
		}
	}

	var images []struct {
		CreatedAt time.Time `json:"created_at"`
		Aliases   []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal([]byte(output), &images); err != nil {
		return HealthCheck{
			Name:    "image_age",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not parse image info: %v", err),
		}
	}

	// Find the image
	for _, img := range images {
		for _, alias := range img.Aliases {
			if alias.Name == imageName {
				age := time.Since(img.CreatedAt)
				days := int(age.Hours() / 24)

				// Warn if older than 30 days
				if days > 30 {
					return HealthCheck{
						Name:    "image_age",
						Status:  StatusWarning,
						Message: fmt.Sprintf("%d days old (consider rebuilding with 'coi build --force')", days),
						Details: map[string]interface{}{
							"created_at": img.CreatedAt.Format("2006-01-02"),
							"age_days":   days,
						},
					}
				}

				return HealthCheck{
					Name:    "image_age",
					Status:  StatusOK,
					Message: fmt.Sprintf("%d days old", days),
					Details: map[string]interface{}{
						"created_at": img.CreatedAt.Format("2006-01-02"),
						"age_days":   days,
					},
				}
			}
		}
	}

	return HealthCheck{
		Name:    "image_age",
		Status:  StatusWarning,
		Message: fmt.Sprintf("Image '%s' not found", imageName),
	}
}

// CheckOrphanedResources checks for orphaned system resources
func CheckOrphanedResources() HealthCheck {
	// Check for orphaned veths
	orphanedVeths := 0
	entries, err := os.ReadDir("/sys/class/net")
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, "veth") {
				continue
			}
			masterPath := fmt.Sprintf("/sys/class/net/%s/master", name)
			if _, err := os.Stat(masterPath); os.IsNotExist(err) {
				orphanedVeths++
			}
		}
	}

	// Check for orphaned firewall rules
	orphanedRules := 0
	if network.NftAvailable() {
		// Get running container IPs and names
		containerIPs := make(map[string]bool)
		containerNames := make(map[string]bool)
		output, err := container.IncusOutput("list", "--format=json")
		if err == nil {
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
			if json.Unmarshal([]byte(output), &containers) == nil {
				for _, c := range containers {
					if c.State.Status == "Running" {
						containerNames[c.Name] = true
						if eth0, ok := c.State.Network["eth0"]; ok {
							for _, addr := range eth0.Addresses {
								if addr.Family == "inet" {
									containerIPs[addr.Address] = true
								}
							}
						}
					}
				}
			}
		}

		// Check nft coi chain for orphaned IPv4 rules
		if ipHandles, err := network.ListCOIFilterRuleIPs(); err == nil {
			for ip := range ipHandles {
				if !containerIPs[ip] {
					orphanedRules++
				}
			}
		}

		// Check ip6 coi chain for orphaned IPv6 drop rules
		if names, err := network.ListCOIIP6RuleContainers(); err == nil {
			for _, name := range names {
				if !containerNames[name] {
					orphanedRules++
				}
			}
		}
	}

	totalOrphans := orphanedVeths + orphanedRules

	if totalOrphans == 0 {
		return HealthCheck{
			Name:    "orphaned_resources",
			Status:  StatusOK,
			Message: "No orphaned resources",
		}
	}

	message := fmt.Sprintf("%d orphaned resource(s) found", totalOrphans)
	if orphanedVeths > 0 {
		message += fmt.Sprintf(" (%d veths", orphanedVeths)
		if orphanedRules > 0 {
			message += fmt.Sprintf(", %d firewall rules)", orphanedRules)
		} else {
			message += ")"
		}
	} else if orphanedRules > 0 {
		message += fmt.Sprintf(" (%d firewall rules)", orphanedRules)
	}
	message += " - run 'coi clean' to remove"

	return HealthCheck{
		Name:    "orphaned_resources",
		Status:  StatusWarning,
		Message: message,
		Details: map[string]interface{}{
			"orphaned_veths":     orphanedVeths,
			"orphaned_nft_rules": orphanedRules,
		},
	}
}

// CheckNFTables checks if nftables is available and properly configured
func CheckNFTables() HealthCheck {
	// Check if nftables binary exists
	nftPath, err := exec.LookPath("nft")
	if err != nil {
		return HealthCheck{
			Name:    "nftables",
			Status:  StatusWarning,
			Message: "nftables not found (required for NFT monitoring)",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	// Check nft version
	versionCmd := exec.Command("nft", "--version")
	versionOutput, vErr := versionCmd.CombinedOutput()

	var nftVersion string
	if vErr == nil {
		if result := evaluateNFTVersion(nftPath, string(versionOutput)); result != nil {
			return *result
		}
		// Version OK — extract for display
		if vs, err := nftmonitor.ExtractNFTVersion(string(versionOutput)); err == nil {
			nftVersion = vs
		}
	}

	// Check if we can run nft commands with sudo (NOPASSWD)
	cmd := exec.Command("sudo", "-n", "nft", "list", "ruleset")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return HealthCheck{
			Name:    "nftables",
			Status:  StatusWarning,
			Message: "nftables installed but sudo access not configured",
			Details: map[string]interface{}{
				"nft_path": nftPath,
				"version":  nftVersion,
				"error":    string(output),
				"hint":     "Run scripts/install-nft-deps.sh to configure passwordless sudo",
			},
		}
	}

	message := "nftables available with sudo access"
	if nftVersion != "" {
		message = fmt.Sprintf("nftables %s available with sudo access", nftVersion)
	}

	return HealthCheck{
		Name:    "nftables",
		Status:  StatusOK,
		Message: message,
		Details: map[string]interface{}{
			"nft_path": nftPath,
			"version":  nftVersion,
		},
	}
}

// evaluateNFTVersion evaluates the raw `nft --version` output and returns
// a failed health check if the version is below minimum. Returns nil if OK.
func evaluateNFTVersion(nftPath, versionOutput string) *HealthCheck {
	vs, err := nftmonitor.ExtractNFTVersion(versionOutput)
	if err != nil {
		return nil // Can't parse, skip version check
	}

	v, err := nftmonitor.ParseNFTVersion(vs)
	if err != nil {
		return nil // Can't parse, skip version check
	}

	if !nftmonitor.MeetsMinimumNFTVersion(v) {
		return &HealthCheck{
			Name:    "nftables",
			Status:  StatusWarning,
			Message: nftmonitor.FormatMinNFTVersionError(v),
			Details: map[string]interface{}{
				"nft_path": nftPath,
				"version":  vs,
			},
		}
	}

	return nil
}

// CheckSystemdJournal checks if systemd-journal access is available
func CheckSystemdJournal() HealthCheck {
	// Check if journalctl exists
	journalPath, err := exec.LookPath("journalctl")
	if err != nil {
		return HealthCheck{
			Name:    "systemd_journal",
			Status:  StatusWarning,
			Message: "journalctl not found (required for NFT monitoring)",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	// Check if user is in systemd-journal group
	currentUser, err := user.Current()
	if err != nil {
		return HealthCheck{
			Name:    "systemd_journal",
			Status:  StatusWarning,
			Message: "Failed to get current user",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	// Try to read kernel logs
	cmd := exec.Command("journalctl", "-k", "-n", "1")
	if err := cmd.Run(); err != nil {
		return HealthCheck{
			Name:    "systemd_journal",
			Status:  StatusWarning,
			Message: "No access to kernel logs (add user to systemd-journal group)",
			Details: map[string]interface{}{
				"journal_path": journalPath,
				"user":         currentUser.Username,
				"hint":         "Run scripts/install-nft-deps.sh to configure access",
			},
		}
	}

	return HealthCheck{
		Name:    "systemd_journal",
		Status:  StatusOK,
		Message: "systemd journal access available",
		Details: map[string]interface{}{
			"journal_path": journalPath,
			"user":         currentUser.Username,
		},
	}
}

// CheckLibsystemd checks if libsystemd development headers are installed
func CheckLibsystemd() HealthCheck {
	// Check if the header file exists
	headerPaths := []string{
		"/usr/include/systemd/sd-journal.h",
		"/usr/include/x86_64-linux-gnu/systemd/sd-journal.h",
		"/usr/include/aarch64-linux-gnu/systemd/sd-journal.h",
	}

	var foundPath string
	for _, path := range headerPaths {
		if _, err := os.Stat(path); err == nil {
			foundPath = path
			break
		}
	}

	if foundPath == "" {
		return HealthCheck{
			Name:    "libsystemd",
			Status:  StatusWarning,
			Message: "libsystemd-dev not installed (required to build NFT monitoring)",
			Details: map[string]interface{}{
				"hint": "Run scripts/install-nft-deps.sh to install dependencies",
			},
		}
	}

	return HealthCheck{
		Name:    "libsystemd",
		Status:  StatusOK,
		Message: "libsystemd-dev installed",
		Details: map[string]interface{}{
			"header_path": foundPath,
		},
	}
}

// CheckAuditLogDirectory checks if the audit log directory exists and is writable
func CheckAuditLogDirectory() HealthCheck {
	auditDir := filepath.Join(os.Getenv("HOME"), ".coi", "audit")

	// Check if directory exists
	info, err := os.Stat(auditDir) //nolint:gosec // G703: path is derived from HOME env var + fixed ".coi/audit" suffix, not user-supplied
	if err != nil {
		if os.IsNotExist(err) {
			// Try to create it
			if err := os.MkdirAll(auditDir, 0o755); err != nil { //nolint:gosec // G703: same path as above
				return HealthCheck{
					Name:    "audit_log_directory",
					Status:  StatusFailed,
					Message: "Failed to create audit log directory",
					Details: map[string]interface{}{
						"path":  auditDir,
						"error": err.Error(),
					},
				}
			}
			return HealthCheck{
				Name:    "audit_log_directory",
				Status:  StatusOK,
				Message: "Audit log directory created",
				Details: map[string]interface{}{
					"path": auditDir,
				},
			}
		}
		return HealthCheck{
			Name:    "audit_log_directory",
			Status:  StatusFailed,
			Message: "Failed to access audit log directory",
			Details: map[string]interface{}{
				"path":  auditDir,
				"error": err.Error(),
			},
		}
	}

	// Verify it's a directory
	if !info.IsDir() {
		return HealthCheck{
			Name:    "audit_log_directory",
			Status:  StatusFailed,
			Message: "Audit log path exists but is not a directory",
			Details: map[string]interface{}{
				"path": auditDir,
			},
		}
	}

	// Check if writable by creating a test file
	testFile := filepath.Join(auditDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil { //nolint:gosec // G703: testFile is derived from HOME env var + fixed path suffix, not user-supplied
		return HealthCheck{
			Name:    "audit_log_directory",
			Status:  StatusFailed,
			Message: "Audit log directory is not writable",
			Details: map[string]interface{}{
				"path":  auditDir,
				"error": err.Error(),
			},
		}
	}
	os.Remove(testFile) //nolint:gosec // G703: testFile is derived from HOME env var + fixed path suffix, not user-supplied

	return HealthCheck{
		Name:    "audit_log_directory",
		Status:  StatusOK,
		Message: "Audit log directory is ready",
		Details: map[string]interface{}{
			"path": auditDir,
		},
	}
}

// CheckProcessMonitoringCapability checks that host-side process collection works.
// Process monitoring reads /proc on the host filtered by container cgroup membership,
// so this check verifies the host walk succeeds — not that ps runs inside the container.
func CheckProcessMonitoringCapability(imageName string) HealthCheck {
	output, err := container.IncusOutput("list", "--format=json")
	if err != nil {
		return HealthCheck{
			Name:    "process_monitoring",
			Status:  StatusWarning,
			Message: "Unable to list containers",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return HealthCheck{
			Name:    "process_monitoring",
			Status:  StatusWarning,
			Message: "Unable to parse container list",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	var testContainer string
	for _, c := range containers {
		if status, ok := c["status"].(string); ok && status == "Running" {
			if name, ok := c["name"].(string); ok {
				testContainer = name
				break
			}
		}
	}

	if testContainer == "" {
		return HealthCheck{
			Name:    "process_monitoring",
			Status:  StatusWarning,
			Message: "No running containers to verify process monitoring",
			Details: map[string]interface{}{
				"hint": "Start a container with 'coi shell' to enable this check",
			},
		}
	}

	stats, err := monitor.CollectProcessStats(context.Background(), testContainer)
	if err != nil {
		return HealthCheck{
			Name:    "process_monitoring",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Host-side process collection failed: %v", err),
			Details: map[string]interface{}{
				"container": testContainer,
				"hint":      "Ensure cgroup v2 is available and the monitor has read access to /proc",
			},
		}
	}

	return HealthCheck{
		Name:    "process_monitoring",
		Status:  StatusOK,
		Message: "Process monitoring is functional (host-side /proc walk)",
		Details: map[string]interface{}{
			"container":     testContainer,
			"process_count": stats.TotalCount,
		},
	}
}

// getContainerGatewayIPFromProc reads the container's default gateway from
// /proc/<init-pid>/net/route on the host. This avoids running ip route inside
// the container, where the output could be influenced by a compromised binary.
func getContainerGatewayIPFromProc(containerName string) (string, error) {
	initPID, err := monitor.GetContainerInitPID(context.Background(), containerName)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/route", initPID))
	if err != nil {
		return "", err
	}
	return parseDefaultGatewayFromProcRoute(string(data))
}

// parseDefaultGatewayFromProcRoute extracts the default gateway IP from the
// content of /proc/<pid>/net/route. Gateway addresses are stored as
// little-endian hex 32-bit integers, e.g. "010080AC" → 172.128.0.1.
// When multiple default routes exist the one with the lowest metric is used.
// Routes with a zero gateway (direct/connected routes) are skipped.
// Kept separate so it can be unit-tested without a live container.
func parseDefaultGatewayFromProcRoute(content string) (string, error) {
	// fields indices: 0=Iface 1=Destination 2=Gateway 3=Flags 4=RefCnt
	//                 5=Use 6=Metric 7=Mask ...
	const (
		colDest    = 1
		colGateway = 2
		colMetric  = 6
		minFields  = 7
	)

	bestMetric := ^uint64(0) // max uint64
	bestGateway := ""

	for i, line := range strings.Split(content, "\n") {
		if i == 0 {
			continue // skip header
		}
		fields := strings.Fields(line)
		if len(fields) < minFields {
			continue
		}
		if fields[colDest] != "00000000" {
			continue // not a default route
		}
		if fields[colGateway] == "00000000" {
			continue // direct-link default, no usable gateway
		}
		n, err := strconv.ParseUint(fields[colGateway], 16, 32)
		if err != nil {
			continue
		}
		metric, _ := strconv.ParseUint(fields[colMetric], 16, 32)
		if bestGateway == "" || metric < bestMetric {
			bestMetric = metric
			ip := net.IP{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)} //nolint:gosec // G115: n fits in uint32 (ParseUint bitSize=32)
			bestGateway = ip.String()
		}
	}

	if bestGateway == "" {
		return "", fmt.Errorf("no usable default gateway found in /proc/net/route")
	}
	return bestGateway, nil
}

// CheckCgroupAvailability checks if cgroup v2 is available for resource monitoring
func CheckCgroupAvailability() HealthCheck {
	cgroupPath := "/sys/fs/cgroup"

	// Check if cgroup filesystem exists
	info, err := os.Stat(cgroupPath)
	if err != nil {
		return HealthCheck{
			Name:    "cgroup_availability",
			Status:  StatusFailed,
			Message: "Cgroup filesystem not found",
			Details: map[string]interface{}{
				"path":  cgroupPath,
				"error": err.Error(),
				"hint":  "Resource monitoring requires cgroup v2",
			},
		}
	}

	if !info.IsDir() {
		return HealthCheck{
			Name:    "cgroup_availability",
			Status:  StatusFailed,
			Message: "Cgroup path is not a directory",
			Details: map[string]interface{}{
				"path": cgroupPath,
			},
		}
	}

	// Check if it's cgroup v2 by looking for cgroup.controllers
	controllersPath := filepath.Join(cgroupPath, "cgroup.controllers")
	if _, err := os.Stat(controllersPath); err != nil {
		return HealthCheck{
			Name:    "cgroup_availability",
			Status:  StatusWarning,
			Message: "Cgroup v1 detected (v2 recommended for monitoring)",
			Details: map[string]interface{}{
				"path": cgroupPath,
				"hint": "Resource monitoring works best with cgroup v2",
			},
		}
	}

	// Read available controllers
	controllers, err := os.ReadFile(controllersPath)
	if err != nil {
		return HealthCheck{
			Name:    "cgroup_availability",
			Status:  StatusOK,
			Message: "Cgroup v2 is available",
			Details: map[string]interface{}{
				"path": cgroupPath,
			},
		}
	}

	return HealthCheck{
		Name:    "cgroup_availability",
		Status:  StatusOK,
		Message: "Cgroup v2 is available with controllers",
		Details: map[string]interface{}{
			"path":        cgroupPath,
			"controllers": strings.TrimSpace(string(controllers)),
		},
	}
}

// CheckMonitoringConfiguration checks if monitoring is properly configured
func CheckMonitoringConfiguration(cfg *config.Config) HealthCheck {
	details := map[string]interface{}{
		"enabled":                config.BoolVal(cfg.Monitoring.Enabled),
		"auto_pause_on_high":     config.BoolVal(cfg.Monitoring.AutoPauseOnHigh),
		"auto_kill_on_critical":  config.BoolVal(cfg.Monitoring.AutoKillOnCritical),
		"poll_interval_sec":      cfg.Monitoring.PollIntervalSec,
		"file_read_threshold_mb": cfg.Monitoring.FileReadThresholdMB,
	}

	if !config.BoolVal(cfg.Monitoring.Enabled) {
		return HealthCheck{
			Name:    "monitoring_configuration",
			Status:  StatusWarning,
			Message: "Security monitoring is disabled (set monitoring.enabled=true in [monitoring] section of config.toml)",
			Details: details,
		}
	}

	// Check for unreasonable values
	warnings := []string{}

	if cfg.Monitoring.PollIntervalSec < 1 {
		warnings = append(warnings, "poll_interval_sec too low (<1s)")
	}
	if cfg.Monitoring.PollIntervalSec > 60 {
		warnings = append(warnings, "poll_interval_sec very high (>60s)")
	}
	if cfg.Monitoring.FileReadThresholdMB < 1 {
		warnings = append(warnings, "file_read_threshold_mb too low (<1MB)")
	}
	if cfg.Monitoring.FileReadThresholdMB > 10000 {
		warnings = append(warnings, "file_read_threshold_mb very high (>10GB)")
	}

	if len(warnings) > 0 {
		details["warnings"] = warnings
		return HealthCheck{
			Name:    "monitoring_configuration",
			Status:  StatusWarning,
			Message: "Monitoring configuration has unusual values",
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "monitoring_configuration",
		Status:  StatusOK,
		Message: "Monitoring is properly configured",
		Details: details,
	}
}

// CheckDockerForwardPolicy checks whether Docker has set the iptables FORWARD
// chain policy to DROP and reports whether coi can handle it
func CheckDockerForwardPolicy() HealthCheck {
	dockerRunning := network.IsDockerRunning()
	forwardDrop := network.ForwardPolicyIsDrop()
	nftAvailable := network.NftAvailable()
	iptablesAvailable := network.IptablesAvailable()

	details := map[string]interface{}{
		"docker_running":      dockerRunning,
		"forward_policy_drop": forwardDrop,
		"nft_available":       nftAvailable,
		"iptables_available":  iptablesAvailable,
	}

	if !dockerRunning {
		return HealthCheck{
			Name:    "docker_forward_policy",
			Status:  StatusOK,
			Message: "Docker not detected",
			Details: details,
		}
	}

	if !forwardDrop {
		return HealthCheck{
			Name:    "docker_forward_policy",
			Status:  StatusOK,
			Message: "Docker running, FORWARD policy is not DROP",
			Details: details,
		}
	}

	// FORWARD is DROP
	if nftAvailable {
		return HealthCheck{
			Name:    "docker_forward_policy",
			Status:  StatusOK,
			Message: "Docker FORWARD DROP detected — nft bridge rules will handle it",
			Details: details,
		}
	}

	if iptablesAvailable {
		return HealthCheck{
			Name:    "docker_forward_policy",
			Status:  StatusWarning,
			Message: "Docker FORWARD DROP detected, nft not available — coi will use iptables fallback automatically",
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "docker_forward_policy",
		Status:  StatusFailed,
		Message: "Docker FORWARD DROP detected, no nft or iptables — containers cannot reach internet",
		Details: details,
	}
}

// CheckKernelVersionHealth checks the running kernel version and warns if too old
func CheckKernelVersionHealth() HealthCheck {
	if runtime.GOOS != "linux" {
		return HealthCheck{
			Name:    "kernel_version",
			Status:  StatusOK,
			Message: fmt.Sprintf("Not applicable (%s)", runtime.GOOS),
		}
	}

	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return HealthCheck{
			Name:    "kernel_version",
			Status:  StatusOK,
			Message: "Could not determine kernel version",
		}
	}

	v, err := container.ParseKernelVersion(string(out))
	if err != nil {
		return HealthCheck{
			Name:    "kernel_version",
			Status:  StatusOK,
			Message: fmt.Sprintf("Could not parse kernel version: %s", strings.TrimSpace(string(out))),
		}
	}

	if !container.MeetsMinimumKernelVersion(v) {
		return HealthCheck{
			Name:   "kernel_version",
			Status: StatusWarning,
			Message: fmt.Sprintf("Kernel %s is below recommended minimum %d.%d — older kernels may lack security features for safe container isolation",
				v.Raw, container.MinKernelVersionMajor, container.MinKernelVersionMinor),
			Details: map[string]interface{}{
				"kernel":        v.Raw,
				"minimum":       fmt.Sprintf("%d.%d", container.MinKernelVersionMajor, container.MinKernelVersionMinor),
				"major":         v.Major,
				"minor":         v.Minor,
				"patch":         v.Patch,
				"meets_minimum": false,
			},
		}
	}

	return HealthCheck{
		Name:    "kernel_version",
		Status:  StatusOK,
		Message: fmt.Sprintf("Kernel %s (>= %d.%d)", v.Raw, container.MinKernelVersionMajor, container.MinKernelVersionMinor),
		Details: map[string]interface{}{
			"kernel":        v.Raw,
			"minimum":       fmt.Sprintf("%d.%d", container.MinKernelVersionMajor, container.MinKernelVersionMinor),
			"major":         v.Major,
			"minor":         v.Minor,
			"patch":         v.Patch,
			"meets_minimum": true,
		},
	}
}

// CheckPrivilegedProfile checks if the default Incus profile has security.privileged=true
func CheckPrivilegedProfile() HealthCheck {
	if !container.Available() {
		return HealthCheck{
			Name:    "privileged_profile",
			Status:  StatusOK,
			Message: "Skipped (Incus not available)",
		}
	}

	output, err := container.IncusOutput("profile", "get", "default", "security.privileged")
	if err != nil {
		return HealthCheck{
			Name:    "privileged_profile",
			Status:  StatusWarning,
			Message: "Could not check default profile — unable to verify container security",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	if strings.TrimSpace(output) == "true" {
		return HealthCheck{
			Name:   "privileged_profile",
			Status: StatusFailed,
			Message: "Default profile has security.privileged=true — this defeats all container isolation. " +
				"Remove with: incus profile unset default security.privileged",
			Details: map[string]interface{}{
				"security.privileged": "true",
			},
		}
	}

	return HealthCheck{
		Name:    "privileged_profile",
		Status:  StatusOK,
		Message: "Default profile uses unprivileged containers",
	}
}

// CheckSecurityPosture checks the overall container security posture by inspecting
// seccomp, AppArmor, and privilege settings on the default Incus profile.
func CheckSecurityPosture() HealthCheck {
	if !container.Available() {
		return HealthCheck{
			Name:    "security_posture",
			Status:  StatusOK,
			Message: "Skipped (Incus not available)",
		}
	}

	details := map[string]interface{}{}

	// Check security.privileged
	privOutput, err := container.IncusOutput("profile", "get", "default", "security.privileged")
	if err != nil {
		return HealthCheck{
			Name:    "security_posture",
			Status:  StatusWarning,
			Message: "Could not check default profile — unable to verify security posture",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	privileged := strings.TrimSpace(privOutput) == "true"
	details["privileged"] = privileged

	if privileged {
		details["seccomp"] = "disabled (privileged)"
		details["apparmor"] = "disabled (privileged)"
		details["raw_seccomp_override"] = false
		details["raw_apparmor_override"] = false

		return HealthCheck{
			Name:   "security_posture",
			Status: StatusFailed,
			Message: "Privileged containers — seccomp and AppArmor are disabled. " +
				"Remove with: incus profile unset default security.privileged",
			Details: details,
		}
	}

	// Check raw.seccomp override
	rawSeccomp, _ := container.IncusOutput("profile", "get", "default", "raw.seccomp")
	rawSeccompOverride := strings.TrimSpace(rawSeccomp) != ""
	details["raw_seccomp_override"] = rawSeccompOverride

	if rawSeccompOverride {
		details["seccomp"] = "custom override"
	} else {
		details["seccomp"] = "enabled (default)"
	}

	// Check raw.apparmor override
	rawApparmor, _ := container.IncusOutput("profile", "get", "default", "raw.apparmor")
	rawApparmorOverride := strings.TrimSpace(rawApparmor) != ""
	details["raw_apparmor_override"] = rawApparmorOverride

	// Check host AppArmor availability
	apparmorAvailable := false

	if runtime.GOOS == "linux" {
		if content, err := os.ReadFile("/sys/module/apparmor/parameters/enabled"); err == nil {
			apparmorAvailable = strings.TrimSpace(string(content)) == "Y"
		}
	}

	if rawApparmorOverride {
		details["apparmor"] = "custom override"
	} else if apparmorAvailable {
		details["apparmor"] = "enabled (default)"
	} else {
		details["apparmor"] = "not available"
	}

	// Determine status
	if rawSeccompOverride || rawApparmorOverride {
		msg := "Custom security profile overrides detected — verify your raw.seccomp/raw.apparmor settings"
		return HealthCheck{
			Name:    "security_posture",
			Status:  StatusWarning,
			Message: msg,
			Details: details,
		}
	}

	if !apparmorAvailable {
		return HealthCheck{
			Name:    "security_posture",
			Status:  StatusOK,
			Message: "Seccomp enabled, AppArmor not available (seccomp-only isolation)",
			Details: details,
		}
	}

	return HealthCheck{
		Name:    "security_posture",
		Status:  StatusOK,
		Message: "Full isolation — unprivileged containers with seccomp and AppArmor",
		Details: details,
	}
}

// CheckImmutableCapability checks whether the COI binary has CAP_LINUX_IMMUTABLE,
// which is needed to apply chattr +i on protected paths as defense-in-depth against
// the unshare+umount bypass of read-only bind mounts.
func CheckImmutableCapability() HealthCheck {
	if runtime.GOOS != "linux" {
		return HealthCheck{
			Name:    "immutable_capability",
			Status:  StatusOK,
			Message: "Skipped (not applicable on " + runtime.GOOS + ")",
		}
	}

	// Read effective capabilities from /proc/self/status
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return HealthCheck{
			Name:    "immutable_capability",
			Status:  StatusWarning,
			Message: "Cannot read process capabilities",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	// Parse CapEff line
	var capEff uint64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			hexVal := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
			_, err := fmt.Sscanf(hexVal, "%x", &capEff)
			if err != nil {
				return HealthCheck{
					Name:    "immutable_capability",
					Status:  StatusWarning,
					Message: "Cannot parse effective capabilities",
					Details: map[string]interface{}{
						"raw": hexVal,
					},
				}
			}
			break
		}
	}

	// CAP_LINUX_IMMUTABLE is bit 9
	const capLinuxImmutable = uint64(1) << 9

	if capEff&capLinuxImmutable != 0 {
		return HealthCheck{
			Name:    "immutable_capability",
			Status:  StatusOK,
			Message: "Host-side immutable protection available (CAP_LINUX_IMMUTABLE present)",
			Details: map[string]interface{}{
				"category": "SECURITY",
			},
		}
	}

	// Resolve the real binary path (setcap doesn't work on symlinks)
	fixCmd := "sudo setcap cap_linux_immutable=ep /path/to/coi"
	if exe, err := os.Executable(); err == nil {
		if resolved, linkErr := filepath.EvalSymlinks(exe); linkErr == nil {
			exe = resolved
		}
		fixCmd = fmt.Sprintf("sudo setcap cap_linux_immutable=ep %s", exe)
	}

	return HealthCheck{
		Name:   "immutable_capability",
		Status: StatusWarning,
		Message: "Host-side immutable protection unavailable — protected paths rely on bind-mount " +
			"read-only only (bypassable with root in container). Fix: " + fixCmd,
		Details: map[string]interface{}{
			"category":   "SECURITY",
			"mitigation": fixCmd,
		},
	}
}

// CheckTimezone reports whether the host timezone can be detected
func CheckTimezone() HealthCheck {
	tz, err := container.DetectHostTimezone()
	if err != nil || tz == "" {
		return HealthCheck{
			Name:    "timezone",
			Status:  StatusWarning,
			Message: "Could not detect host timezone — containers will use UTC",
			Details: map[string]interface{}{
				"detected": false,
			},
		}
	}

	return HealthCheck{
		Name:    "timezone",
		Status:  StatusOK,
		Message: fmt.Sprintf("Host timezone: %s", tz),
		Details: map[string]interface{}{
			"detected": true,
			"timezone": tz,
		},
	}
}
