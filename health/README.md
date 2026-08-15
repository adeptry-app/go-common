# health

Dependency health checking with aggregated results.

## Usage

```go
import (
    "time"

    "github.com/adeptry-app/go-common/health"
)

// Create aggregator with timeout
healthAgg := health.NewAggregator(3 * time.Second)

// Register checkers
healthAgg.Register(health.NewPgxChecker(pool))
healthAgg.Register(health.NewConsumerChecker(consumer.ConsumptionError))
healthAgg.Register(health.NewRedisChecker(client))
healthAgg.Register(health.NewMinIOChecker(client, "bucket"))

// Use as Gin handler
router.GET("/health", healthAgg.Handler())
```

## Response Format

```json
{
  "status": "healthy",
  "checks": {
    "postgres": { "status": "healthy", "latency": "1.2ms" },
    "consumer": { "status": "healthy", "latency": "0.1ms" },
    "redis": { "status": "healthy", "latency": "0.3ms" }
  }
}
```

## HTTP Status Codes

- `200 OK` - All checks healthy
- `503 Service Unavailable` - Any check unhealthy or degraded

## Available Checkers

- `NewPgxChecker(pool *pgxpool.Pool)` - PostgreSQL ping, reported as "postgres"
- `NewConsumerChecker(provider func() error)` - Fails while a queue consumer is
  stopped or stuck retrying receives; pass `consumer.ConsumptionError`. A
  consumer that is receiving nothing has no connection to lose, so nothing else
  reveals it
- `NewRedisChecker(client *redis.Client)` - PING command
- `NewMinIOChecker(client *minio.Client, bucket string)` - Bucket check

## Status Types

- `healthy` - Check passed
- `degraded` - Partial failure (e.g., missing bucket)
- `unhealthy` - Check failed

## Timeout Configuration

Choose timeout based on your deployment:

- **App Runner**: 5s health check timeout → use 3s aggregator timeout
- **Docker Compose**: 10s default → use 3-5s aggregator timeout
- **Kubernetes**: configurable → match probe timeout minus buffer

The timeout is a hard deadline, not a hint to cooperative checkers. A checker
that never returns is reported `unhealthy` with `check did not complete within
<timeout>` and its late result is discarded, so one wedged dependency cannot
hold the probe open. The last 50ms of the window (half of it, under a 100ms
timeout) is reserved for checkers that honour cancellation to report their own
reason, so the whole run still fits inside the timeout you configured. A run cut
short by the caller's own context reports `check cancelled` instead, so a client
that hung up is not read back as a slow dependency.

## Custom Checkers

Implement the `Checker` interface:

```go
type MyChecker struct {
    client *MyClient
}

func (c *MyChecker) Name() string { return "myservice" }

func (c *MyChecker) Check(ctx context.Context) health.CheckResult {
    start := time.Now()
    if err := c.client.Ping(ctx); err != nil {
        return health.CheckResult{
            Status:  health.StatusUnhealthy,
            Latency: time.Since(start).String(),
            Error:   err.Error(),
        }
    }
    return health.CheckResult{
        Status:  health.StatusHealthy,
        Latency: time.Since(start).String(),
    }
}
```
