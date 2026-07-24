package repository

import (
	"context"
	"fmt"

	"github.com/adeptry-app/go-common/models"
	"gorm.io/gorm"
)

// MarkEmail writes a worker status through messaging.mark_email, which only
// touches in-flight rows so a slow worker cannot overwrite a row the sweeper
// already abandoned. A no-op (row gone or terminal) is not an error.
func MarkEmail(db *gorm.DB, ctx context.Context, id int64, status string, lastError *string) error {
	switch status {
	case models.EmailStatusSent, models.EmailStatusFailed, models.EmailStatusRetrying:
	default:
		return fmt.Errorf("invalid worker email status: %q", status)
	}

	return db.WithContext(ctx).
		Exec("SELECT messaging.mark_email(?, ?, ?)", id, status, lastError).Error
}
