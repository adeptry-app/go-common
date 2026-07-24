# models

Shared GORM database models.

## Schema Domains

### storage.*

- `StorageFile` - File metadata for MinIO storage

### messaging.*

- `Email` - Outbound emails; statuses `pending`, `processing`, `retrying`, `sent`, `failed`
- `Recipient` - Email recipients for notifications
- `DeliveryAttempt` - Email delivery tracking

## Usage

```go
import (
    "fmt"

    "github.com/adeptry-app/go-common/models"
)

var email models.Email
db.First(&email, id)
```

Worker status writes go through `repository.MarkEmail`, not a direct update.
