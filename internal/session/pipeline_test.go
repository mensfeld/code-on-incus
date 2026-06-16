package session

import (
	"context"
	"errors"
	"testing"
)

func TestPipeline_RunsPhaseInOrder(t *testing.T) {
	var order []string
	p := &Pipeline{}

	p.Run(context.Background(),
		PhaseFunc{"a", func(_ context.Context) (Teardown, error) { order = append(order, "a"); return nil, nil }},
		PhaseFunc{"b", func(_ context.Context) (Teardown, error) { order = append(order, "b"); return nil, nil }},
		PhaseFunc{"c", func(_ context.Context) (Teardown, error) { order = append(order, "c"); return nil, nil }},
	)

	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("unexpected phase order: %v", order)
	}
}

func TestPipeline_StopsOnError(t *testing.T) {
	var ran []string
	p := &Pipeline{}
	sentinel := errors.New("boom")

	err := p.Run(context.Background(),
		PhaseFunc{"ok", func(_ context.Context) (Teardown, error) { ran = append(ran, "ok"); return nil, nil }},
		PhaseFunc{"fail", func(_ context.Context) (Teardown, error) { return nil, sentinel }},
		PhaseFunc{"skip", func(_ context.Context) (Teardown, error) { ran = append(ran, "skip"); return nil, nil }},
	)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("unexpected error: %v", err)
	}
	if len(ran) != 1 || ran[0] != "ok" {
		t.Errorf("expected only 'ok' to run before error, got: %v", ran)
	}
}

func TestPipeline_TeardownRunsLIFO(t *testing.T) {
	var order []string
	p := &Pipeline{}

	p.Run(context.Background(),
		PhaseFunc{"a", func(_ context.Context) (Teardown, error) {
			return func() { order = append(order, "td-a") }, nil
		}},
		PhaseFunc{"b", func(_ context.Context) (Teardown, error) {
			return func() { order = append(order, "td-b") }, nil
		}},
		PhaseFunc{"c", func(_ context.Context) (Teardown, error) {
			return func() { order = append(order, "td-c") }, nil
		}},
	)

	p.Teardown()

	if len(order) != 3 || order[0] != "td-c" || order[1] != "td-b" || order[2] != "td-a" {
		t.Errorf("expected LIFO order [td-c, td-b, td-a], got: %v", order)
	}
}

func TestPipeline_TeardownIdempotent(t *testing.T) {
	var count int
	p := &Pipeline{}

	p.Run(context.Background(),
		PhaseFunc{"x", func(_ context.Context) (Teardown, error) {
			return func() { count++ }, nil
		}},
	)

	p.Teardown()
	p.Teardown()
	p.Teardown()

	if count != 1 {
		t.Errorf("teardown ran %d times, expected 1", count)
	}
}

func TestPipeline_CancelledContextSkipsPhase(t *testing.T) {
	var ran []string
	p := &Pipeline{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run

	err := p.Run(ctx,
		PhaseFunc{"should-skip", func(_ context.Context) (Teardown, error) {
			ran = append(ran, "ran")
			return nil, nil
		}},
	)

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if len(ran) != 0 {
		t.Errorf("phase ran despite cancelled context: %v", ran)
	}
}

func TestPipeline_TeardownRunsForCompletedPhasesOnError(t *testing.T) {
	var torn []string
	p := &Pipeline{}

	p.Run(context.Background(),
		PhaseFunc{"a", func(_ context.Context) (Teardown, error) {
			return func() { torn = append(torn, "a") }, nil
		}},
		PhaseFunc{"fail", func(_ context.Context) (Teardown, error) {
			return nil, errors.New("oops")
		}},
		PhaseFunc{"c", func(_ context.Context) (Teardown, error) {
			return func() { torn = append(torn, "c") }, nil
		}},
	)
	p.Teardown()

	// Only "a" registered a teardown; "c" never ran
	if len(torn) != 1 || torn[0] != "a" {
		t.Errorf("expected [a], got: %v", torn)
	}
}
