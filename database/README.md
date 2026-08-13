# database

PostgreSQL connection pooling for pgx.

## pgx Usage

Builds the pool from the shared `config.DatabaseConfig` and verifies
connectivity with a ping. The appName argument is reported as the PostgreSQL
`application_name`.

```go
import (
    "log"

    "github.com/adeptry-app/go-common/database"
)

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

The actor is typed and constructed, never assembled field by field: the
database resolves the username from the id, so a caller cannot pair one with a
name it does not own.

```go
auth := database.UserActor(42, ip, ua) // AnonymousActor() for a route with no session

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

- `NewPgxPool(ctx, cfg, appName, opts...)` - Create a pgx connection pool
  from `config.DatabaseConfig`
- `WithPoolSize(maxConns, minConns int32) PgxPoolOption` - Set pool sizing
- `WithStatementTimeout(d time.Duration) PgxPoolOption` - Cap one statement server
  side; pair with a matching handler deadline. Rides the startup packet, so it
  costs no round trip. Non-positive leaves the server default
- `UserActor(userID, clientIP, userAgent) AuthContext` - Actor for an
  authenticated request
- `AnonymousActor() AuthContext` - Actor for a route served without a session
- `CallJSON` / `CallBool` / `CallDiscard` / `CallInto` - Audited single-row
  SQL function calls (see above)
