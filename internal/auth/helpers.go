// Package auth provides helpers and small utilities for authentication.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"media-server-pro/pkg/models"
)

// generateID returns a new hex-encoded random ID.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// generateSessionID returns a new URL-safe base64 session ID.
func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return base64.URLEncoding.EncodeToString(b)
}

// generateSalt returns a new salt (same as generateID).
func generateSalt() string {
	return generateID()
}

// generateRandomPassword creates a cryptographically random password of the given length.
func generateRandomPassword(length int) (string, error) {
	if length < 1 || length > 1024 {
		return "", fmt.Errorf("password length must be between 1 and 1024")
	}
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range b {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		b[i] = charset[num.Int64()]
	}
	return string(b), nil
}

// GenerateSecurePassword generates a cryptographically secure random password.
func (m *Module) GenerateSecurePassword(length int) (string, error) {
	return generateRandomPassword(length)
}

// GetActiveSessions returns active sessions for a user, looking in both the
// regular and admin in-memory caches so an admin's sessions are also visible
// to the admin Users tab. Returns copies so callers cannot mutate shared
// cache entries.
func (m *Module) GetActiveSessions(username string) []*models.Session {
	m.sessionsMu.RLock()
	defer m.sessionsMu.RUnlock()

	sessions := make([]*models.Session, 0)
	for _, session := range m.sessions {
		if session.Username == username && !session.IsExpired() {
			tmp := *session
			sessions = append(sessions, &tmp)
		}
	}
	for _, admin := range m.adminSessions {
		if admin.Username == username && !admin.IsExpired() {
			tmp := admin.Session
			sessions = append(sessions, &tmp)
		}
	}
	return sessions
}

// GetActiveSessionsByUserID returns all active sessions belonging to the given
// user, looking in both the regular and admin in-memory caches so admin users
// can also enumerate their devices. Returns copies.
func (m *Module) GetActiveSessionsByUserID(userID string) []*models.Session {
	m.sessionsMu.RLock()
	defer m.sessionsMu.RUnlock()

	sessions := make([]*models.Session, 0)
	for _, session := range m.sessions {
		if session.UserID == userID && !session.IsExpired() {
			tmp := *session
			sessions = append(sessions, &tmp)
		}
	}
	for _, admin := range m.adminSessions {
		if admin.UserID == userID && !admin.IsExpired() {
			tmp := admin.Session
			sessions = append(sessions, &tmp)
		}
	}
	return sessions
}

// RevokeUserSession deletes the named session if it belongs to userID. Returns
// ErrSessionNotFound when the session does not exist or does not belong to the
// caller — handlers must NOT distinguish between the two so a user cannot probe
// other users' session IDs.
func (m *Module) RevokeUserSession(ctx context.Context, userID, sessionID string) error {
	m.sessionsMu.RLock()
	var owner string
	if s, ok := m.sessions[sessionID]; ok {
		owner = s.UserID
	} else if a, ok := m.adminSessions[sessionID]; ok {
		owner = a.UserID
	}
	m.sessionsMu.RUnlock()
	if owner == "" || owner != userID {
		return ErrSessionNotFound
	}
	if err := m.Logout(ctx, sessionID); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			// Already gone from cache; try admin path for completeness.
			return m.LogoutAdmin(ctx, sessionID)
		}
		return err
	}
	return nil
}
