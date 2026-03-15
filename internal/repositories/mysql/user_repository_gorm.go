// Package mysql provides MySQL/GORM implementations of repository interfaces.
package mysql

import (
	"context"
	"errors"

	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"

	"gorm.io/gorm"
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
		if err := tx.Create(&user.Preferences).Error; err != nil {
			return err
		}

		return nil
	})
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

	perms, err := r.permsRepo.Get(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if perms != nil {
		user.Permissions = *perms
	}

	prefs, err := r.prefsRepo.Get(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if prefs != nil {
		user.Preferences = *prefs
	}

	return &user, nil
}

// GetByID retrieves a user by ID with all related data
func (r *UserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrUserNotFound
		}
		return nil, err
	}

	perms, err := r.permsRepo.Get(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if perms != nil {
		user.Permissions = *perms
	}

	prefs, err := r.prefsRepo.Get(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if prefs != nil {
		user.Preferences = *prefs
	}

	return &user, nil
}

// Update updates an existing user and related data using selective Updates()
// so password hash is only written when intentionally changed, reducing DB load and audit noise.
func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		userUpdates := map[string]interface{}{
			"username":       user.Username,
			"email":          user.Email,
			"role":           user.Role,
			"type":           user.Type,
			"enabled":        user.Enabled,
			"last_login":     user.LastLogin,
			"storage_used":   user.StorageUsed,
			"active_streams": user.ActiveStreams,
			"metadata":       user.Metadata,
			"watch_history":  user.WatchHistory,
		}
		if user.PasswordHash != "" {
			userUpdates["password_hash"] = user.PasswordHash
			userUpdates["salt"] = user.Salt
		}
		if err := tx.Model(user).Where("id = ?", user.ID).Updates(userUpdates).Error; err != nil {
			return err
		}

		// Update permissions inside the same transaction
		user.Permissions.UserID = user.ID
		if err := tx.Model(&user.Permissions).Where("user_id = ?", user.ID).Updates(map[string]interface{}{
			"can_stream":           user.Permissions.CanStream,
			"can_download":         user.Permissions.CanDownload,
			"can_upload":           user.Permissions.CanUpload,
			"can_delete":           user.Permissions.CanDelete,
			"can_manage":           user.Permissions.CanManage,
			"can_view_mature":      user.Permissions.CanViewMature,
			"can_create_playlists": user.Permissions.CanCreatePlaylists,
		}).Error; err != nil {
			return err
		}

		// Update preferences inside the same transaction
		user.Preferences.UserID = user.ID
		if err := tx.Model(&user.Preferences).Where("user_id = ?", user.ID).Updates(map[string]interface{}{
			"theme":                  user.Preferences.Theme,
			"view_mode":              user.Preferences.ViewMode,
			"default_quality":        user.Preferences.DefaultQuality,
			"auto_play":              user.Preferences.AutoPlay,
			"playback_speed":         user.Preferences.PlaybackSpeed,
			"volume":                 user.Preferences.Volume,
			"show_mature":            user.Preferences.ShowMature,
			"mature_preference_set":  user.Preferences.MaturePreferenceSet,
			"language":               user.Preferences.Language,
			"equalizer_preset":       user.Preferences.EqualizerPreset,
			"resume_playback":        user.Preferences.ResumePlayback,
			"show_analytics":         user.Preferences.ShowAnalytics,
			"items_per_page":        user.Preferences.ItemsPerPage,
			"sort_by":                user.Preferences.SortBy,
			"sort_order":             user.Preferences.SortOrder,
			"filter_category":       user.Preferences.FilterCategory,
			"filter_media_type":     user.Preferences.FilterMediaType,
			"custom_eq_presets":     user.Preferences.CustomEQPresets,
			"show_continue_watching": user.Preferences.ShowContinueWatching,
			"show_recommended":      user.Preferences.ShowRecommended,
			"show_trending":         user.Preferences.ShowTrending,
		}).Error; err != nil {
			return err
		}

		return nil
	})
}

// Delete removes a user. Related records (permissions, preferences, sessions)
// are automatically removed via ON DELETE CASCADE foreign key constraints
// defined in the database schema.
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.User{}, "id = ?", id).Error
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
		Where("id = ?", userID).
		Update("storage_used", gorm.Expr("COALESCE(storage_used, 0) + ?", delta)).Error
}
