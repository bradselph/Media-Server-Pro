// Package auth handles login, admin login, and rate limiting.
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
// Uses double-checked locking to prevent the TOCTOU window where two concurrent callers
// both miss the cache, both load from DB, and then race to write — potentially leaving
// callers holding stale pointers that diverge from the cached copy.
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
	// Re-check: another goroutine may have populated the cache while we were loading from DB.
	if existing, ok := m.users[username]; ok {
		m.usersMu.Unlock()
		return existing, nil
	}
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
		// Record failed attempt so disabled accounts incur the same brute-force
		// penalty as wrong passwords, preventing unlimited-rate enumeration.
		// Return generic credentials error to avoid leaking account existence.
		m.recordFailedAttempt(req.IPAddress)
		m.log.Debug("Login failed - account disabled: %s", req.Username)
		return nil, ErrInvalidCredentials
	}
	if verifyErr := m.verifyPasswordWithCacheRefresh(ctx, user, &creds{Username: req.Username, Password: req.Password}); verifyErr != nil {
		m.recordFailedAttempt(req.IPAddress)
		m.log.Debug("Login failed - invalid password for: %s", req.Username)
		return nil, ErrInvalidCredentials
	}
	m.clearAttempts(req.IPAddress)
	session, err := m.createSession(ctx, user, &sessionRequestContext{IPAddress: req.IPAddress, UserAgent: req.UserAgent})
	if err != nil {
		return nil, fmt.Errorf("session creation failed: %w", err)
	}
	// Copy user before mutation to avoid data race on shared pointer.
	// Only update LastLogin/PreviousLastLogin fields on the existing cached pointer
	// under the lock (instead of replacing the entire pointer) to avoid clobbering
	// concurrent password changes or preference updates.
	userCopy := *user
	userCopy.PreviousLastLogin = userCopy.LastLogin
	userCopy.LastLogin = new(time.Now())
	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Warn("Failed to persist LastLogin for %s: %v", req.Username, err)
	} else {
		m.usersMu.Lock()
		if u, ok := m.users[req.Username]; ok {
			u.PreviousLastLogin = userCopy.PreviousLastLogin
			u.LastLogin = userCopy.LastLogin
		}
		m.usersMu.Unlock()
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
		// Record the failed attempt so wrong-username attempts accrue lockout penalty,
		// preventing username enumeration without incurring lockout.
		m.recordFailedAttempt(req.IPAddress)
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

	// Return a minimal AdminSession carrying just the username so the handler can
	// call CreateSessionForUser. The AdminSession is NOT stored in adminSessions or
	// the session repository — the handler creates the actual usable session itself.
	session := &models.AdminSession{
		Session: models.Session{
			Username: req.Username,
			UserID:   adminUser.ID,
			Role:     models.RoleAdmin,
		},
	}

	m.log.Info("Admin logged in from %s", req.IPAddress)
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
		// Window expired: start fresh count but increment Windows so repeated
		// lockout-window breaches accumulate a penalty instead of fully resetting.
		attempt.Windows++
		attempt.Count = 1
		attempt.FirstTry = time.Now()
		attempt.LockedAt = nil
		// Re-lock immediately if this IP has already triggered enough lockout windows.
		if attempt.Windows >= cfg.Auth.MaxLoginAttempts {
			attempt.LockedAt = new(time.Now())
			m.log.Warn("Re-locked IP %s after %d repeated lockout windows", ip, attempt.Windows)
		}
		return
	}

	attempt.Count++
	if attempt.Count >= cfg.Auth.MaxLoginAttempts {
		attempt.LockedAt = new(time.Now())
		m.log.Warn("Locked out IP due to too many failed attempts: %s", ip)
	}
}

// clearAttempts clears login attempts for the IP (e.g. after successful login).
func (m *Module) clearAttempts(ip string) {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()
	delete(m.loginAttempts, ip)
}
