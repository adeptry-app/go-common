package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// mockChecker is a test helper for simulating health checks
type mockChecker struct {
	name   string
	result CheckResult
	delay  time.Duration
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context) CheckResult {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return CheckResult{
				Status: StatusUnhealthy,
				Error:  "context cancelled",
			}
		}
	}
	return m.result
}

func TestNewAggregator(t *testing.T) {
	agg := NewAggregator(5 * time.Second)

	if agg.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", agg.timeout)
	}
	if len(agg.checkers) != 0 {
		t.Errorf("expected no checkers, got %d", len(agg.checkers))
	}
}

func TestAggregator_Register(t *testing.T) {
	agg := NewAggregator(5 * time.Second)
	checker := &mockChecker{name: "test"}

	agg.Register(checker)

	if len(agg.checkers) != 1 {
		t.Errorf("expected 1 checker, got %d", len(agg.checkers))
	}
}

func TestAggregator_Check_NoCheckers(t *testing.T) {
	agg := NewAggregator(5 * time.Second)

	health := agg.Check(context.Background())

	if health.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", health.Status)
	}
	if len(health.Checks) != 0 {
		t.Errorf("expected no checks, got %d", len(health.Checks))
	}
}

func TestAggregator_Check_AllHealthy(t *testing.T) {
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusHealthy, Latency: "1ms"},
	})
	agg.Register(&mockChecker{
		name:   "cache",
		result: CheckResult{Status: StatusHealthy, Latency: "2ms"},
	})

	health := agg.Check(context.Background())

	if health.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", health.Status)
	}
	if len(health.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(health.Checks))
	}
	if health.Checks["db"].Status != StatusHealthy {
		t.Errorf("expected db healthy, got %s", health.Checks["db"].Status)
	}
	if health.Checks["cache"].Status != StatusHealthy {
		t.Errorf("expected cache healthy, got %s", health.Checks["cache"].Status)
	}
}

func TestAggregator_Check_OneUnhealthy(t *testing.T) {
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusHealthy, Latency: "1ms"},
	})
	agg.Register(&mockChecker{
		name:   "cache",
		result: CheckResult{Status: StatusUnhealthy, Error: "connection refused"},
	})

	health := agg.Check(context.Background())

	if health.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", health.Status)
	}
	if health.Checks["cache"].Status != StatusUnhealthy {
		t.Errorf("expected cache unhealthy, got %s", health.Checks["cache"].Status)
	}
}

func TestAggregator_Check_OneDegraded(t *testing.T) {
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusHealthy, Latency: "1ms"},
	})
	agg.Register(&mockChecker{
		name:   "cache",
		result: CheckResult{Status: StatusDegraded, Error: "high latency"},
	})

	health := agg.Check(context.Background())

	if health.Status != StatusDegraded {
		t.Errorf("expected degraded status, got %s", health.Status)
	}
}

func TestAggregator_Check_UnhealthyOverridesDegraded(t *testing.T) {
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusDegraded},
	})
	agg.Register(&mockChecker{
		name:   "cache",
		result: CheckResult{Status: StatusUnhealthy},
	})

	health := agg.Check(context.Background())

	if health.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy (overrides degraded), got %s", health.Status)
	}
}

func TestAggregator_Check_Timeout(t *testing.T) {
	agg := NewAggregator(50 * time.Millisecond)
	agg.Register(&mockChecker{
		name:  "slow",
		delay: 200 * time.Millisecond,
		result: CheckResult{
			Status:  StatusHealthy,
			Latency: "200ms",
		},
	})

	health := agg.Check(context.Background())

	if health.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy due to timeout, got %s", health.Status)
	}
	if health.Checks["slow"].Error != "context cancelled" {
		t.Errorf("expected 'context cancelled' error, got %s", health.Checks["slow"].Error)
	}
}

// blockedChecker ignores cancellation, standing in for a dependency call that
// never returns until the test releases it.
type blockedChecker struct {
	name    string
	release chan struct{}
}

func (b *blockedChecker) Name() string { return b.name }

func (b *blockedChecker) Check(context.Context) CheckResult {
	<-b.release
	return CheckResult{Status: StatusHealthy}
}

