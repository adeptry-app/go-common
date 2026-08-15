package queue

// Integration tests exercising the full publish -> consume -> retry -> DLQ
// flow against real SQS via LocalStack in testcontainers. They are skipped in
// -short mode, and when no Docker daemon is available outside CI. One
// container is shared by all tests; each test creates its own queue pair.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
)

var (
	localstackOnce      sync.Once
	localstackContainer testcontainers.Container
	localstackEndpoint  string
	localstackErr       error
)

const testRegion = "eu-west-1"

func TestMain(m *testing.M) {
	// LocalStack accepts any credentials, but the signer needs some.
	_ = os.Setenv("AWS_ACCESS_KEY_ID", "test")
	_ = os.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	code := m.Run()
	if localstackContainer != nil {
		_ = testcontainers.TerminateContainer(localstackContainer)
	}
	os.Exit(code)
}

func startLocalStack() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "localstack/localstack:4.14",
			ExposedPorts: []string{"4566/tcp"},
			Env:          map[string]string{"SERVICES": "sqs", "DEBUG": "0"},
			WaitingFor:   wait.ForHTTP("/_localstack/health").WithPort("4566/tcp").WithStartupTimeout(4 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		localstackErr = err
		return
	}
	localstackContainer = container

	endpoint, err := container.PortEndpoint(ctx, "4566/tcp", "http")
	if err != nil {
		localstackErr = err
		return
	}
	localstackEndpoint = endpoint
}

// integrationConfig starts LocalStack, creates the queue pair for this test and
// returns a config pointing at them. A missing Docker daemon skips locally and
// fails in CI, so a runner without Docker cannot report green untested.
func integrationConfig(t *testing.T, name string, retryDelays []time.Duration) config.SQSConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	localstackOnce.Do(startLocalStack)
	if localstackErr != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("LocalStack container unavailable in CI: %v", localstackErr)
		}
		t.Skipf("LocalStack container unavailable (is Docker running?): %v", localstackErr)
	}

	client := rawClient(t)
	return config.SQSConfig{
		QueueURL:          createQueue(t, client, name),
		DLQURL:            createQueue(t, client, name+"_dlq"),
		Region:            testRegion,
		Endpoint:          localstackEndpoint,
		RetryDelays:       retryDelays,
		VisibilityTimeout: time.Minute,
	}
}

// rawClient talks to LocalStack directly, for setup and for assertions the
// publisher and consumer surfaces do not expose.
func rawClient(t *testing.T) *sqs.Client {
	t.Helper()
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(testRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	return sqs.NewFromConfig(awsCfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String(localstackEndpoint) })
}

func createQueue(t *testing.T, client *sqs.Client, name string) string {
	t.Helper()
	out, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName:  aws.String(name),
		Attributes: map[string]string{"VisibilityTimeout": "60"},
	})
	if err != nil {
		t.Fatalf("create queue %s: %v", name, err)
	}
	return aws.ToString(out.QueueUrl)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestPublisher(t *testing.T, cfg config.SQSConfig, opts ...PublisherOption) *SQSPublisher {
	t.Helper()
	pub, err := NewSQSPublisher(context.Background(), cfg, opts...)
	if err != nil {
		t.Fatalf("NewSQSPublisher: %v", err)
	}
	t.Cleanup(func() { _ = pub.Close() })
	return pub
}

func newTestConsumer(t *testing.T, cfg config.SQSConfig) *SQSConsumer {
	t.Helper()
	consumer, err := NewSQSConsumer(context.Background(), cfg, testLogger())
	if err != nil {
		t.Fatalf("NewSQSConsumer: %v", err)
	}
	t.Cleanup(func() { _ = consumer.Close() })
	return consumer
}

