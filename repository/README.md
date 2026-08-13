# repository

Shared database repository implementations.

## ActionLogRepository

Audit log storage:

```go
import (
    "github.com/adeptry-app/go-common/audit"
    "github.com/adeptry-app/go-common/repository"
)

repo := repository.NewActionLogRepository(pool)

err := repo.LogAction(ctx, &repository.ActionLog{
    ActionType: audit.ActionLoginSuccess,
    UserID:     &userID,
    IPAddress:  &clientIP,
    UserAgent:  &userAgent,
    Source:     &source,
    Metadata:   jsonMetadata,
})
```

The entry is written by `audit.log_action`, which returns nothing: a trail
entry must never be the reason a business operation reports failure.

Note: Prefer `audit` package helpers for logging events with automatic context extraction.
