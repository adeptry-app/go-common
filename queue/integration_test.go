package queue

// Integration tests exercising the full publish -> consume -> retry -> DLQ
// flow against a real RabbitMQ instance via testcontainers. They are skipped
// in -short mode and when no Docker daemon is available. One container is
// shared by all tests; each test uses its own exchange and queue names.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
)

var (
	rabbitOnce      sync.Once
	rabbitContainer testcontainers.Container
	rabbitHost      string
	rabbitPort      int
	rabbitErr       error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if rabbitContainer != nil {
		_ = testcontainers.TerminateContainer(rabbitContainer)
	}
	os.Exit(code)
}

func startRabbitMQ() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rabbitmq:4.3-alpine",
			ExposedPorts: []string{"5672/tcp"},
			Env: map[string]string{
				// The default guest user only accepts loopback connections,
				// which mapped container ports are not.
				"RABBITMQ_DEFAULT_USER": "it_user",
				"RABBITMQ_DEFAULT_PASS": "it_pass",
			},
			WaitingFor: wait.ForLog("Server startup complete").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		rabbitErr = err
		return
	}
	rabbitContainer = container

	host, err := container.Host(ctx)
	if err != nil {
		rabbitErr = err
		return
	}
	port, err := container.MappedPort(ctx, "5672")
	if err != nil {
		rabbitErr = err
		return
	}
	rabbitHost = host
	rabbitPort = int(port.Num())
}

