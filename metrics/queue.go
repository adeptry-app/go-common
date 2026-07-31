package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// QueueMetrics holds Prometheus metrics for message queue operations.
// It implements the queue package's MetricsRecorder interface; pass it to
// queue.WithPublisherMetrics / queue.WithConsumerMetrics.
type QueueMetrics struct {
	PublishesTotal  *prometheus.CounterVec
	PublishDuration *prometheus.HistogramVec
	ConsumesTotal   *prometheus.CounterVec
	ConsumeDuration *prometheus.HistogramVec
	ReconnectsTotal *prometheus.CounterVec
	QueueDepth      *prometheus.GaugeVec
}

// NewQueueMetrics creates a QueueMetrics instance with registered Prometheus
// metrics. Like New, it must be called at most once per process per service
// (duplicate registration panics).
func NewQueueMetrics(cfg Config) *QueueMetrics {
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "adeptry"
	}

	return &QueueMetrics{
		PublishesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "queue_publishes_total",
				Help:      "Total number of queue publish attempts",
			},
			[]string{"queue", "status"},
		),

		PublishDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "queue_publish_duration_seconds",
				Help:      "Queue publish latency in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"queue"},
		),

		ConsumesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "queue_consumes_total",
				Help:      "Total number of consumed messages by outcome (success, retry, dlq, requeued)",
			},
			[]string{"queue", "outcome"},
		),

		ConsumeDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "queue_consume_duration_seconds",
				Help:      "Message handler execution time in seconds",
				Buckets:   []float64{.01, .05, .1, .5, 1, 5, 15, 30, 60, 120, 300},
			},
			[]string{"queue"},
		),

		ReconnectsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "queue_reconnects_total",
				Help:      "Total number of RabbitMQ reconnect cycles",
			},
			[]string{"component"},
		),

		QueueDepth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "queue_depth",
				Help:      "Number of messages in a queue (e.g. DLQ depth)",
			},
			[]string{"queue"},
		),
	}
}

// RecordPublish records a publish attempt.
func (q *QueueMetrics) RecordPublish(queue string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "error"
	}
	q.PublishesTotal.WithLabelValues(queue, status).Inc()
	q.PublishDuration.WithLabelValues(queue).Observe(duration.Seconds())
}

// RecordConsume records a processed delivery.
func (q *QueueMetrics) RecordConsume(queue, outcome string, duration time.Duration) {
	q.ConsumesTotal.WithLabelValues(queue, outcome).Inc()
	q.ConsumeDuration.WithLabelValues(queue).Observe(duration.Seconds())
}

// RecordReconnect records the start of a reconnect cycle.
func (q *QueueMetrics) RecordReconnect(component string) {
	q.ReconnectsTotal.WithLabelValues(component).Inc()
}

// SetQueueDepth sets the current depth of a queue (e.g. polled DLQ depth).
func (q *QueueMetrics) SetQueueDepth(queue string, depth float64) {
	q.QueueDepth.WithLabelValues(queue).Set(depth)
}
