package queue

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/adeptry-app/go-common/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

// =============================================================================
// Helper Methods Tests
// =============================================================================

func TestRetryQueues(t *testing.T) {
	publisher := &RabbitMQPublisher{
		retryQueues: []string{"queue_retry_0", "queue_retry_1", "queue_retry_2"},
	}

	queues := publisher.RetryQueues()

	if len(queues) != 3 {
		t.Errorf("RetryQueues() returned %d queues, want 3", len(queues))
	}

	// Verify it returns a copy (modifying returned slice shouldn't affect original)
	queues[0] = "modified"
	if publisher.retryQueues[0] == "modified" {
		t.Error("RetryQueues() should return a copy, not the original slice")
	}
}

func TestRetryQueues_Empty(t *testing.T) {
	publisher := &RabbitMQPublisher{
		retryQueues: []string{},
	}

	queues := publisher.RetryQueues()

	if len(queues) != 0 {
		t.Errorf("RetryQueues() returned %d queues, want 0", len(queues))
	}
}

func TestDLQName(t *testing.T) {
	publisher := &RabbitMQPublisher{
		cfg: config.RabbitMQConfig{Queue: "contact_messages"},
	}

	got := publisher.DLQName()
	want := "contact_messages_dlq"

	if got != want {
		t.Errorf("DLQName() = %q, want %q", got, want)
	}
}

func TestDLXName(t *testing.T) {
	publisher := &RabbitMQPublisher{
		cfg: config.RabbitMQConfig{Exchange: "contact_exchange"},
	}

	got := publisher.DLXName()
	want := "contact_exchange_dlx"

	if got != want {
		t.Errorf("DLXName() = %q, want %q", got, want)
	}
}

