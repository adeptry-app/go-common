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
| [database](database/) | PostgreSQL connection pooling with pgx |
| [jwt](jwt/) | EdDSA token issuing and local validation |
| [middleware](middleware/) | Auth and security middleware for Gin |
| [audit](audit/) | Security event logging |
| [repository](repository/) | Shared repository implementations |
| [handlers](handlers/) | Common HTTP handler utilities |
| [logger](logger/) | Structured logging with slog |
| [metrics](metrics/) | Prometheus metrics collection |
| [server](server/) | Router construction and HTTP server with graceful shutdown |
| [queue](queue/) | RabbitMQ pub/sub with reconnection, retries, and DLQ |
| [redis](redis/) | Redis client and fixed-window counter |
| [session](session/) | Redis session state and access-token revocation |
| [ratelimit](ratelimit/) | Gin rate-limit middleware |
| [health](health/) | Dependency health checking |

## Quick Start

```go
import (
    "context"
    "log"
    "time"

    "github.com/adeptry-app/go-common/config"
    "github.com/adeptry-app/go-common/database"
    "github.com/adeptry-app/go-common/health"
    "github.com/adeptry-app/go-common/jwt"
    "github.com/adeptry-app/go-common/logger"
    "github.com/adeptry-app/go-common/metrics"
    "github.com/adeptry-app/go-common/middleware"
    "github.com/adeptry-app/go-common/server"
)

// Configuration
dbCfg := config.NewDatabaseConfig()
appLogger := logger.New(logger.Config{ServiceName: "example"})
metricsCollector := metrics.New(metrics.Config{ServiceName: "example"})

// Database
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

pool, err := database.NewPgxPool(ctx, dbCfg, "example")
if err != nil {
    log.Fatalf("database: %v", err)
}
defer pool.Close()

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
healthAgg.Register(health.NewPgxChecker(pool))
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
