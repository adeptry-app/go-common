# queue

SQS message publishing and consuming with a per-failure retry ladder, DLQ
quarantine, error classification, and stale row recovery.

## Publisher Usage

```go
import (
    "log"

    "github.com/adeptry-app/go-common/queue"
)

publisher, err := queue.NewSQSPublisher(ctx, cfg,
    queue.WithPublisherMetrics(queueMetrics),   // optional Prometheus metrics
)
if err != nil {
    log.Fatal(err)
}
defer publisher.Close()

// Publish a message (the correlation ID is taken from ctx, see Tracing)
err = publisher.Publish(ctx, message)

// The retry budget handlers measure the claimed row's attempt against
maxRetries := publisher.MaxRetries()
```

Both constructors check their queues with `GetQueueAttributes` and fail if one
is unreachable, so a typo'd URL or a missing IAM grant fails the deploy rather
than every later message. The consumer checks the DLQ too, since nothing else
touches it until the first quarantine. `ctx` bounds construction only, not the
lifetime of the publisher.

## Consumer Usage

```go
import (
    "context"
    "log"

    "github.com/adeptry-app/go-common/queue"
)

consumer, err := queue.NewSQSConsumer(ctx, cfg, logger,
    queue.WithConsumerMetrics(queueMetrics),    // optional Prometheus metrics
)
if err != nil {
    log.Fatal(err)
}
defer consumer.Close()

// Consume messages (blocks until context cancelled or Close is called)
err = consumer.Consume(ctx, func(c context.Context, d queue.Delivery) error {
    // Return nil to delete the message.
    // Return queue.WithAttempt(row.Attempt, err) to ride the retry ladder.
    // Return queue.Permanent(err) to quarantine it in the DLQ.
    return nil
})
```

`Delivery` is transport-neutral: AWS SDK types never leave this package.

| Field | Meaning |
| ----- | ------- |
| `Body` | Raw message payload |
| `MessageID` | Queue message id, carried onto the DLQ copy |
| `CorrelationID` | Links back to the originating request |
| `ReceiveCount` | Every receive, including ones a deploy cut short |
| `ReceivedAt` | When this delivery arrived |

## Stale Row Recovery

At-least-once delivery still loses work when the publish itself fails after
the row is committed, or when a worker dies mid-job. `StaleSweeper` closes
that gap: it periodically asks the database which rows are stranded and
re-publishes them, making the row (not the message) the source of truth.

```go
sweeper := queue.StaleSweeper{
    Sweep: func(ctx context.Context) ([]int64, error) {
        return repo.SweepStaleEmails(ctx, cfg.Sweeper)
    },
    Event:     func(id int64) any { return models.EmailEvent{EmailID: id} },
    Publisher: publisher,
    Kind:      "email",
    Interval:  cfg.Sweeper.Interval,
    Logger:    appLogger,
}
go sweeper.Run(ctx) // blocks until ctx is cancelled
```

`Sweep` is expected to fail rows past the attempt budget itself and return
only the ids worth re-publishing, so the attempt ceiling lives in one place
alongside the claim. Pair it with an atomic claim in the handler: the sweeper
guarantees at-least-once recovery, the claim keeps duplicate deliveries from
doing the work twice. Tune with `config.SweeperConfig`.

Note the sweeper charges an attempt when it re-publishes, and the claim
charges another, so a recovered row rides a correspondingly shorter ladder.
A queue outage also costs an attempt: a failed publish is only logged, and the
row waits a whole processing-age window for the next pass.

## Features

- Stale row recovery via `StaleSweeper`
- Retry ladder applied as a per-failure visibility timeout, with optional jitter
- DLQ quarantine for permanent failures and spent attempt budgets
- Permanent error classification (`queue.Permanent` / `queue.ErrPermanent`)
- Ladder indexing by business attempt (`queue.WithAttempt`)
- Optional concurrent message processing
- Correlation ID propagation between HTTP requests and queue messages
- Optional Prometheus metrics (see the `metrics` package)
- Thread-safe publishing and consuming

## Delivery Semantics

Delivery is **at-least-once**. Handlers must be idempotent. Duplicates can
occur when:

