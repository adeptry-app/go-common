package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
)

// Consumer errors
var (
	ErrConsumerClosed   = errors.New("consumer is closed")
	ErrAlreadyConsuming = errors.New("consumer is already consuming")
	ErrNotConsuming     = errors.New("consumer has not started")
	ErrReceiveFailed    = errors.New("failed to receive messages")
)

// Backoff between failed receives, so an outage does not spin the loop.
const (
	receiveInitialDelay = 1 * time.Second
	receiveMaxDelay     = 30 * time.Second
)

// settleTimeout bounds the call that settles a delivery. Settling runs on a
// detached context so a shutdown mid-handler still releases the message.
const settleTimeout = 10 * time.Second

// MessageHandler processes a single message delivery.
// Return nil to delete the message, return error to trigger retry logic.
// Wrap errors with Permanent() to skip retries and go straight to the DLQ, and
// with WithAttempt() to pick the ladder step matching the row's own attempt.
// The context carries the message correlation ID (logger.GetCorrelationID).
//
// A panic inside the handler does not crash the process: it is recovered,
// logged with the stack, and treated as a transient handler error that rides
// the retry ladder.
type MessageHandler func(ctx context.Context, delivery Delivery) error

// Consumer defines the interface for message queue consuming
type Consumer interface {
	// Consume starts consuming messages and blocks until context is cancelled
	Consume(ctx context.Context, handler MessageHandler) error
	// Close stops consuming
	Close() error
}

// SQSConsumer implements Consumer for SQS
type SQSConsumer struct {
	mu        sync.Mutex
	closed    bool
	started   bool
	consuming bool
	runErr    error
	runDone   chan struct{}
	client    sqsAPI
	cfg       config.SQSConfig
	name      string
	logger    *slog.Logger
	metrics   MetricsRecorder
	stopCh    chan struct{}
}

// NewSQSConsumer creates a consumer for the queue named by cfg.QueueURL.
// If logger is nil, slog.Default() is used. ctx bounds credential resolution
// only, not the lifetime of the consumer.
func NewSQSConsumer(
	ctx context.Context,
	cfg config.SQSConfig,
	logger *slog.Logger,
	opts ...ConsumerOption,
) (*SQSConsumer, error) {
	client, err := newSQSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return newConsumer(client, cfg, logger, opts...), nil
}

