package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/metrics"
)

// One instance for the package: metrics.New registers with the global
// Prometheus registry, which panics on a duplicate.
var testMetrics = metrics.New(metrics.Config{ServiceName: "router", Namespace: "test"})

func newTestRouter(trusted []string) (*gin.Engine, error) {
	gin.SetMode(gin.TestMode)
	cfg := config.ServiceConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: "GET,POST,OPTIONS",
		TrustedProxies: trusted,
	}
	return NewRouter(cfg, slog.New(slog.DiscardHandler), testMetrics)
}

// gin trusts every peer until SetTrustedProxies is called, which makes
// c.ClientIP() whatever the caller puts in X-Forwarded-For.
func TestNewRouter_IgnoresForwardedForFromAnUntrustedPeer(t *testing.T) {
	router, err := newTestRouter([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	router.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// httptest dials from 192.0.2.1, which 10.0.0.0/8 does not cover.
	if got := rec.Body.String(); got != "192.0.2.1" {
		t.Errorf("ClientIP() = %q, want the peer address", got)
	}
}

func TestNewRouter_RejectsAMalformedCIDR(t *testing.T) {
	if _, err := newTestRouter([]string{"not-a-cidr"}); err == nil {
		t.Error("NewRouter() error = nil, want a set trusted proxies failure")
	}
}

func TestNewRouter_AppliesCORS(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		wantStatus int
		wantOrigin string
	}{
		{"listed origin", "https://example.com", http.StatusOK, "https://example.com"},
		{"unlisted origin", "https://evil.test", http.StatusForbidden, ""},
		{"no origin", "", http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, err := newTestRouter([]string{"10.0.0.0/8"})
			if err != nil {
				t.Fatalf("NewRouter() error = %v", err)
			}
			router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantOrigin {
				t.Errorf("Allow-Origin = %q, want %q", got, tt.wantOrigin)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}
