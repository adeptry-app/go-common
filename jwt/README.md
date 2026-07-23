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
ttl := claims.GetTTL()

// Generate tokens with scopes (full service only)
scopes := map[string]string{
    "profile": "read",
    "projects": "edit",
    "users": "delete",
}
accessToken, err := jwtService.GenerateAccessToken(userID, username, scopes)
refreshToken, err := jwtService.GenerateRefreshToken(userID, username, scopes)

// Generate tokens without scopes
accessToken, err := jwtService.GenerateAccessToken(userID, username, nil)
refreshToken, err := jwtService.GenerateRefreshToken(userID, username, nil)
```

## Benefits

- No network calls (faster, more resilient)
- No single point of failure
- Industry standard approach
