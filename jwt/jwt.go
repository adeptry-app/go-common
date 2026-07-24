// Package jwt provides JWT token generation and validation for portfolio services.
package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token types distinguish what a token may be used for. Refresh tokens are only
// valid at the token endpoint; everything else requires an access token.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Common errors returned by the JWT service.
var (
	ErrSecretTooShort       = errors.New("JWT secret must be at least 32 bytes (256 bits)")
	ErrInvalidSigningMethod = errors.New("invalid signing method: expected HMAC")
	ErrInvalidToken         = errors.New("invalid token")
	ErrTokenGenDisabled     = errors.New("token generation not configured")
	ErrInvalidUserID        = errors.New("user ID must be positive")
	ErrEmptyUsername        = errors.New("username cannot be empty")
)

// Identity is the user a token is issued for. Its fields are inlined into the
// token payload by the embedding in Claims.
type Identity struct {
	UserID        int64             `json:"user_id"`
	Username      string            `json:"username"`
	Email         string            `json:"email,omitempty"`
	EmailVerified bool              `json:"email_verified,omitempty"`
	DisplayName   string            `json:"display_name,omitempty"`
	Scopes        map[string]string `json:"scopes,omitempty"`
}

// Claims represents JWT token claims with user information.
type Claims struct {
	Identity
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

// Service defines JWT token operations.
type Service interface {
	ValidateToken(tokenString string) (*Claims, error)
	GenerateAccessToken(id Identity) (string, error)
	GenerateRefreshToken(id Identity) (string, error)
	GetAccessExpiry() time.Duration
	GetRefreshExpiry() time.Duration
}

type service struct {
	secret        string
	accessExpiry  time.Duration
	refreshExpiry time.Duration
}

// NewService creates a new JWT Service for generating and validating tokens.
// Secret must be at least 32 bytes for HMAC-SHA256 security.
func NewService(secret string, accessExpiry, refreshExpiry time.Duration) (Service, error) {
	if len(secret) < 32 {
		return nil, ErrSecretTooShort
	}
	return &service{
		secret:        secret,
		accessExpiry:  accessExpiry,
		refreshExpiry: refreshExpiry,
	}, nil
}

// NewValidatorOnly creates a JWT service for validation only (no token generation).
// Use for services that only need to validate tokens (admin-api, files-api).
func NewValidatorOnly(secret string) (Service, error) {
	if len(secret) < 32 {
		return nil, ErrSecretTooShort
	}
	return &service{
		secret:        secret,
		accessExpiry:  0,
		refreshExpiry: 0,
	}, nil
}

// ValidateToken parses and validates a JWT token string.
func (s *service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSigningMethod
		}
		return []byte(s.secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

// GenerateAccessToken creates a short-lived access token for the given user.
func (s *service) GenerateAccessToken(id Identity) (string, error) {
	if s.accessExpiry == 0 {
		return "", ErrTokenGenDisabled
	}
	return s.generateToken(id, s.accessExpiry, TokenTypeAccess)
}

// GenerateRefreshToken creates a long-lived refresh token for the given user.
func (s *service) GenerateRefreshToken(id Identity) (string, error) {
	if s.refreshExpiry == 0 {
		return "", ErrTokenGenDisabled
	}
	return s.generateToken(id, s.refreshExpiry, TokenTypeRefresh)
}

func (s *service) generateToken(id Identity, expiry time.Duration, tokenType string) (string, error) {
	if id.UserID <= 0 {
		return "", ErrInvalidUserID
	}
	if id.Username == "" {
		return "", ErrEmptyUsername
	}

	// Defensive copy of scopes map to prevent caller modifications affecting claims
	if id.Scopes != nil {
		scopesCopy := make(map[string]string, len(id.Scopes))
		for k, v := range id.Scopes {
			scopesCopy[k] = v
		}
		id.Scopes = scopesCopy
	}

	now := time.Now()
	claims := Claims{
		Identity:  id,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.secret))
}

// GetAccessExpiry returns the configured access token expiration duration.
func (s *service) GetAccessExpiry() time.Duration {
	return s.accessExpiry
}

// GetRefreshExpiry returns the configured refresh token expiration duration.
func (s *service) GetRefreshExpiry() time.Duration {
	return s.refreshExpiry
}

// GetTTL returns the remaining time-to-live for the token in seconds.
// Returns 0 if the token has expired or has no expiry.
func (c *Claims) GetTTL() int64 {
	if c.ExpiresAt == nil {
		return 0
	}
	ttl := time.Until(c.ExpiresAt.Time).Seconds()
	if ttl < 0 {
		return 0
	}
	return int64(ttl)
}
