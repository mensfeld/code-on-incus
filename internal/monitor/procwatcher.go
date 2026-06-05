package monitor

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	cnIdxProc         = 1
	cnValProc         = 1
	procCnMcastListen = 1

	procEventExec = 0x00000002
	procEventUID  = 0x00000004
)

// execPattern defines how to detect a suspicious process exec.
// arg0 (if non-empty) restricts the match to processes whose argv[0] basename
// starts with the given prefix (case-insensitive), preventing false positives
// from shell scripts whose argument strings happen to contain suspicious words
// (e.g. "bash -c '... python -c socket.socket ...'").
// All keywords must also appear in the full cmdline (case-insensitive).
type execPattern struct {
	name     string
	arg0     string // argv[0] basename must start with this prefix (case-insensitive)
	keywords []string
}

var execPatterns = []execPattern{
	// Netcat reverse shells — requires argv[0] to be nc/ncat so that
	// shell scripts that mention nc in arguments don't trigger.
	{name: "nc-exec", arg0: "nc", keywords: []string{"-e"}},
	{name: "ncat-exec", arg0: "ncat", keywords: []string{"-e"}},
	// Bash TCP/UDP redirect — /dev/tcp appearing in bash's own argument
	// string is the canonical reverse-shell one-liner indicator.
	{name: "bash-tcp-redirect", arg0: "bash", keywords: []string{"/dev/tcp/"}},
	{name: "bash-udp-redirect", arg0: "bash", keywords: []string{"/dev/udp/"}},
	// Python reverse shells — python3 must be checked before python (substring).
	// arg0 scoping prevents "bash -c 'python3 -c socket.socket'" from matching.
	{name: "python3-socket", arg0: "python3", keywords: []string{"socket.socket"}},
	{name: "python-socket", arg0: "python", keywords: []string{"socket.socket"}},
	// Perl reverse shells
	{name: "perl-socket", arg0: "perl", keywords: []string{"-mio"}},
	// Socat reverse shells
	{name: "socat-exec", arg0: "socat", keywords: []string{"exec:"}},
	// Crypto miners — presence of the binary name is sufficient.
	{name: "xmrig", arg0: "xmrig", keywords: []string{}},
}

// ProcEventWatcher monitors container process exec events via the kernel's
// NETLINK_CONNECTOR/PROC_EVENTS interface. It subscribes to PROC_EVENT_EXEC
// and PROC_EVENT_UID notifications at the host level, filters them to the
// target container's cgroup, and emits ThreatEvents for suspicious activity
// (reverse shell patterns, privilege escalation to root).
//
// Because events arrive via a host-side netlink socket, an attacker inside the
// container cannot suppress or forge them.
type ProcEventWatcher struct {
	containerName string
	onThreat      func(ThreatEvent)
	onError       func(error)
}

// NewProcEventWatcher creates a ProcEventWatcher for the named container.
func NewProcEventWatcher(containerName string, onThreat func(ThreatEvent), onError func(error)) *ProcEventWatcher {
	return &ProcEventWatcher{
		containerName: containerName,
		onThreat:      onThreat,
		onError:       onError,
	}
}

