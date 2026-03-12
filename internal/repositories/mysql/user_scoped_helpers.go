// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"

	"gorm.io/gorm"
)

// TODO: Bug — firstByUserID does not translate gorm.ErrRecordNotFound into a domain
// error. When UserPermissionsRepository.Get or UserPreferencesRepository.Get is called
// and no record exists, the raw GORM error propagates. In UserRepository.GetByUsername,
// the caller uses `if err == nil` to check success, so ErrRecordNotFound causes the
// permissions/preferences to silently use zero values. But direct callers of
// permsRepo.Get/prefsRepo.Get outside UserRepository will receive a GORM-specific error.
// Consider checking for gorm.ErrRecordNotFound and returning nil, nil (not found is
// not an error for optional sub-records) or a domain-specific sentinel.

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
