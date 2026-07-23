# audit

Centralized security event logging with automatic context extraction.

## Usage

```go
import "github.com/adeptry-app/go-common/audit"

// 1. Add context middleware
router.Use(audit.ContextMiddleware())

// 2. Log events in handlers
audit.LogFromContext(c, actionLogRepo, audit.ActionLoginSuccess, nil, nil,
    map[string]interface{}{"username": username})
```

Automatically extracts: Client IP, User-Agent, user_id from context.

## Action Types

```go
audit.ActionLoginSuccess
audit.ActionLoginFailure
audit.ActionLogout
audit.ActionTokenRefresh
audit.ActionTokenValidation
audit.ActionFileUpload
audit.ActionFileDownload
audit.ActionFileDelete
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
