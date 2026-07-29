package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newTimeoutRouter(d time.Duration, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Timeout(d))
	router.GET("/resource", handler)
	return router
}

func serveTimeoutRequest(router *gin.Engine) {
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/resource", nil))
}

func TestTimeout_SetsDeadline(t *testing.T) {
	const d = time.Minute
	var deadline time.Time
	var ok bool

	serveTimeoutRequest(newTimeoutRouter(d, func(c *gin.Context) {
		deadline, ok = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	}))

	if !ok {
		t.Fatal("handler context carries no deadline")
	}
	// Two-sided, or an implementation ignoring d and hardcoding something
	// smaller would pass.
	if remaining := time.Until(deadline); remaining > d || remaining < d-time.Second {
		t.Errorf("deadline is %v away, want ~%v", remaining, d)
	}
}

// A non-positive duration must disable the deadline, not expire every request
// before its handler runs.
func TestTimeout_NonPositiveDisablesTheDeadline(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		t.Run(d.String(), func(t *testing.T) {
			var ran bool
			var hasDeadline bool

			serveTimeoutRequest(newTimeoutRouter(d, func(c *gin.Context) {
				ran = true
				_, hasDeadline = c.Request.Context().Deadline()
				c.Status(http.StatusOK)
			}))

			if !ran {
				t.Fatal("handler did not run")
			}
			if hasDeadline {
				t.Error("a non-positive duration must leave the context unbounded")
			}
		})
	}
}

// The deadline has to reach the pool, or a query outlives the request that
// wanted it and keeps holding a connection.
func TestTimeout_CancelsHandlerWork(t *testing.T) {
	var err error

	serveTimeoutRequest(newTimeoutRouter(10*time.Millisecond, func(c *gin.Context) {
		<-c.Request.Context().Done()
		err = c.Request.Context().Err()
		c.Status(http.StatusOK)
	}))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("handler context error = %v, want %v", err, context.DeadlineExceeded)
	}
}

// Cancel must fire once the handler returns, so a finished request stops
// pinning its context.
func TestTimeout_CancelsAfterHandlerReturns(t *testing.T) {
	var ctx context.Context

	serveTimeoutRequest(newTimeoutRouter(time.Minute, func(c *gin.Context) {
		ctx = c.Request.Context()
		c.Status(http.StatusOK)
	}))

	if ctx.Err() == nil {
		t.Error("context should be cancelled once the request completed")
	}
}
