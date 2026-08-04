// Package sweeper runs a recovery pass on an interval: find the work a normal
// path could not finish, then settle each item. The row, not the message, is the
// source of truth, so without a sweep a publish that fails after its row commits
// loses the work outright and a cleanup that fails after its row commits leaves
// an orphan nothing can find.
package sweeper

import (
	"context"
	"log/slog"
	"time"
)

// DefaultInterval applies when Config.Interval is not positive.
const DefaultInterval = time.Minute

// Config is one recovery loop. T is whatever a pass hands back: a row id to
// re-publish, a tombstone to reap.
type Config[T any] struct {
	// Sweep runs one recovery pass and returns the items to settle. Items past
	// whatever budget applies are expected to be dealt with inside the call
	// rather than returned.
	Sweep func(ctx context.Context) ([]T, error)

	// Handle settles one item, reporting whether it is now done. It owns its
	// own error logging: only it knows what the failure means.
	Handle func(ctx context.Context, item T) bool

	// Kind names the swept work in log messages, e.g. "email".
	Kind string

	// Interval between passes; DefaultInterval when not positive.
	Interval time.Duration

	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

func (c Config[T]) logger() *slog.Logger {
	if c.Logger == nil {
		return slog.Default()
	}
	return c.Logger
}

// Run sweeps once on entry, then on every tick, and blocks until ctx is
// cancelled. The entry sweep matters because a service restarting faster than
// interval would otherwise never reach a tick.
func Run[T any](ctx context.Context, cfg Config[T]) {
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

// Pass runs one recovery pass. A failure on one item never stops the rest: the
// next pass picks up whatever is left.
func Pass[T any](ctx context.Context, cfg Config[T]) {
	log := cfg.logger()

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
