package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// bodyLimitRouter echoes the body it manages to read, or reports 413.
func bodyLimitRouter(maxSize int64) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(BodyLimit(maxSize))
	router.POST("/test", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.String(http.StatusOK, string(body))
	})
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

func TestBodyLimit(t *testing.T) {
	tests := []struct {
		name       string
		maxSize    int64
		payload    string
		wantStatus int
	}{
		{"under the limit", 1024, `{"small":"payload"}`, http.StatusOK},
		{"exactly at the limit", 10, strings.Repeat("x", 10), http.StatusOK},
		{"over the limit", 16, strings.Repeat("x", 100), http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			bodyLimitRouter(tt.maxSize).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK && w.Body.String() != tt.payload {
				t.Errorf("body = %q, want %q", w.Body.String(), tt.payload)
			}
		})
	}
}

func TestBodyLimit_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	bodyLimitRouter(1024).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
