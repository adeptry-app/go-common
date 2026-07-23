package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Common errors returned by the queue package.
var (
	ErrConnectionFailed    = errors.New("failed to connect to RabbitMQ")
	ErrChannelFailed       = errors.New("failed to open channel")
	ErrQueueSetupFailed    = errors.New("failed to setup queue infrastructure")
	ErrMarshalFailed       = errors.New("failed to marshal message")
	ErrPublishFailed       = errors.New("failed to publish message")
	ErrPublishNotConfirmed = errors.New("publish not confirmed by broker")
	ErrPublisherClosed     = errors.New("publisher is closed")
	ErrRetryOutOfBounds    = errors.New("retry index out of bounds")
	ErrCloseFailed         = errors.New("failed to close connection")
)

// Publisher defines the interface for message queue publishing with retry support
type Publisher interface {
	Publish(ctx context.Context, message interface{}) error
	PublishToRetry(ctx context.Context, retryIndex int, body []byte, correlationId string, headers amqp.Table) error
	PublishToDLQ(ctx context.Context, body []byte, correlationId string) error
	MaxRetries() int
	Close() error
}

// RabbitMQPublisher implements Publisher for RabbitMQ.
// All publish methods are safe for concurrent use.
type RabbitMQPublisher struct {
	mu          sync.Mutex
	closed      bool
	conn        *amqp.Connection
	channel     *amqp.Channel
	cfg         config.RabbitMQConfig
	retryQueues []string // Names of retry queues in order
	logger      *slog.Logger
	metrics     MetricsRecorder
	stopCh      chan struct{}
	stopOnce    sync.Once
	inflight    sync.WaitGroup // publishes in progress, including confirm waits
}

// RetryQueues returns a copy of the retry queue names for use by consumers
func (p *RabbitMQPublisher) RetryQueues() []string {
	return append([]string(nil), p.retryQueues...)
}

// DLQName returns the dead letter queue name
func (p *RabbitMQPublisher) DLQName() string {
	return p.cfg.Queue + "_dlq"
}

// DLXName returns the dead letter exchange name
func (p *RabbitMQPublisher) DLXName() string {
	return p.cfg.Exchange + "_dlx"
}

// NewRabbitMQPublisher creates a new RabbitMQ publisher with exchange, retry queues, and DLQ.
//
// The publisher is safe for concurrent use from multiple goroutines.
//
// Unless cfg.DisableReconnect is set, the publisher automatically reconnects
// with exponential backoff when the connection or channel drops, re-declaring
// the topology on success. Publishes issued while disconnected fail fast with
// ErrPublishFailed; callers decide whether to retry. The initial connection
// is still fail-fast: if RabbitMQ is unreachable at startup an error is
// returned.
//
// With cfg.PublisherConfirms enabled, every publish blocks until the broker
// confirms the message (bounded by ctx) and returns ErrPublishNotConfirmed
// on a broker NACK.
//
// Retry flow: Consumers must explicitly call PublishToRetry() to route failed messages through
// the retry chain. The main queue's dead-letter config routes directly to DLQ for unhandled
// failures (e.g., message rejected without calling PublishToRetry).
//
// If cfg.RetryDelays is empty, rejected messages route directly to the DLQ with no retry attempts.
func NewRabbitMQPublisher(cfg config.RabbitMQConfig, opts ...PublisherOption) (*RabbitMQPublisher, error) {
	p := &RabbitMQPublisher{
		cfg:     cfg.WithDefaults(),
		logger:  slog.Default(),
		metrics: noopMetrics{},
		stopCh:  make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}

	conn, ch, retryQueues, err := p.connect()
	if err != nil {
		return nil, err
	}
	p.conn = conn
	p.channel = ch
	p.retryQueues = retryQueues

	if !p.cfg.DisableReconnect {
		go p.supervise(conn, ch)
	}

	return p, nil
}

// connect dials, opens a channel, declares the topology, and enables confirm
// mode when configured. On error all opened resources are closed.
func (p *RabbitMQPublisher) connect() (*amqp.Connection, *amqp.Channel, []string, error) {
	conn, err := dial(p.cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrChannelFailed, err)
	}

	cleanup := func() { _ = closeResources(ch, conn) }

	retryQueues, err := declareTopology(ch, p.cfg)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}

	if p.cfg.PublisherConfirms {
		if err := ch.Confirm(false); err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("%w: enable confirms: %v", ErrChannelFailed, err)
		}
	}

	return conn, ch, retryQueues, nil
}

