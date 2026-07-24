package jwt

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret        = "test-secret-key-at-least-32-chars-long"
	testAccessExpiry  = 15 * time.Minute
	testRefreshExpiry = 168 * time.Hour
)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewService(t *testing.T) {
	svc, err := NewService(testSecret, testAccessExpiry, testRefreshExpiry)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if svc == nil {
		t.Fatal("NewService returned nil")
	}

	if got := svc.GetAccessExpiry(); got != testAccessExpiry {
		t.Errorf("GetAccessExpiry() = %v, want %v", got, testAccessExpiry)
	}

	if got := svc.GetRefreshExpiry(); got != testRefreshExpiry {
		t.Errorf("GetRefreshExpiry() = %v, want %v", got, testRefreshExpiry)
	}
}

func TestNewService_EmptySecret(t *testing.T) {
	_, err := NewService("", testAccessExpiry, testRefreshExpiry)
	if err != ErrSecretTooShort {
		t.Errorf("NewService() error = %v, want %v", err, ErrSecretTooShort)
	}
}

func TestNewService_ShortSecret(t *testing.T) {
	_, err := NewService("short", testAccessExpiry, testRefreshExpiry)
	if err != ErrSecretTooShort {
		t.Errorf("NewService() error = %v, want %v", err, ErrSecretTooShort)
	}
}

func TestNewService_Exactly32Bytes(t *testing.T) {
	svc, err := NewService("12345678901234567890123456789012", testAccessExpiry, testRefreshExpiry)
	if err != nil {
		t.Errorf("NewService() unexpected error = %v", err)
	}
	if svc == nil {
		t.Error("NewService() returned nil for 32-byte secret")
	}
}

func TestNewValidatorOnly(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr error
	}{
		{
			name:    "valid secret",
			secret:  testSecret,
			wantErr: nil,
		},
		{
			name:    "secret too short",
			secret:  "short",
			wantErr: ErrSecretTooShort,
		},
		{
			name:    "empty secret",
			secret:  "",
			wantErr: ErrSecretTooShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewValidatorOnly(tt.secret)
			if err != tt.wantErr {
				t.Errorf("NewValidatorOnly() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && svc == nil {
				t.Error("NewValidatorOnly() returned nil")
			}
		})
	}
}

func TestValidatorOnlyCannotGenerateTokens(t *testing.T) {
	svc, err := NewValidatorOnly(testSecret)
	if err != nil {
		t.Fatalf("NewValidatorOnly() error = %v", err)
	}

	scopes := map[string]string{"profile": "read"}
	_, err = svc.GenerateAccessToken(Identity{UserID: 123, Username: "user", Scopes: scopes})
	if err != ErrTokenGenDisabled {
		t.Errorf("GenerateAccessToken() error = %v, want %v", err, ErrTokenGenDisabled)
	}

	_, err = svc.GenerateRefreshToken(Identity{UserID: 123, Username: "user", Scopes: scopes})
	if err != ErrTokenGenDisabled {
		t.Errorf("GenerateRefreshToken() error = %v, want %v", err, ErrTokenGenDisabled)
	}
}

func TestValidatorOnlyExpiryIsZero(t *testing.T) {
	svc, _ := NewValidatorOnly(testSecret)

	if svc.GetAccessExpiry() != 0 {
		t.Errorf("GetAccessExpiry() = %v, want 0", svc.GetAccessExpiry())
	}
	if svc.GetRefreshExpiry() != 0 {
		t.Errorf("GetRefreshExpiry() = %v, want 0", svc.GetRefreshExpiry())
	}
}

func TestServiceInterfaceCompliance(t *testing.T) {
	var _ Service = (*service)(nil)
}

// =============================================================================
// GenerateAccessToken Tests
// =============================================================================

