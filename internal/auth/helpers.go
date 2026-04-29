// Package auth provides helpers and small utilities for authentication.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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

// GetActiveSessions returns active sessions for a user.
// Returns copies so callers cannot mutate shared cache entries.
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
	return sessions
}
