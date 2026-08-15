package config

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// DefaultRetryDelays Tests
// =============================================================================

func TestDefaultRetryDelays(t *testing.T) {
	delays := DefaultRetryDelays()

	// Should return 5 default delays
	if len(delays) != 5 {
		t.Errorf("DefaultRetryDelays() returned %d delays, want 5", len(delays))
	}

	// Verify expected values
	expected := []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		30 * time.Minute,
		2 * time.Hour,
		11 * time.Hour,
	}

	for i, want := range expected {
		if delays[i] != want {
			t.Errorf("DefaultRetryDelays()[%d] = %v, want %v", i, delays[i], want)
		}
	}

	// Verify it returns a copy (modifying returned slice shouldn't affect future calls)
	delays[0] = 999 * time.Hour
	newDelays := DefaultRetryDelays()
	if newDelays[0] == 999*time.Hour {
		t.Error("DefaultRetryDelays() should return a copy, not the original slice")
	}
}

// =============================================================================
// parseRetryDelays Tests
// =============================================================================

func TestParseRetryDelays(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantVals []time.Duration
	}{
		{
			name:    "empty string returns defaults",
			input:   "",
			wantLen: 5,
		},
		{
			name:     "single duration",
			input:    "30s",
			wantLen:  1,
			wantVals: []time.Duration{30 * time.Second},
		},
		{
			name:     "multiple durations",
			input:    "1m,5m,30m",
			wantLen:  3,
			wantVals: []time.Duration{1 * time.Minute, 5 * time.Minute, 30 * time.Minute},
		},
		{
			name:     "with spaces",
			input:    "1m, 5m, 30m",
			wantLen:  3,
			wantVals: []time.Duration{1 * time.Minute, 5 * time.Minute, 30 * time.Minute},
		},
		{
			name:     "hours up to the ceiling",
			input:    "1h,2h,11h",
			wantLen:  3,
			wantVals: []time.Duration{1 * time.Hour, 2 * time.Hour, 11 * time.Hour},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delays := parseRetryDelays(tt.input)

			if len(delays) != tt.wantLen {
				t.Errorf("parseRetryDelays(%q) returned %d delays, want %d", tt.input, len(delays), tt.wantLen)
				return
			}

			if tt.wantVals != nil {
				for i, want := range tt.wantVals {
					if delays[i] != want {
						t.Errorf("parseRetryDelays(%q)[%d] = %v, want %v", tt.input, i, delays[i], want)
					}
				}
			}
		})
	}
}

func TestParseRetryDelays_Panics(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "invalid duration format",
			input: "invalid",
		},
		{
			name:  "negative duration",
			input: "-5m",
		},
		{
			name:  "zero duration",
			input: "0s",
		},
		{
			// 12h is the SQS ceiling measured from receipt, so it can never be
			// requested successfully after a handler has already run.
			name:  "delay above the retry ceiling",
			input: "1m,12h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("parseRetryDelays(%q) should panic", tt.input)
				}
			}()

			parseRetryDelays(tt.input)
		})
	}
}

// =============================================================================
// WithDefaults Tests
// =============================================================================

func TestWithDefaults_FillsZeroValues(t *testing.T) {
	cfg := SQSConfig{}.WithDefaults()

	if cfg.MaxNumberOfMessages != 1 {
		t.Errorf("MaxNumberOfMessages = %d, want 1", cfg.MaxNumberOfMessages)
	}
	if cfg.WaitTimeSeconds != 20 {
		t.Errorf("WaitTimeSeconds = %d, want 20", cfg.WaitTimeSeconds)
	}
	if cfg.VisibilityTimeout != 30*time.Second {
		t.Errorf("VisibilityTimeout = %v, want 30s", cfg.VisibilityTimeout)
	}
	if cfg.ConsumerConcurrency != 1 {
		t.Errorf("ConsumerConcurrency = %d, want 1", cfg.ConsumerConcurrency)
	}
}

func TestWithDefaults_KeepsExplicitValues(t *testing.T) {
	cfg := SQSConfig{
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     5,
		VisibilityTimeout:   6 * time.Minute,
		ConsumerConcurrency: 4,
	}.WithDefaults()

	if cfg.MaxNumberOfMessages != 10 {
		t.Errorf("MaxNumberOfMessages = %d, want 10", cfg.MaxNumberOfMessages)
	}
	if cfg.WaitTimeSeconds != 5 {
		t.Errorf("WaitTimeSeconds = %d, want 5", cfg.WaitTimeSeconds)
	}
	if cfg.VisibilityTimeout != 6*time.Minute {
		t.Errorf("VisibilityTimeout = %v, want 6m", cfg.VisibilityTimeout)
	}
	if cfg.ConsumerConcurrency != 4 {
		t.Errorf("ConsumerConcurrency = %d, want 4", cfg.ConsumerConcurrency)
	}
}

