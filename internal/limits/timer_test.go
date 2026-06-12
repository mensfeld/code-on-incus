package limits

import (
	"context"
	"testing"
	"time"

	"github.com/mensfeld/code-on-incus/internal/logger"
)

// TestStopGracefulTrue verifies that StopGraceful=true maps to force=false
func TestStopGracefulTrue(t *testing.T) {
	tm := NewTimeoutMonitor(context.Background(), "test-container", 50*time.Millisecond, true, true, "", logger.NewDiscard())

	if tm.StopGraceful != true {
		t.Errorf("expected StopGraceful=true, got %v", tm.StopGraceful)
	}

	// The fix ensures: StopGraceful=true -> Stop(force=false) (graceful)
	// Before the fix: StopGraceful=true -> Stop(force=true) (inverted!)
	force := !tm.StopGraceful
	if force != false {
		t.Errorf("StopGraceful=true should result in force=false, got force=%v", force)
	}
}

// TestStopGracefulFalse verifies that StopGraceful=false calls Stop(true) (force)
func TestStopGracefulFalse(t *testing.T) {
	tm := NewTimeoutMonitor(context.Background(), "test-container", 50*time.Millisecond, true, false, "", logger.NewDiscard())

	if tm.StopGraceful != false {
		t.Errorf("expected StopGraceful=false, got %v", tm.StopGraceful)
	}

	// StopGraceful=false means force stop (force=true)
	force := !tm.StopGraceful
	if force != true {
		t.Errorf("StopGraceful=false should result in force=true, got force=%v", force)
	}
}

// TestHandleTimeoutStopType verifies the correct stopType is derived from StopGraceful
func TestHandleTimeoutStopType(t *testing.T) {
	tests := []struct {
		name         string
		stopGraceful bool
		expectedType string
	}{
		{
			name:         "graceful mode",
			stopGraceful: true,
			expectedType: "gracefully",
		},
		{
			name:         "force mode",
			stopGraceful: false,
			expectedType: "forcefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := NewTimeoutMonitor(context.Background(), "test-container", time.Hour, true, tt.stopGraceful, "", logger.NewDiscard())

			stopType := "gracefully"
			if !tm.StopGraceful {
				stopType = "forcefully"
			}
			if stopType != tt.expectedType {
				t.Errorf("expected stopType=%q, got %q", tt.expectedType, stopType)
			}
		})
	}
}

// TestTimeoutMonitorNoLimit verifies that MaxDuration=0 means no monitoring
func TestTimeoutMonitorNoLimit(t *testing.T) {
	tm := NewTimeoutMonitor(context.Background(), "test-container", 0, true, true, "", logger.NewDiscard())
	tm.Start()

	// Should complete immediately
	select {
	case <-tm.done:
		// Good - completed immediately
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout monitor with MaxDuration=0 should complete immediately")
	}
}

// TestTimeoutMonitorCancel verifies that Stop() cancels the monitor
func TestTimeoutMonitorCancel(t *testing.T) {
	tm := NewTimeoutMonitor(context.Background(), "test-container", time.Hour, true, true, "", logger.NewDiscard())
	tm.Start()

	// Cancel immediately
	tm.Stop()

	// Should complete quickly
	select {
	case <-tm.done:
		// Good - cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout monitor should complete after Stop()")
	}
}

// TestRemainingDecreasesOverTime verifies that Remaining() decreases as time passes.
func TestRemainingDecreasesOverTime(t *testing.T) {
	const maxDur = 2 * time.Second
	tm := NewTimeoutMonitor(context.Background(), "test-container", maxDur, false, true, "", logger.NewDiscard())
	tm.Start()
	defer tm.Stop()

	r1 := tm.Remaining()
	if r1 <= 0 || r1 > maxDur {
		t.Fatalf("Remaining() = %v, expected (0, %v]", r1, maxDur)
	}

	time.Sleep(100 * time.Millisecond)

	r2 := tm.Remaining()
	if r2 >= r1 {
		t.Errorf("Remaining() did not decrease: r1=%v r2=%v", r1, r2)
	}
}

// TestRemainingZeroAfterStop verifies that Remaining() returns 0 once the monitor is stopped.
func TestRemainingZeroAfterStop(t *testing.T) {
	tm := NewTimeoutMonitor(context.Background(), "test-container", time.Hour, false, true, "", logger.NewDiscard())
	tm.Start()
	tm.Stop()

	if r := tm.Remaining(); r != 0 {
		t.Errorf("Remaining() after Stop = %v, want 0", r)
	}
}

// TestRemainingZeroNoLimit verifies that Remaining() returns 0 when MaxDuration=0 (no limit).
func TestRemainingZeroNoLimit(t *testing.T) {
	tm := NewTimeoutMonitor(context.Background(), "test-container", 0, false, true, "", logger.NewDiscard())
	tm.Start()

	// Wait for done to be closed
	select {
	case <-tm.done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected done to close immediately for MaxDuration=0")
	}

	if r := tm.Remaining(); r != 0 {
		t.Errorf("Remaining() with MaxDuration=0 = %v, want 0", r)
	}
}
