// Password updates and verification.
package auth

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
)

// UpdatePassword updates a user's password
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

	m.usersMu.Lock()
	user.PasswordHash = string(hash)
	user.Salt = salt
	m.usersMu.Unlock()

	m.log.Info("Password updated for user: %s", username)

	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after password update: %v", err)
		return fmt.Errorf("password updated in memory but failed to persist: %w", err)
	}
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

	m.usersMu.Lock()
	user.PasswordHash = string(hash)
	user.Salt = salt
	m.usersMu.Unlock()

	m.log.Info("Password set for user: %s (admin action)", username)

	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after password set: %v", err)
		return fmt.Errorf("password set in memory but failed to persist: %w", err)
	}
	return nil
}

// ChangeAdminPassword verifies the current admin password and replaces it with a new one.
func (m *Module) ChangeAdminPassword(_ context.Context, currentPassword, newPassword string) error {
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

	m.log.Info("Admin password changed successfully")
	return nil
}

// VerifyPassword verifies a user's password without creating a session
func (m *Module) VerifyPassword(username, password string) error {
	m.usersMu.RLock()
	user, exists := m.users[username]
	m.usersMu.RUnlock()

	if !exists {
		return ErrUserNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password+user.Salt)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}