func TestGenerateAccessToken(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

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
			wantErr:  nil,
		},
		{
			name:     "valid user with long username",
			userID:   999,
			username: "very_long_username_with_special_chars_123",
			wantErr:  nil,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := map[string]string{"profile": "read", "projects": "edit"}
			token, err := svc.GenerateAccessToken(Identity{UserID: tt.userID, Username: tt.username, Scopes: scopes})

			if err != tt.wantErr {
				t.Errorf("GenerateAccessToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				if token == "" {
					t.Error("Generated token is empty")
				}

				// Verify token can be validated
				claims, err := svc.ValidateToken(token)
				if err != nil {
					t.Fatalf("ValidateToken() error = %v", err)
				}
				if claims.UserID != tt.userID {
					t.Errorf("Claims.UserID = %v, want %v", claims.UserID, tt.userID)
				}
				if claims.Username != tt.username {
					t.Errorf("Claims.Username = %v, want %v", claims.Username, tt.username)
				}
			}
		})
	}
}

func TestGenerateAccessToken_VeryLargeUserID(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	largeID := int64(9223372036854775807) // Max int64
	scopes := map[string]string{"profile": "read"}

	token, err := svc.GenerateAccessToken(Identity{UserID: largeID, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if claims.UserID != largeID {
		t.Errorf("Claims.UserID = %v, want %v", claims.UserID, largeID)
	}
}

func TestGenerateAccessToken_SpecialCharactersInUsername(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

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
			token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: tt.username, Scopes: scopes})
			if err != nil {
				t.Fatalf("GenerateAccessToken() error = %v", err)
			}

			claims, err := svc.ValidateToken(token)
			if err != nil {
				t.Fatalf("ValidateToken() error = %v", err)
			}

			if claims.Username != tt.username {
				t.Errorf("Claims.Username = %v, want %v", claims.Username, tt.username)
			}
		})
	}
}

func TestGenerateAccessToken_TokensAreDifferent(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate multiple tokens for same user
	token1, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Sleep to ensure different IssuedAt timestamp (JWT timestamps are in seconds)
	time.Sleep(1001 * time.Millisecond)

	token2, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Tokens should be different due to different IssuedAt times
	if token1 == token2 {
		t.Error("Sequential tokens for same user should be different")
	}

	// But both should be valid
	claims1, err := svc.ValidateToken(token1)
	if err != nil {
		t.Fatalf("ValidateToken(token1) error = %v", err)
	}
	if claims1.UserID != 1 {
		t.Errorf("Claims1.UserID = %v, want 1", claims1.UserID)
	}

	claims2, err := svc.ValidateToken(token2)
	if err != nil {
		t.Fatalf("ValidateToken(token2) error = %v", err)
	}
	if claims2.UserID != 1 {
		t.Errorf("Claims2.UserID = %v, want 1", claims2.UserID)
	}
}

func TestGenerateAccessToken_ClaimsStructure(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	userID := int64(42)
	username := "testuser"
	scopes := map[string]string{"profile": "read", "projects": "edit"}
	beforeGeneration := time.Now()

	token, err := svc.GenerateAccessToken(Identity{UserID: userID, Username: username, Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	afterGeneration := time.Now()

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
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

func TestGenerateAccessToken_SigningMethod(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate valid token
	validToken, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Parse and verify it uses HMAC
	token, err := jwt.ParseWithClaims(validToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method is HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			t.Errorf("Token uses %v, want *jwt.SigningMethodHMAC", token.Method)
		}
		return []byte(testSecret), nil
	})

	if err != nil {
		t.Fatalf("ParseWithClaims() error = %v", err)
	}
	if !token.Valid {
		t.Error("Token should be valid")
	}
}

func TestGenerateAccessToken_WithScopes(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: tt.scopes})
			if err != nil {
				t.Fatalf("GenerateAccessToken() error = %v", err)
			}

			claims, err := svc.ValidateToken(token)
			if err != nil {
				t.Fatalf("ValidateToken() error = %v", err)
			}

			// Verify scopes are correctly stored
			if tt.scopes == nil {
				if claims.Scopes != nil {
					t.Errorf("Claims.Scopes = %v, want nil", claims.Scopes)
				}
			} else if len(tt.scopes) == 0 {
				// Empty map might be nil or empty after JSON round trip
				if len(claims.Scopes) != 0 {
					t.Errorf("Claims.Scopes = %v, want empty or nil", claims.Scopes)
				}
			} else {
				if len(claims.Scopes) != len(tt.scopes) {
					t.Errorf("Claims.Scopes length = %d, want %d", len(claims.Scopes), len(tt.scopes))
				}
				for resource, level := range tt.scopes {
					if claims.Scopes[resource] != level {
						t.Errorf("Claims.Scopes[%s] = %v, want %v", resource, claims.Scopes[resource], level)
					}
				}
			}
		})
	}
}

