package config

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// NewSweeperConfig Tests
// =============================================================================

// clearSweeperEnv blanks every sweeper variable so a value inherited from the
// developer's shell cannot decide a test.
func clearSweeperEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SWEEPER_INTERVAL",
		"SWEEPER_PASS_TIMEOUT",
		"SWEEPER_PENDING_AGE",
		"SWEEPER_PROCESSING_AGE",
		"SWEEPER_MAX_ATTEMPTS",
	} {
		t.Setenv(key, "")
	}
}

// testLadder is a 10m-max retry ladder; paired with a 5m job timeout it puts
// the ProcessingAge floor at 15m.
var testLadder = []time.Duration{time.Minute, 10 * time.Minute, 5 * time.Minute}

func TestNewSweeperConfig_Defaults(t *testing.T) {
	clearSweeperEnv(t)

	cfg := NewSweeperConfig(testLadder, 5*time.Minute)

	if cfg.Interval != time.Minute {
		t.Errorf("Interval = %s, want 1m", cfg.Interval)
	}
	if cfg.PendingAge != 2*time.Minute {
		t.Errorf("PendingAge = %s, want 2m", cfg.PendingAge)
	}
	// The 15m floor plus one interval of slack.
	if cfg.ProcessingAge != 16*time.Minute {
		t.Errorf("ProcessingAge = %s, want 16m", cfg.ProcessingAge)
	}
	if cfg.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4", cfg.MaxAttempts)
	}
}

// The derived default must clear the floor for any ladder, not just the short
// ones: a fixed default silently under-shoots the long ones.
func TestNewSweeperConfig_DefaultClearsFloorForLongLadder(t *testing.T) {
	clearSweeperEnv(t)

	jobTimeout := 5 * time.Minute
	ladder := DefaultRetryDelays()
	floor := ladder[len(ladder)-1] + jobTimeout

	cfg := NewSweeperConfig(ladder, jobTimeout)

	if cfg.ProcessingAge <= floor {
		t.Errorf("ProcessingAge = %s, want above the %s floor", cfg.ProcessingAge, floor)
	}
}

func TestNewSweeperConfig_ReadsEnvironment(t *testing.T) {
	clearSweeperEnv(t)
	t.Setenv("SWEEPER_INTERVAL", "30s")
	t.Setenv("SWEEPER_PENDING_AGE", "5m")
	t.Setenv("SWEEPER_PROCESSING_AGE", "1h")
	t.Setenv("SWEEPER_MAX_ATTEMPTS", "7")

	cfg := NewSweeperConfig(testLadder, 5*time.Minute)

	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %s, want 30s", cfg.Interval)
	}
	if cfg.PendingAge != 5*time.Minute {
		t.Errorf("PendingAge = %s, want 5m", cfg.PendingAge)
	}
	if cfg.ProcessingAge != time.Hour {
		t.Errorf("ProcessingAge = %s, want 1h", cfg.ProcessingAge)
	}
	if cfg.MaxAttempts != 7 {
		t.Errorf("MaxAttempts = %d, want 7", cfg.MaxAttempts)
	}
}

func TestNewSweeperConfig_OutOfRangePanics(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		value  string
	}{
		{name: "interval below minimum", envVar: "SWEEPER_INTERVAL", value: "500ms"},
		{name: "pending age below minimum", envVar: "SWEEPER_PENDING_AGE", value: "0s"},
		{name: "processing age below minimum", envVar: "SWEEPER_PROCESSING_AGE", value: "100ms"},
		{name: "max attempts below minimum", envVar: "SWEEPER_MAX_ATTEMPTS", value: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearSweeperEnv(t)
			t.Setenv(tt.envVar, tt.value)

			defer func() {
				if r := recover(); r == nil {
					t.Errorf("NewSweeperConfig should panic for %s=%s", tt.envVar, tt.value)
				}
			}()

			NewSweeperConfig(testLadder, 5*time.Minute)
		})
	}
}

func TestNewSweeperConfig_MalformedValuePanics(t *testing.T) {
	clearSweeperEnv(t)
	t.Setenv("SWEEPER_INTERVAL", "soon")

	defer func() {
		if r := recover(); r == nil {
			t.Error("NewSweeperConfig should panic for a malformed duration")
		}
	}()

	NewSweeperConfig(testLadder, 5*time.Minute)
}

// =============================================================================
// ProcessingAge Floor Tests
// =============================================================================

func TestNewSweeperConfig_ProcessingAgeFloor(t *testing.T) {
	tests := []struct {
		name          string
		processingAge string
		wantPanic     bool
	}{
		{name: "above the floor", processingAge: "16m"},
		{name: "equal to the floor", processingAge: "15m", wantPanic: true},
		{name: "below the floor", processingAge: "2m", wantPanic: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearSweeperEnv(t)
			t.Setenv("SWEEPER_PROCESSING_AGE", tt.processingAge)

			defer func() {
				r := recover()
				if !tt.wantPanic {
					if r != nil {
						t.Errorf("unexpected panic: %v", r)
					}
					return
				}
				if r == nil {
					t.Fatalf("ProcessingAge %s should panic against a 15m floor", tt.processingAge)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, "SWEEPER_PROCESSING_AGE") {
					t.Errorf("panic = %v, want it to name SWEEPER_PROCESSING_AGE", r)
				}
			}()

			NewSweeperConfig(testLadder, 5*time.Minute)
		})
	}
}

func TestNewSweeperConfig_NoRetryLadder(t *testing.T) {
	clearSweeperEnv(t)
	t.Setenv("SWEEPER_PROCESSING_AGE", "1m")

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic with no retry delays: %v", r)
		}
	}()

	NewSweeperConfig(nil, 30*time.Second)
}
