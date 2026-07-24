# models

Shared GORM database models.

## Schema Domains

### storage.*

- `StorageFile` - File metadata for MinIO storage

## Usage

```go
import "github.com/adeptry-app/go-common/models"

var file models.StorageFile
db.First(&file, id)
```

Messaging models live in the messaging repos, not here.
