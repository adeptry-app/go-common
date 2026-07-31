package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/adeptry-app/go-common/database"
	"github.com/adeptry-app/go-common/middleware"
)

// The CRUD shape these helpers serve: authenticate, resolve path parameters,
// call one database routine, render its JSONB verbatim. Repository methods take
// the AuthContext so the routine can set audit context; they return raw JSONB
// because the database owns the response shape.

// RepoFunc calls a repository method and returns JSONB.
type RepoFunc func(ctx context.Context, auth database.AuthContext) (json.RawMessage, error)

// RepoIDFunc calls a repository method with an ID parameter.
type RepoIDFunc func(ctx context.Context, auth database.AuthContext, id int64) (json.RawMessage, error)

// RepoUpsertFunc calls a repository upsert method with JSON data.
type RepoUpsertFunc func(ctx context.Context, auth database.AuthContext, data json.RawMessage) (json.RawMessage, error)

// RepoStringFunc calls a repository method with a string parameter.
type RepoStringFunc func(ctx context.Context, auth database.AuthContext, code string) (json.RawMessage, error)

// RepoDeleteFunc calls a repository delete method.
type RepoDeleteFunc func(ctx context.Context, auth database.AuthContext, id int64) (bool, error)

// RepoDeleteStringFunc calls a repository delete method with a string parameter.
type RepoDeleteStringFunc func(ctx context.Context, auth database.AuthContext, code string) (bool, error)

// RepoTwoIDFunc calls a repository method with two ID parameters.
type RepoTwoIDFunc func(ctx context.Context, auth database.AuthContext, id1 int64, id2 int64) (json.RawMessage, error)

// RepoDeleteTwoIDFunc calls a repository delete method with two ID parameters.
type RepoDeleteTwoIDFunc func(ctx context.Context, auth database.AuthContext, id1 int64, id2 int64) (bool, error)

// RequireAuth returns the auth context or writes a 401 response. ok=false means
// the caller has already responded and must return immediately.
func RequireAuth(c *gin.Context) (database.AuthContext, bool) {
	auth, err := middleware.AuthContextFrom(c)
	if err != nil {
		RespondError(c, http.StatusUnauthorized, "unauthorized")
		return database.AuthContext{}, false
	}
	return auth, true
}

// ----------------------------------------------------------------------------
// Request handlers
// ----------------------------------------------------------------------------

