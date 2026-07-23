package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
	amqp "github.com/rabbitmq/amqp091-go"
)

// RetryCountHeader is the AMQP header key for tracking retry attempts
const RetryCountHeader = "x-retry-count"

// Consumer errors
var (
	ErrConsumerClosed        = errors.New("consumer is closed")
	ErrConsumeSetupFailed    = errors.New("failed to setup consumer")
	ErrNilPublisher          = errors.New("publisher is required")
	ErrAlreadyConsuming      = errors.New("consumer is already consuming")
	ErrDeliveryChannelClosed = errors.New("delivery channel closed")
	ErrReconnectFailed       = errors.New("reconnect attempts exhausted")
)

// MessageHandler processes a single message delivery.
// Return nil to ACK the message, return error to trigger retry logic.
// Wrap errors with Permanent() to skip retries and go straight to the DLQ.
// The context carries the message correlation ID (logger.GetCorrelationID).
//
// A panic inside the handler does not crash the process: it is recovered,
// logged with the stack, and treated as a transient handler error that rides
// the retry ladder (reaching the DLQ once retries are exhausted).
type MessageHandler func(ctx context.Context, delivery amqp.Delivery) error

// Consumer defines the interface for message queue consuming
type Consumer interface {
	// Consume starts consuming messages and blocks until context is cancelled
	Consume(ctx context.Context, handler MessageHandler) error
	// Close stops consuming and closes connections
	Close() error
}

// RabbitMQConsumer implements Consumer for RabbitMQ
type RabbitMQConsumer struct {
	mu        sync.Mutex
	closed    bool
	consuming bool
	runDone   chan struct{}
	conn      *amqp.Connection
	channel   *amqp.Channel
	publisher *RabbitMQPublisher
	config    config.RabbitMQConfig
	logger    *slog.Logger
	metrics   MetricsRecorder
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewRabbitMQConsumer creates a new consumer that shares queue infrastructure with the publisher.
// The publisher must be created first as it declares all queues.
// If logger is nil, slog.Default() is used.
func NewRabbitMQConsumer(
	cfg config.RabbitMQConfig,
	publisher *RabbitMQPublisher,
	logger *slog.Logger,
	opts ...ConsumerOption,
) (*RabbitMQConsumer, error) {
	if publisher == nil {
		return nil, ErrNilPublisher
	}
	if logger == nil {
		logger = slog.Default()
	}

	c := &RabbitMQConsumer{
		publisher: publisher,
		config:    cfg.WithDefaults(),
		logger:    logger,
		metrics:   noopMetrics{},
		stopCh:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}

	// Dial with the normalized config so the initial connection matches the
	// reconnect path in setupConsume.
	conn, err := dial(c.config)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrChannelFailed, err)
	}

	// Set QoS for fair dispatch
	if err := ch.Qos(c.config.PrefetchCount, 0, false); err != nil {
		_ = closeResources(ch, conn)
		return nil, fmt.Errorf("%w: set qos: %v", ErrConsumeSetupFailed, err)
	}

	c.conn = conn
	c.channel = ch
	return c, nil
}

func (c *RabbitMQConsumer) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// receiveLoop exit reasons.
const (
	exitCtx = iota
	exitStop
	exitStream
)

