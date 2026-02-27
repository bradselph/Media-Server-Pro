// Package mysql provides MySQL/GORM implementations of repositories
package mysql

import (
	"context"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
)

// UserPreferencesRepository implements repositories.UserPreferencesRepository using GORM
type UserPreferencesRepository struct {
	db *gorm.DB
}

// NewUserPreferencesRepository creates a new GORM-based user preferences repository
func NewUserPreferencesRepository(db *gorm.DB) repositories.UserPreferencesRepository {
	return &UserPreferencesRepository{db: db}
}

// Upsert creates or updates user preferences
func (r *UserPreferencesRepository) Upsert(ctx context.Context, prefs *models.UserPreferences) error {
	// Use GORM's Save which does INSERT or UPDATE
	return r.db.WithContext(ctx).Save(prefs).Error
}

// Get retrieves user preferences
func (r *UserPreferencesRepository) Get(ctx context.Context, userID string) (*models.UserPreferences, error) {
	var prefs models.UserPreferences
	err := r.db.WithContext(ctx).First(&prefs, "user_id = ?", userID).Error
	if err != nil {
		return nil, err
	}
	return &prefs, nil
}

// Delete removes user preferences
func (r *UserPreferencesRepository) Delete(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Delete(&models.UserPreferences{}, "user_id = ?", userID).Error
}
