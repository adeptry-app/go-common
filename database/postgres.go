package database

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/adeptry-app/go-common/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// GORM connection pool settings. Lifetimes match the pgx pool; the sizes are
// deliberately larger, since pgx callers size their own pools per service.
const (
	maxOpenConns    = 25
	maxIdleConns    = 10
	connMaxLifetime = time.Hour
	connMaxIdleTime = 10 * time.Minute
)

// Connect establishes a PostgreSQL connection with connection pooling
func Connect(cfg config.DatabaseConfig) (*gorm.DB, error) {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	// Build DSN as a URL so credentials containing spaces or separators survive;
	// the keyword/value form ends an unquoted value at the first whitespace.
	q := url.Values{}
	q.Set("sslmode", sslMode)
	q.Set("TimeZone", "UTC")
	dsn := (&url.URL{
		Scheme:   "postgresql",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     cfg.Name,
		RawQuery: q.Encode(),
	}).String()

	// Open database connection
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connMaxIdleTime)

	return db, nil
}

// CloseDB closes the underlying database connection pool.
// Returns error if the connection cannot be closed.
//
// Example with defer and error handling:
//
//	db, err := Connect(cfg)
//	if err != nil {
//	    return err
//	}
//	defer func() {
//	    if closeErr := CloseDB(db); closeErr != nil {
//	        log.Printf("failed to close database: %v", closeErr)
//	    }
//	}()
func CloseDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}
	return sqlDB.Close()
}
