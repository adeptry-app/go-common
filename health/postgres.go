package health

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PostgresChecker checks PostgreSQL database connectivity
type PostgresChecker struct {
	db *gorm.DB
}

// NewPostgresChecker creates a new PostgreSQL health checker
func NewPostgresChecker(db *gorm.DB) Checker {
	return &PostgresChecker{db: db}
}

// Name returns the name of this checker
func (c *PostgresChecker) Name() string {
	return "postgres"
}

// Check verifies the database connection is alive
func (c *PostgresChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	if c.db == nil {
		return Unhealthy(start, "database is nil")
	}

	sqlDB, err := c.db.DB()
	if err != nil {
		return Unhealthy(start, "failed to get database instance: %v", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return Unhealthy(start, "ping failed: %v", err)
	}

	return Healthy(start)
}
