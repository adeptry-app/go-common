package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ActionLog represents an entry in the audit.action_log table
type ActionLog struct {
	ID           int64           `json:"id" gorm:"primaryKey"`
	ActionType   string          `json:"action_type" gorm:"column:action_type"`
	ResourceType *string         `json:"resource_type,omitempty" gorm:"column:resource_type"`
	ResourceID   *int64          `json:"resource_id,omitempty" gorm:"column:resource_id"`
	UserID       *int64          `json:"user_id,omitempty" gorm:"column:user_id"`
	IPAddress    *string         `json:"ip_address,omitempty" gorm:"column:ip_address"`
	UserAgent    *string         `json:"user_agent,omitempty" gorm:"column:user_agent"`
	Source       *string         `json:"source,omitempty" gorm:"column:source"`
	Metadata     json.RawMessage `json:"metadata,omitempty" gorm:"column:metadata;type:jsonb"`
	CreatedAt    time.Time       `json:"created_at" gorm:"column:created_at"`
}

func (ActionLog) TableName() string {
	return "audit.action_log"
}

// ActionLogRepository handles action log database operations
type ActionLogRepository interface {
	LogAction(log *ActionLog) error
}

type actionLogRepository struct {
	db *gorm.DB
}

// NewActionLogRepository creates a new action log repository
func NewActionLogRepository(db *gorm.DB) ActionLogRepository {
	return &actionLogRepository{db: db}
}

// LogAction inserts a new action log entry
func (r *actionLogRepository) LogAction(log *ActionLog) error {
	if err := r.db.Create(log).Error; err != nil {
		return fmt.Errorf("failed to create action log: %w", err)
	}
	return nil
}
