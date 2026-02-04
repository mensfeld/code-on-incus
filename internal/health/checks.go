package health

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mensfeld/code-on-incus/internal/config"
	"github.com/mensfeld/code-on-incus/internal/container"
	"github.com/mensfeld/code-on-incus/internal/network"
	"github.com/mensfeld/code-on-incus/internal/session"
	"github.com/mensfeld/code-on-incus/internal/tool"
)

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
		if isColimaEnvironment() {
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

// isColimaEnvironment detects if running inside a Colima/Lima VM
func isColimaEnvironment() bool {
	// Check for virtiofs mounts (characteristic of Lima VMs)
	if content, err := os.ReadFile("/proc/mounts"); err == nil {
		if strings.Contains(string(content), "virtiofs") {
			return true
		}
	}

	// Check for lima user
	if currentUser, err := user.Current(); err == nil {
		if currentUser.Username == "lima" {
			return true
		}
	}

	return false
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

	// Parse version - extract server version from multi-line output
	// Example output: "Client version: 6.20\nServer version: 6.20"
	version := strings.TrimSpace(versionOutput)
	for _, line := range strings.Split(version, "\n") {
		if strings.HasPrefix(line, "Server version:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "Server version:"))
			break
		}
	}

	return HealthCheck{
		Name:    "incus",
		Status:  StatusOK,
		Message: fmt.Sprintf("Running (version %s)", version),
		Details: map[string]interface{}{
			"version": version,
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

	// Get user's groups
	groups, err := currentUser.GroupIds()
	if err != nil {
		return HealthCheck{
			Name:    "permissions",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not determine user groups: %v", err),
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

	// Check if user is in the group
	for _, gid := range groups {
		if gid == incusGroup.Gid {
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

	return HealthCheck{
		Name:    "permissions",
		Status:  StatusFailed,
		Message: fmt.Sprintf("User '%s' not in incus-admin group", currentUser.Username),
	}
}

// CheckImage verifies that the default image exists
func CheckImage(imageName string) HealthCheck {
	if imageName == "" {
		imageName = "coi"
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
	// Get default profile to find network device
	output, err := container.IncusOutput("profile", "device", "show", "default")
	if err != nil {
		return HealthCheck{
			Name:    "network_bridge",
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not get default profile: %v", err),
		}
	}

	// Parse network name from profile (looking for eth0 device)
	var networkName string
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "eth0:" {
			// Look for network: line
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
		return HealthCheck{
			Name:    "network_bridge",
			Status:  StatusFailed,
			Message: "No eth0 network device in default profile",
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

// CheckFirewall verifies firewalld availability based on network mode
func CheckFirewall(mode config.NetworkMode) HealthCheck {
	available := network.FirewallAvailable()
	isColima := isColimaEnvironment()

	if mode == config.NetworkModeOpen {
		// Firewall not required for open mode
		if available {
			return HealthCheck{
				Name:    "firewall",
				Status:  StatusOK,
				Message: "Available (not required for open mode)",
			}
		}
		return HealthCheck{
			Name:    "firewall",
			Status:  StatusOK,
			Message: "Not available (not required for open mode)",
		}
	}

	// Required for restricted/allowlist modes
	if !available {
		message := fmt.Sprintf("Not available (required for %s mode)", mode)
		if isColima {
			message = "Not available - use --network=open for Colima"
		}
		return HealthCheck{
			Name:    "firewall",
			Status:  StatusFailed,
			Message: message,
			Details: map[string]interface{}{
				"colima": isColima,
			},
		}
	}

	return HealthCheck{
		Name:    "firewall",
		Status:  StatusOK,
		Message: fmt.Sprintf("Running (%s mode available)", mode),
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
		imageName = "coi"
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

	// Launch ephemeral container
	if err := container.LaunchContainer(imageName, containerName); err != nil {
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
	var containerReady bool
	for i := 0; i < 30; i++ {
		running, err := container.ContainerRunning(containerName)
		if err == nil && running {
			// Try a simple command to verify container is responsive
			_, err := container.IncusOutput("exec", containerName, "--", "echo", "ready")
			if err == nil {
				containerReady = true
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if !containerReady {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusFailed,
			Message: "Test container failed to start within timeout",
		}
	}

	// Wait for DHCP to assign an IP (up to 15 seconds)
	var hasIP bool
	for i := 0; i < 15; i++ {
		// Check if eth0 has an IPv4 address
		ipOutput, err := container.IncusOutput("exec", containerName, "--", "ip", "-4", "addr", "show", "eth0")
		if err == nil && strings.Contains(ipOutput, "inet ") {
			hasIP = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !hasIP {
		return HealthCheck{
			Name:    "container_connectivity",
			Status:  StatusFailed,
			Message: "Container failed to get IP address (DHCP not working)",
		}
	}

	// Test 1: DNS resolution using getent
	dnsOutput, dnsErr := container.IncusOutput("exec", containerName, "--", "getent", "hosts", "api.anthropic.com")

	// Test 2: HTTP connectivity using curl
	httpOutput, httpErr := container.IncusOutput("exec", containerName, "--", "curl", "-s", "--connect-timeout", "5", "-o", "/dev/null", "-w", "%{http_code}", "https://api.anthropic.com")

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
	if !network.FirewallAvailable() {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusWarning,
			Message: "Skipped (firewalld not available)",
		}
	}

	// Skip if no image available
	if imageName == "" {
		imageName = "coi"
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

	// Launch ephemeral container
	if err := container.LaunchContainer(imageName, containerName); err != nil {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusFailed,
			Message: fmt.Sprintf("Failed to launch test container: %v", err),
		}
	}

	// Track if we applied firewall rules (for cleanup)
	var firewallManager *network.FirewallManager

	// Ensure cleanup on any exit path
	defer func() {
		// Remove firewall rules first
		if firewallManager != nil {
			_ = firewallManager.RemoveRules()
		}
		// Then stop/delete container
		_ = container.StopContainer(containerName)
		_ = container.DeleteContainer(containerName)
	}()

	// Wait for container to be ready and have network (up to 30 seconds)
	var containerReady bool
	for i := 0; i < 30; i++ {
		running, err := container.ContainerRunning(containerName)
		if err == nil && running {
			_, err := container.IncusOutput("exec", containerName, "--", "echo", "ready")
			if err == nil {
				containerReady = true
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if !containerReady {
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

	// Get gateway IP for firewall rules
	gatewayIP := ""
	// Try to extract gateway from container's route
	routeOutput, err := container.IncusOutput("exec", containerName, "--", "ip", "route", "show", "default")
	if err == nil {
		// Parse "default via 10.128.178.1 dev eth0"
		parts := strings.Fields(routeOutput)
		for i, part := range parts {
			if part == "via" && i+1 < len(parts) {
				gatewayIP = parts[i+1]
				break
			}
		}
	}

	// Apply restricted mode firewall rules
	firewallManager = network.NewFirewallManager(containerIP, gatewayIP)
	restrictedConfig := &config.NetworkConfig{
		Mode:                  config.NetworkModeRestricted,
		BlockPrivateNetworks:  true,
		BlockMetadataEndpoint: true,
	}

	if err := firewallManager.ApplyRestricted(restrictedConfig); err != nil {
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

	details := map[string]interface{}{
		"container_ip":        containerIP,
		"external_access":     externalOK,
		"private_blocked":     privateBlocked,
		"private_10_blocked":  privateBlocked,
		"private_192_blocked": private2Blocked,
	}

	if externalOK {
		details["external_status"] = httpOutput
	}

	// Evaluate results
	if externalOK && privateBlocked && private2Blocked {
		return HealthCheck{
			Name:    "network_restriction",
			Status:  StatusOK,
			Message: "Restricted mode working (external OK, private networks blocked)",
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

	return HealthCheck{
		Name:    "network_restriction",
		Status:  StatusWarning,
		Message: "Restricted mode partially working",
		Details: details,
	}
}

// CheckPasswordlessSudo verifies passwordless sudo for firewall-cmd
func CheckPasswordlessSudo() HealthCheck {
	// On macOS, not needed
	if runtime.GOOS == "darwin" {
		return HealthCheck{
			Name:    "passwordless_sudo",
			Status:  StatusOK,
			Message: "macOS - not required",
		}
	}

	// Try to run firewall-cmd --state with sudo -n
	cmd := exec.Command("sudo", "-n", "firewall-cmd", "--state")
	err := cmd.Run()
	if err != nil {
		// Check if firewall-cmd even exists
		if _, lookErr := exec.LookPath("firewall-cmd"); lookErr != nil {
			return HealthCheck{
				Name:    "passwordless_sudo",
				Status:  StatusOK,
				Message: "firewall-cmd not installed (not needed for open mode)",
			}
		}

		return HealthCheck{
			Name:    "passwordless_sudo",
			Status:  StatusWarning,
			Message: "Passwordless sudo not configured for firewall-cmd",
		}
	}

	return HealthCheck{
		Name:    "passwordless_sudo",
		Status:  StatusOK,
		Message: "Configured for firewall-cmd",
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
		imageName = "coi"
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
