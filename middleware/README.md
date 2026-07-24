# middleware

Gin middleware for authentication and security.

## AuthMiddleware

Local JWT token validation with automatic TTL handling:

```go
import (
    "github.com/adeptry-app/go-common/jwt"
    "github.com/adeptry-app/go-common/middleware"
)

jwtService, _ := jwt.NewValidatorOnly(secret)
authMiddleware := middleware.NewAuthMiddleware(jwtService)

protected := router.Group("/api/v1")
protected.Use(authMiddleware.ValidateToken())
protected.Use(authMiddleware.AddTTLHeader())
```

### Token Extraction

The middleware extracts JWT tokens in the following order:

1. **Cookie** (httpOnly `access_token`, exported as
   `middleware.AccessTokenCookie`) - for browser requests
2. **Authorization header** (`Bearer <token>`) - for service-to-service calls

Writers of the cookie must use `middleware.AccessTokenCookie` so the name has a
single definition across services.

`ValidateToken` accepts access tokens only; a refresh token is rejected with
401. After validation, access the token subject in handlers:

```go
id, ok := middleware.GetIdentity(c)
// jwt.Identity: UserID, Username, Email, EmailVerified, DisplayName, Scopes
```

The gin context keys are unexported, so `SetIdentity` / `GetIdentity` are the
only way in and out. Tests that need an authenticated request call
`middleware.SetIdentity(c, jwt.Identity{UserID: 1, Scopes: ...})` rather than
setting a key by hand.

## SecurityMiddleware

CORS validation and security headers:

```go
securityMiddleware := middleware.NewSecurityMiddleware(
    allowedOrigins,  // []string
    "GET,POST,PUT,DELETE,OPTIONS",
    "Content-Type,Authorization",
    true, // allow credentials
)
router.Use(securityMiddleware.Apply())
```

Features: CORS whitelisting, preflight handling, security headers
(X-Content-Type-Options, X-Frame-Options, X-XSS-Protection).
