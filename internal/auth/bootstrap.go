// Admin bootstrap: default admin password and admin user record.

package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		// Write to file with 0o600 to avoid password in systemd journal/container logs
		dataDir := cfg.Directories.Data
		if dataDir == "" {
			dataDir = "data"
		}
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			m.log.Warn("Cannot create data dir for password file: %v", err)
		} else {
			path := filepath.Join(dataDir, "admin-initial-password.txt")
			if err := os.WriteFile(path, []byte(defaultPassword+"\n"), 0o600); err != nil {
				m.log.Warn("Cannot write admin password file: %v", err)
			} else {
				m.log.Warn("Default admin password written to %s (chmod 0o600) - PLEASE CHANGE IMMEDIATELY", path)
			}
		}
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
		m.usersByID[existingUser.ID] = existingUser
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
	m.usersByID[adminUser.ID] = adminUser
	m.usersMu.Unlock()

	m.log.Info("Created admin user record in database for: %s", adminUsername)
	return nil
}
