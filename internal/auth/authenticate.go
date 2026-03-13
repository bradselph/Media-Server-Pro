// Login, admin login, and rate limiting.
package auth

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/pkg/models"
)

// AuthRequest holds credentials and request context for Authenticate and AdminAuthenticate.
type AuthRequest struct {
	Username  string
	Password  string
	IPAddress string
	UserAgent string
}

// creds holds username and password for password verification (avoids string-heavy args).
type creds struct {
	Username string
	Password string
}

// getOrLoadUser returns the user from cache or loads from DB and caches. Returns (nil, err) when not found or on error.
func (m *Module) getOrLoadUser(ctx context.Context, username string) (*models.User, error) {
	m.usersMu.RLock()
	user, exists := m.users[username]
	m.usersMu.RUnlock()
	if exists {
		return user, nil
	}
	user, err := m.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	m.usersMu.Lock()
	m.users[username] = user
	m.usersMu.Unlock()
	return user, nil
}

// verifyPasswordWithCacheRefresh checks password against cached user; on mismatch, retries from DB and refreshes cache if DB matches.
func (m *Module) verifyPasswordWithCacheRefresh(ctx context.Context, user *models.User, c *creds) error {
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(c.Password+user.Salt))
	if err == nil {
		return nil
	}
	dbUser, dbErr := m.userRepo.GetByUsername(ctx, c.Username)
	if dbErr != nil || dbUser.PasswordHash == "" {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(dbUser.PasswordHash), []byte(c.Password+dbUser.Salt)) != nil {
		return err
	}
	m.log.Warn("Password matched DB but not cache for user %s — refreshing cache", c.Username)
	m.usersMu.Lock()
	m.users[c.Username] = dbUser
	m.usersMu.Unlock()
	*user = *dbUser
	return nil
}

// Authenticate validates credentials and returns a session.
func (m *Module) Authenticate(ctx context.Context, req *AuthRequest) (*models.Session, error) {
	if m.isLockedOut(req.IPAddress) {
		m.log.Warn("Login attempt from locked out IP: %s", req.IPAddress)
		return nil, ErrAccountLocked
	}
	user, err := m.getOrLoadUser(ctx, req.Username)
	if err != nil || user == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		m.recordFailedAttempt(req.IPAddress)
		m.log.Debug("Login failed - user not found: %s", req.Username)
		return nil, ErrInvalidCredentials
	}
	if !user.Enabled {
		m.log.Debug("Login failed - account disabled: %s", req.Username)
		return nil, ErrAccountDisabled
	}
	if err := m.verifyPasswordWithCacheRefresh(ctx, user, &creds{Username: req.Username, Password: req.Password}); err != nil {
		m.recordFailedAttempt(req.IPAddress)
		m.log.Debug("Login failed - invalid password for: %s", req.Username)
		return nil, ErrInvalidCredentials
	}
	m.clearAttempts(req.IPAddress)
	session := m.createSession(ctx, user, &sessionRequestContext{IPAddress: req.IPAddress, UserAgent: req.UserAgent})
	m.usersMu.Lock()
	now := time.Now()
	user.LastLogin = &now
	m.usersMu.Unlock()
	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Warn("Failed to persist LastLogin for %s: %v", req.Username, err)
	}
	m.log.Info("User logged in: %s from %s", req.Username, req.IPAddress)
	return session, nil
}

