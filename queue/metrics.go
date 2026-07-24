package queue

import "time"

// Consume outcomes reported to MetricsRecorder.RecordConsume.
const (
	OutcomeSuccess  = "success"  // handler succeeded, message ACKed
	OutcomeRetry    = "retry"    // handler failed, message sent to a retry queue
	OutcomeDLQ      = "dlq"      // message sent to the DLQ (retries exhausted or permanent error)
	OutcomeRequeued = "requeued" // message requeued without consuming a retry attempt (shutdown)
)

// Components reported to MetricsRecorder.RecordReconnect.
const (
	ComponentPublisher = "publisher"
	ComponentConsumer  = "consumer"
)

// MetricsRecorder receives queue events for instrumentation. Implementations
// must be safe for concurrent use and must not panic - recorder calls run on
// the publish and consume paths and, unlike message handlers, are not
// recovered. The metrics package provides a Prometheus implementation
// (metrics.QueueMetrics); pass it via WithPublisherMetrics /
// WithConsumerMetrics. When no recorder is configured, events are discarded.
type MetricsRecorder interface {
	// RecordPublish is called after every publish attempt. queue is the
	// routing target (main queue, retry queue, or DLQ name).
	RecordPublish(queue string, success bool, duration time.Duration)
	// RecordConsume is called after a delivery is processed. outcome is one
	// of the Outcome* constants; duration is the handler execution time.
	RecordConsume(queue string, outcome string, duration time.Duration)
	// RecordReconnect is called when a dropped connection is detected and a
	// reconnect cycle starts. component is "publisher" or "consumer".
	RecordReconnect(component string)
}

// noopMetrics is the default MetricsRecorder that discards all events.
type noopMetrics struct{}

func (noopMetrics) RecordPublish(string, bool, time.Duration)   {}
func (noopMetrics) RecordConsume(string, string, time.Duration) {}
func (noopMetrics) RecordReconnect(string)                      {}
