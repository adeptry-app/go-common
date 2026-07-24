# queue

RabbitMQ message publishing and consuming with automatic reconnection, retry
support, dead letter queues, error classification, and optional publisher
confirms.

## Publisher Usage

```go
import (
    "log"

    "github.com/adeptry-app/go-common/queue"
)

publisher, err := queue.NewRabbitMQPublisher(cfg,
    queue.WithPublisherLogger(appLogger),       // optional, for reconnect logs
    queue.WithPublisherMetrics(queueMetrics),   // optional Prometheus metrics
)
if err != nil {
    log.Fatal(err)
}
defer publisher.Close()

// Publish message (CorrelationId is taken from ctx when present, see Tracing)
err = publisher.Publish(ctx, message)

// Retry failed message (with headers for retry tracking)
err = publisher.PublishToRetry(ctx, retryIndex, body, correlationID, headers)

// Send to dead letter queue
err = publisher.PublishToDLQ(ctx, body, correlationID)
```

## Consumer Usage

```go
import (
    "context"
    "log"

    "github.com/adeptry-app/go-common/queue"
    amqp "github.com/rabbitmq/amqp091-go"
)

consumer, err := queue.NewRabbitMQConsumer(cfg, publisher, logger,
    queue.WithConsumerMetrics(queueMetrics),    // optional Prometheus metrics
)
if err != nil {
    log.Fatal(err)
}
defer consumer.Close()

// Consume messages (blocks until context cancelled or Close is called)
err = consumer.Consume(ctx, func(c context.Context, d amqp.Delivery) error {
    // Return nil to ACK.
    // Return an error to trigger the retry ladder.
    // Return queue.Permanent(err) to skip retries and go straight to the DLQ.
    return nil
})

// Get retry count from message headers
retryCount := queue.GetRetryCount(delivery)

// Inside a handler: will this delivery retry on a transient error, or is
// this the final attempt before the DLQ? Uses the same predicate the
// consumer applies after the handler returns.
finalAttempt := !queue.WillRetry(delivery, publisher.MaxRetries())
```

## Features

- Automatic exchange and queue declaration
- Automatic reconnection with exponential backoff (publisher and consumer)
- Configurable retry delays with TTL-based routing and optional jitter
- Dead letter queue for permanent failures
- Permanent error classification (`queue.Permanent` / `queue.ErrPermanent`)
- Optional publisher confirms
- Optional concurrent message processing
- Correlation ID propagation between HTTP requests and queue messages
- Optional Prometheus metrics (see the `metrics` package)
- Thread-safe publishing and consuming

## Delivery Semantics

Delivery is **at-least-once**. Handlers must be idempotent. Duplicates can
occur when:

- A handler succeeds but the ACK fails (broker redelivers).
- A message is republished to a retry queue but the ACK of the original
  fails (both copies eventually arrive).
- The consumer reconnects while messages were in flight.

The `redelivered` flag and `MessageId` on the delivery can support
deduplication where needed.

## Retry Flow

1. Message fails processing with an ordinary error: the consumer republishes
   it to the next retry queue with an incremented `x-retry-count` header and
   ACKs the original.
2. After the queue TTL expires, the message returns to the main queue.
3. When `retryCount >= MaxRetries()`, the message is NACKed and routes to the
   DLQ.
4. Errors matching `queue.ErrPermanent` skip the ladder entirely and route
   straight to the DLQ.
5. If the consume context is cancelled while a message is being handled
   (shutdown), the message is requeued without consuming a retry attempt.