// Consume starts consuming messages from the queue and blocks until the
// context is cancelled or Close is called.
//
// Unlike the publisher (whose supervisor goroutine reconnects in the
// background), reconnection runs inline in this loop because in-flight
// handlers must be drained before the channel they ack on is replaced.
//
// Unless cfg.DisableReconnect is set, a dropped connection or channel is
// re-established with exponential backoff and consumption resumes; Consume
// then only returns ctx.Err() on cancellation, ErrConsumerClosed after
// Close, or ErrReconnectFailed when cfg.ReconnectMaxAttempts is exceeded.
// With reconnection disabled, it returns ErrDeliveryChannelClosed when the
// broker connection drops (the previous behavior).
//
// Messages are processed with cfg.ConsumerConcurrency parallel handlers
// (default 1, sequential). Consume waits for in-flight handlers to finish
// before returning. Only one Consume may be active per consumer; concurrent
// calls return ErrAlreadyConsuming.
//
// The handler is called for each message; return nil to ACK, an error to
// trigger the retry ladder, or a Permanent()-wrapped error to send the
// message straight to the DLQ. If the context is cancelled while a message
// is being handled, the message is requeued without consuming a retry
// attempt.
func (c *RabbitMQConsumer) Consume(ctx context.Context, handler MessageHandler) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrConsumerClosed
	}
	if c.consuming {
		c.mu.Unlock()
		return ErrAlreadyConsuming
	}
	c.consuming = true
	runDone := make(chan struct{})
	c.runDone = runDone
	c.mu.Unlock()

	sem := make(chan struct{}, c.config.ConsumerConcurrency)
	var wg sync.WaitGroup

	defer func() {
		wg.Wait()
		c.teardown()
		c.mu.Lock()
		c.consuming = false
		c.mu.Unlock()
		close(runDone)
	}()

	if c.config.ConsumerConcurrency > c.config.PrefetchCount {
		c.logger.Warn("ConsumerConcurrency exceeds PrefetchCount; effective parallelism is capped by prefetch",
			"concurrency", c.config.ConsumerConcurrency,
			"prefetch", c.config.PrefetchCount,
		)
	}

	attempt := 0
	for {
		deliveries, err := c.setupConsume()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if c.isClosed() {
				return ErrConsumerClosed
			}
			if c.config.DisableReconnect {
				return err
			}
			attempt++
			if c.config.ReconnectMaxAttempts > 0 && attempt > c.config.ReconnectMaxAttempts {
				return fmt.Errorf("%w: %v", ErrReconnectFailed, err)
			}
			c.logger.Warn("Consumer setup failed, retrying",
				"queue", c.config.Queue,
				"attempt", attempt,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.stopCh:
				return ErrConsumerClosed
			case <-time.After(reconnectDelay(c.config, attempt)):
			}
			continue
		}
		attempt = 0

		c.logger.Info("Consumer started", "queue", c.config.Queue, "tag", c.config.ConsumerTag)

		switch c.receiveLoop(ctx, deliveries, handler, sem, &wg) {
		case exitCtx:
			c.logger.Info("Consumer stopping", "reason", ctx.Err())
			return ctx.Err()
		case exitStop:
			c.logger.Info("Consumer stopping", "reason", "closed")
			return ErrConsumerClosed
		case exitStream:
			if c.config.DisableReconnect {
				c.logger.Warn("Delivery channel closed")
				return ErrDeliveryChannelClosed
			}
			c.logger.Warn("Delivery channel closed, reconnecting", "queue", c.config.Queue)
			c.metrics.RecordReconnect("consumer")
			// Let in-flight handlers finish before tearing down the channel
			// they ack on.
			wg.Wait()
			c.teardown()
		}
	}
}

// setupConsume ensures a live connection and channel, re-declares the
// topology (idempotent, recovers queues after a broker data loss), applies
// QoS, and starts delivery.
func (c *RabbitMQConsumer) setupConsume() (<-chan amqp.Delivery, error) {
	c.mu.Lock()
	conn, ch := c.conn, c.channel
	c.mu.Unlock()

	if conn == nil || conn.IsClosed() {
		c.teardown()
		newConn, err := dial(c.config)
		if err != nil {
			return nil, err
		}
		conn = newConn
		ch = nil
	}

	if ch == nil || ch.IsClosed() {
		newCh, err := conn.Channel()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: %v", ErrChannelFailed, err)
		}
		ch = newCh
	}

	cleanup := func() { _ = closeResources(ch, conn) }

	if _, err := declareTopology(ch, c.config); err != nil {
		cleanup()
		return nil, err
	}

	if err := ch.Qos(c.config.PrefetchCount, 0, false); err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: set qos: %v", ErrConsumeSetupFailed, err)
	}

	deliveries, err := ch.Consume(
		c.config.Queue,
		c.config.ConsumerTag,
		false, // auto-ack disabled for manual control
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: consume: %v", ErrConsumeSetupFailed, err)
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		cleanup()
		return nil, ErrConsumerClosed
	}
	c.conn = conn
	c.channel = ch
	c.mu.Unlock()

	return deliveries, nil
}

// receiveLoop dispatches deliveries to handler goroutines bounded by sem
// until the context is cancelled, the consumer is closed, or the delivery
// stream closes.
func (c *RabbitMQConsumer) receiveLoop(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	handler MessageHandler,
	sem chan struct{},
	wg *sync.WaitGroup,
) int {
	for {
		select {
		case <-ctx.Done():
			return exitCtx
		case <-c.stopCh:
			return exitStop
		case delivery, ok := <-deliveries:
			if !ok {
				return exitStream
			}

			// Acquire a concurrency slot before dispatching so slot order
			// follows delivery order.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				c.requeue(delivery)
				return exitCtx
			case <-c.stopCh:
				c.requeue(delivery)
				return exitStop
			}

			wg.Add(1)
			go func(d amqp.Delivery) {
				defer wg.Done()
				defer func() { <-sem }()
				c.processDelivery(ctx, d, handler)
			}(delivery)
		}
	}
}

