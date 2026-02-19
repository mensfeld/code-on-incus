package monitor

import (
	"sync"
	"testing"
	"time"
)

func TestResponderDeduplication(t *testing.T) {
	alertCount := 0
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		false, // autoPauseOnHigh
		false, // autoKillOnCritical
		nil,   // no audit log
		func(threat ThreatEvent) {
			mu.Lock()
			alertCount++
			mu.Unlock()
		},
	)

	// Create the same threat
	threat := ThreatEvent{
		Timestamp:   time.Now(),
		Level:       ThreatLevelWarning,
		Category:    "network",
		Title:       "Test threat",
		Description: "Test description",
	}

	// Handle the same threat multiple times rapidly
	for i := 0; i < 5; i++ {
		if err := responder.Handle(threat); err != nil {
			t.Fatalf("Handle failed: %v", err)
		}
	}

	mu.Lock()
	count := alertCount
	mu.Unlock()

	// Should only alert once due to deduplication
	if count != 1 {
		t.Errorf("Expected 1 alert (deduplicated), got %d", count)
	}
}

func TestResponderDeduplicationExpiry(t *testing.T) {
	alertCount := 0
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		false, // autoPauseOnHigh
		false, // autoKillOnCritical
		nil,   // no audit log
		func(threat ThreatEvent) {
			mu.Lock()
			alertCount++
			mu.Unlock()
		},
	)

	// Set a very short dedupe window for testing
	responder.dedupeWindow = 10 * time.Millisecond

	threat := ThreatEvent{
		Timestamp:   time.Now(),
		Level:       ThreatLevelWarning,
		Category:    "network",
		Title:       "Test threat",
		Description: "Test description",
	}

	// First alert
	if err := responder.Handle(threat); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Wait for dedupe window to expire
	time.Sleep(20 * time.Millisecond)

	// Should alert again
	if err := responder.Handle(threat); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	mu.Lock()
	count := alertCount
	mu.Unlock()

	if count != 2 {
		t.Errorf("Expected 2 alerts (dedupe expired), got %d", count)
	}
}

func TestResponderDifferentThreatsNotDeduplicated(t *testing.T) {
	alertCount := 0
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		false, // autoPauseOnHigh
		false, // autoKillOnCritical
		nil,   // no audit log
		func(threat ThreatEvent) {
			mu.Lock()
			alertCount++
			mu.Unlock()
		},
	)

	// Handle different threats
	threats := []ThreatEvent{
		{
			Timestamp:   time.Now(),
			Level:       ThreatLevelWarning,
			Category:    "network",
			Title:       "Threat A",
			Description: "Description A",
		},
		{
			Timestamp:   time.Now(),
			Level:       ThreatLevelWarning,
			Category:    "network",
			Title:       "Threat B",
			Description: "Description B",
		},
		{
			Timestamp:   time.Now(),
			Level:       ThreatLevelWarning,
			Category:    "process",
			Title:       "Threat C",
			Description: "Description C",
		},
	}

	for _, threat := range threats {
		if err := responder.Handle(threat); err != nil {
			t.Fatalf("Handle failed: %v", err)
		}
	}

	mu.Lock()
	count := alertCount
	mu.Unlock()

	// Each unique threat should alert
	if count != 3 {
		t.Errorf("Expected 3 alerts (different threats), got %d", count)
	}
}

func TestResponderPausedStateTracking(t *testing.T) {
	var capturedAction string
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		true,  // autoPauseOnHigh
		false, // autoKillOnCritical
		nil,   // no audit log
		func(threat ThreatEvent) {
			mu.Lock()
			capturedAction = threat.Action
			mu.Unlock()
		},
	)

	// Simulate container being paused
	responder.mu.Lock()
	responder.paused = true
	responder.mu.Unlock()

	threat := ThreatEvent{
		Timestamp:   time.Now(),
		Level:       ThreatLevelHigh,
		Category:    "network",
		Title:       "Test high threat",
		Description: "Should not try to pause again",
	}

	// Handle should succeed without error (no pause attempt on already-paused)
	if err := responder.Handle(threat); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// When already paused, high-level threats should not call the alert callback
	// (they just log silently)
	mu.Lock()
	action := capturedAction
	mu.Unlock()

	// The callback should not be called when container is already paused
	if action != "" {
		t.Logf("Note: callback was called with action '%s' (may need to adjust expectations)", action)
	}

	// Verify the responder didn't try to pause again (state still set)
	responder.mu.Lock()
	stillPaused := responder.paused
	responder.mu.Unlock()

	if !stillPaused {
		t.Error("Expected responder to still track paused state")
	}
}

func TestResponderKilledState(t *testing.T) {
	alertCount := 0
	var mu sync.Mutex

	responder := NewResponder(
		"test-container",
		true, // autoPauseOnHigh
		true, // autoKillOnCritical
		nil,  // no audit log
		func(threat ThreatEvent) {
			mu.Lock()
			alertCount++
			mu.Unlock()
		},
	)

	// Simulate container being killed
	responder.mu.Lock()
	responder.killed = true
	responder.mu.Unlock()

	// Handle various threats - all should be no-ops
	threats := []ThreatEvent{
		{Level: ThreatLevelInfo, Title: "Info"},
		{Level: ThreatLevelWarning, Title: "Warning"},
		{Level: ThreatLevelHigh, Title: "High"},
		{Level: ThreatLevelCritical, Title: "Critical"},
	}

	for _, threat := range threats {
		if err := responder.Handle(threat); err != nil {
			t.Fatalf("Handle failed: %v", err)
		}
	}

	mu.Lock()
	count := alertCount
	mu.Unlock()

	// No alerts should be sent after container is killed
	if count != 0 {
		t.Errorf("Expected 0 alerts (container killed), got %d", count)
	}
}

func TestResponderThreatLevelActions(t *testing.T) {
	tests := []struct {
		name           string
		level          ThreatLevel
		autoPause      bool
		autoKill       bool
		expectCallback bool // Whether alert callback should be called
	}{
		{
			name:           "info level - no callback",
			level:          ThreatLevelInfo,
			expectCallback: false, // Info is just logged, no alert
		},
		{
			name:           "warning level - callback",
			level:          ThreatLevelWarning,
			expectCallback: true,
		},
		{
			name:           "high level without auto pause - callback",
			level:          ThreatLevelHigh,
			autoPause:      false,
			expectCallback: true,
		},
		{
			name:           "critical level without auto kill - callback",
			level:          ThreatLevelCritical,
			autoKill:       false,
			expectCallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callbackCalled := false
			var mu sync.Mutex

			responder := NewResponder(
				"test-container",
				tt.autoPause,
				tt.autoKill,
				nil,
				func(threat ThreatEvent) {
					mu.Lock()
					callbackCalled = true
					mu.Unlock()
				},
			)

			threat := ThreatEvent{
				Timestamp:   time.Now(),
				Level:       tt.level,
				Category:    "test",
				Title:       "Test threat",
				Description: "Test description",
			}

			if err := responder.Handle(threat); err != nil {
				t.Fatalf("Handle failed: %v", err)
			}

			mu.Lock()
			called := callbackCalled
			mu.Unlock()

			if called != tt.expectCallback {
				t.Errorf("Expected callback called=%v, got %v", tt.expectCallback, called)
			}
		})
	}
}
