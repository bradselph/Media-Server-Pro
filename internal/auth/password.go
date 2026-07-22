// Password updates and verification.

package auth

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
)

const errPasswordTooShort = "password must be at least 8 characters"

// UpdatePassword updates a user's password. Works on a copy to avoid data races;
// only updates the cache after successful DB persistence.
func (m *Module) UpdatePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	m.authFlowMu.Lock()
	defer m.authFlowMu.Unlock()
	m.userWriteMu.Lock()
	defer m.userWriteMu.Unlock()
	if len(newPassword) < 8 {
		return errors.New(errPasswordTooShort)
	}

	m.usersMu.RLock()
	user, exists := m.users[username]
	if !exists {
		m.usersMu.RUnlock()
		return ErrUserNotFound
	}
	currentHash := user.PasswordHash
	currentSalt := user.Salt
	m.usersMu.RUnlock()

	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(oldPassword+currentSalt)); err != nil {
		return ErrInvalidCredentials
	}

	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword+salt), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf(errHashPasswordFmt, err)
	}

	newHash := string(hash)
	adminConfigHashBefore := ""
	if m.config != nil {
		adminConfigHashBefore = m.config.Get().Admin.PasswordHash
	}

	// Hold write lock through CAS check + DB write + cache update to prevent
	// concurrent password changes from racing on the DB write.
	m.usersMu.Lock()
	user, exists = m.users[username]
	if !exists || user == nil {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}
	if user.PasswordHash != currentHash {
		m.usersMu.Unlock()
		return ErrInvalidCredentials
	}

	m.log.Info("Password updated for user: %s", username)

	// For the built-in admin, update the config-backed login credential first.
	// If the DB write then fails, roll the config hash back before returning.
	if err := m.syncAdminConfigPasswordIfNeeded(username, newPassword); err != nil {
		m.usersMu.Unlock()
		return err
	}
	adminConfigHashAfter := ""
	if m.config != nil {
		adminConfigHashAfter = m.config.Get().Admin.PasswordHash
	}
	if err := m.userRepo.UpdatePasswordHash(ctx, username, newHash, salt); err != nil {
		rollbackErr := m.rollbackAdminConfigPassword(username, adminConfigHashAfter, adminConfigHashBefore)
		m.usersMu.Unlock()
		m.log.Error("Failed to save user after password update: %v", err)
		return errors.Join(fmt.Errorf("password update failed to persist: %w", err), rollbackErr)
	}
	user.PasswordHash = newHash
	user.Salt = salt
	m.usersMu.Unlock()

	// Evict all sessions so the old password cannot be reused via an existing session.
	if err := m.evictSessionsForUser(ctx, username, "password changed by user"); err != nil {
		return fmt.Errorf("password updated but session revocation failed: %w", err)
	}

	return nil
}

// syncAdminConfigPasswordIfNeeded keeps cfg.Admin.PasswordHash — the credential
// AdminAuthenticate checks for the built-in admin, stored UNSALTED — in lockstep
// when the admin account's password is changed through the generic user paths
// (UpdatePassword / SetPassword). Those paths only write the DB/cache user record,
// which admin login never reads, so without this the new password would never
// work at login and the old one would keep working indefinitely. A no-op for
// every other username. Uses an unsalted hash to match AdminAuthenticate's
// bcrypt.CompareHashAndPassword(cfg.Admin.PasswordHash, password) check.
func (m *Module) syncAdminConfigPasswordIfNeeded(username, newPassword string) error {
	if m.config == nil {
		return nil
	}
	cfg := m.config.Get()
	if cfg.Admin.Username == "" || username != cfg.Admin.Username {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf(errHashPasswordFmt, err)
	}
	if err := m.config.Update(func(c *config.Config) {
		c.Admin.PasswordHash = string(hash)
	}); err != nil {
		return fmt.Errorf("failed to sync admin password to config: %w", err)
	}
	m.log.Info("Synced built-in admin login credential after password change")
	return nil
}

// rollbackAdminConfigPassword restores the config-backed built-in-admin hash
// after a later DB password write fails. The compare-and-swap avoids clobbering
// a concurrent successful password change.
func (m *Module) rollbackAdminConfigPassword(username, expectedHash, oldHash string) error {
	if m.config == nil || expectedHash == oldHash {
		return nil
	}
	cfg := m.config.Get()
	if cfg.Admin.Username == "" || username != cfg.Admin.Username {
		return nil
	}
	restored := false
	if err := m.config.Update(func(c *config.Config) {
		if c.Admin.PasswordHash == expectedHash {
			c.Admin.PasswordHash = oldHash
			restored = true
		}
	}); err != nil {
		return fmt.Errorf("failed to roll back admin login credential: %w", err)
	}
	if !restored && m.config.Get().Admin.PasswordHash != oldHash {
		return fmt.Errorf("admin login credential changed concurrently; rollback skipped")
	}
	return nil
}

