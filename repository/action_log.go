package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ActionLog is one entry for the audit.action_log table. The row id and its
// timestamp are the database's to assign and nothing reads them back.
type ActionLog struct {
	ActionType   string          `json:"actionType"`
	ResourceType *string         `json:"resourceType,omitempty"`
	ResourceID   *int64          `json:"resourceId,omitempty"`
	UserID       *int64          `json:"userId,omitempty"`
	IPAddress    *string         `json:"ipAddress,omitempty"`
	UserAgent    *string         `json:"userAgent,omitempty"`
	Source       *string         `json:"source,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// ActionLogRepository handles action log database operations
type ActionLogRepository interface {
	LogAction(ctx context.Context, log *ActionLog) error
}

type actionLogRepository struct {
	pool *pgxpool.Pool
}

// NewActionLogRepository creates a new action log repository
func NewActionLogRepository(pool *pgxpool.Pool) ActionLogRepository {
	return &actionLogRepository{pool: pool}
}

// LogAction appends one entry to the audit trail.
func (r *actionLogRepository) LogAction(ctx context.Context, log *ActionLog) error {
	payload, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal action log: %w", err)
	}

	if _, err := r.pool.Exec(ctx, "SELECT audit.log_action($1::jsonb)", payload); err != nil {
		return fmt.Errorf("failed to create action log: %w", err)
	}
	return nil
}