// HandleGet: auth → repo call → JSON response
func HandleGet(c *gin.Context, fn RepoFunc) {
	auth, ok := RequireAuth(c)
	if !ok {
		return
	}

	result, err := fn(c.Request.Context(), auth)
	if err != nil {
		HandlePgxError(c, err)
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}

// HandleGetByID: auth → path param → repo call → null check → JSON response
func HandleGetByID(c *gin.Context, paramName string, fn RepoIDFunc) {
	auth, ok := RequireAuth(c)
	if !ok {
		return
	}

	id, err := PathParamInt64(c, paramName)
	if err != nil {
		RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := fn(c.Request.Context(), auth, id)
	if err != nil {
		HandlePgxError(c, err)
		return
	}

	respondJSONOrNotFound(c, result)
}

// HandleGetByString: auth → string path param → repo call → null check → JSON response
func HandleGetByString(c *gin.Context, paramName string, fn RepoStringFunc) {
	auth, ok := RequireAuth(c)
	if !ok {
		return
	}
	GetByStringResponse(c, auth, paramName, fn)
}

// GetByStringResponse: string path param → repo call → null check → JSON
// response. Post-auth tail shared by authed and public (AnonymousActor) lookups.
func GetByStringResponse(c *gin.Context, auth database.AuthContext, paramName string, fn RepoStringFunc) {
	value := c.Param(paramName)
	if value == "" {
		RespondError(c, http.StatusBadRequest, fmt.Sprintf("missing path parameter: %s", paramName))
		return
	}

	result, err := fn(c.Request.Context(), auth, value)
	if err != nil {
		HandlePgxError(c, err)
		return
	}

	respondJSONOrNotFound(c, result)
}

// HandleGetByTwoIDs: auth → two path params → repo call → null check → JSON response
func HandleGetByTwoIDs(c *gin.Context, param1, param2 string, fn RepoTwoIDFunc) {
	auth, ok := RequireAuth(c)
	if !ok {
		return
	}

	id1, id2, ok := twoPathParams(c, param1, param2)
	if !ok {
		return
	}

	result, err := fn(c.Request.Context(), auth, id1, id2)
	if err != nil {
		HandlePgxError(c, err)
		return
	}

	respondJSONOrNotFound(c, result)
}

// HandlePost: auth → read body → repo call → JSON response. Empty bodies are
// rejected (CRUD payloads are mandatory).
//
// Any ids given are bound into the payload first, so the URL decides which row
// is written instead of the body. With none, the body is forwarded verbatim.
func HandlePost(c *gin.Context, fn RepoUpsertFunc, ids ...PathID) {
	auth, ok := RequireAuth(c)
	if !ok {
		return
	}

	body, ok := ReadBody(c)
	if !ok {
		return
	}
	if !json.Valid(body) {
		RespondError(c, http.StatusBadRequest, "invalid JSON")
		return
	}

	if len(ids) > 0 {
		set, err := BindPathIDs(c, ids)
		if err != nil {
			RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if body, err = MergeJSONObject(body, set); err != nil {
			RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	result, err := fn(c.Request.Context(), auth, body)
	if err != nil {
		HandlePgxError(c, err)
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}

// HandleDeleteCommon: auth → repo delete closure → 204 or 404. Callers parse
// their param and pass a closure that captures it.
func HandleDeleteCommon(c *gin.Context, del func(ctx context.Context, auth database.AuthContext) (bool, error)) {
	auth, ok := RequireAuth(c)
	if !ok {
		return
	}

	deleted, err := del(c.Request.Context(), auth)
	if err != nil {
		HandlePgxError(c, err)
		return
	}

	if !deleted {
		RespondError(c, http.StatusNotFound, "not found")
		return
	}

	c.Status(http.StatusNoContent)
}

// HandleDelete: auth → path param → repo call → 204 or 404
func HandleDelete(c *gin.Context, paramName string, fn RepoDeleteFunc) {
	id, err := PathParamInt64(c, paramName)
	if err != nil {
		RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	HandleDeleteCommon(c, func(ctx context.Context, auth database.AuthContext) (bool, error) {
		return fn(ctx, auth, id)
	})
}

// HandleDeleteByString: auth → string path param → repo call → 204 or 404
func HandleDeleteByString(c *gin.Context, paramName string, fn RepoDeleteStringFunc) {
	value := c.Param(paramName)
	if value == "" {
		RespondError(c, http.StatusBadRequest, fmt.Sprintf("missing path parameter: %s", paramName))
		return
	}
	HandleDeleteCommon(c, func(ctx context.Context, auth database.AuthContext) (bool, error) {
		return fn(ctx, auth, value)
	})
}

// HandleDeleteByTwoIDs: auth → two path params → repo call → 204 or 404
func HandleDeleteByTwoIDs(c *gin.Context, param1, param2 string, fn RepoDeleteTwoIDFunc) {
	id1, id2, ok := twoPathParams(c, param1, param2)
	if !ok {
		return
	}
	HandleDeleteCommon(c, func(ctx context.Context, auth database.AuthContext) (bool, error) {
		return fn(ctx, auth, id1, id2)
	})
}

// twoPathParams parses both parameters, responding 400 on the first failure.
func twoPathParams(c *gin.Context, param1, param2 string) (int64, int64, bool) {
	id1, err := PathParamInt64(c, param1)
	if err != nil {
		RespondError(c, http.StatusBadRequest, err.Error())
		return 0, 0, false
	}
	id2, err := PathParamInt64(c, param2)
	if err != nil {
		RespondError(c, http.StatusBadRequest, err.Error())
		return 0, 0, false
	}
	return id1, id2, true
}

// respondJSONOrNotFound renders result, or 404 when the routine returned NULL.
func respondJSONOrNotFound(c *gin.Context, result json.RawMessage) {
	if IsNullJSON(result) {
		RespondError(c, http.StatusNotFound, "not found")
		return
	}
	c.Data(http.StatusOK, "application/json", result)
}
