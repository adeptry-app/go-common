package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// CheckRowsAffected returns gorm.ErrRecordNotFound if no rows were affected.
// Use for delete/update operations that should fail if target doesn't exist.
func CheckRowsAffected(result *gorm.DB) error {
	if result == nil {
		return errors.New("CheckRowsAffected: result is nil")
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SafeUpdater provides safe update operations for GORM repositories.
// Checks existence before updating to ensure idempotent updates (no false 404s).
type SafeUpdater struct {
	db *gorm.DB
}

// NewSafeUpdater creates a new SafeUpdater instance.
func NewSafeUpdater(db *gorm.DB) *SafeUpdater {
	return &SafeUpdater{db: db}
}

// Update performs an update excluding system fields (ID, CreatedAt, UpdatedAt).
// Uses Select("*") with Updates to include zero-value fields (e.g., bool false).
// Checks existence first to ensure idempotent updates (no false 404s).
func (s *SafeUpdater) Update(ctx context.Context, model interface{}, id int64) error {
	// Check existence using COUNT to avoid loading data
	var count int64
	if err := s.db.WithContext(ctx).Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}

	db := s.db.WithContext(ctx).Model(model).Where("id = ?", id)

	// Now perform update - RowsAffected=0 is OK (idempotent)
	// Select("*") ensures zero-value fields (like bool false) are included in the update
	return db.Select("*").Omit("ID", "CreatedAt", "UpdatedAt").Updates(model).Error
}