func TestAggregator_Check_UncooperativeCheckerCannotBlockForever(t *testing.T) {
	blocked := &blockedChecker{name: "stuck", release: make(chan struct{})}
	defer close(blocked.release)

	agg := NewAggregator(50 * time.Millisecond)
	agg.Register(blocked)
	agg.Register(&mockChecker{name: "db", result: CheckResult{Status: StatusHealthy}})

	start := time.Now()
	health := agg.Check(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("Check() took %v, want it to return at the deadline", elapsed)
	}
	if health.Status != StatusUnhealthy {
		t.Errorf("status = %s, want %s", health.Status, StatusUnhealthy)
	}
	if got := health.Checks["stuck"].Error; got != "check did not complete within 50ms" {
		t.Errorf("stuck error = %q, want the timeout reason", got)
	}
	if health.Checks["db"].Status != StatusHealthy {
		t.Errorf("db status = %s, want %s", health.Checks["db"].Status, StatusHealthy)
	}
}

func TestAggregator_Grace_FitsInsideTheConfiguredTimeout(t *testing.T) {
	tests := []struct {
		timeout time.Duration
		want    time.Duration
	}{
		{3 * time.Second, lateResultGrace},
		{100 * time.Millisecond, lateResultGrace},
		{50 * time.Millisecond, 25 * time.Millisecond},
		{10 * time.Millisecond, 5 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.timeout.String(), func(t *testing.T) {
			agg := NewAggregator(tt.timeout)
			if got := agg.grace(); got != tt.want {
				t.Errorf("grace() = %v, want %v", got, tt.want)
			}
			if got := agg.grace(); got > tt.timeout {
				t.Errorf("grace() = %v, longer than the whole timeout %v", got, tt.timeout)
			}
		})
	}
}

func TestAggregator_Check_LateCheckerDoesNotLeak(t *testing.T) {
	blocked := &blockedChecker{name: "stuck", release: make(chan struct{})}

	agg := NewAggregator(50 * time.Millisecond)
	before := runtime.NumGoroutine()
	agg.Register(blocked)

	agg.Check(context.Background())
	close(blocked.release)

	// The late sender must hand its result to the buffer and exit.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Errorf("goroutines = %d, want at most %d: the late result blocked its sender", got, before)
	}
}

func TestAggregator_Handler_Healthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusHealthy, Latency: "1ms"},
	})

	router := gin.New()
	router.GET("/health", agg.Handler())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestAggregator_Handler_Unhealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusUnhealthy, Error: "connection refused"},
	})

	router := gin.New()
	router.GET("/health", agg.Handler())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestAggregator_Handler_Degraded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agg := NewAggregator(5 * time.Second)
	agg.Register(&mockChecker{
		name:   "db",
		result: CheckResult{Status: StatusDegraded, Error: "high latency"},
	})

	router := gin.New()
	router.GET("/health", agg.Handler())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 for degraded, got %d", w.Code)
	}
}

func TestAggregator_Register_Concurrent(t *testing.T) {
	agg := NewAggregator(5 * time.Second)
	const numGoroutines = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			agg.Register(&mockChecker{
				name:   fmt.Sprintf("checker-%d", id),
				result: CheckResult{Status: StatusHealthy},
			})
		}(i)
	}

	wg.Wait()

	if len(agg.checkers) != numGoroutines {
		t.Errorf("expected %d checkers, got %d", numGoroutines, len(agg.checkers))
	}
}

func TestNewAggregator_ZeroTimeout(t *testing.T) {
	agg := NewAggregator(0)

	if agg.timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v for zero input, got %v", DefaultTimeout, agg.timeout)
	}
}

func TestNewAggregator_NegativeTimeout(t *testing.T) {
	agg := NewAggregator(-5 * time.Second)

	if agg.timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v for negative input, got %v", DefaultTimeout, agg.timeout)
	}
}

func TestAggregator_Register_NilChecker(t *testing.T) {
	agg := NewAggregator(5 * time.Second)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when registering nil checker")
		}
	}()

	agg.Register(nil)
}
