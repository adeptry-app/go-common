package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

		// SQL owns the text for business rules, lifecycle conflicts and plan limits.
		{"raise_exception", pgErr("P0001", "Level must be 1-30"), http.StatusBadRequest, "Level must be 1-30", true},
		{"not in prerequisite state", pgErr("55000", "Book is archived"), http.StatusConflict, "Book is archived", true},
		{"plan limit", pgErr("P0402", "Free plan allows 5 heroes"), http.StatusPaymentRequired, "Free plan allows 5 heroes", true},

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

// HandlePgxError end to end: status, body and whether it logs.
func TestHandlePgxError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
		wantLogged bool
	}{
		// Not logged, no server fault.
		{"no rows", pgx.ErrNoRows, http.StatusNotFound, "not found", false},
		{"no data found", pgErr("P0002", ""), http.StatusNotFound, "not found", false},
		{"plan limit", pgErr("P0402", "Free plan allows 5 heroes"), http.StatusPaymentRequired, "Free plan allows 5 heroes", false},

		{"unique violation", pgErr("23505", "dup"), http.StatusConflict, "resource already exists", true},
		{"foreign key violation", pgErr("23503", "fk"), http.StatusBadRequest, "referenced resource not found", true},
		{"raise_exception keeps the SQL message", pgErr("P0001", "test error"), http.StatusBadRequest, "test error", true},
		{"unmapped SQLSTATE", pgErr("XX000", "internal"), http.StatusInternalServerError, "internal server error", true},
		{"non-database error", errors.New("something broke"), http.StatusInternalServerError, "internal server error", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			// slog.SetDefault also rewires the std log writer and flags.
			previousLogger, previousWriter, previousFlags := slog.Default(), log.Writer(), log.Flags()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
			t.Cleanup(func() {
				slog.SetDefault(previousLogger)
				log.SetOutput(previousWriter)
				log.SetFlags(previousFlags)
			})

			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			HandlePgxError(c, tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			var resp errorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v (%s)", err, w.Body.String())
			}
			if resp.Error != tt.wantMsg {
				t.Errorf("message = %q, want %q", resp.Error, tt.wantMsg)
			}
			if logged := buf.Len() > 0; logged != tt.wantLogged {
				t.Errorf("logged = %v, want %v (%s)", logged, tt.wantLogged, buf.String())
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
