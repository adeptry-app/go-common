// Package handlers provides common HTTP handler utilities.
package handlers

import (
	"errors"
	"net/http"

	"github.com/adeptry-app/go-common/logger"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"
)

// HandleRepositoryError answers a GORM repository error: its own not-found
// with notFoundMsg, then the shared database mapping, then a logged 500 with
// internalMsg.
//
// The driver errors reach us untranslated (gorm.Config sets no TranslateError),
// so a cancelled request or a constraint violation is a *pgconn.PgError here
// too and must not be reported as a server fault.
func HandleRepositoryError(c *gin.Context, err error, notFoundMsg, internalMsg string) {
	// Every flavour of not-found answers with the caller's wording: which one
	// the driver produced is not something the client should be able to tell.
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, pgx.ErrNoRows) || IsNoDataFound(err) {
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
