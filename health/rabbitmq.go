package health

import (
	"context"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQChecker checks RabbitMQ connection status
type RabbitMQChecker struct {
	provider func() *amqp.Connection
}

// NewRabbitMQChecker creates a RabbitMQ health checker for a fixed connection.
//
// Note: when the connection owner reconnects automatically (the queue
// package's default), the pointer captured here goes stale and the check
// reports unhealthy forever after the first reconnect. Use
// NewRabbitMQCheckerWithProvider in that case.
func NewRabbitMQChecker(conn *amqp.Connection) Checker {
	return &RabbitMQChecker{provider: func() *amqp.Connection { return conn }}
}

// NewRabbitMQCheckerWithProvider creates a RabbitMQ health checker that
// resolves the connection on every check, so it stays accurate across
// reconnects. Pass the publisher's or consumer's Connection method:
//
//	healthAgg.Register(health.NewRabbitMQCheckerWithProvider(publisher.Connection))
func NewRabbitMQCheckerWithProvider(provider func() *amqp.Connection) Checker {
	if provider == nil {
		provider = func() *amqp.Connection { return nil }
	}
	return &RabbitMQChecker{provider: provider}
}

// Name returns the name of this checker
func (c *RabbitMQChecker) Name() string {
	return "rabbitmq"
}

// Check verifies the RabbitMQ connection is open
func (c *RabbitMQChecker) Check(_ context.Context) CheckResult {
	start := time.Now()

	conn := c.provider()
	if conn == nil {
		return Unhealthy(start, "connection is nil")
	}

	if conn.IsClosed() {
		return Unhealthy(start, "connection is closed")
	}

	return Healthy(start)
}

// QueueDepthChecker reports the number of messages in a queue (typically a
// DLQ) so growth is visible on the health endpoint.
type QueueDepthChecker struct {
	provider          func() *amqp.Connection
	queue             string
	degradedThreshold int
}

// NewQueueDepthChecker creates a checker that reports the message count of
// queueName under "details.messages". With degradedThreshold > 0 the check
// reports degraded once the depth reaches the threshold; with 0 it only
// reports the depth and stays healthy. Note that the default health handler
// returns HTTP 503 for degraded, so only set a threshold where that is
// acceptable.
//
//	healthAgg.Register(health.NewQueueDepthChecker(publisher.Connection, publisher.DLQName(), 0))
func NewQueueDepthChecker(provider func() *amqp.Connection, queueName string, degradedThreshold int) Checker {
	if provider == nil {
		provider = func() *amqp.Connection { return nil }
	}
	return &QueueDepthChecker{
		provider:          provider,
		queue:             queueName,
		degradedThreshold: degradedThreshold,
	}
}

// Name returns the name of this checker, unique per monitored queue.
func (c *QueueDepthChecker) Name() string {
	return "queue:" + c.queue
}

// Check inspects the queue and reports its message count.
func (c *QueueDepthChecker) Check(_ context.Context) CheckResult {
	start := time.Now()

	conn := c.provider()
	if conn == nil || conn.IsClosed() {
		return Unhealthy(start, "connection unavailable")
	}

	// A short-lived channel per check keeps the checker independent of the
	// owner's channel lifecycle.
	ch, err := conn.Channel()
	if err != nil {
		return Unhealthy(start, "open channel: %v", err)
	}
	defer func() { _ = ch.Close() }()

	// Passive declare returns queue info without modifying the topology;
	// the server ignores the flags and only checks existence.
	queue, err := ch.QueueDeclarePassive(c.queue, true, false, false, false, nil)
	if err != nil {
		return Unhealthy(start, "inspect queue: %v", err)
	}

	result := Healthy(start)
	if c.degradedThreshold > 0 && queue.Messages >= c.degradedThreshold {
		result = Degraded(start, "queue depth %d reached threshold %d", queue.Messages, c.degradedThreshold)
	}
	result.Details = map[string]any{"messages": queue.Messages}
	return result
}

// ConsumerChecker reports whether messages are still being consumed.
type ConsumerChecker struct {
	provider func() error
}

// NewConsumerChecker creates a checker that fails while consumption is stopped
// or stuck retrying, for a reason other than shutdown. The broker connection
// check stays healthy in both cases, so a worker needs both. Pass the method:
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
