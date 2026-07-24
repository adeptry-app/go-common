# config

Configuration management with validation and environment variable helpers.

## Usage

```go
import "github.com/adeptry-app/go-common/config"

// Service configuration
cfg := config.NewServiceConfig(8080)

// Database configuration
dbCfg := config.NewDatabaseConfig()

// Environment helpers
value := config.GetEnv("KEY", "default")
required := config.GetEnvRequired("KEY")
boolVal := config.GetEnvBool("FEATURE_FLAG", false)
intVal := config.GetEnvInt("PORT", 8080)
```

### Boolean values

`GetEnvBool` and the `RABBITMQ_*` boolean variables accept case-insensitive
`true`, `false`, `1`, or `0` (surrounding whitespace ignored). Empty or
whitespace-only values keep the default. Anything else (e.g. `yes`, `on`,
`t`) panics at startup instead of silently reading as false.

## Configuration Types

- `ServiceConfig` - Port, environment, allowed origins
- `DatabaseConfig` - PostgreSQL connection settings
- `JWTConfig` - JWT secret and expiration
- `RedisConfig` - Redis connection settings
- `S3Config` - MinIO/S3 storage settings
- `RabbitMQConfig` - RabbitMQ connection and queue settings
- `SweeperConfig` - stale-row sweeper tuning for queue workers
- `CookieConfig` - httpOnly cookie settings for authentication

## Environment Variables

### ServiceConfig

- `PORT` - Server port (default from constructor)
- `ENVIRONMENT` - development, staging, production
- `ALLOWED_ORIGINS` - CORS origins (required, comma-separated)

### DatabaseConfig

- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` - Required
- `DB_SSLMODE` - Optional (disable, allow, prefer, require, verify-ca, verify-full)

### RedisConfig

- `REDIS_HOST`, `REDIS_PORT` - Required
- Optional: `REDIS_PASSWORD`, `REDIS_TLS` (default false)

### RabbitMQConfig

- `RABBITMQ_HOST`, `RABBITMQ_PORT`, `RABBITMQ_USER`, `RABBITMQ_PASSWORD` - Required
- Optional: `RABBITMQ_TLS`, `RABBITMQ_EXCHANGE`, `RABBITMQ_QUEUE`,
  `RABBITMQ_RETRY_DELAYS`, `RABBITMQ_RETRY_JITTER`, `RABBITMQ_HEARTBEAT`,
  `RABBITMQ_PUBLISHER_CONFIRMS`, `RABBITMQ_RECONNECT`,
  `RABBITMQ_RECONNECT_MAX_ATTEMPTS`, `RABBITMQ_RECONNECT_INITIAL_DELAY`,
  `RABBITMQ_RECONNECT_MAX_DELAY`, `RABBITMQ_PREFETCH_COUNT`,
  `RABBITMQ_CONSUMER_TAG`, `RABBITMQ_CONSUMER_CONCURRENCY`

See the `queue` package README for defaults and semantics.

`NewRabbitMQConfigWithPrefix(prefix)` reads each variable as
`<prefix>RABBITMQ_*` with fallback to the un-prefixed name, allowing one
service to configure multiple queues independently.

### SweeperConfig

- `SWEEPER_INTERVAL` - Time between recovery passes (default `1m`)
- `SWEEPER_PENDING_AGE` - Age at which a never-claimed row is stale
  (default `2m`)
- `SWEEPER_PROCESSING_AGE` - Quiet time before an in-flight row is stale
  (defaults to one interval above the floor below)
- `SWEEPER_MAX_ATTEMPTS` - Attempt budget before a stale row is failed
  (default `4`)

`SWEEPER_PROCESSING_AGE` must exceed the longest `RABBITMQ_RETRY_DELAYS` rung
plus the worker's job timeout, or the sweeper double-publishes work that is
still legitimately waiting. That floor is deployment-specific, so
`NewSweeperConfig` takes both and panics on an override at or under it:

```go
cfg.Sweeper = config.NewSweeperConfig(cfg.RabbitMQ.RetryDelays, cfg.JobTimeout)
```

Recovery runs on the same timescale as the ladder: a 12h last rung means a dead
worker's row is also recovered around 12h later. Shorten the ladder if that is
too slow. See `queue.StaleSweeper` for the loop that consumes this config.

### CookieConfig

- `COOKIE_DOMAIN` - Cookie domain (e.g., ".example.com" for prod, "" for local)
- `COOKIE_SECURE` - true for HTTPS only (default: false)
- `COOKIE_SAMESITE` - Strict, Lax, or None (default: Lax)
- `COOKIE_PATH` - Access token cookie path (default: "/")
- `COOKIE_REFRESH_PATH` - Refresh token cookie path (default: "/"). Must match
  the refresh endpoint URL as seen by the browser (e.g., "/auth/v1/refresh" for
  dev, "/api/v1/auth/refresh" for prod)
