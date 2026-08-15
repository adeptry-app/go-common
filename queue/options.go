package queue

// PublisherOption configures an SQSPublisher.
type PublisherOption func(*SQSPublisher)

// WithPublisherMetrics sets the metrics recorder for publish events.
// Defaults to a no-op recorder.
func WithPublisherMetrics(m MetricsRecorder) PublisherOption {
	return func(p *SQSPublisher) {
		if m != nil {
			p.metrics = m
		}
	}
}

// ConsumerOption configures an SQSConsumer.
type ConsumerOption func(*SQSConsumer)

// WithConsumerMetrics sets the metrics recorder for consume events.
// Defaults to a no-op recorder.
func WithConsumerMetrics(m MetricsRecorder) ConsumerOption {
	return func(c *SQSConsumer) {
		if m != nil {
			c.metrics = m
		}
	}
}