- A handler succeeds but the delete fails (SQS redelivers).
- A worker dies between receiving and settling a message.
- A deploy or Spot interruption cuts a handler short.

The claim in the handler is what makes that safe: a second delivery of a row
already `processing` finds it unclaimable and deletes it. `MessageID` supports
deduplication where more is needed.

## Retry Flow

1. A handler that fails transiently returns `queue.WithAttempt(attempt, err)`,
   where `attempt` is the business attempt its own claim charged in Postgres.
2. The consumer calls `ChangeMessageVisibility` with `RetryDelays[attempt-1]`,
   so the message returns after that ladder step. No retry queues exist.
3. When the row's attempt budget is spent, the handler fails the row and
   returns `queue.Permanent(err)`; the consumer copies the body to the DLQ and
   deletes the source. Exhaustion is a business decision with a database record
   behind it, never a side effect of receive counting.
4. Errors matching `queue.ErrPermanent` skip the ladder entirely and are
   quarantined the same way.
5. If the consume context is cancelled while a message is being handled
   (shutdown), the message is made visible again immediately. That costs a
   receive, not a ladder step - though it may well have cost a business
   attempt, which the sweeper is what recovers.

### Why the ladder is not indexed by `ReceiveCount`

SQS counts every receive, including one a deploy cut short before the handler
did anything, and the count is not resettable. Indexing the ladder by it would
let two rolling deploys push a message straight to the last rung. The business
attempt lives in Postgres, is charged by the claim, and is the only thing the
ladder is indexed by.

`ReceiveCount` has exactly one narrow use: a failure that happened *before* the
row was claimed has no business attempt to index by, and still has to back off
rather than spin. Such a failure can never spend a retry from the budget,
because the budget is counted in Postgres and a pre-claim failure never
reached it.

The queue's own `maxReceiveCount` redrive policy stays as a backstop that no
correct path reaches: hitting it means the consumer never settled the message
at all (a crash loop, an OOM kill, a task that died between receive and
settle), and the row behind it is still `retrying` for the sweeper to recover.

### Panics

A handler panic does not crash the worker: the consumer recovers it, logs
the stack at error level, and treats it as a transient handler error that
rides the retry ladder. A deterministically panicking message therefore
reaches the DLQ once the row's budget is spent instead of crash-looping the
process. Mark input-dependent failures with `queue.Permanent` in the handler
when retrying is pointless.

### Retry Jitter

With `RetryJitter` set (0 to 1), each ladder step is randomly shortened by up
to that fraction (e.g. jitter 0.2 on a 5m delay yields delays between 4m and
5m). This spreads out retries of messages that failed together. Jitter only
ever shortens, so it cannot push a delay past the ceiling below.

## Visibility Timeouts

SQS caps a visibility timeout at 12h **measured from the `ReceiveMessage`
call**, not from the `ChangeMessageVisibility` call, and rejects an
over-budget request with HTTP 400 rather than clamping it. Two defences:

- `config.NewSQSConfig` rejects any ladder step above 11h, leaving an hour of
  headroom for handler runtime.
- The consumer clamps every visibility change to what is left of the ceiling.

A visibility change does not persist: SQS reverts to the queue's configured
timeout on the next receive. That is correct here, since each ladder step is a
fresh receive that sets its own delay, but it means the queue-level
`VisibilityTimeout` must still exceed the handler timeout on its own. The
consumer passes `VisibilityTimeout` on every receive, so a queue created out of
band cannot silently fall back to the SQS default of 30s and redeliver a job
that is still running.

## Concurrency

`Consume` processes messages sequentially by default. Set
`ConsumerConcurrency` to process N messages in parallel. A receive never asks
for more messages than there are free handler slots, and `MaxNumberOfMessages`
caps the batch size (SQS allows at most 10).

## Graceful Shutdown

- Cancelling the consume context stops receiving; in-flight handlers run to
  completion (handlers should honor ctx for long work) and their messages are
  made visible again immediately if they fail.
- Settling runs on a detached context, so a message whose handler succeeded
  during shutdown is still deleted rather than redelivered.
- `Close()` on the consumer blocks until in-flight handlers finish. Never call
  it from inside a handler (it would deadlock waiting for that handler); cancel
  the consume context instead.
- Shut down the consumer before the publisher.

