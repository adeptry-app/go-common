package config

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

// SweeperLoop tunes any recovery loop: pass cadence and pass deadline.
type SweeperLoop struct {
	// Interval between recovery passes.
	Interval time.Duration `validate:"min=1s"`

	// PassTimeout bounds one whole pass.
	PassTimeout time.Duration `validate:"min=1s"`
}

// NewSweeperLoop loads <prefix>_INTERVAL and <prefix>_PASS_TIMEOUT.
func NewSweeperLoop(prefix string, defaultInterval, defaultPassTimeout time.Duration) SweeperLoop {
	return SweeperLoop{
		Interval:    GetEnvDuration(prefix+"_INTERVAL", defaultInterval),
		PassTimeout: GetEnvDuration(prefix+"_PASS_TIMEOUT", defaultPassTimeout),
	}
}

// SweeperConfig tunes the stale-row sweeper behind the queue.
type SweeperConfig struct {
	SweeperLoop

	// PendingAge is how long a never-claimed row may sit, from created_at.
	PendingAge time.Duration `validate:"min=1s"`

	// ProcessingAge is how long an in-flight or retrying row may sit quiet.
	ProcessingAge time.Duration `validate:"min=1s"`

	// MaxAttempts is the attempt budget; a row at or past it is failed.
	MaxAttempts int `validate:"min=1"`
}

// NewSweeperConfig loads sweeper tuning, sizing ProcessingAge against the
// caller's retry ladder plus job timeout. Panics below that floor.
func NewSweeperConfig(retryDelays []time.Duration, jobTimeout time.Duration) SweeperConfig {
	var maxRetryDelay time.Duration
	for _, d := range retryDelays {
		maxRetryDelay = max(maxRetryDelay, d)
	}
	floor := maxRetryDelay + jobTimeout

	loop := NewSweeperLoop("SWEEPER", time.Minute, 2*time.Minute)
	cfg := SweeperConfig{
		SweeperLoop:   loop,
		PendingAge:    GetEnvDuration("SWEEPER_PENDING_AGE", 2*time.Minute),
		ProcessingAge: GetEnvDuration("SWEEPER_PROCESSING_AGE", floor+loop.Interval),
		MaxAttempts:   GetEnvInt("SWEEPER_MAX_ATTEMPTS", 4),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid sweeper configuration: %v", err))
	}
	if cfg.ProcessingAge <= floor {
		panic(fmt.Sprintf("SWEEPER_PROCESSING_AGE (%s) must exceed max SQS_RETRY_DELAYS (%s) + job timeout (%s) = %s",
			cfg.ProcessingAge, maxRetryDelay, jobTimeout, floor))
	}

	return cfg
}
