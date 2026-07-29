package config

import (
	"testing"
	"time"
)

func TestNewServiceConfig_RequestTimeout(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset takes the default", "", defaultRequestTimeout},
		{"seconds", "45s", 45 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", "https://example.com")
			// Blank first: GetEnvDuration treats empty as unset, so the default
			// case is exercised even when the dev environment sets the var.
			t.Setenv("REQUEST_TIMEOUT", tt.env)

			cfg := NewServiceConfig(8080)

			if cfg.RequestTimeout != tt.want {
				t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, tt.want)
			}
		})
	}
}

// The three appliers read one field; validation is what stops a value that
// would disable them all reaching those call sites.
func TestNewServiceConfig_RejectsSubSecondRequestTimeout(t *testing.T) {
	for _, value := range []string{"0s", "500ms"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", "https://example.com")
			t.Setenv("REQUEST_TIMEOUT", value)

			defer func() {
				if recover() == nil {
					t.Errorf("REQUEST_TIMEOUT=%s should fail validation", value)
				}
			}()
			NewServiceConfig(8080)
		})
	}
}
