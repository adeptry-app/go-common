package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/adeptry-app/go-common/jwt"
)

func newAuthContextRequest(t *testing.T, userAgent string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if userAgent != "" {
		c.Request.Header.Set("User-Agent", userAgent)
	}
	return c
}

func TestAuthContextFrom(t *testing.T) {
	c := newAuthContextRequest(t, "curl/8.0")
	SetIdentity(c, jwt.Identity{UserID: 42, Username: "kaladin"})

	auth, err := AuthContextFrom(c)
	if err != nil {
		t.Fatalf("AuthContextFrom() error = %v", err)
	}
	if auth.UserID != 42 {
		t.Errorf("UserID = %d, want 42", auth.UserID)
	}
	if auth.Username != "kaladin" {
		t.Errorf("Username = %q, want kaladin", auth.Username)
	}
	if auth.UserAgent != "curl/8.0" {
		t.Errorf("UserAgent = %q, want curl/8.0", auth.UserAgent)
	}
}

func TestAuthContextFrom_MissingIdentity(t *testing.T) {
	tests := []struct {
		name string
		set  func(*gin.Context)
	}{
		{"no identity", func(_ *gin.Context) {}},
		{"blank username", func(c *gin.Context) {
			SetIdentity(c, jwt.Identity{UserID: 42, Username: ""})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newAuthContextRequest(t, "")
			tt.set(c)

			if _, err := AuthContextFrom(c); !errors.Is(err, ErrMissingAuthContext) {
				t.Errorf("error = %v, want %v", err, ErrMissingAuthContext)
			}
		})
	}
}

// The cache is what lets a rate limiter and a handler share one extraction.
func TestAuthContextFrom_CachesPerRequest(t *testing.T) {
	c := newAuthContextRequest(t, "curl/8.0")
	SetIdentity(c, jwt.Identity{UserID: 42, Username: "kaladin"})

	first, err := AuthContextFrom(c)
	if err != nil {
		t.Fatalf("AuthContextFrom() error = %v", err)
	}

	// A changed identity must not surface: the first extraction is the record
	// every audited write in this request is attributed to.
	SetIdentity(c, jwt.Identity{UserID: 99, Username: "szeth"})
	second, err := AuthContextFrom(c)
	if err != nil {
		t.Fatalf("AuthContextFrom() error = %v", err)
	}

	if second != first {
		t.Errorf("second call returned %+v, want the cached %+v", second, first)
	}
}

func TestTruncateUserAgent(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want int // expected byte length
	}{
		{"short is untouched", "curl/8.0", len("curl/8.0")},
		{"empty", "", 0},
		{"exactly at the cap", strings.Repeat("a", userAgentMaxLen), userAgentMaxLen},
		{"one over the cap", strings.Repeat("a", userAgentMaxLen+1), userAgentMaxLen},
		{"far over the cap", strings.Repeat("a", userAgentMaxLen*3), userAgentMaxLen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateUserAgent(tt.ua)
			if len(got) != tt.want {
				t.Errorf("len = %d, want %d", len(got), tt.want)
			}
			if !strings.HasPrefix(tt.ua, got) {
				t.Error("result must be a prefix of the input")
			}
		})
	}
}

// Cutting at a fixed byte offset would split a multi-byte rune and store
// invalid UTF-8 in the audit log.
func TestTruncateUserAgent_NeverSplitsARune(t *testing.T) {
	// Multi-byte runes straddling the cap at every offset within one rune.
	for _, filler := range []string{"", "a", "aa", "aaa"} {
		ua := filler + strings.Repeat("日", userAgentMaxLen)

		got := TruncateUserAgent(ua)

		if !utf8.ValidString(got) {
			t.Errorf("filler %q produced invalid UTF-8", filler)
		}
		if len(got) > userAgentMaxLen {
			t.Errorf("filler %q: len = %d, want at most %d", filler, len(got), userAgentMaxLen)
		}
		if len(got) < userAgentMaxLen-3 {
			t.Errorf("filler %q: len = %d, trimmed more than one rune", filler, len(got))
		}
	}
}
