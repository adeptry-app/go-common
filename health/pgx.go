package health

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxChecker checks PostgreSQL connectivity through a pgx connection pool
type PgxChecker struct {
	pool *pgxpool.Pool
}

// NewPgxChecker creates a PostgreSQL health checker for a pgx pool. It
// reports under the same "postgres" name as the GORM checker, so register
// one or the other per service, not both.
func NewPgxChecker(pool *pgxpool.Pool) Checker {
	return &PgxChecker{pool: pool}
}

// Name returns the name of this checker
func (c *PgxChecker) Name() string {
	return "postgres"
}

// Check verifies the database connection is alive
func (c *PgxChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	if c.pool == nil {
		return Unhealthy(start, "pool is nil")
	}

	if err := c.pool.Ping(ctx); err != nil {
		return Unhealthy(start, "ping failed: %v", err)
	}

	return Healthy(start)
}