// receiveOne pulls a single message directly, returning false when the queue
// stays empty for the poll window. A peeked message comes back a second later,
// so a poll cannot hide it for the queue's whole visibility timeout.
func receiveOne(t *testing.T, client *sqs.Client, queueURL string, wait int32) (types.Message, bool) {
	t.Helper()
	out, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:                    aws.String(queueURL),
		MaxNumberOfMessages:         1,
		WaitTimeSeconds:             wait,
		VisibilityTimeout:           1,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameApproximateReceiveCount},
		MessageAttributeNames:       []string{"All"},
	})
	if err != nil {
		t.Fatalf("receive from %s: %v", queueURL, err)
	}
	if len(out.Messages) == 0 {
		return types.Message{}, false
	}
	return out.Messages[0], true
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
	consumer := newTestConsumer(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type seen struct {
		delivery  Delivery
		contextID string
	}
	received := make(chan seen, 1)
	consumeDone := make(chan struct{})
	go func() {
		defer close(consumeDone)
		_ = consumer.Consume(ctx, func(handlerCtx context.Context, d Delivery) error {
			received <- seen{delivery: d, contextID: logger.GetCorrelationID(handlerCtx)}
			return nil
		})
	}()

	pubCtx := logger.AddCorrelationID(ctx, "corr-12345")
	if err := pub.Publish(pubCtx, map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case s := <-received:
		var payload map[string]string
		if err := json.Unmarshal(s.delivery.Body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if payload["hello"] != "world" {
			t.Errorf("payload = %v, want hello=world", payload)
		}
		if s.delivery.MessageID == "" {
			t.Error("expected a message id on the delivery")
		}
		if s.delivery.CorrelationID != "corr-12345" {
			t.Errorf("delivery correlation ID = %q, want corr-12345", s.delivery.CorrelationID)
		}
		if s.contextID != "corr-12345" {
			t.Errorf("handler context correlation ID = %q, want corr-12345", s.contextID)
		}
	case <-ctx.Done():
		t.Fatal("message was not consumed before timeout")
	}

	// Consume drains in-flight handlers, so returning means the message was
	// settled: a handler that succeeded leaves nothing on the queue.
	cancel()
	<-consumeDone
	if _, ok := receiveOne(t, rawClient(t), cfg.QueueURL, 3); ok {
		t.Error("message should have been deleted after the handler succeeded")
	}
}

// =============================================================================
// DLQ Flow
// =============================================================================

func TestIntegration_PermanentErrorToDLQ(t *testing.T) {
	// A long ladder proves the message did NOT take the retry path.
	cfg := integrationConfig(t, "it_perm", []time.Duration{30 * time.Second})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var attempts atomic.Int32
	consumeDone := make(chan struct{})
	go func() {
		defer close(consumeDone)
		_ = consumer.Consume(ctx, func(context.Context, Delivery) error {
			attempts.Add(1)
			return Permanent(errors.New("malformed payload"))
		})
	}()

	if err := pub.Publish(ctx, map[string]string{"job": "broken"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	client := rawClient(t)
	var quarantined types.Message
	waitFor(t, 30*time.Second, "permanent failure to reach the DLQ", func() bool {
		m, ok := receiveOne(t, client, cfg.DLQURL, 1)
		if ok {
			quarantined = m
		}
		return ok
	})

	if got := aws.ToString(quarantined.MessageAttributes[sourceIDAttribute].StringValue); got == "" {
		t.Error("DLQ copy should carry the source message id for deduping")
	}

	// The DLQ copy lands before the source delete, so settle the consumer first
	// or an invisible message reads the same as a deleted one.
	cancel()
	<-consumeDone

	if got := attempts.Load(); got != 1 {
		t.Errorf("handler attempts = %d, want 1 (no retries for permanent errors)", got)
	}
	if _, ok := receiveOne(t, client, cfg.QueueURL, 3); ok {
		t.Error("quarantined message should have been deleted from the source queue")
	}
}

// =============================================================================
// Retry Ladder
// =============================================================================

func TestIntegration_LadderIndexedByAttemptNotReceiveCount(t *testing.T) {
	// Step 1 is short, step 2 is long: a redelivery inside the timeout proves
	// the first rung was used even though the message was received four times.
	cfg := integrationConfig(t, "it_ladder", []time.Duration{2 * time.Second, 60 * time.Second})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, map[string]string{"job": "operational-receives"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Burn receives the way a rolling deploy would, without ever handling it.
	client := rawClient(t)
	for range 4 {
		waitFor(t, 20*time.Second, "the message to become visible again", func() bool {
			_, ok := receiveOne(t, client, cfg.QueueURL, 1)
			return ok
		})
	}

	var calls atomic.Int32
	handled := make(chan Delivery, 4)
	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, d Delivery) error {
			handled <- d
			if calls.Add(1) == 1 {
				return WithAttempt(1, errors.New("provider timeout"))
			}
			return nil
		})
	}()

	var first Delivery
	select {
	case first = <-handled:
	case <-ctx.Done():
		t.Fatal("message was not handled before timeout")
	}
	if first.ReceiveCount < 5 {
		t.Fatalf("first handled delivery ReceiveCount = %d, want at least 5", first.ReceiveCount)
	}

	start := time.Now()
	select {
	case <-handled:
	case <-time.After(45 * time.Second):
		t.Fatal("redelivery did not arrive on the first ladder step; the receive count indexed it")
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("redelivery arrived after %v, want at least the 2s first step", elapsed)
	}
}

// =============================================================================
// Shutdown Semantics
// =============================================================================

func TestIntegration_ShutdownReturnsMessageImmediately(t *testing.T) {
	cfg := integrationConfig(t, "it_shutdown", []time.Duration{60 * time.Second})
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg)

	rootCtx, rootCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer rootCancel()

	consumeCtx, consumeCancel := context.WithCancel(rootCtx)
	defer consumeCancel()

	started := make(chan struct{})
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- consumer.Consume(consumeCtx, func(handlerCtx context.Context, _ Delivery) error {
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

	consumeCancel()
	select {
	case err := <-consumeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Consume returned %v, want context.Canceled", err)
		}
	case <-rootCtx.Done():
		t.Fatal("Consume did not return after cancellation")
	}

	// The queue's own 60s visibility timeout is still running, so getting the
	// message straight back proves the consumer handed it over on shutdown.
	client := rawClient(t)
	var redelivered types.Message
	waitFor(t, 20*time.Second, "the message to be immediately visible again", func() bool {
		m, ok := receiveOne(t, client, cfg.QueueURL, 1)
		if ok {
			redelivered = m
		}
		return ok
	})

	raw := redelivered.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]
	count, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("receive count %q is not numeric: %v", raw, err)
	}
	if count < 2 {
		t.Errorf("receive count = %d, want at least 2 (the shutdown costs a receive)", count)
	}
}

// =============================================================================
// Concurrency
// =============================================================================

func TestIntegration_ConcurrentConsumption(t *testing.T) {
	cfg := integrationConfig(t, "it_conc", nil)
	cfg.MaxNumberOfMessages = 3
	cfg.ConsumerConcurrency = 3
	pub := newTestPublisher(t, cfg)
	consumer := newTestConsumer(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var inFlight, peak atomic.Int32
	done := make(chan struct{}, 3)
	go func() {
		_ = consumer.Consume(ctx, func(context.Context, Delivery) error {
			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
			inFlight.Add(-1)
			done <- struct{}{}
			return nil
		})
	}()

	for i := range 3 {
		if err := pub.Publish(ctx, map[string]int{"n": i}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	for range 3 {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("not all messages processed before timeout")
		}
	}

	if got := peak.Load(); got < 2 {
		t.Errorf("peak concurrent handlers = %d, want at least 2", got)
	}
}
