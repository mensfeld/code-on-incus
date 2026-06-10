package session

import (
	"context"
	"fmt"
	"sync"
)

// Teardown is a cleanup function registered by a Phase. Teardowns run in LIFO
// order when Pipeline.Teardown is called.
type Teardown func()

// Phase is a single step in a session pipeline. Run performs the step's work
// and returns an optional Teardown that reverses its effects. If Run returns
// an error, the pipeline stops immediately; already-registered teardowns are
// called by the pipeline's defer (not by Run itself).
type Phase interface {
	Name() string
	Run(ctx context.Context) (Teardown, error)
}

// PhaseFunc adapts an inline function to the Phase interface.
type PhaseFunc struct {
	PhaseName string
	RunFn     func(ctx context.Context) (Teardown, error)
}

func (p PhaseFunc) Name() string                              { return p.PhaseName }
func (p PhaseFunc) Run(ctx context.Context) (Teardown, error) { return p.RunFn(ctx) }

// Pipeline executes Phases in order and tracks their Teardowns for LIFO
// cleanup. Teardown is idempotent: subsequent calls after the first are
// no-ops. It is safe to call Teardown concurrently with Run.
type Pipeline struct {
	mu           sync.Mutex
	teardowns    []Teardown
	teardownOnce sync.Once
}

// Run executes each phase in order. If ctx is cancelled before a phase starts,
// Run stops and returns ctx.Err(). Teardowns registered so far are NOT called
// by Run — the caller must call Pipeline.Teardown (typically via defer) to
// ensure cleanup happens on every exit path.
func (p *Pipeline) Run(ctx context.Context, phases ...Phase) error {
	for _, phase := range phases {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		td, err := phase.Run(ctx)
		if td != nil {
			p.mu.Lock()
			p.teardowns = append(p.teardowns, td)
			p.mu.Unlock()
		}
		if err != nil {
			return fmt.Errorf("%s: %w", phase.Name(), err)
		}
	}
	return nil
}

// AddTeardown registers a teardown function directly, outside of a phase.
// Useful when the caller accumulates teardowns incrementally (e.g. each step
// adds its own cleanup before the next step runs). Nil teardowns are silently
// ignored, matching the behaviour of Pipeline.Run.
func (p *Pipeline) AddTeardown(td Teardown) {
	if td == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.teardowns = append(p.teardowns, td)
}

// Teardown invokes all registered teardowns in reverse order (LIFO). It is
// safe to call from multiple goroutines simultaneously; only the first call
// executes — subsequent calls are no-ops. Callers must not call Teardown
// concurrently with Run: teardowns registered after Teardown's snapshot is
// taken will not be executed.
func (p *Pipeline) Teardown() {
	p.teardownOnce.Do(func() {
		p.mu.Lock()
		tds := make([]Teardown, len(p.teardowns))
		copy(tds, p.teardowns)
		p.mu.Unlock()
		for i := len(tds) - 1; i >= 0; i-- {
			tds[i]()
		}
	})
}
