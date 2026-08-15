package health

import (
	"context"
	"errors"
	"testing"
)

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
			provider:   func() error { return errors.New("failed to receive messages") },
			wantStatus: StatusUnhealthy,
			wantError:  "consumption stopped: failed to receive messages",
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
