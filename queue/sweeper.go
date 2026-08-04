package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/adeptry-app/go-common/sweeper"
)

// DefaultSweepInterval applies when StaleSweeper.Interval is not positive.
const DefaultSweepInterval = sweeper.DefaultInterval

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
// cancelled.
func (s StaleSweeper) Run(ctx context.Context) {
	sweeper.Run(ctx, s.config())
}

func (s StaleSweeper) pass(ctx context.Context) {
	sweeper.Pass(ctx, s.config())
}

// config expresses re-publishing as the generic loop's Handle: what is
// queue-specific here is only where a swept id goes.
func (s StaleSweeper) config() sweeper.Config[int64] {
	return sweeper.Config[int64]{
		Sweep:    s.Sweep,
		Handle:   s.republish,
		Kind:     s.Kind,
		Interval: s.Interval,
		Logger:   s.Logger,
	}
}

func (s StaleSweeper) republish(ctx context.Context, id int64) bool {
	if err := s.Publisher.Publish(ctx, s.Event(id)); err != nil {
		log := s.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("Re-publish stale row failed", "kind", s.Kind, "id", id, "error", err)
		return false
	}
	return true
}
