//go:build linux

package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}

// TestAgentScript_AuditdLineProducesFileEvent runs agent.sh against a fixture
// audit.log file (no real auditd needed) and verifies a PATH record turns
// into a type=file JSON line.
//
// Skipped on non-Linux because the script depends on POSIX `tail -F`, `awk`,
// and `sed` behaviour that varies on macOS BSD tools.
func TestAgentScript_AuditdLineProducesFileEvent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("agent.sh is targeted at Linux containers")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh in PATH")
	}

	tmp := t.TempDir()

	// 1. Write the embedded agent to a file we can run.
	agentPath := filepath.Join(tmp, "agent.sh")
	if err := os.WriteFile(agentPath, agentScript, 0o700); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	// 2. Re-point the agent's audit log path to a fixture under our temp dir.
	//    The simplest way is to sed the path before running — keeps the
	//    in-tree script untouched.
	patched := strings.NewReplacer(
		"/var/log/audit/audit.log", filepath.Join(tmp, "audit.log"),
		"/var/log/syslog", filepath.Join(tmp, "syslog-disabled"),
		"/var/log/auth.log", filepath.Join(tmp, "auth-disabled"),
		"/var/log/messages", filepath.Join(tmp, "messages-disabled"),
	).Replace(string(agentScript))
	if err := os.WriteFile(agentPath, []byte(patched), 0o700); err != nil {
		t.Fatalf("write patched agent: %v", err)
	}

	// 3. Pre-create the fixture file so tail can attach.
	auditLog := filepath.Join(tmp, "audit.log")
	if err := os.WriteFile(auditLog, []byte{}, 0o644); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	// 4. Run the agent with very long sleep intervals so we don't get noise
	//    from the ss/ps loops.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", agentPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(),
		"COI_AUDIT_NET_INTERVAL=3600",
		"COI_AUDIT_PROC_INTERVAL=3600",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		killProcessGroup(cmd)
		_ = cmd.Wait()
	}()

	// 5. Append an auditd-style PATH record into the fixture.
	go func() {
		time.Sleep(500 * time.Millisecond)
		f, err := os.OpenFile(auditLog, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Logf("open append: %v", err)
			return
		}
		defer f.Close()
		_, _ = f.WriteString(`type=PATH msg=audit(1714928461.123:42): item=0 name="/etc/shadow" inode=0 dev=00:00 mode=0100640 ouid=0 ogid=0 rdev=00:00 nametype=NORMAL` + "\n")
	}()

	// 6. Read agent stdout for a few seconds. To avoid blocking on
	//    Scanner.Scan() past the deadline, we close the agent's stdout via
	//    a separate goroutine that kills the process when the deadline
	//    fires.
	deadline := time.Now().Add(5 * time.Second)
	bailTimer := time.AfterFunc(time.Until(deadline), func() {
		killProcessGroup(cmd)
	})
	defer bailTimer.Stop()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var foundFile, foundStart bool
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		ev, perr := ParseLine(line)
		if perr != nil {
			continue
		}
		if ev == nil {
			continue
		}
		if ev.Type == TypeAudit && ev.Msg == "collector.started" {
			foundStart = true
		}
		if ev.Type == TypeFile && ev.Path == "/etc/shadow" {
			foundFile = true
			killProcessGroup(cmd)
			break
		}
	}
	if !foundStart {
		t.Errorf("collector.started bootstrap event not seen")
	}
	if !foundFile {
		t.Errorf("expected type=file event for /etc/shadow, did not see it")
	}
}

// TestAgentScript_EmitsHeartbeat runs agent.sh with a short heartbeat
// interval and verifies that at least one type=heartbeat event with a valid
// seq lands on stdout. This locks the wire shape the host-side watcher
// depends on.
func TestAgentScript_EmitsHeartbeat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("agent.sh is targeted at Linux containers")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh in PATH")
	}

	tmp := t.TempDir()
	agentPath := filepath.Join(tmp, "agent.sh")
	patched := strings.NewReplacer(
		"/var/log/audit/audit.log", filepath.Join(tmp, "noop1"),
		"/var/log/syslog", filepath.Join(tmp, "noop2"),
		"/var/log/auth.log", filepath.Join(tmp, "noop3"),
		"/var/log/messages", filepath.Join(tmp, "noop4"),
	).Replace(string(agentScript))
	if err := os.WriteFile(agentPath, []byte(patched), 0o700); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", agentPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(),
		"COI_AUDIT_NET_INTERVAL=3600",
		"COI_AUDIT_PROC_INTERVAL=3600",
		"COI_AUDIT_HEARTBEAT_INTERVAL=1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		killProcessGroup(cmd)
		_ = cmd.Wait()
	}()

	bailTimer := time.AfterFunc(4*time.Second, func() {
		killProcessGroup(cmd)
	})
	defer bailTimer.Stop()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var hbCount int
	var sawSeqZero bool
	var sawSeqOne bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, perr := ParseLine(line)
		if perr != nil || ev == nil {
			continue
		}
		if ev.Type != TypeHeartbeat {
			continue
		}
		hbCount++
		if ev.Seq == 0 {
			sawSeqZero = true
		}
		if ev.Seq == 1 {
			sawSeqOne = true
		}
		if hbCount >= 2 {
			killProcessGroup(cmd)
			break
		}
	}
	if hbCount < 2 {
		t.Errorf("expected at least 2 heartbeats with COI_AUDIT_HEARTBEAT_INTERVAL=1, got %d", hbCount)
	}
	if !sawSeqZero {
		t.Errorf("first heartbeat should carry seq=0")
	}
	if !sawSeqOne {
		t.Errorf("second heartbeat should carry seq=1")
	}
}

// TestAgentScript_EmitsValidJSON makes sure every line on stdout from a quick
// startup round-trip parses back to a valid JSON object.
func TestAgentScript_EmitsValidJSON(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	tmp := t.TempDir()
	agentPath := filepath.Join(tmp, "agent.sh")
	// Patch all log paths to nonexistent so the auditd/syslog branches no-op
	// quickly and we just get the bootstrap event + ss/ps snapshots.
	patched := strings.NewReplacer(
		"/var/log/audit/audit.log", filepath.Join(tmp, "noop1"),
		"/var/log/syslog", filepath.Join(tmp, "noop2"),
		"/var/log/auth.log", filepath.Join(tmp, "noop3"),
		"/var/log/messages", filepath.Join(tmp, "noop4"),
	).Replace(string(agentScript))
	if err := os.WriteFile(agentPath, []byte(patched), 0o700); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", agentPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(),
		"COI_AUDIT_NET_INTERVAL=1",
		"COI_AUDIT_PROC_INTERVAL=1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		killProcessGroup(cmd)
		_ = cmd.Wait()
	}()

	bailTimer := time.AfterFunc(2500*time.Millisecond, func() {
		killProcessGroup(cmd)
	})
	defer bailTimer.Stop()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Errorf("invalid JSON on stdout: %q (%v)", line, err)
			continue
		}
		if _, ok := raw["ts"].(string); !ok {
			t.Errorf("ts missing/non-string: %q", line)
		}
		if _, ok := raw["type"].(string); !ok {
			t.Errorf("type missing/non-string: %q", line)
		}
		count++
	}
	if count == 0 {
		t.Errorf("expected at least the bootstrap event, got 0 JSON lines")
	}
}