// =============================================================================
// Prefixed Environment Tests
// =============================================================================

func setBaseSQSEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SQS_QUEUE_URL", "http://sqs.local/000000000000/base_queue")
	t.Setenv("SQS_DLQ_URL", "http://sqs.local/000000000000/base_queue_dlq")
	t.Setenv("SQS_REGION", "eu-west-1")
}

func TestNewSQSConfigWithPrefix_EmptyPrefixUsesBaseVars(t *testing.T) {
	setBaseSQSEnv(t)

	cfg := NewSQSConfigWithPrefix("")

	if cfg.QueueURL != "http://sqs.local/000000000000/base_queue" {
		t.Errorf("QueueURL = %q, want the base value", cfg.QueueURL)
	}
	if cfg.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", cfg.Region, "eu-west-1")
	}
}

func TestNewSQSConfigWithPrefix_PrefixedOverridesBase(t *testing.T) {
	setBaseSQSEnv(t)
	t.Setenv("AI_SQS_QUEUE_URL", "http://sqs.local/000000000000/ai_requests")
	t.Setenv("AI_SQS_RETRY_DELAYS", "5s,15s")
	t.Setenv("AI_SQS_MAX_MESSAGES", "2")

	cfg := NewSQSConfigWithPrefix("AI_")

	// Prefixed values win.
	if cfg.QueueURL != "http://sqs.local/000000000000/ai_requests" {
		t.Errorf("QueueURL = %q, want the prefixed value", cfg.QueueURL)
	}
	if len(cfg.RetryDelays) != 2 || cfg.RetryDelays[0] != 5*time.Second {
		t.Errorf("RetryDelays = %v, want [5s 15s]", cfg.RetryDelays)
	}
	if cfg.MaxNumberOfMessages != 2 {
		t.Errorf("MaxNumberOfMessages = %d, want 2", cfg.MaxNumberOfMessages)
	}

	// Un-prefixed values are the fallback for shared settings.
	if cfg.Region != "eu-west-1" {
		t.Errorf("Region = %q, want fallback %q", cfg.Region, "eu-west-1")
	}
	if cfg.DLQURL != "http://sqs.local/000000000000/base_queue_dlq" {
		t.Errorf("DLQURL = %q, want the base fallback", cfg.DLQURL)
	}
}

func TestNewSQSConfigWithPrefix_WhitespacePrefixedFallsBack(t *testing.T) {
	setBaseSQSEnv(t)
	// A whitespace-only prefixed value counts as unset and must fall back to
	// the un-prefixed variable, not short-circuit to the default.
	t.Setenv("AI_SQS_QUEUE_URL", "   ")

	cfg := NewSQSConfigWithPrefix("AI_")

	if cfg.QueueURL != "http://sqs.local/000000000000/base_queue" {
		t.Errorf("QueueURL = %q, want the un-prefixed fallback", cfg.QueueURL)
	}
}

func TestNewSQSConfig_Defaults(t *testing.T) {
	setBaseSQSEnv(t)

	cfg := NewSQSConfig()

	if cfg.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty", cfg.Endpoint)
	}
	if len(cfg.RetryDelays) != 5 {
		t.Errorf("RetryDelays = %v, want the 5 defaults", cfg.RetryDelays)
	}
	if cfg.RetryJitter != 0 {
		t.Errorf("RetryJitter = %v, want 0", cfg.RetryJitter)
	}
	if cfg.MaxNumberOfMessages != 1 {
		t.Errorf("MaxNumberOfMessages = %d, want 1", cfg.MaxNumberOfMessages)
	}
	if cfg.WaitTimeSeconds != 20 {
		t.Errorf("WaitTimeSeconds = %d, want 20", cfg.WaitTimeSeconds)
	}
	if cfg.VisibilityTimeout != 30*time.Second {
		t.Errorf("VisibilityTimeout = %v, want 30s", cfg.VisibilityTimeout)
	}
	if cfg.ConsumerConcurrency != 1 {
		t.Errorf("ConsumerConcurrency = %d, want 1", cfg.ConsumerConcurrency)
	}
}

