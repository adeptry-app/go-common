// Package ratelimit caps how often a caller may hit a route group, on top of
// the fixed-window counter in package redis.
package ratelimit

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	"github.com/adeptry-app/go-common/handlers"
	"github.com/adeptry-app/go-common/logger"
	"github.com/adeptry-app/go-common/redis"
)

// budget bounds the limiter's own Redis calls. A limiter that fails open must
// do so quickly, or a stalled Redis parks every request for its full retry run.
const budget = 250 * time.Millisecond

// keyFunc derives the bucket a request counts against. ok is false when the
// request carries no usable identity.
type keyFunc func(c *gin.Context) (key string, ok bool)

// ByIP caps how often one client address may hit a route group. Every request
// counts, because on these routes the request itself is the cost. The address
// comes from c.ClientIP(), so it is only as trustworthy as ServiceConfig.TrustedProxies.
func ByIP(client *goredis.Client, prefix string, max int64, window time.Duration) gin.HandlerFunc {
	return enforce(client, max, window, false, byIP(prefix))
}

// ByIPAttempt caps failed attempts from one address, refunding anything that
// succeeds so a NAT'd office cannot exhaust the budget by logging in.
func ByIPAttempt(client *goredis.Client, prefix string, max int64, window time.Duration) gin.HandlerFunc {
	return enforce(client, max, window, true, byIP(prefix))
}

func byIP(prefix string) keyFunc {
	return func(c *gin.Context) (string, bool) {
		return prefix + c.ClientIP(), true
	}
}

// ByUser caps one authenticated user, and 401s a request that carries no identity.
func ByUser(client *goredis.Client, prefix string, max int64, window time.Duration) gin.HandlerFunc {
	return enforce(client, max, window, false, func(c *gin.Context) (string, bool) {
		auth, ok := handlers.RequireAuth(c)
		if !ok {
			return "", false
		}
		return prefix + strconv.FormatInt(auth.UserID(), 10), true
	})
}

func enforce(client *goredis.Client, max int64, window time.Duration, refundSuccess bool, key keyFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		bucket, ok := key(c)
		if !ok {
			// RequireAuth has already written the 401.
			c.Abort()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), budget)
		// Charge before admitting, so the gate is atomic: reading first would let
		// a burst of parallel requests all clear the same pre-charge count.
		count, ttl, err := redis.Bump(ctx, client, bucket, window)
		cancel()
		if err != nil {
			// Fail open: the limiter is a mitigation, not an access control.
			logger.GetLogger(c).Warn("rate limit charge failed, fail-open", "key", bucket, "error", err)
			c.Next()
			return
		}
		if count > max {
			c.Header("Retry-After", strconv.FormatInt(ttl, 10))
			handlers.RespondError(c, http.StatusTooManyRequests, "too many requests, try again later")
			c.Abort()
			return
		}

		c.Next()

		// Not deferred: a panic must skip this so the charge stands.
		if !refundSuccess || c.Writer.Status() >= http.StatusBadRequest {
			return
		}
		// The request context may already be cancelled once the handler returns.
		refundCtx, cancelRefund := context.WithTimeout(context.WithoutCancel(c.Request.Context()), budget)
		defer cancelRefund()
		if err := redis.Refund(refundCtx, client, bucket); err != nil {
			logger.GetLogger(c).Warn("rate limit refund failed", "key", bucket, "error", err)
		}
	}
}
