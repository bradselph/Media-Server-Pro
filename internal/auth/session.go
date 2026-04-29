// Session lifecycle and validation.

package auth

import (
	"context"
	"errors"
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

// cleanupExpiredLoginAttempts removes stale login attempt records.
// Evicts: (a) any entry whose FirstTry is older than 2× LockoutDuration regardless of count,
// and (b) non-locked entries whose window has expired (LockedAt == nil and FirstTry older
// than LockoutDuration) — prevents the map growing unboundedly under IP-rotation attacks.
// Caller must not hold sessionsMu; attemptsMu is taken internally.
func (m *Module) cleanupExpiredLoginAttempts() {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()
	cfg := m.config.Get()
	now := time.Now()
	hardCutoff := now.Add(-cfg.Auth.LockoutDuration * 2)
	windowCutoff := now.Add(-cfg.Auth.LockoutDuration)
	for ip, attempt := range m.loginAttempts {
		if attempt.FirstTry.Before(hardCutoff) {
			delete(m.loginAttempts, ip)
		} else if attempt.LockedAt == nil && attempt.FirstTry.Before(windowCutoff) {
			// Window expired and never triggered a lockout — safe to evict.
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
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		// Propagate DB errors (timeouts, connection failures) so callers can
		// return 503 instead of 401 and avoid clearing the user's session cookie.
		return nil, err
	}
	m.sessionsMu.Lock()
	if existing, ok := m.sessions[sessionID]; ok {
		m.sessionsMu.Unlock()
		return existing, nil
	}
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
	// Read and update session fields under write lock to prevent data race
	// with concurrent ValidateSession calls that write LastActivity.
	m.sessionsMu.Lock()
	if session.IsExpired() {
		m.sessionsMu.Unlock()
		m.removeExpiredSession(ctx, sessionID)
		return nil, nil, ErrSessionExpired
	}
	userID := session.UserID
	session.LastActivity = time.Now()
	sessionCopy := *session
	m.sessionsMu.Unlock()
	user, err := m.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if !user.Enabled {
		return nil, nil, ErrAccountDisabled
	}
	// Persist LastActivity in background using the safe copy, bounded by a
	// semaphore to prevent goroutine accumulation under sustained load.
	// ErrSessionNotFound is suppressed: the cleanup ticker may have deleted the
	// DB row between the lock release above and the goroutine executing.
	select {
	case m.sessionUpdateSem <- struct{}{}:
		go func() { //nolint:gosec // G118: background context intentional for async fire-and-forget DB write
			defer func() { <-m.sessionUpdateSem }()
			if err := m.sessionRepo.Update(context.Background(), &sessionCopy); err != nil && !errors.Is(err, ErrSessionNotFound) {
				m.log.Warn("Failed to persist session LastActivity for %s: %v", sessionCopy.Username, err)
			}
		}()
	default:
		m.log.Debug("Session LastActivity persist skipped for %s (semaphore full)", sessionCopy.Username)
	}
	// Return the copy, not the shared pointer from the map, to prevent concurrent
	// callers from racing on the same *Session after the lock is released.
	return &sessionCopy, user, nil
}

// Logout invalidates a session. Checks both m.sessions and m.adminSessions so
// that admin sessions are always revocable regardless of which map they landed in.
func (m *Module) Logout(ctx context.Context, sessionID string) error {
	var username string
	var exists bool

	m.sessionsMu.Lock()
	if session, ok := m.sessions[sessionID]; ok {
		username = session.Username
		delete(m.sessions, sessionID)
		exists = true
	} else if adminSession, ok := m.adminSessions[sessionID]; ok {
		username = adminSession.Username
		delete(m.adminSessions, sessionID)
		exists = true
	}
	m.sessionsMu.Unlock()

	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete session from repository: %v", err)
	}

	if !exists {
		return ErrSessionNotFound
	}
	m.log.Info("User logged out: %s", username)
	return nil
}

// LogoutAdmin invalidates an admin session. Checks both m.adminSessions and
// m.sessions defensively so that sessions are always revocable.
func (m *Module) LogoutAdmin(ctx context.Context, sessionID string) error {
	var username string
	var exists bool

	m.sessionsMu.Lock()
	if session, ok := m.adminSessions[sessionID]; ok {
		username = session.Username
		delete(m.adminSessions, sessionID)
		exists = true
	} else if session, ok := m.sessions[sessionID]; ok {
		username = session.Username
		delete(m.sessions, sessionID)
		exists = true
	}
	m.sessionsMu.Unlock()

	// Always attempt DB delete before checking exists — mirrors Logout's pattern.
	// Without this, a session present in DB but absent from the in-memory cache
	// (e.g. after a server restart) would never be revoked and remain valid until
	// natural expiry.
	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete admin session from repository: %v", err)
	}

	if !exists {
		return ErrSessionNotFound
	}

	m.log.Info("Admin logged out: %s", username)
	return nil
}

// CreateSessionForUser creates a new session for a user without password verification.
// Uses getOrLoadUser to handle cache misses (e.g., during startup before full cache warm-up).
func (m *Module) CreateSessionForUser(ctx context.Context, params *CreateSessionParams) (*models.Session, error) {
	user, err := m.getOrLoadUser(ctx, params.Username)
	if err != nil || user == nil {
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
	if session.Role == models.RoleAdmin {
		m.adminSessions[session.ID] = &models.AdminSession{Session: *session}
	} else {
		m.sessions[session.ID] = session
	}
	m.sessionsMu.Unlock()

	return session, nil
}