// integrationConfig skips the test when integration testing is not possible
// and returns a config pointing at the shared container with fast reconnect
// delays suitable for tests.
func integrationConfig(t *testing.T, name string, retryDelays []time.Duration) config.RabbitMQConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	rabbitOnce.Do(startRabbitMQ)
	if rabbitErr != nil {
		t.Skipf("RabbitMQ container unavailable (is Docker running?): %v", rabbitErr)
	}

	return config.RabbitMQConfig{
		Host:                  rabbitHost,
		Port:                  rabbitPort,
		User:                  "it_user",
		Password:              "it_pass",
		Exchange:              name + "_ex",
		Queue:                 name,
		RetryDelays:           retryDelays,
		ReconnectInitialDelay: 100 * time.Millisecond,
		ReconnectMaxDelay:     500 * time.Millisecond,
		PrefetchCount:         1,
		ConsumerTag:           "it-" + name,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestPublisher(t *testing.T, cfg config.RabbitMQConfig, opts ...PublisherOption) *RabbitMQPublisher {
	t.Helper()
	opts = append(opts, WithPublisherLogger(testLogger()))
	pub, err := NewRabbitMQPublisher(cfg, opts...)
	if err != nil {
		t.Fatalf("NewRabbitMQPublisher: %v", err)
	}
	t.Cleanup(func() { _ = pub.Close() })
	return pub
}

func newTestConsumer(t *testing.T, cfg config.RabbitMQConfig, pub *RabbitMQPublisher) *RabbitMQConsumer {
	t.Helper()
	consumer, err := NewRabbitMQConsumer(cfg, pub, testLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQConsumer: %v", err)
	}
	t.Cleanup(func() { _ = consumer.Close() })
	return consumer
}

// queueDepth returns the current message count of a queue.
func queueDepth(t *testing.T, pub *RabbitMQPublisher, queueName string) int {
	t.Helper()
	conn := pub.Connection()
	if conn == nil || conn.IsClosed() {
		t.Fatalf("publisher connection unavailable")
	}
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer func() { _ = ch.Close() }()
	q, err := ch.QueueDeclarePassive(queueName, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("inspect queue %s: %v", queueName, err)
	}
	return q.Messages
}

// waitFor polls cond until it returns true or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// =============================================================================
// Happy Path
// =============================================================================

func TestIntegration_PublishConsume(t *testing.T) {
	cfg := integrationConfig(t, "it_happy", nil)
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	received := make(chan amqp.Delivery, 1)
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, d amqp.Delivery) error {
			received <- d
			return nil
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case d := <-received:
		var payload map[string]string
		if err := json.Unmarshal(d.Body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if payload["hello"] != "world" {
			t.Errorf("payload = %v, want hello=world", payload)
		}
		if d.CorrelationId == "" {
			t.Error("expected a generated CorrelationId")
		}
		if d.MessageId == "" {
			t.Error("expected a generated MessageId")
		}
	case <-ctx.Done():
		t.Fatal("message was not consumed before timeout")
	}
}

func TestIntegration_CorrelationIDPropagation(t *testing.T) {
	cfg := integrationConfig(t, "it_corr", nil)
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type seen struct {
		deliveryID string
		contextID  string
	}
	received := make(chan seen, 1)
	go func() {
		_ = consumer.Consume(ctx, func(handlerCtx context.Context, d amqp.Delivery) error {
			received <- seen{deliveryID: d.CorrelationId, contextID: logger.GetCorrelationID(handlerCtx)}
			return nil
		})
	}()

	pubCtx := logger.AddCorrelationID(ctx, "corr-12345")
	if err := pub.Publish(pubCtx, map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case s := <-received:
		if s.deliveryID != "corr-12345" {
			t.Errorf("delivery CorrelationId = %q, want corr-12345", s.deliveryID)
		}
		if s.contextID != "corr-12345" {
			t.Errorf("handler context correlation ID = %q, want corr-12345", s.contextID)
		}
	case <-ctx.Done():
		t.Fatal("message was not consumed before timeout")
	}
}

// =============================================================================
// Retry and DLQ Flow
// =============================================================================

func TestIntegration_RetryFlow(t *testing.T) {
	cfg := integrationConfig(t, "it_retry", []time.Duration{300 * time.Millisecond, 500 * time.Millisecond})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var mu sync.Mutex
	var retryCounts []int
	done := make(chan struct{})

	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, d amqp.Delivery) error {
			mu.Lock()
			retryCounts = append(retryCounts, GetRetryCount(d))
			attempt := len(retryCounts)
			mu.Unlock()
			if attempt < 3 {
				return errors.New("transient failure")
			}
			close(done)
			return nil
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"job": "retry-me"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("message did not succeed after retries before timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []int{0, 1, 2}
	if len(retryCounts) != len(want) {
		t.Fatalf("retryCounts = %v, want %v", retryCounts, want)
	}
	for i := range want {
		if retryCounts[i] != want[i] {
			t.Errorf("retryCounts[%d] = %d, want %d", i, retryCounts[i], want[i])
		}
	}
}

func TestIntegration_MaxRetriesToDLQ(t *testing.T) {
	cfg := integrationConfig(t, "it_dlq", []time.Duration{200 * time.Millisecond, 200 * time.Millisecond})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var attempts atomic.Int32
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, _ amqp.Delivery) error {
			attempts.Add(1)
			return errors.New("always fails")
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"job": "doomed"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, 30*time.Second, "message to reach DLQ", func() bool {
		return queueDepth(t, pub, pub.DLQName()) == 1
	})

	// Initial attempt + one per retry queue.
	if got := attempts.Load(); got != 3 {
		t.Errorf("handler attempts = %d, want 3", got)
	}
}

func TestIntegration_PermanentErrorToDLQ(t *testing.T) {
	// Long retry delays prove the message did NOT take the retry path.
	cfg := integrationConfig(t, "it_perm", []time.Duration{30 * time.Second})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var attempts atomic.Int32
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, _ amqp.Delivery) error {
			attempts.Add(1)
			return Permanent(errors.New("malformed payload"))
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"job": "broken"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, 15*time.Second, "permanent failure to reach DLQ", func() bool {
		return queueDepth(t, pub, pub.DLQName()) == 1
	})

	if got := attempts.Load(); got != 1 {
		t.Errorf("handler attempts = %d, want 1 (no retries for permanent errors)", got)
	}
	if got := queueDepth(t, pub, cfg.Queue+"_retry_0"); got != 0 {
		t.Errorf("retry queue depth = %d, want 0", got)
	}
}

// =============================================================================
// Publisher Confirms
// =============================================================================

func TestIntegration_PublisherConfirms(t *testing.T) {
	cfg := integrationConfig(t, "it_confirm", nil)
	cfg.PublisherConfirms = true
	pub := newTestPublisher(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, map[string]string{"confirmed": "yes"}); err != nil {
		t.Fatalf("Publish with confirms: %v", err)
	}

	waitFor(t, 10*time.Second, "confirmed message to be queued", func() bool {
		return queueDepth(t, pub, cfg.Queue) == 1
	})
}

// =============================================================================
// Reconnection
// =============================================================================