// requeue returns an unprocessed delivery to the queue during shutdown.
func (c *RabbitMQConsumer) requeue(delivery amqp.Delivery) {
	if err := delivery.Nack(false, true); err != nil {
		c.logger.Error("Failed to NACK message for requeue", "error", err, "messageId", delivery.MessageId)
	}
}

// processDelivery handles a single message with retry logic
func (c *RabbitMQConsumer) processDelivery(ctx context.Context, delivery amqp.Delivery, handler MessageHandler) {
	retryCount := GetRetryCount(delivery)

	// Propagate the message correlation ID so handler logs line up with the
	// publishing request.
	if delivery.CorrelationId != "" {
		ctx = logger.AddCorrelationID(ctx, delivery.CorrelationId)
	}

	c.logger.Debug("Processing message",
		"messageId", delivery.MessageId,
		"correlationId", delivery.CorrelationId,
		"retryCount", retryCount,
	)

	start := time.Now()
	err := c.invokeHandler(ctx, delivery, handler)
	duration := time.Since(start)

	if err == nil {
		// Success - ACK
		if ackErr := delivery.Ack(false); ackErr != nil {
			c.logger.Error("Failed to ACK message", "error", ackErr, "messageId", delivery.MessageId)
		}
		c.metrics.RecordConsume(c.config.Queue, OutcomeSuccess, duration)
		return
	}

	// Shutdown in progress - the failure is most likely caused by the
	// cancelled context, so requeue without consuming a retry attempt.
	if ctx.Err() != nil {
		c.logger.Info("Requeueing message due to shutdown",
			"messageId", delivery.MessageId,
			"error", err,
		)
		c.requeue(delivery)
		c.metrics.RecordConsume(c.config.Queue, OutcomeRequeued, duration)
		return
	}

	// Permanent failure - NACK without requeue routes to the DLQ via the
	// main queue's dead-letter configuration.
	if errors.Is(err, ErrPermanent) {
		c.logger.Warn("Permanent failure, sending to DLQ",
			"error", err,
			"messageId", delivery.MessageId,
			"retryCount", retryCount,
		)
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			c.logger.Error("Failed to NACK message", "error", nackErr)
		}
		c.metrics.RecordConsume(c.config.Queue, OutcomeDLQ, duration)
		return
	}

	// Handler returned error - determine retry or DLQ
	c.logger.Warn("Handler failed",
		"error", err,
		"messageId", delivery.MessageId,
		"retryCount", retryCount,
		"maxRetries", c.publisher.MaxRetries(),
	)

	if WillRetry(delivery, c.publisher.MaxRetries()) {
		// Try to republish to retry queue first (before ACK)
		if pubErr := c.publishToRetryWithCount(ctx, retryCount, delivery); pubErr != nil {
			// Publish failed - NACK to requeue for redelivery
			c.logger.Error("Failed to publish to retry queue, requeueing",
				"error", pubErr,
				"retryIndex", retryCount,
				"messageId", delivery.MessageId,
			)
			c.requeue(delivery)
			c.metrics.RecordConsume(c.config.Queue, OutcomeRequeued, duration)
			return
		}

		// Publish succeeded - now ACK the original
		if ackErr := delivery.Ack(false); ackErr != nil {
			// At-least-once delivery: the message is already in the retry
			// queue and the broker will redeliver this copy, so a duplicate
			// is possible. Handlers must be idempotent.
			c.logger.Error("Failed to ACK after retry publish", "error", ackErr)
			c.metrics.RecordConsume(c.config.Queue, OutcomeRetry, duration)
			return
		}

		c.logger.Info("Message queued for retry",
			"messageId", delivery.MessageId,
			"retryIndex", retryCount,
			"nextRetryCount", retryCount+1,
		)
		c.metrics.RecordConsume(c.config.Queue, OutcomeRetry, duration)
	} else {
		// Max retries exhausted - NACK to DLQ
		c.logger.Error("Max retries exhausted, sending to DLQ",
			"messageId", delivery.MessageId,
			"retryCount", retryCount,
		)
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			c.logger.Error("Failed to NACK message", "error", nackErr)
		}
		c.metrics.RecordConsume(c.config.Queue, OutcomeDLQ, duration)
	}
}

