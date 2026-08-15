package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/adeptry-app/go-common/config"
)

// =============================================================================
// Test Doubles
// =============================================================================

// fakeSQS stands in for the SQS client: it records every call and hands out
// pre-loaded receive batches, blocking like a long poll once they run out.
type fakeSQS struct {
	mu         sync.Mutex
	sent       []*sqs.SendMessageInput
	deleted    []*sqs.DeleteMessageInput
	visibility []*sqs.ChangeMessageVisibilityInput
	requested  []int32

	batches    chan []types.Message
	sendErr    error
	deleteErr  error
	receiveErr error
}

func newFakeSQS() *fakeSQS {
	return &fakeSQS{batches: make(chan []types.Message, 8)}
}

func (f *fakeSQS) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, in)
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &sqs.SendMessageOutput{}, nil
}

func (f *fakeSQS) ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	f.mu.Lock()
	f.requested = append(f.requested, in.MaxNumberOfMessages)
	err := f.receiveErr
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}
	select {
	case batch := <-f.batches:
		return &sqs.ReceiveMessageOutput{Messages: batch}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeSQS) DeleteMessage(_ context.Context, in *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, in)
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &sqs.DeleteMessageOutput{}, nil
}

func (f *fakeSQS) ChangeMessageVisibility(_ context.Context, in *sqs.ChangeMessageVisibilityInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.visibility = append(f.visibility, in)
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

func (f *fakeSQS) counts() (sent, deleted, visibility int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent), len(f.deleted), len(f.visibility)
}

// batchSizes reports what each receive asked for.
func (f *fakeSQS) batchSizes() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int32(nil), f.requested...)
}

// waitForFirstReceive blocks until Consume has polled at least once.
func waitForFirstReceive(t *testing.T, fake *fakeSQS) {
	t.Helper()
	waitFor(t, 5*time.Second, "the first receive", func() bool {
		return len(fake.batchSizes()) > 0
	})
}

// testConfig is a unit-test config; no client is built from it.
func testConfig(retryDelays ...time.Duration) config.SQSConfig {
	return config.SQSConfig{
		QueueURL:    "http://sqs.local/000000000000/jobs",
		DLQURL:      "http://sqs.local/000000000000/jobs_dlq",
		Region:      "eu-west-1",
		RetryDelays: retryDelays,
	}
}

// testMessage builds a received SQS message with the given receive count.
func testMessage(body string, receiveCount int) types.Message {
	return types.Message{
		MessageId:     aws.String("msg-1"),
		ReceiptHandle: aws.String("receipt-1"),
		Body:          aws.String(body),
		Attributes: map[string]string{
			string(types.MessageSystemAttributeNameApproximateReceiveCount): fmt.Sprint(receiveCount),
		},
		MessageAttributes: map[string]types.MessageAttributeValue{
			correlationAttribute: stringAttribute("corr-1"),
		},
	}
}

// =============================================================================
// Delivery Tests
// =============================================================================

func TestDeliveryFrom(t *testing.T) {
	receivedAt := time.Now()
	d := deliveryFrom(testMessage(`{"id":1}`, 4), receivedAt)

	if string(d.Body) != `{"id":1}` {
		t.Errorf("Body = %q, want the message body", d.Body)
	}
	if d.MessageID != "msg-1" {
		t.Errorf("MessageID = %q, want msg-1", d.MessageID)
	}
	if d.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID = %q, want corr-1", d.CorrelationID)
	}
	if d.ReceiveCount != 4 {
		t.Errorf("ReceiveCount = %d, want 4", d.ReceiveCount)
	}
	if !d.ReceivedAt.Equal(receivedAt) {
		t.Errorf("ReceivedAt = %v, want %v", d.ReceivedAt, receivedAt)
	}
}

func TestDeliveryFrom_MissingAttributes(t *testing.T) {
	d := deliveryFrom(types.Message{Body: aws.String("{}")}, time.Now())

	if d.ReceiveCount != 1 {
		t.Errorf("ReceiveCount = %d, want 1 for a message with no attributes", d.ReceiveCount)
	}
	if d.CorrelationID != "" {
		t.Errorf("CorrelationID = %q, want empty", d.CorrelationID)
	}
	if d.MessageID != "" {
		t.Errorf("MessageID = %q, want empty", d.MessageID)
	}
}

// =============================================================================
// Retry Ladder Tests
// =============================================================================

