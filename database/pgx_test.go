package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adeptry-app/go-common/config"
)

func testDatabaseConfig() config.DatabaseConfig {
	return config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "portfolio",
		Password: "secret",
		Name:     "portfolio",
		SSLMode:  "disable",
	}
}

// =============================================================================
// buildPgxConfig Tests
// =============================================================================

func TestBuildPgxConfig_ConnectionFields(t *testing.T) {
	cfg := testDatabaseConfig()
	cfg.Password = "p@ss:word/123"

	poolCfg, err := buildPgxConfig(cfg, "worker")
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}

	conn := poolCfg.ConnConfig
	if conn.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", conn.Host)
	}
	if conn.Port != 5432 {
		t.Errorf("Port = %d, want 5432", conn.Port)
	}
	if conn.Database != "portfolio" {
		t.Errorf("Database = %q, want portfolio", conn.Database)
	}
	if conn.User != "portfolio" {
		t.Errorf("User = %q, want portfolio", conn.User)
	}
	if conn.Password != "p@ss:word/123" {
		t.Errorf("Password = %q, special characters were not preserved", conn.Password)
	}
}

func TestBuildPgxConfig_Defaults(t *testing.T) {
	poolCfg, err := buildPgxConfig(testDatabaseConfig(), "worker")
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}

	if poolCfg.MaxConns != 10 {
		t.Errorf("MaxConns = %d, want 10", poolCfg.MaxConns)
	}
	if poolCfg.MinConns != 2 {
		t.Errorf("MinConns = %d, want 2", poolCfg.MinConns)
	}
	if poolCfg.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 1h", poolCfg.MaxConnLifetime)
	}
	if poolCfg.MaxConnIdleTime != 10*time.Minute {
		t.Errorf("MaxConnIdleTime = %v, want 10m", poolCfg.MaxConnIdleTime)
	}
	if poolCfg.HealthCheckPeriod != 30*time.Second {
		t.Errorf("HealthCheckPeriod = %v, want 30s", poolCfg.HealthCheckPeriod)
	}
}

func TestBuildPgxConfig_ApplicationName(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		want    string
		wantSet bool
	}{
		{"set when provided", "messaging-worker", "messaging-worker", true},
		{"omitted when empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			poolCfg, err := buildPgxConfig(testDatabaseConfig(), tt.appName)
			if err != nil {
				t.Fatalf("buildPgxConfig() error = %v", err)
			}

			got, ok := poolCfg.ConnConfig.RuntimeParams["application_name"]
			if ok != tt.wantSet {
				t.Fatalf("application_name set = %v, want %v", ok, tt.wantSet)
			}
			if ok && got != tt.want {
				t.Errorf("application_name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPgxConfig_SSLMode(t *testing.T) {
	// Empty SSLMode falls back to disable (no TLS).
	cfg := testDatabaseConfig()
	cfg.SSLMode = ""
	poolCfg, err := buildPgxConfig(cfg, "worker")
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}
	if poolCfg.ConnConfig.TLSConfig != nil {
		t.Error("empty SSLMode should fall back to disable (nil TLSConfig)")
	}

	// Explicit require enables TLS.
	cfg.SSLMode = "require"
	poolCfg, err = buildPgxConfig(cfg, "worker")
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Error("SSLMode require should set a TLS config")
	}
}

// =============================================================================
// NewPgxPool Tests
// =============================================================================

func TestNewPgxPool_PingFailureReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1 on loopback has no listener; the connectivity ping must fail
	// and surface a wrapped error instead of returning a broken pool.
	cfg := testDatabaseConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 1

	pool, err := NewPgxPool(ctx, cfg, "test")

	if err == nil {
		if pool != nil {
			pool.Close()
		}
		t.Fatal("NewPgxPool should fail when the database is unreachable")
	}
	if !strings.Contains(err.Error(), "ping database") {
		t.Errorf("error = %v, want it to wrap the ping failure", err)
	}
	if pool != nil {
		t.Error("pool should be nil on error")
	}
}

// =============================================================================
// Option Tests
// =============================================================================

func TestWithPoolSize(t *testing.T) {
	poolCfg, err := buildPgxConfig(testDatabaseConfig(), "api", WithPoolSize(25, 5))
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}

	if poolCfg.MaxConns != 25 {
		t.Errorf("MaxConns = %d, want 25", poolCfg.MaxConns)
	}
	if poolCfg.MinConns != 5 {
		t.Errorf("MinConns = %d, want 5", poolCfg.MinConns)
	}
}

func TestPgxPoolOption_OverridesDefaults(t *testing.T) {
	custom := func(c *pgxpool.Config) { c.MaxConnLifetime = 2 * time.Hour }

	poolCfg, err := buildPgxConfig(testDatabaseConfig(), "worker", custom)
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}

	if poolCfg.MaxConnLifetime != 2*time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 2h (option should override default)", poolCfg.MaxConnLifetime)
	}
}

// =============================================================================
// WithStatementTimeout Tests
// =============================================================================

func TestWithStatementTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    string
		wantSet bool
	}{
		{"seconds become milliseconds", 30 * time.Second, "30000", true},
		{"sub-second is kept", 250 * time.Millisecond, "250", true},
		{"zero leaves the server default", 0, "", false},
		{"negative leaves the server default", -time.Second, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			poolCfg, err := buildPgxConfig(testDatabaseConfig(), "worker", WithStatementTimeout(tt.timeout))
			if err != nil {
				t.Fatalf("buildPgxConfig() error = %v", err)
			}

			got, ok := poolCfg.ConnConfig.RuntimeParams["statement_timeout"]
			if ok != tt.wantSet {
				t.Fatalf("statement_timeout set = %v, want %v", ok, tt.wantSet)
			}
			if tt.wantSet && got != tt.want {
				t.Errorf("statement_timeout = %q, want %q", got, tt.want)
			}
		})
	}
}

// It must not clobber the parameters buildPgxConfig already set.
func TestWithStatementTimeout_KeepsOtherRuntimeParams(t *testing.T) {
	poolCfg, err := buildPgxConfig(testDatabaseConfig(), "worker", WithStatementTimeout(time.Second))
	if err != nil {
		t.Fatalf("buildPgxConfig() error = %v", err)
	}

	if got := poolCfg.ConnConfig.RuntimeParams["application_name"]; got != "worker" {
		t.Errorf("application_name = %q, want worker", got)
	}
}

// Postgres reads 0 as "no limit", so rounding down would disable the very cap
// the caller asked for.
func TestWithStatementTimeout_SubMillisecondRoundsUp(t *testing.T) {
	for _, d := range []time.Duration{time.Nanosecond, 500 * time.Microsecond, 999 * time.Microsecond} {
		t.Run(d.String(), func(t *testing.T) {
			poolCfg, err := buildPgxConfig(testDatabaseConfig(), "worker", WithStatementTimeout(d))
			if err != nil {
				t.Fatalf("buildPgxConfig() error = %v", err)
			}

			if got := poolCfg.ConnConfig.RuntimeParams["statement_timeout"]; got != "1" {
				t.Errorf("statement_timeout = %q, want \"1\"", got)
			}
		})
	}
}
