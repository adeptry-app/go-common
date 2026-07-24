package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
