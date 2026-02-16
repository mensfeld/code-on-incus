package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./falco-poc <container-name>")
		fmt.Println("Example: ./falco-poc test-falco")
		os.Exit(1)
	}

	containerName := os.Args[1]

	fmt.Printf("🔍 Reading Falco events from journald...\n")
	fmt.Printf("🎯 Filtering events for container: %s\n", containerName)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Waiting for events... (trigger some in the container)")
	fmt.Println("")

	// Follow Falco logs from journald
	cmd := exec.Command("journalctl", "-u", "falco-modern-bpf", "-f", "-n", "0", "-o", "cat")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("❌ Failed to create pipe: %v\n", err)
		os.Exit(1)
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("❌ Failed to start journalctl: %v\n", err)
		os.Exit(1)
	}

	// Read and filter events
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip non-event lines
		if !strings.Contains(line, ": Warning ") &&
			!strings.Contains(line, ": Notice ") &&
			!strings.Contains(line, ": Error ") &&
			!strings.Contains(line, ": Critical ") &&
			!strings.Contains(line, ": Alert ") &&
			!strings.Contains(line, ": Emergency ") {
			continue
		}

		// Filter by container name
		if !strings.Contains(line, "container_id="+containerName) &&
			!strings.Contains(line, "container_name="+containerName) {
			continue
		}

		// Print event
		printEvent(line, containerName)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("❌ Error reading events: %v\n", err)
		os.Exit(1)
	}

	cmd.Wait()
}

func printEvent(line, containerName string) {
	colorReset := "\033[0m"
	colorRed := "\033[31m"
	colorYellow := "\033[33m"
	colorCyan := "\033[36m"

	// Determine priority/color
	color := colorYellow
	priority := "UNKNOWN"

	if strings.Contains(line, ": Critical ") || strings.Contains(line, ": Emergency ") || strings.Contains(line, ": Alert ") {
		color = colorRed
		priority = "CRITICAL"
	} else if strings.Contains(line, ": Error ") {
		color = colorRed
		priority = "ERROR"
	} else if strings.Contains(line, ": Warning ") {
		color = colorYellow
		priority = "WARNING"
	} else if strings.Contains(line, ": Notice ") {
		color = colorCyan
		priority = "NOTICE"
	}

	// Extract rule name (first part after timestamp)
	parts := strings.SplitN(line, ": ", 3)
	rule := "Event"
	if len(parts) >= 3 {
		ruleParts := strings.SplitN(parts[2], " (", 2)
		if len(ruleParts) > 0 {
			rule = ruleParts[0]
		}
	}

	fmt.Printf("%s⚠️  [%s] %s%s\n", color, priority, rule, colorReset)
	fmt.Printf("   📝 %s\n", line)
	fmt.Printf("   📦 Container: %s\n", containerName)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("")
}
