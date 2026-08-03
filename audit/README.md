# audit

Centralized security event logging with automatic context extraction.

## Usage

```go
import "github.com/adeptry-app/go-common/audit"

// 1. Add context middleware
router.Use(audit.ContextMiddleware())

// 2. Log events in handlers
source := "auth-service"
audit.LogFromContext(c, actionLogRepo, audit.ActionLoginSuccess, nil, nil, &source,
    map[string]interface{}{"username": username})
```

Automatically extracts: Client IP, User-Agent, user_id from context. The user ID
comes from `middleware.GetIdentity`, so it is only populated on routes behind
`AuthMiddleware.ValidateToken`; elsewhere pass it explicitly with `LogAction`.

## Action Types

```go
audit.ActionLoginSuccess
audit.ActionLoginFailure
audit.ActionLogout
audit.ActionTokenRefresh
audit.ActionTokenValidation
audit.ActionTokenReuse
audit.ActionRegistrationSuccess
audit.ActionRegistrationFailure
audit.ActionFileUpload
audit.ActionFileDownload
audit.ActionFileDelete
```

Credential and identity lifecycle, so support can tell recovery from takeover.
Metadata carries the reason, never the credential:

```go
audit.ActionPasswordChangeSuccess
audit.ActionPasswordChangeFailure
audit.ActionPasswordSetSuccess
audit.ActionPasswordSetFailure
audit.ActionPasswordResetRequest
audit.ActionPasswordResetSuccess
audit.ActionPasswordResetFailure
audit.ActionEmailChangeRequest
audit.ActionEmailChangeFailure
audit.ActionEmailVerifySuccess
audit.ActionEmailVerifyFailure
audit.ActionOAuthLink
audit.ActionOAuthUnlink
audit.ActionSessionsRevoked
```

## Resource Types

```go
audit.ResourceTypeFile
audit.ResourceTypeUser
```

## Helpers

```go
clientIP := audit.GetClientIP(c)
userAgent := audit.GetUserAgent(c)
userID := audit.GetUserID(c)
```
