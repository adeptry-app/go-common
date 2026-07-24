package queue

import (
	"context"
	"log/slog"
	"time"
)

// DefaultSweepInterval applies when StaleSweeper.Interval is not positive.
const DefaultSweepInterval = time.Minute

// EventPublisher is the publish-only subset of Publisher that StaleSweeper needs.
type EventPublisher interface {
	Publish(ctx context.Context, message any) error
}

// StaleSweeper recovers work rows whose queue message never arrived or whose
// worker died mid-job, by re-publishing the ids a sweep query hands back. The
// row, not the message, is the source of truth: without this the queue's
// at-least-once guarantee still loses work when a publish fails after the row
// commits.
type StaleSweeper struct {
	// Sweep runs one recovery pass and returns the ids to re-publish. Rows
	// past the attempt budget are expected to be failed inside the call
	// rather than returned.
	Sweep func(ctx context.Context) ([]int64, error)

	// Event builds the queue message for a swept id.
	Event func(id int64) any

	// Publisher receives the rebuilt events.
	Publisher EventPublisher

	// Kind names the swept work in log messages, e.g. "email".
	Kind string

	// Interval between passes; DefaultSweepInterval when not positive.
	Interval time.Duration

	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Run sweeps once on entry, then on every tick, and blocks until ctx is
// cancelled. The entry sweep matters because a worker restarting faster than
// interval would otherwise never reach a tick.
func (s StaleSweeper) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = DefaultSweepInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if ctx.Err() != nil {
		return
	}
	s.pass(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pass(ctx)
		}
	}
}

func (s StaleSweeper) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.Default()
	}
	return s.Logger
}

func (s StaleSweeper) pass(ctx context.Context) {
	log := s.logger()

	ids, err := s.Sweep(ctx)
	if err != nil {
		log.Error("Sweep stale rows failed", "kind", s.Kind, "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}

	republished := 0
	for _, id := range ids {
		if err := s.Publisher.Publish(ctx, s.Event(id)); err != nil {
			log.Error("Re-publish stale row failed", "kind", s.Kind, "id", id, "error", err)
			continue
		}
		republished++
	}

	log.Info("Re-published stale rows", "kind", s.Kind, "republished", republished, "stale", len(ids))
}
