package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/adeptry-app/go-common/queue"
)

// Compile-time check: QueueMetrics satisfies the queue package recorder
// interface.
var _ queue.MetricsRecorder = (*QueueMetrics)(nil)

// NewQueueMetrics registers in the default Prometheus registry, so it can
// only be called once per process; all behavior is exercised in this single
// test.
func TestQueueMetrics(t *testing.T) {
	m := NewQueueMetrics(Config{ServiceName: "queuetest", Namespace: "testns"})

	m.RecordPublish("jobs", true, 10*time.Millisecond)
	m.RecordPublish("jobs", false, 20*time.Millisecond)
	m.RecordConsume("jobs", queue.OutcomeSuccess, time.Second)
	m.RecordConsume("jobs", queue.OutcomeDLQ, time.Second)
	m.SetQueueDepth("jobs_dlq", 7)

	if got := testutil.ToFloat64(m.PublishesTotal.WithLabelValues("jobs", "success")); got != 1 {
		t.Errorf("publishes success = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.PublishesTotal.WithLabelValues("jobs", "error")); got != 1 {
		t.Errorf("publishes error = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.ConsumesTotal.WithLabelValues("jobs", queue.OutcomeSuccess)); got != 1 {
		t.Errorf("consumes success = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.ConsumesTotal.WithLabelValues("jobs", queue.OutcomeDLQ)); got != 1 {
		t.Errorf("consumes dlq = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.QueueDepth.WithLabelValues("jobs_dlq")); got != 7 {
		t.Errorf("queue depth = %v, want 7", got)
	}
}
