package logger

import (
	"log/slog"
	"time"

	"github.com/adeptry-app/go-common/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestLogger returns a Gin middleware that logs HTTP requests with structured logging
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()

		// Generate request ID
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Get correlation ID from header (for cross-service tracing)
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			correlationID = requestID // Use request ID if no correlation ID provided
		}

		// Add IDs to context
		ctx := AddRequestID(c.Request.Context(), requestID)
		ctx = AddCorrelationID(ctx, correlationID)
		c.Request = c.Request.WithContext(ctx)

		// Add request ID to response headers
		c.Header("X-Request-ID", requestID)
		c.Header("X-Correlation-ID", correlationID)

		// Store logger in context for handlers to use
		c.Set("logger", WithContext(ctx, logger))

		// Process request
		c.Next()

		// Calculate duration
		duration := time.Since(start)

		// Get status code
		status := c.Writer.Status()

		// Build log attributes
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		}

		// Add query params if present
		if c.Request.URL.RawQuery != "" {
			attrs = append(attrs, "query", c.Request.URL.RawQuery)
		}

		// Add user ID if authenticated
		if userID, exists := c.Get(middleware.CtxKeyUserID); exists {
			attrs = append(attrs, "user_id", userID)
			// Also add to context for future use
			if uid, ok := userID.(int64); ok {
				ctx = AddUserID(ctx, uid)
				c.Request = c.Request.WithContext(ctx)
			}
		}

		// Add error if present
		if len(c.Errors) > 0 {
			attrs = append(attrs, "error", c.Errors.String())
		}

		// Log with appropriate level based on status code
		logWithContext := WithContext(ctx, logger)
		if status >= 500 {
			logWithContext.Error("HTTP request failed", attrs...)
		} else if status >= 400 {
			logWithContext.Warn("HTTP request client error", attrs...)
		} else {
			logWithContext.Info("HTTP request completed", attrs...)
		}
	}
}

// GetLogger retrieves the logger from Gin context
func GetLogger(c *gin.Context) *slog.Logger {
	if logger, exists := c.Get("logger"); exists {
		if l, ok := logger.(*slog.Logger); ok {
			return l
		}
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
					"path", c.Request.URL.Path,
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
