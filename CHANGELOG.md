# Changelog

## v1.4.0

Breaking: `middleware.Claims` / `GetClaims` become `SetIdentity` /
`GetIdentity(c) (jwt.Identity, bool)`; the gin context keys are unexported.
Writing them by hand (`c.Set("user_id", ...)`, common in route tests) still
compiles and now leaves the request unauthenticated.

Needs migration `V20260724120000` - `repository.ActionLog` writes
`audit.action_log.source`, which never existed, so every audit insert failed.

- `jwt.Service.ValidateAccessToken` / `ValidateRefreshToken` enforce
  `token_type`, returning `jwt.ErrWrongTokenType`; every call site hand-wrote it.
- New constants for values that were literals: `middleware.AccessTokenCookie`,
  `HeaderTokenTTL`, `queue.ComponentPublisher` / `ComponentConsumer`, three
  `audit.Action*`.
- Fixed: `AddTTLHeader` ran after `c.Next()` so `X-Token-TTL` never shipped;
  `database.Connect`'s unquoted DSN truncated passwords containing a space;
  `RequestLogger` logged `user_id` twice; `RequirePermission` returned 500 for an
  unusable identity; `GetEnvInt`/`Int64`/`Duration` now panic on malformed input
  like `GetEnvBool`.
- `database.Connect` takes `config.DatabaseConfig`; `PostgresConfig` is gone, so
  the four services drop their translation blocks. TimeZone and the pool sizes
  are fixed defaults - nobody overrode them.
- `renderer.SubjectForType` becomes `SubjectFor(emailType, data)`. `contact_form`
  has no static subject, so the old call returned `("", true)` and both API
  services stored an empty subject; it now comes from `data["subject"]`.
