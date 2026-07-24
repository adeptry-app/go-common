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

// Check runs all registered health checks and returns the aggregated result
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

	// Create context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Run checks concurrently
	type checkResultWithName struct {
		name   string
		result CheckResult
	}

	results := make(chan checkResultWithName, len(checkers))
	var wg sync.WaitGroup

	for _, checker := range checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()
			result := c.Check(checkCtx)
			results <- checkResultWithName{
				name:   c.Name(),
				result: result,
			}
		}(checker)
	}

	// Wait for all checks to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for r := range results {
		health.Checks[r.name] = r.result

		// Update overall status based on individual check results
		switch r.result.Status {
		case StatusUnhealthy:
			health.Status = StatusUnhealthy
		case StatusDegraded:
			if health.Status != StatusUnhealthy {
				health.Status = StatusDegraded
			}
		}
	}

	return health
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
