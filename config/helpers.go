package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// unprefixed reads plain environment variables through the same helpers the
// prefixed (RabbitMQ) loader uses, so both share one set of parsing rules.
var unprefixed = prefixedEnv{}

// GetEnv returns environment variable value or default if not set
func GetEnv(key, defaultValue string) string {
	return unprefixed.get(key, defaultValue)
}

// GetEnvRequired returns environment variable value or panics if not set
func GetEnvRequired(key string) string {
	return unprefixed.required(key)
}

// GetEnvRequiredInt returns environment variable as an integer, panicking when
// it is unset or unparseable.
func GetEnvRequiredInt(key string) int {
	return unprefixed.requiredInt(key)
}

// GetEnvRequiredList splits a required comma-separated variable, dropping blank
// entries. It panics when the variable is unset or holds no usable entry.
func GetEnvRequiredList(key string) []string {
	raw := strings.Split(unprefixed.required(key), ",")
	list := make([]string, 0, len(raw))
	for _, entry := range raw {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			list = append(list, trimmed)
		}
	}

	if len(list) == 0 {
		panic(fmt.Sprintf("Required environment variable %s has no entries", key))
	}
	return list
}

// GetEnvBool returns environment variable as boolean or default if not set
// (empty or whitespace-only counts as unset). Accepted values are
// case-insensitive true/false/1/0 with surrounding whitespace ignored; any
// other value panics at startup so a typo cannot silently flip a flag.
func GetEnvBool(key string, defaultValue bool) bool {
	return unprefixed.bool(key, defaultValue)
}

// parseBool interprets the accepted boolean forms: case-insensitive
// true/false/1/0. ok is false for anything else. Deliberately narrower than
// strconv.ParseBool: the single-letter t/f forms are cryptic in env files
// and widen the accepted-typo surface.
func parseBool(val string) (value, ok bool) {
	switch strings.ToLower(val) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}

// GetEnvInt returns environment variable as integer or default if not set.
// A malformed value panics at startup rather than silently using the default.
func GetEnvInt(key string, defaultValue int) int {
	return parsedEnv(key, defaultValue, strconv.Atoi)
}

// GetEnvInt64 returns environment variable as int64 or default if not set.
// A malformed value panics at startup rather than silently using the default.
func GetEnvInt64(key string, defaultValue int64) int64 {
	return parsedEnv(key, defaultValue, func(s string) (int64, error) {
		return strconv.ParseInt(s, 10, 64)
	})
}

// GetEnvDuration returns environment variable as time.Duration or default if not set.
// Expected format: Go duration strings (e.g., "15m", "1h30m", "24h", "168h").
// See https://pkg.go.dev/time#ParseDuration for full format specification.
// A malformed value panics at startup rather than silently using the default.
func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	return parsedEnv(key, defaultValue, time.ParseDuration)
}

// parsedEnv reads key, falling back to defaultValue when unset and panicking
// when set but unparseable, matching GetEnvBool's fail-loud contract.
func parsedEnv[T any](key string, defaultValue T, parse func(string) (T, error)) T {
	val := strings.TrimSpace(GetEnv(key, ""))
	if val == "" {
		return defaultValue
	}
	parsed, err := parse(val)
	if err != nil {
		panic(fmt.Sprintf("Invalid value for %s: %q (%v)", key, val, err))
	}
	return parsed
}