func TestRetryDelay_IndexedByBusinessAttempt(t *testing.T) {
	ladder := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	c := newConsumer(newFakeSQS(), testConfig(ladder...), testLogger())

	tests := []struct {
		name         string
		receiveCount int
		err          error
		want         time.Duration
	}{
		{
			// The regression this design exists to prevent: operational
			// redeliveries must not push a first failure up the ladder.
			name:         "inflated receive count with attempt 1 takes the first step",
			receiveCount: 5,
			err:          WithAttempt(1, errors.New("transient")),
			want:         30 * time.Second,
		},
		{
			name:         "attempt indexes its own step",
			receiveCount: 1,
			err:          WithAttempt(2, errors.New("transient")),
			want:         2 * time.Minute,
		},
		{
			name:         "attempt past the ladder clamps to the last step",
			receiveCount: 1,
			err:          WithAttempt(9, errors.New("transient")),
			want:         10 * time.Minute,
		},
		{
			name:         "wrapped attempt is still read",
			receiveCount: 1,
			err:          fmt.Errorf("handler: %w", WithAttempt(2, errors.New("transient"))),
			want:         2 * time.Minute,
		},
		{
			// A pre-claim failure has no business attempt, so it backs off on
			// the receive count rather than spinning.
			name:         "no attempt falls back to the receive count",
			receiveCount: 2,
			err:          errors.New("database unreachable"),
			want:         2 * time.Minute,
		},
		{
			name:         "receive count past the ladder clamps to the last step",
			receiveCount: 7,
			err:          errors.New("database unreachable"),
			want:         10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Delivery{ReceiveCount: tt.receiveCount, ReceivedAt: time.Now()}

			if got := c.retryDelay(d, tt.err); got != tt.want {
				t.Errorf("retryDelay() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryDelay_ClampsToRemainingVisibilityCeiling(t *testing.T) {
	// SQS measures the 12h ceiling from the receive, not from this call, and
	// rejects an over-budget request instead of clamping it.
	c := newConsumer(newFakeSQS(), testConfig(11*time.Hour), testLogger())
	d := Delivery{ReceiveCount: 1, ReceivedAt: time.Now().Add(-2 * time.Hour)}

	got := c.retryDelay(d, WithAttempt(1, errors.New("transient")))

	if got >= 11*time.Hour {
		t.Errorf("retryDelay() = %v, want it reduced below the 11h step", got)
	}
	if got > config.MaxVisibilityTimeout-2*time.Hour {
		t.Errorf("retryDelay() = %v, want at most the 10h left of the ceiling", got)
	}
}

func TestRetryDelay_NoLadderConfigured(t *testing.T) {
	c := newConsumer(newFakeSQS(), testConfig(), testLogger())
	d := Delivery{ReceiveCount: 1, ReceivedAt: time.Now()}

	if got := c.retryDelay(d, errors.New("transient")); got != 0 {
		t.Errorf("retryDelay() = %v, want 0 with no ladder configured", got)
	}
}

// =============================================================================
// Jitter Tests
// =============================================================================

func TestJittered_Disabled(t *testing.T) {
	tests := []struct {
		name   string
		delay  time.Duration
		jitter float64
	}{
		{"zero jitter", 5 * time.Second, 0},
		{"negative jitter", 5 * time.Second, -0.5},
		{"zero delay", 0, 0.5},
		{"negative delay", -time.Second, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jittered(tt.delay, tt.jitter); got != tt.delay {
				t.Errorf("jittered(%v, %v) = %v, want %v", tt.delay, tt.jitter, got, tt.delay)
			}
		})
	}
}

func TestJittered_WithinBounds(t *testing.T) {
	delay := 10 * time.Second

	for range 100 {
		got := jittered(delay, 0.3)
		if got < 7*time.Second || got > delay {
			t.Fatalf("jittered = %v, want in [7s, 10s]", got)
		}
	}
}

func TestJittered_ClampsJitterAboveOne(t *testing.T) {
	delay := 1 * time.Second

	for range 100 {
		got := jittered(delay, 5.0)
		if got < 0 || got > delay {
			t.Fatalf("jittered = %v, want in [0, 1s]", got)
		}
	}
}

// =============================================================================
// Receive Backoff Tests
// =============================================================================

func TestReceiveBackoff_ExponentialGrowthWithCap(t *testing.T) {
	tests := []struct {
		attempt int
		base    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{6, 30 * time.Second}, // capped
		{10, 30 * time.Second},
	}

	for _, tt := range tests {
		got := receiveBackoff(tt.attempt)
		// Jitter adds up to 25% on top of the base delay.
		maxWithJitter := tt.base + tt.base/4
		if got < tt.base || got > maxWithJitter {
			t.Errorf("receiveBackoff(attempt=%d) = %v, want in [%v, %v]", tt.attempt, got, tt.base, maxWithJitter)
		}
	}
}

// =============================================================================
// invokeHandler Tests
// =============================================================================

func TestInvokeHandler_RecoversPanic(t *testing.T) {
	c := newConsumer(newFakeSQS(), testConfig(), testLogger())

	err := c.invokeHandler(context.Background(), Delivery{MessageID: "m1"}, func(context.Context, Delivery) error {
		panic("boom")
	})

	if err == nil {
		t.Fatal("invokeHandler should convert a panic into an error")
	}
	if !strings.Contains(err.Error(), "handler panic: boom") {
		t.Errorf("error = %q, want it to contain the panic value", err.Error())
	}
	// Panics ride the retry ladder; they must not classify as permanent.
	if errors.Is(err, ErrPermanent) {
		t.Error("panic error should not match ErrPermanent")
	}
}

func TestInvokeHandler_PassesThroughHandlerResult(t *testing.T) {
	c := newConsumer(newFakeSQS(), testConfig(), testLogger())

	if err := c.invokeHandler(context.Background(), Delivery{}, func(context.Context, Delivery) error {
		return nil
	}); err != nil {
		t.Errorf("invokeHandler() = %v, want nil for successful handler", err)
	}

	want := errors.New("transient")
	got := c.invokeHandler(context.Background(), Delivery{}, func(context.Context, Delivery) error {
		return want
	})
	if !errors.Is(got, want) {
		t.Errorf("invokeHandler() = %v, want the handler's own error", got)
	}
}

// =============================================================================
// Settling Tests
// =============================================================================

func TestProcessMessage_SuccessDeletes(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(time.Minute), testLogger())

	c.processMessage(context.Background(), testMessage("{}", 1), time.Now(), func(context.Context, Delivery) error {
		return nil
	})

	sent, deleted, visibility := fake.counts()
	if deleted != 1 || sent != 0 || visibility != 0 {
		t.Fatalf("sent=%d deleted=%d visibility=%d, want 0/1/0", sent, deleted, visibility)
	}
	if aws.ToString(fake.deleted[0].QueueUrl) != c.cfg.QueueURL {
		t.Errorf("deleted from %q, want the main queue", aws.ToString(fake.deleted[0].QueueUrl))
	}
}

func TestProcessMessage_PermanentQuarantines(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(time.Minute), testLogger())

	c.processMessage(context.Background(), testMessage(`{"bad":true}`, 1), time.Now(), func(context.Context, Delivery) error {
		return Permanent(errors.New("malformed payload"))
	})

	sent, deleted, visibility := fake.counts()
	if sent != 1 || deleted != 1 || visibility != 0 {
		t.Fatalf("sent=%d deleted=%d visibility=%d, want 1/1/0", sent, deleted, visibility)
	}

	dlqSend := fake.sent[0]
	if aws.ToString(dlqSend.QueueUrl) != c.cfg.DLQURL {
		t.Errorf("quarantined to %q, want the DLQ", aws.ToString(dlqSend.QueueUrl))
	}
	if aws.ToString(dlqSend.MessageBody) != `{"bad":true}` {
		t.Errorf("DLQ body = %q, want the source body", aws.ToString(dlqSend.MessageBody))
	}
	// The two calls are not atomic, so the copy carries the source id to dedupe on.
	if got := aws.ToString(dlqSend.MessageAttributes[sourceIDAttribute].StringValue); got != "msg-1" {
		t.Errorf("DLQ %s = %q, want msg-1", sourceIDAttribute, got)
	}
	if got := aws.ToString(dlqSend.MessageAttributes[correlationAttribute].StringValue); got != "corr-1" {
		t.Errorf("DLQ %s = %q, want corr-1", correlationAttribute, got)
	}
}

func TestProcessMessage_DLQSendFailureKeepsMessage(t *testing.T) {
	fake := newFakeSQS()
	fake.sendErr = errors.New("sqs unavailable")
	c := newConsumer(fake, testConfig(time.Minute), testLogger())

	c.processMessage(context.Background(), testMessage("{}", 1), time.Now(), func(context.Context, Delivery) error {
		return Permanent(errors.New("malformed payload"))
	})

	// Deleting after a failed quarantine would lose the body outright.
	if _, deleted, _ := fake.counts(); deleted != 0 {
		t.Errorf("deleted = %d, want 0 when the DLQ send failed", deleted)
	}
}

func TestProcessMessage_TransientHidesForLadderStep(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(30*time.Second, 2*time.Minute), testLogger())

	c.processMessage(context.Background(), testMessage("{}", 5), time.Now(), func(context.Context, Delivery) error {
		return WithAttempt(1, errors.New("provider timeout"))
	})

	sent, deleted, visibility := fake.counts()
	if visibility != 1 || sent != 0 || deleted != 0 {
		t.Fatalf("sent=%d deleted=%d visibility=%d, want 0/0/1", sent, deleted, visibility)
	}
	if got := fake.visibility[0].VisibilityTimeout; got != 30 {
		t.Errorf("VisibilityTimeout = %d, want 30 (first ladder step)", got)
	}
}

