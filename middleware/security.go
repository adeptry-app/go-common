package middleware

import (
	"slices"

	"github.com/gin-gonic/gin"
)

// SecurityMiddleware provides CORS validation and security headers for Gin handlers.
// Use NewSecurityMiddleware to construct with validation.
type SecurityMiddleware struct {
	// allowedOrigins is a list of exact-match allowed origin URLs (e.g., "https://example.com").
	// The Origin request header must match one of these exactly for CORS headers to be applied.
	allowedOrigins []string
	// allowedMethods is a comma-separated list of HTTP methods (e.g., "GET,POST,PUT,DELETE").
	allowedMethods string
	// allowedHeaders is a comma-separated list of allowed request headers (e.g., "Content-Type,Authorization").
	allowedHeaders string
	// allowCredentials controls whether Access-Control-Allow-Credentials header is sent.
	// Only enable when origins are specific (not "*") and you need cookie/auth support.
	allowCredentials bool
}

// NewSecurityMiddleware creates a new security middleware with CORS configuration
func NewSecurityMiddleware(allowedOrigins []string, allowedMethods string, allowedHeaders string, allowCredentials bool) *SecurityMiddleware {
	if len(allowedOrigins) == 0 {
		panic("allowedOrigins must contain at least one origin")
	}
	for _, origin := range allowedOrigins {
		if origin == "" {
			panic("allowedOrigins must not contain empty strings")
		}
	}

	return &SecurityMiddleware{
		allowedOrigins:   allowedOrigins,
		allowedMethods:   allowedMethods,
		allowedHeaders:   allowedHeaders,
		allowCredentials: allowCredentials,
	}
}

// Apply returns a Gin middleware that adds security headers and validates CORS.
// A request carrying an unlisted Origin is rejected with 403 before the handler.
func (m *SecurityMiddleware) Apply() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// CORS validation - only set headers if origin is allowed
		allowed := slices.Contains(m.allowedOrigins, origin)

		if allowed {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Methods", m.allowedMethods)
			c.Writer.Header().Set("Access-Control-Allow-Headers", m.allowedHeaders)
			c.Writer.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
			if m.allowCredentials {
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Standard security headers (applied to all requests)
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		c.Writer.Header().Set("X-XSS-Protection", "1; mode=block")
		c.Writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Handle preflight OPTIONS requests
		if c.Request.Method == "OPTIONS" {
			if allowed {
				c.AbortWithStatus(204)
			} else {
				c.AbortWithStatus(403)
			}
			return
		}

		// A browser only sends Origin cross-site or on state-changing requests,
		// so an unlisted value is never legitimate - reject before the handler.
		if origin != "" && !allowed {
			c.AbortWithStatus(403)
			return
		}

		c.Next()
	}
}