// Run listens for process events until ctx is cancelled. Setup errors (cgroup
// resolution failures, NETLINK_CONNECTOR unavailability) are surfaced via
// onError and cause Run to return early.
func (pw *ProcEventWatcher) Run(ctx context.Context) {
	cgroupPath, err := GetCgroupPath(ctx, pw.containerName)
	if err != nil {
		if ctx.Err() == nil && pw.onError != nil {
			pw.onError(fmt.Errorf("procwatcher: could not resolve cgroup: %w", err))
		}
		return
	}

	const cgroupMount = "/sys/fs/cgroup"
	relCgroup := strings.TrimPrefix(cgroupPath, cgroupMount)
	if relCgroup == cgroupPath {
		if pw.onError != nil {
			pw.onError(fmt.Errorf("procwatcher: unexpected cgroup path format: %s", cgroupPath))
		}
		return
	}
	// Strip the trailing init.scope or init.service sub-cgroup so the prefix
	// matches all container processes, not only those under the init slice.
	// Guard against producing an empty relCgroup (would match every process).
	if idx := strings.LastIndex(relCgroup, "/"); idx > 0 {
		last := relCgroup[idx+1:]
		if strings.HasSuffix(last, ".scope") || strings.HasSuffix(last, ".service") {
			relCgroup = relCgroup[:idx]
		}
	}

	sock, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM, unix.NETLINK_CONNECTOR)
	if err != nil {
		if pw.onError != nil {
			pw.onError(fmt.Errorf("procwatcher: netlink socket: %w", err))
		}
		return
	}
	defer unix.Close(sock)

	bindAddr := &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: cnIdxProc}
	if err := unix.Bind(sock, bindAddr); err != nil {
		if pw.onError != nil {
			pw.onError(fmt.Errorf("procwatcher: bind: %w", err))
		}
		return
	}

	if err := procSubscribe(sock); err != nil {
		if pw.onError != nil {
			pw.onError(fmt.Errorf("procwatcher: subscribe: %w", err))
		}
		return
	}

	go func() {
		<-ctx.Done()
		unix.Close(sock)
	}()

	buf := make([]byte, 4096)
	for {
		n, err := unix.Read(sock, buf)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return // socket closed (ctx cancelled) or unrecoverable error
		}
		pw.processMessages(buf[:n], relCgroup)
	}
}

// procSubscribe sends the PROC_CN_MCAST_LISTEN message to enable proc events.
//
// Message layout (40 bytes total):
//
//	nlmsghdr (16) + cn_msg (20) + uint32 mcast-op (4)
func procSubscribe(sock int) error {
	const dataLen = 4 // sizeof(PROC_CN_MCAST_LISTEN)
	const msgLen = 16 + 20 + dataLen

	var buf [msgLen]byte

	// nlmsghdr at offset 0
	binary.LittleEndian.PutUint32(buf[0:], msgLen)
	binary.LittleEndian.PutUint16(buf[4:], unix.NLMSG_DONE)
	// flags, seq, pid left as zero

	// cn_msg at offset 16
	binary.LittleEndian.PutUint32(buf[16:], cnIdxProc)
	binary.LittleEndian.PutUint32(buf[20:], cnValProc)
	// seq, ack left as zero
	binary.LittleEndian.PutUint16(buf[32:], dataLen)
	// flags left as zero

	// PROC_CN_MCAST_LISTEN at offset 36
	binary.LittleEndian.PutUint32(buf[36:], procCnMcastListen)

	return unix.Sendto(sock, buf[:], 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
}

// processMessages parses one or more netlink messages from buf and dispatches
// matching proc events to the appropriate handler.
func (pw *ProcEventWatcher) processMessages(buf []byte, relCgroup string) {
	const nlHdrSize = 16 // sizeof(nlmsghdr)
	const cnMsgSize = 20 // sizeof(cn_msg)
	const evHdrSize = 16 // proc_event: what(4) + cpu(4) + timestamp(8)

	for len(buf) >= nlHdrSize {
		msgLen := int(binary.LittleEndian.Uint32(buf[0:]))
		if msgLen < nlHdrSize || msgLen > len(buf) {
			break
		}
		data := buf[nlHdrSize:msgLen]
		// Advance by NLMSG_ALIGN(msgLen) — round up to 4-byte boundary.
		aligned := (msgLen + 3) &^ 3
		if aligned >= len(buf) {
			buf = buf[len(buf):]
		} else {
			buf = buf[aligned:]
		}

		if len(data) < cnMsgSize+evHdrSize {
			continue
		}
		// cn_msg.len is at offset 16 within the cn_msg block.
		cnLen := int(binary.LittleEndian.Uint16(data[16:]))
		eventData := data[cnMsgSize:]
		if len(eventData) < cnLen || cnLen < evHdrSize {
			continue
		}
		eventData = eventData[:cnLen]
		payload := eventData[evHdrSize:] // after proc_event header (what+cpu+timestamp)

		what := binary.LittleEndian.Uint32(eventData[0:])
		switch what {
		case procEventExec:
			if len(payload) < 8 {
				continue
			}
			pid := int(binary.LittleEndian.Uint32(payload[0:]))
			pw.handleExec(pid, relCgroup)

		case procEventUID:
			if len(payload) < 16 {
				continue
			}
			pid := int(binary.LittleEndian.Uint32(payload[0:]))
			euid := binary.LittleEndian.Uint32(payload[12:])
			if euid == 0 {
				pw.handleUIDToRoot(pid, relCgroup)
			}
		}
	}
}

// handleExec checks whether the exec'd process belongs to the container and
// whether its cmdline matches a suspicious pattern.
func (pw *ProcEventWatcher) handleExec(pid int, relCgroup string) {
	if !procInContainerCgroup(pid, relCgroup) {
		return
	}
	cmd := procReadCmdline(pid)
	if cmd == "" {
		return
	}
	pattern := matchSuspiciousExec(cmd)
	if pattern == "" {
		return
	}
	ev := ThreatEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Level:     ThreatLevelHigh,
		Category:  "proc_event",
		Title:     "Suspicious process execution detected",
		Description: fmt.Sprintf("Process (PID %d) matched suspicious exec pattern '%s': %s",
			pid, pattern, truncateToRunes(cmd, 200)),
		Evidence: Evidence{
			ProcEvent: &ProcEventThreat{
				PID:     pid,
				Command: truncateToRunes(cmd, 300),
				Pattern: pattern,
			},
		},
		Action: "pending",
	}
	if pw.onThreat != nil {
		pw.onThreat(ev)
	}
}

