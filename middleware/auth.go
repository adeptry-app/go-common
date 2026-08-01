package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/gin-gonic/gin"
)

// AccessTokenCookie carries the access token on browser requests. ExtractToken
// reads it; the auth service writes it.
const AccessTokenCookie = "access_token"

// HeaderTokenTTL carries the access token's remaining lifetime in seconds.
const HeaderTokenTTL = "X-Token-TTL" // #nosec G101 -- header name, not a credential

// Gin context keys for the values ValidateToken stores. Unexported so the
// accessors below are the only way in or out.
const (
	ctxKeyTokenTTL = "token_ttl"
	ctxKeyIdentity = "identity"
)

// SetIdentity marks the request as authenticated for the given subject. Only
// ValidateToken needs this in production; tests use it to stand in for a token.
func SetIdentity(c *gin.Context, id jwt.Identity) {
	c.Set(ctxKeyIdentity, id)
}

// GetIdentity returns the token subject stored by SetIdentity. ok is false when
// the auth middleware did not run. Treat Scopes as read-only; it is the stored
// map, not a copy.
func GetIdentity(c *gin.Context) (jwt.Identity, bool) {
	v, _ := c.Get(ctxKeyIdentity)
	if id, ok := v.(jwt.Identity); ok && id.UserID > 0 {
		return id, true
	}
	return jwt.Identity{}, false
}

// abortUnauthorized ends the request with the 401 envelope every service returns.
func abortUnauthorized(c *gin.Context, reason string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized - " + reason})
}

// ErrSessionRevoked marks a session that is gone or superseded, as opposed to
// one the validator could not reach. Only the former is the caller's problem.
var ErrSessionRevoked = errors.New("session revoked")

// SessionValidator answers whether the session a browser token was minted for is
// still live. Implementations read Redis or a bounded revocation cache, and
// return ErrSessionRevoked when the session is dead or the version has moved on.
type SessionValidator interface {
	ValidateSession(ctx context.Context, userID int64, session jwt.Session) error
}

// AuthMiddleware provides JWT token validation
type AuthMiddleware struct {
	verifier jwt.Verifier
	sessions SessionValidator
}

// AuthOption customizes the middleware at construction.
type AuthOption func(*AuthMiddleware)

// WithSessionValidator makes ValidateToken check each browser token against live
// session state. Without it validation is local only, so nothing a service does
// to a session takes effect before the token expires.
// Panics if v is nil: a silently disabled revocation check is worse than a boot failure.
func WithSessionValidator(v SessionValidator) AuthOption {
	if v == nil {
		panic("middleware: cannot use a nil session validator")
	}
	return func(m *AuthMiddleware) {
		m.sessions = v
	}
}

// NewAuthMiddleware creates a new auth middleware instance. It takes a Verifier,
// not an Issuer, so a service behind this middleware cannot mint tokens.
func NewAuthMiddleware(verifier jwt.Verifier, opts ...AuthOption) *AuthMiddleware {
	m := &AuthMiddleware{
		verifier: verifier,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// checkSession asks the validator about the token's session. A service token or
// a token minted before session binding carries no sid and is left alone.
func (m *AuthMiddleware) checkSession(c *gin.Context, claims *jwt.Claims) error {
	if m.sessions == nil || claims.SessionID == "" {
		return nil
	}
	return m.sessions.ValidateSession(c.Request.Context(), claims.UserID, claims.Session())
}

// ValidateToken returns a Gin middleware that validates JWT tokens locally
func (m *AuthMiddleware) ValidateToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ExtractToken(c)
		if token == "" {
			abortUnauthorized(c, "no token provided")
			return
		}

		// Validate locally; refresh tokens are only valid at the token endpoint
		claims, err := m.verifier.ValidateAccessToken(token)
		if err != nil {
			slog.Warn("token validation failed",
				"error", err,
				"path", c.Request.URL.Path,
			)
			abortUnauthorized(c, "invalid token")
			return
		}

		// Get TTL from claims
		ttl := claims.GetTTL()
		if ttl <= 0 {
			slog.Warn("token expired",
				"path", c.Request.URL.Path,
			)
			abortUnauthorized(c, "token expired")
			return
		}

		if err := m.checkSession(c, claims); err != nil {
			if errors.Is(err, ErrSessionRevoked) {
				abortUnauthorized(c, "session revoked")
				return
			}
			// Failing open here would make every revocation advisory.
			slog.Error("session validation unavailable",
				"error", err,
				"path", c.Request.URL.Path,
			)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "session validation unavailable"})
			return
		}

		// Store TTL and the token subject for downstream handlers
		c.Set(ctxKeyTokenTTL, ttl)
		SetIdentity(c, claims.Identity)

		c.Next()
	}
}

// AddTTLHeader returns a middleware that adds X-Token-TTL header to responses.
// Must be added after ValidateToken; the header is written before the handler
// runs because gin flushes the header map on the first body write.
func (m *AuthMiddleware) AddTTLHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, _ := c.Get(ctxKeyTokenTTL)
		if ttl, ok := v.(int64); ok && ttl > 0 {
			c.Header(HeaderTokenTTL, strconv.FormatInt(ttl, 10))
		}
		c.Next()
	}
}

// ExtractToken returns the caller's raw JWT, preferring the access token cookie
// over the Authorization header. Also used to forward the token between services.
func ExtractToken(c *gin.Context) string {
	// Try cookie first (browser requests)
	if cookie, err := c.Cookie(AccessTokenCookie); err == nil && cookie != "" {
		return cookie
	}

	// Fallback to Authorization header (service-to-service calls)
	bearerToken := c.GetHeader("Authorization")
	parts := strings.Split(bearerToken, " ")
	if len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}
