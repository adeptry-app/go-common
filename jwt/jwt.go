// Package jwt issues and verifies EdDSA-signed JWTs. Only the auth service holds
// a private key; every other service gets a Verifier, so a service that can check
// a token still cannot mint one.
package jwt

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token types distinguish what a token may be used for. Refresh tokens are only
// valid at the token endpoint; everything else requires an access token.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// IssuerName is the `iss` every token carries and every verifier requires.
const IssuerName = "adeptry-auth"

// Audiences name the service a token may be presented to, so a token captured
// at one service is not replayable at the next.
const (
	AudienceAuth      = "auth-service"
	AudiencePublicAPI = "public-api"
	AudienceFilesAPI  = "files-api"
	AudienceMessaging = "messaging-api"
)

// signingMethod is the only algorithm accepted, on both the signing and the
// verifying side. Pinning it is what stops an algorithm-confusion forgery.
const signingMethod = "EdDSA"

// Common errors returned by this package.
var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrInvalidUserID  = errors.New("user ID must be positive")
	ErrEmptyUsername  = errors.New("username cannot be empty")
	ErrEmptyAudience  = errors.New("audience cannot be empty")
	ErrWrongTokenType = errors.New("wrong token type")
	ErrUnknownKeyID   = errors.New("unknown key id")
	ErrDuplicateKeyID = errors.New("duplicate key id")
	ErrKeyMismatch    = errors.New("private key does not match the public key of the same id")
	ErrMalformedKey   = errors.New(`key must be formatted as "<kid>:<base64 DER>"`)
	ErrNoPublicKeys   = errors.New("no public keys configured")
	ErrInvalidExpiry  = errors.New("token expiry must be positive")
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

// Verifier checks tokens minted by the auth service; it holds public keys only.
// There is deliberately no untyped validator: a caller that does not say which
// kind it expects is one refactor away from accepting a refresh token as a session.
type Verifier interface {
	ValidateAccessToken(tokenString string) (*Claims, error)
	ValidateRefreshToken(tokenString string) (*Claims, error)
}

// Issuer mints tokens as well as verifying them. Only the auth service builds
// one; everywhere else the Verifier interface makes minting a compile error.
type Issuer interface {
	Verifier
	GenerateAccessToken(id Identity) (string, error)
	GenerateRefreshToken(id Identity) (string, error)
	GenerateServiceToken(id Identity, audience string) (string, error)
	GetAccessExpiry() time.Duration
	GetRefreshExpiry() time.Duration
}

type verifier struct {
	parser     *jwt.Parser
	publicKeys map[string]ed25519.PublicKey
}

type issuer struct {
	verifier
	keyID          string
	privateKey     ed25519.PrivateKey
	accessAudience []string
	accessExpiry   time.Duration
	refreshExpiry  time.Duration
}

// IssuerConfig is the signing service's configuration. PrivateKey is a single
// "<kid>:<base64 PKCS8 DER>" entry; PublicKeys is the comma-separated list.
type IssuerConfig struct {
	PrivateKey     string
	PublicKeys     string
	AccessAudience []string
	AccessExpiry   time.Duration
	RefreshExpiry  time.Duration
}

// NewVerifier creates a validation-only service for the given audience.
// publicKeys is a comma-separated list of "<kid>:<base64 PKIX DER>" Ed25519 keys.
func NewVerifier(publicKeys, audience string) (Verifier, error) {
	// Returning newVerifier directly would hand back a non-nil interface
	// wrapping a nil *verifier on failure, deferring the panic to a request.
	v, err := newVerifier(publicKeys, audience)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// NewIssuer creates the signing service. The private key must appear, matched, in
// PublicKeys so a mispaired config fails at boot rather than at first login.
func NewIssuer(cfg IssuerConfig) (Issuer, error) {
	if cfg.AccessExpiry <= 0 || cfg.RefreshExpiry <= 0 {
		return nil, ErrInvalidExpiry
	}

	audience, err := accessAudience(cfg.AccessAudience)
	if err != nil {
		return nil, err
	}

	v, err := newVerifier(cfg.PublicKeys, AudienceAuth)
	if err != nil {
		return nil, err
	}

	kid, key, err := parsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}

	pub, ok := v.publicKeys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: private key %q is not in the public key list", ErrUnknownKeyID, kid)
	}
	if !key.Public().(ed25519.PublicKey).Equal(pub) {
		return nil, fmt.Errorf("%w: %q", ErrKeyMismatch, kid)
	}

	return &issuer{
		verifier:       *v,
		keyID:          kid,
		privateKey:     key,
		accessAudience: audience,
		accessExpiry:   cfg.AccessExpiry,
		refreshExpiry:  cfg.RefreshExpiry,
	}, nil
}

// accessAudience always leads with AudienceAuth: a truncated config must not lock
// the auth service out of validating tokens on its own protected routes.
func accessAudience(configured []string) ([]string, error) {
	if len(configured) == 0 {
		return nil, ErrEmptyAudience
	}

	audience := []string{AudienceAuth}
	for _, entry := range configured {
		if entry == "" {
			return nil, ErrEmptyAudience
		}
		if !slices.Contains(audience, entry) {
			audience = append(audience, entry)
		}
	}
	return audience, nil
}

