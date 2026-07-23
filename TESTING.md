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
go test -v -run TestNewService ./jwt/

# Run all JWT tests
go test -v ./jwt/

# Run all Health tests
go test -v ./health/
```

## Test Files

**`jwt/jwt_test.go`** - 55+ tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Constructor | 7 | NewService, NewValidatorOnly, validation |
| Token Generation | 14 | Access tokens, refresh tokens, claims, scopes |
| Token Validation | 11 | Valid, expired, tampered, malformed |
| Scopes | 18+ | Access/refresh with scopes, nil/empty, defensive copy |
| Expiry Handling | 5 | TTL, boundary conditions |
| Concurrency | 2 | Thread-safety verification |

**`health/health_test.go`** - 15 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Aggregator | 6 | Constructor, Register, nil guard, no checkers, timeout |
| Health Status | 4 | Healthy, unhealthy, degraded, priority |
| Timeout | 1 | Context cancellation |
| HTTP Handler | 3 | 200 OK, 503 responses |
| Concurrency | 1 | Thread-safe Register |

**`health/postgres_test.go`** - 3 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Constructor | 2 | NewPostgresChecker, Name |
| Error Handling | 1 | Nil database |

**`database/pgx_test.go`** - 7 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Config Builder | 4 | Connection fields, defaults, application_name, sslmode fallback |
| NewPgxPool | 1 | Ping failure returns wrapped error, no pool leaked |
| Options | 2 | WithPoolSize, option overrides defaults |

**`health/pgx_test.go`** - 4 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Constructor | 2 | NewPgxChecker, Name |
| Error Handling | 2 | Nil pool; ping failure reports unhealthy with latency |

**`health/rabbitmq_test.go`** - 7 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Constructor | 4 | NewRabbitMQChecker, provider variant, QueueDepthChecker, Name |
| Error Handling | 3 | Nil connection, nil provider |

**`health/redis_test.go`** - 3 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Constructor | 2 | NewRedisChecker, Name |
| Error Handling | 1 | Nil client |

**`health/minio_test.go`** - 4 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Constructor | 2 | NewMinIOChecker, Name |
| Error Handling | 2 | Nil client with/without bucket |

**`queue/publisher_test.go`** - 14 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Helper Methods | 5 | RetryQueues, DLQName, DLXName, MaxRetries |
| Error Definitions | 1 | All publisher errors |
| Validation | 1 | PublishToRetry bounds checking |
| Close | 2 | Idempotent close behavior |
| Message Defaults | 1 | Persistent delivery, JSON content type |
| Jitter | 3 | jitteredExpiration bounds, disabled cases, clamping |
| Interface | 1 | Publisher interface compliance |

**`queue/consumer_test.go`** - 9 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| GetRetryCount | 1 | Header parsing (13 sub-tests, incl. int8/int16/negative) |
| WillRetry | 1 | Retry-vs-DLQ predicate (6 sub-tests, boundary cases) |
| Panic Recovery | 2 | invokeHandler converts panics, passes results through |
| Constants | 1 | RetryCountHeader value |
| Error Definitions | 1 | All consumer errors |
| Close | 2 | Idempotent close behavior |
| Interface | 1 | Consumer interface compliance |

**`queue/permanent_test.go`** - 7 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Permanent | 7 | nil handling, errors.Is matching, wrapping, Unwrap |

**`queue/connection_test.go`** - 3 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Backoff | 2 | Exponential growth, cap, jitter bounds, defaults |
| Helpers | 1 | closeError formatting |

**`queue/integration_test.go`** - 14 tests (RabbitMQ via testcontainers)

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| Happy Path | 2 | Publish/consume, correlation ID propagation |
| Retry/DLQ | 3 | Retry ladder counts, max retries to DLQ, permanent to DLQ |
| Panic Recovery | 1 | Panic rides retry ladder to DLQ, consumption continues |
| Confirms | 1 | Publisher confirm mode |
| Reconnection | 2 | Publisher and consumer recovery after connection loss |
| Shutdown | 2 | Requeue without burning retry, Close waits for in-flight |
| Concurrency | 1 | Parallel handlers reach configured concurrency |
| Misc | 2 | ErrAlreadyConsuming, jittered per-message TTL |

Integration tests are skipped with `-short` or when Docker is unavailable.

**`config/rabbitmq_test.go`** - 16 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| URL | 1 | URL generation with credentials |
| RetryDelays | 3 | Defaults, parsing, panic cases |
| WithDefaults | 2 | Zero-value normalization, explicit values kept |
| Prefixed Env | 6 | Prefix override, fallback (incl. whitespace-only), defaults, new fields, jitter validation |
| Bool Parsing | 2 | Accepted forms incl. trimming; malformed values panic naming the resolved variable |
| Consumer Settings | 2 | PrefetchCount, ConsumerTag fields |

**`config/helpers_test.go`** - 2 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| GetEnvBool | 2 | Accepted forms, defaults, trimming (12 sub-tests); panics for yes/no/on/off/t/f |

**`middleware/permission_test.go`** - 15 tests

| Category | Tests | Coverage |
| -------- | ----- | -------- |
| HasPermission | 19 | Hierarchical levels, fail-safe for invalid levels |
| ValidLevel | 9 | Valid/invalid level strings |
| RequirePermission | 9 | No scopes, invalid format, all permission levels |
| Panic Handling | 4 | Invalid level causes panic at startup |
| Constants | 1 | Level constant values |
| Response Details | 1 | Forbidden response includes resource/required/have |

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