func TestProcessMessage_NoLadderLeavesQueueTimeout(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(), testLogger())

	c.processMessage(context.Background(), testMessage("{}", 1), time.Now(), func(context.Context, Delivery) error {
		return errors.New("database unreachable")
	})

	// Asking for zero would make the message immediately visible and spin it.
	if _, _, visibility := fake.counts(); visibility != 0 {
		t.Errorf("visibility changes = %d, want 0 with no ladder configured", visibility)
	}
}

func TestProcessMessage_ShutdownReturnsMessageImmediately(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(time.Hour), testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.processMessage(ctx, testMessage("{}", 1), time.Now(), func(handlerCtx context.Context, _ Delivery) error {
		return handlerCtx.Err()
	})

	sent, deleted, visibility := fake.counts()
	if visibility != 1 || sent != 0 || deleted != 0 {
		t.Fatalf("sent=%d deleted=%d visibility=%d, want 0/0/1", sent, deleted, visibility)
	}
	// Immediately visible again: a shutdown costs a receive, never a ladder step.
	if got := fake.visibility[0].VisibilityTimeout; got != 0 {
		t.Errorf("VisibilityTimeout = %d, want 0", got)
	}
}

func TestProcessMessage_ShutdownStillDeletesASucceededMessage(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(time.Minute), testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.processMessage(ctx, testMessage("{}", 1), time.Now(), func(context.Context, Delivery) error {
		return nil
	})

	// Settling runs on a detached context, so the work is not repeated.
	if _, deleted, _ := fake.counts(); deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

// =============================================================================
// Consume Tests
// =============================================================================

func TestConsume_DispatchesAndDrains(t *testing.T) {
	fake := newFakeSQS()
	fake.batches <- []types.Message{testMessage(`{"n":1}`, 1)}
	c := newConsumer(fake, testConfig(time.Minute), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	handled := make(chan Delivery, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.Consume(ctx, func(_ context.Context, d Delivery) error {
			handled <- d
			return nil
		})
	}()

	select {
	case d := <-handled:
		if string(d.Body) != `{"n":1}` {
			t.Errorf("handler body = %q, want the published body", d.Body)
		}
	case <-ctx.Done():
		t.Fatal("message was not handled before timeout")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Consume returned %v, want context.Canceled", err)
	}
	if _, deleted, _ := fake.counts(); deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

func TestConsume_RequestsNoMoreThanFreeSlots(t *testing.T) {
	fake := newFakeSQS()
	cfg := testConfig(time.Minute)
	cfg.MaxNumberOfMessages = 10
	cfg.ConsumerConcurrency = 2
	c := newConsumer(fake, cfg, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Consume(ctx, func(context.Context, Delivery) error { return nil })
	}()

	waitForFirstReceive(t, fake)

	cancel()
	<-done

	for i, requested := range fake.batchSizes() {
		if requested < 1 || requested > 2 {
			t.Errorf("receive %d asked for %d messages, want at most the concurrency of 2", i, requested)
		}
	}
}

func TestConsume_AlreadyConsuming(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Consume(ctx, func(context.Context, Delivery) error { return nil })
	}()

	waitForFirstReceive(t, fake)

	if err := c.Consume(ctx, func(context.Context, Delivery) error { return nil }); !errors.Is(err, ErrAlreadyConsuming) {
		t.Errorf("second Consume returned %v, want ErrAlreadyConsuming", err)
	}

	cancel()
	<-done
}

func TestConsume_CloseInterruptsLongPoll(t *testing.T) {
	fake := newFakeSQS()
	c := newConsumer(fake, testConfig(), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Consume(ctx, func(context.Context, Delivery) error { return nil })
	}()

	waitForFirstReceive(t, fake)

	// Close must not wait out the poll window before returning.
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()

	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; the long poll was not interrupted")
	}

	if err := <-done; !errors.Is(err, ErrConsumerClosed) {
		t.Errorf("Consume returned %v, want ErrConsumerClosed", err)
	}
}

func TestConsume_RetriesFailedReceives(t *testing.T) {
	fake := newFakeSQS()
	fake.receiveErr = errors.New("sqs unavailable")
	c := newConsumer(fake, testConfig(), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Consume(ctx, func(context.Context, Delivery) error { return nil })
	}()

	// Consume never returns while receives fail, so the retry state is the only
	// signal a health check has.
	waitFor(t, 5*time.Second, "the receive failure to surface on ConsumptionError", func() bool {
		return errors.Is(c.ConsumptionError(), ErrReceiveFailed)
	})

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Consume returned %v, want context.Canceled", err)
	}
}

