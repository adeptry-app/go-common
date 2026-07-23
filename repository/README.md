# repository

Shared database repository implementations.

## ActionLogRepository

Audit log storage and querying:

```go
import "github.com/adeptry-app/go-common/repository"

repo := repository.NewActionLogRepository(db)

// Log an action
err := repo.LogAction(models.ActionLog{
    Action:     "login_success",
    UserID:     &userID,
    ClientIP:   &clientIP,
    UserAgent:  &userAgent,
    Details:    jsonDetails,
})

// Query logs
logs, err := repo.GetActionsByType("login_success", 100)
logs, err := repo.GetActionsByUser(userID, 50)
logs, err := repo.GetActionsByResource("file", fileID)
count, err := repo.CountActionsByResource("file", fileID)
```

Note: Prefer `audit` package helpers for logging events with automatic context extraction.
