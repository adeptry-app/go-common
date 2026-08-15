package queue

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/adeptry-app/go-common/logger"
)

// =============================================================================
// Publish Tests
// =============================================================================

func TestPublish_SendsBodyAndCorrelationAttribute(t *testing.T) {
	fake := newFakeSQS()
	p := newPublisher(fake, testConfig(time.Minute))

	if err := p.Publish(context.Background(), map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(fake.sent))
	}
	sent := fake.sent[0]
	if aws.ToString(sent.QueueUrl) != p.cfg.QueueURL {
		t.Errorf("QueueUrl = %q, want the main queue", aws.ToString(sent.QueueUrl))
	}
	if aws.ToString(sent.MessageBody) != `{"hello":"world"}` {
		t.Errorf("MessageBody = %q, want the marshalled payload", aws.ToString(sent.MessageBody))
	}
	// SQS has no correlation-id field, so it travels as a message attribute.
	if aws.ToString(sent.MessageAttributes[correlationAttribute].StringValue) == "" {
		t.Error("expected a generated correlation id attribute")
	}
}

func TestPublish_UsesContextCorrelationID(t *testing.T) {
	fake := newFakeSQS()
	p := newPublisher(fake, testConfig(time.Minute))

	ctx := logger.AddCorrelationID(context.Background(), "corr-12345")
	if err := p.Publish(ctx, map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := aws.ToString(fake.sent[0].MessageAttributes[correlationAttribute].StringValue)
	if got != "corr-12345" {
		t.Errorf("correlation attribute = %q, want corr-12345", got)
	}
}

func TestPublish_MarshalFailure(t *testing.T) {
	fake := newFakeSQS()
	p := newPublisher(fake, testConfig(time.Minute))

	err := p.Publish(context.Background(), math.Inf(1))

	if !errors.Is(err, ErrMarshalFailed) {
		t.Errorf("Publish() = %v, want ErrMarshalFailed", err)
	}
	if len(fake.sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(fake.sent))
	}
}

func TestPublish_SendFailure(t *testing.T) {
	fake := newFakeSQS()
	fake.sendErr = errors.New("sqs unavailable")
	p := newPublisher(fake, testConfig(time.Minute))

	if err := p.Publish(context.Background(), map[string]string{"k": "v"}); !errors.Is(err, ErrPublishFailed) {
		t.Errorf("Publish() = %v, want ErrPublishFailed", err)
	}
}

func TestPublish_AfterClose(t *testing.T) {
	fake := newFakeSQS()
	p := newPublisher(fake, testConfig(time.Minute))

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := p.Publish(context.Background(), map[string]string{"k": "v"}); !errors.Is(err, ErrPublisherClosed) {
		t.Errorf("Publish() = %v, want ErrPublisherClosed", err)
	}
	if len(fake.sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(fake.sent))
	}
}

func TestPublish_ConcurrentWithClose(t *testing.T) {
	fake := newFakeSQS()
	p := newPublisher(fake, testConfig(time.Minute))

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A publish racing Close either lands or is rejected; nothing else.
			if err := p.Publish(context.Background(), map[string]string{"k": "v"}); err != nil && !errors.Is(err, ErrPublisherClosed) {
				t.Errorf("Publish() = %v, want nil or ErrPublisherClosed", err)
			}
		}()
	}

	if err := p.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
	wg.Wait()
}

// =============================================================================
// Helper Methods Tests
// =============================================================================

func TestMaxRetries(t *testing.T) {
	tests := []struct {
		name        string
		retryDelays []time.Duration
		want        int
	}{
		{
			name:        "no ladder",
			retryDelays: nil,
			want:        0,
		},
		{
			name:        "single step",
			retryDelays: []time.Duration{time.Minute},
			want:        1,
		},
		{
			name:        "five steps",
			retryDelays: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 11 * time.Hour},
			want:        5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPublisher(newFakeSQS(), testConfig(tt.retryDelays...))

			if got := p.MaxRetries(); got != tt.want {
				t.Errorf("MaxRetries() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSeconds(t *testing.T) {
	tests := []struct {
		name  string
		delay time.Duration
		want  int32
	}{
		{"whole seconds", 30 * time.Second, 30},
		{"rounds a fraction up", 90500 * time.Millisecond, 91},
		// Truncating these to 0 would mean "visible now" on a visibility
		// change and "unset" on a receive - jitter can produce both.
		{"rounds a sub-second delay up", 500 * time.Millisecond, 1},
		{"rounds the smallest delay up", time.Nanosecond, 1},
		{"zero stays zero", 0, 0},
		{"negative stays zero", -time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seconds(tt.delay); got != tt.want {
				t.Errorf("seconds(%v) = %d, want %d", tt.delay, got, tt.want)
			}
		})
	}
}

func TestQueueName(t *testing.T) {
	tests := []struct {
		queueURL string
		want     string
	}{
		{"https://sqs.eu-west-1.amazonaws.com/123456789012/ai_requests", "ai_requests"},
		{"http://localstack:4566/000000000000/emails_dlq", "emails_dlq"},
		{"emails", "emails"},
	}

	for _, tt := range tests {
		t.Run(tt.queueURL, func(t *testing.T) {
			if got := queueName(tt.queueURL); got != tt.want {
				t.Errorf("queueName(%q) = %q, want %q", tt.queueURL, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Error Definitions Tests
// =============================================================================

func TestErrorDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "ErrClientFailed",
			err:     ErrClientFailed,
			wantMsg: "failed to create SQS client",
		},
		{
			name:    "ErrMarshalFailed",
			err:     ErrMarshalFailed,
			wantMsg: "failed to marshal message",
		},
		{
			name:    "ErrPublishFailed",
			err:     ErrPublishFailed,
			wantMsg: "failed to publish message",
		},
		{
			name:    "ErrPublisherClosed",
			err:     ErrPublisherClosed,
			wantMsg: "publisher is closed",
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

func TestClose_Idempotent(t *testing.T) {
	p := newPublisher(newFakeSQS(), testConfig(time.Minute))

	if err := p.Close(); err != nil {
		t.Errorf("First Close() error = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Second Close() error = %v, want nil (idempotent)", err)
	}
	if !p.closed {
		t.Error("Publisher should be marked as closed")
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestPublisherInterfaceCompliance(t *testing.T) {
	var _ Publisher = (*SQSPublisher)(nil)
}
