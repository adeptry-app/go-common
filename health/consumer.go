package health

import (
	"context"
	"time"
)

// ConsumerChecker reports whether messages are still being consumed.
type ConsumerChecker struct {
	provider func() error
}

// NewConsumerChecker creates a checker that fails while consumption is stopped
// or stuck retrying, for a reason other than shutdown. A worker that is
// receiving nothing has no connection to lose, so nothing else reveals it.
// Pass the method:
//
//	healthAgg.Register(health.NewConsumerChecker(consumer.ConsumptionError))
func NewConsumerChecker(provider func() error) Checker {
	return &ConsumerChecker{provider: provider}
}

// Name returns the name of this checker
func (c *ConsumerChecker) Name() string {
	return "consumer"
}

// Check reports unhealthy while consumption is not running as expected.
func (c *ConsumerChecker) Check(_ context.Context) CheckResult {
	start := time.Now()

	if c.provider == nil {
		return Unhealthy(start, "consumption state unavailable")
	}
	if err := c.provider(); err != nil {
		return Unhealthy(start, "consumption stopped: %v", err)
	}

	return Healthy(start)
}
