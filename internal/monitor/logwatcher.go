package monitor

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mensfeld/code-on-incus/internal/container"
)

// logCandidates are log paths (relative to container rootfs) that the watcher tails.
var logCandidates = []string{
	"var/log/auth.log",
	"var/log/syslog",
}

// logPattern defines a set of keywords that must all appear in a log line
// (case-insensitive) and the threat to raise when matched.
type logPattern struct {
	name     string
	keywords []string
	level    ThreatLevel
	title    string
	desc     string
}

var authLogPatterns = []logPattern{
	{
		name:     "ssh_failed_password",
		keywords: []string{"failed password"},
		level:    ThreatLevelWarning,
		title:    "SSH authentication failure",
		desc:     "Failed SSH password attempt detected in auth log",
	},
	{
		name:     "ssh_invalid_user",
		keywords: []string{"invalid user"},
		level:    ThreatLevelWarning,
		title:    "SSH login attempt with invalid user",
		desc:     "SSH login attempt with an unknown username detected in auth log",
	},
	{
		name:     "pam_auth_failure",
		keywords: []string{"pam_unix", "authentication failure"},
		level:    ThreatLevelWarning,
		title:    "PAM authentication failure",
		desc:     "PAM authentication failure detected in auth log",
	},
	{
		name:     "sudo_not_allowed",
		keywords: []string{"sudo", "command not allowed"},
		level:    ThreatLevelHigh,
		title:    "Unauthorized sudo command",
		desc:     "A user attempted an unauthorized sudo command",
	},
	{
		name:     "sudo_not_in_sudoers",
		keywords: []string{"sudo", "sudoers"},
		level:    ThreatLevelHigh,
		title:    "Unauthorized sudo attempt (not in sudoers)",
		desc:     "A user not listed in sudoers attempted to use sudo",
	},
	{
		name:     "su_failed",
		keywords: []string{"failed su for"},
		level:    ThreatLevelHigh,
		title:    "Failed su privilege escalation",
		desc:     "A failed su attempt was detected, indicating a possible privilege escalation",
	},
	{
		name:     "root_session",
		keywords: []string{"session opened for user root"},
		level:    ThreatLevelWarning,
		title:    "Root session opened",
		desc:     "A root session was opened inside the container",
	},
}

// LogWatcher tails auth.log and syslog from the container via the Incus file
// API. Reading is performed through the Incus daemon (which runs as root), so
// no CAP_SYS_PTRACE is required on the monitoring daemon side. The watcher
// polls every 5 seconds and processes only lines appended since the last read.
type LogWatcher struct {
	containerName string
	onThreat      func(ThreatEvent)
	onError       func(error)
}

// NewLogWatcher creates a LogWatcher for the named container.
func NewLogWatcher(containerName string, onThreat func(ThreatEvent), onError func(error)) *LogWatcher {
	return &LogWatcher{
		containerName: containerName,
		onThreat:      onThreat,
		onError:       onError,
	}
}

// Run tails log files until ctx is cancelled. It polls every 5 seconds using
// incus file pull, which reads through the Incus daemon and does not require
// direct filesystem access to /proc/<pid>/root/.
func (lw *LogWatcher) Run(ctx context.Context) {
	offsets := make(map[string]int64) // log candidate path → bytes consumed

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, rel := range logCandidates {
				lw.pollFile(ctx, rel, offsets)
			}
		}
	}
}

// pollFile fetches the named log file from the container via incus file pull,
// processes lines that have appeared since the last poll, and updates offsets.
func (lw *LogWatcher) pollFile(ctx context.Context, rel string, offsets map[string]int64) {
	// Pull to a temp file via the Incus daemon (no ptrace needed).
	tmp, err := os.CreateTemp("", "coi-log-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	// incus file pull <container>/<path> <local-dest>
	src := lw.containerName + "/" + rel
	if _, err := container.IncusOutputContext(ctx, "file", "pull", src, tmpPath); err != nil {
		return // file absent or container not running — benign
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return
	}

	offset := offsets[rel]
	if int64(len(content)) <= offset {
		return // no new content
	}

	newData := content[offset:]
	offsets[rel] = int64(len(content))

	logFile := rel[strings.LastIndex(rel, "/")+1:]
	reader := bufio.NewReader(bytes.NewReader(newData))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break // incomplete line or EOF — wait for more data next tick
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		if threat := parseAuthLogLine(logFile, trimmed); threat != nil && lw.onThreat != nil {
			lw.onThreat(*threat)
		}
	}
}

// parseAuthLogLine checks a single log line against known auth patterns and
// returns a ThreatEvent if it matches, nil otherwise.
func parseAuthLogLine(logFile, line string) *ThreatEvent {
	lower := strings.ToLower(line)
	for _, p := range authLogPatterns {
		matched := true
		for _, kw := range p.keywords {
			if !strings.Contains(lower, kw) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}

		evLine := truncateToRunes(line, 300)

		return &ThreatEvent{
			ID:          uuid.New().String(),
			Timestamp:   time.Now(),
			Level:       p.level,
			Category:    "auth",
			Title:       p.title,
			Description: p.desc + ": " + evLine,
			Evidence: Evidence{
				AuthLog: &AuthLogThreat{
					LogFile: logFile,
					Line:    evLine,
					Pattern: p.name,
				},
			},
			Action: "pending",
		}
	}
	return nil
}

// truncateToRunes truncates s to at most max Unicode code points, appending
// "…" if truncation occurs. This avoids splitting UTF-8 sequences mid-byte.
func truncateToRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "…"
}
