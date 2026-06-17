package audit

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a deterministic time source for the watcher tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// transition records a single watcher callback invocation.
type transition struct {
	sessionID string
	status    WatcherStatus
	lastSeen  time.Time
}

// recorder captures transitions in order behind a mutex.
type recorder struct {
	mu  sync.Mutex
	out []transition
}

func (r *recorder) record(sessionID string, status WatcherStatus, lastSeen time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out = append(r.out, transition{sessionID, status, lastSeen})
}

func (r *recorder) snapshot() []transition {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]transition, len(r.out))
	copy(out, r.out)
	return out
}

func newTestWatcher(rec *recorder, clk *fakeClock, stale, check time.Duration) *HeartbeatWatcher {
	w := NewHeartbeatWatcher(rec.record, stale, check)
	w.now = clk.Now
	return w
}

// lastSeen reads a session's last-seen timestamp directly from the watcher's
// internal state (the exported accessor was removed as dead code).
func (w *HeartbeatWatcher) lastSeen(sessionID string) (time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.sessions[sessionID]
	if !ok || !st.seen {
		return time.Time{}, false
	}
	return st.lastSeen, true
}

// isStale reads a session's stale flag directly from the watcher's internal
// state (the exported accessor was removed as dead code).
func (w *HeartbeatWatcher) isStale(sessionID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.sessions[sessionID]
	if !ok {
		return false
	}
	return st.stale
}

func TestWatcher_FirstHeartbeatFiresFirstSeen(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	w.Observe("coi-a")

	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("want 1 transition, got %d", len(got))
	}
	if got[0].status != StatusFirstSeen {
		t.Errorf("want StatusFirstSeen, got %v", got[0].status)
	}
	if got[0].sessionID != "coi-a" {
		t.Errorf("want sessionID coi-a, got %q", got[0].sessionID)
	}
	if last, ok := w.lastSeen("coi-a"); !ok || !last.Equal(clk.Now()) {
		t.Errorf("lastSeen mismatch: %v ok=%v", last, ok)
	}
}

func TestWatcher_HeartbeatUpdatesLastSeen(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	w.Observe("coi-a")
	t0 := clk.Now()

	clk.Advance(15 * time.Second)
	w.Observe("coi-a")
	t1 := clk.Now()

	last, ok := w.lastSeen("coi-a")
	if !ok {
		t.Fatal("session not tracked")
	}
	if !last.Equal(t1) {
		t.Errorf("last-seen not updated: got %v, want %v (initial %v)", last, t1, t0)
	}
	// Only the first-seen transition should have fired so far; subsequent
	// in-window heartbeats are silent.
	if got := rec.snapshot(); len(got) != 1 {
		t.Errorf("want 1 transition, got %d", len(got))
	}
}

func TestWatcher_StaleFiresAfterThreshold(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	w.Observe("coi-a")
	rec.mu.Lock()
	rec.out = nil
	rec.mu.Unlock()

	// Inside the window: tick should not flip stale.
	clk.Advance(20 * time.Second)
	w.tick()
	if w.isStale("coi-a") {
		t.Fatal("session went stale before threshold")
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Errorf("unexpected transitions inside window: %+v", got)
	}

	// Past threshold: tick must flip stale and fire exactly once.
	clk.Advance(20 * time.Second)
	w.tick()
	if !w.isStale("coi-a") {
		t.Fatal("session did not go stale after threshold")
	}
	got := rec.snapshot()
	if len(got) != 1 || got[0].status != StatusStale {
		t.Fatalf("want one StatusStale, got %+v", got)
	}

	// Repeated ticks while still stale must not duplicate the transition.
	clk.Advance(60 * time.Second)
	w.tick()
	w.tick()
	if got := rec.snapshot(); len(got) != 1 {
		t.Errorf("stale transition fired more than once: %d", len(got))
	}
}

func TestWatcher_RecoveryFiresAlive(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	w.Observe("coi-a")
	clk.Advance(40 * time.Second)
	w.tick() // -> stale

	rec.mu.Lock()
	rec.out = nil
	rec.mu.Unlock()

	// Heartbeat resumes.
	clk.Advance(2 * time.Second)
	w.Observe("coi-a")

	if w.isStale("coi-a") {
		t.Fatal("session still marked stale after heartbeat resumed")
	}
	got := rec.snapshot()
	if len(got) != 1 || got[0].status != StatusAlive {
		t.Fatalf("want one StatusAlive, got %+v", got)
	}
}

func TestWatcher_MultipleSessionsTrackedIndependently(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	w.Observe("coi-a")
	w.Observe("coi-b")
	rec.mu.Lock()
	rec.out = nil
	rec.mu.Unlock()

	clk.Advance(40 * time.Second)
	w.Observe("coi-b") // b stays alive
	w.tick()           // a should go stale

	if !w.isStale("coi-a") {
		t.Error("coi-a should be stale")
	}
	if w.isStale("coi-b") {
		t.Error("coi-b should still be alive")
	}
	got := rec.snapshot()
	var staleCount int
	for _, tr := range got {
		if tr.status == StatusStale && tr.sessionID == "coi-a" {
			staleCount++
		}
		if tr.status == StatusStale && tr.sessionID == "coi-b" {
			t.Errorf("coi-b should not have gone stale: %+v", tr)
		}
	}
	if staleCount != 1 {
		t.Errorf("want exactly one stale transition for coi-a, got %d (all: %+v)", staleCount, got)
	}
}

func TestWatcher_EmptySessionIDIgnored(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	w.Observe("")
	if got := rec.snapshot(); len(got) != 0 {
		t.Errorf("empty sessionID should not fire: %+v", got)
	}
}

func TestCollector_FeedsHeartbeatToWatcher(t *testing.T) {
	clk := newFakeClock()
	rec := &recorder{}
	w := newTestWatcher(rec, clk, 35*time.Second, 5*time.Second)

	in := strings.Join([]string{
		`{"ts":"t","type":"heartbeat","seq":0,"sources":"auditd ss ps"}`,
		`{"ts":"t","type":"exec","pid":1,"comm":"sh"}`,
		`{"ts":"t","type":"heartbeat","seq":1,"sources":"auditd ss ps"}`,
	}, "\n")

	var out strings.Builder
	c := &Collector{
		SessionID: "coi-x",
		Container: "coi-x",
		Out:       &out,
		Watcher:   w,
	}
	if err := c.Run(context.Background(), strings.NewReader(in)); err != nil {
		t.Fatalf("collector: %v", err)
	}

	// Heartbeats should have flowed through (i.e. they're not filtered).
	if !strings.Contains(out.String(), `"type":"heartbeat"`) {
		t.Error("heartbeat events should be re-emitted on stdout")
	}
	// And the watcher should have observed exactly one first-seen transition
	// (subsequent heartbeats inside the window are silent).
	got := rec.snapshot()
	if len(got) != 1 || got[0].status != StatusFirstSeen || got[0].sessionID != "coi-x" {
		t.Errorf("want one StatusFirstSeen for coi-x, got %+v", got)
	}
}
