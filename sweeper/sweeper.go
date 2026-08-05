// Package sweeper runs a recovery pass on an interval: find the work a normal
// path could not finish, then settle each item.
package sweeper

import (
	"context"
	"log/slog"
	"time"
)

// DefaultInterval applies when Loop.Interval is not positive.
const DefaultInterval = time.Minute

// Loop wires one recovery loop. T is what a pass hands back.
type Loop[T any] struct {
	// Sweep runs one recovery pass and returns the items to settle.
	Sweep func(ctx context.Context) ([]T, error)

	// Handle settles one item, reporting whether it is now done, and logs its
	// own failures.
	Handle func(ctx context.Context, item T) bool

	// Kind names the swept work in log messages, e.g. "email".
	Kind string

	// Interval between passes; DefaultInterval when not positive.
	Interval time.Duration

	// Timeout bounds one whole pass; not positive means no deadline.
	Timeout time.Duration

	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

func (c Loop[T]) logger() *slog.Logger {
	if c.Logger == nil {
		return slog.Default()
	}
	return c.Logger
}

// Run sweeps on entry, then on every tick, and blocks until ctx is cancelled.
func Run[T any](ctx context.Context, cfg Loop[T]) {
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if ctx.Err() != nil {
		return
	}
	Pass(ctx, cfg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Pass(ctx, cfg)
		}
	}
}

// Pass runs one recovery pass, continuing past an item it cannot settle.
func Pass[T any](ctx context.Context, cfg Loop[T]) {
	log := cfg.logger()

	// One deadline across sweep and every handle, not one per call.
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	items, err := cfg.Sweep(ctx)
	if err != nil {
		log.Error("Sweep stale rows failed", "kind", cfg.Kind, "error", err)
		return
	}
	if len(items) == 0 {
		return
	}

	settled := 0
	for _, item := range items {
		if cfg.Handle(ctx, item) {
			settled++
		}
	}

	log.Info("Swept stale rows", "kind", cfg.Kind, "settled", settled, "stale", len(items))
}
