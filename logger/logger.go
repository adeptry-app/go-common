package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/adeptry-app/go-common/config"
)

// ContextKey is the type for context keys
type ContextKey string

const (
	// RequestIDKey is the context key for request ID
	RequestIDKey ContextKey = "request_id"
	// CorrelationIDKey is the context key for correlation ID
	CorrelationIDKey ContextKey = "correlation_id"
	// UserIDKey is the context key for user ID
	UserIDKey ContextKey = "user_id"
)

// Config holds logger configuration
type Config struct {
	Level       string // debug, info, warn, error
	Format      string // json, text
	ServiceName string
	AddSource   bool // Add source file and line number
}

// New creates a new structured logger with the given configuration
func New(cfg Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)

	// Add service name as default attribute
	if cfg.ServiceName != "" {
		logger = logger.With("service", cfg.ServiceName)
	}

	return logger
}

// NewFromEnv creates a logger from the standard LOG_LEVEL, LOG_FORMAT, and
// LOG_SOURCE environment variables. A malformed LOG_SOURCE panics at startup.
func NewFromEnv(serviceName string) *slog.Logger {
	return New(Config{
		Level:       config.GetEnv("LOG_LEVEL", ""),
		Format:      config.GetEnv("LOG_FORMAT", ""),
		ServiceName: serviceName,
		AddSource:   config.GetEnvBool("LOG_SOURCE", false),
	})
}

// WithContext creates a logger with context values
func WithContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	attrs := []any{}

	if requestID := ctx.Value(RequestIDKey); requestID != nil {
		attrs = append(attrs, "request_id", requestID)
	}

	if correlationID := ctx.Value(CorrelationIDKey); correlationID != nil {
		attrs = append(attrs, "correlation_id", correlationID)
	}

	if userID := ctx.Value(UserIDKey); userID != nil {
		attrs = append(attrs, "user_id", userID)
	}

	if len(attrs) > 0 {
		return logger.With(attrs...)
	}

	return logger
}

// FromContext retrieves logger from context or returns the default logger
func FromContext(ctx context.Context, defaultLogger *slog.Logger) *slog.Logger {
	return WithContext(ctx, defaultLogger)
}

// AddRequestID adds request ID to context
func AddRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// AddCorrelationID adds correlation ID to context
func AddCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// AddUserID adds user ID to context
func AddUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// GetRequestID retrieves request ID from context
func GetRequestID(ctx context.Context) string {
	if requestID := ctx.Value(RequestIDKey); requestID != nil {
		if id, ok := requestID.(string); ok {
			return id
		}
	}
	return ""
}

// GetCorrelationID retrieves correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if correlationID := ctx.Value(CorrelationIDKey); correlationID != nil {
		if id, ok := correlationID.(string); ok {
			return id
		}
	}
	return ""
}

// GetUserID retrieves user ID from context
func GetUserID(ctx context.Context) int64 {
	if userID := ctx.Value(UserIDKey); userID != nil {
		if id, ok := userID.(int64); ok {
			return id
		}
	}
	return 0
}
