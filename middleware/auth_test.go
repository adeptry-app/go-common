package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/gin-gonic/gin"
)

func newClaimsTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func TestGetClaims_FullSet(t *testing.T) {
	c := newClaimsTestContext()
	c.Set(CtxKeyUserID, int64(42))
	c.Set(CtxKeyUsername, "kaladin")
	c.Set(CtxKeyDisplayName, "Kaladin Stormblessed")
	c.Set(CtxKeyScopes, map[string]string{"heroes": "edit"})

	claims, ok := GetClaims(c)
	if !ok {
		t.Fatal("GetClaims() ok = false, want true")
	}
	if claims.UserID != 42 {
		t.Errorf("UserID = %d, want 42", claims.UserID)
	}
	if claims.Username != "kaladin" {
		t.Errorf("Username = %q, want %q", claims.Username, "kaladin")
	}
	if claims.DisplayName != "Kaladin Stormblessed" {
		t.Errorf("DisplayName = %q, want %q", claims.DisplayName, "Kaladin Stormblessed")
	}
	if claims.Scopes["heroes"] != "edit" {
		t.Errorf("Scopes = %v, want heroes:edit", claims.Scopes)
	}
}

func TestGetClaims_UserIDIsTheSentinel(t *testing.T) {
	c := newClaimsTestContext()
	c.Set(CtxKeyUserID, int64(1))

	claims, ok := GetClaims(c)
	if !ok {
		t.Fatal("GetClaims() ok = false, want true")
	}
	if claims.Username != "" || claims.DisplayName != "" || claims.Scopes != nil {
		t.Errorf("claims = %+v, want zero fields besides UserID", claims)
	}
}

func TestGetClaims_NotAuthenticated(t *testing.T) {
	tests := []struct {
		name string
		set  func(*gin.Context)
	}{
		{"nothing set", func(_ *gin.Context) {}},
		{"wrong user_id type", func(c *gin.Context) {
			c.Set(CtxKeyUserID, "1")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClaimsTestContext()
			tt.set(c)
			if _, ok := GetClaims(c); ok {
				t.Error("GetClaims() ok = true, want false")
			}
		})
	}
}

func TestValidateToken_RejectsNonAccessTokens(t *testing.T) {
	const secret = "test-secret-key-at-least-32-bytes-long"
	jwtService, err := jwt.NewService(secret, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	access, err := jwtService.GenerateAccessToken(42, "kaladin", nil)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	refresh, err := jwtService.GenerateRefreshToken(42, "kaladin", nil)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"access token", access, http.StatusOK},
		{"refresh token", refresh, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			router := gin.New()
			router.Use(NewAuthMiddleware(jwtService).ValidateToken())
			router.GET("/heroes", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/heroes", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name       string
		cookie     string
		authHeader string
		want       string
	}{
		{"cookie preferred over header", "cookie_token", "Bearer header_token", "cookie_token"},
		{"falls back to header", "", "Bearer header_token", "header_token"},
		{"lowercase bearer rejected", "", "bearer header_token", ""},
		{"basic auth rejected", "", "Basic dXNlcjpwYXNz", ""},
		{"no scheme rejected", "", "just_a_token", ""},
		{"extra segment rejected", "", "Bearer token extra", ""},
		{"nothing provided", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClaimsTestContext()
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != "" {
				c.Request.AddCookie(&http.Cookie{Name: "access_token", Value: tt.cookie})
			}
			if tt.authHeader != "" {
				c.Request.Header.Set("Authorization", tt.authHeader)
			}

			if got := ExtractToken(c); got != tt.want {
				t.Errorf("ExtractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}
