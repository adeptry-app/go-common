package logger

import (
	"log/slog"
	"time"

	"github.com/adeptry-app/go-common/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ctxKeyLogger is the gin key holding the request-scoped logger; GetLogger is
// the only reader.
const ctxKeyLogger = "logger"

// RequestLogger returns a Gin middleware that logs HTTP requests with structured logging
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()

		// Generate request ID
		requestID := c.GetHeader(HeaderRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Get correlation ID from header (for cross-service tracing)
		correlationID := c.GetHeader(HeaderCorrelationID)
		if correlationID == "" {
			correlationID = requestID // Use request ID if no correlation ID provided
		}

		// Add IDs to context
		ctx := AddRequestID(c.Request.Context(), requestID)
		ctx = AddCorrelationID(ctx, correlationID)
		c.Request = c.Request.WithContext(ctx)

		// Add request ID to response headers
		c.Header(HeaderRequestID, requestID)
		c.Header(HeaderCorrelationID, correlationID)

		// Store logger in context for handlers to use
		requestLogger := WithContext(ctx, logger)
		c.Set(ctxKeyLogger, requestLogger)

		// Process request
		c.Next()

		// Calculate duration
		duration := time.Since(start)

		// Get status code
		status := c.Writer.Status()

		// Build log attributes
		attrs := []any{
			"method", c.Request.Method,
			"route", middleware.RouteTemplate(c),
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		}

		// Add query params if present
		if c.Request.URL.RawQuery != "" {
			attrs = append(attrs, "query", c.Request.URL.RawQuery)
		}

		// Add user ID if authenticated. Not added to ctx: the request is over,
		// so WithContext below would only duplicate the attribute.
		if id, ok := middleware.GetIdentity(c); ok {
			attrs = append(attrs, "user_id", id.UserID)
		}

		// Add error if present
		if len(c.Errors) > 0 {
			attrs = append(attrs, "error", c.Errors.String())
		}

		// Log with appropriate level based on status code
		if status >= 500 {
			requestLogger.Error("HTTP request failed", attrs...)
		} else if status >= 400 {
			requestLogger.Warn("HTTP request client error", attrs...)
		} else {
			requestLogger.Info("HTTP request completed", attrs...)
		}
	}
}

// GetLogger retrieves the logger from Gin context
func GetLogger(c *gin.Context) *slog.Logger {
	v, _ := c.Get(ctxKeyLogger)
	if l, ok := v.(*slog.Logger); ok {
		return l
	}
	// Fallback to default logger
	return slog.Default()
}

// Recovery returns a Gin middleware that recovers from panics and logs them
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get logger with context
				logWithContext := GetLogger(c)

				logWithContext.Error("Panic recovered",
					"error", err,
					"method", c.Request.Method,
					"route", middleware.RouteTemplate(c),
				)

				// Abort with internal server error
				c.AbortWithStatusJSON(500, gin.H{
					"error": "internal server error",
				})
			}
		}()
		c.Next()
	}
}
