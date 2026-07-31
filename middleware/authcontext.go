package middleware

import (
	"errors"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/adeptry-app/go-common/database"
)

// ErrMissingAuthContext indicates auth middleware did not run or set context values.
var ErrMissingAuthContext = errors.New("missing auth context: user_id or username not set")

// ctxKeyAuthContext caches the parsed AuthContext so middleware and handlers
// share one extraction (incl. ClientIP parsing) per request. Unexported, like
// ctxKeyIdentity: the accessor below is the only way in.
const ctxKeyAuthContext = "rpg_auth_ctx"

// AuthContextFrom returns the request's database.AuthContext, parsing it on
// first call. It lives here rather than beside the CRUD handlers because
// middleware cannot import those, and rate limiters key on the same identity.
func AuthContextFrom(c *gin.Context) (database.AuthContext, error) {
	if v, ok := c.Get(ctxKeyAuthContext); ok {
		if auth, ok := v.(database.AuthContext); ok {
			return auth, nil
		}
	}

	// GetIdentity already rejects a non-positive user ID.
	id, ok := GetIdentity(c)
	if !ok || id.Username == "" {
		return database.AuthContext{}, ErrMissingAuthContext
	}

	auth := database.UserActor(id.UserID, c.ClientIP(), TruncateUserAgent(c.GetHeader("User-Agent")))
	c.Set(ctxKeyAuthContext, auth)
	return auth, nil
}

// userAgentMaxLen caps stored User-Agent length to prevent audit-log poisoning.
const userAgentMaxLen = 512

// TruncateUserAgent enforces the byte cap without splitting a rune, by backing
// up to the nearest rune boundary (at most 3 bytes).
func TruncateUserAgent(ua string) string {
	if len(ua) <= userAgentMaxLen {
		return ua
	}
	cut := userAgentMaxLen
	for cut > 0 && !utf8.RuneStart(ua[cut]) {
		cut--
	}
	return ua[:cut]
}
