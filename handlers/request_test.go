package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newBody wraps a string as a request body reader.
func newBody(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

func paramContext(params gin.Params) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = params
	return c
}

func TestIsNullJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil", nil, true},
		{"empty", json.RawMessage(``), true},
		{"SQL null", json.RawMessage(`null`), true},
		{"object", json.RawMessage(`{"id":1}`), false},
		{"empty object", json.RawMessage(`{}`), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNullJSON(tt.raw); got != tt.want {
				t.Errorf("IsNullJSON(%s) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPathParamInt64(t *testing.T) {
	tests := []struct {
		name    string
		params  gin.Params
		want    int64
		wantErr bool
	}{
		{"present", gin.Params{{Key: "id", Value: "42"}}, 42, false},
		{"negative", gin.Params{{Key: "id", Value: "-1"}}, -1, false},
		{"absent", nil, 0, true},
		{"empty", gin.Params{{Key: "id", Value: ""}}, 0, true},
		{"not a number", gin.Params{{Key: "id", Value: "abc"}}, 0, true},
		{"overflows int64", gin.Params{{Key: "id", Value: "99999999999999999999"}}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PathParamInt64(paramContext(tt.params), "id")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMergeJSONObject(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		set     map[string]any
		del     []string
		want    string
		wantErr bool
	}{
		{
			name: "sets a field",
			raw:  json.RawMessage(`{"name":"Kaladin"}`),
			set:  map[string]any{"id": 7},
			want: `{"id":7,"name":"Kaladin"}`,
		},
		{
			name: "overrides an existing field",
			raw:  json.RawMessage(`{"id":999}`),
			set:  map[string]any{"id": 7},
			want: `{"id":7}`,
		},
		{
			name: "deletes a field",
			raw:  json.RawMessage(`{"id":1,"heroId":2}`),
			del:  []string{"heroId"},
			want: `{"id":1}`,
		},
		{
			name: "empty payload starts from an object",
			set:  map[string]any{"id": 7},
			want: `{"id":7}`,
		},
		{name: "rejects an array", raw: json.RawMessage(`[1,2,3]`), wantErr: true},
		{name: "rejects null", raw: json.RawMessage(`null`), wantErr: true},
		{name: "rejects a scalar", raw: json.RawMessage(`"str"`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeJSONObject(tt.raw, tt.set, tt.del...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			// Compare decoded, since map marshalling fixes key order but the
			// test should not depend on it.
			var gotObj, wantObj map[string]any
			_ = json.Unmarshal(got, &gotObj)
			_ = json.Unmarshal([]byte(tt.want), &wantObj)
			if len(gotObj) != len(wantObj) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
			for k, v := range wantObj {
				if gotObj[k] != v {
					t.Errorf("%s = %v, want %v", k, gotObj[k], v)
				}
			}
		})
	}
}

// Numbers must survive the round trip exactly; decoding into `any` would turn
// a large id into a lossy float64.
func TestMergeJSONObject_PreservesNumericPrecision(t *testing.T) {
	raw := json.RawMessage(`{"big":9007199254740993}`)

	got, err := MergeJSONObject(raw, map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("MergeJSONObject() error = %v", err)
	}
	if !json.Valid(got) {
		t.Fatalf("invalid JSON: %s", got)
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(got, &obj)
	if string(obj["big"]) != "9007199254740993" {
		t.Errorf("big = %s, want 9007199254740993", obj["big"])
	}
}

func TestBindPathIDs(t *testing.T) {
	tests := []struct {
		name    string
		params  gin.Params
		ids     []PathID
		want    map[string]any
		wantErr bool
	}{
		{
			name:   "binds a present parameter",
			params: gin.Params{{Key: "id", Value: "7"}},
			ids:    []PathID{{Field: "id", Param: "id"}},
			want:   map[string]any{"id": int64(7)},
		},
		{
			name:   "skips an absent parameter",
			params: nil,
			ids:    []PathID{{Field: "id", Param: "id"}},
			want:   map[string]any{},
		},
		{
			name:   "first present parameter wins",
			params: gin.Params{{Key: "cid", Value: "9"}},
			ids:    []PathID{{Field: "id", Param: "nid"}, {Field: "id", Param: "cid"}},
			want:   map[string]any{"id": int64(9)},
		},
		{
			name:   "earlier match is not overwritten",
			params: gin.Params{{Key: "nid", Value: "4"}, {Key: "cid", Value: "9"}},
			ids:    []PathID{{Field: "id", Param: "nid"}, {Field: "id", Param: "cid"}},
			want:   map[string]any{"id": int64(4)},
		},
		{
			name:   "binds distinct fields together",
			params: gin.Params{{Key: "id", Value: "4"}, {Key: "cid", Value: "9"}},
			ids:    []PathID{{Field: "combatId", Param: "cid"}, {Field: "campaignId", Param: "id"}},
			want:   map[string]any{"combatId": int64(9), "campaignId": int64(4)},
		},
		{
			name:    "rejects a non-integer parameter",
			params:  gin.Params{{Key: "id", Value: "abc"}},
			ids:     []PathID{{Field: "id", Param: "id"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BindPathIDs(paramContext(tt.params), tt.ids)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

type detachKey struct{}

func TestDetachedContext_SurvivesCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), detachKey{}, "kept"))

	ctx, release := DetachedContext(parent)
	defer release()
	cancel()

	if err := ctx.Err(); err != nil {
		t.Errorf("detached context was cancelled with the parent: %v", err)
	}
	if got := ctx.Value(detachKey{}); got != "kept" {
		t.Errorf("value = %v, want it carried over", got)
	}
}

// The real caller's parent is a request context carrying middleware.Timeout's
// deadline, and post-commit work runs after that deadline may have passed. A
// detach that dropped only cancellation would inherit the dead deadline.
func TestDetachedContext_DropsAnExpiredParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()

	if parent.Err() == nil {
		t.Fatal("parent should already be expired")
	}

	ctx, release := DetachedContext(parent)
	defer release()

	if err := ctx.Err(); err != nil {
		t.Fatalf("detached context inherited the expired deadline: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("detached context must still be bounded")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > PostCommitTimeout {
		t.Errorf("deadline is %v away, want up to %v", remaining, PostCommitTimeout)
	}
}

func TestReadBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		limit      int64
		wantOK     bool
		wantStatus int
	}{
		{"reads the body", `{"a":1}`, 1 << 20, true, http.StatusOK},
		{"empty body is not an error", ``, 1 << 20, true, http.StatusOK},
		{"over the limit is 413", `{"a":123456789}`, 4, false, http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", http.NoBody)
			c.Request.Body = http.MaxBytesReader(w, newBody(tt.body), tt.limit)

			body, ok := ReadBody(c)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				if w.Code != tt.wantStatus {
					t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
				}
				return
			}
			if string(body) != tt.body {
				t.Errorf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

// timeoutReader fails the way a body read does once ReadTimeout expires.
type timeoutReader struct{}

func (timeoutReader) Read([]byte) (int, error) { return 0, timeoutError{} }
func (timeoutReader) Close() error             { return nil }

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

// A client too slow for ReadTimeout is not sending a malformed request, and
// logging it as one hides the real cause.
func TestReadBody_TimeoutIsNotABadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	c.Request.Body = timeoutReader{}

	if _, ok := ReadBody(c); ok {
		t.Fatal("ReadBody() ok = true, want false")
	}
	if w.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestTimeout)
	}
}

func TestReadJSONBody(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		want   string
		wantOK bool
	}{
		{"passes valid JSON through", `{"a":1}`, `{"a":1}`, true},
		{"empty becomes an object", ``, `{}`, true},
		{"rejects malformed JSON", `{nope}`, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", newBody(tt.body))

			got, ok := ReadJSONBody(c)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (status %d)", ok, tt.wantOK, w.Code)
			}
			if tt.wantOK && string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
			if !tt.wantOK && w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
}
