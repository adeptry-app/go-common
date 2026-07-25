# jwt

EdDSA (Ed25519) token issuing and local validation.

Only the auth service holds a private key and builds an `Issuer`; every other
service gets a `Verifier`, so a service that can check a token cannot mint one.

## Configuration

| Variable | Read by | Value |
| -------- | ------- | ----- |
| `JWT_PRIVATE_KEY` | issuer | one `<kid>:<base64 PKCS8 DER>` entry |
| `JWT_PUBLIC_KEYS` | issuer + every verifier | comma-separated `<kid>:<base64 PKIX DER>` |
| `JWT_ACCESS_AUDIENCE` | issuer | comma-separated services the access cookie reaches |

Rotation: publish the new key to every verifier's `JWT_PUBLIC_KEYS` first, then
switch the issuer's `JWT_PRIVATE_KEY`, then drop the retired key.

## Usage

```go
import (
    "log"

    "github.com/adeptry-app/go-common/config"
    "github.com/adeptry-app/go-common/jwt"
)

// For services that only validate tokens
verifier, err := jwt.NewVerifier(config.GetEnvRequired("JWT_PUBLIC_KEYS"), jwt.AudiencePublicAPI)
if err != nil {
    log.Fatalf("jwt verifier: %v", err)
}

// For the auth service, which also mints them
cfg := config.NewJWTConfig()
issuer, err := jwt.NewIssuer(jwt.IssuerConfig{
    PrivateKey:     cfg.PrivateKey,
    PublicKeys:     cfg.PublicKeys,
    AccessAudience: cfg.AccessAudience,
    AccessExpiry:   cfg.AccessExpiry,
    RefreshExpiry:  cfg.RefreshExpiry,
})
if err != nil {
    log.Fatalf("jwt issuer: %v", err)
}

// Validate a token
claims, err := verifier.ValidateAccessToken(tokenString)
userID := claims.UserID
username := claims.Username
scopes := claims.Scopes  // map[string]string{"profile": "read", ...}
tokenType := claims.TokenType  // jwt.TokenTypeAccess or jwt.TokenTypeRefresh
ttl := claims.GetTTL()

// Generate tokens (issuer only). Scopes and the profile fields are optional.
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
accessToken, err := issuer.GenerateAccessToken(identity)
refreshToken, err := issuer.GenerateRefreshToken(identity)

// One service-to-service call, addressed to a single audience
serviceToken, err := issuer.GenerateServiceToken(identity, jwt.AudienceMessaging)
```

## Audiences

Every verifier accepts only tokens listing its own audience, so a token captured
at one service is not replayable at the next.

| Token | `aud` |
| ----- | ----- |
| Access | `auth-service` plus `JWT_ACCESS_AUDIENCE` |
| Refresh | `auth-service` |
| Service | the single audience passed to `GenerateServiceToken` |

`auth-service` is always included so a truncated `JWT_ACCESS_AUDIENCE` cannot lock
the auth service out of its own protected routes. Adding a browser-facing service
is an env change, not a release.

Refresh tokens are only valid at the token endpoint: `middleware.ValidateToken`
rejects any token whose `TokenType` is not `jwt.TokenTypeAccess`.

## Benefits

- No network calls (faster, more resilient)
- A leaked verifier key cannot mint tokens
- Key rotation without redeploying the issuer