// invokeHandler calls the handler, converting a panic into an ordinary
// transient (not Permanent) error so processDelivery's ack/nack routing
// still runs: a temporary cause gets its retry chances, and a deterministic
// panic reaches the DLQ once the ladder is exhausted instead of
// crash-looping the process.
func (c *RabbitMQConsumer) invokeHandler(ctx context.Context, delivery amqp.Delivery, handler MessageHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("Handler panicked",
				"panic", r,
				"stack", string(debug.Stack()),
				"messageId", delivery.MessageId,
				"correlationId", delivery.CorrelationId,
				"retryCount", GetRetryCount(delivery),
			)
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return handler(ctx, delivery)
}

// publishToRetryWithCount publishes to retry queue with incremented retry count header
func (c *RabbitMQConsumer) publishToRetryWithCount(ctx context.Context, currentRetry int, delivery amqp.Delivery) error {
	// Create new headers with incremented retry count
	headers := make(amqp.Table)
	for k, v := range delivery.Headers {
		headers[k] = v
	}

	// Safe conversion: retry count is bounded by MaxRetries (typically < 10)
	nextRetry := currentRetry + 1
	headers[RetryCountHeader] = int32(nextRetry) //nolint:gosec // bounded by MaxRetries check

	// Use publisher's channel for retry publish
	return c.publisher.PublishToRetry(ctx, currentRetry, delivery.Body, delivery.CorrelationId, headers)
}

// WillRetry reports whether a delivery that fails with a transient error
// will be routed to a retry queue (true) or dead-lettered (false). It is
// the exact predicate the consumer applies after the handler returns, so
// handlers can record the upcoming routing decision without re-deriving it.
// maxRetries is the number of configured retry queues
// (publisher.MaxRetries()). Errors matching ErrPermanent go straight to the
// DLQ regardless.
func WillRetry(delivery amqp.Delivery, maxRetries int) bool {
	return GetRetryCount(delivery) < maxRetries
}

// GetRetryCount extracts the retry count from message headers.
// Unknown types and negative values are treated as 0.
func GetRetryCount(delivery amqp.Delivery) int {
	if delivery.Headers == nil {
		return 0
	}

	val, ok := delivery.Headers[RetryCountHeader]
	if !ok {
		return 0
	}

	count := 0
	switch v := val.(type) {
	case int8:
		count = int(v)
	case int16:
		count = int(v)
	case int32:
		count = int(v)
	case int64:
		count = int(v)
	case int:
		count = v
	}
	if count < 0 {
		return 0
	}
	return count
}

// Connection returns the current AMQP connection for health checks. The
// returned pointer changes after a reconnect; health checks should use
// health.NewRabbitMQCheckerWithProvider(consumer.Connection) so they always
// observe the current connection.
func (c *RabbitMQConsumer) Connection() *amqp.Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

// teardown closes and clears the current channel and connection, ignoring
// errors (used during reconnects and final cleanup).
func (c *RabbitMQConsumer) teardown() {
	c.mu.Lock()
	ch, conn := c.channel, c.conn
	c.channel, c.conn = nil, nil
	c.mu.Unlock()

	_ = closeResources(ch, conn)
}

// Close stops consuming and closes the connection. If a Consume call is
// active, Close blocks until in-flight handlers finish and Consume returns.
// Idempotent: subsequent calls return nil.
//
// Close must not be called from inside a MessageHandler: it waits for that
// very handler to finish and would deadlock. To stop consuming from within a
// handler, cancel the context passed to Consume instead.
func (c *RabbitMQConsumer) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	runDone := c.runDone
	c.mu.Unlock()

	c.stopOnce.Do(func() {
		if c.stopCh != nil {
			close(c.stopCh)
		}
	})

	// Wait for an active Consume to drain in-flight handlers and release its
	// connection.
	if runDone != nil {
		<-runDone
	}

	c.mu.Lock()
	ch, conn := c.channel, c.conn
	c.channel, c.conn = nil, nil
	c.mu.Unlock()

	if err := closeResources(ch, conn); err != nil {
		return fmt.Errorf("%w: %w", ErrCloseFailed, err)
	}
	return nil
}
