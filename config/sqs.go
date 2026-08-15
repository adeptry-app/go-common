package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// SQS ceilings. A visibility timeout is measured from the ReceiveMessage call,
// so the ladder stops an hour short to leave room for handler runtime.
const (
	MaxVisibilityTimeout = 12 * time.Hour
	MaxRetryDelay        = 11 * time.Hour
)

// Default values for optional SQS settings.
const (
	DefaultMaxNumberOfMessages = 1
	DefaultWaitTimeSeconds     = 20
	DefaultVisibilityTimeout   = 30 * time.Second
	DefaultConsumerConcurrency = 1
)

// SQSConfig holds SQS queue configuration.
type SQSConfig struct {
	QueueURL string `validate:"required,url"`
	DLQURL   string `validate:"required,url"`
	Region   string `validate:"required"`

	// Endpoint overrides the AWS endpoint for LocalStack; development only.
	Endpoint string

	// RetryDelays is the retry ladder, applied as a per-failure visibility
	// timeout. Indexed by the business attempt the handler reports back.
	RetryDelays []time.Duration

	// RetryJitter randomly shortens each retry delay by up to this fraction
	// (0 to 1) to spread out retries of messages that failed together.
	// 0 disables jitter. See queue package docs for semantics.
	RetryJitter float64 `validate:"min=0,max=1"`

	// MaxNumberOfMessages is the receive batch size; SQS caps it at 10.
	MaxNumberOfMessages int `validate:"omitempty,min=1,max=10"`

	// WaitTimeSeconds is the long-poll wait; SQS caps it at 20.
	WaitTimeSeconds int `validate:"min=0,max=20"`

	// VisibilityTimeout hides a received message from other consumers. It must
	// exceed the handler timeout, since SQS reverts to it on every receive.
	VisibilityTimeout time.Duration

	// ConsumerConcurrency is the number of messages processed in parallel by
	// Consume. Defaults to 1 (sequential).
	ConsumerConcurrency int `validate:"omitempty,min=1"`
}

// WithDefaults returns a copy of the config with zero-valued optional fields
// replaced by their defaults. The queue package applies it on construction,
// so configs built as struct literals behave the same as env-loaded ones.
func (c SQSConfig) WithDefaults() SQSConfig {
	if c.MaxNumberOfMessages <= 0 {
		c.MaxNumberOfMessages = DefaultMaxNumberOfMessages
	}
	if c.WaitTimeSeconds <= 0 {
		c.WaitTimeSeconds = DefaultWaitTimeSeconds
	}
	if c.VisibilityTimeout <= 0 {
		c.VisibilityTimeout = DefaultVisibilityTimeout
	}
	if c.ConsumerConcurrency <= 0 {
		c.ConsumerConcurrency = DefaultConsumerConcurrency
	}
	return c
}

// defaultRetryDelays provides sensible defaults for retry delays
// Designed for email delivery: quick retry for transient issues, longer waits for outages
var defaultRetryDelays = []time.Duration{
	1 * time.Minute,  // Transient network issues
	5 * time.Minute,  // Service temporarily unavailable
	30 * time.Minute, // Longer outage
	2 * time.Hour,    // Extended issue
	11 * time.Hour,   // Major outage, last retry before permanent failure
}

// DefaultRetryDelays returns a copy of the default retry delays
func DefaultRetryDelays() []time.Duration {
	return append([]time.Duration(nil), defaultRetryDelays...)
}

// NewSQSConfig loads SQS configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewSQSConfig() SQSConfig {
	return NewSQSConfigWithPrefix("")
}

// NewSQSConfigWithPrefix loads SQS configuration from environment variables
// with an optional prefix, allowing one service to configure multiple queues
// independently (e.g. prefix "AI_" reads AI_SQS_QUEUE_URL). For each variable
// the prefixed name is checked first, falling back to the un-prefixed name, so
// shared settings (region, endpoint) only need to be set once. It panics if
// required variables are missing or invalid.
func NewSQSConfigWithPrefix(prefix string) SQSConfig {
	env := prefixedEnv{prefix: prefix}

	cfg := SQSConfig{
		QueueURL:            env.required("SQS_QUEUE_URL"),
		DLQURL:              env.required("SQS_DLQ_URL"),
		Region:              env.required("SQS_REGION"),
		Endpoint:            env.get("SQS_ENDPOINT", ""),
		RetryDelays:         parseRetryDelays(env.get("SQS_RETRY_DELAYS", "")),
		RetryJitter:         env.float("SQS_RETRY_JITTER", 0),
		MaxNumberOfMessages: env.int("SQS_MAX_MESSAGES", DefaultMaxNumberOfMessages),
		WaitTimeSeconds:     env.int("SQS_WAIT_TIME_SECONDS", DefaultWaitTimeSeconds),
		VisibilityTimeout:   env.duration("SQS_VISIBILITY_TIMEOUT", DefaultVisibilityTimeout),
		ConsumerConcurrency: env.int("SQS_CONSUMER_CONCURRENCY", DefaultConsumerConcurrency),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid SQS configuration: %v", err))
	}
	if cfg.VisibilityTimeout <= 0 || cfg.VisibilityTimeout > MaxVisibilityTimeout {
		panic(fmt.Sprintf("%s (%s) must be greater than 0 and at most %s",
			env.name("SQS_VISIBILITY_TIMEOUT"), cfg.VisibilityTimeout, MaxVisibilityTimeout))
	}
	// A stray LocalStack endpoint would redirect deployed traffic away from AWS.
	if cfg.Endpoint != "" && GetEnv("ENVIRONMENT", "development") != "development" {
		panic(fmt.Sprintf("%s is only allowed when ENVIRONMENT is development", env.name("SQS_ENDPOINT")))
	}

	return cfg
}