func TestNewSQSConfig_FieldsFromEnv(t *testing.T) {
	setBaseSQSEnv(t)
	t.Setenv("SQS_ENDPOINT", "http://localstack:4566")
	t.Setenv("SQS_RETRY_DELAYS", "30s,2m,10m")
	t.Setenv("SQS_RETRY_JITTER", "0.25")
	t.Setenv("SQS_MAX_MESSAGES", "5")
	t.Setenv("SQS_WAIT_TIME_SECONDS", "10")
	t.Setenv("SQS_VISIBILITY_TIMEOUT", "6m")
	t.Setenv("SQS_CONSUMER_CONCURRENCY", "4")

	cfg := NewSQSConfig()

	if cfg.Endpoint != "http://localstack:4566" {
		t.Errorf("Endpoint = %q, want the LocalStack endpoint", cfg.Endpoint)
	}
	if len(cfg.RetryDelays) != 3 || cfg.RetryDelays[2] != 10*time.Minute {
		t.Errorf("RetryDelays = %v, want [30s 2m 10m]", cfg.RetryDelays)
	}
	if cfg.RetryJitter != 0.25 {
		t.Errorf("RetryJitter = %v, want 0.25", cfg.RetryJitter)
	}
	if cfg.MaxNumberOfMessages != 5 {
		t.Errorf("MaxNumberOfMessages = %d, want 5", cfg.MaxNumberOfMessages)
	}
	if cfg.WaitTimeSeconds != 10 {
		t.Errorf("WaitTimeSeconds = %d, want 10", cfg.WaitTimeSeconds)
	}
	if cfg.VisibilityTimeout != 6*time.Minute {
		t.Errorf("VisibilityTimeout = %v, want 6m", cfg.VisibilityTimeout)
	}
	if cfg.ConsumerConcurrency != 4 {
		t.Errorf("ConsumerConcurrency = %d, want 4", cfg.ConsumerConcurrency)
	}
}

func TestNewSQSConfig_MissingRequiredPanics(t *testing.T) {
	tests := []string{"SQS_QUEUE_URL", "SQS_DLQ_URL", "SQS_REGION"}

	for _, missing := range tests {
		t.Run(missing, func(t *testing.T) {
			setBaseSQSEnv(t)
			t.Setenv(missing, "")

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("NewSQSConfig should panic when %s is unset", missing)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, missing) {
					t.Errorf("panic = %v, want it to name %s", r, missing)
				}
			}()

			NewSQSConfig()
		})
	}
}

func TestNewSQSConfig_InvalidValuesPanic(t *testing.T) {
	tests := []struct {
		name        string
		envVar      string
		value       string
		wantInPanic string
	}{
		{"queue url is not a url", "SQS_QUEUE_URL", "ai_requests", "QueueURL"},
		{"jitter above one", "SQS_RETRY_JITTER", "1.5", "RetryJitter"},
		{"batch size above the SQS cap", "SQS_MAX_MESSAGES", "11", "MaxNumberOfMessages"},
		{"long poll above the SQS cap", "SQS_WAIT_TIME_SECONDS", "21", "WaitTimeSeconds"},
		{"negative concurrency", "SQS_CONSUMER_CONCURRENCY", "-1", "ConsumerConcurrency"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBaseSQSEnv(t)
			t.Setenv(tt.envVar, tt.value)

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("NewSQSConfig should panic for %s=%s", tt.envVar, tt.value)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, tt.wantInPanic) {
					t.Errorf("panic = %v, want it to name %s", r, tt.wantInPanic)
				}
			}()

			NewSQSConfig()
		})
	}
}

func TestNewSQSConfig_VisibilityTimeoutBounds(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantPanic bool
	}{
		{"at the SQS ceiling", "12h", false},
		{"above the SQS ceiling", "12h1s", true},
		{"zero", "0s", true},
		{"negative", "-1s", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBaseSQSEnv(t)
			t.Setenv("SQS_VISIBILITY_TIMEOUT", tt.value)

			defer func() {
				r := recover()
				if tt.wantPanic && r == nil {
					t.Fatalf("NewSQSConfig should panic for SQS_VISIBILITY_TIMEOUT=%s", tt.value)
				}
				if !tt.wantPanic && r != nil {
					t.Fatalf("NewSQSConfig panicked for SQS_VISIBILITY_TIMEOUT=%s: %v", tt.value, r)
				}
			}()

			NewSQSConfig()
		})
	}
}

func TestNewSQSConfig_EndpointOutsideDevelopmentPanics(t *testing.T) {
	tests := []struct {
		environment string
		wantPanic   bool
	}{
		{"development", false},
		{"staging", true},
		{"production", true},
	}

	for _, tt := range tests {
		t.Run(tt.environment, func(t *testing.T) {
			setBaseSQSEnv(t)
			t.Setenv("ENVIRONMENT", tt.environment)
			t.Setenv("SQS_ENDPOINT", "http://localstack:4566")

			defer func() {
				r := recover()
				if tt.wantPanic && r == nil {
					t.Fatalf("NewSQSConfig should panic for SQS_ENDPOINT in %s", tt.environment)
				}
				if !tt.wantPanic && r != nil {
					t.Fatalf("NewSQSConfig panicked for SQS_ENDPOINT in %s: %v", tt.environment, r)
				}
			}()

			NewSQSConfig()
		})
	}
}