// AdminAuthenticate authenticates admin credentials.
func (m *Module) AdminAuthenticate(ctx context.Context, req *AuthRequest) (*models.AdminSession, error) {
	if m.isLockedOut(req.IPAddress) {
		m.log.Warn("Admin login attempt from locked out IP: %s", req.IPAddress)
		return nil, ErrAccountLocked
	}

	cfg := m.config.Get()
	adminLoginAllowed := cfg.Admin.Enabled &&
		cfg.Admin.PasswordHash != "" &&
		req.Username == cfg.Admin.Username
	if !adminLoginAllowed {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		return nil, ErrNotAdminUsername
	}

	err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte(req.Password))
	if err != nil {
		m.recordFailedAttempt(req.IPAddress)
		m.log.Warn("Admin login failed - invalid password for admin from %s", req.IPAddress)
		return nil, ErrAdminWrongPassword
	}

	m.clearAttempts(req.IPAddress)

	adminUser, err := m.GetUser(ctx, req.Username)
	if err != nil {
		m.log.Error("Failed to get admin user record: %v", err)
		return nil, fmt.Errorf("failed to get admin user record: %w", err)
	}

	session := &models.AdminSession{
		Session: models.Session{
			ID:           generateSessionID(),
			UserID:       adminUser.ID,
			Username:     req.Username,
			Role:         models.RoleAdmin,
			CreatedAt:    time.Now(),
			ExpiresAt:    time.Now().Add(cfg.Admin.SessionTimeout),
			LastActivity: time.Now(),
			IPAddress:    req.IPAddress,
			UserAgent:    req.UserAgent,
		},
	}

	if err := m.sessionRepo.Create(ctx, &session.Session); err != nil {
		m.log.Warn("Failed to persist admin session to repository: %v", err)
	}

	m.sessionsMu.Lock()
	m.adminSessions[session.ID] = session
	m.sessionsMu.Unlock()

	m.log.Info("Admin logged in from %s", req.IPAddress)
	return session, nil
}

// ValidateAdminSession validates an admin session
// TODO: Bug — expired admin sessions are removed from the in-memory map but NOT from
// the session repository (database). Over time, expired admin sessions accumulate in the
// DB. The cleanupExpiredSessions method does handle this but only runs every 5 minutes,
// so there is a window. Also, unlike ValidateSession, this does not update LastActivity
// on the session, so admin sessions never refresh their expiry on use.
func (m *Module) ValidateAdminSession(sessionID string) (*models.AdminSession, error) {
	m.sessionsMu.RLock()
	session, exists := m.adminSessions[sessionID]
	m.sessionsMu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}
	if session.Session.IsExpired() {
		m.sessionsMu.Lock()
		delete(m.adminSessions, sessionID)
		m.sessionsMu.Unlock()
		return nil, ErrSessionExpired
	}
	return session, nil
}

// isLockedOut returns whether the IP is currently locked out due to failed attempts.
// When lockout has expired, the attempt is reset so the next failure starts a fresh count.
func (m *Module) isLockedOut(ip string) bool {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()

	attempt, exists := m.loginAttempts[ip]
	if !exists {
		return false
	}

	cfg := m.config.Get()
	if attempt.LockedAt != nil {
		if time.Since(*attempt.LockedAt) < cfg.Auth.LockoutDuration {
			return true
		}
		// Lockout expired — reset so next failed attempt doesn't immediately re-lock
		attempt.Count = 0
		attempt.LockedAt = nil
	}
	return false
}

// recordFailedAttempt records a failed login attempt for the IP.
func (m *Module) recordFailedAttempt(ip string) {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()

	cfg := m.config.Get()
	attempt, exists := m.loginAttempts[ip]
	if !exists {
		m.loginAttempts[ip] = &loginAttempt{
			Count:    1,
			FirstTry: time.Now(),
		}
		return
	}

	if time.Since(attempt.FirstTry) > cfg.Auth.LockoutDuration {
		attempt.Count = 1
		attempt.FirstTry = time.Now()
		attempt.LockedAt = nil
		return
	}

	attempt.Count++
	if attempt.Count >= cfg.Auth.MaxLoginAttempts {
		lockedAt := time.Now()
		attempt.LockedAt = &lockedAt
		m.log.Warn("Locked out IP due to too many failed attempts: %s", ip)
	}
}

// clearAttempts clears login attempts for the IP (e.g. after successful login).
func (m *Module) clearAttempts(ip string) {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()
	delete(m.loginAttempts, ip)
}
