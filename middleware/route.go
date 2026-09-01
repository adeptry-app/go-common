package middleware

import "github.com/gin-gonic/gin"

// unmatchedRoute labels a request that matched no route.
const unmatchedRoute = "unmatched"

// RouteTemplate returns the matched route pattern, never the raw request target.
func RouteTemplate(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return unmatchedRoute
}
