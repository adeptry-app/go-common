package health

import (
	"context"
	"errors"
	"testing"
)

func TestNewRabbitMQChecker(t *testing.T) {
	checker := NewRabbitMQChecker(nil)

	if checker == nil {
		t.Fatal("expected checker to not be nil")
	}
}

func TestRabbitMQChecker_Name(t *testing.T) {
	checker := NewRabbitMQChecker(nil)

	if checker.Name() != "rabbitmq" {
		t.Errorf("expected name 'rabbitmq', got %s", checker.Name())
	}
}

func TestRabbitMQChecker_Check_NilConnection(t *testing.T) {
	checker := NewRabbitMQChecker(nil)

	result := checker.Check(context.Background())

	if result.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status for nil connection, got %s", result.Status)
	}
	if result.Error != "connection is nil" {
		t.Errorf("expected 'connection is nil' error, got %s", result.Error)
	}
}

func TestNewRabbitMQCheckerWithProvider_NilProvider(t *testing.T) {
	checker := NewRabbitMQCheckerWithProvider(nil)

	result := checker.Check(context.Background())

	if result.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status for nil provider, got %s", result.Status)
	}
}

func TestNewRabbitMQCheckerWithProvider_Name(t *testing.T) {
	checker := NewRabbitMQCheckerWithProvider(nil)

	if checker.Name() != "rabbitmq" {
		t.Errorf("expected name 'rabbitmq', got %s", checker.Name())
	}
}

func TestQueueDepthChecker_Name(t *testing.T) {
	checker := NewQueueDepthChecker(nil, "contact_messages_dlq", 0)

	want := "queue:contact_messages_dlq"
	if checker.Name() != want {
		t.Errorf("expected name %q, got %s", want, checker.Name())
	}
}

func TestConsumerChecker_Name(t *testing.T) {
	checker := NewConsumerChecker(nil)

	if checker.Name() != "consumer" {
		t.Errorf("expected name 'consumer', got %s", checker.Name())
	}
}

func TestConsumerChecker_Check(t *testing.T) {
	tests := []struct {
		name       string
		provider   func() error
		wantStatus Status
		wantError  string
	}{
		{"nil provider", nil, StatusUnhealthy, "consumption state unavailable"},
		{"consuming", func() error { return nil }, StatusHealthy, ""},
		{
			name:       "consumption stopped",
			provider:   func() error { return errors.New("reconnect attempts exhausted") },
			wantStatus: StatusUnhealthy,
			wantError:  "consumption stopped: reconnect attempts exhausted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewConsumerChecker(tt.provider).Check(context.Background())

			if result.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s", result.Status, tt.wantStatus)
			}
			if result.Error != tt.wantError {
				t.Errorf("error = %q, want %q", result.Error, tt.wantError)
			}
		})
	}
}

func TestQueueDepthChecker_Check_NilConnection(t *testing.T) {
	checker := NewQueueDepthChecker(nil, "contact_messages_dlq", 0)

	result := checker.Check(context.Background())

	if result.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status for nil connection, got %s", result.Status)
	}
	if result.Error != "connection unavailable" {
		t.Errorf("expected 'connection unavailable' error, got %s", result.Error)
	}
}
