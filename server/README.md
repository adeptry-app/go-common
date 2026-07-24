# server

HTTP server utilities with graceful shutdown.

## Usage

```go
import (
    "log"

    "github.com/adeptry-app/go-common/server"
)

cfg := server.DefaultConfig("8080")
if err := server.Run(router, cfg, logger); err != nil {
    log.Fatal(err)
}

// With cleanup function for resource cleanup
server.RunWithCleanup(router, cfg, logger, func() {
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

- Graceful shutdown on SIGINT/SIGTERM
- Configurable timeouts with sensible defaults
- Structured logging integration
- Optional cleanup function for resource release