// supervise watches for connection or channel loss and reconnects with
// backoff until Close is called or the attempt limit is reached.
func (p *RabbitMQPublisher) supervise(conn *amqp.Connection, ch *amqp.Channel) {
	for {
		connClose := conn.NotifyClose(make(chan *amqp.Error, 1))
		chClose := ch.NotifyClose(make(chan *amqp.Error, 1))

		var reason *amqp.Error
		select {
		case <-p.stopCh:
			return
		case reason = <-connClose:
		case reason = <-chClose:
		}

		if p.isClosed() {
			return
		}

		p.logger.Warn("RabbitMQ publisher connection lost, reconnecting",
			"queue", p.cfg.Queue,
			"reason", closeError(reason),
		)
		p.metrics.RecordReconnect("publisher")

		newConn, newCh, ok := p.reconnect()
		if !ok {
			return
		}
		conn, ch = newConn, newCh
	}
}

// reconnect loops with backoff until a new connection is established, the
// publisher is closed, or ReconnectMaxAttempts is exceeded.
func (p *RabbitMQPublisher) reconnect() (*amqp.Connection, *amqp.Channel, bool) {
	// Tear down whatever is left of the old connection (it may still be
	// half-open after a channel-level failure).
	p.mu.Lock()
	oldConn, oldCh := p.conn, p.channel
	p.mu.Unlock()
	_ = closeResources(oldCh, oldConn)

	for attempt := 1; ; attempt++ {
		if p.cfg.ReconnectMaxAttempts > 0 && attempt > p.cfg.ReconnectMaxAttempts {
			p.logger.Error("RabbitMQ publisher reconnect attempts exhausted",
				"queue", p.cfg.Queue,
				"attempts", p.cfg.ReconnectMaxAttempts,
			)
			return nil, nil, false
		}

		select {
		case <-p.stopCh:
			return nil, nil, false
		case <-time.After(reconnectDelay(p.cfg, attempt)):
		}

		// retryQueues is deterministic from the config and immutable after
		// construction, so the redeclared names are ignored here.
		conn, ch, _, err := p.connect()
		if err != nil {
			p.logger.Warn("RabbitMQ publisher reconnect attempt failed",
				"queue", p.cfg.Queue,
				"attempt", attempt,
				"error", err,
			)
			continue
		}

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = closeResources(ch, conn)
			return nil, nil, false
		}
		p.conn = conn
		p.channel = ch
		p.mu.Unlock()

		p.logger.Info("RabbitMQ publisher reconnected", "queue", p.cfg.Queue, "attempt", attempt)
		return conn, ch, true
	}
}

func (p *RabbitMQPublisher) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// publish is the internal helper for all publish operations.
// correlationId links related messages (e.g., original + retries).
// headers is optional (can be nil). expiration is an optional per-message TTL
// in milliseconds (empty string means no per-message TTL).
func (p *RabbitMQPublisher) publish(ctx context.Context, exchange, routingKey string, body []byte, correlationId string, headers amqp.Table, expiration string) error {
	start := time.Now()
	err := p.doPublish(ctx, exchange, routingKey, body, correlationId, headers, expiration)
	p.metrics.RecordPublish(routingKey, err == nil, time.Since(start))
	return err
}

func (p *RabbitMQPublisher) doPublish(ctx context.Context, exchange, routingKey string, body []byte, correlationId string, headers amqp.Table, expiration string) error {
	publishing := amqp.Publishing{
		DeliveryMode:  amqp.Persistent,
		ContentType:   "application/json",
		Body:          body,
		Timestamp:     time.Now(),
		MessageId:     uuid.NewString(),
		CorrelationId: correlationId,
		Headers:       headers,
		Expiration:    expiration,
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("%w", ErrPublisherClosed)
	}
	// Registered under the lock so Close cannot start tearing down between
	// the closed check and the publish.
	p.inflight.Add(1)
	defer p.inflight.Done()

	var confirmation *amqp.DeferredConfirmation
	var err error
	if p.cfg.PublisherConfirms {
		confirmation, err = p.channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, publishing)
	} else {
		err = p.channel.PublishWithContext(ctx, exchange, routingKey, false, false, publishing)
	}
	p.mu.Unlock()

	if err != nil {
		return fmt.Errorf("%w: %v", ErrPublishFailed, err)
	}

	// Wait for the broker confirmation outside the lock so concurrent
	// publishes are not serialized on broker round-trips. A channel loss
	// NACKs all pending confirmations, so this cannot hang.
	if confirmation != nil {
		acked, err := confirmation.WaitContext(ctx)
		if err != nil {
			return fmt.Errorf("%w: confirm wait: %v", ErrPublishFailed, err)
		}
		if !acked {
			return fmt.Errorf("%w", ErrPublishNotConfirmed)
		}
	}
	return nil
}

