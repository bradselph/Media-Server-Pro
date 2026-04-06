// Password updates and verification.
package auth

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
)

// UpdatePassword updates a user's password. Works on a copy to avoid data races;
// only updates the cache after successful DB persistence.
func (m *Module) UpdatePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
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

	// Work on a copy; only update cache after DB success to avoid cache/DB divergence.
	m.usersMu.RLock()
	user, exists = m.users[username]
	if !exists || user == nil {
		m.usersMu.RUnlock()
		return ErrUserNotFound
	}
	if user.PasswordHash != currentHash {
		m.usersMu.RUnlock()
		return ErrUserNotFound
	}
	userCopy := *user
	m.usersMu.RUnlock()

	userCopy.PasswordHash = string(hash)
	userCopy.Salt = salt

	m.log.Info("Password updated for user: %s", username)

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after password update: %v", err)
		return fmt.Errorf("password update failed to persist: %w", err)
	}
	m.usersMu.Lock()
	if u, ok := m.users[username]; ok && u.PasswordHash == currentHash {
		u.PasswordHash = userCopy.PasswordHash
		u.Salt = userCopy.Salt
	}
	m.usersMu.Unlock()
	return nil
}

// SetPassword sets a user's password (admin action, no old password required)
func (m *Module) SetPassword(ctx context.Context, username, newPassword string) error {
	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	m.usersMu.RLock()
	user, exists := m.users[username]
	m.usersMu.RUnlock()
	if !exists {
		return ErrUserNotFound
	}

	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword+salt), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf(errHashPasswordFmt, err)
	}

	// Work on a copy; only update cache after DB success to avoid cache/DB divergence.
	userCopy := *user
	userCopy.PasswordHash = string(hash)
	userCopy.Salt = salt

	m.log.Info("Password set for user: %s (admin action)", username)

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after password set: %v", err)
		return fmt.Errorf("password set failed to persist: %w", err)
	}
	m.usersMu.Lock()
	user.PasswordHash = userCopy.PasswordHash
	user.Salt = userCopy.Salt
	m.usersMu.Unlock()
	return nil
}

// ChangeAdminPassword verifies the current admin password and replaces it with a new one.
func (m *Module) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error {
	cfg := m.config.Get()

	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := m.config.Update(func(c *config.Config) {
		c.Admin.PasswordHash = string(hash)
	}); err != nil {
		return fmt.Errorf("failed to persist new admin password: %w", err)
	}

	// Evict all existing admin sessions so the old password can no longer be used.
	m.evictSessionsForUser(ctx, cfg.Admin.Username, "admin password changed")

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
