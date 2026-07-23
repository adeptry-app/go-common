# handlers

Common HTTP handler utilities for Gin.

## Usage

```go
import "github.com/adeptry-app/go-common/handlers"

// Error responses
handlers.RespondError(c, http.StatusBadRequest, "Invalid input")
handlers.LogAndRespondError(c, http.StatusInternalServerError, err, "Operation failed")
handlers.HandleRepositoryError(c, err, "Resource not found", "Database error")

// Location header for created resources
handlers.SetLocationHeader(c, resourceID) // Sets Location: /current/path/{id}
```
