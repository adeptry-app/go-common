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

- `ServiceConfig` - Port, environment, allowed origins, request deadline, body
  cap, trusted proxies
- `DatabaseConfig` - PostgreSQL connection settings
- `JWTConfig` - JWT signing keys, access audience, expiration (issuer only)
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
- `SWAGGER_HOST` - Swagger UI host; empty disables swagger
- `REQUEST_TIMEOUT` - Per-request deadline (default `30s`, minimum `1s`). Read by
  `middleware.Timeout`, `database.WithStatementTimeout` and
  `server.Config.RequestTimeout` so the three cannot disagree
- `MAX_BODY_SIZE` - Request body cap in bytes (default `65536`, minimum `1024`).
  Applied by `middleware.BodyLimit`
- `TRUSTED_PROXIES` - CIDRs whose `X-Forwarded-For` gin may believe,
  comma-separated (default: loopback plus RFC1918/ULA). Applied by
  `server.NewRouter`; gin trusts every peer until it is, which makes
  `c.ClientIP()` caller-controlled
- `CORS_ALLOWED_METHODS` - `Access-Control-Allow-Methods`, comma-separated
  (default `GET,POST,PUT,PATCH,DELETE,OPTIONS`). Narrow it to the verbs the
  service actually routes

### DatabaseConfig

- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` - Required
- `DB_SSLMODE` - Optional (disable, allow, prefer, require, verify-ca, verify-full)

### JWTConfig

Loaded by the token issuer (auth-service) only. Verifying services read
`JWT_PUBLIC_KEYS` themselves via `GetEnvRequired` and pass their own audience.

- `JWT_PRIVATE_KEY` - Signing key, one `<kid>:<base64 PKCS8 DER>` (required)
- `JWT_PUBLIC_KEYS` - Verification keys, comma-separated
  `<kid>:<base64 PKIX DER>` (required)
- `JWT_ACCESS_AUDIENCE` - Services the access cookie reaches, comma-separated
  (required). `auth-service` is always added.
- `JWT_ACCESS_EXPIRY` - Access token lifetime (default `15m`)
- `JWT_REFRESH_EXPIRY` - Refresh token lifetime (default `168h`)

See the `jwt` package README for the key format and rotation order.

### RedisConfig

- `REDIS_HOST`, `REDIS_PORT` - Required
- Optional: `REDIS_PASSWORD`, `REDIS_TLS` (default false)

### S3Config

- `S3_ENDPOINT` - Storage endpoint URL (required)
- `S3_ACCESS_KEY`, `S3_SECRET_KEY` - Both or neither; omit to use AWS IAM role credentials
- `S3_USE_SSL` - Optional (default false)
- `S3_IMAGES_BUCKET`, `S3_DOCUMENTS_BUCKET`, `S3_MINIATURES_BUCKET`,
  `S3_AVATARS_BUCKET` - Optional, default to the local MinIO bucket names

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