// newConsumer wires an already-built client, so tests can pass a fake.
func newConsumer(client sqsAPI, cfg config.SQSConfig, log *slog.Logger, opts ...ConsumerOption) *SQSConsumer {
	if log == nil {
		log = slog.Default()
	}

	c := &SQSConsumer{
		client:  client,
		cfg:     cfg.WithDefaults(),
		name:    queueName(cfg.QueueURL),
		logger:  log,
		metrics: noopMetrics{},
		stopCh:  make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *SQSConsumer) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Consume long-polls the queue and blocks until the context is cancelled or
// Close is called.
//
// Messages are processed with cfg.ConsumerConcurrency parallel handlers
// (default 1, sequential), and a receive never asks for more messages than
// there are free handler slots. Consume waits for in-flight handlers to finish
// before returning. Only one Consume may be active per consumer; concurrent
// calls return ErrAlreadyConsuming.
//
// A failed receive is retried with backoff indefinitely, so Consume returns
// only on context cancellation or Close; ConsumptionError is what makes a
// stuck consumer visible. The handler is called for each message; return nil
// to delete it, an error to ride the retry ladder, or a Permanent()-wrapped
// error to quarantine it in the DLQ. If the context is cancelled while a
// message is being handled, the message is made visible again immediately.
//
// Visibility is set at receive and never heartbeated, so a handler must finish
// inside cfg.VisibilityTimeout or its message is redelivered while it runs.
func (c *SQSConsumer) Consume(ctx context.Context, handler MessageHandler) (retErr error) {
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
	c.started = true
	c.runErr = nil
	runDone := make(chan struct{})
	c.runDone = runDone
	c.mu.Unlock()

	sem := make(chan struct{}, c.cfg.ConsumerConcurrency)
	var wg sync.WaitGroup

	defer func() {
		wg.Wait()
		c.mu.Lock()
		c.consuming = false
		c.runErr = retErr
		c.mu.Unlock()
		close(runDone)
	}()

	// Close must interrupt an in-flight long poll rather than wait it out.
	pollCtx, stopPolling := context.WithCancel(ctx)
	defer stopPolling()
	go func() {
		select {
		case <-pollCtx.Done():
		case <-c.stopCh:
			stopPolling()
		}
	}()

	c.logger.Info("Consumer started", "queue", c.name, "concurrency", c.cfg.ConsumerConcurrency)

	failures := 0
	for {
		slots, err := c.reserve(ctx, sem)
		if err != nil {
			return err
		}

		messages, receivedAt, err := c.receive(pollCtx, slots)
		if err != nil {
			release(sem, slots)
			if ctx.Err() != nil {
				c.logger.Info("Consumer stopping", "reason", ctx.Err())
				return ctx.Err()
			}
			if c.isClosed() {
				c.logger.Info("Consumer stopping", "reason", "closed")
				return ErrConsumerClosed
			}
			failures++
			// Consume never returns while receives keep failing, so the retry
			// state itself has to be what health checks observe.
			c.setRunErr(fmt.Errorf("receive failed after %d attempt(s): %w", failures, err))
			c.logger.Warn("Receive failed, retrying", "queue", c.name, "attempt", failures, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.stopCh:
				return ErrConsumerClosed
			case <-time.After(receiveBackoff(failures)):
			}
			continue
		}
		failures = 0
		c.setRunErr(nil)

		// More messages than slots would leave a handler goroutine blocked on
		// the semaphore forever; the surplus returns when its visibility lapses.
		if len(messages) > slots {
			c.logger.Error("Received more messages than reserved",
				"queue", c.name, "messages", len(messages), "slots", slots)
			messages = messages[:slots]
		}

		release(sem, slots-len(messages))
		for _, message := range messages {
			wg.Add(1)
			go func(m types.Message) {
				defer wg.Done()
				defer func() { <-sem }()
				c.processMessage(ctx, m, receivedAt, handler)
			}(message)
		}
	}
}

// reserve blocks for one handler slot, then takes any others already free, so
// a receive never pulls more messages than can be handled at once.
func (c *SQSConsumer) reserve(ctx context.Context, sem chan struct{}) (int, error) {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		c.logger.Info("Consumer stopping", "reason", ctx.Err())
		return 0, ctx.Err()
	case <-c.stopCh:
		c.logger.Info("Consumer stopping", "reason", "closed")
		return 0, ErrConsumerClosed
	}

	slots := 1
	for slots < c.cfg.MaxNumberOfMessages {
		select {
		case sem <- struct{}{}:
			slots++
		default:
			return slots, nil
		}
	}
	return slots, nil
}

// release hands back slots a receive did not use.
func release(sem chan struct{}, n int) {
	for range n {
		<-sem
	}
}

// receive long-polls for up to max messages and reports when they arrived,
// which is the instant every visibility timeout on them is measured from.
func (c *SQSConsumer) receive(ctx context.Context, max int) ([]types.Message, time.Time, error) {
	out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(c.cfg.QueueURL),
		MaxNumberOfMessages: count(max),
		WaitTimeSeconds:     count(c.cfg.WaitTimeSeconds),
		// Passed on every receive so a queue created out of band cannot fall
		// back to the 30s SQS default and redeliver a job that is still running.
		VisibilityTimeout:           seconds(c.cfg.VisibilityTimeout),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameApproximateReceiveCount},
		MessageAttributeNames:       []string{correlationAttribute},
	})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %v", ErrReceiveFailed, err)
	}
	return out.Messages, time.Now(), nil
}

