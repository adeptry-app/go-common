package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit caps how many bytes a handler may read from a request body, so an
// oversized upload is refused while it streams rather than after it is buffered.
//
// It writes no response of its own: the reader returns an error the handler's
// existing bind/parse error path reports.
func BodyLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		}
		c.Next()
	}
}