// =============================================================================
// GenerateRefreshToken Tests
// =============================================================================

func TestGenerateRefreshToken(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	userID := int64(123)
	username := "testuser"
	scopes := map[string]string{"profile": "read"}

	token, err := svc.GenerateRefreshToken(Identity{UserID: userID, Username: username, Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("Generated refresh token is empty")
	}

	// Verify token contains correct claims
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("Claims.UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Username != username {
		t.Errorf("Claims.Username = %v, want %v", claims.Username, username)
	}
}

func TestGenerateRefreshToken_WithScopes(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := svc.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser", Scopes: tt.scopes})
			if err != nil {
				t.Fatalf("GenerateRefreshToken() error = %v", err)
			}

			claims, err := svc.ValidateToken(token)
			if err != nil {
				t.Fatalf("ValidateToken() error = %v", err)
			}

			// Verify scopes are correctly stored
			if tt.scopes == nil {
				if claims.Scopes != nil {
					t.Errorf("Claims.Scopes = %v, want nil", claims.Scopes)
				}
			} else if len(tt.scopes) == 0 {
				// Empty map might be nil or empty after JSON round trip
				if len(claims.Scopes) != 0 {
					t.Errorf("Claims.Scopes = %v, want empty or nil", claims.Scopes)
				}
			} else {
				if len(claims.Scopes) != len(tt.scopes) {
					t.Errorf("Claims.Scopes length = %d, want %d", len(claims.Scopes), len(tt.scopes))
				}
				for resource, level := range tt.scopes {
					if claims.Scopes[resource] != level {
						t.Errorf("Claims.Scopes[%s] = %v, want %v", resource, claims.Scopes[resource], level)
					}
				}
			}
		})
	}
}