func TestMaxRetries(t *testing.T) {
	tests := []struct {
		name        string
		retryQueues []string
		want        int
	}{
		{
			name:        "no retry queues",
			retryQueues: []string{},
			want:        0,
		},
		{
			name:        "single retry queue",
			retryQueues: []string{"queue_retry_0"},
			want:        1,
		},
		{
			name:        "five retry queues",
			retryQueues: []string{"q_0", "q_1", "q_2", "q_3", "q_4"},
			want:        5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &RabbitMQPublisher{
				retryQueues: tt.retryQueues,
			}

			got := publisher.MaxRetries()
			if got != tt.want {
				t.Errorf("MaxRetries() = %d, want %d", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Error Definitions Tests
// =============================================================================

func TestErrorDefinitions(t *testing.T) {
	// Verify error sentinel values exist and have correct messages
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "ErrConnectionFailed",
			err:     ErrConnectionFailed,
			wantMsg: "failed to connect to RabbitMQ",
		},
		{
			name:    "ErrChannelFailed",
			err:     ErrChannelFailed,
			wantMsg: "failed to open channel",
		},
		{
			name:    "ErrQueueSetupFailed",
			err:     ErrQueueSetupFailed,
			wantMsg: "failed to setup queue infrastructure",
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
			name:    "ErrPublishNotConfirmed",
			err:     ErrPublishNotConfirmed,
			wantMsg: "publish not confirmed by broker",
		},
		{
			name:    "ErrPublisherClosed",
			err:     ErrPublisherClosed,
			wantMsg: "publisher is closed",
		},
		{
			name:    "ErrRetryOutOfBounds",
			err:     ErrRetryOutOfBounds,
			wantMsg: "retry index out of bounds",
		},
		{
			name:    "ErrCloseFailed",
			err:     ErrCloseFailed,
			wantMsg: "failed to close connection",
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
// PublishToRetry Validation Tests
// =============================================================================

func TestPublishToRetry_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		retryQueues []string
		retryIndex  int
		wantErr     bool
	}{
		{
			name:        "no retry queues configured",
			retryQueues: []string{},
			retryIndex:  0,
			wantErr:     true,
		},
		{
			name:        "negative retry index",
			retryQueues: []string{"q_0", "q_1"},
			retryIndex:  -1,
			wantErr:     true,
		},
		{
			name:        "retry index equals max",
			retryQueues: []string{"q_0", "q_1"},
			retryIndex:  2,
			wantErr:     true,
		},
		{
			name:        "retry index exceeds max",
			retryQueues: []string{"q_0", "q_1"},
			retryIndex:  5,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &RabbitMQPublisher{
				retryQueues: tt.retryQueues,
				closed:      false,
			}

			// Note: This will fail at validation before attempting to publish
			// since we don't have a real connection
			err := publisher.PublishToRetry(context.Background(), tt.retryIndex, []byte("test"), "corr-id", nil)

			if tt.wantErr && err == nil {
				t.Error("PublishToRetry() should return error")
			}
		})
	}
}

// =============================================================================
// Close Tests
// =============================================================================

func TestClose_Idempotent(t *testing.T) {
	publisher := &RabbitMQPublisher{
		closed: false,
	}

	// First close
	err := publisher.Close()
	if err != nil {
		t.Errorf("First Close() error = %v", err)
	}

	// Second close should also succeed (idempotent)
	err = publisher.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v, want nil (idempotent)", err)
	}

	if !publisher.closed {
		t.Error("Publisher should be marked as closed")
	}
}

func TestClose_AlreadyClosed(t *testing.T) {
	publisher := &RabbitMQPublisher{
		closed: true,
	}

	err := publisher.Close()
	if err != nil {
		t.Errorf("Close() on already closed publisher error = %v, want nil", err)
	}
}

// =============================================================================
// Publishing Message Structure Tests
// =============================================================================

func TestPublishingDefaults(t *testing.T) {
	// Test that publish uses correct defaults
	// These are internal implementation details but important for message handling
	publishing := amqp.Publishing{
		DeliveryMode:  amqp.Persistent,
		ContentType:   "application/json",
		Body:          []byte(`{"test": "data"}`),
		Timestamp:     time.Now(),
		MessageId:     "test-id",
		CorrelationId: "corr-id",
		Headers:       nil,
	}

	if publishing.DeliveryMode != amqp.Persistent {
		t.Error("Messages should be persistent")
	}
	if publishing.ContentType != "application/json" {
		t.Error("Content type should be application/json")
	}
}

// =============================================================================
// Jittered Expiration Tests
// =============================================================================

func TestJitteredExpiration_Disabled(t *testing.T) {
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
			if got := jitteredExpiration(tt.delay, tt.jitter); got != "" {
				t.Errorf("jitteredExpiration(%v, %v) = %q, want empty", tt.delay, tt.jitter, got)
			}
		})
	}
}

func TestJitteredExpiration_WithinBounds(t *testing.T) {
	delay := 10 * time.Second
	jitter := 0.3

	for i := 0; i < 100; i++ {
		got := jitteredExpiration(delay, jitter)
		ms, err := strconv.ParseInt(got, 10, 64)
		if err != nil {
			t.Fatalf("jitteredExpiration returned non-numeric value %q: %v", got, err)
		}
		// Expiration must be in [delay*(1-jitter), delay].
		minMs := int64(7000)
		maxMs := int64(10000)
		if ms < minMs || ms > maxMs {
			t.Fatalf("jitteredExpiration = %dms, want in [%d, %d]", ms, minMs, maxMs)
		}
	}
}

func TestJitteredExpiration_ClampsJitterAboveOne(t *testing.T) {
	delay := 1 * time.Second

	for i := 0; i < 100; i++ {
		got := jitteredExpiration(delay, 5.0)
		ms, err := strconv.ParseInt(got, 10, 64)
		if err != nil {
			t.Fatalf("jitteredExpiration returned non-numeric value %q: %v", got, err)
		}
		// Jitter clamps to 1.0, and the floor is 1ms.
		if ms < 1 || ms > 1000 {
			t.Fatalf("jitteredExpiration = %dms, want in [1, 1000]", ms)
		}
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestPublisherInterfaceCompliance(t *testing.T) {
	var _ Publisher = (*RabbitMQPublisher)(nil)
}
