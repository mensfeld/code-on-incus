package monitor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
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

// LogWatcher tails auth.log and syslog from the container. On each tick it
// first attempts a host-side read via /proc/<initPID>/root/<rel> (tamper-proof:
// an attacker inside the container cannot hide lines by manipulating their own
// /var/log). If that path is unreadable (e.g. the monitor lacks ptrace
// permission in the container's user-namespace), it falls back to incus file
// pull via the Incus daemon. Only lines appended since the last poll are
// processed.
type LogWatcher struct {
	containerName string
	onThreat      func(ThreatEvent)
	onError       func(error)
	initPID       int // cached init PID; 0 = not yet resolved
}

// NewLogWatcher creates a LogWatcher for the named container.
func NewLogWatcher(containerName string, onThreat func(ThreatEvent), onError func(error)) *LogWatcher {
	return &LogWatcher{
		containerName: containerName,
		onThreat:      onThreat,
		onError:       onError,
	}
}

// Run tails log files until ctx is cancelled, polling every 5 seconds.
func (lw *LogWatcher) Run(ctx context.Context) {
	offsets := make(map[string]int64) // log candidate path → bytes consumed

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Lazily resolve and cache the init PID so subsequent polls use a
			// direct file read without spawning incus info each time.
			if lw.initPID == 0 {
				if pid, err := GetContainerInitPID(ctx, lw.containerName); err == nil {
					lw.initPID = pid
				}
			}
			for _, rel := range logCandidates {
				lw.pollFile(ctx, rel, offsets)
			}
		}
	}
}

// pollFile reads new lines from rel (relative to container rootfs), processes
// them, and advances the offset. It tries a direct host-side read first and
// falls back to incus file pull if that fails.
func (lw *LogWatcher) pollFile(ctx context.Context, rel string, offsets map[string]int64) {
	var newData []byte
	hostOK := false

	// Primary path: host-side read via /proc/<initPID>/root/<rel>.
	if lw.initPID > 0 {
		hostPath := fmt.Sprintf("/proc/%d/root/%s", lw.initPID, rel)
		data, newOffset, err := readFileChunk(hostPath, offsets[rel])
		if err == nil {
			hostOK = true
			newData = data
			offsets[rel] = newOffset
		}
	}

	// Fallback: incus file pull via the Incus daemon.
	if !hostOK {
		data, newOffset, ok := lw.pullFileChunk(ctx, rel, offsets[rel])
		if !ok {
			return
		}
		newData = data
		offsets[rel] = newOffset
	}

	if len(newData) == 0 {
		return
	}

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

// readFileChunk opens path, seeks to offset, and reads all bytes appended
// since that offset. Returns (nil, offset, nil) when the file has not grown.
func readFileChunk(path string, offset int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if fi.Size() <= offset {
		return nil, offset, nil // no new data
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}

	return data, offset + int64(len(data)), nil
}

// pullFileChunk fetches the log file from the container via incus file pull
// and returns bytes appended since offset. Returns (nil, offset, true) when
// there is no new content, and (nil, offset, false) on error.
func (lw *LogWatcher) pullFileChunk(ctx context.Context, rel string, offset int64) ([]byte, int64, bool) {
	tmp, err := os.CreateTemp("", "coi-log-*")
	if err != nil {
		return nil, offset, false
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	src := lw.containerName + "/" + rel
	if _, err := container.IncusOutputContext(ctx, "file", "pull", src, tmpPath); err != nil {
		return nil, offset, false // file absent or container not running — benign
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, offset, false
	}

	if int64(len(content)) <= offset {
		return nil, offset, true // no new content
	}

	return content[offset:], int64(len(content)), true
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
