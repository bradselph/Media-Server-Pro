// Package mysql provides MySQL/GORM implementations of repository interfaces.
package mysql

import (
	"context"
	"encoding/json"
	"errors"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserRepository implements repositories.UserRepository using GORM
type UserRepository struct {
	db        *gorm.DB
	prefsRepo repositories.UserPreferencesRepository
	permsRepo repositories.UserPermissionsRepository
}

// NewUserRepository creates a new GORM-backed user repository
func NewUserRepository(db *gorm.DB) repositories.UserRepository {
	return &UserRepository{
		db:        db,
		prefsRepo: NewUserPreferencesRepository(db),
		permsRepo: NewUserPermissionsRepository(db),
	}
}

// Create inserts a new user with permissions and preferences
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Create user record
		if err := tx.Create(user).Error; err != nil {
			return err
		}

		// Create permissions
		user.Permissions.UserID = user.ID
		if err := tx.Create(&user.Permissions).Error; err != nil {
			return err
		}

		// Create preferences
		user.Preferences.UserID = user.ID
		return tx.Create(&user.Preferences).Error
	})
}

// loadRelated populates Permissions and Preferences on a fetched user.
func (r *UserRepository) loadRelated(ctx context.Context, user *models.User) error {
	perms, err := r.permsRepo.Get(ctx, user.ID)
	if err != nil {
		return err
	}
	if perms != nil {
		user.Permissions = *perms
	}

	prefs, err := r.prefsRepo.Get(ctx, user.ID)
	if err != nil {
		return err
	}
	if prefs != nil {
		user.Preferences = *prefs
	}
	return nil
}

// GetByUsername retrieves a user by username with all related data.
// Errors from permsRepo.Get and prefsRepo.Get are propagated so transient DB failures fail the request.
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, "username = ?", username).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrUserNotFound
		}
		return nil, err
	}
	if err := r.loadRelated(ctx, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByID retrieves a user by ID with all related data.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, sqlIDEq, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrUserNotFound
		}
		return nil, err
	}
	if err := r.loadRelated(ctx, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// marshalJSONParam marshals a value to a JSON string for use in GORM Updates maps.
// database/sql cannot bind complex Go types (maps, slices) directly; they must be
// pre-serialized to JSON strings. Returns nil (SQL NULL) if v is nil.
func marshalJSONParam(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return string(b)
}

// Update updates an existing user and related data using selective Updates()
// so password hash is only written when intentionally changed, reducing DB load and audit noise.
func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// JSON fields must be pre-serialized: database/sql cannot bind map/slice types directly.
		userUpdates := map[string]any{
			"username":            user.Username,
			"email":               user.Email,
			"role":                user.Role,
			"type":                user.Type,
			"enabled":             user.Enabled,
			"last_login":          user.LastLogin,
			"previous_last_login": user.PreviousLastLogin,
			"storage_used":        user.StorageUsed,
			"active_streams":      user.ActiveStreams,
			"metadata":            marshalJSONParam(user.Metadata),
			"watch_history":       marshalJSONParam(user.WatchHistory),
		}
		if user.PasswordHash != "" {
			userUpdates["password_hash"] = user.PasswordHash
			userUpdates["salt"] = user.Salt
		}
		if err := tx.Model(&models.User{}).Where(sqlIDEq, user.ID).Updates(userUpdates).Error; err != nil {
			return err
		}

		// Upsert permissions: INSERT … ON DUPLICATE KEY UPDATE so a missing row
		// gets created rather than silently skipping the update.
		user.Permissions.UserID = user.ID
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"can_stream", "can_download", "can_upload", "can_delete",
				"can_manage", "can_view_mature", "can_create_playlists",
			}),
		}).Create(&user.Permissions).Error; err != nil {
			return err
		}

		// Upsert preferences: same pattern. GORM's serializer:json tag handles
		// CustomEQPresets automatically when using Create.
		user.Preferences.UserID = user.ID
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"theme", "view_mode", "default_quality", "auto_play",
				"playback_speed", "volume", "show_mature", "mature_preference_set",
				"language", "equalizer_preset", "resume_playback", "show_analytics",
				"items_per_page", "sort_by", "sort_order", "filter_category",
				"filter_media_type", "custom_eq_presets",
				"show_continue_watching", "show_recommended", "show_trending",
				"skip_interval", "shuffle_enabled", "show_buffer_bar", "download_prompt",
			}),
		}).Create(&user.Preferences).Error
	})
}

// UpdatePasswordHash writes only password_hash and salt for the named user,
// eliminating the full-snapshot race present in Update.
func (r *UserRepository) UpdatePasswordHash(ctx context.Context, username, passwordHash, salt string) error {
	result := r.db.WithContext(ctx).Model(&models.User{}).
		Where("username = ?", username).
		Updates(map[string]any{
			"password_hash": passwordHash,
			"salt":          salt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrUserNotFound
	}
	return nil
}

// Delete removes a user. Related records (permissions, preferences, sessions)
// are automatically removed via ON DELETE CASCADE foreign key constraints
// defined in the database schema.
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&models.User{}, sqlIDEq, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return repositories.ErrUserNotFound
	}
	return nil
}

// List retrieves all users with permissions and preferences (batch-loaded to avoid N+1).
func (r *UserRepository) List(ctx context.Context) ([]*models.User, error) {
	var users []*models.User
	if err := r.db.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return users, nil
	}

	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}

	var allPerms []models.UserPermissions
	if err := r.db.WithContext(ctx).Where("user_id IN ?", ids).Find(&allPerms).Error; err != nil {
		return nil, err
	}
	permsByUser := make(map[string]*models.UserPermissions)
	for i := range allPerms {
		permsByUser[allPerms[i].UserID] = &allPerms[i]
	}

	var allPrefs []models.UserPreferences
	if err := r.db.WithContext(ctx).Where("user_id IN ?", ids).Find(&allPrefs).Error; err != nil {
		return nil, err
	}
	prefsByUser := make(map[string]*models.UserPreferences)
	for i := range allPrefs {
		prefsByUser[allPrefs[i].UserID] = &allPrefs[i]
	}

	for _, user := range users {
		if p := permsByUser[user.ID]; p != nil {
			user.Permissions = *p
		}
		if p := prefsByUser[user.ID]; p != nil {
			user.Preferences = *p
		}
	}
	return users, nil
}

// IncrementStorageUsed atomically adds delta to the user's storage_used.
func (r *UserRepository) IncrementStorageUsed(ctx context.Context, userID string, delta int64) error {
	return r.db.WithContext(ctx).Model(&models.User{}).
		Where(sqlIDEq, userID).
		Update("storage_used", gorm.Expr("GREATEST(COALESCE(storage_used, 0) + ?, 0)", delta)).Error
}
