# ratelimit

Gin middleware capping how often a caller may hit a route group, over the
counter in [redis](../redis/). Fails open on a Redis error: a limiter is a
mitigation, not an access control.

## Usage

```go
import "github.com/adeptry-app/go-common/ratelimit"

// Every request counts.
g.Use(ratelimit.ByIP(client, "svc:signup:ip:", 10, time.Minute))

// Failures count, successes are refunded.
g.Use(ratelimit.ByIPAttempt(client, "svc:login:ip:", 10, time.Minute))

// Per authenticated user; 401s an unauthenticated request.
g.Use(ratelimit.ByUser(client, "svc:ai:", 20, time.Minute))
```

`ByIP` and `ByIPAttempt` key on `c.ClientIP()`, which is only as trustworthy as
`ServiceConfig.TrustedProxies` - see [server](../server/).

Rejections carry `Retry-After`, taken from the bucket's own TTL.
