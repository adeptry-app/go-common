package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/adeptry-app/go-common/jwt"
	"github.com/gin-gonic/gin"
)

// Gin context keys for the values ValidateToken stores.
const (
	CtxKeyTokenTTL    = "token_ttl"
	CtxKeyUserID      = "user_id"
	CtxKeyUsername    = "username"
	CtxKeyDisplayName = "display_name"
	CtxKeyScopes      = "scopes"
)

// Claims is the session identity ValidateToken stores on the gin context.
// Treat Scopes as read-only; it is the stored map, not a copy.
type Claims struct {
	UserID      int64
	Username    string
	DisplayName string
	Scopes      map[string]string
}

// GetClaims returns the identity stored by ValidateToken. ok is false when
// the auth middleware did not run.
func GetClaims(c *gin.Context) (Claims, bool) {
	v, exists := c.Get(CtxKeyUserID)
	uid, ok := v.(int64)
	if !exists || !ok {
		return Claims{}, false
	}
	return Claims{
		UserID:      uid,
		Username:    c.GetString(CtxKeyUsername),
		DisplayName: c.GetString(CtxKeyDisplayName),
		Scopes:      c.GetStringMapString(CtxKeyScopes),
	}, true
}

// AuthMiddleware provides JWT token validation
type AuthMiddleware struct {
	jwtService jwt.Service
}

// NewAuthMiddleware creates a new auth middleware instance with JWT service
func NewAuthMiddleware(jwtService jwt.Service) *AuthMiddleware {
	return &AuthMiddleware{
		jwtService: jwtService,
	}
}

// ValidateToken returns a Gin middleware that validates JWT tokens locally
func (m *AuthMiddleware) ValidateToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ExtractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized - no token provided"})
			c.Abort()
			return
		}

		// Validate token locally
		claims, err := m.jwtService.ValidateToken(token)
		if err != nil {
			slog.Warn("token validation failed",
				"error", err,
				"path", c.Request.URL.Path,
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized - invalid token"})
			c.Abort()
			return
		}

		// Refresh tokens are only valid at the auth service token endpoint
		if claims.TokenType != jwt.TokenTypeAccess {
			slog.Warn("rejected non-access token",
				"token_type", claims.TokenType,
				"path", c.Request.URL.Path,
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized - invalid token"})
			c.Abort()
			return
		}

		// Get TTL from claims
		ttl := claims.GetTTL()
		if ttl <= 0 {
			slog.Warn("token expired",
				"path", c.Request.URL.Path,
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized - token expired"})
			c.Abort()
			return
		}

		// Store TTL and user claims in context for downstream handlers
		c.Set(CtxKeyTokenTTL, ttl)
		c.Set(CtxKeyUserID, claims.UserID)
		c.Set(CtxKeyUsername, claims.Username)
		c.Set(CtxKeyDisplayName, claims.DisplayName)
		c.Set(CtxKeyScopes, claims.Scopes)

		c.Next()
	}
}

// AddTTLHeader returns a middleware that adds X-Token-TTL header to responses
// This should be added after ValidateToken middleware
func (m *AuthMiddleware) AddTTLHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// After request processing, add TTL header if available in context
		if ttl, exists := c.Get(CtxKeyTokenTTL); exists {
			if ttlValue, ok := ttl.(int64); ok && ttlValue > 0 {
				c.Header("X-Token-TTL", fmt.Sprintf("%d", ttlValue))
			}
		}
	}
}

// ExtractToken returns the caller's raw JWT, preferring the access token cookie
// over the Authorization header. Also used to forward the token between services.
func ExtractToken(c *gin.Context) string {
	// Try cookie first (browser requests)
	if cookie, err := c.Cookie("access_token"); err == nil && cookie != "" {
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
