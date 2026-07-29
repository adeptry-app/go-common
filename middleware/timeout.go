package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout bounds a request end to end. Handlers pass the request context to the
// pool, so the deadline cancels the in-flight query rather than orphaning it.
//
// It writes no response of its own: the cancelled context surfaces as a query
// error the handler's existing error path reports. A non-positive d disables
// the deadline, matching WithStatementTimeout and Config.WithRequestTimeout -
// otherwise a misread config would expire every request before its handler ran.
func Timeout(d time.Duration) gin.HandlerFunc {
	if d <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
