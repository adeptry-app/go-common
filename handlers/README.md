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

## Postgres-backed CRUD

For services whose handlers are "authenticate, resolve path parameters, call one
database routine, render its JSONB verbatim". The repository method takes the
`database.AuthContext` so the routine can set audit context, and returns raw
JSONB because the database owns the response shape.

```go
func (h *Handler) GetHero(c *gin.Context) {
    handlers.HandleGetByID(c, "id", h.repo.GetHero)
}
func (h *Handler) DeleteHero(c *gin.Context) {
    handlers.HandleDelete(c, "id", h.repo.DeleteHero)
}

// The URL, not the body, decides which row a write touches. A parameter absent
// from the route is skipped, so one handler serves POST (create) and PUT /:id.
func (h *Handler) UpsertHero(c *gin.Context) {
    handlers.HandlePost(c, h.repo.UpsertHero,
        handlers.PathID{Field: "id", Param: "id"})
}
func (h *Handler) AddFavorite(c *gin.Context) {
    handlers.HandlePost(c, h.repo.AddFavorite,
        handlers.PathID{Field: "heroId", Param: "id"})
}
```

`HandleGet`, `HandleGetByID`, `HandleGetByString`, `HandleGetByTwoIDs`,
`HandlePost`, `HandleDelete`, `HandleDeleteByString`, `HandleDeleteByTwoIDs` and
`HandleDeleteCommon` cover the shapes; a NULL JSONB result becomes 404.

`HandlePgxError` maps pgx/SQLSTATE to status codes - a `P0001` message reaches
the client verbatim (that is the contract for business rules raised in SQL),
everything else is logged behind a fixed message. `PgErrorResponse` is the same
mapping as a pure function, for callers that render errors themselves.

`HandleRepositoryError` shares that mapping for repositories that word their
own not-found and internal messages.

### Post-commit work

Cache invalidation and object cleanup run after the row is committed, so they
must not die with the request:

```go
ctx, cancel := handlers.DetachedContext(c.Request.Context())
defer cancel()
```

### Primitives

`ReadBody`, `ReadJSONBody`, `PathParamInt64`, `MergeJSONObject`, `BindPathIDs`,
`IsNullJSON`, `RequireAuth`, `AuthContextFrom`.