// processMessage runs the handler and settles the message on its outcome.
func (c *SQSConsumer) processMessage(ctx context.Context, m types.Message, receivedAt time.Time, handler MessageHandler) {
	delivery := deliveryFrom(m, receivedAt)

	// Propagate the message correlation ID so handler logs line up with the
	// publishing request.
	if delivery.CorrelationID != "" {
		ctx = logger.AddCorrelationID(ctx, delivery.CorrelationID)
	}

	c.logger.Debug("Processing message",
		"messageId", delivery.MessageID,
		"correlationId", delivery.CorrelationID,
		"receiveCount", delivery.ReceiveCount,
	)

	start := time.Now()
	err := c.invokeHandler(ctx, delivery, handler)
	duration := time.Since(start)

	// Settling outlives the consume context: a shutdown mid-handler must still
	// release the message rather than leave it hidden for the whole timeout.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()

	switch {
	case err == nil:
		c.delete(settleCtx, m, delivery)
		c.metrics.RecordConsume(c.name, OutcomeSuccess, duration)

	case ctx.Err() != nil:
		// Shutdown: hand the message straight back, costing a receive rather
		// than a ladder step.
		c.logger.Info("Returning message due to shutdown", "messageId", delivery.MessageID, "error", err)
		c.changeVisibility(settleCtx, m, delivery, 0)
		c.metrics.RecordConsume(c.name, OutcomeRequeued, duration)

	case errors.Is(err, ErrPermanent):
		c.logger.Warn("Permanent failure, sending to DLQ",
			"error", err,
			"messageId", delivery.MessageID,
			"receiveCount", delivery.ReceiveCount,
		)
		c.quarantine(settleCtx, m, delivery)
		c.metrics.RecordConsume(c.name, OutcomeDLQ, duration)

	default:
		delay := c.retryDelay(delivery, err)
		c.logger.Warn("Handler failed, backing off",
			"error", err,
			"messageId", delivery.MessageID,
			"receiveCount", delivery.ReceiveCount,
			"delay", delay,
		)
		// With no ladder configured the queue's own visibility timeout is the
		// back-off; asking for zero would spin the message.
		if delay > 0 {
			c.changeVisibility(settleCtx, m, delivery, delay)
		}
		c.metrics.RecordConsume(c.name, OutcomeRetry, duration)
	}
}

// invokeHandler calls the handler, converting a panic into an ordinary
// transient (not Permanent) error so processMessage's settling still runs: a
// temporary cause gets its retry chances, and a deterministic panic reaches
// the DLQ once the row's budget is spent instead of crash-looping the process.
func (c *SQSConsumer) invokeHandler(ctx context.Context, delivery Delivery, handler MessageHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("Handler panicked",
				"panic", r,
				"stack", string(debug.Stack()),
				"messageId", delivery.MessageID,
				"correlationId", delivery.CorrelationID,
				"receiveCount", delivery.ReceiveCount,
			)
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return handler(ctx, delivery)
}

// retryDelay picks the ladder step for a failed delivery: the business attempt
// the handler reported, or the receive count when it reported none (a failure
// before the row was claimed, which must still back off rather than spin).
// Both are clamped to the ladder, and the result to what is left of the SQS
// ceiling, which runs from the receive rather than from this call.
func (c *SQSConsumer) retryDelay(delivery Delivery, err error) time.Duration {
	if len(c.cfg.RetryDelays) == 0 {
		return 0
	}

	step, ok := AttemptOf(err)
	if !ok {
		step = delivery.ReceiveCount
	}
	step = min(max(step, 1), len(c.cfg.RetryDelays))

	delay := jittered(c.cfg.RetryDelays[step-1], c.cfg.RetryJitter)
	remaining := config.MaxVisibilityTimeout - time.Since(delivery.ReceivedAt)
	return max(min(delay, remaining), 0)
}

// jittered randomly shortens delay by up to the jitter fraction (0 to 1) so
// messages that failed together do not all return at the same instant.
// Jitter only ever shortens, so it cannot push a delay past the ceiling.
func jittered(delay time.Duration, jitter float64) time.Duration {
	if jitter <= 0 || delay <= 0 {
		return delay
	}
	if jitter > 1 {
		jitter = 1
	}
	return delay - time.Duration(randFloat64()*jitter*float64(delay))
}

