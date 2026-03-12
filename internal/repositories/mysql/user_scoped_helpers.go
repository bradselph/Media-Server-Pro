// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"

	"gorm.io/gorm"
)

// firstByUserID loads the first record with the given user_id into dest.
// dest must be a pointer to a struct (e.g. &models.UserPermissions{}).
func firstByUserID[T any](ctx context.Context, db *gorm.DB, userID string) (*T, error) {
	var dest T
	if err := db.WithContext(ctx).First(&dest, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &dest, nil
}

// deleteByUserID deletes records for the given user_id.
// model is an empty struct instance (e.g. &models.UserPermissions{}).
func deleteByUserID(ctx context.Context, db *gorm.DB, model any, userID string) error {
	return db.WithContext(ctx).Delete(model, "user_id = ?", userID).Error
}
