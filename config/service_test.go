package config

import (
	"net"
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

func TestNewServiceConfig_MaxBodySize(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int64
	}{
		{"unset takes the default", "", defaultMaxBodySize},
		{"explicit override", "1048576", 1 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", "https://example.com")
			t.Setenv("MAX_BODY_SIZE", tt.env)

			cfg := NewServiceConfig(8080)

			if cfg.MaxBodySize != tt.want {
				t.Errorf("MaxBodySize = %d, want %d", cfg.MaxBodySize, tt.want)
			}
		})
	}
}

// A sub-KiB cap rejects payloads every service sends: a misread variable.
func TestNewServiceConfig_RejectsTinyMaxBodySize(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://example.com")
	t.Setenv("MAX_BODY_SIZE", "512")

	defer func() {
		if recover() == nil {
			t.Error("MAX_BODY_SIZE=512 should fail validation")
		}
	}()
	NewServiceConfig(8080)
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

// gin trusts every proxy by default, so an unset variable must still yield a
// list that excludes public peers rather than an empty one.
func TestNewServiceConfig_TrustedProxies(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://example.com")
	t.Setenv("TRUSTED_PROXIES", "")

	cfg := NewServiceConfig(8080)

	if len(cfg.TrustedProxies) != len(defaultTrustedProxies) {
		t.Fatalf("TrustedProxies = %v, want the private-range default", cfg.TrustedProxies)
	}
	for _, cidr := range cfg.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			t.Errorf("default entry %q is not a CIDR: %v", cidr, err)
		}
	}
}

// A whitespace-only value would otherwise reach the CORS header verbatim.
func TestNewServiceConfig_AllowedMethods(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"unset falls back", "", "GET,POST,PUT,PATCH,DELETE,OPTIONS"},
		{"whitespace falls back", "  ", "GET,POST,PUT,PATCH,DELETE,OPTIONS"},
		{"narrowed and normalised", "GET, POST ,OPTIONS,", "GET,POST,OPTIONS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", "https://example.com")
			t.Setenv("CORS_ALLOWED_METHODS", tt.env)

			if got := NewServiceConfig(8080).AllowedMethods; got != tt.want {
				t.Errorf("AllowedMethods = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewServiceConfig_RejectsNonCIDRTrustedProxy(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://example.com")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1")

	defer func() {
		if recover() == nil {
			t.Error("a bare address should fail CIDR validation")
		}
	}()
	NewServiceConfig(8080)
}
