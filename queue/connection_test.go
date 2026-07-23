package queue

import (
	"testing"
	"time"

	"github.com/adeptry-app/go-common/config"
	amqp "github.com/rabbitmq/amqp091-go"
)

// =============================================================================
// reconnectDelay Tests
// =============================================================================

func TestReconnectDelay_ExponentialGrowthWithCap(t *testing.T) {
	cfg := config.RabbitMQConfig{
		ReconnectInitialDelay: 1 * time.Second,
		ReconnectMaxDelay:     8 * time.Second,
	}

	tests := []struct {
		attempt int
		base    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 8 * time.Second}, // capped
		{10, 8 * time.Second},
	}

	for _, tt := range tests {
		got := reconnectDelay(cfg, tt.attempt)
		// Jitter adds up to 25% on top of the base delay.
		maxWithJitter := tt.base + tt.base/4
		if got < tt.base || got > maxWithJitter {
			t.Errorf("reconnectDelay(attempt=%d) = %v, want in [%v, %v]", tt.attempt, got, tt.base, maxWithJitter)
		}
	}
}

func TestReconnectDelay_NormalizedZeroConfig(t *testing.T) {
	// reconnectDelay requires a normalized config; WithDefaults supplies the
	// backoff defaults for zero values.
	cfg := config.RabbitMQConfig{}.WithDefaults()

	got := reconnectDelay(cfg, 1)

	base := config.DefaultReconnectInitialDelay
	if got < base || got > base+base/4 {
		t.Errorf("reconnectDelay with normalized zero config = %v, want in [%v, %v]", got, base, base+base/4)
	}
}

// =============================================================================
// closeError Tests
// =============================================================================

func TestCloseError(t *testing.T) {
	if got := closeError(nil); got != "graceful close" {
		t.Errorf("closeError(nil) = %q, want %q", got, "graceful close")
	}

	amqpErr := &amqp.Error{Code: 320, Reason: "connection forced"}
	if got := closeError(amqpErr); got == "" || got == "graceful close" {
		t.Errorf("closeError(non-nil) = %q, want the error text", got)
	}
}