// handleUIDToRoot emits a threat when a container process elevates its
// effective UID to 0 (root).
func (pw *ProcEventWatcher) handleUIDToRoot(pid int, relCgroup string) {
	if !procInContainerCgroup(pid, relCgroup) {
		return
	}
	cmd := procReadCmdline(pid)
	ev := ThreatEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Level:     ThreatLevelHigh,
		Category:  "proc_event",
		Title:     "Process privilege escalation to root detected",
		Description: fmt.Sprintf("Process (PID %d) changed effective UID to root: %s",
			pid, truncateToRunes(cmd, 200)),
		Evidence: Evidence{
			ProcEvent: &ProcEventThreat{
				PID:     pid,
				Command: truncateToRunes(cmd, 300),
				Pattern: "uid-to-root",
			},
		},
		Action: "pending",
	}
	if pw.onThreat != nil {
		pw.onThreat(ev)
	}
}

// procInContainerCgroup returns true if /proc/<pid>/cgroup shows membership in
// the container's cgroup subtree (relCgroup is relative to /sys/fs/cgroup).
// It delegates to processBelongsToContainerCgroup for boundary-safe matching,
// preventing false positives when container names share a common prefix
// (e.g. "coi-1" must not match "coi-11").
func procInContainerCgroup(pid int, relCgroup string) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false
	}
	return processBelongsToContainerCgroup(string(data), relCgroup)
}

// procReadCmdline reads /proc/<pid>/cmdline, replacing NUL delimiters with
// spaces, and returns the result. Returns "" if the file is unreadable (process
// may have already exited).
func procReadCmdline(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return ""
	}
	return strings.TrimRight(strings.ReplaceAll(string(data), "\x00", " "), " ")
}

// matchSuspiciousExec returns the name of the first matching execPattern for
// the given command, or "" if none match.
func matchSuspiciousExec(cmd string) string {
	if cmd == "" {
		return ""
	}
	// Extract argv[0] (first space-delimited token) then its basename.
	arg0 := cmd
	if idx := strings.Index(cmd, " "); idx >= 0 {
		arg0 = cmd[:idx]
	}
	arg0Base := strings.ToLower(arg0[strings.LastIndex(arg0, "/")+1:])

	lower := strings.ToLower(cmd)
	for _, p := range execPatterns {
		if p.arg0 != "" && !strings.HasPrefix(arg0Base, p.arg0) {
			continue
		}
		matched := true
		for _, kw := range p.keywords {
			if !strings.Contains(lower, kw) {
				matched = false
				break
			}
		}
		if matched {
			return p.name
		}
	}
	return ""
}
