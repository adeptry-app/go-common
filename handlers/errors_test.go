package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func recordRepositoryError(err error) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	HandleRepositoryError(c, err, "file not found", "failed to fetch file")
	return w
}

func TestHandleRepositoryError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		// Which flavour produced the miss is not the client's business, so every
		// not-found answers with the caller's wording.
		{"pgx no rows", pgx.ErrNoRows, http.StatusNotFound, "file not found"},
		{"P0002 no_data_found", pgErr("P0002", ""), http.StatusNotFound, "file not found"},

		// A caller that went away and a constraint the caller broke are not
		// server faults and must not read as one.
		{"cancelled", context.Canceled, StatusClientClosedRequest, "client closed request"},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, "request timed out"},
		{"unique violation", pgErr("23505", ""), http.StatusConflict, "resource already exists"},
		{"statement timeout", pgErr("57014", ""), http.StatusGatewayTimeout, "request timed out"},

		{"unmapped falls back", errors.New("boom"), http.StatusInternalServerError, "failed to fetch file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := recordRepositoryError(tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response: %v (%s)", err, w.Body.String())
			}
			if got := body["error"]; got != tt.wantMsg {
				t.Errorf("error = %v, want %q", got, tt.wantMsg)
			}
		})
	}
}
