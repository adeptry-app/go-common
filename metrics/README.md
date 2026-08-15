# metrics

Prometheus metrics collection with Gin middleware.

## Usage

```go
import (
    "github.com/adeptry-app/go-common/metrics"
    "github.com/gin-gonic/gin"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

metricsCollector := metrics.New(metrics.Config{
    ServiceName: "public-api",
    Namespace:   "adeptry",
})

// HTTP metrics middleware
router.Use(metricsCollector.Middleware())

// Expose metrics endpoint
router.GET("/metrics", gin.WrapH(promhttp.Handler()))
```

## Collected Metrics

- `http_requests_total` - Total HTTP requests by method, path, status
- `http_request_duration_seconds` - Request latency histogram

## Queue Metrics

`QueueMetrics` implements the queue package's `MetricsRecorder` interface:

```go
import (
    "log"

    "github.com/adeptry-app/go-common/metrics"
    "github.com/adeptry-app/go-common/queue"
)

queueMetrics := metrics.NewQueueMetrics(metrics.Config{
    ServiceName: "ai-service",
    Namespace:   "adeptry",
})

publisher, err := queue.NewSQSPublisher(ctx, cfg, queue.WithPublisherMetrics(queueMetrics))
if err != nil {
    log.Fatal(err)
}
consumer, err := queue.NewSQSConsumer(ctx, cfg, logger, queue.WithConsumerMetrics(queueMetrics))
if err != nil {
    log.Fatal(err)
}

// Optional: poll and expose DLQ depth
queueMetrics.SetQueueDepth("emails_dlq", float64(depth))
```

- `queue_publishes_total{queue,status}` - Publish attempts (success/error)
- `queue_publish_duration_seconds{queue}` - Publish latency histogram
- `queue_consumes_total{queue,outcome}` - Processed messages (success,
  retry, dlq, requeued)
- `queue_consume_duration_seconds{queue}` - Handler execution time histogram
- `queue_depth{queue}` - Gauge for queue depth (set via `SetQueueDepth`)