// =============================================================================
// Error Definitions Tests
// =============================================================================

func TestConsumerErrorDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "ErrConsumerClosed",
			err:     ErrConsumerClosed,
			wantMsg: "consumer is closed",
		},
		{
			name:    "ErrAlreadyConsuming",
			err:     ErrAlreadyConsuming,
			wantMsg: "consumer is already consuming",
		},
		{
			name:    "ErrReceiveFailed",
			err:     ErrReceiveFailed,
			wantMsg: "failed to receive messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.wantMsg {
				t.Errorf("%s.Error() = %q, want %q", tt.name, tt.err.Error(), tt.wantMsg)
			}
		})
	}
}

// =============================================================================
// Close Tests
// =============================================================================

func TestConsumerClose_Idempotent(t *testing.T) {
	c := newConsumer(newFakeSQS(), testConfig(), testLogger())

	if err := c.Close(); err != nil {
		t.Errorf("First Close() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Second Close() error = %v, want nil (idempotent)", err)
	}
	if !c.closed {
		t.Error("Consumer should be marked as closed")
	}
}

func TestConsumerConsume_AfterClose(t *testing.T) {
	c := newConsumer(newFakeSQS(), testConfig(), testLogger())
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := c.Consume(context.Background(), func(context.Context, Delivery) error { return nil })
	if !errors.Is(err, ErrConsumerClosed) {
		t.Errorf("Consume after Close = %v, want ErrConsumerClosed", err)
	}
}

