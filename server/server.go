// Package server provides HTTP server utilities with graceful shutdown support.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Config holds server configuration.
type Config struct {
	// Port to listen on (default: 8080)
	Port string
	// ShutdownTimeout is the maximum duration to wait for active connections
	// to finish during shutdown (default: 30s)
	ShutdownTimeout time.Duration
	// ReadTimeout is the maximum duration for reading the entire request (default: 30s)
	ReadTimeout time.Duration
	// WriteTimeout is the maximum duration before timing out writes of the response (default: 30s)
	WriteTimeout time.Duration
	// IdleTimeout is the maximum amount of time to wait for the next request (default: 120s)
	IdleTimeout time.Duration
	// ReadHeaderTimeout bounds header reading (default: 10s). Separate from
	// ReadTimeout, which net/http would otherwise reuse: headers arrive before
	// the handler, so they gain nothing from RequestTimeout's socket margin.
	ReadHeaderTimeout time.Duration
	// RequestTimeout is the per-request handler deadline (see
	// middleware.Timeout). When set it OVERRIDES ReadTimeout and WriteTimeout,
	// so the socket cannot expire before the handler it is carrying. Leave it
	// zero to set those two directly.
	RequestTimeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(port string) Config {
	return Config{Port: port}.withDefaults()
}

// requestTimeoutMargin keeps the socket deadlines outside the handler's, so a
// timeout surfaces as the handler's error rather than a cut connection. It
// exceeds handlers.PostCommitTimeout, because post-commit work runs inside the
// handler on a context the request deadline no longer bounds.
const requestTimeoutMargin = 10 * time.Second

// withDefaults fills zero fields; the sole definition of the documented defaults.
func (c Config) withDefaults() Config {
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	// Unconditional: DefaultConfig has already filled the 30s defaults, so a
	// RequestTimeout set afterwards would otherwise be silently ignored.
	if c.RequestTimeout > 0 {
		socket := c.RequestTimeout + requestTimeoutMargin
		// Config is public, so a RequestTimeout near the int64 ceiling would
		// wrap negative here - which net/http reads as an expired deadline.
		if socket < c.RequestTimeout {
			socket = math.MaxInt64
		}
		c.ReadTimeout = socket
		c.WriteTimeout = socket
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 30 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 120 * time.Second
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = 10 * time.Second
	}
	return c
}

// Run starts an HTTP server with graceful shutdown support.
// It blocks until either:
//   - SIGTERM or SIGINT is received (graceful shutdown), or
//   - ListenAndServe fails to start (returns error immediately)
//
// The handler is typically a *gin.Engine or any http.Handler.
func Run(handler http.Handler, cfg Config, logger *slog.Logger) error {
	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	// Guard against nil logger
	if logger == nil {
		logger = slog.Default()
	}

	cfg = cfg.withDefaults()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	// Channel to receive shutdown signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Channel to receive server errors
	serverErr := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		logger.Info("Server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		logger.Info("Shutdown signal received", "signal", sig.String())
	}

	// Create context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Attempt graceful shutdown
	logger.Info("Shutting down server", "timeout", cfg.ShutdownTimeout.String())
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	logger.Info("Server stopped gracefully")
	return nil
}

// RunWithCleanup starts an HTTP server with graceful shutdown and cleanup function support.
// The cleanup function is always called (if non-nil) after Run returns, regardless of whether
// the server shut down gracefully or failed to start. This ensures resources like database
// connections are always released. Use this to close database connections, flush buffers, etc.
func RunWithCleanup(handler http.Handler, cfg Config, logger *slog.Logger, cleanup func()) error {
	// Guard against nil logger for cleanup logging
	if logger == nil {
		logger = slog.Default()
	}

	err := Run(handler, cfg, logger)

	if cleanup != nil {
		logger.Info("Running cleanup")
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Cleanup panicked", "panic", r)
				}
			}()
			cleanup()
		}()
		logger.Info("Cleanup completed")
	}

	return err
}
