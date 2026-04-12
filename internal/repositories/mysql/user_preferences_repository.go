// Package mysql provides MySQL/GORM implementations of repositories
//
//nolint:dupl // Parallel to user_permissions_repository by design; shared logic in user_scoped_helpers.go
package mysql

import (
	"context"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserPreferencesRepository implements repositories.UserPreferencesRepository using GORM
type UserPreferencesRepository struct {
	db *gorm.DB
}

// NewUserPreferencesRepository creates a new GORM-based user preferences repository
func NewUserPreferencesRepository(db *gorm.DB) repositories.UserPreferencesRepository {
	return &UserPreferencesRepository{db: db}
}

// Upsert creates or updates user preferences.
// Uses an explicit ON CONFLICT upsert instead of Save() so that rows deleted
// from the DB while the in-memory object has a non-zero UserID are re-inserted
// rather than silently no-op'd by an UPDATE that matches 0 rows.
func (r *UserPreferencesRepository) Upsert(ctx context.Context, prefs *models.UserPreferences) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).
		Create(prefs).Error
}

// Get retrieves user preferences
func (r *UserPreferencesRepository) Get(ctx context.Context, userID string) (*models.UserPreferences, error) {
	return firstByUserID[models.UserPreferences](ctx, r.db, userID)
}

// Delete removes user preferences
func (r *UserPreferencesRepository) Delete(ctx context.Context, userID string) error {
	return deleteByUserID(ctx, r.db, &models.UserPreferences{}, userID)
}
