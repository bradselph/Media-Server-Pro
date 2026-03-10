// Admin bootstrap: default admin password and admin user record.
package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
	"media-server-pro/pkg/models"
)

// ensureDefaultAdmin ensures the admin password hash is set in config and that
// the admin user record exists in the database so that AdminAuthenticate can
// retrieve the user ID when building a session.
func (m *Module) ensureDefaultAdmin() error {
	cfg := m.config.Get()
	if cfg.Admin.PasswordHash == "" {
		defaultPassword, err := generateRandomPassword(16)
		if err != nil {
			return fmt.Errorf("failed to generate default admin password: %w", err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if err := m.config.Update(func(c *config.Config) {
			c.Admin.PasswordHash = string(hash)
		}); err != nil {
			return fmt.Errorf("failed to update admin password: %w", err)
		}
		_, _ = fmt.Fprintf(os.Stderr, "\n*** Default admin password (change immediately): %s ***\n\n", defaultPassword)
		m.log.Warn("Created default admin with a generated password - check stderr output. PLEASE CHANGE THIS IMMEDIATELY")
	}
	return m.ensureAdminUserRecord()
}

// ensureAdminUserRecord creates the admin user row in the database if it does not already exist.
func (m *Module) ensureAdminUserRecord() error {
	cfg := m.config.Get()
	adminUsername := cfg.Admin.Username

	m.usersMu.RLock()
	_, exists := m.users[adminUsername]
	m.usersMu.RUnlock()
	if exists {
		return nil
	}

	ctx := context.Background()

	existingUser, err := m.userRepo.GetByUsername(ctx, adminUsername)
	if err == nil {
		m.usersMu.Lock()
		m.users[adminUsername] = existingUser
		m.usersMu.Unlock()
		return nil
	}

	adminUser := &models.User{
		ID:           generateID(),
		Username:     adminUsername,
		PasswordHash: cfg.Admin.PasswordHash,
		Salt:         "",
		Role:         models.RoleAdmin,
		Type:         "admin",
		Enabled:      true,
		CreatedAt:    time.Now(),
		Permissions: models.UserPermissions{
			CanStream:          true,
			CanDownload:        true,
			CanUpload:          true,
			CanDelete:          true,
			CanManage:          true,
			CanViewMature:      true,
			CanCreatePlaylists: true,
		},
		Preferences: models.UserPreferences{
			Theme:                "dark",
			ViewMode:             "grid",
			DefaultQuality:       "auto",
			AutoPlay:             false,
			PlaybackSpeed:        1.0,
			Volume:               1.0,
			Language:             "en",
			ResumePlayback:       true,
			ShowAnalytics:        true,
			ShowContinueWatching: true,
			ShowRecommended:      true,
			ShowTrending:         true,
		},
		WatchHistory: make([]models.WatchHistoryItem, 0),
	}

	if err := m.userRepo.Create(ctx, adminUser); err != nil {
		return fmt.Errorf("failed to create admin user record: %w", err)
	}

	m.usersMu.Lock()
	m.users[adminUsername] = adminUser
	m.usersMu.Unlock()

	m.log.Info("Created admin user record in database for: %s", adminUsername)
	return nil
}
