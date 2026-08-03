package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Status represents the health status of a service or dependency
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"

	// DefaultTimeout is used when zero or negative timeout is provided
	DefaultTimeout = 3 * time.Second

	// lateResultGrace is how long the collector keeps taking results after
	// cancelling the checks, so a checker that honours cancellation reports its
	// own reason. It is spent inside the configured timeout, not on top of it.
	lateResultGrace = 50 * time.Millisecond
)

// CheckResult represents the result of a single health check
type CheckResult struct {
	Status  Status         `json:"status"`
	Latency string         `json:"latency,omitempty"`
	Error   string         `json:"error,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// Healthy builds a passing result, timing it from start. Checkers use this so
// Latency is always populated.
func Healthy(start time.Time) CheckResult {
	return CheckResult{Status: StatusHealthy, Latency: time.Since(start).String()}
}

// Unhealthy builds a failing result with a formatted reason, timed from start.
func Unhealthy(start time.Time, format string, args ...any) CheckResult {
	return failing(StatusUnhealthy, start, format, args...)
}

// Degraded builds a working-but-impaired result with a formatted reason.
func Degraded(start time.Time, format string, args ...any) CheckResult {
	return failing(StatusDegraded, start, format, args...)
}

func failing(status Status, start time.Time, format string, args ...any) CheckResult {
	return CheckResult{
		Status:  status,
		Latency: time.Since(start).String(),
		Error:   fmt.Sprintf(format, args...),
	}
}

// Checker is the interface for health check implementations
type Checker interface {
	Name() string
	Check(ctx context.Context) CheckResult
}

// Health represents the overall health status with individual check results
type Health struct {
	Status Status                 `json:"status"`
	Checks map[string]CheckResult `json:"checks"`
}

// record stores one check result and folds it into the overall status.
func (h *Health) record(name string, result CheckResult) {
	h.Checks[name] = result

	switch result.Status {
	case StatusUnhealthy:
		h.Status = StatusUnhealthy
	case StatusDegraded:
		if h.Status != StatusUnhealthy {
			h.Status = StatusDegraded
		}
	}
}

// Aggregator manages multiple health checkers and provides a unified health endpoint
type Aggregator struct {
	checkers []Checker
	timeout  time.Duration
	mu       sync.RWMutex
}

// NewAggregator creates a new health aggregator with the specified timeout for checks.
// If timeout is zero or negative, DefaultTimeout (3s) is used.
func NewAggregator(timeout time.Duration) *Aggregator {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Aggregator{
		checkers: make([]Checker, 0),
		timeout:  timeout,
	}
}

// Register adds a health checker to the aggregator.
// Panics if checker is nil to fail fast on misconfiguration.
func (a *Aggregator) Register(checker Checker) {
	if checker == nil {
		panic("health: cannot register nil checker")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkers = append(a.checkers, checker)
}

// checkOutcome is one checker's verdict, tagged with its registration index so
// two checkers sharing a name still count as two outstanding checks.
type checkOutcome struct {
	index  int
	result CheckResult
}

// Check runs all registered health checks and returns the aggregated result.
// The timeout is a hard deadline: a checker that ignores cancellation cannot
// hold the endpoint open, its slot is reported unhealthy instead.
func (a *Aggregator) Check(ctx context.Context) Health {
	a.mu.RLock()
	checkers := make([]Checker, len(a.checkers))
	copy(checkers, a.checkers)
	a.mu.RUnlock()

	health := Health{
		Status: StatusHealthy,
		Checks: make(map[string]CheckResult),
	}

	if len(checkers) == 0 {
		return health
	}

	start := time.Now()
	grace := a.grace()
	checkCtx, cancel := context.WithTimeout(ctx, a.timeout-grace)
	defer cancel()

	// One slot per checker, so a checker that finishes after the deadline hands
	// its result over and exits rather than blocking on a reader that is gone.
	results := make(chan checkOutcome, len(checkers))
	pending := make(map[int]string, len(checkers))

	for i, checker := range checkers {
		pending[i] = checker.Name()
		go func() {
			results <- checkOutcome{index: i, result: checker.Check(checkCtx)}
		}()
	}

	for len(pending) > 0 {
		select {
		case r := <-results:
			health.record(pending[r.index], r.result)
			delete(pending, r.index)
		case <-checkCtx.Done():
			// Read the caller's state now: a disconnect during the grace window
			// must not be mistaken for the reason the checks were cut short.
			a.expire(&health, pending, results, start, grace, ctx.Err())
			return health
		}
	}

	return health
}

// grace is the share of the timeout reserved for cancelled checkers to report,
// so the whole run still fits inside what the caller configured.
func (a *Aggregator) grace() time.Duration {
	if half := a.timeout / 2; half < lateResultGrace {
		return half
	}
	return lateResultGrace
}

// expire records whatever the cancelled checkers still report within the grace
// window, then fails every check that never reported at all. callerErr is the
// caller's context error when it, rather than the timeout, ended the run.
func (a *Aggregator) expire(health *Health, pending map[int]string, results <-chan checkOutcome, start time.Time, grace time.Duration, callerErr error) {
	window := time.NewTimer(grace)
	defer window.Stop()

	for len(pending) > 0 {
		select {
		case r := <-results:
			health.record(pending[r.index], r.result)
			delete(pending, r.index)
		case <-window.C:
			expired := Unhealthy(start, "check did not complete within %s", a.timeout)
			if callerErr != nil {
				expired = Unhealthy(start, "check cancelled: %v", callerErr)
			}
			for _, name := range pending {
				health.record(name, expired)
			}
			return
		}
	}
}

// Handler returns a gin.HandlerFunc for the health endpoint
func (a *Aggregator) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		health := a.Check(c.Request.Context())

		statusCode := http.StatusOK
		if health.Status != StatusHealthy {
			statusCode = http.StatusServiceUnavailable
		}

		c.JSON(statusCode, health)
	}
}
