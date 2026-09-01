package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Share and invite codes ride in the path, so the raw target must never be logged.
func TestRequestLoggerRecordsTheRouteNotTheTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		target string
		want   string
	}{
		{"/shares/canary-code", "/shares/:code"},
		{"/wp-admin.php", "unmatched"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			var buf bytes.Buffer
			router := gin.New()
			router.Use(RequestLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
			router.GET("/shares/:code", func(c *gin.Context) { c.Status(http.StatusOK) })

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.target, nil))

			if strings.Contains(buf.String(), "canary-code") {
				t.Errorf("the request target reached the log: %s", buf.String())
			}

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("log line is not JSON: %v", err)
			}
			if entry["route"] != tt.want {
				t.Errorf("route = %v, want %v", entry["route"], tt.want)
			}
		})
	}
}
