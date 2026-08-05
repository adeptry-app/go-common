package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPgErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
		wantOK     bool
	}{
		{"no rows", pgx.ErrNoRows, http.StatusNotFound, "not found", true},
		{"cancelled", context.Canceled, StatusClientClosedRequest, "client closed request", true},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, "request timed out", true},
		{"wrapped no rows", fmt.Errorf("query: %w", pgx.ErrNoRows), http.StatusNotFound, "not found", true},

		{"unique violation", pgErr("23505", ""), http.StatusConflict, "resource already exists", true},
		{"foreign key", pgErr("23503", ""), http.StatusBadRequest, "referenced resource not found", true},
		// PG 18 raises this, not 23503, when ON DELETE RESTRICT blocks a delete.
		{"restrict violation", pgErr("23001", ""), http.StatusConflict, "resource is still referenced", true},
		{"check violation", pgErr("23514", ""), http.StatusBadRequest, "validation constraint failed", true},
		{"no data found", pgErr("P0002", ""), http.StatusNotFound, "not found", true},
		{"insufficient privilege", pgErr("42501", ""), http.StatusForbidden, "access denied", true},
		{"invalid parameter", pgErr("22023", ""), http.StatusBadRequest, "invalid parameter value", true},
		{"value too long", pgErr("22001", ""), http.StatusBadRequest, "value too long", true},
		{"transaction aborted", pgErr("25P02", ""), http.StatusInternalServerError, "transaction aborted", true},
		{"serialization failure", pgErr("40001", ""), http.StatusConflict, "transaction conflict, please retry", true},
		{"deadlock", pgErr("40P01", ""), http.StatusConflict, "transaction conflict, please retry", true},
		{"too many connections", pgErr("53300", ""), http.StatusServiceUnavailable, "service temporarily unavailable", true},
		{"program limit", pgErr("54000", ""), http.StatusServiceUnavailable, "service temporarily unavailable", true},

		// statement_timeout kills land here; without this they read as a 500.
		{"query canceled", pgErr("57014", ""), http.StatusGatewayTimeout, "request timed out", true},

		// SQL owns the text for business rules and lifecycle conflicts.
		{"raise_exception", pgErr("P0001", "Level must be 1-30"), http.StatusBadRequest, "Level must be 1-30", true},
		{"not in prerequisite state", pgErr("55000", "Book is archived"), http.StatusConflict, "Book is archived", true},

		{"connection exception", pgErr("08006", ""), http.StatusServiceUnavailable, "database connection error", true},
		{"connection class, any member", pgErr("08P01", ""), http.StatusServiceUnavailable, "database connection error", true},

		{"unmapped SQLSTATE", pgErr("XX000", "internal"), 0, "", false},
		{"not a database error", errors.New("boom"), 0, "", false},
		{"nil", nil, 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, msg, ok := PgErrorResponse(tt.err)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestIsNoDataFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"P0002", pgErr("P0002", ""), true},
		{"wrapped P0002", fmt.Errorf("call: %w", pgErr("P0002", "")), true},
		{"another code", pgErr("P0001", ""), false},
		{"not a database error", errors.New("boom"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNoDataFound(tt.err); got != tt.want {
				t.Errorf("IsNoDataFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func pgErr(code, message string) error {
	return &pgconn.PgError{Code: code, Message: message}
}
