package config

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

// SweeperConfig tunes the periodic stale-row sweeper that recovers work whose
// queue message was lost (publish failed, or landed unconfirmed) or whose
// worker died mid-job.
type SweeperConfig struct {
	// Interval between recovery passes.
	Interval time.Duration `validate:"min=1s"`

	// PendingAge is how long a never-claimed row may sit before it counts as
	// stale, anchored on its creation time.
	PendingAge time.Duration `validate:"min=1s"`

	// ProcessingAge is how long an in-flight or retrying row may sit quiet
	// before it counts as stale. Derived from the retry ladder, never a fixed
	// default: see NewSweeperConfig.
	ProcessingAge time.Duration `validate:"min=1s"`

	// MaxAttempts is the attempt budget; a stale row at or past it is failed
	// instead of re-published.
	MaxAttempts int `validate:"min=1"`
}

// NewSweeperConfig loads sweeper tuning from the environment, sizing
// ProcessingAge against the caller's retry ladder and job timeout. It panics if
// any value is malformed, out of range, or below that floor.
//
// A retrying row legitimately sits quiet for the longest rung and a processing
// row for the job timeout, so a ProcessingAge at or under their sum
// double-publishes live work and burns the attempt budget. The floor cannot be
// a constant: it depends entirely on config this constructor is handed.
func NewSweeperConfig(retryDelays []time.Duration, jobTimeout time.Duration) SweeperConfig {
	var maxRetryDelay time.Duration
	for _, d := range retryDelays {
		maxRetryDelay = max(maxRetryDelay, d)
	}
	floor := maxRetryDelay + jobTimeout

	interval := GetEnvDuration("SWEEPER_INTERVAL", time.Minute)
	cfg := SweeperConfig{
		Interval:      interval,
		PendingAge:    GetEnvDuration("SWEEPER_PENDING_AGE", 2*time.Minute),
		ProcessingAge: GetEnvDuration("SWEEPER_PROCESSING_AGE", floor+interval),
		MaxAttempts:   GetEnvInt("SWEEPER_MAX_ATTEMPTS", 4),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid sweeper configuration: %v", err))
	}
	if cfg.ProcessingAge <= floor {
		panic(fmt.Sprintf("SWEEPER_PROCESSING_AGE (%s) must exceed max RABBITMQ_RETRY_DELAYS (%s) + job timeout (%s) = %s",
			cfg.ProcessingAge, maxRetryDelay, jobTimeout, floor))
	}

	return cfg
}
