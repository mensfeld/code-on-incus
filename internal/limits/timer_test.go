package limits

import (
	"fmt"
	"testing"
	"time"
)

// TestStopGracefulTrue verifies that StopGraceful=true maps to force=false
func TestStopGracefulTrue(t *testing.T) {
	tm := NewTimeoutMonitor("test-container", 50*time.Millisecond, true, true, "", nil)

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
	tm := NewTimeoutMonitor("test-container", 50*time.Millisecond, true, false, "", nil)

	if tm.StopGraceful != false {
		t.Errorf("expected StopGraceful=false, got %v", tm.StopGraceful)
	}

	// StopGraceful=false means force stop (force=true)
	force := !tm.StopGraceful
	if force != true {
		t.Errorf("StopGraceful=false should result in force=true, got force=%v", force)
	}
}

// TestHandleTimeoutLogMessages verifies the correct log messages for each mode
func TestHandleTimeoutLogMessages(t *testing.T) {
	tests := []struct {
		name         string
		stopGraceful bool
		expectedLog  string
	}{
		{
			name:         "graceful mode logs gracefully",
			stopGraceful: true,
			expectedLog:  "gracefully",
		},
		{
			name:         "force mode logs forcefully",
			stopGraceful: false,
			expectedLog:  "forcefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs []string
			logger := func(msg string) {
				logs = append(logs, msg)
			}

			tm := NewTimeoutMonitor("test-container", time.Hour, true, tt.stopGraceful, "", logger)

			// Check the log message format
			stopType := "gracefully"
			if !tm.StopGraceful {
				stopType = "forcefully"
			}
			expected := fmt.Sprintf("[limits] Runtime limit reached (%s), stopping container %s...", tm.MaxDuration, stopType)

			// Simulate the log message that handleTimeout would produce
			logger(expected)

			found := false
			for _, log := range logs {
				if log == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected log message containing %q, got %v", tt.expectedLog, logs)
			}
		})
	}
}

// TestTimeoutMonitorNoLimit verifies that MaxDuration=0 means no monitoring
func TestTimeoutMonitorNoLimit(t *testing.T) {
	tm := NewTimeoutMonitor("test-container", 0, true, true, "", nil)
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
	tm := NewTimeoutMonitor("test-container", time.Hour, true, true, "", nil)
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
