package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAuthLogLine_FailedPassword(t *testing.T) {
	line := "Jun  5 12:00:00 host sshd[1234]: Failed password for invalid user attacker from 1.2.3.4 port 22222 ssh2"
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Level != ThreatLevelWarning {
		t.Errorf("level = %s, want warning", ev.Level)
	}
	if ev.Category != "auth" {
		t.Errorf("category = %s, want auth", ev.Category)
	}
	if ev.Evidence.AuthLog == nil {
		t.Fatal("AuthLog evidence is nil")
	}
	if ev.Evidence.AuthLog.Pattern != "ssh_failed_password" {
		t.Errorf("pattern = %s, want ssh_failed_password", ev.Evidence.AuthLog.Pattern)
	}
	if ev.Evidence.AuthLog.LogFile != "auth.log" {
		t.Errorf("log_file = %s, want auth.log", ev.Evidence.AuthLog.LogFile)
	}
}

func TestParseAuthLogLine_InvalidUser(t *testing.T) {
	line := "Jun  5 12:00:01 host sshd[1234]: Invalid user foo from 10.0.0.1 port 54321"
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Evidence.AuthLog.Pattern != "ssh_invalid_user" {
		t.Errorf("pattern = %s, want ssh_invalid_user", ev.Evidence.AuthLog.Pattern)
	}
	if ev.Level != ThreatLevelWarning {
		t.Errorf("level = %s, want warning", ev.Level)
	}
}

func TestParseAuthLogLine_PamAuthFailure(t *testing.T) {
	line := "Jun  5 12:00:02 host pam_unix(sshd:auth): authentication failure; logname= uid=0 euid=0 tty=ssh"
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Evidence.AuthLog.Pattern != "pam_auth_failure" {
		t.Errorf("pattern = %s, want pam_auth_failure", ev.Evidence.AuthLog.Pattern)
	}
}

func TestParseAuthLogLine_SudoNotAllowed(t *testing.T) {
	line := "Jun  5 12:00:03 host sudo: user : command not allowed ; TTY=pts/0 ; PWD=/home/user ; USER=root ; COMMAND=/bin/bash"
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Level != ThreatLevelHigh {
		t.Errorf("level = %s, want high", ev.Level)
	}
	if ev.Evidence.AuthLog.Pattern != "sudo_not_allowed" {
		t.Errorf("pattern = %s, want sudo_not_allowed", ev.Evidence.AuthLog.Pattern)
	}
}

func TestParseAuthLogLine_SudoNotInSudoers(t *testing.T) {
	line := "Jun  5 12:00:04 host sudo: hacker is not in the sudoers file. This incident will be reported."
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Level != ThreatLevelHigh {
		t.Errorf("level = %s, want high", ev.Level)
	}
	if ev.Evidence.AuthLog.Pattern != "sudo_not_in_sudoers" {
		t.Errorf("pattern = %s, want sudo_not_in_sudoers", ev.Evidence.AuthLog.Pattern)
	}
}

func TestParseAuthLogLine_SuFailed(t *testing.T) {
	line := "Jun  5 12:00:05 host su[5678]: FAILED su for root by user"
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Level != ThreatLevelHigh {
		t.Errorf("level = %s, want high", ev.Level)
	}
	if ev.Evidence.AuthLog.Pattern != "su_failed" {
		t.Errorf("pattern = %s, want su_failed", ev.Evidence.AuthLog.Pattern)
	}
}

func TestParseAuthLogLine_RootSession(t *testing.T) {
	line := "Jun  5 12:00:06 host su[5678]: pam_unix(su:session): session opened for user root by user(uid=1000)"
	ev := parseAuthLogLine("auth.log", line)
	if ev == nil {
		t.Fatal("expected threat event, got nil")
	}
	if ev.Level != ThreatLevelWarning {
		t.Errorf("level = %s, want warning", ev.Level)
	}
	if ev.Evidence.AuthLog.Pattern != "root_session" {
		t.Errorf("pattern = %s, want root_session", ev.Evidence.AuthLog.Pattern)
	}
}

func TestParseAuthLogLine_Benign(t *testing.T) {
	benignLines := []string{
		"Jun  5 12:00:07 host sshd[1234]: Accepted publickey for code from 10.0.0.1 port 54321 ssh2",
		"Jun  5 12:00:08 host CRON[9876]: pam_unix(cron:session): session opened for user code by (uid=0)",
		"Jun  5 12:00:09 host systemd-logind[1]: New session 1 of user code.",
		"",
		"some random log line with no keywords",
	}
	for _, line := range benignLines {
		ev := parseAuthLogLine("auth.log", line)
		if ev != nil {
			t.Errorf("line %q: expected nil, got threat %s (pattern=%s)", line, ev.Title, ev.Evidence.AuthLog.Pattern)
		}
	}
}

func TestParseAuthLogLine_LongLineTruncated(t *testing.T) {
	long := "Failed password for "
	for len(long) < 350 {
		long += "x"
	}
	ev := parseAuthLogLine("auth.log", long)
	if ev == nil {
		t.Fatal("expected threat event for long line, got nil")
	}
	if len(ev.Evidence.AuthLog.Line) > 305 {
		t.Errorf("line not truncated: len=%d", len(ev.Evidence.AuthLog.Line))
	}
}

func TestEvidenceString_AuthLog(t *testing.T) {
	e := Evidence{AuthLog: &AuthLogThreat{LogFile: "auth.log", Pattern: "ssh_failed_password"}}
	s := e.String()
	if s != "auth:auth.log:ssh_failed_password" {
		t.Errorf("Evidence.String() = %q, want auth:auth.log:ssh_failed_password", s)
	}
}

func TestReadFileChunk_ReadsFromBeginning(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test.log")
	content := "line one\nline two\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, newOffset, err := readFileChunk(f, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != content {
		t.Errorf("want %q got %q", content, string(data))
	}
	if newOffset != int64(len(content)) {
		t.Errorf("want offset %d got %d", len(content), newOffset)
	}
}

func TestReadFileChunk_RespectsOffset(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test.log")
	first := "already read\n"
	second := "new line\n"
	if err := os.WriteFile(f, []byte(first+second), 0o644); err != nil {
		t.Fatal(err)
	}

	data, newOffset, err := readFileChunk(f, int64(len(first)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != second {
		t.Errorf("want %q got %q", second, string(data))
	}
	if newOffset != int64(len(first)+len(second)) {
		t.Errorf("want offset %d got %d", len(first)+len(second), newOffset)
	}
}

func TestReadFileChunk_NoNewContent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test.log")
	content := "existing\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	data, newOffset, err := readFileChunk(f, int64(len(content)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("want nil data when no new content, got %q", string(data))
	}
	if newOffset != int64(len(content)) {
		t.Errorf("offset should not change, want %d got %d", len(content), newOffset)
	}
}

func TestReadFileChunk_FileNotFound(t *testing.T) {
	_, _, err := readFileChunk("/nonexistent/path/file.log", 0)
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}