Handlers that need to record the upcoming routing decision (e.g. "will
retry" vs "final attempt") should use `queue.WillRetry(delivery,
publisher.MaxRetries())` instead of re-deriving it from the retry count - it
is the same predicate the consumer applies, so the two cannot drift. A
handler returning a `queue.Permanent` error always goes to the DLQ
regardless of what `WillRetry` reported.

### Panics

A handler panic does not crash the worker: the consumer recovers it, logs
the stack at error level, and treats it as a transient handler error that
rides the retry ladder. A deterministically panicking message therefore
reaches the DLQ after the configured retries instead of crash-looping the
process. Mark input-dependent failures with `queue.Permanent` in the handler
when retrying is pointless.

### Retry Jitter

With `RetryJitter` set (0 to 1), each retry message gets a per-message TTL
randomly shortened by up to that fraction (e.g. jitter 0.2 on a 5m delay
yields TTLs between 4m and 5m). This spreads out retries of messages that
failed together. The queue-level TTL stays the upper bound, so existing
topologies are unaffected. Note RabbitMQ only expires messages at the queue
head, so under bursts a message may wait for messages ahead of it.

## Reconnection

Both publisher and consumer automatically re-establish dropped connections
and channels with exponential backoff (defaults: 1s initial, 30s cap, plus
jitter, unlimited attempts), re-declaring topology on success. Disable with
`DisableReconnect` / `RABBITMQ_RECONNECT=false` to restore the old fail-fast
behavior.

- Publishes issued while disconnected fail fast with `ErrPublishFailed`;
  callers decide whether to retry.
- `Consume` resumes delivery after reconnecting and then only returns on
  context cancellation, `Close()`, or when `ReconnectMaxAttempts` is
  exceeded (`ErrReconnectFailed`).
- The initial connection in the constructors remains fail-fast so
  misconfiguration is caught at startup. Wrap the constructor in a retry
  loop if you need patience at boot.
- AMQP heartbeats default to 10s (`RABBITMQ_HEARTBEAT`) so dead peers are
  detected even on quiet connections.

## Publisher Confirms

With `PublisherConfirms` enabled, `Publish` blocks until the broker confirms
the message was received (bounded by ctx) and returns
`ErrPublishNotConfirmed` on a broker NACK. This closes the window where a
broker crash between channel-accept and persist silently loses a message, at
the cost of one broker round-trip per publish. Off by default.

## Concurrency and Prefetch

`Consume` processes messages sequentially by default. Set
`ConsumerConcurrency` (and a matching `PrefetchCount`) to process N messages
in parallel; effective parallelism is `min(ConsumerConcurrency,
PrefetchCount)`.

For long-running handlers keep `PrefetchCount` equal to
`ConsumerConcurrency`: prefetched messages sit unacknowledged on this
consumer and are invisible to others until processed.

## Graceful Shutdown

- Cancelling the consume context stops fetching; in-flight handlers run to
  completion (handlers should honor ctx for long work) and their messages
  are requeued without burning a retry attempt if they fail.
- `Close()` on the consumer blocks until in-flight handlers finish and the
  connection is released. Never call it from inside a handler (it would
  deadlock waiting for that handler); cancel the consume context instead.
- Shut down the consumer before the publisher: in-flight handlers may still
  publish to retry queues.

## Tracing

`Publish` uses the correlation ID from the context
(`logger.GetCorrelationID`) as the message `CorrelationId` when present, so
messages link back to the originating HTTP request. The consumer puts the
delivery's correlation ID back into the handler context, so handlers using
`logger.FromContext` log it automatically. Retries preserve it.

## Connection Ownership

Both publisher and consumer own their own RabbitMQ connections.
`Connection()` returns the current connection for read-only purposes; the
pointer changes after a reconnect, so health checks must use the provider
form:

```go
healthAgg.Register(health.NewRabbitMQCheckerWithProvider(publisher.Connection))
```

**Do not call `conn.Close()` directly** - use `publisher.Close()` or
`consumer.Close()` instead.

## DLQ Monitoring

Expose the DLQ depth on the health endpoint (and optionally as a Prometheus
gauge via `metrics.QueueMetrics.SetQueueDepth`):

```go
healthAgg.Register(
    health.NewQueueDepthChecker(publisher.Connection, publisher.DLQName(), 0))
