package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// Default values for optional RabbitMQ settings.
const (
	DefaultHeartbeat             = 10 * time.Second
	DefaultReconnectInitialDelay = 1 * time.Second
	DefaultReconnectMaxDelay     = 30 * time.Second
)

// RabbitMQConfig holds RabbitMQ connection configuration
type RabbitMQConfig struct {
	Host        string `validate:"required"`
	Port        int    `validate:"required,min=1,max=65535"`
	User        string `validate:"required"`
	Password    string `validate:"required"`
	Exchange    string `validate:"required"`
	Queue       string `validate:"required"`
	TLS         bool
	RetryDelays []time.Duration // Delays for retry queues (e.g., 5s, 30s, 5m, 30m, 2h)

	// RetryJitter randomly shortens each retry delay by up to this fraction
	// (0 to 1) to spread out retries of messages that failed together.
	// 0 disables jitter. See queue package docs for semantics.
	RetryJitter float64 `validate:"min=0,max=1"`

	// Heartbeat is the AMQP heartbeat interval. Zero means DefaultHeartbeat.
	Heartbeat time.Duration

	// PublisherConfirms enables publisher confirm mode: Publish blocks until
	// the broker acknowledges the message. Off by default.
	PublisherConfirms bool

	// Reconnection settings. Reconnection is enabled by default; set
	// DisableReconnect to true to restore the old fail-fast behavior.
	DisableReconnect      bool
	ReconnectMaxAttempts  int           `validate:"min=0"` // 0 = unlimited
	ReconnectInitialDelay time.Duration // zero means DefaultReconnectInitialDelay
	ReconnectMaxDelay     time.Duration // zero means DefaultReconnectMaxDelay

	// Consumer-specific settings (optional, only used by consumers)
	PrefetchCount int    `validate:"omitempty,min=1"` // Number of messages to prefetch (QoS), defaults to 1
	ConsumerTag   string // Unique identifier for this consumer

	// ConsumerConcurrency is the number of messages processed in parallel by
	// Consume. Defaults to 1 (sequential, the previous behavior). Effective
	// parallelism is capped by PrefetchCount.
	ConsumerConcurrency int `validate:"omitempty,min=1"`
}

// WithDefaults returns a copy of the config with zero-valued optional fields
// replaced by their defaults. The queue package applies it on construction,
// so configs built as struct literals behave the same as env-loaded ones.
func (c RabbitMQConfig) WithDefaults() RabbitMQConfig {
	if c.Heartbeat <= 0 {
		c.Heartbeat = DefaultHeartbeat
	}
	if c.ReconnectInitialDelay <= 0 {
		c.ReconnectInitialDelay = DefaultReconnectInitialDelay
	}
	if c.ReconnectMaxDelay <= 0 {
		c.ReconnectMaxDelay = DefaultReconnectMaxDelay
	}
	if c.PrefetchCount <= 0 {
		c.PrefetchCount = 1
	}
	if c.ConsumerConcurrency <= 0 {
		c.ConsumerConcurrency = 1
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
	12 * time.Hour,   // Major outage, last retry before permanent failure
}

// DefaultRetryDelays returns a copy of the default retry delays
func DefaultRetryDelays() []time.Duration {
	return append([]time.Duration(nil), defaultRetryDelays...)
}

// NewRabbitMQConfig loads RabbitMQ configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewRabbitMQConfig() RabbitMQConfig {
	return NewRabbitMQConfigWithPrefix("")
}

// NewRabbitMQConfigWithPrefix loads RabbitMQ configuration from environment
// variables with an optional prefix, allowing one service to configure
// multiple queues independently (e.g. prefix "AI_" reads AI_RABBITMQ_QUEUE).
// For each variable the prefixed name is checked first, falling back to the
// un-prefixed name, so shared settings (host, credentials) only need to be
// set once. It panics if required variables are missing or invalid.
func NewRabbitMQConfigWithPrefix(prefix string) RabbitMQConfig {
	env := prefixedEnv{prefix: prefix}

	cfg := RabbitMQConfig{
		Host:                  env.required("RABBITMQ_HOST"),
		Port:                  env.requiredInt("RABBITMQ_PORT"),
		User:                  env.required("RABBITMQ_USER"),
		Password:              env.required("RABBITMQ_PASSWORD"),
		Exchange:              env.get("RABBITMQ_EXCHANGE", "contact_messages"),
		Queue:                 env.get("RABBITMQ_QUEUE", "contact_messages"),
		TLS:                   env.bool("RABBITMQ_TLS", false),
		RetryDelays:           parseRetryDelays(env.get("RABBITMQ_RETRY_DELAYS", "")),
		RetryJitter:           env.float("RABBITMQ_RETRY_JITTER", 0),
		Heartbeat:             env.duration("RABBITMQ_HEARTBEAT", DefaultHeartbeat),
		PublisherConfirms:     env.bool("RABBITMQ_PUBLISHER_CONFIRMS", false),
		DisableReconnect:      !env.bool("RABBITMQ_RECONNECT", true),
		ReconnectMaxAttempts:  env.int("RABBITMQ_RECONNECT_MAX_ATTEMPTS", 0),
		ReconnectInitialDelay: env.duration("RABBITMQ_RECONNECT_INITIAL_DELAY", DefaultReconnectInitialDelay),
		ReconnectMaxDelay:     env.duration("RABBITMQ_RECONNECT_MAX_DELAY", DefaultReconnectMaxDelay),
		PrefetchCount:         env.int("RABBITMQ_PREFETCH_COUNT", 1),
		ConsumerTag:           env.get("RABBITMQ_CONSUMER_TAG", ""),
		ConsumerConcurrency:   env.int("RABBITMQ_CONSUMER_CONCURRENCY", 1),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid RabbitMQ configuration: %v", err))
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

// parseRetryDelays parses comma-separated duration strings (e.g., "5s,30s,5m,30m,2h")
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
		delays = append(delays, d)
	}

	if len(delays) == 0 {
		return DefaultRetryDelays()
	}

	return delays
}

// URL returns the AMQP connection URL with properly encoded credentials.
// Uses amqps:// scheme when TLS is enabled, amqp:// otherwise.
func (c RabbitMQConfig) URL() string {
	scheme := "amqp"
	if c.TLS {
		scheme = "amqps"
	}
	u := &url.URL{
		Scheme: scheme,
		User:   url.UserPassword(c.User, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   "/",
	}
	return u.String()
}
