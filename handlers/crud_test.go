package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/adeptry-app/go-common/database"
	"github.com/adeptry-app/go-common/jwt"
	"github.com/adeptry-app/go-common/middleware"
)

// withAuth gives the request the identity RequireAuth resolves.
func withAuth(c *gin.Context) {
	middleware.SetIdentity(c, jwt.Identity{UserID: 1, Username: "testuser"})
}

func performRequest(t *testing.T, r *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var req *http.Request
	if body != nil {
		req, _ = http.NewRequest(method, path, bytes.NewBuffer(body))
	} else {
		req, _ = http.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// errorResponse matches the JSON shape from RespondError/LogAndRespondError.
type errorResponse struct {
	Error string `json:"error"`
}

// echoRepoFunc captures the payload the handler forwarded to the repository.
func echoRepoFunc(got *json.RawMessage) RepoUpsertFunc {
	return func(_ context.Context, _ database.AuthContext, data json.RawMessage) (json.RawMessage, error) {
		*got = data
		return json.RawMessage(`{"ok":true}`), nil
	}
}

// ----------------------------------------------------------------------------
// HandlePost path-ID binding
// ----------------------------------------------------------------------------

func TestHandlePost_BindsPathIDs(t *testing.T) {
	tests := []struct {
		name       string
		route      string
		request    string
		body       string
		ids        []PathID
		wantFields map[string]float64
	}{
		{
			name:       "PUT binds the row named by the URL",
			route:      "/heroes/:id",
			request:    "/heroes/7",
			body:       `{"name":"Kaladin"}`,
			ids:        []PathID{{Field: "id", Param: "id"}},
			wantFields: map[string]float64{"id": 7},
		},
		{
			// The DB authorizes by owner, but a body id pointing elsewhere would
			// still write the wrong row of your own and misattribute the audit.
			name:       "URL wins over a conflicting body id",
			route:      "/heroes/:id",
			request:    "/heroes/7",
			body:       `{"id":999,"name":"Kaladin"}`,
			ids:        []PathID{{Field: "id", Param: "id"}},
			wantFields: map[string]float64{"id": 7},
		},
		{
			name:       "nested write binds its parent",
			route:      "/heroes/:id/favorites",
			request:    "/heroes/12/favorites",
			body:       `{"actionId":3}`,
			ids:        []PathID{{Field: "heroId", Param: "id"}},
			wantFields: map[string]float64{"heroId": 12, "actionId": 3},
		},
		{
			name:    "two parameters bind together",
			route:   "/campaigns/:id/combats/:cid/end-round",
			request: "/campaigns/4/combats/9/end-round",
			body:    `{"round":2}`,
			ids: []PathID{
				{Field: "combatId", Param: "cid"},
				{Field: "campaignId", Param: "id"},
			},
			wantFields: map[string]float64{"combatId": 9, "campaignId": 4, "round": 2},
		},
		{
			name:       "first present parameter wins",
			route:      "/campaigns/:id/npcs/:nid",
			request:    "/campaigns/4/npcs/11",
			body:       `{}`,
			ids:        []PathID{{Field: "id", Param: "nid"}, {Field: "id", Param: "cid"}},
			wantFields: map[string]float64{"id": 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got json.RawMessage
			router := gin.New()
			router.POST(tt.route, func(c *gin.Context) {
				withAuth(c)
				HandlePost(c, echoRepoFunc(&got), tt.ids...)
			})

			w := performRequest(t, router, "POST", tt.request, []byte(tt.body))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}

			var payload map[string]any
			if err := json.Unmarshal(got, &payload); err != nil {
				t.Fatalf("unmarshal forwarded payload: %v", err)
			}
			for field, want := range tt.wantFields {
				if payload[field] != want {
					t.Errorf("%s = %v, want %v", field, payload[field], want)
				}
			}
		})
	}
}

// POST /heroes has no :id, so the same handler must still create.
func TestHandlePost_AbsentParamIsSkipped(t *testing.T) {
	var got json.RawMessage
	router := gin.New()
	router.POST("/heroes", func(c *gin.Context) {
		withAuth(c)
		HandlePost(c, echoRepoFunc(&got), PathID{Field: "id", Param: "id"})
	})

	w := performRequest(t, router, "POST", "/heroes", []byte(`{"name":"Shallan"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal forwarded payload: %v", err)
	}
	if _, ok := payload["id"]; ok {
		t.Errorf("create payload must not carry an id, got %v", payload["id"])
	}
}

// With no ids the body must reach the repository byte for byte.
func TestHandlePost_NoIDsForwardsBodyVerbatim(t *testing.T) {
	var got json.RawMessage
	body := `{"b":1,"a":[2,3],"nested":{"z":null}}`

	router := gin.New()
	router.POST("/heroes/:id", func(c *gin.Context) {
		withAuth(c)
		HandlePost(c, echoRepoFunc(&got))
	})

	w := performRequest(t, router, "POST", "/heroes/7", []byte(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if string(got) != body {
		t.Errorf("forwarded %s, want the original bytes %s", got, body)
	}
}

func TestHandlePost_RejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		request string
		body    []byte
	}{
		{"non-integer path id", "/heroes/abc", []byte(`{}`)},
		{"non-object payload", "/heroes/7", []byte(`[1,2,3]`)},
		{"invalid JSON", "/heroes/7", []byte(`{nope}`)},
		// The regression that forced ReadBody over ReadJSONBody: an empty body
		// must not become an object carrying only the path ids.
		{"empty body", "/heroes/7", []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			router := gin.New()
			router.POST("/heroes/:id", func(c *gin.Context) {
				withAuth(c)
				HandlePost(c, func(_ context.Context, _ database.AuthContext, _ json.RawMessage) (json.RawMessage, error) {
					called = true
					return nil, nil
				}, PathID{Field: "id", Param: "id"})
			})

			w := performRequest(t, router, "POST", tt.request, tt.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
			if called {
				t.Error("repository must not be reached")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// HandlePgxError rendering (the mapping itself is TestPgErrorResponse)
// ----------------------------------------------------------------------------

func TestHandlePgxError_RendersMapping(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"no rows", pgx.ErrNoRows, http.StatusNotFound, "not found"},
		{"unique_violation", pgErr("23505", "dup"), http.StatusConflict, "resource already exists"},
		{"raise_exception keeps the SQL message", pgErr("P0001", "test error"), http.StatusBadRequest, "test error"},
		{"unknown pg code", pgErr("99999", "unknown"), http.StatusInternalServerError, "internal server error"},
		{"non-database error", errors.New("something broke"), http.StatusInternalServerError, "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("GET", "/", nil)

			HandlePgxError(c, tt.err)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			var resp errorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}
			if resp.Error != tt.message {
				t.Errorf("message = %q, want %q", resp.Error, tt.message)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// HandleGet
// ----------------------------------------------------------------------------

func TestHandleGet_Success(t *testing.T) {
	router := gin.New()
	expected := json.RawMessage(`{"key":"value"}`)
	router.GET("/test", func(c *gin.Context) {
		withAuth(c)
		HandleGet(c, func(_ context.Context, _ database.AuthContext) (json.RawMessage, error) {
			return expected, nil
		})
	})

	w := performRequest(t, router, "GET", "/test", nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != string(expected) {
		t.Errorf("body = %s, want %s", w.Body.String(), expected)
	}
}

func TestHandleGet_NoAuth(t *testing.T) {
	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		HandleGet(c, func(_ context.Context, _ database.AuthContext) (json.RawMessage, error) {
			t.Fatal("repo should not be called without auth")
			return nil, nil
		})
	})

	w := performRequest(t, router, "GET", "/test", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGet_RepoError(t *testing.T) {
	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		withAuth(c)
		HandleGet(c, func(_ context.Context, _ database.AuthContext) (json.RawMessage, error) {
			return nil, errors.New("db down")
		})
	})

	w := performRequest(t, router, "GET", "/test", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ----------------------------------------------------------------------------
// HandleGetByID
// ----------------------------------------------------------------------------

func TestHandleGetByID_Success(t *testing.T) {
	router := gin.New()
	expected := json.RawMessage(`{"id":1}`)
	router.GET("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleGetByID(c, "id", func(_ context.Context, _ database.AuthContext, id int64) (json.RawMessage, error) {
			if id != 42 {
				t.Errorf("id = %d, want 42", id)
			}
			return expected, nil
		})
	})

	w := performRequest(t, router, "GET", "/items/42", nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != string(expected) {
		t.Errorf("body = %s, want %s", w.Body.String(), expected)
	}
}

func TestHandleGetByID_NoAuth(t *testing.T) {
	router := gin.New()
	router.GET("/items/:id", func(c *gin.Context) {
		HandleGetByID(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (json.RawMessage, error) {
			t.Fatal("repo should not be called without auth")
			return nil, nil
		})
	})

	w := performRequest(t, router, "GET", "/items/1", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetByID_InvalidID(t *testing.T) {
	router := gin.New()
	router.GET("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleGetByID(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (json.RawMessage, error) {
			t.Fatal("repo should not be called with invalid ID")
			return nil, nil
		})
	})

	tests := []struct {
		name string
		id   string
	}{
		{"alphabetic", "abc"},
		{"float", "1.5"},
		{"special chars", "!@#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := performRequest(t, router, "GET", "/items/"+tt.id, nil)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

// A routine returning SQL NULL, and one returning nothing at all, are both 404.
func TestHandleGetByID_AbsentResult(t *testing.T) {
	tests := []struct {
		name   string
		result json.RawMessage
	}{
		{"SQL null", json.RawMessage("null")},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/items/:id", func(c *gin.Context) {
				withAuth(c)
				HandleGetByID(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (json.RawMessage, error) {
					return tt.result, nil
				})
			})

			w := performRequest(t, router, "GET", "/items/1", nil)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
			}
		})
	}
}

func TestHandleGetByID_RepoError(t *testing.T) {
	router := gin.New()
	router.GET("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleGetByID(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (json.RawMessage, error) {
			return nil, pgx.ErrNoRows
		})
	})

	w := performRequest(t, router, "GET", "/items/1", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ----------------------------------------------------------------------------
// HandleGetByString
// ----------------------------------------------------------------------------

func TestHandleGetByString_Success(t *testing.T) {
	router := gin.New()
	expected := json.RawMessage(`{"code":"warrior"}`)
	router.GET("/items/:code", func(c *gin.Context) {
		withAuth(c)
		HandleGetByString(c, "code", func(_ context.Context, _ database.AuthContext, code string) (json.RawMessage, error) {
			if code != "warrior" {
				t.Errorf("code = %q, want %q", code, "warrior")
			}
			return expected, nil
		})
	})

	w := performRequest(t, router, "GET", "/items/warrior", nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleGetByString_NoAuth(t *testing.T) {
	router := gin.New()
	router.GET("/items/:code", func(c *gin.Context) {
		HandleGetByString(c, "code", func(_ context.Context, _ database.AuthContext, _ string) (json.RawMessage, error) {
			t.Fatal("repo should not be called without auth")
			return nil, nil
		})
	})

	w := performRequest(t, router, "GET", "/items/test", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetByString_NullResult(t *testing.T) {
	router := gin.New()
	router.GET("/items/:code", func(c *gin.Context) {
		withAuth(c)
		HandleGetByString(c, "code", func(_ context.Context, _ database.AuthContext, _ string) (json.RawMessage, error) {
			return json.RawMessage("null"), nil
		})
	})

	w := performRequest(t, router, "GET", "/items/nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetByString_RepoError(t *testing.T) {
	router := gin.New()
	router.GET("/items/:code", func(c *gin.Context) {
		withAuth(c)
		HandleGetByString(c, "code", func(_ context.Context, _ database.AuthContext, _ string) (json.RawMessage, error) {
			return nil, errors.New("db error")
		})
	})

	w := performRequest(t, router, "GET", "/items/test", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ----------------------------------------------------------------------------
// HandleGetByTwoIDs
// ----------------------------------------------------------------------------

func TestHandleGetByTwoIDs(t *testing.T) {
	tests := []struct {
		name    string
		request string
		status  int
		wantHit bool
	}{
		{"both ids parse", "/campaigns/4/combats/9", http.StatusOK, true},
		{"first id invalid", "/campaigns/abc/combats/9", http.StatusBadRequest, false},
		{"second id invalid", "/campaigns/4/combats/xyz", http.StatusBadRequest, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			router := gin.New()
			router.GET("/campaigns/:id/combats/:cid", func(c *gin.Context) {
				withAuth(c)
				HandleGetByTwoIDs(c, "id", "cid", func(_ context.Context, _ database.AuthContext, id1, id2 int64) (json.RawMessage, error) {
					called = true
					if id1 != 4 || id2 != 9 {
						t.Errorf("ids = %d,%d want 4,9", id1, id2)
					}
					return json.RawMessage(`{"ok":true}`), nil
				})
			})

			w := performRequest(t, router, "GET", tt.request, nil)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			if called != tt.wantHit {
				t.Errorf("repo called = %v, want %v", called, tt.wantHit)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// HandleDelete
// ----------------------------------------------------------------------------

func TestHandleDelete_Success(t *testing.T) {
	router := gin.New()
	router.DELETE("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleDelete(c, "id", func(_ context.Context, _ database.AuthContext, id int64) (bool, error) {
			if id != 42 {
				t.Errorf("id = %d, want 42", id)
			}
			return true, nil
		})
	})

	w := performRequest(t, router, "DELETE", "/items/42", nil)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestHandleDelete_NoAuth(t *testing.T) {
	router := gin.New()
	router.DELETE("/items/:id", func(c *gin.Context) {
		HandleDelete(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (bool, error) {
			t.Fatal("repo should not be called without auth")
			return false, nil
		})
	})

	w := performRequest(t, router, "DELETE", "/items/1", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleDelete_InvalidID(t *testing.T) {
	router := gin.New()
	router.DELETE("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleDelete(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (bool, error) {
			t.Fatal("repo should not be called with invalid ID")
			return false, nil
		})
	})

	w := performRequest(t, router, "DELETE", "/items/abc", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDelete_NotFound(t *testing.T) {
	router := gin.New()
	router.DELETE("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleDelete(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (bool, error) {
			return false, nil
		})
	})

	w := performRequest(t, router, "DELETE", "/items/999", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDelete_RepoError(t *testing.T) {
	router := gin.New()
	router.DELETE("/items/:id", func(c *gin.Context) {
		withAuth(c)
		HandleDelete(c, "id", func(_ context.Context, _ database.AuthContext, _ int64) (bool, error) {
			return false, errors.New("db error")
		})
	})

	w := performRequest(t, router, "DELETE", "/items/1", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ----------------------------------------------------------------------------
// HandleDeleteByString / HandleDeleteByTwoIDs
// ----------------------------------------------------------------------------

func TestHandleDeleteByString(t *testing.T) {
	tests := []struct {
		name    string
		deleted bool
		status  int
	}{
		{"deleted", true, http.StatusNoContent},
		{"absent", false, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.DELETE("/items/:code", func(c *gin.Context) {
				withAuth(c)
				HandleDeleteByString(c, "code", func(_ context.Context, _ database.AuthContext, code string) (bool, error) {
					if code != "warrior" {
						t.Errorf("code = %q, want %q", code, "warrior")
					}
					return tt.deleted, nil
				})
			})

			w := performRequest(t, router, "DELETE", "/items/warrior", nil)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestHandleDeleteByTwoIDs_InvalidIDStopsShortOfTheRepo(t *testing.T) {
	called := false
	router := gin.New()
	router.DELETE("/campaigns/:id/heroes/:hid", func(c *gin.Context) {
		withAuth(c)
		HandleDeleteByTwoIDs(c, "id", "hid", func(_ context.Context, _ database.AuthContext, _, _ int64) (bool, error) {
			called = true
			return true, nil
		})
	})

	w := performRequest(t, router, "DELETE", "/campaigns/4/heroes/abc", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if called {
		t.Error("repository must not be reached")
	}
}

// RequireAuth is the gate every handler above shares.
func TestRequireAuth(t *testing.T) {
	router := gin.New()
	router.GET("/authed", func(c *gin.Context) {
		withAuth(c)
		auth, ok := RequireAuth(c)
		if !ok {
			t.Fatal("RequireAuth rejected an authenticated request")
		}
		if auth.UserID != 1 || auth.Username != "testuser" {
			t.Errorf("auth = %+v, want UserID 1 / testuser", auth)
		}
		c.Status(http.StatusOK)
	})

	if w := performRequest(t, router, "GET", "/authed", nil); w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// A pgconn error reaching HandlePgxError must not be reported as a server fault.
func TestHandlePgxError_ConstraintIsNotAServerFault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/", nil)

	HandlePgxError(c, &pgconn.PgError{Code: "23503", Message: "fk"})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
