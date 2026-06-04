package monitor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// CollectProcessStats collects running processes from the container.
// It first attempts to walk the host /proc filtered by the container's PID
// namespace, which is tamper-resistant because it runs outside the container.
// If that fails it falls back to incus exec ps aux.
func CollectProcessStats(ctx context.Context, containerName string) (ProcessStats, error) {
	processes, err := collectProcessesViaHostProc(ctx, containerName)
	if err != nil {
		// Fallback: run ps inside the container.
		return collectProcessesViaIncusExec(ctx, containerName)
	}

	for i := range processes {
		processes[i].EnvAccess = checkEnvAccess(processes[i].Command)
	}
	return ProcessStats{
		Available:  true,
		TotalCount: len(processes),
		Processes:  processes,
	}, nil
}

// collectProcessesViaHostProc walks the host /proc directory and returns all
// processes whose PID namespace matches the container's init process. Reading
// from the host means an attacker inside the container cannot hide processes by
// manipulating their own /proc or killing ps.
func collectProcessesViaHostProc(ctx context.Context, containerName string) ([]Process, error) {
	initPID, err := GetContainerInitPID(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("could not resolve container init PID: %w", err)
	}

	// The namespace symlink looks like "pid:[4026532456]". All processes in the
	// container share the same value.
	containerNS, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", initPID))
	if err != nil {
		return nil, fmt.Errorf("could not read pid namespace of init PID %d: %w", initPID, err)
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("could not read /proc: %w", err)
	}

	var processes []Process
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		ns, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
		if err != nil || ns != containerNS {
			continue
		}

		proc, err := readProcessFromProc(pid)
		if err != nil {
			continue
		}
		processes = append(processes, proc)
	}

	return processes, nil
}

// readProcessFromProc reads /proc/<pid>/status and /proc/<pid>/cmdline to build
// a Process. The PID here is the host-side PID.
func readProcessFromProc(pid int) (Process, error) {
	statusData, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return Process{}, err
	}

	name, ppid, uid := parseStatusFields(string(statusData))

	command := name
	if cmdlineData, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil && len(cmdlineData) > 0 {
		command = parseCmdline(cmdlineData)
	}

	return Process{
		PID:     pid,
		PPID:    ppid,
		User:    strconv.Itoa(uid),
		Command: command,
	}, nil
}

// parseStatusFields extracts Name, PPid, and real Uid from /proc/<pid>/status
// content. Kept separate so it can be unit-tested without a live /proc.
func parseStatusFields(status string) (name string, ppid, uid int) {
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "Name:":
			name = fields[1]
		case "PPid:":
			ppid, _ = strconv.Atoi(fields[1])
		case "Uid:":
			uid, _ = strconv.Atoi(fields[1]) // real UID is the first field
		}
	}
	return
}

// parseCmdline converts null-delimited /proc/<pid>/cmdline bytes to a
// space-joined command string. Kept separate so it can be unit-tested.
func parseCmdline(data []byte) string {
	trimmed := strings.TrimRight(string(data), "\x00")
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Split(trimmed, "\x00"), " ")
}

// collectProcessesViaIncusExec is the fallback path: runs ps aux inside the
// container via incus exec. An attacker inside the container can manipulate
// this view, but it remains available in environments where host /proc walks
// are not possible.
func collectProcessesViaIncusExec(ctx context.Context, containerName string) (ProcessStats, error) {
	output, err := container.IncusOutputContext(ctx, "exec", containerName, "--", "ps", "aux")
	if err != nil {
		return ProcessStats{Available: false}, fmt.Errorf("failed to execute ps: %w", err)
	}

	processes, err := parseProcessList(output)
	if err != nil {
		return ProcessStats{Available: false}, fmt.Errorf("failed to parse process list: %w", err)
	}

	for i := range processes {
		processes[i].EnvAccess = checkEnvAccess(processes[i].Command)
	}
	return ProcessStats{
		Available:  true,
		TotalCount: len(processes),
		Processes:  processes,
	}, nil
}

// parseProcessList parses output from 'ps aux'
func parseProcessList(output string) ([]Process, error) {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid ps output: too few lines")
	}

	// Skip header line
	lines = lines[1:]

	var processes []Process
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse ps aux output format:
		// USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		// COMMAND is everything from field 10 onwards
		command := strings.Join(fields[10:], " ")

		// Try to extract PPID (not available in 'ps aux', so we'll use 0)
		// In a full implementation, we could use 'ps -eo pid,ppid,user,comm,args'
		processes = append(processes, Process{
			PID:     pid,
			PPID:    0, // Not available in 'ps aux'
			User:    fields[0],
			Command: command,
		})
	}

	return processes, nil
}

