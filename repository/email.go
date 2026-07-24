package repository

import (
	"context"
	"fmt"

	"github.com/adeptry-app/go-common/models"
	"gorm.io/gorm"
)

// UpdateEmailStatus updates the status of an email record. last_error is set to
// lastError as given (nil clears it); sent_at is stamped only for the sent
// status and attempts incremented only for failed.
func UpdateEmailStatus(db *gorm.DB, ctx context.Context, id int64, status string, lastError *string) error {
	if !models.ValidEmailStatus(status) {
		return fmt.Errorf("invalid email status: %q", status)
	}

	updates := map[string]interface{}{
		"status":     status,
		"last_error": lastError,
	}
	if status == models.EmailStatusSent {
		updates["sent_at"] = db.NowFunc()
	}
	if status == models.EmailStatusFailed {
		updates["attempts"] = gorm.Expr("attempts + 1")
	}

	result := db.WithContext(ctx).
		Model(&models.Email{}).
		Where("id = ?", id).
		Updates(updates)

	return CheckRowsAffected(result)
}
