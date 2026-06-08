package monitor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
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

// logState tracks the read position and inode for a single log file across
// polls so the watcher can detect log rotation and only process new lines.
type logState struct {
	offset        int64
	inode         uint64 // 0 = unknown (incus fallback has no fd-level inode)
	usingFallback bool   // true while the host-side path is unavailable
}

// LogWatcher tails auth.log and syslog from the container, polling every
// 5 seconds. On each tick it resolves the container's init PID fresh (same
// as the network monitor) and attempts a direct read from
// /proc/<initPID>/root/<rel>. This avoids the incus daemon subprocess and
// temp-file overhead of the previous incus file pull approach.
//
// Note: unlike the network (/proc/<pid>/net/*) and process (cgroup walk)
// monitors which read kernel-maintained data, auth.log and syslog are
// regular files inside the container's writable filesystem. A root attacker
// inside the container could truncate or modify them. What the host-side
// read buys is independence from the incus daemon/agent, no subprocess or
// temp-file per tick, and access to the live mount-namespace view.
//
// When the host path is unreadable (e.g. the monitor lacks ptrace permission
// in the container's user-namespace) the watcher falls back to incus file
// pull and logs the transition once via onError.
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

// Run tails log files until ctx is cancelled, polling every 5 seconds.
// The init PID is resolved on every tick (matching the network monitor)
// so a container restart never leaves a stale or recycled PID in use.
func (lw *LogWatcher) Run(ctx context.Context) {
	states := make(map[string]logState)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pid, _ := GetContainerInitPID(ctx, lw.containerName)
			for _, rel := range logCandidates {
				lw.pollFile(ctx, rel, states, pid)
			}
		}
	}
}

// pollFile reads lines appended to rel since the last poll and fires threat
// events for any that match. It tries a direct host-side read first and falls
// back to incus file pull on failure, logging the transition once.
//
// Only newline-terminated lines are processed; a trailing partial line is
// left at the current offset and re-read once the newline arrives.
func (lw *LogWatcher) pollFile(ctx context.Context, rel string, states map[string]logState, pid int) {
	state := states[rel]

	var newData []byte
	var base int64
	var inode uint64
	hostOK := false

	if pid > 0 {
		hostPath := fmt.Sprintf("/proc/%d/root/%s", pid, rel)
		d, b, ino, err := readFileChunk(hostPath, state)
		if err == nil {
			hostOK = true
			newData = d
			base = b
			inode = ino
		}
	}

	if !hostOK {
		// Log once when transitioning into fallback mode.
		if !state.usingFallback && lw.onError != nil {
			lw.onError(fmt.Errorf("logwatcher: host-side read unavailable for %s (pid=%d), falling back to incus file pull", rel, pid))
		}
		d, b, ok := lw.pullFileChunk(ctx, rel, state)
		if !ok {
			return
		}
		newData = d
		base = b
		inode = 0 // incus file pull has no file descriptor, so no inode
	}

	newState := logState{inode: inode, usingFallback: !hostOK}

	if len(newData) == 0 {
		newState.offset = base
		states[rel] = newState
		return
	}

	// Only advance the offset to the last complete (newline-terminated) line.
	// Bytes after the final newline are a partial line mid-write; leaving the
	// offset before them means they will be re-read and completed next tick.
	lastNL := bytes.LastIndexByte(newData, '\n')
	if lastNL < 0 {
		// No complete line yet — preserve offset, update inode.
		newState.offset = base
		states[rel] = newState
		return
	}
	newState.offset = base + int64(lastNL+1)
	states[rel] = newState

	logFile := rel[strings.LastIndex(rel, "/")+1:]
	reader := bufio.NewReader(bytes.NewReader(newData[:lastNL+1]))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
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

// readFileChunk opens path, detects log rotation (inode change or file
// smaller than the stored offset), seeks to the appropriate offset, and
// reads all available bytes. It returns the data, the base offset the read
// started from (0 after a rotation reset), and the current inode.
// Returns (nil, offset, inode, nil) when the file has not grown.
func readFileChunk(path string, state logState) ([]byte, int64, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, state.offset, 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, state.offset, 0, err
	}

	var curInode uint64
	if sysInfo, ok := fi.Sys().(*syscall.Stat_t); ok {
		curInode = sysInfo.Ino
	}

	offset := state.offset
	// Detect log rotation: inode changed (skip check when state.inode is 0,
	// which means the previous read used the incus fallback with no inode) or
	// file is smaller than our offset (truncation / fresh rotation target).
	inodeChanged := state.inode != 0 && curInode != 0 && curInode != state.inode
	if inodeChanged || fi.Size() < offset {
		offset = 0
	}

	if fi.Size() == offset {
		return nil, offset, curInode, nil // no new data
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, curInode, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, curInode, err
	}

	return data, offset, curInode, nil
}

// pullFileChunk fetches the log file via incus file pull and returns bytes
// appended since state.offset. It detects truncation by size (no inode
// available for this path). Returns (nil, base, true) when there is no new
// content and (nil, offset, false) on error.
func (lw *LogWatcher) pullFileChunk(ctx context.Context, rel string, state logState) ([]byte, int64, bool) {
	tmp, err := os.CreateTemp("", "coi-log-*")
	if err != nil {
		return nil, state.offset, false
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	src := lw.containerName + "/" + rel
	if _, err := container.IncusOutputContext(ctx, "file", "pull", src, tmpPath); err != nil {
		return nil, state.offset, false // file absent or container not running — benign
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, state.offset, false
	}

	offset := state.offset
	// Detect truncation (no inode for incus file pull).
	if int64(len(content)) < offset {
		offset = 0
	}

	if int64(len(content)) == offset {
		return nil, offset, true // no new content
	}

	return content[offset:], offset, true
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