func TestIntegration_PublisherReconnect(t *testing.T) {
	cfg := integrationConfig(t, "it_pubrec", nil)
	pub := newTestPublisher(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, map[string]string{"n": "1"}); err != nil {
		t.Fatalf("initial Publish: %v", err)
	}

	// Simulate a dropped connection; the supervisor must rebuild it.
	if err := pub.Connection().Close(); err != nil {
		t.Fatalf("force-close connection: %v", err)
	}

	waitFor(t, 15*time.Second, "publisher to reconnect", func() bool {
		return pub.Publish(ctx, map[string]string{"n": "2"}) == nil
	})

	conn := pub.Connection()
	if conn == nil || conn.IsClosed() {
		t.Error("expected a live connection after reconnect")
	}
}

func TestIntegration_ConsumerReconnect(t *testing.T) {
	cfg := integrationConfig(t, "it_conrec", nil)
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	received := make(chan string, 10)
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, d amqp.Delivery) error {
			var payload map[string]string
			_ = json.Unmarshal(d.Body, &payload)
			received <- payload["n"]
			return nil
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"n": "before"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case got := <-received:
		if got != "before" {
			t.Fatalf("received %q, want before", got)
		}
	case <-ctx.Done():
		t.Fatal("first message not consumed before timeout")
	}

	// Simulate a dropped consumer connection.
	if err := consumer.Connection().Close(); err != nil {
		t.Fatalf("force-close consumer connection: %v", err)
	}

	// Give the consumer a moment to notice and rebuild, then publish again.
	if err := pub.Publish(ctx, map[string]string{"n": "after"}); err != nil {
		t.Fatalf("Publish after reconnect: %v", err)
	}

	select {
	case got := <-received:
		if got != "after" {
			t.Fatalf("received %q, want after", got)
		}
	case <-ctx.Done():
		t.Fatal("message after reconnect not consumed before timeout")
	}
}

// =============================================================================
// Shutdown Semantics
// =============================================================================

func TestIntegration_ShutdownRequeuesWithoutBurningRetry(t *testing.T) {
	cfg := integrationConfig(t, "it_shutdown", []time.Duration{30 * time.Second})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	rootCtx, rootCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer rootCancel()

	consumeCtx, consumeCancel := context.WithCancel(rootCtx)
	defer consumeCancel()

	started := make(chan struct{})
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- consumer.Consume(consumeCtx, func(handlerCtx context.Context, _ amqp.Delivery) error {
			close(started)
			<-handlerCtx.Done()
			return handlerCtx.Err()
		})
	}()

	if err := pub.Publish(rootCtx, map[string]string{"job": "long"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-started:
	case <-rootCtx.Done():
		t.Fatal("handler did not start before timeout")
	}

	// Cancel mid-handler: the message must be requeued, not retried.
	consumeCancel()
	select {
	case err := <-consumeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Consume returned %v, want context.Canceled", err)
		}
	case <-rootCtx.Done():
		t.Fatal("Consume did not return after cancellation")
	}

	// Re-consume with the same instance: the message comes back with an
	// unchanged retry count and the retry queue stays empty.
	redelivered := make(chan int, 1)
	consume2Ctx, consume2Cancel := context.WithCancel(rootCtx)
	defer consume2Cancel()
	go func() {
		_ = consumer.Consume(consume2Ctx, func(_ context.Context, d amqp.Delivery) error {
			redelivered <- GetRetryCount(d)
			return nil
		})
	}()

	select {
	case count := <-redelivered:
		if count != 0 {
			t.Errorf("redelivered retry count = %d, want 0 (shutdown must not burn a retry)", count)
		}
	case <-rootCtx.Done():
		t.Fatal("message was not redelivered before timeout")
	}

	if got := queueDepth(t, pub, cfg.Queue+"_retry_0"); got != 0 {
		t.Errorf("retry queue depth = %d, want 0", got)
	}
}

func TestIntegration_CloseWaitsForInflightHandler(t *testing.T) {
	cfg := integrationConfig(t, "it_close", nil)
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	started := make(chan struct{})
	var finished atomic.Bool
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, _ amqp.Delivery) error {
			close(started)
			time.Sleep(500 * time.Millisecond)
			finished.Store(true)
			return nil
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"job": "slow"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("handler did not start before timeout")
	}

	if err := consumer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !finished.Load() {
		t.Error("Close returned before the in-flight handler finished")
	}

	// The handler returned nil after Close started, so the message must have
	// been ACKed on the still-open channel - nothing left behind.
	if got := queueDepth(t, pub, cfg.Queue); got != 0 {
		t.Errorf("main queue depth after close = %d, want 0", got)
	}
}