func newVerifier(publicKeys, audience string) (*verifier, error) {
	if audience == "" {
		return nil, ErrEmptyAudience
	}

	keys, err := parsePublicKeys(publicKeys)
	if err != nil {
		return nil, err
	}

	return &verifier{
		publicKeys: keys,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{signingMethod}),
			jwt.WithIssuer(IssuerName),
			jwt.WithAudience(audience),
			jwt.WithExpirationRequired(),
		),
	}, nil
}

// validateToken checks a token's signature, issuer, audience and expiry. The
// token type is checked by validateTyped, the only caller.
func (v *verifier) validateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := v.parser.ParseWithClaims(tokenString, claims, v.keyFor)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ValidateAccessToken parses a token and rejects anything but an access token.
// A refresh token accepted as a session would be a long-lived credential.
func (v *verifier) ValidateAccessToken(tokenString string) (*Claims, error) {
	return v.validateTyped(tokenString, TokenTypeAccess)
}

// ValidateRefreshToken parses a token and rejects anything but a refresh token.
func (v *verifier) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return v.validateTyped(tokenString, TokenTypeRefresh)
}

func (v *verifier) validateTyped(tokenString, want string) (*Claims, error) {
	claims, err := v.validateToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != want {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrWrongTokenType, claims.TokenType, want)
	}
	return claims, nil
}

// keyFor resolves the key named by the token's `kid` header. A missing or unknown
// kid fails rather than falling back to trying every configured key.
func (v *verifier) keyFor(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	key, ok := v.publicKeys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, kid)
	}
	return key, nil
}

// GenerateAccessToken creates a short-lived access token addressed to every
// service the browser presents the cookie to.
func (i *issuer) GenerateAccessToken(id Identity) (string, error) {
	return i.generateToken(id, i.accessAudience, i.accessExpiry, TokenTypeAccess)
}

// GenerateRefreshToken creates a long-lived refresh token usable only at the
// auth service.
func (i *issuer) GenerateRefreshToken(id Identity) (string, error) {
	return i.generateToken(id, []string{AudienceAuth}, i.refreshExpiry, TokenTypeRefresh)
}

// GenerateServiceToken creates an access token for one service-to-service call.
// The single audience keeps it from being replayed against another service.
func (i *issuer) GenerateServiceToken(id Identity, audience string) (string, error) {
	if audience == "" {
		return "", ErrEmptyAudience
	}
	return i.generateToken(id, []string{audience}, i.accessExpiry, TokenTypeAccess)
}

func (i *issuer) generateToken(id Identity, audience []string, expiry time.Duration, tokenType string) (string, error) {
	if id.UserID <= 0 {
		return "", ErrInvalidUserID
	}
	if id.Username == "" {
		return "", ErrEmptyUsername
	}

	// Defensive copy of scopes map to prevent caller modifications affecting claims
	id.Scopes = maps.Clone(id.Scopes)

	now := time.Now()
	claims := Claims{
		Identity:  id,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    IssuerName,
			Audience:  audience,
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = i.keyID
	return token.SignedString(i.privateKey)
}

// GetAccessExpiry returns the configured access token expiration duration.
func (i *issuer) GetAccessExpiry() time.Duration {
	return i.accessExpiry
}

// GetRefreshExpiry returns the configured refresh token expiration duration.
func (i *issuer) GetRefreshExpiry() time.Duration {
	return i.refreshExpiry
}

// parsePrivateKey reads a "<kid>:<base64 PKCS8 DER>" Ed25519 private key.
func parsePrivateKey(s string) (string, ed25519.PrivateKey, error) {
	kid, der, err := splitKeyEntry(s)
	if err != nil {
		return "", nil, err
	}

	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return "", nil, fmt.Errorf("parse private key %q: %w", kid, err)
	}

	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return "", nil, fmt.Errorf("%w: %q", jwt.ErrNotEdPrivateKey, kid)
	}
	return kid, key, nil
}

// parsePublicKeys reads a comma-separated list of "<kid>:<base64 PKIX DER>" keys.
// The list is what makes rotation possible: publish first, then switch signing.
func parsePublicKeys(s string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)

	for entry := range strings.SplitSeq(s, ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}

		kid, der, err := splitKeyEntry(entry)
		if err != nil {
			return nil, err
		}

		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, fmt.Errorf("parse public key %q: %w", kid, err)
		}

		key, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: %q", jwt.ErrNotEdPublicKey, kid)
		}
		// A repeated kid would silently drop one key and 401 everything it signed.
		if _, dup := keys[kid]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateKeyID, kid)
		}
		keys[kid] = key
	}

	if len(keys) == 0 {
		return nil, ErrNoPublicKeys
	}
	return keys, nil
}

func splitKeyEntry(s string) (string, []byte, error) {
	kid, encoded, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || kid == "" || encoded == "" {
		return "", nil, ErrMalformedKey
	}

	der, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrMalformedKey, err)
	}
	return kid, der, nil
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