```

## Configuration

```go
cfg := config.RabbitMQConfig{
    Host:        "localhost",
    Port:        5672,
    User:        "guest",
    Password:    "guest",
    TLS:         false,            // Set to true for amqps:// (Amazon MQ, production)
    Exchange:    "messaging",
    Queue:       "contact_messages",
    RetryDelays: []time.Duration{1*time.Minute, 5*time.Minute, 30*time.Minute},
    RetryJitter: 0.2,              // optional, 0 disables

    PublisherConfirms: false,      // optional broker-confirmed publishes

    // Reconnection (zero values mean: enabled, unlimited, 1s initial, 30s cap)
    DisableReconnect:      false,
    ReconnectMaxAttempts:  0,
    ReconnectInitialDelay: time.Second,
    ReconnectMaxDelay:     30 * time.Second,

    // Consumer-specific (optional)
    PrefetchCount:       1,             // QoS prefetch count
    ConsumerTag:         "my-consumer", // Unique consumer identifier
    ConsumerConcurrency: 1,             // parallel handlers
}
```

## Environment Variables

| Variable | Required | Default | Description |
| -------- | -------- | ------- | ----------- |
| `RABBITMQ_HOST` | Yes | - | RabbitMQ hostname |
| `RABBITMQ_PORT` | Yes | - | RabbitMQ port |
| `RABBITMQ_USER` | Yes | - | Username |
| `RABBITMQ_PASSWORD` | Yes | - | Password |
| `RABBITMQ_TLS` | No | `false` | Use TLS (amqps://) connection |
| `RABBITMQ_EXCHANGE` | No | `contact_messages` | Exchange name |
| `RABBITMQ_QUEUE` | No | `contact_messages` | Queue name |
| `RABBITMQ_RETRY_DELAYS` | No | `1m,5m,30m,2h,12h` | Comma-separated durations |
| `RABBITMQ_RETRY_JITTER` | No | `0` | Retry delay jitter fraction (0-1) |
| `RABBITMQ_HEARTBEAT` | No | `10s` | AMQP heartbeat interval |
| `RABBITMQ_PUBLISHER_CONFIRMS` | No | `false` | Enable publisher confirms |
| `RABBITMQ_RECONNECT` | No | `true` | Automatic reconnection |
| `RABBITMQ_RECONNECT_MAX_ATTEMPTS` | No | `0` | Reconnect attempt limit (0 = unlimited) |
| `RABBITMQ_RECONNECT_INITIAL_DELAY` | No | `1s` | First reconnect backoff delay |
| `RABBITMQ_RECONNECT_MAX_DELAY` | No | `30s` | Reconnect backoff cap |
| `RABBITMQ_PREFETCH_COUNT` | No | `1` | Consumer QoS prefetch |
| `RABBITMQ_CONSUMER_TAG` | No | `""` | Consumer identifier |
| `RABBITMQ_CONSUMER_CONCURRENCY` | No | `1` | Parallel message handlers |

### Multiple Queues per Service

`config.NewRabbitMQConfigWithPrefix("AI_")` reads `AI_RABBITMQ_*` variables,
falling back to the un-prefixed names, so two queues with different retry
ladders can coexist in one service:

```text
RABBITMQ_HOST=...                  # shared
RABBITMQ_USER=...                  # shared
RABBITMQ_QUEUE=contact_messages    # queue 1
AI_RABBITMQ_QUEUE=ai_jobs          # queue 2
AI_RABBITMQ_RETRY_DELAYS=5s,15s,1m # queue 2 retry ladder
```

## Queue Infrastructure

Created automatically by `NewRabbitMQPublisher` (and re-declared on every
reconnect and consumer setup):

- **Main queue** (`contact_messages`) - Primary message queue
- **Retry queues** (`contact_messages_retry_0`, `_1`, etc.) - TTL-based delays
- **DLQ** (`contact_messages_dlq`) - Permanent failures
- **Exchanges** - Direct exchanges for routing

All queues and exchanges are durable and messages are published persistent.

**Changing `RETRY_DELAYS` against an existing topology**: RabbitMQ rejects
re-declaring a queue with different arguments (`PRECONDITION_FAILED`).
Delete the old retry queues (or use a new queue name) before changing the
delay ladder of a deployed queue.

## Testing

Integration tests in `integration_test.go` cover publish/consume, the retry
ladder, DLQ routing, permanent errors, reconnection, confirms, shutdown
semantics, and concurrency against a real RabbitMQ in Docker
(testcontainers-go). They are skipped with `-short` or when Docker is
unavailable.
