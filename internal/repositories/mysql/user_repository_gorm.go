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

// GetByUsername retrieves a user by username with all related data
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, "username = ?", username).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repositories.ErrUserNotFound
		}
		return nil, err
	}

	// Load permissions
	perms, err := r.permsRepo.Get(ctx, user.ID)
	if err == nil {
		user.Permissions = *perms
	}

	// Load preferences
	prefs, err := r.prefsRepo.Get(ctx, user.ID)
	if err == nil {
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

	// Load permissions
	perms, err := r.permsRepo.Get(ctx, user.ID)
	if err == nil {
		user.Permissions = *perms
	}

	// Load preferences
	prefs, err := r.prefsRepo.Get(ctx, user.ID)
	if err == nil {
		user.Preferences = *prefs
	}

	return &user, nil
}

// Update updates an existing user and related data
func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update user record
		if err := tx.Save(user).Error; err != nil {
			return err
		}

		// Update permissions inside the same transaction so all three writes
		// succeed or fail together. Using tx instead of r.permsRepo.Upsert()
		// ensures atomicity — the sub-repositories hold their own *gorm.DB
		// reference and would execute outside this transaction boundary.
		user.Permissions.UserID = user.ID
		if err := tx.Save(&user.Permissions).Error; err != nil {
			return err
		}

		// Update preferences inside the same transaction
		user.Preferences.UserID = user.ID
		if err := tx.Save(&user.Preferences).Error; err != nil {
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

// List retrieves all users
func (r *UserRepository) List(ctx context.Context) ([]*models.User, error) {
	var users []*models.User
	err := r.db.WithContext(ctx).Find(&users).Error
	if err != nil {
		return nil, err
	}

	// Load permissions and preferences for each user
	for _, user := range users {
		perms, err := r.permsRepo.Get(ctx, user.ID)
		if err == nil {
			user.Permissions = *perms
		}

		prefs, err := r.prefsRepo.Get(ctx, user.ID)
		if err == nil {
			user.Preferences = *prefs
		}
	}

	return users, nil
}