// Publish sends a message to the main queue.
// The message CorrelationId is taken from the context (logger.GetCorrelationID)
// when present so messages can be traced back to the originating request;
// otherwise a new one is generated. It is preserved through retries.
func (p *RabbitMQPublisher) Publish(ctx context.Context, message interface{}) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMarshalFailed, err)
	}
	correlationId := logger.GetCorrelationID(ctx)
	if correlationId == "" {
		correlationId = uuid.NewString()
	}
	return p.publish(ctx, p.cfg.Exchange, p.cfg.Queue, body, correlationId, nil, "")
}

// PublishToRetry sends a message to a specific retry queue by index.
// Returns error if retryIndex is out of bounds (should send to DLQ instead).
// The correlationId should be preserved from the original message for tracing.
// Headers is optional (can be nil) - used to track retry count.
//
// When cfg.RetryJitter > 0, a per-message TTL shortened by up to that
// fraction is set so messages that failed together do not all return to the
// main queue at the same instant. The queue-level TTL remains the upper
// bound; per-message expiry only shortens the delay (RabbitMQ uses the lower
// of the two). Because RabbitMQ expires only the queue head, a message may
// still wait for messages ahead of it.
func (p *RabbitMQPublisher) PublishToRetry(ctx context.Context, retryIndex int, body []byte, correlationId string, headers amqp.Table) error {
	maxRetries := p.MaxRetries()
	if maxRetries == 0 {
		return fmt.Errorf("%w: no retry queues configured", ErrRetryOutOfBounds)
	}
	if retryIndex < 0 || retryIndex >= maxRetries {
		return fmt.Errorf("%w: index %d, max %d", ErrRetryOutOfBounds, retryIndex, maxRetries-1)
	}

	expiration := ""
	if retryIndex < len(p.cfg.RetryDelays) {
		expiration = jitteredExpiration(p.cfg.RetryDelays[retryIndex], p.cfg.RetryJitter)
	}

	return p.publish(ctx, p.cfg.Exchange, p.retryQueues[retryIndex], body, correlationId, headers, expiration)
}

// jitteredExpiration returns a per-message TTL in milliseconds, randomly
// shortened from delay by up to jitter (a 0..1 fraction). Returns "" (no
// per-message TTL) when jitter is disabled or delay is not positive.
func jitteredExpiration(delay time.Duration, jitter float64) string {
	if jitter <= 0 || delay <= 0 {
		return ""
	}
	if jitter > 1 {
		jitter = 1
	}
	reduced := delay - time.Duration(rand.Float64()*jitter*float64(delay))
	ms := reduced.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return strconv.FormatInt(ms, 10)
}

// PublishToDLQ sends a message to the dead letter queue (permanent failure).
// The correlationId should be preserved from the original message for tracing.
func (p *RabbitMQPublisher) PublishToDLQ(ctx context.Context, body []byte, correlationId string) error {
	return p.publish(ctx, p.DLXName(), p.DLQName(), body, correlationId, nil, "")
}

// MaxRetries returns the number of retry queues (attempts before DLQ)
func (p *RabbitMQPublisher) MaxRetries() int {
	return len(p.retryQueues)
}

// Connection returns the current AMQP connection for health checks. The
// returned pointer changes after a reconnect; health checks should use
// health.NewRabbitMQCheckerWithProvider(publisher.Connection) so they always
// observe the current connection.
func (p *RabbitMQPublisher) Connection() *amqp.Connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn
}

// Close closes the channel and connection and stops the reconnect supervisor.
// Safe to call concurrently with Publish methods - will wait for in-flight publishes to complete.
// Idempotent: subsequent calls return nil.
func (p *RabbitMQPublisher) Close() error {
	p.stopOnce.Do(func() {
		if p.stopCh != nil {
			close(p.stopCh)
		}
	})

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	ch, conn := p.channel, p.conn
	p.mu.Unlock()

	// Wait for in-flight publishes (including publisher-confirm waits, which
	// happen outside the mutex) so closing the channel does not NACK
	// confirmations for messages the broker already accepted.
	p.inflight.Wait()

	if err := closeResources(ch, conn); err != nil {
		return fmt.Errorf("%w: %w", ErrCloseFailed, err)
	}
	return nil
}
