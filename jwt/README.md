# jwt

Local JWT token validation and generation using HS256 signing.

## Usage

```go
import "github.com/adeptry-app/go-common/jwt"

// For services that only validate tokens
jwtService, err := jwt.NewValidatorOnly(secret)

// For services that generate and validate tokens
jwtService, err := jwt.NewService(secret, 15*time.Minute, 168*time.Hour)

// Validate a token
claims, err := jwtService.ValidateToken(tokenString)
userID := claims.UserID
username := claims.Username
scopes := claims.Scopes  // map[string]string{"profile": "read", ...}
tokenType := claims.TokenType  // jwt.TokenTypeAccess or jwt.TokenTypeRefresh
ttl := claims.GetTTL()

// Generate tokens (full service only). Scopes and the profile fields are optional.
identity := jwt.Identity{
    UserID:        userID,
    Username:      username,
    Email:         email,
    EmailVerified: true,
    DisplayName:   displayName,
    Scopes: map[string]string{
        "profile":  "read",
        "projects": "edit",
    },
}
accessToken, err := jwtService.GenerateAccessToken(identity)
refreshToken, err := jwtService.GenerateRefreshToken(identity)
```

Refresh tokens are only valid at the token endpoint: `middleware.ValidateToken`
rejects any token whose `TokenType` is not `jwt.TokenTypeAccess`.

## Benefits

- No network calls (faster, more resilient)
- No single point of failure
- Industry standard approach
