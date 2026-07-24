# database

Database connection utilities for GORM and pgx with connection pooling.

## GORM Usage

Both entry points take the shared `config.DatabaseConfig`.

```go
import (
    "github.com/adeptry-app/go-common/config"
    "github.com/adeptry-app/go-common/database"
)

db, err := database.Connect(config.NewDatabaseConfig())
if err != nil {
    log.Fatal(err)
}
defer func() {
    if closeErr := database.CloseDB(db); closeErr != nil {
        log.Printf("failed to close database: %v", closeErr)
    }
}()
```

## pgx Usage

For services using pgx directly instead of GORM. Builds the pool from the
shared `config.DatabaseConfig` and verifies connectivity with a ping. The
appName argument is reported as the PostgreSQL `application_name`.

```go
import "github.com/adeptry-app/go-common/database"

pool, err := database.NewPgxPool(ctx, cfg.DatabaseConfig, "my-service")
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

// Larger pool for a busier service
pool, err = database.NewPgxPool(ctx, cfg.DatabaseConfig, "my-api",
    database.WithPoolSize(25, 5))
```

Defaults: MaxConns 10, MinConns 2, MaxConnLifetime 1h, MaxConnIdleTime 10m,
HealthCheckPeriod 30s, sslmode `disable` when the config leaves it empty.
Options receive the `*pgxpool.Config` after defaults are applied and may
change any field.

## Audited SQL function calls

For services following the "SQL functions own the logic" convention: each call
runs `SELECT audit.set_context(...)` plus the function query in one pgx batch
(one implicit transaction, one network round trip - see the `CallInto` doc
comment for the semantics).

```go
auth := database.AuthContext{
    UserID: 42, Username: "user", ClientIP: ip, UserAgent: ua,
}

row, err := database.CallJSON(ctx, pool, auth,
    "SELECT heroes.get_hero($1)", id)
deleted, err := database.CallBool(ctx, pool, auth,
    "SELECT heroes.delete_hero($1)", id)
err = database.CallDiscard(ctx, pool, auth,
    "SELECT heroes.upsert_hero_avatar($1, $2)", id, key)
```

`CallInto(ctx, pool, auth, dest, query, args...)` is the generic form for
other scan targets.

## Functions

- `Connect(cfg config.DatabaseConfig) (*gorm.DB, error)` - PostgreSQL via GORM
- `CloseDB(db *gorm.DB) error` - Close database connection
- `NewPgxPool(ctx, cfg, appName, opts...)` - Create a pgx connection pool
  from `config.DatabaseConfig`
- `WithPoolSize(maxConns, minConns int32) PgxPoolOption` - Set pool sizing
- `CallJSON` / `CallBool` / `CallDiscard` / `CallInto` - Audited single-row
  SQL function calls (see above)
