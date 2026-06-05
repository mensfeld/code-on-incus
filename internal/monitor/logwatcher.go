package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
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

// watchedFile tracks an open log file and the read offset within it.
type watchedFile struct {
	name   string // short name, e.g. "auth.log"
	f      *os.File
	offset int64
}

// LogWatcher tails auth.log and syslog from the container rootfs via the
// host-side path /proc/<initPID>/root/... using inotify. Because the read
// happens outside the container, code running inside the container cannot
// blind the observer by manipulating /var/log or replacing log binaries.
// The watcher rescans for missing files every 5 seconds so files created
// after daemon startup are picked up automatically.
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
	if err != nil || initPID <= 0 {
		if ctx.Err() == nil && lw.onError != nil {
			lw.onError(fmt.Errorf("logwatcher: could not resolve init PID: %w", err))
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

	wdToFile := make(map[int32]*watchedFile)
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
		wd, err := unix.InotifyAddWatch(ifd, path, unix.IN_MODIFY)
		if err != nil {
			f.Close()
			return
		}
		name := rel[strings.LastIndex(rel, "/")+1:]
		wdToFile[int32(wd)] = &watchedFile{name: name, f: f, offset: off}
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

	// Read inotify events in a background goroutine and forward watch descriptors.
	eventCh := make(chan int32, 32)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := unix.Read(ifd, buf)
			if err != nil || n < unix.SizeofInotifyEvent {
				return // fd closed on ctx cancellation
			}
			for off := 0; off+unix.SizeofInotifyEvent <= n; {
				ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
				select {
				case eventCh <- ev.Wd:
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
		case wd := <-eventCh:
			wf, ok := wdToFile[wd]
			if ok {
				lw.readNewLines(wf)
			}
		case <-rescanTicker.C:
			for _, rel := range logCandidates {
				tryWatch(rel)
			}
		}
	}
}

// readNewLines reads lines appended since the last offset and emits threats.
func (lw *LogWatcher) readNewLines(wf *watchedFile) {
	if _, err := wf.f.Seek(wf.offset, 0); err != nil {
		return
	}
	scanner := bufio.NewScanner(wf.f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		wf.offset += int64(len(line)) + 1 // +1 for the newline byte

		if threat := parseAuthLogLine(wf.name, line); threat != nil && lw.onThreat != nil {
			lw.onThreat(*threat)
		}
	}
}

// parseAuthLogLine matches a single log line against known patterns and
// returns a ThreatEvent if it matches, nil otherwise. Exported for testing.
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

		evLine := line
		if len(evLine) > 300 {
			evLine = evLine[:300] + "…"
		}

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