// prefixedEnv looks up environment variables with a prefix, falling back to
// the un-prefixed name.
type prefixedEnv struct {
	prefix string
}

// lookup returns the value and the name of the variable it was actually read
// from: the prefixed name when set, otherwise the un-prefixed fallback. A
// prefixed value that is empty or whitespace-only counts as unset and falls
// through to the un-prefixed name. Values are returned raw (not trimmed), so
// whitespace-significant values like passwords survive. The resolved name
// keeps error messages accurate for both forms.
func (e prefixedEnv) lookup(key string) (value, varName string) {
	if e.prefix != "" {
		name := e.prefix + key
		if v := os.Getenv(name); strings.TrimSpace(v) != "" {
			return v, name
		}
	}
	return os.Getenv(key), key
}

func (e prefixedEnv) get(key, defaultValue string) string {
	if v, _ := e.lookup(key); v != "" {
		return v
	}
	return defaultValue
}

// name is the variable to blame in diagnostics: both forms when a prefix is
// set, the plain key otherwise (the un-prefixed helpers in helpers.go).
func (e prefixedEnv) name(key string) string {
	if e.prefix == "" {
		return key
	}
	return fmt.Sprintf("%s%s (or %s)", e.prefix, key, key)
}

func (e prefixedEnv) required(key string) string {
	v, _ := e.lookup(key)
	if v == "" {
		panic(fmt.Sprintf("Required environment variable %s is not set", e.name(key)))
	}
	return v
}

func (e prefixedEnv) requiredInt(key string) int {
	v, varName := e.lookup(key)
	if v == "" {
		panic(fmt.Sprintf("Required environment variable %s is not set", e.name(key)))
	}
	intVal, err := strconv.Atoi(v)
	if err != nil {
		panic(fmt.Sprintf("Invalid integer value for %s: %v", varName, err))
	}
	return intVal
}

// bool parses the accepted boolean forms (case-insensitive true/false/1/0,
// surrounding whitespace ignored) and panics on anything else, naming the
// resolved variable, so a typo cannot silently flip a flag. Empty or
// whitespace-only keeps the default.
func (e prefixedEnv) bool(key string, defaultValue bool) bool {
	v, varName := e.lookup(key)
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultValue
	}
	b, ok := parseBool(v)
	if !ok {
		panic(fmt.Sprintf("Invalid boolean value for %s: %q (accepted: true, false, 1, 0)", varName, v))
	}
	return b
}

func (e prefixedEnv) int(key string, defaultValue int) int {
	v, varName := e.lookup(key)
	if v == "" {
		return defaultValue
	}
	intVal, err := strconv.Atoi(v)
	if err != nil {
		panic(fmt.Sprintf("Invalid integer value for %s: %v", varName, err))
	}
	return intVal
}

func (e prefixedEnv) float(key string, defaultValue float64) float64 {
	v, varName := e.lookup(key)
	if v == "" {
		return defaultValue
	}
	floatVal, err := strconv.ParseFloat(v, 64)
	if err != nil {
		panic(fmt.Sprintf("Invalid float value for %s: %v", varName, err))
	}
	return floatVal
}

func (e prefixedEnv) duration(key string, defaultValue time.Duration) time.Duration {
	v, varName := e.lookup(key)
	if v == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		panic(fmt.Sprintf("Invalid duration value for %s: %v", varName, err))
	}
	return d
}

// parseRetryDelays parses comma-separated duration strings (e.g., "5s,30s,5m,30m,2h").
// A step above MaxRetryDelay could never be requested successfully.
func parseRetryDelays(s string) []time.Duration {
	parts := splitList(s)
	delays := make([]time.Duration, 0, len(parts))

	for _, part := range parts {
		d, err := time.ParseDuration(part)
		if err != nil {
			panic(fmt.Sprintf("Invalid retry delay %q: %v", part, err))
		}
		if d <= 0 {
			panic(fmt.Sprintf("Retry delay must be positive, got %q", part))
		}
		if d > MaxRetryDelay {
			panic(fmt.Sprintf("Retry delay must be at most %s, got %q", MaxRetryDelay, part))
		}
		delays = append(delays, d)
	}

	if len(delays) == 0 {
		return DefaultRetryDelays()
	}

	return delays
}
