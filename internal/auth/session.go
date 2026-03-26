// Session lifecycle and validation.
package auth

import (
	"context"
	"fmt"
	"time"

	"media-server-pro/pkg/models"
)

// CreateSessionParams holds parameters for creating a session without password verification (avoids string-heavy args).
type CreateSessionParams struct {
	Username  string
	IPAddress string
	UserAgent string
}

// sessionRequestContext holds request context for internal session creation (avoids string-heavy args).
type sessionRequestContext struct {
	IPAddress string
	UserAgent string
}

// deleteExpiredFromMap removes entries whose expiry time is before now and returns the count deleted.
func deleteExpiredFromMap[V any](m map[string]V, now time.Time, expiresAt func(V) time.Time) int {
	n := 0
	for id, v := range m {
		if expiresAt(v).Before(now) {
			delete(m, id)
			n++
		}
	}
	return n
}

// cleanupExpiredLoginAttempts removes login attempts older than twice the lockout duration.
// Caller must not hold sessionsMu; attemptsMu is taken internally.
func (m *Module) cleanupExpiredLoginAttempts() {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()
	cfg := m.config.Get()
	cutoff := time.Now().Add(-cfg.Auth.LockoutDuration * 2)
	for ip, attempt := range m.loginAttempts {
		if attempt.FirstTry.Before(cutoff) {
			delete(m.loginAttempts, ip)
		}
	}
}

// cleanupExpiredSessionsCache removes expired sessions from in-memory maps and login attempts.
// Caller is responsible for calling sessionRepo.DeleteExpired once if DB cleanup is desired.
func (m *Module) cleanupExpiredSessionsCache() {
	m.sessionsMu.Lock()
	now := time.Now()
	expired := deleteExpiredFromMap(m.sessions, now, func(s *models.Session) time.Time { return s.ExpiresAt })
	expired += deleteExpiredFromMap(m.adminSessions, now, func(s *models.AdminSession) time.Time { return s.ExpiresAt })
	m.sessionsMu.Unlock()

	if expired > 0 {
		m.log.Debug("Cleaned up %d expired sessions from cache", expired)
	}
	m.cleanupExpiredLoginAttempts()
}

// cleanupExpiredSessions removes expired sessions from DB and cache (used by internal ticker).
func (m *Module) cleanupExpiredSessions() {
	if err := m.sessionRepo.DeleteExpired(context.Background()); err != nil {
		m.log.Warn("Failed to cleanup expired sessions: %v", err)
	}
	m.cleanupExpiredSessionsCache()
}

// CleanupExpiredSessions removes expired sessions from storage and cache (public method for background tasks).
func (m *Module) CleanupExpiredSessions(ctx context.Context) error {
	if err := m.sessionRepo.DeleteExpired(ctx); err != nil {
		return err
	}
	m.cleanupExpiredSessionsCache()
	return nil
}

// getOrLoadSession returns the session from cache or loads it from the repository and caches it.
func (m *Module) getOrLoadSession(ctx context.Context, sessionID string) (*models.Session, error) {
	m.sessionsMu.RLock()
	session, exists := m.sessions[sessionID]
	m.sessionsMu.RUnlock()
	if exists {
		return session, nil
	}
	session, err := m.sessionRepo.Get(ctx, sessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	m.sessionsMu.Lock()
	m.sessions[sessionID] = session
	m.sessionsMu.Unlock()
	return session, nil
}

// removeExpiredSession deletes the session from repository and cache.
func (m *Module) removeExpiredSession(ctx context.Context, sessionID string) {
	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete expired session: %v", err)
	}
	m.sessionsMu.Lock()
	delete(m.sessions, sessionID)
	m.sessionsMu.Unlock()
}

// ValidateSession validates a session and returns the associated user
func (m *Module) ValidateSession(ctx context.Context, sessionID string) (*models.Session, *models.User, error) {
	session, err := m.getOrLoadSession(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if session.IsExpired() {
		m.removeExpiredSession(ctx, sessionID)
		return nil, nil, ErrSessionExpired
	}
	user, err := m.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}
	if !user.Enabled {
		return nil, nil, ErrAccountDisabled
	}
	// Update LastActivity under write lock to avoid data race with concurrent ValidateSession calls
	m.sessionsMu.Lock()
	session.LastActivity = time.Now()
	sessionCopy := *session
	m.sessionsMu.Unlock()
	// Persist LastActivity in background using the safe copy
	go func() {
		if err := m.sessionRepo.Update(context.Background(), &sessionCopy); err != nil {
			m.log.Warn("Failed to persist session LastActivity for %s: %v", sessionCopy.Username, err)
		}
	}()
	return session, user, nil
}

// Logout invalidates a session
func (m *Module) Logout(ctx context.Context, sessionID string) error {
	m.sessionsMu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		m.log.Info("User logged out: %s", session.Username)
		delete(m.sessions, sessionID)
	}
	m.sessionsMu.Unlock()

	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete session from repository: %v", err)
	}

	if !exists {
		return ErrSessionNotFound
	}
	return nil
}

// LogoutAdmin invalidates an admin session
func (m *Module) LogoutAdmin(ctx context.Context, sessionID string) error {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()

	session, exists := m.adminSessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete admin session from repository: %v", err)
	}

	m.log.Info("Admin logged out: %s", session.Username)
	delete(m.adminSessions, sessionID)
	return nil
}

// CreateSessionForUser creates a new session for a user without password verification.
func (m *Module) CreateSessionForUser(ctx context.Context, params *CreateSessionParams) (*models.Session, error) {
	m.usersMu.RLock()
	user, exists := m.users[params.Username]
	m.usersMu.RUnlock()

	if !exists {
		return nil, ErrUserNotFound
	}

	if !user.Enabled {
		return nil, ErrAccountDisabled
	}

	return m.createSession(ctx, user, &sessionRequestContext{IPAddress: params.IPAddress, UserAgent: params.UserAgent})
}

// createSession creates a new session for a user.
// Returns an error if the session cannot be persisted to the database.
func (m *Module) createSession(ctx context.Context, user *models.User, req *sessionRequestContext) (*models.Session, error) {
	cfg := m.config.Get()

	session := &models.Session{
		ID:           generateSessionID(),
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(cfg.Auth.SessionTimeout),
		LastActivity: time.Now(),
		IPAddress:    req.IPAddress,
		UserAgent:    req.UserAgent,
	}

	if err := m.sessionRepo.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to persist session: %w", err)
	}

	m.sessionsMu.Lock()
	m.sessions[session.ID] = session
	m.sessionsMu.Unlock()

	return session, nil
}
