// Package audit provides utilities for security event logging.
package audit

import (
	"encoding/json"
	"strings"

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

// Action types for audit logging
const (
	ActionLoginSuccess    = "login_success"
	ActionLoginFailure    = "login_failure"
	ActionLogout          = "logout"
	ActionTokenRefresh    = "token_refresh"
	ActionTokenValidation = "token_validation_failure"
	ActionFileUpload      = "file_upload"
	ActionFileDownload    = "file_download"
	ActionFileDelete      = "file_delete"
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

// extractClientIP gets IP address from request headers or remote address
func extractClientIP(c *gin.Context) *string {
	// Try X-Forwarded-For first (for requests through Traefik/proxy)
	ip := c.GetHeader("X-Forwarded-For")
	if ip != "" {
		// X-Forwarded-For can be a comma-separated list, take the first IP
		if idx := strings.Index(ip, ","); idx > 0 {
			ip = strings.TrimSpace(ip[:idx])
		}
		return &ip
	}

	// Fall back to X-Real-IP
	ip = c.GetHeader("X-Real-IP")
	if ip != "" {
		return &ip
	}

	// Fall back to remote address
	ip = c.ClientIP()
	if ip != "" {
		return &ip
	}

	return nil
}

// extractUserAgent gets user agent from request headers
func extractUserAgent(c *gin.Context) *string {
	ua := c.GetHeader("User-Agent")
	if ua != "" {
		return &ua
	}
	return nil
}

// GetClientIP retrieves client IP from context (set by ContextMiddleware)
func GetClientIP(c *gin.Context) *string {
	if ip, exists := c.Get(contextKeyClientIP); exists {
		if ipStr, ok := ip.(*string); ok {
			return ipStr
		}
	}
	// Fallback to direct extraction if middleware not used
	return extractClientIP(c)
}

// GetUserAgent retrieves user agent from context (set by ContextMiddleware)
func GetUserAgent(c *gin.Context) *string {
	if ua, exists := c.Get(contextKeyUserAgent); exists {
		if uaStr, ok := ua.(*string); ok {
			return uaStr
		}
	}
	// Fallback to direct extraction if middleware not used
	return extractUserAgent(c)
}

// GetUserID retrieves user ID from context (set by auth middleware after token validation)
func GetUserID(c *gin.Context) *int64 {
	if userID, exists := c.Get(middleware.CtxKeyUserID); exists {
		if id, ok := userID.(int64); ok {
			return &id
		}
	}
	return nil
}

// LogFromContext logs an action using context values (IP, UA, user_id)
// This is the recommended way to log audit events - requires ContextMiddleware
// Errors are logged internally and not returned to avoid blocking business operations
func LogFromContext(c *gin.Context, repo repository.ActionLogRepository, actionType string, resourceType *string, resourceID *int64, source *string, metadata map[string]interface{}) error {
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
		UserID:       GetUserID(c),
		IPAddress:    GetClientIP(c),
		UserAgent:    GetUserAgent(c),
		Source:       source,
		Metadata:     metadataJSON,
	}

	if err := repo.LogAction(actionLog); err != nil {
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

// LogAction is a helper that logs an action with explicit user ID and source
// Use LogFromContext instead when user_id is in context from auth middleware
// Errors are logged internally and not returned to avoid blocking business operations
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

	if err := repo.LogAction(actionLog); err != nil {
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
