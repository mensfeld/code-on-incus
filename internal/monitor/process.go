package monitor

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mensfeld/code-on-incus/internal/container"
)

// CollectProcessStats collects running processes from the container
func CollectProcessStats(ctx context.Context, containerName string) (ProcessStats, error) {
	// Execute: incus exec <container> -- ps aux
	output, err := container.IncusOutput("exec", containerName, "--", "ps", "aux")
	if err != nil {
		return ProcessStats{Available: false}, fmt.Errorf("failed to execute ps: %w", err)
	}

	processes, err := parseProcessList(output)
	if err != nil {
		return ProcessStats{Available: false}, fmt.Errorf("failed to parse process list: %w", err)
	}

	// For each process, check if it has accessed environment variables
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
			log.Printf("[detector] checkEnvAccess: MATCHED env command %q in %q", envCmd, command)
			return true
		}
	}

	// Check for grep/awk/sed parsing environment variables
	if strings.Contains(cmdLower, "grep") && (strings.Contains(cmdLower, "api") ||
		strings.Contains(cmdLower, "key") ||
		strings.Contains(cmdLower, "password") ||
		strings.Contains(cmdLower, "secret") ||
		strings.Contains(cmdLower, "token")) {
		log.Printf("[detector] checkEnvAccess: MATCHED grep pattern in %q", command)
		return true
	}

	// Check for /proc/*/environ access
	if strings.Contains(cmdLower, "/proc/") && strings.Contains(cmdLower, "environ") {
		log.Printf("[detector] checkEnvAccess: MATCHED /proc/environ in %q", command)
		return true
	}

	return false
}

// DetectReverseShells checks processes for reverse shell indicators
func DetectReverseShells(processes []Process) []ProcessThreat {
	var threats []ProcessThreat

	log.Printf("[detector] DetectReverseShells: checking %d processes", len(processes))

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
		log.Printf("[detector] Checking PID %d: %q", proc.PID, proc.Command)

		// DEBUG: Log if we see PHP, fsockopen, python, perl, socket
		if strings.Contains(cmdLower, "php") || strings.Contains(cmdLower, "fsockopen") ||
			strings.Contains(cmdLower, "python") || strings.Contains(cmdLower, "perl") ||
			strings.Contains(cmdLower, "socket") {
			log.Printf("[detector] ⚠️  INTERESTING PROCESS: PID %d contains suspicious keyword: %q", proc.PID, proc.Command)
		}

		for _, pattern := range reverseShellPatterns {
			if strings.Contains(cmdLower, strings.ToLower(pattern.pattern)) {
				log.Printf("[detector] Pattern MATCHED: %q in command %q", pattern.pattern, proc.Command)

				// Additional check: if it's a network-related command, it's more suspicious
				isNetworkRelated := strings.Contains(cmdLower, ":") ||
					strings.Contains(cmdLower, "sock") || // Matches socket, fsockopen, etc.
					strings.Contains(cmdLower, "tcp") ||
					strings.Contains(cmdLower, "udp") ||
					containsIPPattern(cmdLower)

				log.Printf("[detector] isNetworkRelated=%v, pattern=%q", isNetworkRelated, pattern.pattern)

				if isNetworkRelated || pattern.pattern == "bash -i" || pattern.pattern == "sh -i" {
					log.Printf("[detector] THREAT DETECTED: PID %d, pattern %q", proc.PID, pattern.pattern)
					threats = append(threats, ProcessThreat{
						PID:        proc.PID,
						Command:    proc.Command,
						User:       proc.User,
						Pattern:    pattern.pattern,
						Indicators: pattern.indicators,
					})
					break
				} else {
					log.Printf("[detector] Pattern matched but not network-related, skipping")
				}
			}
		}
	}

	log.Printf("[detector] DetectReverseShells: found %d threats", len(threats))

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

	log.Printf("[detector] DetectEnvScanning: checking %d processes", len(processes))

	for _, proc := range processes {
		log.Printf("[detector] PID %d EnvAccess=%v: %q", proc.PID, proc.EnvAccess, proc.Command)
		if proc.EnvAccess {
			log.Printf("[detector] ENV SCANNING DETECTED: PID %d", proc.PID)
			threats = append(threats, ProcessThreat{
				PID:        proc.PID,
				Command:    proc.Command,
				User:       proc.User,
				Pattern:    "environment scanning",
				Indicators: []string{"accessing environment variables"},
			})
		}
	}

	log.Printf("[detector] DetectEnvScanning: found %d threats", len(threats))
	return threats
}