// SetPassword sets a user's password (admin action, no old password required)
func (m *Module) SetPassword(ctx context.Context, username, newPassword string) error {
	m.authFlowMu.Lock()
	defer m.authFlowMu.Unlock()
	m.userWriteMu.Lock()
	defer m.userWriteMu.Unlock()
	if len(newPassword) < 8 {
		return errors.New(errPasswordTooShort)
	}

	var exists bool
	m.usersMu.RLock()
	_, exists = m.users[username]
	m.usersMu.RUnlock()
	if !exists {
		return ErrUserNotFound
	}

	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword+salt), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf(errHashPasswordFmt, err)
	}

	newHash := string(hash)
	adminConfigHashBefore := ""
	if m.config != nil {
		adminConfigHashBefore = m.config.Get().Admin.PasswordHash
	}

	// Hold write lock through DB write + cache update to prevent concurrent
	// password changes from racing.
	m.usersMu.Lock()
	user, exists := m.users[username]
	if !exists || user == nil {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	m.log.Info("Password set for user: %s (admin action)", username)

	if err := m.syncAdminConfigPasswordIfNeeded(username, newPassword); err != nil {
		m.usersMu.Unlock()
		return err
	}
	adminConfigHashAfter := ""
	if m.config != nil {
		adminConfigHashAfter = m.config.Get().Admin.PasswordHash
	}
	if err := m.userRepo.UpdatePasswordHash(ctx, username, newHash, salt); err != nil {
		rollbackErr := m.rollbackAdminConfigPassword(username, adminConfigHashAfter, adminConfigHashBefore)
		m.usersMu.Unlock()
		m.log.Error("Failed to save user after password set: %v", err)
		return errors.Join(fmt.Errorf("password set failed to persist: %w", err), rollbackErr)
	}
	user.PasswordHash = newHash
	user.Salt = salt
	m.usersMu.Unlock()

	// Evict all sessions so the old password cannot be reused via an existing session.
	if err := m.evictSessionsForUser(ctx, username, "password reset by admin"); err != nil {
		return fmt.Errorf("password updated but session revocation failed: %w", err)
	}

	return nil
}

// ChangeAdminPassword verifies the current admin password and replaces it with a new one.
func (m *Module) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error {
	m.authFlowMu.Lock()
	defer m.authFlowMu.Unlock()
	m.userWriteMu.Lock()
	defer m.userWriteMu.Unlock()
	cfg := m.config.Get()

	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	if len(newPassword) < 8 {
		return errors.New(errPasswordTooShort)
	}

	configHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}
	dbSalt := generateSalt()
	dbHash, err := bcrypt.GenerateFromPassword([]byte(newPassword+dbSalt), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash new database password: %w", err)
	}

	// The built-in admin credential is intentionally represented twice: an
	// unsalted bcrypt hash in config for the admin-login gate and a separately
	// salted hash in the user row for the regular authentication path. Commit
	// both while authFlowMu excludes new sessions, rolling config back if the
	// targeted user-row write fails.
	m.usersMu.RLock()
	adminUser := m.users[cfg.Admin.Username]
	m.usersMu.RUnlock()
	if adminUser == nil {
		return ErrUserNotFound
	}

	// Compare-and-swap under the config write lock (config.Update runs the
	// closure while holding it): only replace the hash if it still matches the
	// value we verified above. Without this, two concurrent admin password
	// changes can both pass the verify and the second silently clobbers the first.
	oldHash := cfg.Admin.PasswordHash
	applied := false
	if err := m.config.Update(func(c *config.Config) {
		if c.Admin.PasswordHash == oldHash {
			c.Admin.PasswordHash = string(configHash)
			applied = true
		}
	}); err != nil {
		return fmt.Errorf("failed to persist new admin password: %w", err)
	}
	if !applied {
		return ErrInvalidCredentials
	}
	if err := m.userRepo.UpdatePasswordHash(ctx, cfg.Admin.Username, string(dbHash), dbSalt); err != nil {
		rollbackErr := m.rollbackAdminConfigPassword(cfg.Admin.Username, string(configHash), oldHash)
		return errors.Join(fmt.Errorf("failed to persist admin user credential: %w", err), rollbackErr)
	}
	m.usersMu.Lock()
	if current := m.users[cfg.Admin.Username]; current != nil {
		current.PasswordHash = string(dbHash)
		current.Salt = dbSalt
	}
	m.usersMu.Unlock()

	// Evict all existing admin sessions so the old password can no longer be used.
	if err := m.evictSessionsForUser(ctx, cfg.Admin.Username, "admin password changed"); err != nil {
		return fmt.Errorf("admin password changed but session revocation failed: %w", err)
	}

	m.log.Info("Admin password changed successfully")
	return nil
}

// VerifyPassword verifies a user's password without creating a session.
// Uses the same cache-refresh as Authenticate so password changes from another instance are visible.
func (m *Module) VerifyPassword(ctx context.Context, username, password string) error {
	user, err := m.getOrLoadUser(ctx, username)
	if err != nil || user == nil {
		return ErrUserNotFound
	}
	return m.verifyPasswordWithCacheRefresh(ctx, user, &creds{Username: username, Password: password})
}
