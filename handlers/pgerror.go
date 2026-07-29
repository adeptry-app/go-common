package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// StatusClientClosedRequest is nginx's non-standard 499, used when the caller
// disconnected before the response: no status is deliverable, but the access
// log should not read as a server fault.
const StatusClientClosedRequest = 499

// IsNoDataFound reports whether err is a P0002 (no_data_found) database error.
func IsNoDataFound(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "P0002"
}

// PgErrorResponse maps a database error to the status and message to answer
// with. ok=false means it is not a recognised database error and the caller
// decides the fallback.
//
// It is pure so the mapping can be tested without a request, and so both
// HandlePgxError and HandleRepositoryError share one table.
func PgErrorResponse(err error) (status int, msg string, ok bool) {
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound, "not found", true
	}
	if errors.Is(err, context.Canceled) {
		return StatusClientClosedRequest, "client closed request", true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "request timed out", true
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return 0, "", false
	}

	switch pgErr.Code {
	case "23505": // unique_violation
		return http.StatusConflict, "resource already exists", true
	case "23503": // foreign_key_violation
		return http.StatusBadRequest, "referenced resource not found", true
	case "23514": // check_violation
		return http.StatusBadRequest, "validation constraint failed", true
	case "P0002": // no_data_found
		return http.StatusNotFound, "not found", true
	case "42501": // insufficient_privilege
		return http.StatusForbidden, "access denied", true
	case "22023": // invalid_parameter_value
		return http.StatusBadRequest, "invalid parameter value", true
	case "22001": // string_data_right_truncation
		return http.StatusBadRequest, "value too long", true
	case "57014": // query_canceled — statement_timeout, or the caller went away
		return http.StatusGatewayTimeout, "request timed out", true
	case "P0001": // raise_exception — SQL owns the message for business rules
		return http.StatusBadRequest, pgErr.Message, true
	case "55000": // object_not_in_prerequisite_state (e.g. soft-deleted row)
		return http.StatusConflict, pgErr.Message, true
	case "40001", "40P01": // serialization_failure, deadlock_detected — retryable
		return http.StatusConflict, "transaction conflict, please retry", true
	case "25P02": // in_failed_sql_transaction
		return http.StatusInternalServerError, "transaction aborted", true
	case "53300", "54000": // too_many_connections, program_limit_exceeded
		return http.StatusServiceUnavailable, "service temporarily unavailable", true
	}

	// SQLSTATE class 08 — connection_exception family.
	if strings.HasPrefix(pgErr.Code, "08") {
		return http.StatusServiceUnavailable, "database connection error", true
	}
	return 0, "", false
}

// HandlePgxError maps pgx/PostgreSQL errors to HTTP responses, falling back to
// a logged 500. Use it for pgx-backed repositories.
func HandlePgxError(c *gin.Context, err error) {
	respondPgError(c, err, "internal server error")
}

// respondPgError answers from the shared mapping, or logs err behind fallback.
// A 404 and a client disconnect are not server faults, so they are not logged.
func respondPgError(c *gin.Context, err error, fallback string) {
	status, msg, ok := PgErrorResponse(err)
	if !ok {
		LogAndRespondError(c, http.StatusInternalServerError, err, fallback)
		return
	}
	if status == http.StatusNotFound || status == StatusClientClosedRequest {
		RespondError(c, status, msg)
		return
	}
	LogAndRespondError(c, status, err, msg)
}
