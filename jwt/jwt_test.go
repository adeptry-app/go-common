package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/adeptry-app/go-common/jwt/jwttest"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testKeyID         = "test1"
	testAccessExpiry  = 15 * time.Minute
	testRefreshExpiry = 168 * time.Hour
)

// testAudience mirrors the services a browser access cookie reaches.
var testAudience = []string{AudiencePublicAPI, AudienceFilesAPI}

// testSession is the browser session the generated test tokens are bound to.
var testSession = Session{ID: "0e2a1f2c-1c4f-4a3f-9a2b-6d1f0b7c8e55", AuthVersion: 3}

func mustIssuer(t *testing.T, private, public string, accessExpiry, refreshExpiry time.Duration) Issuer {
	t.Helper()

	iss, err := NewIssuer(IssuerConfig{
		PrivateKey:     private,
		PublicKeys:     public,
		AccessAudience: testAudience,
		AccessExpiry:   accessExpiry,
		RefreshExpiry:  refreshExpiry,
	})
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	return iss
}

func newIssuer(private, public string, audience []string, access, refresh time.Duration) (Issuer, error) {
	return NewIssuer(IssuerConfig{
		PrivateKey:     private,
		PublicKeys:     public,
		AccessAudience: audience,
		AccessExpiry:   access,
		RefreshExpiry:  refresh,
	})
}

func mustVerifier(t *testing.T, publicKeys, audience string) Verifier {
	t.Helper()

	ver, err := NewVerifier(publicKeys, audience)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	return ver
}

func newTestIssuer(t *testing.T, accessExpiry, refreshExpiry time.Duration) Issuer {
	t.Helper()

	private, public := jwttest.KeyPair(t, testKeyID)
	return mustIssuer(t, private, public, accessExpiry, refreshExpiry)
}

// newTestPair returns an issuer and a verifier for a different audience, both on
// the same key.
func newTestPair(t *testing.T, audience string) (Issuer, Verifier) {
	t.Helper()

	private, public := jwttest.KeyPair(t, testKeyID)
	return mustIssuer(t, private, public, testAccessExpiry, testRefreshExpiry),
		mustVerifier(t, public, audience)
}

// ecdsaPublicKey returns a well-formed PKIX key that is not Ed25519.
func ecdsaPublicKey(t *testing.T, kid string) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(&testECDSAKey(t).PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	return kid + ":" + base64.StdEncoding.EncodeToString(der)
}

// ecdsaPrivateKey returns a well-formed PKCS8 key that is not Ed25519.
func ecdsaPrivateKey(t *testing.T, kid string) string {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(testECDSAKey(t))
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	return kid + ":" + base64.StdEncoding.EncodeToString(der)
}

func testECDSAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	return key
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewIssuer_KeyErrors(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)
	_, otherPublic := jwttest.KeyPair(t, testKeyID)
	_, unrelatedPublic := jwttest.KeyPair(t, "other")

	tests := []struct {
		name       string
		private    string
		publicKeys string
		wantErr    error
	}{
		{
			name:       "private key id absent from public list",
			private:    private,
			publicKeys: unrelatedPublic,
			wantErr:    ErrUnknownKeyID,
		},
		{
			name:       "same key id, different key",
			private:    private,
			publicKeys: otherPublic,
			wantErr:    ErrKeyMismatch,
		},
		{
			name:       "private key missing the kid prefix",
			private:    strings.SplitN(private, ":", 2)[1],
			publicKeys: public,
			wantErr:    ErrMalformedKey,
		},
		{
			name:       "private key not base64",
			private:    testKeyID + ":not-base64!!",
			publicKeys: public,
			wantErr:    ErrMalformedKey,
		},
		{
			name:       "no public keys",
			private:    private,
			publicKeys: "",
			wantErr:    ErrNoPublicKeys,
		},
		{
			name:       "well-formed private key that is not Ed25519",
			private:    ecdsaPrivateKey(t, testKeyID),
			publicKeys: public,
			wantErr:    jwt.ErrNotEdPrivateKey,
		},
		{
			name:       "same key id listed twice",
			private:    private,
			publicKeys: public + "," + otherPublic,
			wantErr:    ErrDuplicateKeyID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newIssuer(tt.private, tt.publicKeys, testAudience, testAccessExpiry, testRefreshExpiry)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewIssuer() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewIssuer_PublicKeyUsedAsPrivate(t *testing.T) {
	_, public := jwttest.KeyPair(t, testKeyID)

	if _, err := newIssuer(public, public, testAudience, testAccessExpiry, testRefreshExpiry); err == nil {
		t.Error("NewIssuer() should reject a public key in the private key slot")
	}
}

func TestNewIssuer_NonPositiveExpiry(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)

	for _, tt := range []struct{ access, refresh time.Duration }{
		{0, testRefreshExpiry},
		{testAccessExpiry, 0},
		{-time.Second, testRefreshExpiry},
	} {
		if _, err := newIssuer(private, public, testAudience, tt.access, tt.refresh); !errors.Is(err, ErrInvalidExpiry) {
			t.Errorf("NewIssuer(%v, %v) error = %v, want %v", tt.access, tt.refresh, err, ErrInvalidExpiry)
		}
	}
}

func TestNewVerifier(t *testing.T) {
	_, public := jwttest.KeyPair(t, testKeyID)

	tests := []struct {
		name       string
		publicKeys string
		audience   string
		wantFail   bool
		wantErr    error
	}{
		{
			name:       "valid",
			publicKeys: public,
			audience:   AudiencePublicAPI,
		},
		{
			name:       "empty audience",
			publicKeys: public,
			audience:   "",
			wantFail:   true,
			wantErr:    ErrEmptyAudience,
		},
		{
			name:       "empty key list",
			publicKeys: "",
			audience:   AudiencePublicAPI,
			wantFail:   true,
			wantErr:    ErrNoPublicKeys,
		},
		{
			name:       "malformed entry",
			publicKeys: "no-colon-here",
			audience:   AudiencePublicAPI,
			wantFail:   true,
			wantErr:    ErrMalformedKey,
		},
		{
			name:       "well-formed key that is not Ed25519",
			publicKeys: ecdsaPublicKey(t, testKeyID),
			audience:   AudiencePublicAPI,
			wantFail:   true,
			wantErr:    jwt.ErrNotEdPublicKey,
		},
		{
			name:       "base64 that is not a key at all",
			publicKeys: testKeyID + ":" + base64.StdEncoding.EncodeToString([]byte("garbage")),
			audience:   AudiencePublicAPI,
			wantFail:   true, // x509 parse error, no sentinel to match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, err := NewVerifier(tt.publicKeys, tt.audience)

			if !tt.wantFail {
				if err != nil {
					t.Fatalf("NewVerifier() unexpected error = %v", err)
				}
				if ver == nil {
					t.Error("NewVerifier() returned nil")
				}
				return
			}

			if err == nil {
				t.Fatal("NewVerifier() should have failed")
			}
			// A typed-nil in the interface would make this pass a nil check and
			// panic on the first request instead.
			if ver != nil {
				t.Errorf("NewVerifier() returned %v alongside error %v, want nil", ver, err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("NewVerifier() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestInterfaceCompliance(t *testing.T) {
	var _ Verifier = (*verifier)(nil)
	var _ Issuer = (*issuer)(nil)
}

// =============================================================================
// GenerateAccessToken Tests
// =============================================================================

func TestGenerateToken_InputValidation(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	tests := []struct {
		name     string
		userID   int64
		username string
		wantErr  error
	}{
		{
			name:     "valid user",
			userID:   1,
			username: "testuser",
		},
		{
			name:     "valid user with long username",
			userID:   999,
			username: "very_long_username_with_special_chars_123",
		},
		{
			name:     "max int64 user ID",
			userID:   math.MaxInt64,
			username: "testuser",
		},
		{
			name:     "zero user ID",
			userID:   0,
			username: "testuser",
			wantErr:  ErrInvalidUserID,
		},
		{
			name:     "negative user ID",
			userID:   -1,
			username: "testuser",
			wantErr:  ErrInvalidUserID,
		},
		{
			name:     "empty username",
			userID:   1,
			username: "",
			wantErr:  ErrEmptyUsername,
		},
	}

	generators := map[string]func(Identity) (string, error){
		"access":  func(id Identity) (string, error) { return iss.GenerateAccessToken(id, testSession) },
		"refresh": func(id Identity) (string, error) { return iss.GenerateRefreshToken(id, testSession) },
		// AudienceAuth so the issuer's own verifier can round-trip the result.
		"service": func(id Identity) (string, error) { return iss.GenerateServiceToken(id, AudienceAuth) },
	}
	validators := map[string]func(string) (*Claims, error){
		"access":  iss.ValidateAccessToken,
		"refresh": iss.ValidateRefreshToken,
		"service": iss.ValidateAccessToken,
	}

	for kind, generate := range generators {
		for _, tt := range tests {
			t.Run(kind+"/"+tt.name, func(t *testing.T) {
				scopes := map[string]string{"profile": "read", "projects": "edit"}
				token, err := generate(Identity{UserID: tt.userID, Username: tt.username, Scopes: scopes})

				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("generate() error = %v, wantErr %v", err, tt.wantErr)
				}
				if tt.wantErr != nil {
					return
				}
				if token == "" {
					t.Fatal("Generated token is empty")
				}

				claims, err := validators[kind](token)
				if err != nil {
					t.Fatalf("validate() error = %v", err)
				}
				if claims.UserID != tt.userID {
					t.Errorf("Claims.UserID = %v, want %v", claims.UserID, tt.userID)
				}
				if claims.Username != tt.username {
					t.Errorf("Claims.Username = %v, want %v", claims.Username, tt.username)
				}
			})
		}
	}
}

func TestGenerateAccessToken_SpecialCharactersInUsername(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	tests := []struct {
		name     string
		username string
	}{
		{
			name:     "unicode characters",
			username: "用户名_123",
		},
		{
			name:     "special symbols",
			username: "user@example.com",
		},
		{
			name:     "spaces and punctuation",
			username: "John Doe Jr.",
		},
		{
			name:     "quotes",
			username: `user"with'quotes`,
		},
		{
			name:     "newlines and tabs",
			username: "user\nwith\ttabs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := map[string]string{"profile": "read"}
			token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: tt.username, Scopes: scopes}, testSession)
			if err != nil {
				t.Fatalf("GenerateAccessToken() error = %v", err)
			}

			claims, err := iss.ValidateAccessToken(token)
			if err != nil {
				t.Fatalf("ValidateAccessToken() error = %v", err)
			}

			if claims.Username != tt.username {
				t.Errorf("Claims.Username = %v, want %v", claims.Username, tt.username)
			}
		})
	}
}

func TestGenerateAccessToken_TokensAreDifferent(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate multiple tokens for same user
	token1, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Sleep to ensure different IssuedAt timestamp (JWT timestamps are in seconds)
	time.Sleep(1001 * time.Millisecond)

	token2, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Tokens should be different due to different IssuedAt times
	if token1 == token2 {
		t.Error("Sequential tokens for same user should be different")
	}

	// But both should be valid
	claims1, err := iss.ValidateAccessToken(token1)
	if err != nil {
		t.Fatalf("ValidateAccessToken(token1) error = %v", err)
	}
	if claims1.UserID != 1 {
		t.Errorf("Claims1.UserID = %v, want 1", claims1.UserID)
	}

	claims2, err := iss.ValidateAccessToken(token2)
	if err != nil {
		t.Fatalf("ValidateAccessToken(token2) error = %v", err)
	}
	if claims2.UserID != 1 {
		t.Errorf("Claims2.UserID = %v, want 1", claims2.UserID)
	}
}

func TestGenerateAccessToken_ClaimsStructure(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	userID := int64(42)
	username := "testuser"
	scopes := map[string]string{"profile": "read", "projects": "edit"}
	beforeGeneration := time.Now()

	token, err := iss.GenerateAccessToken(Identity{UserID: userID, Username: username, Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	afterGeneration := time.Now()

	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	// Verify custom claims
	if claims.UserID != userID {
		t.Errorf("Claims.UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Username != username {
		t.Errorf("Claims.Username = %v, want %v", claims.Username, username)
	}

	// Verify registered claims
	if claims.ExpiresAt == nil {
		t.Error("Claims.ExpiresAt is nil")
	}
	if claims.IssuedAt == nil {
		t.Error("Claims.IssuedAt is nil")
	}
	if claims.Issuer != IssuerName {
		t.Errorf("Claims.Issuer = %q, want %q", claims.Issuer, IssuerName)
	}

	// IssuedAt should be between before and after generation
	issuedAt := claims.IssuedAt.Time
	if issuedAt.Before(beforeGeneration.Add(-time.Second)) || issuedAt.After(afterGeneration.Add(time.Second)) {
		t.Errorf("IssuedAt %v not within expected range [%v, %v]", issuedAt, beforeGeneration, afterGeneration)
	}

	// ExpiresAt should be IssuedAt + expiry
	expectedExpiry := issuedAt.Add(testAccessExpiry)
	expiresAt := claims.ExpiresAt.Time
	diff := expiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("ExpiresAt difference = %v, want within 1 second", diff)
	}
}

func TestGenerateAccessToken_SigningMethodAndKeyID(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)
	iss := mustIssuer(t, private, public, testAccessExpiry, testRefreshExpiry)

	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	parsed, _, err := jwt.NewParser().ParseUnverified(token, &Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}

	if parsed.Method.Alg() != signingMethod {
		t.Errorf("Token alg = %q, want %q", parsed.Method.Alg(), signingMethod)
	}
	if parsed.Header["kid"] != testKeyID {
		t.Errorf("Token kid = %v, want %q", parsed.Header["kid"], testKeyID)
	}
}

func TestGenerateToken_WithScopes(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	tests := []struct {
		name   string
		scopes map[string]string
	}{
		{
			name:   "with scopes",
			scopes: map[string]string{"profile": "read", "projects": "edit", "users": "delete"},
		},
		{
			name:   "empty scopes",
			scopes: map[string]string{},
		},
		{
			name:   "nil scopes",
			scopes: nil,
		},
		{
			name:   "scopes with special characters",
			scopes: map[string]string{"user:profile": "read", "admin/settings": "edit", "key-with-dash": "delete"},
		},
	}

	generators := map[string]func(Identity) (string, error){
		"access":  func(id Identity) (string, error) { return iss.GenerateAccessToken(id, testSession) },
		"refresh": func(id Identity) (string, error) { return iss.GenerateRefreshToken(id, testSession) },
	}
	validators := map[string]func(string) (*Claims, error){
		"access":  iss.ValidateAccessToken,
		"refresh": iss.ValidateRefreshToken,
	}

	for kind, generate := range generators {
		for _, tt := range tests {
			t.Run(kind+"/"+tt.name, func(t *testing.T) {
				token, err := generate(Identity{UserID: 1, Username: "testuser", Scopes: tt.scopes})
				if err != nil {
					t.Fatalf("generate() error = %v", err)
				}

				claims, err := validators[kind](token)
				if err != nil {
					t.Fatalf("validate() error = %v", err)
				}
				checkScopes(t, tt.scopes, claims.Scopes)
			})
		}
	}
}

// checkScopes compares scopes across a JSON round trip, where an empty map comes
// back as nil.
func checkScopes(t *testing.T, want, got map[string]string) {
	t.Helper()

	if want == nil {
		if got != nil {
			t.Errorf("Claims.Scopes = %v, want nil", got)
		}
		return
	}
	if len(got) != len(want) {
		t.Errorf("Claims.Scopes = %v, want %v", got, want)
		return
	}
	for resource, level := range want {
		if got[resource] != level {
			t.Errorf("Claims.Scopes[%s] = %v, want %v", resource, got[resource], level)
		}
	}
}

// =============================================================================
// Audience Tests
// =============================================================================

func TestAccessTokenAudience(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	// AudienceAuth is implicit; the rest come from the configured list.
	want := append([]string{AudienceAuth}, testAudience...)
	if !slices.Equal(claims.Audience, want) {
		t.Errorf("access token audience = %v, want %v", claims.Audience, want)
	}
	if slices.Contains(claims.Audience, AudienceMessaging) {
		t.Errorf("access token audience %v must not include %q", claims.Audience, AudienceMessaging)
	}
}

func TestAccessAudience(t *testing.T) {
	tests := []struct {
		name       string
		configured []string
		want       []string
		wantErr    error
	}{
		{
			name:       "auth service is prepended",
			configured: []string{AudiencePublicAPI, AudienceFilesAPI},
			want:       []string{AudienceAuth, AudiencePublicAPI, AudienceFilesAPI},
		},
		{
			name:       "explicit auth service is not duplicated",
			configured: []string{AudienceAuth, AudiencePublicAPI},
			want:       []string{AudienceAuth, AudiencePublicAPI},
		},
		{
			name:       "repeated entry is collapsed",
			configured: []string{AudiencePublicAPI, AudiencePublicAPI},
			want:       []string{AudienceAuth, AudiencePublicAPI},
		},
		{
			name:       "unset",
			configured: nil,
			wantErr:    ErrEmptyAudience,
		},
		{
			name:       "blank entry",
			configured: []string{AudiencePublicAPI, ""},
			wantErr:    ErrEmptyAudience,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := accessAudience(tt.configured)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("accessAudience() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && !slices.Equal(got, tt.want) {
				t.Errorf("accessAudience() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateAccessToken_UsesConfiguredAudience(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)
	iss, err := newIssuer(private, public, []string{AudienceMessaging}, testAccessExpiry, testRefreshExpiry)
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// A service not in the configured list must reject the token.
	if _, err := mustVerifier(t, public, AudiencePublicAPI).ValidateAccessToken(token); err == nil {
		t.Error("public-api must reject a token whose audience excludes it")
	}
	if _, err := mustVerifier(t, public, AudienceMessaging).ValidateAccessToken(token); err != nil {
		t.Errorf("messaging-api should accept the token: %v", err)
	}
}

func TestRefreshTokenAudienceIsAuthOnly(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	token, err := iss.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	claims, err := iss.ValidateRefreshToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if len(claims.Audience) != 1 || claims.Audience[0] != AudienceAuth {
		t.Errorf("refresh token audience = %v, want [%q]", claims.Audience, AudienceAuth)
	}
}

func TestVerifierRejectsForeignAudience(t *testing.T) {
	iss, publicAPI := newTestPair(t, AudiencePublicAPI)

	serviceToken, err := iss.GenerateServiceToken(Identity{UserID: 1, Username: "auth-service"}, AudienceMessaging)
	if err != nil {
		t.Fatalf("GenerateServiceToken() error = %v", err)
	}

	if _, err := publicAPI.ValidateAccessToken(serviceToken); err == nil {
		t.Error("public-api must reject a token addressed to messaging-api")
	}
}

func TestVerifierRejectsBrowserTokenAtMessaging(t *testing.T) {
	iss, messaging := newTestPair(t, AudienceMessaging)

	access, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	if _, err := messaging.ValidateAccessToken(access); err == nil {
		t.Error("messaging-api must reject a browser access token")
	}
}

func TestVerifierRejectsRefreshTokenAtOtherServices(t *testing.T) {
	iss, filesAPI := newTestPair(t, AudienceFilesAPI)

	refresh, err := iss.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	if _, err := filesAPI.ValidateRefreshToken(refresh); err == nil {
		t.Error("files-api must reject a refresh token")
	}
}

func TestGenerateServiceToken(t *testing.T) {
	iss, messaging := newTestPair(t, AudienceMessaging)

	id := Identity{UserID: 1, Username: "auth-service", Scopes: map[string]string{"emails": "edit"}}
	token, err := iss.GenerateServiceToken(id, AudienceMessaging)
	if err != nil {
		t.Fatalf("GenerateServiceToken() error = %v", err)
	}

	claims, err := messaging.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != AudienceMessaging {
		t.Errorf("service token audience = %v, want [%q]", claims.Audience, AudienceMessaging)
	}
	if claims.Scopes["emails"] != "edit" {
		t.Errorf("Claims.Scopes[emails] = %q, want edit", claims.Scopes["emails"])
	}
}

func TestGenerateServiceToken_InputValidation(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)
	id := Identity{UserID: 1, Username: "auth-service"}

	if _, err := iss.GenerateServiceToken(id, ""); !errors.Is(err, ErrEmptyAudience) {
		t.Errorf("GenerateServiceToken() error = %v, want %v", err, ErrEmptyAudience)
	}
	if _, err := iss.GenerateServiceToken(Identity{Username: "auth-service"}, AudienceMessaging); !errors.Is(err, ErrInvalidUserID) {
		t.Error("GenerateServiceToken() should reject a non-positive user id")
	}
}

func TestGenerateToken_SessionClaimsRoundTrip(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)
	id := Identity{UserID: 1, Username: "testuser"}

	access, err := iss.GenerateAccessToken(id, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	refresh, err := iss.GenerateRefreshToken(id, testSession)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	tokens := map[string]string{"access": access, "refresh": refresh}
	validators := map[string]func(string) (*Claims, error){
		"access":  iss.ValidateAccessToken,
		"refresh": iss.ValidateRefreshToken,
	}
	for kind, token := range tokens {
		t.Run(kind, func(t *testing.T) {
			claims, err := validators[kind](token)
			if err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if claims.Session() != testSession {
				t.Errorf("Claims.Session() = %+v, want %+v", claims.Session(), testSession)
			}
		})
	}
}

func TestGenerateToken_SessionIsRequiredForBrowserTokens(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)
	id := Identity{UserID: 1, Username: "testuser"}

	if _, err := iss.GenerateAccessToken(id, Session{AuthVersion: 1}); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("GenerateAccessToken() error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := iss.GenerateRefreshToken(id, Session{AuthVersion: 1}); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("GenerateRefreshToken() error = %v, want %v", err, ErrEmptySessionID)
	}
}

func TestGenerateServiceToken_CarriesNoSession(t *testing.T) {
	iss, messaging := newTestPair(t, AudienceMessaging)

	token, err := iss.GenerateServiceToken(Identity{UserID: 1, Username: "auth-service"}, AudienceMessaging)
	if err != nil {
		t.Fatalf("GenerateServiceToken() error = %v", err)
	}

	claims, err := messaging.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.Session() != (Session{}) {
		t.Errorf("service token session = %+v, want none", claims.Session())
	}
}

func TestVerifierRejectsForeignIssuer(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)

	_, key, err := parsePrivateKey(private)
	if err != nil {
		t.Fatalf("parsePrivateKey() error = %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		Identity:  Identity{UserID: 1, Username: "testuser"},
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "someone-else",
			Audience:  []string{AudiencePublicAPI},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	token.Header["kid"] = testKeyID
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	// The signature and audience check out; only `iss` is wrong.
	ver := mustVerifier(t, public, AudiencePublicAPI)
	if _, err := ver.ValidateAccessToken(signed); err == nil {
		t.Error("ValidateAccessToken() should reject a foreign issuer")
	}
}

// =============================================================================
// Key Rotation Tests
// =============================================================================

func TestKeyRotation_VerifierAcceptsBothKeys(t *testing.T) {
	oldPrivate, oldPublic := jwttest.KeyPair(t, "old")
	newPrivate, newPublic := jwttest.KeyPair(t, "new")
	both := oldPublic + "," + newPublic

	oldIssuer := mustIssuer(t, oldPrivate, both, testAccessExpiry, testRefreshExpiry)
	newIssuer := mustIssuer(t, newPrivate, both, testAccessExpiry, testRefreshExpiry)
	ver := mustVerifier(t, both, AudiencePublicAPI)

	id := Identity{UserID: 1, Username: "testuser"}
	for name, iss := range map[string]Issuer{"old": oldIssuer, "new": newIssuer} {
		token, err := iss.GenerateAccessToken(id, testSession)
		if err != nil {
			t.Fatalf("GenerateAccessToken(%s) error = %v", name, err)
		}
		if _, err := ver.ValidateAccessToken(token); err != nil {
			t.Errorf("ValidateAccessToken(%s) error = %v", name, err)
		}
	}

	// Retiring the old key must invalidate tokens it signed.
	retired := mustVerifier(t, newPublic, AudiencePublicAPI)
	oldToken, err := oldIssuer.GenerateAccessToken(id, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken(old) error = %v", err)
	}
	if _, err := retired.ValidateAccessToken(oldToken); !errors.Is(err, ErrUnknownKeyID) {
		t.Errorf("ValidateAccessToken(retired) error = %v, want %v", err, ErrUnknownKeyID)
	}
}

// =============================================================================
// GenerateRefreshToken Tests
// =============================================================================

func TestGenerateToken_ScopesDefensiveCopy(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	originalScopes := map[string]string{"profile": "read", "projects": "edit"}
	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: originalScopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Mutate original map after token generation
	originalScopes["profile"] = "delete"
	originalScopes["newkey"] = "read"

	// Validate token and verify claims weren't affected by mutation
	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	// Claims should have original values, not mutated ones
	if claims.Scopes["profile"] != "read" {
		t.Errorf("Claims.Scopes[profile] = %v, want 'read' (defensive copy failed)", claims.Scopes["profile"])
	}
	if _, exists := claims.Scopes["newkey"]; exists {
		t.Error("Claims.Scopes contains 'newkey' but shouldn't (defensive copy failed)")
	}
	if len(claims.Scopes) != 2 {
		t.Errorf("Claims.Scopes length = %d, want 2", len(claims.Scopes))
	}
}

// =============================================================================
// Validation Tests
// =============================================================================

func TestValidateAccessToken_ValidToken(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	userID := int64(1)
	username := "testuser"
	scopes := map[string]string{"profile": "read"}

	token, err := iss.GenerateAccessToken(Identity{UserID: userID, Username: username, Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("Claims.UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Username != username {
		t.Errorf("Claims.Username = %v, want %v", claims.Username, username)
	}
}

func TestValidateAccessToken_ExpiredToken(t *testing.T) {
	t.Parallel()

	// Create issuer with very short expiry
	iss := newTestIssuer(t, 1*time.Millisecond, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	if _, err = iss.ValidateAccessToken(token); err == nil {
		t.Error("ValidateAccessToken() should fail for expired token")
	}
}

func TestValidateAccessToken_InvalidSignature(t *testing.T) {
	iss1 := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)
	iss2 := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate token with iss1
	token, err := iss1.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Both issuers share the key id but hold different key material
	if _, err = iss2.ValidateAccessToken(token); err == nil {
		t.Error("ValidateAccessToken() should fail for a token signed by another key")
	}
}

func TestValidateAccessToken_MalformedToken(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "random string",
			token: "not-a-jwt-token",
		},
		{
			name:  "incomplete token",
			token: "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9",
		},
		{
			name:  "token with invalid parts",
			token: "header.payload",
		},
		{
			name:  "invalid base64",
			token: "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.###.xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := iss.ValidateAccessToken(tt.token); err == nil {
				t.Error("ValidateAccessToken() should fail for malformed token")
			}
		})
	}
}

func TestValidateAccessToken_TamperedToken(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Tamper with the token by changing a character
	tamperedToken := token[:len(token)-5] + "XXXXX"

	if _, err = iss.ValidateAccessToken(tamperedToken); err == nil {
		t.Error("ValidateAccessToken() should fail for tampered token")
	}
}

func TestValidateAccessToken_AlgorithmConfusion(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)
	iss := mustIssuer(t, private, public, testAccessExpiry, testRefreshExpiry)

	// Forge an HS256 token using the (public) verification key as the HMAC secret.
	_, der, err := splitKeyEntry(public)
	if err != nil {
		t.Fatalf("splitKeyEntry() error = %v", err)
	}

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Identity:  Identity{UserID: 1, Username: "attacker", Scopes: map[string]string{"heroes": "delete"}},
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    IssuerName,
			Audience:  []string{AudienceAuth},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	forged.Header["kid"] = testKeyID
	signed, err := forged.SignedString(der)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	if _, err := iss.ValidateAccessToken(signed); err == nil {
		t.Error("ValidateAccessToken() must reject an HS256 token signed with the public key")
	}
}

func TestValidateAccessToken_WrongSigningMethodNone(t *testing.T) {
	_, verifier := newTestPair(t, AudiencePublicAPI)

	claims := &Claims{
		Identity:  Identity{UserID: 123, Username: "user"},
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    IssuerName,
			Audience:  []string{AudiencePublicAPI},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	token.Header["kid"] = testKeyID
	tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	if _, err := verifier.ValidateAccessToken(tokenString); err == nil {
		t.Error("ValidateAccessToken() should fail for token with 'none' signing method")
	}
}

func TestValidateAccessToken_UnknownKeyID(t *testing.T) {
	signingPrivate, signingPublic := jwttest.KeyPair(t, "signing")
	_, trustedPublic := jwttest.KeyPair(t, "trusted")

	signer := mustIssuer(t, signingPrivate, signingPublic, testAccessExpiry, testRefreshExpiry)
	token, err := signer.GenerateAccessToken(Identity{UserID: 1, Username: "testuser"}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// A verifier that has never seen the signing key id rejects on lookup.
	ver := mustVerifier(t, trustedPublic, AudienceAuth)
	if _, err := ver.ValidateAccessToken(token); !errors.Is(err, ErrUnknownKeyID) {
		t.Errorf("ValidateAccessToken() error = %v, want %v", err, ErrUnknownKeyID)
	}
}

func TestValidateAccessToken_MissingKeyID(t *testing.T) {
	private, public := jwttest.KeyPair(t, testKeyID)
	_, key, err := parsePrivateKey(private)
	if err != nil {
		t.Fatalf("parsePrivateKey() error = %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		Identity:  Identity{UserID: 1, Username: "testuser"},
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    IssuerName,
			Audience:  []string{AudiencePublicAPI},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	ver := mustVerifier(t, public, AudiencePublicAPI)
	if _, err := ver.ValidateAccessToken(signed); !errors.Is(err, ErrUnknownKeyID) {
		t.Errorf("ValidateAccessToken() error = %v, want %v", err, ErrUnknownKeyID)
	}
}

func TestValidateAccessToken_InvalidClaimsStructure(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate a valid token
	validToken, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Parse the token to get its parts
	parts := strings.Split(validToken, ".")
	if len(parts) != 3 {
		t.Fatalf("Expected 3 parts in JWT, got %d", len(parts))
	}

	// Create a token with corrupted payload but valid signature structure
	corruptedPayload := "eyJpbnZhbGlkIjoiY2xhaW1zIn0" // {"invalid":"claims"}
	corruptedToken := parts[0] + "." + corruptedPayload + "." + parts[2]

	if _, err = iss.ValidateAccessToken(corruptedToken); err == nil {
		t.Error("ValidateAccessToken() should fail for token with invalid claims structure")
	}
}

func TestValidateAccessToken_ExpiryBoundary(t *testing.T) {
	t.Parallel()

	// Test token validation exactly at expiry boundary
	// JWT timestamps are truncated to seconds, so use multi-second expiry
	iss := newTestIssuer(t, 3*time.Second, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Should be valid immediately
	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims == nil {
		t.Fatal("Claims should not be nil")
	}

	// Wait 1 second - should still be valid
	time.Sleep(1 * time.Second)

	// Should still be valid
	claims, err = iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims == nil {
		t.Fatal("Claims should not be nil")
	}

	// Wait past expiry (3+ seconds total)
	time.Sleep(2500 * time.Millisecond)

	// Should now be invalid
	if _, err = iss.ValidateAccessToken(token); err == nil {
		t.Error("ValidateAccessToken() should fail for expired token")
	}
}

// =============================================================================
// Expiry Tests
// =============================================================================

func TestGetExpiryMethods(t *testing.T) {
	customAccess := 30 * time.Minute
	customRefresh := 720 * time.Hour

	iss := newTestIssuer(t, customAccess, customRefresh)

	if got := iss.GetAccessExpiry(); got != customAccess {
		t.Errorf("GetAccessExpiry() = %v, want %v", got, customAccess)
	}
	if got := iss.GetRefreshExpiry(); got != customRefresh {
		t.Errorf("GetRefreshExpiry() = %v, want %v", got, customRefresh)
	}
}

func TestRefreshTokenExpiry(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := iss.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	claims, err := iss.ValidateRefreshToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	// Calculate expected expiry
	expectedExpiry := claims.IssuedAt.Add(testRefreshExpiry)
	actualExpiry := claims.ExpiresAt.Time

	// Should be within 1 second due to timing
	diff := actualExpiry.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("Expiry difference = %v, want within 1 second", diff)
	}

	// Refresh token should expire much later than access token
	accessToken, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	accessClaims, err := iss.ValidateAccessToken(accessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if !claims.ExpiresAt.After(accessClaims.ExpiresAt.Time) {
		t.Error("Refresh token should expire after access token")
	}
}

func TestVeryLongExpiry(t *testing.T) {
	longExpiry := 8760 * time.Hour // 1 year
	iss := newTestIssuer(t, longExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	// Verify expiry is approximately 1 year from now
	expectedExpiry := claims.IssuedAt.Add(longExpiry)
	diff := claims.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("Expiry difference = %v, want within 1 second", diff)
	}
}

// =============================================================================
// Token Type Tests
// =============================================================================

func TestGenerateToken_TokenType(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	access, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: nil}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	accessClaims, err := iss.ValidateAccessToken(access)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if accessClaims.TokenType != TokenTypeAccess {
		t.Errorf("access TokenType = %q, want %q", accessClaims.TokenType, TokenTypeAccess)
	}

	refresh, err := iss.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser", Scopes: nil}, testSession)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}
	refreshClaims, err := iss.ValidateRefreshToken(refresh)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if refreshClaims.TokenType != TokenTypeRefresh {
		t.Errorf("refresh TokenType = %q, want %q", refreshClaims.TokenType, TokenTypeRefresh)
	}
}

func TestValidateTyped_RejectsTheOtherTokenType(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)
	id := Identity{UserID: 1, Username: "testuser", Scopes: nil}

	access, err := iss.GenerateAccessToken(id, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	refresh, err := iss.GenerateRefreshToken(id, testSession)
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	if _, err := iss.ValidateAccessToken(access); err != nil {
		t.Errorf("ValidateAccessToken(access) error = %v", err)
	}
	if _, err := iss.ValidateRefreshToken(refresh); err != nil {
		t.Errorf("ValidateRefreshToken(refresh) error = %v", err)
	}
	if _, err := iss.ValidateAccessToken(refresh); !errors.Is(err, ErrWrongTokenType) {
		t.Errorf("ValidateAccessToken(refresh) error = %v, want %v", err, ErrWrongTokenType)
	}
	if _, err := iss.ValidateRefreshToken(access); !errors.Is(err, ErrWrongTokenType) {
		t.Errorf("ValidateRefreshToken(access) error = %v, want %v", err, ErrWrongTokenType)
	}
	if _, err := iss.ValidateAccessToken("not-a-token"); errors.Is(err, ErrWrongTokenType) {
		t.Error("a malformed token must fail validation, not the type check")
	}
}

func TestGenerateAccessToken_ProfileClaims(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	token, err := iss.GenerateAccessToken(Identity{
		UserID:        7,
		Username:      "kaladin",
		Email:         "kal@example.com",
		EmailVerified: true,
		DisplayName:   "Kaladin Stormblessed",
		Scopes:        map[string]string{"heroes": "edit"},
	}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := iss.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.Email != "kal@example.com" || !claims.EmailVerified || claims.DisplayName != "Kaladin Stormblessed" {
		t.Errorf("profile claims = %q/%v/%q", claims.Email, claims.EmailVerified, claims.DisplayName)
	}
}

// =============================================================================
// GetTTL Tests
// =============================================================================

func TestGetTTL(t *testing.T) {
	tests := []struct {
		name     string
		claims   *Claims
		wantZero bool
	}{
		{
			name: "valid future expiry",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
				},
			},
			wantZero: false,
		},
		{
			name: "expired",
			claims: &Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
				},
			},
			wantZero: true,
		},
		{
			name:     "nil expiry",
			claims:   &Claims{},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttl := tt.claims.GetTTL()
			if tt.wantZero && ttl != 0 {
				t.Errorf("expected TTL 0, got %d", ttl)
			}
			if !tt.wantZero && ttl <= 0 {
				t.Errorf("expected positive TTL, got %d", ttl)
			}
		})
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestConcurrentTokenGeneration(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	concurrency := 10
	done := make(chan bool, concurrency)
	tokens := make(chan string, concurrency)

	// Generate tokens concurrently
	for i := range concurrency {
		go func(userID int64) {
			scopes := map[string]string{"profile": "read"}
			token, err := iss.GenerateAccessToken(Identity{UserID: userID, Username: "testuser", Scopes: scopes}, testSession)
			if err != nil {
				t.Errorf("GenerateAccessToken() error = %v", err)
			}
			tokens <- token
			done <- true
		}(int64(i + 1))
	}

	// Wait for all goroutines
	for range concurrency {
		<-done
	}
	close(tokens)

	// Verify all tokens are valid and unique
	seen := make(map[string]bool)
	count := 0
	for token := range tokens {
		if token == "" {
			t.Error("Generated token is empty")
			continue
		}

		if seen[token] {
			t.Error("Duplicate token generated")
		}
		seen[token] = true

		claims, err := iss.ValidateAccessToken(token)
		if err != nil {
			t.Errorf("ValidateAccessToken() error = %v", err)
		}
		if claims == nil {
			t.Error("Claims should not be nil")
		}
		count++
	}

	if count != concurrency {
		t.Errorf("Expected %d tokens, got %d", concurrency, count)
	}
}

func TestConcurrentTokenValidation(t *testing.T) {
	iss := newTestIssuer(t, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate a single token
	token, err := iss.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes}, testSession)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	concurrency := 20
	done := make(chan bool, concurrency)
	errs := make(chan error, concurrency)

	// Validate token concurrently
	for range concurrency {
		go func() {
			claims, err := iss.ValidateAccessToken(token)
			if err != nil {
				errs <- err
			} else if claims == nil {
				errs <- jwt.ErrTokenInvalidClaims
			} else if claims.UserID != 1 {
				errs <- jwt.ErrTokenInvalidClaims
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for range concurrency {
		<-done
	}
	close(errs)

	// Check for any errors
	for err := range errs {
		t.Errorf("Concurrent validation error: %v", err)
	}
}
