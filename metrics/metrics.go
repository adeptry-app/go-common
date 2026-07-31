package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// unmatchedPath is the label used for requests that match no route
const unmatchedPath = "unmatched"

// Metrics holds all Prometheus metrics
type Metrics struct {
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight prometheus.Gauge
}

// Config holds metrics configuration
type Config struct {
	ServiceName string
	Namespace   string // e.g., "adeptry"
}

// New creates a new Metrics instance with registered Prometheus metrics
func New(cfg Config) *Metrics {
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "adeptry"
	}

	return &Metrics{
		// HTTP request metrics
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),

		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request latency in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			// No status label: RequestsTotal already carries it, and duplicating it
			// here would make the histogram's _count a copy of that counter.
			[]string{"method", "path"},
		),

		RequestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "http_requests_in_flight",
				Help:      "Current number of HTTP requests being processed",
			},
		),
	}
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, path string, status int, duration time.Duration) {
	m.RequestsTotal.WithLabelValues(method, path, strconv.Itoa(status)).Inc()
	m.RequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// Middleware returns a Gin middleware that records HTTP metrics
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Increment in-flight requests
		m.RequestsInFlight.Inc()
		defer m.RequestsInFlight.Dec()

		// Start timer
		start := time.Now()

		// Process request
		c.Next()

		// Record metrics
		duration := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		path := c.FullPath() // Use route pattern, not actual path with IDs

		// Unmatched routes have no pattern; a raw URL label would be unbounded cardinality
		if path == "" {
			path = unmatchedPath
		}

		m.RecordHTTPRequest(method, path, status, duration)
	}
}
