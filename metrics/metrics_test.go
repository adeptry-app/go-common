package metrics

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// pathLabels returns every "path" label value recorded for a metric family.
func pathLabels(t *testing.T, name string) []string {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		paths := make([]string, 0, len(family.GetMetric()))
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "path" {
					paths = append(paths, label.GetValue())
				}
			}
		}
		return paths
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}

// New registers in the default Prometheus registry, so it can only be called
// once per process; all middleware labeling behavior is exercised here.
func TestMiddlewarePathLabels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := New(Config{ServiceName: "middlewaretest", Namespace: "testns"})

	router := gin.New()
	router.Use(m.Middleware())
	router.GET("/heroes/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, target := range []string{"/heroes/2314", "/wp-admin.php"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	}

	families := []string{
		"testns_middlewaretest_http_requests_total",
		"testns_middlewaretest_http_request_duration_seconds",
	}
	for _, family := range families {
		paths := pathLabels(t, family)
		if !slices.Contains(paths, unmatchedPath) {
			t.Errorf("%s: paths = %v, want %q for unmatched route", family, paths, unmatchedPath)
		}
		if slices.Contains(paths, "/wp-admin.php") {
			t.Errorf("%s: recorded raw URL path, paths = %v", family, paths)
		}
		if !slices.Contains(paths, "/heroes/:id") {
			t.Errorf("%s: paths = %v, want route pattern for matched route", family, paths)
		}
	}
}
