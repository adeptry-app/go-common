# Testing Guide

## Overview

The go-common library uses Go's standard `testing` package for unit tests.

## Quick Commands

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run specific test
go test -v -run TestNewVerifier ./jwt/

# Run all JWT tests
go test -v ./jwt/

# Run all Health tests
go test -v ./health/
```

## Test Files

**`jwt/jwt_test.go`**

| Category | Coverage |
| -------- | -------- |
| Constructor | NewIssuer, NewVerifier, key pairing, duplicate `kid`, format and expiry errors |
| Token Generation | Access tokens, refresh tokens, service tokens, claims, scopes |
| Token Validation | Valid, expired, tampered, malformed, algorithm confusion, unknown `kid` |
| Audience | Browser vs service audiences; each verifier rejects tokens meant for another |
| Key Rotation | Multiple public keys accepted; retired key stops validating |
| Scopes | Access/refresh with scopes, nil/empty, defensive copy |
| Token Type | `token_type` claim set per token kind; typed validators reject the other kind |
| Profile Claims | Email, display name, verified flag on access tokens |
| Expiry Handling | TTL, boundary conditions |
| Concurrency | Thread-safety verification |

**`health/health_test.go`**

| Category | Coverage |
| -------- | -------- |
| Aggregator | Constructor, Register, nil guard, no checkers, timeout |
| Health Status | Healthy, unhealthy, degraded, priority |
| Timeout | Context cancellation |
| HTTP Handler | 200 OK, 503 responses |
| Concurrency | Thread-safe Register |

**`health/postgres_test.go`**

| Category | Coverage |
| -------- | -------- |
| Constructor | NewPostgresChecker, Name |
| Error Handling | Nil database |

**`database/pgx_test.go`**

| Category | Coverage |
| -------- | -------- |
| Config Builder | Connection fields, defaults, application_name, sslmode fallback |
| NewPgxPool | Ping failure returns wrapped error, no pool leaked |
| Options | WithPoolSize, option overrides defaults |

**`health/pgx_test.go`**

| Category | Coverage |
| -------- | -------- |
| Constructor | NewPgxChecker, Name |
| Error Handling | Nil pool; ping failure reports unhealthy with latency |

**`health/rabbitmq_test.go`**

| Category | Coverage |
| -------- | -------- |
| Constructor | NewRabbitMQChecker, provider variant, QueueDepthChecker, Name |
| Error Handling | Nil connection, nil provider |

**`health/redis_test.go`**

| Category | Coverage |
| -------- | -------- |
| Constructor | NewRedisChecker, Name |
| Error Handling | Nil client |

**`health/minio_test.go`**

| Category | Coverage |
| -------- | -------- |
| Constructor | NewMinIOChecker, Name |
| Error Handling | Nil client with/without bucket |

**`queue/publisher_test.go`**

| Category | Coverage |
| -------- | -------- |
| Helper Methods | RetryQueues, DLQName, DLXName, MaxRetries |
| Error Definitions | All publisher errors |
| Validation | PublishToRetry bounds checking |
| Close | Idempotent close behavior |
| Jitter | jitteredExpiration bounds, disabled cases, clamping |
| Interface | Publisher interface compliance |

**`queue/consumer_test.go`**

| Category | Coverage |
| -------- | -------- |
| GetRetryCount | Header parsing, incl. int8/int16/negative |
| WillRetry | Retry-vs-DLQ predicate, boundary cases |
| Panic Recovery | invokeHandler converts panics, passes results through |
| Constants | RetryCountHeader value |
| Error Definitions | All consumer errors |
| Close | Idempotent close behavior |
| Interface | Consumer interface compliance |

**`queue/permanent_test.go`**

| Category | Coverage |
| -------- | -------- |
| Permanent | nil handling, errors.Is matching, wrapping, Unwrap |

**`queue/connection_test.go`**

| Category | Coverage |
| -------- | -------- |
| Backoff | Exponential growth, cap, jitter bounds, defaults |
| Helpers | closeError formatting |

**`queue/integration_test.go`** (RabbitMQ via testcontainers)

| Category | Coverage |
| -------- | -------- |
| Happy Path | Publish/consume, correlation ID propagation |
| Retry/DLQ | Retry ladder counts, max retries to DLQ, permanent to DLQ |
| Panic Recovery | Panic rides retry ladder to DLQ, consumption continues |
| Confirms | Publisher confirm mode |
| Reconnection | Publisher and consumer recovery after connection loss |
| Shutdown | Requeue without burning retry, Close waits for in-flight |
| Concurrency | Parallel handlers reach configured concurrency |
| Misc | ErrAlreadyConsuming, jittered per-message TTL |

Integration tests are skipped with `-short` or when Docker is unavailable.

**`config/rabbitmq_test.go`**

| Category | Coverage |
| -------- | -------- |
| URL | URL generation with credentials |
| RetryDelays | Defaults, parsing, panic cases |
| WithDefaults | Zero-value normalization, explicit values kept |
| Prefixed Env | Prefix override, fallback (incl. whitespace-only), defaults, new fields, jitter validation |
| Bool Parsing | Accepted forms incl. trimming; malformed values panic naming the resolved variable |
| Consumer Settings | PrefetchCount, ConsumerTag fields |

**`config/helpers_test.go`**

| Category | Coverage |
| -------- | -------- |
| GetEnvBool | Accepted forms, defaults, trimming; panics for yes/no/on/off/t/f |

**`config/redis_test.go`**

| Category | Coverage |
| -------- | -------- |
| TLS | Defaults to false, enabled form, malformed value panics |

**`logger/logger_test.go`**

| Category | Coverage |
| -------- | -------- |
| NewFromEnv | Defaults, level from env, invalid LOG_SOURCE panics |

**`metrics/metrics_test.go`** and **`metrics/queue_test.go`**

| Category | Coverage |
| -------- | -------- |
| HTTP Middleware | Path labels use the matched route; unmatched paths collapse to a sentinel |
| Queue | Publish/consume counters, reconnects, queue depth |

**`middleware/auth_test.go`**

| Category | Coverage |
| -------- | -------- |
| GetIdentity | Full identity, unauthenticated cases (unset, wrong type, no user ID) |
| ValidateToken | Refresh tokens rejected on access-token routes |
| ExtractToken | Cookie preferred over header; strict `Bearer` scheme |

**`middleware/permission_test.go`**

| Category | Coverage |
| -------- | -------- |
| HasPermission | Hierarchical levels, fail-safe for invalid levels |
| ValidLevel | Valid/invalid level strings |
| RequirePermission | No identity, all permission levels |
| Panic Handling | Invalid level causes panic at startup |
| Constants | Level constant values |
| Response Details | Forbidden response includes resource/required/have |

## Key Testing Patterns

**Mock Checker**: Function fields allow per-test behavior customization

```go
checker := &mockChecker{
    name:   "db",
    result: CheckResult{Status: StatusHealthy, Latency: "1ms"},
}
```

**HTTP Testing**: Uses `httptest.ResponseRecorder` with Gin router

```go
w := httptest.NewRecorder()
req, _ := http.NewRequest(http.MethodGet, "/health", nil)
router.ServeHTTP(w, req)
```

**Table-driven tests**: Multiple scenarios with `tests := []struct{...}`

**Concurrency**: Goroutines + channels for thread-safety verification

## Contributing Tests

1. Follow naming: `Test<FunctionName>_<Scenario>`
2. Organize by function with section markers
3. Use table-driven tests for multiple scenarios
4. Account for JWT second precision in timing tests
5. Clean up resources with `defer` or `t.Cleanup()`
6. Verify: `go test -cover ./...`
