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

	Pass(context.Background(), Config[string]{
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

	Pass(context.Background(), Config[string]{
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

func TestPass_SweepErrorHandlesNothing(t *testing.T) {
	handleCalled := false

	Pass(context.Background(), Config[string]{
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

// A service restarting faster than the interval would never reach a tick, so
// the first pass runs on entry.
func TestRun_SweepsBeforeFirstTick(t *testing.T) {
	passes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Run(ctx, Config[string]{
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
		Run(ctx, Config[string]{
			Sweep: func(context.Context) ([]string, error) {
				// Non-blocking: a full buffer must not pin the goroutine, or it
				// never observes the cancellation below.
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

// A zero Interval and a nil Logger must fall back rather than panic, and an
// already-cancelled context must return before spending a pass.
func TestRun_AppliesDefaults(t *testing.T) {
	passes := 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := Config[string]{
		Sweep: func(context.Context) ([]string, error) {
			passes++
			return []string{"a"}, nil
		},
		Handle: func(context.Context, string) bool { return true },
	}

	// Run returns before any pass here, so the nil logger is exercised directly.
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
