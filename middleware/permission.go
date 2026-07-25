package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Permission level constants for type-safe usage
const (
	LevelNone   = "none"
	LevelRead   = "read"
	LevelEdit   = "edit"
	LevelDelete = "delete"
)

// Resource constants every service gates its routes on. Codes must match the
// auth.resources rows seeded by R__seed_rbac.sql.
const (
	ResourceClassifiers = "classifiers"
	ResourceHeroes      = "heroes"
	ResourceCampaigns   = "campaigns"
	ResourceFiles       = "files"
	ResourceEmails      = "emails"
)

// Permission levels (hierarchical): none(0) < read(1) < edit(2) < delete(3)
var levelValues = map[string]int{
	LevelNone:   0,
	LevelRead:   1,
	LevelEdit:   2,
	LevelDelete: 3,
}

// ValidLevel checks if a permission level string is recognized
func ValidLevel(level string) bool {
	_, ok := levelValues[level]
	return ok
}

// HasPermission checks if user level meets required level.
// Unknown userLevel defaults to 0 (none), denying access.
// Unknown requiredLevel defaults to max (delete), also denying access.
// This fail-safe prevents typos from accidentally granting access.
func HasPermission(userLevel, requiredLevel string) bool {
	userVal := levelValues[userLevel] // defaults to 0 if unknown
	requiredVal, ok := levelValues[requiredLevel]
	if !ok {
		requiredVal = levelValues[LevelDelete] // unknown required = max = deny
	}
	return userVal >= requiredVal
}

// RequirePermission returns middleware that checks user has required permission.
// Panics if level is not a valid permission level (none, read, edit, delete).
// This catches typos at startup rather than silently granting access.
func RequirePermission(resource, level string) gin.HandlerFunc {
	if !ValidLevel(level) {
		panic("middleware: invalid permission level: " + level)
	}
	return func(c *gin.Context) {
		id, ok := GetIdentity(c)
		if !ok {
			abortUnauthorized(c, "no identity")
			return
		}

		userLevel := id.Scopes[resource]
		if !HasPermission(userLevel, level) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":    "insufficient permissions",
				"resource": resource,
				"required": level,
				"have":     userLevel,
			})
			return
		}

		c.Next()
	}
}
