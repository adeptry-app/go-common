package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/adeptry-app/go-common/jwt/jwttest"
	"github.com/gin-gonic/gin"
)

// testSession is the browser session the generated test tokens are bound to.
var testSession = jwt.Session{ID: "62f0d0f7-6a2a-4a3a-9f3e-2f0f8b1d4c77", AuthVersion: 2}

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

// newTestPair returns an issuer and a verifier for the public-api audience.
func newTestPair(t *testing.T) (jwt.Issuer, jwt.Verifier) {
	t.Helper()

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
	return issuer, verifier
}

func TestValidateToken_RejectsNonAccessTokens(t *testing.T) {
	issuer, verifier := newTestPair(t)

	access, err := issuer.GenerateAccessToken(jwt.Identity{UserID: 42, Username: "kaladin", Scopes: nil}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	refresh, err := issuer.GenerateRefreshToken(jwt.Identity{UserID: 42, Username: "kaladin", Scopes: nil}, testSession)
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
		// No validator wired, so a session-bound token passes on local checks alone.
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

// recordingValidator captures what the middleware asked about and answers with
// a fixed verdict.
type recordingValidator struct {
	err     error
	calls   int
	userID  int64
	session jwt.Session
}

func (r *recordingValidator) ValidateSession(_ context.Context, userID int64, session jwt.Session) error {
	r.calls++
	r.userID = userID
	r.session = session
	return r.err
}

func TestValidateToken_SessionValidation(t *testing.T) {
	issuer, verifier := newTestPair(t)

	identity := jwt.Identity{UserID: 42, Username: "kaladin"}
	bound, err := issuer.GenerateAccessToken(identity, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	// A service token carries no session, so S2S calls outlive a user logout.
	unbound, err := issuer.GenerateServiceToken(identity, jwt.AudiencePublicAPI)
	if err != nil {
		t.Fatalf("GenerateServiceToken() error = %v", err)
	}

	tests := []struct {
		name       string
		token      string
		err        error
		wantStatus int
		wantCalls  int
	}{
		{"live session", bound, nil, http.StatusOK, 1},
		{"revoked session", bound, jwt.ErrSessionRevoked, http.StatusUnauthorized, 1},
		{"validator unreachable", bound, errors.New("redis down"), http.StatusServiceUnavailable, 1},
		{"token without a session", unbound, jwt.ErrSessionRevoked, http.StatusOK, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			validator := &recordingValidator{err: tt.err}
			router := gin.New()
			router.Use(NewAuthMiddleware(verifier, WithSessionValidator(validator)).ValidateToken())
			router.GET("/heroes", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/heroes", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if validator.calls != tt.wantCalls {
				t.Errorf("validator calls = %d, want %d", validator.calls, tt.wantCalls)
			}
			if tt.wantCalls > 0 {
				if validator.userID != identity.UserID {
					t.Errorf("validated user = %d, want %d", validator.userID, identity.UserID)
				}
				if validator.session != testSession {
					t.Errorf("validated session = %+v, want %+v", validator.session, testSession)
				}
			}
		})
	}
}

func TestValidateTokenOptional(t *testing.T) {
	issuer, verifier := newTestPair(t)

	identity := jwt.Identity{UserID: 42, Username: "kaladin"}
	valid, err := issuer.GenerateAccessToken(identity, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	tests := []struct {
		name         string
		token        string
		validatorErr error
		wantStatus   int
		wantIdentity bool
	}{
		{"no token", "", nil, http.StatusOK, false},
		{"valid token", valid, nil, http.StatusOK, true},
		{"garbage token", "not-a-jwt", nil, http.StatusOK, false},
		{"revoked session", valid, jwt.ErrSessionRevoked, http.StatusOK, false},
		{"validator unreachable", valid, errors.New("redis down"), http.StatusServiceUnavailable, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			validator := &recordingValidator{err: tt.validatorErr}
			gotIdentity := false

			router := gin.New()
			router.Use(NewAuthMiddleware(verifier, WithSessionValidator(validator)).ValidateTokenOptional())
			router.GET("/compendium", func(c *gin.Context) {
				_, gotIdentity = GetIdentity(c)
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/compendium", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if gotIdentity != tt.wantIdentity {
				t.Errorf("identity present = %v, want %v", gotIdentity, tt.wantIdentity)
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
