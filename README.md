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
| [jwt](jwt/) | Local JWT validation and generation |
| [middleware](middleware/) | Auth and security middleware for Gin |
| [models](models/) | Shared GORM database models |
| [audit](audit/) | Security event logging |
| [repository](repository/) | Shared repository implementations |
| [handlers](handlers/) | Common HTTP handler utilities |
| [logger](logger/) | Structured logging with slog |
| [metrics](metrics/) | Prometheus metrics collection |
| [server](server/) | HTTP server with graceful shutdown |
| [queue](queue/) | RabbitMQ pub/sub with reconnection, retries, and DLQ |
| [health](health/) | Dependency health checking |
| [renderer](renderer/) | HTML email templates and their subjects |

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
)

// Configuration
dbCfg := config.NewDatabaseConfig()
jwtCfg := config.NewJWTConfig()

// Database
db, _ := database.Connect(dbCfg)
defer func() {
    if err := database.CloseDB(db); err != nil {
        log.Printf("failed to close database: %v", err)
    }
}()

// Auth middleware
jwtService, _ := jwt.NewValidatorOnly(jwtCfg.Secret)
authMiddleware := middleware.NewAuthMiddleware(jwtService)

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

## Version

Current version: `v1.4.0`

See [CHANGELOG.md](CHANGELOG.md) for breaking changes and migration guides.

## License

[MIT](LICENSE)