func TestGenerateToken_ScopesDefensiveCopy(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	originalScopes := map[string]string{"profile": "read", "projects": "edit"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: originalScopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Mutate original map after token generation
	originalScopes["profile"] = "delete"
	originalScopes["newkey"] = "read"

	// Validate token and verify claims weren't affected by mutation
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
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

func TestGenerateRefreshToken_InputValidation(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	tests := []struct {
		name     string
		userID   int64
		username string
		wantErr  error
	}{
		{
			name:     "valid inputs",
			userID:   123,
			username: "testuser",
			wantErr:  nil,
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
			userID:   123,
			username: "",
			wantErr:  ErrEmptyUsername,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := map[string]string{"profile": "read"}
			_, err := svc.GenerateRefreshToken(Identity{UserID: tt.userID, Username: tt.username, Scopes: scopes})
			if err != tt.wantErr {
				t.Errorf("GenerateRefreshToken() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ValidateToken Tests
// =============================================================================

func TestValidateToken_ValidToken(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	userID := int64(1)
	username := "testuser"
	scopes := map[string]string{"profile": "read"}

	token, err := svc.GenerateAccessToken(Identity{UserID: userID, Username: username, Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("Claims.UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Username != username {
		t.Errorf("Claims.Username = %v, want %v", claims.Username, username)
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	// Create service with very short expiry
	shortExpiry := 1 * time.Millisecond
	svc, _ := NewService(testSecret, shortExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Error("ValidateToken() should fail for expired token")
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	svc1, _ := NewService("secret1-at-least-32-chars-long-11111", testAccessExpiry, testRefreshExpiry)
	svc2, _ := NewService("secret2-at-least-32-chars-long-22222", testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate token with svc1
	token, err := svc1.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Try to validate with svc2 (different secret)
	_, err = svc2.ValidateToken(token)
	if err == nil {
		t.Error("ValidateToken() should fail for token signed with different secret")
	}
}

func TestValidateToken_MalformedToken(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

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
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:  "token with invalid parts",
			token: "header.payload",
		},
		{
			name:  "invalid base64",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.###.xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ValidateToken(tt.token)
			if err == nil {
				t.Error("ValidateToken() should fail for malformed token")
			}
		})
	}
}

func TestValidateToken_TamperedToken(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Tamper with the token by changing a character
	tamperedToken := token[:len(token)-5] + "XXXXX"

	_, err = svc.ValidateToken(tamperedToken)
	if err == nil {
		t.Error("ValidateToken() should fail for tampered token")
	}
}

func TestValidateToken_WrongSigningMethod(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	// Create a token header claiming RS256 (RSA) instead of HS256 (HMAC)
	// #nosec G101 - This is a test token, not actual credentials
	tokenString := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxLCJ1c2VybmFtZSI6InRlc3R1c2VyIiwiZXhwIjoxNzAwMDAwMDAwfQ.invalid_signature"

	_, err := svc.ValidateToken(tokenString)
	if err == nil {
		t.Error("ValidateToken() should fail for token with wrong signing method")
	}
}

func TestValidateToken_WrongSigningMethodNone(t *testing.T) {
	// Create a token with signing method "none"
	claims := &Claims{
		Identity: Identity{UserID: 123, Username: "user"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	svc, _ := NewValidatorOnly(testSecret)
	_, err := svc.ValidateToken(tokenString)

	if err == nil {
		t.Error("ValidateToken() should fail for token with 'none' signing method")
	}
}

func TestValidateToken_InvalidClaimsStructure(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate a valid token
	validToken, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
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

	_, err = svc.ValidateToken(corruptedToken)
	if err == nil {
		t.Error("ValidateToken() should fail for token with invalid claims structure")
	}
}

func TestValidateToken_ExpiryBoundary(t *testing.T) {
	// Test token validation exactly at expiry boundary
	// JWT timestamps are truncated to seconds, so use multi-second expiry
	shortExpiry := 3 * time.Second
	svc, _ := NewService(testSecret, shortExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Should be valid immediately
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims == nil {
		t.Fatal("Claims should not be nil")
	}

	// Wait 1 second - should still be valid
	time.Sleep(1 * time.Second)

	// Should still be valid
	claims, err = svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims == nil {
		t.Fatal("Claims should not be nil")
	}

	// Wait past expiry (3+ seconds total)
	time.Sleep(2500 * time.Millisecond)

	// Should now be invalid
	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Error("ValidateToken() should fail for expired token")
	}
}

func TestValidateToken_RemainingTime(t *testing.T) {
	svc, _ := NewService(testSecret, 10*time.Second, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Validate immediately and check remaining time
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	remaining := time.Until(claims.ExpiresAt.Time)

	// Should be close to 10 seconds (within 1 second tolerance)
	if remaining < 9*time.Second || remaining > 11*time.Second {
		t.Errorf("Remaining time = %v, want ~10s", remaining)
	}

	// Wait 2 seconds
	time.Sleep(2 * time.Second)

	// Check remaining time again
	claims, err = svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	remaining = time.Until(claims.ExpiresAt.Time)

	// Should be close to 8 seconds (within 1 second tolerance)
	if remaining < 7*time.Second || remaining > 9*time.Second {
		t.Errorf("Remaining time = %v, want ~8s", remaining)
	}
}

// =============================================================================
// Expiry Tests
// =============================================================================

func TestGetExpiryMethods(t *testing.T) {
	customAccess := 30 * time.Minute
	customRefresh := 720 * time.Hour

	svc, _ := NewService(testSecret, customAccess, customRefresh)

	if got := svc.GetAccessExpiry(); got != customAccess {
		t.Errorf("GetAccessExpiry() = %v, want %v", got, customAccess)
	}
	if got := svc.GetRefreshExpiry(); got != customRefresh {
		t.Errorf("GetRefreshExpiry() = %v, want %v", got, customRefresh)
	}
}

func TestAccessTokenExpiry(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	// Calculate expected expiry
	expectedExpiry := claims.IssuedAt.Add(testAccessExpiry)
	actualExpiry := claims.ExpiresAt.Time

	// Should be within 1 second due to timing
	diff := actualExpiry.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("Expiry difference = %v, want within 1 second", diff)
	}
}

func TestRefreshTokenExpiry(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
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
	accessToken, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	accessClaims, err := svc.ValidateToken(accessToken)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if !claims.ExpiresAt.After(accessClaims.ExpiresAt.Time) {
		t.Error("Refresh token should expire after access token")
	}
}

func TestVeryShortExpiry(t *testing.T) {
	svc, _ := NewService(testSecret, 1*time.Nanosecond, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Token should be expired almost immediately due to JWT second precision
	time.Sleep(100 * time.Millisecond)

	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Error("ValidateToken() should fail for token with nanosecond expiry")
	}
}

func TestVeryLongExpiry(t *testing.T) {
	longExpiry := 8760 * time.Hour // 1 year
	svc, _ := NewService(testSecret, longExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	// Verify expiry is approximately 1 year from now
	expectedExpiry := claims.IssuedAt.Add(longExpiry)
	diff := claims.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("Expiry difference = %v, want within 1 second", diff)
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
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	concurrency := 10
	done := make(chan bool, concurrency)
	tokens := make(chan string, concurrency)

	// Generate tokens concurrently
	for i := range concurrency {
		go func(userID int64) {
			scopes := map[string]string{"profile": "read"}
			token, err := svc.GenerateAccessToken(Identity{UserID: userID, Username: "testuser", Scopes: scopes})
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
			t.Errorf("Duplicate token generated: %s", token)
		}
		seen[token] = true

		claims, err := svc.ValidateToken(token)
		if err != nil {
			t.Errorf("ValidateToken() error = %v", err)
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
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	scopes := map[string]string{"profile": "read"}
	// Generate a single token
	token, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: scopes})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	concurrency := 20
	done := make(chan bool, concurrency)
	errors := make(chan error, concurrency)

	// Validate token concurrently
	for range concurrency {
		go func() {
			claims, err := svc.ValidateToken(token)
			if err != nil {
				errors <- err
			} else if claims == nil {
				errors <- jwt.ErrTokenInvalidClaims
			} else if claims.UserID != 1 {
				errors <- jwt.ErrTokenInvalidClaims
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for range concurrency {
		<-done
	}
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent validation error: %v", err)
	}
}

func TestGenerateToken_TokenType(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	access, err := svc.GenerateAccessToken(Identity{UserID: 1, Username: "testuser", Scopes: nil})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	accessClaims, err := svc.ValidateToken(access)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if accessClaims.TokenType != TokenTypeAccess {
		t.Errorf("access TokenType = %q, want %q", accessClaims.TokenType, TokenTypeAccess)
	}

	refresh, err := svc.GenerateRefreshToken(Identity{UserID: 1, Username: "testuser", Scopes: nil})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}
	refreshClaims, err := svc.ValidateToken(refresh)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if refreshClaims.TokenType != TokenTypeRefresh {
		t.Errorf("refresh TokenType = %q, want %q", refreshClaims.TokenType, TokenTypeRefresh)
	}
}

func TestGenerateAccessToken_ProfileClaims(t *testing.T) {
	svc, _ := NewService(testSecret, testAccessExpiry, testRefreshExpiry)

	token, err := svc.GenerateAccessToken(Identity{
		UserID:        7,
		Username:      "kaladin",
		Email:         "kal@example.com",
		EmailVerified: true,
		DisplayName:   "Kaladin Stormblessed",
		Scopes:        map[string]string{"heroes": "edit"},
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.Email != "kal@example.com" || !claims.EmailVerified || claims.DisplayName != "Kaladin Stormblessed" {
		t.Errorf("profile claims = %q/%v/%q", claims.Email, claims.EmailVerified, claims.DisplayName)
	}
}
