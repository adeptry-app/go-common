package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/adeptry-app/go-common/middleware"
)

func newTestClient(t *testing.T) (*goredis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return goredis.NewClient(&goredis.Options{Addr: mr.Addr()}), mr
}

// limitRouter runs limiter over a handler whose status the caller chooses, so a
// refunding limiter can be driven down both branches.
func limitRouter(limiter gin.HandlerFunc, status int, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			middleware.SetIdentity(c, jwt.Identity{UserID: 42, Username: "kaladin"})
		})
	}
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) { c.Status(status) })
	return router
}

func get(router *gin.Engine) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
	return rec
}

func TestByIP_RejectsOverTheCap(t *testing.T) {
	client, _ := newTestClient(t)
	router := limitRouter(ByIP(client, "t:", 2, time.Minute), http.StatusOK, false)

	for i := 1; i <= 2; i++ {
		if rec := get(router); rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	rec := get(router)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("over the cap = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want %q", got, "60")
	}
}

// A limiter is a mitigation, not an access control: a dead Redis must not lock
// every caller out.
func TestByIP_FailsOpenWhenRedisIsDown(t *testing.T) {
	client, mr := newTestClient(t)
	mr.Close()

	if rec := get(limitRouter(ByIP(client, "t:", 1, time.Minute), http.StatusOK, false)); rec.Code != http.StatusOK {
		t.Errorf("with Redis down = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestByIPAttempt_RefundsOnlySuccesses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantTTL bool
	}{
		{"success is refunded", http.StatusOK, false},
		{"failure stands", http.StatusUnauthorized, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, mr := newTestClient(t)
			router := limitRouter(ByIPAttempt(client, "t:", 5, time.Minute), tt.status, false)

			if rec := get(router); rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}

			got, err := mr.Get("t:192.0.2.1")
			if err != nil {
				t.Fatalf("counter missing: %v", err)
			}
			want := "0"
			if tt.wantTTL {
				want = "1"
			}
			if got != want {
				t.Errorf("counter = %q, want %q", got, want)
			}
		})
	}
}

func TestByUser_RejectsUnauthenticated(t *testing.T) {
	client, _ := newTestClient(t)

	rec := get(limitRouter(ByUser(client, "t:", 5, time.Minute), http.StatusOK, false))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// Retry-After comes from the bucket's own TTL, so it must shrink as the window
// drains rather than reporting a full window every time.
func TestByUser_ReportsTheRemainingWindow(t *testing.T) {
	client, mr := newTestClient(t)
	router := limitRouter(ByUser(client, "t:", 1, time.Minute), http.StatusOK, true)

	if rec := get(router); rec.Code != http.StatusOK {
		t.Fatalf("first request = %d, want %d", rec.Code, http.StatusOK)
	}
	mr.FastForward(45 * time.Second)

	rec := get(router)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got != "15" {
		t.Errorf("Retry-After = %q, want %q", got, "15")
	}
}
