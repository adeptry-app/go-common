package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adeptry-app/go-common/config"
)

// PgxPoolOption customizes the pgxpool configuration before the pool is
// created. Options run after defaults are applied and may change any field.
type PgxPoolOption func(*pgxpool.Config)

// WithPoolSize sets the maximum and minimum number of pool connections.
func WithPoolSize(maxConns, minConns int32) PgxPoolOption {
	return func(c *pgxpool.Config) {
		c.MaxConns = maxConns
		c.MinConns = minConns
	}
}

// NewPgxPool creates a pgx connection pool from the shared DatabaseConfig
// and verifies connectivity with a ping. appName is reported as the
// PostgreSQL application_name (visible in pg_stat_activity).
//
// Defaults: MaxConns 10, MinConns 2, MaxConnLifetime 1h, MaxConnIdleTime
// 10m, HealthCheckPeriod 30s, sslmode "disable" when cfg.SSLMode is empty.
// Override via options, e.g. WithPoolSize(25, 5).
func NewPgxPool(ctx context.Context, cfg config.DatabaseConfig, appName string, opts ...PgxPoolOption) (*pgxpool.Pool, error) {
	poolCfg, err := buildPgxConfig(cfg, appName, opts...)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// buildPgxConfig translates DatabaseConfig into a pgxpool configuration with
// defaults applied and options run last.
func buildPgxConfig(cfg config.DatabaseConfig, appName string, opts ...PgxPoolOption) (*pgxpool.Config, error) {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	q := url.Values{}
	q.Set("sslmode", sslMode)
	if appName != "" {
		q.Set("application_name", appName)
	}

	u := &url.URL{
		Scheme:   "postgresql",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     cfg.Name,
		RawQuery: q.Encode(),
	}

	poolCfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	// Defaults; lifetime and idle settings match the GORM Connect defaults.
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 10 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	for _, opt := range opts {
		opt(poolCfg)
	}

	return poolCfg, nil
}
