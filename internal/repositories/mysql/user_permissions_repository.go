// Package mysql provides MySQL/GORM implementations of repositories
//
//nolint:dupl // Parallel to user_preferences_repository by design; shared logic in user_scoped_helpers.go
package mysql

import (
	"context"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserPermissionsRepository implements repositories.UserPermissionsRepository using GORM
type UserPermissionsRepository struct {
	db *gorm.DB
}

// NewUserPermissionsRepository creates a new GORM-based user permissions repository
func NewUserPermissionsRepository(db *gorm.DB) repositories.UserPermissionsRepository {
	return &UserPermissionsRepository{db: db}
}

// Upsert creates or updates user permissions
func (r *UserPermissionsRepository) Upsert(ctx context.Context, perms *models.UserPermissions) error {
	// Uses an explicit ON CONFLICT upsert instead of Save() so that rows deleted
	// from the DB while the in-memory object has a non-zero UserID are re-inserted
	// rather than silently no-op'd by an UPDATE that matches 0 rows.
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(perms).Error
}

// Get retrieves user permissions
func (r *UserPermissionsRepository) Get(ctx context.Context, userID string) (*models.UserPermissions, error) {
	return firstByUserID[models.UserPermissions](ctx, r.db, userID)
}

// Delete removes user permissions
func (r *UserPermissionsRepository) Delete(ctx context.Context, userID string) error {
	return deleteByUserID(ctx, r.db, &models.UserPermissions{}, userID)
}