- Removed, no callers: four `ActionLogRepository` query methods, the metrics DB
  and external-call families, the `status` label on
  `http_request_duration_seconds`, `models.ContactMessage*`,
  `DeliveryStatusPending` (rejected by the table's CHECK constraint), the
  `utils` package, and the portfolio/miniatures models.

## v1.3.0

Breaking: `GenerateAccessToken` / `GenerateRefreshToken` take a `jwt.Identity`
instead of `(userID, username, scopes)`, and `jwt.Claims` embeds `Identity`
rather than declaring the identity fields itself. The token payload is
unchanged (embedded structs are inlined) and `claims.UserID` still resolves,
but `Claims` literals now need `Identity: jwt.Identity{...}`.

- `jwt.Identity` - the user a token is issued for, including `Email`,
  `EmailVerified` and `DisplayName`. The generators previously dropped those
  claims, so auth-service signed its own tokens with a private copy of the
  factory; one definition now serves both.

## v0.53.0

Additive release, no breaking API changes.

- `database.AuthContext` + `CallJSON` / `CallBool` / `CallDiscard` /
  `CallInto` - audited single-row SQL function calls: each runs
  `SELECT audit.set_context($1, $2, $3, $4)` plus the query in one pgx
  batch (one implicit transaction, one network round trip). Replaces
  per-service copies of the begin/set_context/query/commit plumbing.
- `logger.NewFromEnv(serviceName)` - builds the logger from `LOG_LEVEL`,
  `LOG_FORMAT`, and `LOG_SOURCE`. `LOG_SOURCE` goes through the strict
  boolean parser, so a typo panics at startup instead of silently reading
  as false.
- `middleware.Claims` + `GetClaims(c)` - typed reader for the identity
  values `ValidateToken` stores on the gin context. The key names are now
  exported constants (`middleware.CtxKeyUserID` etc.) used by the writer
  and every in-library reader (`RequirePermission`, `audit.GetUserID`,
  the request logger), so the contract can no longer drift; existing
  direct `c.Get` callers keep working unchanged.
- `config.RedisConfig.TLS` - loaded from `REDIS_TLS` (default false),
  mirroring `RabbitMQConfig.TLS`. Replaces service-local
  environment-string compares for deciding Redis TLS. Services that
  defaulted TLS on in production must set `REDIS_TLS=true` there (or keep
  their own default) when adopting the field.

## v0.52.0

Additive release, no breaking API changes.

**Behavior change** (review before upgrading): boolean environment
variables (`GetEnvBool` callers such as `COOKIE_SECURE` and `S3_USE_SSL`,
and the `RABBITMQ_*` booleans) now accept only case-insensitive
true/false/1/0 with surrounding whitespace ignored. Malformed values
(e.g. `yes`, `on`, `truex`) previously read
silently as false and now panic at startup, matching the strictness of
numeric and duration parsing. Empty or whitespace-only values keep the
default as before.

**Additive**:

- `queue.WillRetry(delivery, maxRetries)` - reports whether a delivery that
  fails with a transient error will be retried (true) or dead-lettered
  (false). The consumer's own routing decision uses the same function, so
  handler-side bookkeeping cannot drift from it. Errors matching
  `queue.ErrPermanent` go to the DLQ regardless.
- Handler panics are now recovered instead of crashing the process: logged
  at error level with the stack, converted to a transient handler error,
  and routed through the normal retry ladder to the DLQ. Previously a
  deterministically panicking message crash-looped the worker because
  broker redelivery, unlike retry-queue republishing, never increments
  the retry count.
- `database.NewPgxPool(ctx, cfg, appName, opts...)` - pgx connection pool
  built from the shared `config.DatabaseConfig` with a connectivity ping.
  Defaults: MaxConns 10, MinConns 2, MaxConnLifetime 1h, MaxConnIdleTime
  10m, HealthCheckPeriod 30s, sslmode "disable" when unset - production
  should set DB_SSLMODE=require, as with the GORM helper. Sizing per
  service via `database.WithPoolSize(maxConns, minConns)`; options receive
  the `*pgxpool.Config` and may change any field.
- `health.NewPgxChecker(pool)` - PostgreSQL health checker for pgx pools,
  reporting under the same "postgres" name as the GORM checker (register
  one or the other). Unlike earlier service-local copies, a nil pool reports
  unhealthy from Check instead of panicking in the constructor, matching
  the other checkers.
- `github.com/jackc/pgx/v5` is now a direct dependency.

## v0.51.0

Queue stack upgrade for long-running workers.

**Behavior changes** (no source changes required, review before upgrading):

- Publisher and consumer now reconnect automatically with exponential
  backoff after a dropped connection or channel, re-declaring topology.
  `Consume` no longer returns when the delivery channel closes; it resumes
  consumption and only returns on context cancellation, `Close()`, or after
  `RABBITMQ_RECONNECT_MAX_ATTEMPTS`. Set `RABBITMQ_RECONNECT=false`
  (`DisableReconnect`) for the old fail-fast behavior.
- `consumer.Close()` now waits for in-flight handlers to finish.
- If the consume context is cancelled while a message is being handled, the
  message is requeued without consuming a retry attempt (previously it
  entered the retry ladder).
- `Publish` uses the context correlation ID (`logger.GetCorrelationID`) as
  the message CorrelationId when present; the consumer injects it back into
  the handler context.
- Invalid numeric `RABBITMQ_*` values now panic at startup instead of
  falling back to defaults with a warning.
- `GetRetryCount` accepts int8/int16 headers and clamps negatives to 0.

**Additive**:

- `queue.Permanent(err)` / `queue.ErrPermanent` route a message straight to
  the DLQ without retries.
- Optional publisher confirms (`RABBITMQ_PUBLISHER_CONFIRMS`), returning
  `queue.ErrPublishNotConfirmed` on broker NACK.
- Optional retry jitter (`RABBITMQ_RETRY_JITTER`, 0-1 fraction) via
  per-message TTLs.
- Optional concurrent consumption (`RABBITMQ_CONSUMER_CONCURRENCY`).
- Configurable heartbeat (`RABBITMQ_HEARTBEAT`, default 10s) and client
  connection name (from `RABBITMQ_CONSUMER_TAG`).
- `config.NewRabbitMQConfigWithPrefix(prefix)` for per-queue env config
  with fallback to un-prefixed names.
- `queue.WithPublisherLogger/WithPublisherMetrics/WithConsumerMetrics`
  options; `metrics.NewQueueMetrics` Prometheus recorder (publish/consume
  totals and durations, reconnects, queue depth gauge).
- `health.NewRabbitMQCheckerWithProvider(provider)` - required with
  reconnection, the fixed-connection checker goes stale after a reconnect.
- `health.NewQueueDepthChecker(provider, queue, threshold)` exposing DLQ
  depth under `details.messages`; `CheckResult` gained a `Details` field.
- `config.RabbitMQConfig.WithDefaults()` normalizing zero-valued optional
  fields; applied automatically by the queue constructors so struct-literal
  configs behave like env-loaded ones.
- Integration test suite (testcontainers-go) covering publish/consume,
  retry ladder, DLQ, permanent errors, reconnection, confirms, shutdown
  semantics, and concurrency.

**Call-site recommendation**: replace
`health.NewRabbitMQChecker(publisher.Connection())` with
`health.NewRabbitMQCheckerWithProvider(publisher.Connection)`.

## v0.34.0

**BREAKING**: JWT token generation methods now require a `scopes` parameter.

```go
// Before (v0.33.0)
token, err := jwtService.GenerateAccessToken(userID, username)
refreshToken, err := jwtService.GenerateRefreshToken(userID, username)

// After (v0.34.0)
scopes := map[string]string{"profile": "read", "projects": "edit"}
token, err := jwtService.GenerateAccessToken(userID, username, scopes)
refreshToken, err := jwtService.GenerateRefreshToken(userID, username, scopes)

// For nil scopes (no permissions)
token, err := jwtService.GenerateAccessToken(userID, username, nil)
```

- `GenerateAccessToken(userID, username)` now requires third `scopes` param
- `GenerateRefreshToken(userID, username)` now requires third `scopes` param
- Add `Scopes` field to JWT `Claims` struct
- Add `middleware/permission.go` with `RequirePermission()` middleware
- Add permission level constants: `LevelNone`, `LevelRead`, `LevelEdit`, `LevelDelete`
- Auth middleware now extracts scopes from JWT into Gin context

## v0.33.0

- Add `health` package for dependency health checking
- Add `Connection()` method to RabbitMQPublisher for health checks
- Add per-package README files
- Restructure main README to link to package docs

## v0.32.0

- Add `CloseDB` helper function to database package
- Add `queue` package with RabbitMQ publisher, retry queues, and DLQ support

## v0.21.0

**BREAKING**: AuthMiddleware now uses local JWT validation.

```go
// Before (v0.20.0)
authMiddleware := middleware.NewAuthMiddleware("http://auth-service:8084/api/v1")

// After (v0.21.0)
jwtService, _ := jwt.NewValidatorOnly(os.Getenv("JWT_SECRET"))
authMiddleware := middleware.NewAuthMiddleware(jwtService)
```

- `NewAuthMiddleware(authServiceURL string, opts...)` changed to
  `NewAuthMiddleware(jwtService jwt.Service)`
- Add `jwt` package for local token validation
- Remove `WithTimeout` option (no longer needed)
- Services must provide `JWT_SECRET` environment variable

## v0.19.0

- Add `SSLMode` field to `DatabaseConfig` with validation
- New environment variable: `DB_SSLMODE` (optional, default: `disable`)
- Valid values: `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`

## v0.12.0

**BREAKING**: `ALLOWED_ORIGINS` environment variable is now required.

- No default value provided for security reasons
- Services will panic on startup if not configured
- Use comma-separated list: `ALLOWED_ORIGINS=http://localhost:8080,https://example.com`
