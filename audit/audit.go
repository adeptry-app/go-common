// Package audit provides utilities for security event logging.
package audit

import (
	"context"
	"encoding/json"

	"github.com/adeptry-app/go-common/logger"
	"github.com/adeptry-app/go-common/middleware"
	"github.com/adeptry-app/go-common/repository"
	"github.com/gin-gonic/gin"
)

// Context keys for storing audit context
const (
	contextKeyClientIP  = "audit_client_ip"
	contextKeyUserAgent = "audit_user_agent"
)

// Action types for audit logging. These are persisted into action_log.action_type
// and read back by tooling, so callers must use these rather than literals.
const (
	ActionLoginSuccess        = "login_success"
	ActionLoginFailure        = "login_failure"
	ActionLogout              = "logout"
	ActionTokenRefresh        = "token_refresh"
	ActionTokenValidation     = "token_validation_failure"
	ActionTokenReuse          = "token_reuse_detected" // #nosec G101 -- action name, not a credential
	ActionRegistrationSuccess = "registration_success"
	ActionRegistrationFailure = "registration_failure"
	ActionFileUpload          = "file_upload"
	ActionFileDownload        = "file_download"
	ActionFileDelete          = "file_delete"
)

// Credential and identity lifecycle events. Each one changes what a credential
// proves, so support can tell recovery apart from takeover after the fact.
// Metadata carries the reason and never the credential itself.
const (
	ActionPasswordChangeSuccess = "password_change_success"
	ActionPasswordChangeFailure = "password_change_failure"
	ActionPasswordSetSuccess    = "password_set_success"
	ActionPasswordSetFailure    = "password_set_failure"
	ActionPasswordResetRequest  = "password_reset_request"
	ActionPasswordResetSuccess  = "password_reset_success"
	ActionPasswordResetFailure  = "password_reset_failure"
	ActionEmailChangeRequest    = "email_change_request"
	ActionEmailChangeFailure    = "email_change_failure"
	ActionEmailVerifySuccess    = "email_verify_success"
	ActionEmailVerifyFailure    = "email_verify_failure"
	ActionOAuthLink             = "oauth_link"
	ActionOAuthUnlink           = "oauth_unlink"
	ActionSessionsRevoked       = "sessions_revoked"
)

// Resource types
const (
	ResourceTypeFile = "file"
	ResourceTypeUser = "user"
)

// ContextMiddleware extracts and stores client IP and user agent in context
// Should be added early in middleware chain, before auth middleware
func ContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract and store IP address
		if ip := extractClientIP(c); ip != nil {
			c.Set(contextKeyClientIP, ip)
		}

		// Extract and store user agent
		if ua := extractUserAgent(c); ua != nil {
			c.Set(contextKeyUserAgent, ua)
		}

		c.Next()
	}
}

// extractClientIP gets the client IP, honouring the proxy headers gin trusts.
func extractClientIP(c *gin.Context) *string {
	return optional(c.ClientIP())
}

// extractUserAgent gets user agent from request headers
func extractUserAgent(c *gin.Context) *string {
	return optional(c.GetHeader("User-Agent"))
}

// optional maps an empty string to a nil column value.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// stored returns the value ContextMiddleware cached, falling back to extracting
// it directly when the middleware did not run.
func stored(c *gin.Context, key string, extract func(*gin.Context) *string) *string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(*string); ok {
			return s
		}
	}
	return extract(c)
}

// GetClientIP retrieves client IP from context (set by ContextMiddleware)
func GetClientIP(c *gin.Context) *string {
	return stored(c, contextKeyClientIP, extractClientIP)
}

// GetUserAgent retrieves user agent from context (set by ContextMiddleware)
func GetUserAgent(c *gin.Context) *string {
	return stored(c, contextKeyUserAgent, extractUserAgent)
}

// GetUserID retrieves user ID from context (set by auth middleware after token validation)
func GetUserID(c *gin.Context) *int64 {
	if id, ok := middleware.GetIdentity(c); ok {
		return &id.UserID
	}
	return nil
}

// LogFromContext logs an action using context values (IP, UA, user_id)
// This is the recommended way to log audit events - requires ContextMiddleware
// Errors are logged and returned; callers discard them rather than fail the operation
func LogFromContext(c *gin.Context, repo repository.ActionLogRepository, actionType string, resourceType *string, resourceID *int64, source *string, metadata map[string]interface{}) error {
	return LogAction(c, repo, actionType, resourceType, resourceID, GetUserID(c), source, metadata)
}

// LogAction is a helper that logs an action with explicit user ID and source
// Use LogFromContext instead when user_id is in context from auth middleware
// Errors are logged and returned; callers discard them rather than fail the operation
func LogAction(c *gin.Context, repo repository.ActionLogRepository, actionType string, resourceType *string, resourceID *int64, userID *int64, source *string, metadata map[string]interface{}) error {
	var metadataJSON json.RawMessage
	if metadata != nil {
		bytes, err := json.Marshal(metadata)
		if err != nil {
			logger.GetLogger(c).Error("Failed to marshal audit metadata",
				"error", err,
				"action_type", actionType,
			)
			return err
		}
		metadataJSON = bytes
	}

	actionLog := &repository.ActionLog{
		ActionType:   actionType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		UserID:       userID,
		IPAddress:    GetClientIP(c),
		UserAgent:    GetUserAgent(c),
		Source:       source,
		Metadata:     metadataJSON,
	}

	// The entry must outlive the request it describes, so a caller that
	// disconnects cannot erase its own trail.
	if err := repo.LogAction(context.WithoutCancel(c.Request.Context()), actionLog); err != nil {
		logger.GetLogger(c).Error("Failed to log audit action",
			"error", err,
			"action_type", actionType,
			"resource_type", resourceType,
			"resource_id", resourceID,
		)
		return err
	}

	return nil
}