// =============================================================================
// Panic Recovery
// =============================================================================

func TestIntegration_HandlerPanicRidesRetryLadderToDLQ(t *testing.T) {
	cfg := integrationConfig(t, "it_panic", []time.Duration{200 * time.Millisecond})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var panics atomic.Int32
	handled := make(chan string, 1)
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, d amqp.Delivery) error {
			var payload map[string]string
			_ = json.Unmarshal(d.Body, &payload)
			if payload["job"] == "panics" {
				panics.Add(1)
				panic("boom")
			}
			handled <- payload["job"]
			return nil
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"job": "panics"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// The panic must not kill the process; the message rides the retry
	// ladder (initial attempt + one retry) and lands in the DLQ.
	waitFor(t, 30*time.Second, "panicking message to reach DLQ", func() bool {
		return queueDepth(t, pub, pub.DLQName()) == 1
	})
	if got := panics.Load(); got != 2 {
		t.Errorf("handler panics = %d, want 2 (initial + one retry)", got)
	}

	// Consumption continues after the panics.
	if err := pub.Publish(ctx, map[string]string{"job": "ok"}); err != nil {
		t.Fatalf("Publish after panic: %v", err)
	}
	select {
	case got := <-handled:
		if got != "ok" {
			t.Errorf("handled %q, want ok", got)
		}
	case <-ctx.Done():
		t.Fatal("message after panic was not consumed before timeout")
	}
}

// =============================================================================
// Concurrency
// =============================================================================

func TestIntegration_ConcurrentConsumption(t *testing.T) {
	cfg := integrationConfig(t, "it_conc", nil)
	cfg.PrefetchCount = 3
	cfg.ConsumerConcurrency = 3
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var inFlight, peak atomic.Int32
	done := make(chan struct{}, 3)
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, _ amqp.Delivery) error {
			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
			inFlight.Add(-1)
			done <- struct{}{}
			return nil
		})
	}()

	for i := 0; i < 3; i++ {
		if err := pub.Publish(ctx, map[string]int{"n": i}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("not all messages processed before timeout")
		}
	}

	if got := peak.Load(); got != 3 {
		t.Errorf("peak concurrent handlers = %d, want 3", got)
	}
}

// =============================================================================
// Misc
// =============================================================================

func TestIntegration_AlreadyConsuming(t *testing.T) {
	cfg := integrationConfig(t, "it_dup", nil)
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg, pub)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Prove the first Consume is active before calling the second one,
	// otherwise the second call could win the race and become the consumer.
	handled := make(chan struct{}, 1)
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, _ amqp.Delivery) error {
			handled <- struct{}{}
			return nil
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"probe": "1"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case <-handled:
	case <-ctx.Done():
		t.Fatal("first Consume did not become active before timeout")
	}

	err := consumer.Consume(ctx, func(_ context.Context, _ amqp.Delivery) error { return nil })
	if !errors.Is(err, ErrAlreadyConsuming) {
		t.Errorf("second Consume returned %v, want ErrAlreadyConsuming", err)
	}
}

func TestIntegration_RetryJitterSetsExpiration(t *testing.T) {
	cfg := integrationConfig(t, "it_jitter", []time.Duration{10 * time.Second})
	cfg.RetryJitter = 0.5
	pub := newTestPublisher(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := pub.PublishToRetry(ctx, 0, []byte(`{}`), "corr", nil); err != nil {
		t.Fatalf("PublishToRetry: %v", err)
	}

	// Consume the retry queue directly to inspect the per-message TTL.
	conn := pub.Connection()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer func() { _ = ch.Close() }()

	var msg amqp.Delivery
	waitFor(t, 10*time.Second, "message in retry queue", func() bool {
		m, ok, getErr := ch.Get(cfg.Queue+"_retry_0", false)
		if getErr != nil || !ok {
			return false
		}
		msg = m
		return true
	})
	defer func() { _ = msg.Nack(false, false) }()

	if msg.Expiration == "" {
		t.Fatal("expected per-message expiration to be set with jitter enabled")
	}
	var ms int
	if _, err := fmt.Sscanf(msg.Expiration, "%d", &ms); err != nil {
		t.Fatalf("expiration %q is not numeric: %v", msg.Expiration, err)
	}
	if ms < 5000 || ms > 10000 {
		t.Errorf("expiration = %dms, want in [5000, 10000] for 10s delay with 0.5 jitter", ms)
	}
}