// receiveBackoff returns the delay before receive attempt n (1-based):
// exponential growth from receiveInitialDelay capped at receiveMaxDelay, with
// up to 25% random jitter to avoid synchronized retry storms.
func receiveBackoff(attempt int) time.Duration {
	delay := receiveInitialDelay
	for i := 1; i < attempt && delay < receiveMaxDelay; i++ {
		delay *= 2
	}
	if delay > receiveMaxDelay {
		delay = receiveMaxDelay
	}
	return delay + time.Duration(randFloat64()*0.25*float64(delay))
}

// quarantine copies the body to the DLQ and then deletes the source. The two
// are not atomic, so the copy carries the source message id for deduping and a
// failed delete leaves the message: the redelivery re-quarantines it rather
// than losing the body.
func (c *SQSConsumer) quarantine(ctx context.Context, m types.Message, delivery Delivery) {
	attributes := make(map[string]types.MessageAttributeValue, 2)
	if delivery.CorrelationID != "" {
		attributes[correlationAttribute] = stringAttribute(delivery.CorrelationID)
	}
	if delivery.MessageID != "" {
		attributes[sourceIDAttribute] = stringAttribute(delivery.MessageID)
	}

	if _, err := c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:          aws.String(c.cfg.DLQURL),
		MessageBody:       m.Body,
		MessageAttributes: attributes,
	}); err != nil {
		c.logger.Error("Failed to send message to DLQ", "error", err, "messageId", delivery.MessageID)
		return
	}

	c.delete(ctx, m, delivery)
}

// delete removes a settled message from the queue.
func (c *SQSConsumer) delete(ctx context.Context, m types.Message, delivery Delivery) {
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.cfg.QueueURL),
		ReceiptHandle: m.ReceiptHandle,
	}); err != nil {
		c.logger.Error("Failed to delete message", "error", err, "messageId", delivery.MessageID)
	}
}

// changeVisibility hides the message for delay before it is redelivered.
func (c *SQSConsumer) changeVisibility(ctx context.Context, m types.Message, delivery Delivery, delay time.Duration) {
	if _, err := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(c.cfg.QueueURL),
		ReceiptHandle:     m.ReceiptHandle,
		VisibilityTimeout: seconds(delay),
	}); err != nil {
		c.logger.Error("Failed to change message visibility",
			"error", err,
			"messageId", delivery.MessageID,
			"delay", delay,
		)
	}
}

// setRunErr records why deliveries are not flowing, or nil once they are.
func (c *SQSConsumer) setRunErr(err error) {
	c.mu.Lock()
	c.runErr = err
	c.mu.Unlock()
}

// ConsumptionError reports why messages are not being consumed - either
// Consume stopped, or it is stuck retrying receives - and nil while deliveries
// flow or after a clean shutdown. Register it with
// health.NewConsumerChecker(consumer.ConsumptionError): a consumer that is
// receiving nothing has no connection to lose, so nothing else reveals it.
func (c *SQSConsumer) ConsumptionError() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	// A worker whose Consume never ran has no connection to lose, so this is
	// the only thing that reveals it.
	if !c.started {
		return ErrNotConsuming
	}
	if errors.Is(c.runErr, context.Canceled) || errors.Is(c.runErr, context.DeadlineExceeded) {
		return nil
	}
	return c.runErr
}

// Close stops consuming. If a Consume call is active, Close blocks until
// in-flight handlers finish and Consume returns.
// Idempotent: subsequent calls return nil.
//
// Close must not be called from inside a MessageHandler: it waits for that
// very handler to finish and would deadlock. To stop consuming from within a
// handler, cancel the context passed to Consume instead.
func (c *SQSConsumer) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	runDone := c.runDone
	c.mu.Unlock()

	// Only one caller gets past the closed gate, so this closes exactly once.
	close(c.stopCh)

	// Wait for an active Consume to drain in-flight handlers.
	if runDone != nil {
		<-runDone
	}
	return nil
}