## Tracing

SQS has no correlation-id field, so `Publish` carries the context correlation
ID (`logger.GetCorrelationID`) as a `correlationId` message attribute when
present, and the consumer puts it back into the handler context, so handlers
using `logger.FromContext` log it automatically. Retries preserve it, and the
DLQ copy carries it too.

## Consumer Liveness

A consumer that is receiving nothing has no connection to lose, so nothing
reveals it except the consumption state:

```go
healthAgg.Register(health.NewConsumerChecker(consumer.ConsumptionError))
```

`ConsumptionError` is nil while deliveries flow and after a clean `Close()` or
context cancellation. Failed receives are retried with backoff indefinitely, so
`Consume` never returns for them - the retry state is the only signal.

## Configuration

```go
cfg := config.SQSConfig{
    QueueURL:    "https://sqs.eu-west-1.amazonaws.com/123456789012/emails",
    DLQURL:      "https://sqs.eu-west-1.amazonaws.com/123456789012/emails_dlq",
    Region:      "eu-west-1",
    Endpoint:    "",               // LocalStack only, development only
    RetryDelays: []time.Duration{1*time.Minute, 5*time.Minute, 30*time.Minute},
    RetryJitter: 0.2,              // optional, 0 disables

    MaxNumberOfMessages: 1,             // receive batch size, 1-10
    WaitTimeSeconds:     20,            // long poll, 0-20
    VisibilityTimeout:   90*time.Second, // must exceed the handler timeout
    ConsumerConcurrency: 1,             // parallel handlers
}
```

Credentials come from the default AWS chain, so an ECS task role needs no
configuration of its own.

## Environment Variables

| Variable | Required | Default | Description |
| -------- | -------- | ------- | ----------- |
| `SQS_QUEUE_URL` | Yes | - | Main queue URL |
| `SQS_DLQ_URL` | Yes | - | Dead letter queue URL |
| `SQS_REGION` | Yes | - | AWS region |
| `SQS_ENDPOINT` | No | `""` | LocalStack endpoint; rejected unless `ENVIRONMENT=development` |
| `SQS_RETRY_DELAYS` | No | `1m,5m,30m,2h,11h` | Comma-separated durations, each at most 11h |
| `SQS_RETRY_JITTER` | No | `0` | Retry delay jitter fraction (0-1) |
| `SQS_MAX_MESSAGES` | No | `1` | Receive batch size (1-10) |
| `SQS_WAIT_TIME_SECONDS` | No | `20` | Long poll wait (0-20) |
| `SQS_VISIBILITY_TIMEOUT` | No | `30s` | Receive visibility timeout (max 12h) |
| `SQS_CONSUMER_CONCURRENCY` | No | `1` | Parallel message handlers |

### Multiple Queues per Service

`config.NewSQSConfigWithPrefix("AI_")` reads `AI_SQS_*` variables, falling back
to the un-prefixed names, so two queues with different retry ladders can
coexist in one service:

```text
SQS_REGION=...                     # shared
SQS_ENDPOINT=...                   # shared
SQS_QUEUE_URL=.../emails           # queue 1
AI_SQS_QUEUE_URL=.../ai_requests   # queue 2
AI_SQS_RETRY_DELAYS=30s,2m,10m     # queue 2 retry ladder
```

## Queue Infrastructure

Queues are created by Terraform in deployed environments and by a LocalStack
init script locally. Neither the publisher nor the consumer declares anything
at startup, but both read their queues' attributes once, so the task role needs
`sqs:GetQueueAttributes` alongside send/receive/delete. Each queue pair is:

- **Main queue** (`emails`) - with a redrive policy to the DLQ as a backstop
- **DLQ** (`emails_dlq`) - permanent failures and quarantined bodies

The consumer quarantines explicitly (send to the DLQ, then delete from the
source), so a message reaching the DLQ through the redrive policy instead means
the consumer never settled it at all.

## Testing

Integration tests in `integration_test.go` cover publish/consume, DLQ
quarantine, ladder indexing, shutdown semantics and concurrency against real
SQS in LocalStack (testcontainers-go). They are skipped with `-short`, and when
Docker is unavailable outside CI - in CI a missing Docker daemon fails.
