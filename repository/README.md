# repository

Shared database repository implementations.

## ActionLogRepository

Audit log storage:

```go
import "github.com/adeptry-app/go-common/repository"

repo := repository.NewActionLogRepository(db)

err := repo.LogAction(&repository.ActionLog{
    ActionType: audit.ActionLoginSuccess,
    UserID:     &userID,
    IPAddress:  &clientIP,
    UserAgent:  &userAgent,
    Source:     &source,
    Metadata:   jsonMetadata,
})
```

Note: Prefer `audit` package helpers for logging events with automatic context extraction.

## SafeUpdater

Updates that stay idempotent: existence is checked first, so a repeat update of
an unchanged row is not a 404.

```go
updater := repository.NewSafeUpdater(db)
err := updater.Update(ctx, &model, id)
```

`CheckRowsAffected(result)` turns a zero-row delete/update into
`gorm.ErrRecordNotFound`, and `UpdateEmailStatus` applies the messaging status
transitions (clears `last_error`, stamps `sent_at`, increments `attempts`).
