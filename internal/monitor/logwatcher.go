package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
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

// watchedFile tracks an open log file, its host-side path, and the read offset.
type watchedFile struct {
	name   string // short filename, e.g. "auth.log"
	path   string // full host-side path, needed to clean up watchedPaths on rotation
	f      *os.File
	offset int64
}

// inotifyEv carries the watch descriptor and event mask from the kernel.
type inotifyEv struct {
	wd   int
	mask uint32
}

// LogWatcher tails auth.log and syslog from the container rootfs via the
// host-side path /proc/<initPID>/root/... using inotify. Because the reads
// happen outside the container, code running inside cannot blind the observer
// by manipulating /var/log or replacing log binaries.
// The watcher rescans for missing files every 5 seconds so files created
// after daemon startup (or after log rotation) are picked up automatically.
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

// Run tails log files until ctx is cancelled. It returns silently if the
// container's init PID cannot be resolved or inotify is unavailable.
func (lw *LogWatcher) Run(ctx context.Context) {
	initPID, err := GetContainerInitPID(ctx, lw.containerName)
	if err != nil {
		if ctx.Err() == nil && lw.onError != nil {
			lw.onError(fmt.Errorf("logwatcher: could not resolve init PID: %w", err))
		}
		return
	}
	if initPID <= 0 {
		if lw.onError != nil {
			lw.onError(fmt.Errorf("logwatcher: invalid init PID %d for container %s", initPID, lw.containerName))
		}
		return
	}

	rootfs := fmt.Sprintf("/proc/%d/root", initPID)

	ifd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		if lw.onError != nil {
			lw.onError(fmt.Errorf("logwatcher: inotify_init: %w", err))
		}
		return
	}

	// Unblock the Read goroutine when the context is cancelled.
	go func() {
		<-ctx.Done()
		unix.Close(ifd)
	}()

	wdToFile := make(map[int]*watchedFile)
	watchedPaths := make(map[string]bool)

	tryWatch := func(rel string) {
		path := rootfs + "/" + rel
		if watchedPaths[path] {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		off, _ := f.Seek(0, 2) // start at EOF — don't replay history
		// Watch for modifications and for rotation/deletion so we can re-open the new inode.
		wd, err := unix.InotifyAddWatch(ifd, path, unix.IN_MODIFY|unix.IN_MOVE_SELF|unix.IN_DELETE_SELF)
		if err != nil {
			f.Close()
			return
		}
		name := rel[strings.LastIndex(rel, "/")+1:]
		wdToFile[wd] = &watchedFile{name: name, path: path, f: f, offset: off}
		watchedPaths[path] = true
	}

	// Initial scan.
	for _, rel := range logCandidates {
		tryWatch(rel)
	}

	defer func() {
		for _, wf := range wdToFile {
			wf.f.Close()
		}
	}()

	// Read inotify events in a background goroutine. Retries on EINTR so a
	// transient signal does not permanently stop log monitoring.
	// ev.Wd is int32; widening to int is always safe.
	eventCh := make(chan inotifyEv, 32)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := unix.Read(ifd, buf)
			if err != nil {
				if err == syscall.EINTR {
					continue // transient signal — retry
				}
				return // fd closed (ctx cancelled) or unrecoverable error
			}
			if n < unix.SizeofInotifyEvent {
				continue
			}
			for off := 0; off+unix.SizeofInotifyEvent <= n; {
				ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
				select {
				case eventCh <- inotifyEv{wd: int(ev.Wd), mask: ev.Mask}: // widening int32→int, always safe
				default:
				}
				off += unix.SizeofInotifyEvent + int(ev.Len)
			}
		}
	}()

	rescanTicker := time.NewTicker(5 * time.Second)
	defer rescanTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case ev := <-eventCh:
			wf, ok := wdToFile[ev.wd]
			if !ok {
				continue
			}
			if ev.mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
				// File rotated or deleted. Remove from tracking so the rescan ticker
				// can re-open the new inode. The kernel removes the watch automatically
				// on IN_DELETE_SELF, so no explicit InotifyRmWatch is needed.
				wf.f.Close()
				delete(wdToFile, ev.wd)
				delete(watchedPaths, wf.path)
			} else if ev.mask&unix.IN_MODIFY != 0 {
				lw.readNewLines(wf)
			}

		case <-rescanTicker.C:
			for _, rel := range logCandidates {
				tryWatch(rel)
			}
		}
	}
}

// readNewLines reads complete lines appended since the last offset and emits threats.
// It uses bufio.Reader.ReadString so offset tracking is byte-accurate and lines of
// any length are handled without the 64 KB ErrTooLong limit of bufio.Scanner.
func (lw *LogWatcher) readNewLines(wf *watchedFile) {
	if _, err := wf.f.Seek(wf.offset, 0); err != nil {
		return
	}
	reader := bufio.NewReader(wf.f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// Incomplete line (no newline yet) or read error — don't advance offset.
			break
		}
		// line includes the trailing '\n' (possibly '\r\n'), so len(line) is the
		// exact byte count consumed from the file.
		wf.offset += int64(len(line))
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		if threat := parseAuthLogLine(wf.name, trimmed); threat != nil && lw.onThreat != nil {
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
func truncateToRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
