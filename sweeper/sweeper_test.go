package sweeper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func staticSweep(items ...string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return items, nil }
}

// =============================================================================
// Pass Tests
// =============================================================================

func TestPass_HandlesEveryItem(t *testing.T) {
	var handled []string

	Pass(context.Background(), Loop[string]{
		Sweep: staticSweep("a", "b", "c"),
		Handle: func(_ context.Context, item string) bool {
			handled = append(handled, item)
			return true
		},
		Logger: discardLogger(),
	})

	if len(handled) != 3 {
		t.Errorf("handled %v, want all three", handled)
	}
}

// One item that cannot be settled must not cost the rest their pass.
func TestPass_ContinuesPastAFailingItem(t *testing.T) {
	var handled []string

	Pass(context.Background(), Loop[string]{
		Sweep: staticSweep("a", "b", "c"),
		Handle: func(_ context.Context, item string) bool {
			handled = append(handled, item)
			return item != "b"
		},
		Logger: discardLogger(),
	})

	if len(handled) != 3 {
		t.Errorf("handled %v, want the failure not to stop the pass", handled)
	}
}

// The deadline covers the sweep and every handle, not each call separately.
func TestPass_TimeoutBoundsTheWholePass(t *testing.T) {
	var sweepDeadline, handleDeadline time.Time

	Pass(context.Background(), Loop[string]{
		Sweep: func(ctx context.Context) ([]string, error) {
			sweepDeadline, _ = ctx.Deadline()
			return []string{"a"}, nil
		},
		Handle: func(ctx context.Context, _ string) bool {
			handleDeadline, _ = ctx.Deadline()
			return true
		},
		Timeout: time.Minute,
		Logger:  discardLogger(),
	})

	if sweepDeadline.IsZero() {
		t.Fatal("expected the sweep to run under a deadline")
	}
	if !handleDeadline.Equal(sweepDeadline) {
		t.Error("expected one deadline across the pass, not one per call")
	}
}

// A sweep that burns the budget leaves nothing for the handles.
func TestPass_TimeoutStopsHandlingAfterASlowSweep(t *testing.T) {
	handleCalled := false

	Pass(context.Background(), Loop[string]{
		Sweep: func(ctx context.Context) ([]string, error) {
			<-ctx.Done()
			return []string{"a"}, nil
		},
		Handle: func(ctx context.Context, _ string) bool {
			handleCalled = ctx.Err() == nil
			return true
		},
		Timeout: 10 * time.Millisecond,
		Logger:  discardLogger(),
	})

	if handleCalled {
		t.Error("expected no item handled on a context the sweep already exhausted")
	}
}

func TestPass_NoTimeoutLeavesTheContextAlone(t *testing.T) {
	hasDeadline := true

	Pass(context.Background(), Loop[string]{
		Sweep: func(ctx context.Context) ([]string, error) {
			_, hasDeadline = ctx.Deadline()
			return nil, nil
		},
		Handle: func(context.Context, string) bool { return true },
		Logger: discardLogger(),
	})

	if hasDeadline {
		t.Error("expected no deadline when none was configured")
	}
}

func TestPass_SweepErrorHandlesNothing(t *testing.T) {
	handleCalled := false

	Pass(context.Background(), Loop[string]{
		Sweep: func(context.Context) ([]string, error) { return nil, errors.New("database error") },
		Handle: func(context.Context, string) bool {
			handleCalled = true
			return true
		},
		Logger: discardLogger(),
	})

	if handleCalled {
		t.Error("a failed sweep has nothing to settle")
	}
}

// =============================================================================
// Run Tests
// =============================================================================

// The first pass runs on entry, before any tick.
func TestRun_SweepsBeforeFirstTick(t *testing.T) {
	passes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Run(ctx, Loop[string]{
		Sweep: func(context.Context) ([]string, error) {
			passes <- struct{}{}
			return nil, nil
		},
		Handle:   func(context.Context, string) bool { return true },
		Interval: time.Hour, // No tick can fire, so only the entry sweep can.
		Logger:   discardLogger(),
	})

	select {
	case <-passes:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not sweep before the first tick")
	}
}

func TestRun_SweepsUntilContextCancelled(t *testing.T) {
	passes := make(chan struct{}, 8)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, Loop[string]{
			Sweep: func(context.Context) ([]string, error) {
				// Non-blocking: a full buffer must not pin the goroutine.
				select {
				case passes <- struct{}{}:
				default:
				}
				return nil, nil
			},
			Handle:   func(context.Context, string) bool { return true },
			Interval: time.Millisecond,
			Logger:   discardLogger(),
		})
		close(done)
	}()

	for range 2 {
		select {
		case <-passes:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("sweeper did not run a pass within 5s")
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// A zero Interval and a nil Logger fall back; a cancelled context sweeps nothing.
func TestRun_AppliesDefaults(t *testing.T) {
	passes := 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := Loop[string]{
		Sweep: func(context.Context) ([]string, error) {
			passes++
			return []string{"a"}, nil
		},
		Handle: func(context.Context, string) bool { return true },
	}

	// Exercises the nil-logger path, which Run skips on a cancelled context.
	Pass(context.Background(), cfg)
	passes = 0

	done := make(chan struct{})
	go func() {
		Run(ctx, cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on a cancelled context")
	}

	if passes != 0 {
		t.Errorf("swept %d times on a cancelled context, want none", passes)
	}
}
