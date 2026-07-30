# redis

The shared Redis client and the fixed-window counter services rate-limit with.
A leaf package: it imports only `config` and the driver, so `ratelimit` and
`health` may both use it without a cycle.

## Usage

```go
import "github.com/adeptry-app/go-common/redis"

client, err := redis.NewClient(cfg.RedisConfig)
```

`NewClient` applies dial/read/write timeouts, pool sizing and a connectivity
check. TLS follows `cfg.TLS` (`REDIS_TLS`), so staging can enable it without
claiming to be production.

`Bump` charges the window and returns the new count with the seconds left in it;
`Refund` returns a unit to a bucket charged up front. Both are one Lua script,
so an increment can never leave a counter without a TTL.

Most services want [ratelimit](../ratelimit/) rather than these two directly.
