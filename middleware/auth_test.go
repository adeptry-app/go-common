package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/adeptry-app/go-common/jwt/jwttest"
	"github.com/gin-gonic/gin"
)

func newIdentityTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func TestGetIdentity_FullSet(t *testing.T) {
	c := newIdentityTestContext()
	SetIdentity(c, jwt.Identity{
		UserID:      42,
		Username:    "kaladin",
		Email:       "kaladin@bridgefour.com",
		DisplayName: "Kaladin Stormblessed",
		Scopes:      map[string]string{"heroes": "edit"},
	})

	id, ok := GetIdentity(c)
	if !ok {
		t.Fatal("GetIdentity() ok = false, want true")
	}
	if id.UserID != 42 {
		t.Errorf("UserID = %d, want 42", id.UserID)
	}
	if id.Username != "kaladin" {
		t.Errorf("Username = %q, want %q", id.Username, "kaladin")
	}
	if id.Email != "kaladin@bridgefour.com" {
		t.Errorf("Email = %q, want %q", id.Email, "kaladin@bridgefour.com")
	}
	if id.DisplayName != "Kaladin Stormblessed" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Kaladin Stormblessed")
	}
	if id.Scopes["heroes"] != "edit" {
		t.Errorf("Scopes = %v, want heroes:edit", id.Scopes)
	}
}

func TestGetIdentity_NotAuthenticated(t *testing.T) {
	tests := []struct {
		name string
		set  func(*gin.Context)
	}{
		{"nothing set", func(_ *gin.Context) {}},
		{"wrong type", func(c *gin.Context) {
			c.Set(ctxKeyIdentity, "kaladin")
		}},
		{"unusable user id", func(c *gin.Context) {
			SetIdentity(c, jwt.Identity{Username: "kaladin"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newIdentityTestContext()
			tt.set(c)
			if _, ok := GetIdentity(c); ok {
				t.Error("GetIdentity() ok = true, want false")
			}
		})
	}
}

func TestValidateToken_RejectsNonAccessTokens(t *testing.T) {
	private, public := jwttest.KeyPair(t, "test1")
	issuer, err := jwt.NewIssuer(jwt.IssuerConfig{
		PrivateKey:     private,
		PublicKeys:     public,
		AccessAudience: []string{jwt.AudiencePublicAPI},
		AccessExpiry:   15 * time.Minute,
		RefreshExpiry:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	verifier, err := jwt.NewVerifier(public, jwt.AudiencePublicAPI)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	access, err := issuer.GenerateAccessToken(jwt.Identity{UserID: 42, Username: "kaladin", Scopes: nil})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	refresh, err := issuer.GenerateRefreshToken(jwt.Identity{UserID: 42, Username: "kaladin", Scopes: nil})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}
	foreign, err := issuer.GenerateServiceToken(jwt.Identity{UserID: 1, Username: "auth-service"}, jwt.AudienceMessaging)
	if err != nil {
		t.Fatalf("GenerateServiceToken() error = %v", err)
	}

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"access token", access, http.StatusOK},
		{"refresh token", refresh, http.StatusUnauthorized},
		{"token for another service", foreign, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			router := gin.New()
			router.Use(NewAuthMiddleware(verifier).ValidateToken())
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
			c := newIdentityTestContext()
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != "" {
				c.Request.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: tt.cookie})
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
