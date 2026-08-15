package queue

import "time"

// Consume outcomes reported to MetricsRecorder.RecordConsume.
const (
	OutcomeSuccess  = "success"  // handler succeeded, message deleted
	OutcomeRetry    = "retry"    // handler failed, message hidden for a ladder step
	OutcomeDLQ      = "dlq"      // message quarantined in the DLQ (permanent error)
	OutcomeRequeued = "requeued" // message made visible again without a ladder step (shutdown)
)

// MetricsRecorder receives queue events for instrumentation. Implementations
// must be safe for concurrent use and must not panic - recorder calls run on
// the publish and consume paths and, unlike message handlers, are not
// recovered. The metrics package provides a Prometheus implementation
// (metrics.QueueMetrics); pass it via WithPublisherMetrics /
// WithConsumerMetrics. When no recorder is configured, events are discarded.
type MetricsRecorder interface {
	// RecordPublish is called after every publish attempt. queue is the name
	// parsed from the queue URL.
	RecordPublish(queue string, success bool, duration time.Duration)
	// RecordConsume is called after a delivery is processed. outcome is one
	// of the Outcome* constants; duration is the handler execution time.
	RecordConsume(queue string, outcome string, duration time.Duration)
}

// noopMetrics is the default MetricsRecorder that discards all events.
type noopMetrics struct{}

func (noopMetrics) RecordPublish(string, bool, time.Duration)   {}
func (noopMetrics) RecordConsume(string, string, time.Duration) {}
