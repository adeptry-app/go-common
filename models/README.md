# models

Shared GORM database models.

## Schema Domains

### storage.*

- `StorageFile` - File metadata for MinIO storage

### messaging.*

- `Email` - Outbound emails with status tracking (`ContactMessageCreate` is the
  public submission DTO, `EmailEvent` the queue payload). Statuses are
  `pending`, `processing`, `retrying`, `sent`, `failed`; `pending` and
  `retrying` are the claimable ones.
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

if !models.ValidEmailStatus(status) {
    return fmt.Errorf("invalid email status: %q", status)
}
```
