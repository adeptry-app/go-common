# middleware

Gin middleware for authentication and security.

## AuthMiddleware

Local JWT token validation with automatic TTL handling:

```go
import (
    "log"

    "github.com/adeptry-app/go-common/config"
    "github.com/adeptry-app/go-common/jwt"
    "github.com/adeptry-app/go-common/middleware"
)

// A Verifier, not an Issuer, so a service behind this middleware cannot mint tokens.
verifier, err := jwt.NewVerifier(config.GetEnvRequired("JWT_PUBLIC_KEYS"), jwt.AudiencePublicAPI)
if err != nil {
    log.Fatalf("jwt verifier: %v", err)
}
authMiddleware := middleware.NewAuthMiddleware(verifier)

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

A request carrying a non-empty, unlisted `Origin` is aborted with 403 before the
handler, rather than running it and merely having the response hidden by the
browser. That covers the browser cases, not every cross-site request: a caller
that omits `Origin` still reaches the handler, so cookie-authenticated services
layer their own CSRF middleware on top. `OPTIONS` is the one exception - it is
answered before that passthrough, so a preflight with no `Origin` gets 403.

## Timeout

Bounds a request end to end. Handlers pass the request context down to the pool,
so the deadline cancels the in-flight query instead of orphaning it:

```go
v1.Use(middleware.Timeout(cfg.RequestTimeout))
```

Pair it with the two server-side halves, or the deadline is only advisory:

All three read the one field, `config.ServiceConfig.RequestTimeout`
(`REQUEST_TIMEOUT`), so they cannot disagree:

```go
v1.Use(middleware.Timeout(cfg.Service.RequestTimeout))

database.NewPgxPool(ctx, cfg.Database, "svc",
    database.WithStatementTimeout(cfg.Service.RequestTimeout))

serverCfg := server.DefaultConfig(port)
serverCfg.RequestTimeout = cfg.Service.RequestTimeout
```

`statement_timeout` stops a query the caller already abandoned from holding a
connection; `server.Config.RequestTimeout` derives the socket deadlines from the
handler deadline, so raising it past the 30s default does not truncate the
response mid-body.

A non-positive duration disables each of the three rather than expiring
everything instantly, so a misread config degrades to "no deadline".

## BodyLimit

Caps how many bytes a handler may read from a request body:

```go
v1.Use(middleware.BodyLimit(cfg.MaxBodySize))
```

Register it before anything that binds or parses, so an oversized body is
refused while it streams instead of after it is buffered. The reader returns an
error the handler's existing bind error path already reports, so the middleware
writes no response of its own.
