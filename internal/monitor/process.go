package monitor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CollectProcessStats collects running processes by walking the host /proc
// filtered to the container's PID namespace. Because the read happens outside
// the container, an attacker inside cannot hide processes by manipulating their
// own /proc, replacing ps, or pausing incus exec.
func CollectProcessStats(ctx context.Context, containerName string) (ProcessStats, error) {
	processes, err := collectProcessesViaHostProc(ctx, containerName)
	if err != nil {
		return ProcessStats{Available: false}, fmt.Errorf("process monitoring unavailable: %w", err)
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
//
// Limitation: processes that create a nested PID namespace inside the container
// (e.g. via unshare -p) will have a different /proc/<pid>/ns/pid inode and will
// not be matched. This is an accepted trade-off; the technique still defeats the
// common attacks of replacing ps or bind-mounting a fake /proc.
func collectProcessesViaHostProc(ctx context.Context, containerName string) ([]Process, error) {
	initPID, err := GetContainerInitPID(ctx, containerName)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
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

	// A running container must have at least its init process visible. Zero
	// results indicate a permissions problem (e.g. hidepid) rather than a
	// genuinely empty container.
	if len(processes) == 0 {
		return nil, fmt.Errorf("no processes found in container namespace (pid ns: %s) — possible hidepid or permission error", containerNS)
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
		PID:  pid,
		PPID: ppid,
		// User is the real (host-side) UID as a decimal string. Unlike the
		// old incus exec ps aux path, this is numeric rather than a username.
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
