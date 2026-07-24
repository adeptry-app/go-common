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
	RequestsTotal        *prometheus.CounterVec
	RequestDuration      *prometheus.HistogramVec
	RequestsInFlight     prometheus.Gauge
	DBQueriesTotal       *prometheus.CounterVec
	DBQueryDuration      *prometheus.HistogramVec
	ExternalCallsTotal   *prometheus.CounterVec
	ExternalCallDuration *prometheus.HistogramVec
}

// Config holds metrics configuration
type Config struct {
	ServiceName string
	Namespace   string // e.g., "portfolio"
}

// New creates a new Metrics instance with registered Prometheus metrics
func New(cfg Config) *Metrics {
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "portfolio"
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
			[]string{"method", "path", "status"},
		),

		RequestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "http_requests_in_flight",
				Help:      "Current number of HTTP requests being processed",
			},
		),

		// Database metrics
		DBQueriesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "db_queries_total",
				Help:      "Total number of database queries",
			},
			[]string{"operation", "table", "status"},
		),

		DBQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "db_query_duration_seconds",
				Help:      "Database query latency in seconds",
				Buckets:   []float64{.0001, .0005, .001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
			[]string{"operation", "table"},
		),

		// External API call metrics
		ExternalCallsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "external_calls_total",
				Help:      "Total number of external API calls",
			},
			[]string{"service", "endpoint", "status"},
		),

		ExternalCallDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: cfg.ServiceName,
				Name:      "external_call_duration_seconds",
				Help:      "External API call latency in seconds",
				Buckets:   []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"service", "endpoint"},
		),
	}
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, path string, status int, duration time.Duration) {
	statusStr := strconv.Itoa(status)
	m.RequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.RequestDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
}

// RecordDBQuery records database query metrics
func (m *Metrics) RecordDBQuery(operation, table, status string, duration time.Duration) {
	m.DBQueriesTotal.WithLabelValues(operation, table, status).Inc()
	m.DBQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}

// RecordExternalCall records external API call metrics
func (m *Metrics) RecordExternalCall(service, endpoint, status string, duration time.Duration) {
	m.ExternalCallsTotal.WithLabelValues(service, endpoint, status).Inc()
	m.ExternalCallDuration.WithLabelValues(service, endpoint).Observe(duration.Seconds())
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
