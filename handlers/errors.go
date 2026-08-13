// Package handlers provides common HTTP handler utilities.
package handlers

import (
	"net/http"

	"github.com/adeptry-app/go-common/logger"
	"github.com/gin-gonic/gin"
)

// HandleRepositoryError answers a repository error: not-found with
// notFoundMsg, then the shared database mapping, then a logged 500 with
// internalMsg. Which flavour of not-found the driver produced is read from
// PgErrorResponse, so the two cannot disagree.
func HandleRepositoryError(c *gin.Context, err error, notFoundMsg, internalMsg string) {
	if status, _, ok := PgErrorResponse(err); ok && status == http.StatusNotFound {
		RespondError(c, http.StatusNotFound, notFoundMsg)
		return
	}
	respondPgError(c, err, internalMsg)
}

// LogAndRespondError logs the error with context and responds with the given status code.
// Use this for non-repository errors (auth failures, validation errors, external service errors).
func LogAndRespondError(c *gin.Context, statusCode int, err error, userMsg string) {
	logger.GetLogger(c).Error(userMsg,
		"error", err,
		"status", statusCode,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
	)
	RespondError(c, statusCode, userMsg)
}

// RespondError responds with an error without logging (for expected errors like validation failures).
// Use this when the error is not exceptional and doesn't need logging (e.g., invalid input).
// Sole producer of the error envelope every service returns.
func RespondError(c *gin.Context, statusCode int, userMsg string) {
	c.JSON(statusCode, gin.H{"error": userMsg})
}
