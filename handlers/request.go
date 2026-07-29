package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// IsNullJSON reports whether a JSONB result is absent: nil/empty or the SQL
// NULL literal.
func IsNullJSON(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// ReadBody reads the request body, mapping the body-limit error to 413.
// ok=false means the caller has already responded and must return immediately.
func ReadBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			RespondError(c, http.StatusRequestEntityTooLarge, "request body too large")
			return nil, false
		}
		RespondError(c, http.StatusBadRequest, "failed to read request body")
		return nil, false
	}
	return body, true
}

// ReadJSONBody reads and validates the request body as JSON. Empty body becomes "{}".
func ReadJSONBody(c *gin.Context) (json.RawMessage, bool) {
	body, ok := ReadBody(c)
	if !ok {
		return nil, false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if !json.Valid(body) {
		RespondError(c, http.StatusBadRequest, "invalid JSON")
		return nil, false
	}
	return body, true
}

// PathParamInt64 parses an integer path parameter; an absent one is an error.
func PathParamInt64(c *gin.Context, param string) (int64, error) {
	str := c.Param(param)
	if str == "" {
		return 0, fmt.Errorf("missing path parameter: %s", param)
	}
	id, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: must be an integer", param)
	}
	return id, nil
}

// MergeJSONObject parses raw as a top-level JSON object (empty/absent starts
// from {}), assigns each set entry, removes each del key, then re-marshals.
func MergeJSONObject(raw json.RawMessage, set map[string]any, del ...string) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("payload must be a JSON object: %w", err)
		}
		if obj == nil {
			return nil, fmt.Errorf("payload must be a JSON object: got null")
		}
	}
	for k, v := range set {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		obj[k] = b
	}
	for _, k := range del {
		delete(obj, k)
	}
	return json.Marshal(obj)
}

// PathID binds a payload field to the path parameter that owns it. Order is
// significant: the first parameter present on the route wins.
type PathID struct {
	Field string
	Param string
}

// BindPathIDs resolves ids against the route's parameters. One absent from the
// route is skipped, so a spec can serve both POST (create) and PUT /:id.
func BindPathIDs(c *gin.Context, ids []PathID) (map[string]any, error) {
	set := make(map[string]any, len(ids))
	for _, want := range ids {
		if c.Param(want.Param) == "" {
			continue
		}
		if _, done := set[want.Field]; done {
			continue
		}
		id, err := PathParamInt64(c, want.Param)
		if err != nil {
			return nil, err
		}
		set[want.Field] = id
	}
	return set, nil
}

// PostCommitTimeout bounds work that runs after the row is already committed.
const PostCommitTimeout = 5 * time.Second

// DetachedContext keeps ctx's values but drops its cancellation, so a client
// that disconnects cannot abandon work the commit already depends on - cache
// invalidation, object cleanup, and the like.
func DetachedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), PostCommitTimeout)
}
