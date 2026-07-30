# Go Common

![CI](https://github.com/adeptry-app/go-common/workflows/CI/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/adeptry-app/go-common)](https://goreportcard.com/report/github.com/adeptry-app/go-common)
[![codecov](https://codecov.io/gh/adeptry-app/go-common/graph/badge.svg)](https://codecov.io/gh/adeptry-app/go-common)
[![CodeRabbit](https://img.shields.io/coderabbit/prs/github/adeptry-app/go-common?label=CodeRabbit&color=2ea44f)](https://coderabbit.ai)

Shared Go package for common code across microservices.

## Prerequisites

- Go 1.26+
- Node.js 22+ and npm 11+

## Packages

| Package | Description |
| ------- | ----------- |
| [config](config/) | Configuration management and environment helpers |
| [database](database/) | PostgreSQL connection with GORM |
| [jwt](jwt/) | EdDSA token issuing and local validation |
| [middleware](middleware/) | Auth and security middleware for Gin |
| [models](models/) | Shared GORM database models |
| [audit](audit/) | Security event logging |
| [repository](repository/) | Shared repository implementations |
| [handlers](handlers/) | Common HTTP handler utilities |
| [logger](logger/) | Structured logging with slog |
| [metrics](metrics/) | Prometheus metrics collection |
| [server](server/) | Router construction and HTTP server with graceful shutdown |
| [queue](queue/) | RabbitMQ pub/sub with reconnection, retries, and DLQ |
| [redis](redis/) | Redis client and fixed-window counter |
| [ratelimit](ratelimit/) | Gin rate-limit middleware |
| [health](health/) | Dependency health checking |

## Quick Start

```go
import (
    "log"
    "time"

    "github.com/adeptry-app/go-common/config"
    "github.com/adeptry-app/go-common/database"
    "github.com/adeptry-app/go-common/health"
    "github.com/adeptry-app/go-common/jwt"
    "github.com/adeptry-app/go-common/middleware"
    "github.com/adeptry-app/go-common/server"
)

// Configuration
dbCfg := config.NewDatabaseConfig()

// Database
db, err := database.Connect(dbCfg)
if err != nil {
    log.Fatalf("database: %v", err)
}
defer func() {
    if err := database.CloseDB(db); err != nil {
        log.Printf("failed to close database: %v", err)
    }
}()

// Auth middleware. Only the auth service builds a jwt.Issuer; every other
// service takes public keys and its own audience, so it cannot mint tokens.
verifier, err := jwt.NewVerifier(config.GetEnvRequired("JWT_PUBLIC_KEYS"), jwt.AudiencePublicAPI)
if err != nil {
    log.Fatalf("jwt verifier: %v", err)
}
authMiddleware := middleware.NewAuthMiddleware(verifier)

// Router: trusted proxies, recovery, request logging, metrics and CORS
router, err := server.NewRouter(config.NewServiceConfig(8080), appLogger, metricsCollector)
if err != nil {
    log.Fatalf("router: %v", err)
}

// Health checks
healthAgg := health.NewAggregator(3 * time.Second)
healthAgg.Register(health.NewPostgresChecker(db))
router.GET("/health", healthAgg.Handler())
```

## Development

```bash
task ci:all              # Run all CI checks
task test                # Run tests
task lint                # Run linter
task format              # Format code
```

## Services Using This Module

- `auth-service` - Authentication and sessions
- `files-api` - File upload/download with MinIO
- `messaging-api` - Contact form and message queue
- `messaging-service` - Email delivery worker
- `public-api` - RPG backend API
- `ai-service` - AI homebrew queue worker

## License

[MIT](LICENSE)