// =============================================================================
// ConsumptionError Tests
// =============================================================================

func TestConsumptionError(t *testing.T) {
	tests := []struct {
		name     string
		consumer *SQSConsumer
		wantErr  error
	}{
		{
			name:     "before Consume runs",
			consumer: &SQSConsumer{},
			wantErr:  nil,
		},
		{
			name:     "while deliveries flow",
			consumer: &SQSConsumer{consuming: true},
			wantErr:  nil,
		},
		{
			name: "while stuck retrying receives",
			consumer: &SQSConsumer{
				consuming: true,
				runErr:    fmt.Errorf("receive failed after 7 attempt(s): %w", ErrReceiveFailed),
			},
			wantErr: ErrReceiveFailed,
		},
		{
			name:     "after Close",
			consumer: &SQSConsumer{closed: true, runErr: ErrConsumerClosed},
			wantErr:  nil,
		},
		{
			name:     "after Close while stuck retrying receives",
			consumer: &SQSConsumer{closed: true, runErr: ErrReceiveFailed},
			wantErr:  nil,
		},
		{
			name:     "after context cancellation",
			consumer: &SQSConsumer{runErr: context.Canceled},
			wantErr:  nil,
		},
		{
			name:     "after context deadline",
			consumer: &SQSConsumer{runErr: context.DeadlineExceeded},
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.consumer.ConsumptionError()
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("ConsumptionError() = %v, want %v", got, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestConsumerInterfaceCompliance(t *testing.T) {
	var _ Consumer = (*SQSConsumer)(nil)
}
