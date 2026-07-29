package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newSecurityRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(NewSecurityMiddleware(
		[]string{"https://app.example.com"},
		"GET,POST,OPTIONS",
		"Content-Type,Authorization",
		true,
	).Apply())
	handler := func(c *gin.Context) { c.String(http.StatusOK, "handled") }
	router.GET("/resource", handler)
	router.POST("/resource", handler)
	router.OPTIONS("/resource", handler)
	return router
}

func TestSecurityMiddleware_Apply(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		origin      string
		wantStatus  int
		wantHandled bool
	}{
		{"allowed origin passes", http.MethodPost, "https://app.example.com", http.StatusOK, true},
		{"disallowed origin rejected before handler", http.MethodPost, "https://evil.example.com", http.StatusForbidden, false},
		{"disallowed origin rejected on GET", http.MethodGet, "https://evil.example.com", http.StatusForbidden, false},
		{"missing origin passes for non-browser clients", http.MethodPost, "", http.StatusOK, true},
		{"allowed preflight", http.MethodOptions, "https://app.example.com", http.StatusNoContent, false},
		{"disallowed preflight", http.MethodOptions, "https://evil.example.com", http.StatusForbidden, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/resource", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			newSecurityRouter().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if handled := rec.Body.String() == "handled"; handled != tt.wantHandled {
				t.Errorf("handler ran = %v, want %v", handled, tt.wantHandled)
			}
		})
	}
}

func TestSecurityMiddleware_Headers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	newSecurityRouter().ServeHTTP(rec, req)

	want := map[string]string{
		"Access-Control-Allow-Origin":      "https://app.example.com",
		"Access-Control-Allow-Credentials": "true",
		"X-Content-Type-Options":           "nosniff",
		"X-Frame-Options":                  "DENY",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

func TestSecurityMiddleware_NoCORSHeadersWithoutOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rec := httptest.NewRecorder()
	newSecurityRouter().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestNewSecurityMiddleware_Panics(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
	}{
		{"empty list", []string{}},
		{"empty string origin", []string{"https://app.example.com", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic, got none")
				}
			}()
			NewSecurityMiddleware(tt.origins, "GET", "Content-Type", false)
		})
	}
}
