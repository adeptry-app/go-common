package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

// =============================================================================
// GetRetryCount Tests
// =============================================================================

func TestGetRetryCount(t *testing.T) {
	tests := []struct {
		name     string
		headers  amqp.Table
		expected int
	}{
		{
			name:     "nil headers returns 0",
			headers:  nil,
			expected: 0,
		},
		{
			name:     "empty headers returns 0",
			headers:  amqp.Table{},
			expected: 0,
		},
		{
			name:     "missing header returns 0",
			headers:  amqp.Table{"other-header": "value"},
			expected: 0,
		},
		{
			name:     "header with int8 value",
			headers:  amqp.Table{RetryCountHeader: int8(4)},
			expected: 4,
		},
		{
			name:     "header with int16 value",
			headers:  amqp.Table{RetryCountHeader: int16(6)},
			expected: 6,
		},
		{
			name:     "header with int32 value",
			headers:  amqp.Table{RetryCountHeader: int32(3)},
			expected: 3,
		},
		{
			name:     "header with negative value returns 0",
			headers:  amqp.Table{RetryCountHeader: int32(-2)},
			expected: 0,
		},
		{
			name:     "header with int64 value",
			headers:  amqp.Table{RetryCountHeader: int64(5)},
			expected: 5,
		},
		{
			name:     "header with int value",
			headers:  amqp.Table{RetryCountHeader: int(2)},
			expected: 2,
		},
		{
			name:     "header with zero value",
			headers:  amqp.Table{RetryCountHeader: int32(0)},
			expected: 0,
		},
		{
			name:     "header with string value returns 0",
			headers:  amqp.Table{RetryCountHeader: "invalid"},
			expected: 0,
		},
		{
			name:     "header with float value returns 0",
			headers:  amqp.Table{RetryCountHeader: 3.14},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delivery := amqp.Delivery{Headers: tt.headers}
			result := GetRetryCount(delivery)

			if result != tt.expected {
				t.Errorf("GetRetryCount() = %d, want %d", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// WillRetry Tests
// =============================================================================

func TestWillRetry(t *testing.T) {
	tests := []struct {
		name       string
		headers    amqp.Table
		maxRetries int
		expected   bool
	}{
		{
			name:       "first attempt with retries configured",
			headers:    nil,
			maxRetries: 3,
			expected:   true,
		},
		{
			name:       "last attempt that still retries",
			headers:    amqp.Table{RetryCountHeader: int32(2)},
			maxRetries: 3,
			expected:   true,
		},
		{
			name:       "retries exhausted",
			headers:    amqp.Table{RetryCountHeader: int32(3)},
			maxRetries: 3,
			expected:   false,
		},
		{
			name:       "count beyond max",
			headers:    amqp.Table{RetryCountHeader: int32(5)},
			maxRetries: 3,
			expected:   false,
		},
		{
			name:       "no retry queues configured",
			headers:    nil,
			maxRetries: 0,
			expected:   false,
		},
		{
			name:       "negative count treated as first attempt",
			headers:    amqp.Table{RetryCountHeader: int32(-1)},
			maxRetries: 1,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delivery := amqp.Delivery{Headers: tt.headers}
			if got := WillRetry(delivery, tt.maxRetries); got != tt.expected {
				t.Errorf("WillRetry() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// invokeHandler Tests
// =============================================================================

func TestInvokeHandler_RecoversPanic(t *testing.T) {
	c := &RabbitMQConsumer{logger: testLogger()}
	delivery := amqp.Delivery{MessageId: "m1", CorrelationId: "c1"}

	err := c.invokeHandler(context.Background(), delivery, func(context.Context, amqp.Delivery) error {
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
	c := &RabbitMQConsumer{logger: testLogger()}
	delivery := amqp.Delivery{}

	if err := c.invokeHandler(context.Background(), delivery, func(context.Context, amqp.Delivery) error {
		return nil
	}); err != nil {
		t.Errorf("invokeHandler() = %v, want nil for successful handler", err)
	}

	want := errors.New("transient")
	got := c.invokeHandler(context.Background(), delivery, func(context.Context, amqp.Delivery) error {
		return want
	})
	if !errors.Is(got, want) {
		t.Errorf("invokeHandler() = %v, want the handler's own error", got)
	}
}

// =============================================================================
// RetryCountHeader Tests
// =============================================================================

func TestRetryCountHeader_Value(t *testing.T) {
	want := "x-retry-count"
	if RetryCountHeader != want {
		t.Errorf("RetryCountHeader = %q, want %q", RetryCountHeader, want)
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
			name:    "ErrConsumeSetupFailed",
			err:     ErrConsumeSetupFailed,
			wantMsg: "failed to setup consumer",
		},
		{
			name:    "ErrNilPublisher",
			err:     ErrNilPublisher,
			wantMsg: "publisher is required",
		},
		{
			name:    "ErrAlreadyConsuming",
			err:     ErrAlreadyConsuming,
			wantMsg: "consumer is already consuming",
		},
		{
			name:    "ErrDeliveryChannelClosed",
			err:     ErrDeliveryChannelClosed,
			wantMsg: "delivery channel closed",
		},
		{
			name:    "ErrReconnectFailed",
			err:     ErrReconnectFailed,
			wantMsg: "reconnect attempts exhausted",
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
	consumer := &RabbitMQConsumer{
		closed: false,
	}

	// First close
	err := consumer.Close()
	if err != nil {
		t.Errorf("First Close() error = %v", err)
	}

	// Second close should also succeed (idempotent)
	err = consumer.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v, want nil (idempotent)", err)
	}

	if !consumer.closed {
		t.Error("Consumer should be marked as closed")
	}
}

func TestConsumerClose_AlreadyClosed(t *testing.T) {
	consumer := &RabbitMQConsumer{
		closed: true,
	}

	err := consumer.Close()
	if err != nil {
		t.Errorf("Close() on already closed consumer error = %v, want nil", err)
	}
}

// =============================================================================
// ConsumptionError Tests
// =============================================================================

func TestConsumptionError(t *testing.T) {
	tests := []struct {
		name     string
		consumer *RabbitMQConsumer
		wantErr  error
	}{
		{
			name:     "before Consume runs",
			consumer: &RabbitMQConsumer{},
			wantErr:  nil,
		},
		{
			name:     "while deliveries flow",
			consumer: &RabbitMQConsumer{consuming: true},
			wantErr:  nil,
		},
		{
			name: "while stuck retrying setup with unlimited attempts",
			consumer: &RabbitMQConsumer{
				consuming: true,
				runErr:    fmt.Errorf("setup failed after 7 attempt(s): %w", ErrConsumeSetupFailed),
			},
			wantErr: ErrConsumeSetupFailed,
		},
		{
			name:     "after Close",
			consumer: &RabbitMQConsumer{closed: true, runErr: ErrConsumerClosed},
			wantErr:  nil,
		},
		{
			name:     "after Close while stuck retrying setup",
			consumer: &RabbitMQConsumer{closed: true, runErr: ErrConsumeSetupFailed},
			wantErr:  nil,
		},
		{
			name:     "after context cancellation",
			consumer: &RabbitMQConsumer{runErr: context.Canceled},
			wantErr:  nil,
		},
		{
			name:     "after context deadline",
			consumer: &RabbitMQConsumer{runErr: context.DeadlineExceeded},
			wantErr:  nil,
		},
		{
			name: "after reconnect attempts exhausted",
			consumer: &RabbitMQConsumer{
				runErr: fmt.Errorf("%w: %v", ErrReconnectFailed, errors.New("dial tcp: refused")),
			},
			wantErr: ErrReconnectFailed,
		},
		{
			name:     "after delivery channel closed with reconnect disabled",
			consumer: &RabbitMQConsumer{runErr: ErrDeliveryChannelClosed},
			wantErr:  ErrDeliveryChannelClosed,
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
	var _ Consumer = (*RabbitMQConsumer)(nil)
}
