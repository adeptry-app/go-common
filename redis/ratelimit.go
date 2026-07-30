package redis

import (
	"context"
	"fmt"
	"math"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// bumpScript increments and arms the window atomically, re-arming a lost TTL,
// and reports the seconds left so a caller can send an exact Retry-After.
var bumpScript = goredis.NewScript(`
local count = redis.call('INCR', KEYS[1])
local ttl = redis.call('TTL', KEYS[1])
if count == 1 or ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

// refundScript decrements only a bucket that still exists.
var refundScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
  return redis.call('DECR', KEYS[1])
end
return 0
`)

// Bump charges one unit against the window, returning the new count and the
// seconds left before the bucket resets.
func Bump(ctx context.Context, client *goredis.Client, key string, window time.Duration) (count, ttlSeconds int64, err error) {
	res, err := bumpScript.Run(ctx, client, []string{key}, WindowSeconds(window)).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(res) != 2 {
		return 0, 0, fmt.Errorf("bump %q: got %d values, want count and ttl", key, len(res))
	}
	return res[0], res[1], nil
}

// Refund returns one unit to a bucket charged up front.
func Refund(ctx context.Context, client *goredis.Client, key string) error {
	return refundScript.Run(ctx, client, []string{key}).Err()
}

// WindowSeconds rounds a window up to at least one second: EXPIRE 0 deletes the
// key, so a sub-second window would silently turn the limiter off.
func WindowSeconds(window time.Duration) int64 {
	return max(1, int64(math.Ceil(window.Seconds())))
}
