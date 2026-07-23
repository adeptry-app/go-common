# health

Dependency health checking with aggregated results.

## Usage

```go
import "github.com/adeptry-app/go-common/health"

// Create aggregator with timeout
healthAgg := health.NewAggregator(3 * time.Second)

// Register checkers
healthAgg.Register(health.NewPostgresChecker(db))
healthAgg.Register(health.NewRabbitMQCheckerWithProvider(publisher.Connection))
healthAgg.Register(
    health.NewQueueDepthChecker(publisher.Connection, publisher.DLQName(), 0))
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
    "rabbitmq": { "status": "healthy", "latency": "0.3ms" },
    "queue:contact_messages_dlq": {
      "status": "healthy",
      "latency": "0.8ms",
      "details": { "messages": 0 }
    }
  }
}
```

## HTTP Status Codes

- `200 OK` - All checks healthy
- `503 Service Unavailable` - Any check unhealthy or degraded

## Available Checkers

- `NewPostgresChecker(db *gorm.DB)` - PostgreSQL ping (GORM)
- `NewPgxChecker(pool *pgxpool.Pool)` - PostgreSQL ping (pgx pool); same
  "postgres" name as the GORM checker, register one or the other
- `NewRabbitMQChecker(conn *amqp.Connection)` - Connection status (fixed
  connection; goes stale if the owner reconnects)
- `NewRabbitMQCheckerWithProvider(provider func() *amqp.Connection)` -
  Connection status resolved on every check; use with the queue package's
  auto-reconnecting publisher/consumer (`publisher.Connection`)
- `NewQueueDepthChecker(provider, queueName, degradedThreshold)` - Reports a
  queue's message count under `details.messages` (DLQ visibility). With
  `degradedThreshold > 0` it turns degraded at that depth; note the default
  handler returns 503 for degraded
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
