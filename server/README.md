# server

The router every service builds, and the HTTP server that runs it with graceful
shutdown.

## Usage

```go
import (
    "log"

    "github.com/adeptry-app/go-common/server"
)

router, err := server.NewRouter(cfg.ServiceConfig, appLogger, metricsCollector)
if err != nil {
    log.Fatal(err)
}

serverCfg := server.DefaultConfig("8080")
if err := server.Run(router, serverCfg, appLogger); err != nil {
    log.Fatal(err)
}

// With cleanup function for resource cleanup
server.RunWithCleanup(router, serverCfg, appLogger, func() {
    db.Close()
    publisher.Close()
})
```

## Configuration

```go
cfg := server.Config{
    Port:            "8080",           // Listen port (default: 8080)
    ShutdownTimeout: 30 * time.Second, // Max wait for active connections
    ReadTimeout:     30 * time.Second, // Max duration for reading request
    WriteTimeout:    30 * time.Second, // Max duration for writing response
    IdleTimeout:     120 * time.Second,// Max wait for next request
}
```

## Features

- `NewRouter` applies `ServiceConfig.TrustedProxies`, panic recovery, request
  logging, metrics and CORS, and switches gin to release mode in production
- Graceful shutdown on SIGINT/SIGTERM
- Configurable timeouts with sensible defaults
- Structured logging integration
- Optional cleanup function for resource release
