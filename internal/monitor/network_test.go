package monitor

import (
	"context"
	"testing"
)

// fakeIncusInfo simulates the output of `incus info <container>` for PID parsing tests.
const fakeIncusInfoOutput = `Name: coi-test-1
Status: Running
Type: container
Architecture: x86_64
PID: 12345
Created: 2024/01/01 00:00 UTC`

const fakeIncusInfoOutputLowercase = `Name: coi-test-2
Status: Running
Pid: 99
Created: 2024/01/01 00:00 UTC`

const fakeIncusInfoNoPID = `Name: coi-test-3
Status: Stopped
Created: 2024/01/01 00:00 UTC`

func TestParseContainerInitPID_FromIncusInfoOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantPID int
		wantErr bool
	}{
		{"uppercase PID key", fakeIncusInfoOutput, 12345, false},
		{"lowercase Pid key", fakeIncusInfoOutputLowercase, 99, false},
		{"no PID in output", fakeIncusInfoNoPID, 0, true},
		{"empty output", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid, err := parseInitPIDFromIncusInfo(tt.output)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v got err=%v", tt.wantErr, err)
			}
			if pid != tt.wantPID {
				t.Errorf("want PID %d got %d", tt.wantPID, pid)
			}
		})
	}
}

func TestParseContainerInitPID_StrictPrefix(t *testing.T) {
	// A line containing "PID:" somewhere in the middle must NOT match.
	tricky := "  Description: has a PID: 0 in the middle\nPID: 777\n"
	pid, err := parseInitPIDFromIncusInfo(tricky)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 777 {
		t.Errorf("want 777 got %d", pid)
	}
}

func TestParseConnections_FallbackEmptyContainerIP(t *testing.T) {
	// When PID resolution fails and containerIP is empty the fallback must
	// return nil rather than all host connections.
	ctx := context.Background()
	// Use a container name that will never resolve via incus info.
	conns, err := parseConnections(ctx, "nonexistent-container-xyz", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conns) != 0 {
		t.Errorf("expected empty result when containerIP is empty, got %d connections", len(conns))
	}
}

func TestParseHexIP_LittleEndian(t *testing.T) {
	// Linux /proc/net/tcp stores IPs in little-endian byte order.
	// 10.0.0.2 is encoded as 0200000A (bytes reversed).
	ip, err := parseHexIP("0200000A")
	if err != nil {
		t.Fatalf("parseHexIP error: %v", err)
	}
	if ip != "10.0.0.2" {
		t.Errorf("want 10.0.0.2 got %s", ip)
	}
}
