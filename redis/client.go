// Package redis holds the shared Redis client and the fixed-window counter
// services rate-limit with.
package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/adeptry-app/go-common/config"
)

// Client defaults.
const (
	dialTimeout  = 5 * time.Second
	readTimeout  = 3 * time.Second
	writeTimeout = 3 * time.Second
	poolSize     = 20
	minIdleConns = 2
	maxRetries   = 2
)

// NewClient dials Redis from the shared RedisConfig and pings it.
func NewClient(cfg config.RedisConfig) (*goredis.Client, error) {
	options := &goredis.Options{
		Addr:         net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Password:     cfg.Password,
		DB:           0,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		PoolSize:     poolSize,
		MinIdleConns: minIdleConns,
		MaxRetries:   maxRetries,
		// Off by default, which makes go-redis swap the caller's context for
		// Background() and retry past any deadline the handler set.
		ContextTimeoutEnabled: true,
	}

	if cfg.TLS {
		options.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: cfg.Host,
		}
	}

	client := goredis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	return client, nil
}
