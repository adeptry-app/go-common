// Package handlers provides common HTTP handler utilities.
package handlers

import (
	"errors"
	"net/http"

	"github.com/adeptry-app/go-common/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HandleRepositoryError checks if the error is a record not found error
// and responds with appropriate HTTP status code (404 or 500).
// For internal errors, it logs the error with structured logging.
func HandleRepositoryError(c *gin.Context, err error, notFoundMsg, internalMsg string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": notFoundMsg})
		return
	}
	// Log internal errors with structured logging
	logger.GetLogger(c).Error("Repository error",
		"error", err,
		"message", internalMsg,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
	)
	c.JSON(http.StatusInternalServerError, gin.H{"error": internalMsg})
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
	c.JSON(statusCode, gin.H{"error": userMsg})
}

// RespondError responds with an error without logging (for expected errors like validation failures).
// Use this when the error is not exceptional and doesn't need logging (e.g., invalid input).
func RespondError(c *gin.Context, statusCode int, userMsg string) {
	c.JSON(statusCode, gin.H{"error": userMsg})
}
