package server

import (
	"testing"
	"time"
)

func TestConfig_RequestTimeoutDerivesSocketDeadlines(t *testing.T) {
	tests := []struct {
		name           string
		requestTimeout time.Duration
		wantRead       time.Duration
		wantWrite      time.Duration
	}{
		{
			// The defect this guards: a 60s handler deadline silently truncated
			// by the 30s default write deadline.
			name:           "outlasts a handler deadline above the default",
			requestTimeout: 60 * time.Second,
			wantRead:       70 * time.Second,
			wantWrite:      70 * time.Second,
		},
		{
			name:           "leaves a margin below the default too",
			requestTimeout: 10 * time.Second,
			wantRead:       20 * time.Second,
			wantWrite:      20 * time.Second,
		},
		{"zero keeps the defaults", 0, 30 * time.Second, 30 * time.Second},
		{"negative keeps the defaults", -time.Second, 30 * time.Second, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Port: "8080", RequestTimeout: tt.requestTimeout}.withDefaults()

			if cfg.ReadTimeout != tt.wantRead {
				t.Errorf("ReadTimeout = %v, want %v", cfg.ReadTimeout, tt.wantRead)
			}
			if cfg.WriteTimeout != tt.wantWrite {
				t.Errorf("WriteTimeout = %v, want %v", cfg.WriteTimeout, tt.wantWrite)
			}
		})
	}
}

// The realistic wiring: DefaultConfig first, then the field. withDefaults runs
// again inside Run, and must not treat the already-filled 30s as intentional.
func TestConfig_RequestTimeoutSetAfterDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("8080")
	cfg.RequestTimeout = 60 * time.Second

	cfg = cfg.withDefaults()

	if cfg.WriteTimeout != 70*time.Second {
		t.Errorf("WriteTimeout = %v, want 70s - a RequestTimeout set after DefaultConfig was ignored", cfg.WriteTimeout)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want it preserved", cfg.Port)
	}
}

// Post-commit work runs inside the handler on a context the request deadline no
// longer bounds, so the socket margin has to cover it too.
func TestConfig_MarginCoversPostCommitWork(t *testing.T) {
	const postCommitTimeout = 5 * time.Second // handlers.PostCommitTimeout

	if requestTimeoutMargin <= postCommitTimeout {
		t.Errorf("margin %v must exceed the post-commit budget %v", requestTimeoutMargin, postCommitTimeout)
	}
}

func TestConfig_RequestTimeoutIsIdempotent(t *testing.T) {
	cfg := Config{RequestTimeout: 45 * time.Second}.withDefaults().withDefaults()

	if cfg.WriteTimeout != 55*time.Second {
		t.Errorf("WriteTimeout = %v, want 55s", cfg.WriteTimeout)
	}
}