// checkEnvAccess checks if a command is likely accessing environment variables
func checkEnvAccess(command string) bool {
	// Check for common environment-scanning commands
	envCommands := []string{
		"env",
		"printenv",
		"set",
		"export",
	}

	cmdLower := strings.ToLower(command)

	// Check if command is exactly one of the env commands
	for _, envCmd := range envCommands {
		if strings.HasPrefix(cmdLower, envCmd+" ") || cmdLower == envCmd {
			return true
		}
	}

	// Check for grep/awk/sed parsing environment variables with secret-related keywords
	secretKeywords := []string{"api", "key", "password", "secret", "token", "credential", "auth"}
	if strings.Contains(cmdLower, "grep") || strings.Contains(cmdLower, "awk") || strings.Contains(cmdLower, "sed") {
		for _, keyword := range secretKeywords {
			if strings.Contains(cmdLower, keyword) {
				return true
			}
		}
	}

	// Check for /proc/*/environ access via any tool
	if strings.Contains(cmdLower, "/proc/") && strings.Contains(cmdLower, "environ") {
		return true
	}

	// Check for language-specific environment access patterns
	langEnvPatterns := []string{
		// Python: os.environ, os.getenv
		"os.environ",
		"os.getenv",
		// Node.js: process.env
		"process.env",
		// Ruby: ENV[
		"env[",
		// awk ENVIRON array
		"environ[",
	}
	for _, pattern := range langEnvPatterns {
		if strings.Contains(cmdLower, pattern) {
			return true
		}
	}

	// Check for binary tools reading /proc/*/environ
	procEnvTools := []string{
		"strings /proc",
		"xxd /proc",
		"hexdump /proc",
		"xargs",
	}
	for _, tool := range procEnvTools {
		if strings.Contains(cmdLower, tool) && strings.Contains(cmdLower, "environ") {
			return true
		}
	}

	// Check for xargs specifically reading null-delimited environ files
	if strings.Contains(cmdLower, "xargs") && strings.Contains(cmdLower, "/proc/") {
		return true
	}

	return false
}

// DetectReverseShells checks processes for reverse shell indicators
func DetectReverseShells(processes []Process) []ProcessThreat {
	var threats []ProcessThreat

	reverseShellPatterns := []struct {
		pattern    string
		indicators []string
	}{
		// Netcat reverse shells
		{"nc -e", []string{"netcat with exec"}},
		{"nc.traditional -e", []string{"netcat with exec"}},
		{"ncat -e", []string{"ncat with exec"}},
		{"nc.openbsd -e", []string{"netcat with exec"}},

		// Bash/sh reverse shells
		{"bash -i", []string{"interactive bash"}},
		{"sh -i", []string{"interactive shell"}},
		{"/dev/tcp/", []string{"bash tcp redirect"}},
		{"/dev/udp/", []string{"bash udp redirect"}},

		// Python reverse shells
		{"python -c", []string{"python one-liner"}},
		{"python3 -c", []string{"python one-liner"}},
		{"socket.socket", []string{"python socket"}},

		// Perl reverse shells
		{"perl -e", []string{"perl one-liner"}},
		{"perl -MIO", []string{"perl IO module"}},

		// PHP reverse shells
		{"php -r", []string{"php one-liner"}},
		{"fsockopen", []string{"php socket"}},

		// Ruby reverse shells
		{"ruby -rsocket", []string{"ruby socket"}},
		{"ruby -e", []string{"ruby one-liner"}},

		// Socat reverse shells
		{"socat", []string{"socat"}},
		{"EXEC:", []string{"socat exec"}},

		// PowerShell reverse shells (if Wine/mono present)
		{"powershell", []string{"powershell"}},
		{"System.Net.Sockets", []string{"dotnet sockets"}},
	}

	for _, proc := range processes {
		cmdLower := strings.ToLower(proc.Command)

		for _, pattern := range reverseShellPatterns {
			if strings.Contains(cmdLower, strings.ToLower(pattern.pattern)) {
				// Additional check: if it's a network-related command, it's more suspicious
				isNetworkRelated := strings.Contains(cmdLower, ":") ||
					strings.Contains(cmdLower, "sock") || // Matches socket, fsockopen, etc.
					strings.Contains(cmdLower, "tcp") ||
					strings.Contains(cmdLower, "udp") ||
					containsIPPattern(cmdLower)

				if isNetworkRelated || pattern.pattern == "bash -i" || pattern.pattern == "sh -i" {
					threats = append(threats, ProcessThreat{
						PID:        proc.PID,
						Command:    proc.Command,
						User:       proc.User,
						Pattern:    pattern.pattern,
						Indicators: pattern.indicators,
					})
					break
				}
			}
		}
	}

	return threats
}

// containsIPPattern checks if command contains an IP address pattern
func containsIPPattern(cmd string) bool {
	// Simple regex-like check for IP patterns (xxx.xxx.xxx.xxx)
	parts := strings.Fields(cmd)
	for _, part := range parts {
		octets := strings.Split(part, ".")
		if len(octets) == 4 {
			allNumeric := true
			for _, octet := range octets {
				if _, err := strconv.Atoi(octet); err != nil {
					allNumeric = false
					break
				}
			}
			if allNumeric {
				return true
			}
		}
	}
	return false
}

// DetectEnvScanning checks processes for environment variable scanning
func DetectEnvScanning(processes []Process) []ProcessThreat {
	var threats []ProcessThreat

	for _, proc := range processes {
		if proc.EnvAccess {
			threats = append(threats, ProcessThreat{
				PID:        proc.PID,
				Command:    proc.Command,
				User:       proc.User,
				Pattern:    "environment scanning",
				Indicators: []string{"accessing environment variables"},
			})
		}
	}

	return threats
}
