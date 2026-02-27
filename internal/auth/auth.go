// Package auth provides authentication and session management.
// It handles user registration, login, sessions, and access control.
//
// Self-service account deletion is available via POST /api/auth/delete-account
// (requires password confirmation). Admin accounts cannot use that endpoint.
//
// Password recovery is not yet implemented — the User model has an Email field
// but email collection and sending infrastructure do not exist. A future implementation
// would require: email collection at registration, a reset request endpoint, a
// token-based reset flow, and an email sending dependency.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	"strings"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

var (
	// Re-export repository errors for backward compatibility
	ErrUserNotFound    = repositories.ErrUserNotFound
	ErrUserExists      = repositories.ErrUserExists
	ErrSessionNotFound = repositories.ErrSessionNotFound

	// Auth-specific errors
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountDisabled    = errors.New("account disabled")
	ErrAccountLocked      = errors.New("account locked")
	ErrSessionExpired     = errors.New("session expired")
	ErrAdminWrongPassword = errors.New("admin username correct but password wrong")
	ErrNotAdminUsername   = errors.New("username does not match admin")

	// dummyHash is a pre-computed bcrypt hash used for constant-time comparison
	// when a user/admin username doesn't exist, preventing timing-based username enumeration.
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-constant-time-pad"), bcrypt.DefaultCost)
)

const errHashPasswordFmt = "failed to hash password: %w"

// Module implements the authentication module
type Module struct {
	config        *config.Manager
	log           *logger.Logger
	dbModule      *database.Module
	userRepo      repositories.UserRepository
	sessionRepo   repositories.SessionRepository
	users         map[string]*models.User    // Kept for backward compatibility and caching
	sessions      map[string]*models.Session // Kept for backward compatibility and caching
	adminSessions map[string]*models.AdminSession
	loginAttempts map[string]*loginAttempt
	usersMu       sync.RWMutex
	sessionsMu    sync.RWMutex
	attemptsMu    sync.RWMutex
	dataDir       string
	healthy       bool
	healthMsg     string
	healthMu      sync.RWMutex
	cleanupTicker *time.Ticker
	cleanupDone   chan struct{}
	stopOnce      sync.Once
}

type loginAttempt struct {
	Count    int
	FirstTry time.Time
	LockedAt *time.Time
}

// NewModule creates a new authentication module.
// The database module is stored but repositories are created during Start()
// when the database connection is available.
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for authentication")
	}

	return &Module{
		config:        cfg,
		log:           logger.New("auth"),
		dbModule:      dbModule,
		users:         make(map[string]*models.User),
		sessions:      make(map[string]*models.Session),
		adminSessions: make(map[string]*models.AdminSession),
		loginAttempts: make(map[string]*loginAttempt),
		dataDir:       cfg.Get().Directories.Data,
		cleanupDone:   make(chan struct{}),
	}, nil
}

// Name returns the module name
func (m *Module) Name() string {
	return "auth"
}

// Start initializes the authentication module
func (m *Module) Start(ctx context.Context) error {
	m.log.Info("Starting authentication module...")

	// Initialize MySQL repositories (database is now connected)
	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}

	m.log.Info("Using MySQL repositories for auth")
	m.userRepo = mysql.NewUserRepository(m.dbModule.GORM())
	m.sessionRepo = mysql.NewSessionRepository(m.dbModule.GORM())

	// Load users and sessions into cache from repositories
	users, err := m.userRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to load users from repository: %v", err)
	} else {
		m.usersMu.Lock()
		for _, user := range users {
			m.users[user.Username] = user
		}
		m.usersMu.Unlock()
	}

	sessions, err := m.sessionRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to load sessions from repository: %v", err)
	} else {
		m.sessionsMu.Lock()
		invalidSessionCount := 0
		for _, session := range sessions {
			// Check for old sessions that have username instead of user ID
			// Old format: user_id = "admin" (username)
			// New format: user_id = "051de7d0a6f171273c5ad9d0b6580724" (UUID)
			if session.UserID == session.Username {
				// This is an old session with invalid format - delete it
				m.log.Info("Deleting old session %s with invalid user_id format (username instead of UUID)", session.ID)
				if err := m.sessionRepo.Delete(ctx, session.ID); err != nil {
					m.log.Warn("Failed to delete invalid session: %v", err)
				}
				invalidSessionCount++
				continue // Skip adding to cache
			}

			// Separate admin sessions from regular sessions based on role
			if session.Role == models.RoleAdmin {
				m.adminSessions[session.ID] = &models.AdminSession{Session: *session}
			} else {
				m.sessions[session.ID] = session
			}
		}
		m.sessionsMu.Unlock()

		if invalidSessionCount > 0 {
			m.log.Info("Cleaned up %d old sessions with invalid format - users will need to log in again", invalidSessionCount)
		}
	}

	// Create default admin if none exists
	if err := m.ensureDefaultAdmin(); err != nil {
		m.log.Error("Failed to create default admin: %v", err)
		m.healthMu.Lock()
		m.healthy = false
		m.healthMsg = fmt.Sprintf("Failed to create default admin: %v", err)
		m.healthMu.Unlock()
		return err
	}

	// Start session cleanup goroutine with panic recovery
	m.cleanupTicker = time.NewTicker(5 * time.Minute)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("Session cleanup loop panic recovered: %v", r)
				m.healthMu.Lock()
				m.healthy = false
				m.healthMsg = fmt.Sprintf("Cleanup loop panicked: %v", r)
				m.healthMu.Unlock()
			}
		}()
		m.cleanupLoop()
	}()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Authentication module started with %d users", len(m.users))
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping authentication module...")

	// Stop cleanup loop safely (only once)
	m.stopOnce.Do(func() {
		if m.cleanupTicker != nil {
			m.cleanupTicker.Stop()
			close(m.cleanupDone)
		}
	})

	// Repositories handle persistence automatically, no manual save needed
	m.log.Debug("Auth module stopped (repositories handle persistence)")

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	healthy := m.healthy
	msg := m.healthMsg
	m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(healthy),
		Message:   msg,
		CheckedAt: time.Now(),
	}
}

// cleanupLoop periodically cleans up expired sessions
func (m *Module) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupExpiredSessions()
		case <-m.cleanupDone:
			return
		}
	}
}

// cleanupExpiredSessions removes expired sessions and old login attempts.
// Lock order: sessionsMu first, then attemptsMu (cleanup is the only place both are held).
func (m *Module) cleanupExpiredSessions() {
	ctx := context.Background()

	// Use repository to delete expired sessions
	if err := m.sessionRepo.DeleteExpired(ctx); err != nil {
		m.log.Warn("Failed to cleanup expired sessions: %v", err)
	}

	// Clean up in-memory cache
	m.sessionsMu.Lock()
	now := time.Now()
	expired := 0

	for id, session := range m.sessions {
		if session.ExpiresAt.Before(now) {
			delete(m.sessions, id)
			expired++
		}
	}

	for id, session := range m.adminSessions {
		if session.ExpiresAt.Before(now) {
			delete(m.adminSessions, id)
			expired++
		}
	}
	m.sessionsMu.Unlock()

	if expired > 0 {
		m.log.Debug("Cleaned up %d expired sessions from cache", expired)
	}

	// Also cleanup old login attempts
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()

	cfg := m.config.Get()
	for ip, attempt := range m.loginAttempts {
		if time.Since(attempt.FirstTry) > cfg.Auth.LockoutDuration*2 {
			delete(m.loginAttempts, ip)
		}
	}
}

// CleanupExpiredSessions removes expired sessions from storage and cache (public method for background tasks)
func (m *Module) CleanupExpiredSessions(ctx context.Context) error {
	// Use repository to delete expired sessions
	if err := m.sessionRepo.DeleteExpired(ctx); err != nil {
		return err
	}

	// Clean up in-memory cache
	m.sessionsMu.Lock()
	now := time.Now()
	expired := 0

	for id, session := range m.sessions {
		if session.ExpiresAt.Before(now) {
			delete(m.sessions, id)
			expired++
		}
	}

	for id, session := range m.adminSessions {
		if session.ExpiresAt.Before(now) {
			delete(m.adminSessions, id)
			expired++
		}
	}
	m.sessionsMu.Unlock()

	if expired > 0 {
		m.log.Debug("Cleaned up %d expired sessions from cache", expired)
	}

	// Also cleanup old login attempts
	m.attemptsMu.Lock()
	cfg := m.config.Get()
	for ip, attempt := range m.loginAttempts {
		if time.Since(attempt.FirstTry) > cfg.Auth.LockoutDuration*2 {
			delete(m.loginAttempts, ip)
		}
	}
	m.attemptsMu.Unlock()

	return nil
}

// CreateUser creates a new user
func (m *Module) CreateUser(ctx context.Context, username, password, email, userType string, role models.UserRole) (*models.User, error) {

	// Check cache for existing user first
	m.usersMu.RLock()
	_, exists := m.users[username]
	m.usersMu.RUnlock()
	if exists {
		return nil, ErrUserExists
	}

	// Generate salt and hash password
	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(password+salt), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf(errHashPasswordFmt, err)
	}

	user := &models.User{
		ID:           generateID(),
		Username:     username,
		PasswordHash: string(hash),
		Salt:         salt,
		Email:        email,
		Role:         role,
		Type:         userType,
		Enabled:      true,
		CreatedAt:    time.Now(),
		Permissions:  m.getDefaultPermissions(userType),
		Preferences: models.UserPreferences{
			Theme:          "dark",
			ViewMode:       "grid",
			DefaultQuality: "auto",
			AutoPlay:       false,
			PlaybackSpeed:  1.0,
			Volume:         1.0,
			Language:       "en",
		},
		WatchHistory: make([]models.WatchHistoryItem, 0),
	}

	// Admin role always gets full permissions regardless of user type config
	if role == models.RoleAdmin {
		user.Permissions = models.UserPermissions{
			CanStream:          true,
			CanDownload:        true,
			CanUpload:          true,
			CanDelete:          true,
			CanManage:          true,
			CanViewMature:      true,
			CanCreatePlaylists: true,
		}
	}

	// Save to repository
	if err := m.userRepo.Create(ctx, user); err != nil {
		// Detect MySQL duplicate entry (Error 1062) and return user-friendly message
		if strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry") {
			return nil, ErrUserExists
		}
		m.log.Error("Failed to create user %s: %v", username, err)
		return nil, fmt.Errorf("failed to create user")
	}

	// Update cache
	m.usersMu.Lock()
	m.users[username] = user
	m.usersMu.Unlock()

	m.log.Info("Created user: %s (type: %s, role: %s)", username, userType, role)
	return user, nil
}

// getDefaultPermissions returns default permissions for a user type
func (m *Module) getDefaultPermissions(userType string) models.UserPermissions {
	cfg := m.config.Get()

	for _, ut := range cfg.Auth.UserTypes {
		if ut.Name == userType {
			return models.UserPermissions{
				CanStream:          true,
				CanDownload:        ut.AllowDownloads,
				CanUpload:          ut.AllowUploads,
				CanDelete:          false,
				CanManage:          false,
				CanViewMature:      true,
				CanCreatePlaylists: ut.AllowPlaylists,
			}
		}
	}

	// Default permissions
	return models.UserPermissions{
		CanStream:          true,
		CanDownload:        false,
		CanUpload:          false,
		CanDelete:          false,
		CanManage:          false,
		CanViewMature:      true,
		CanCreatePlaylists: false,
	}
}

// GetUser retrieves a user by username
func (m *Module) GetUser(ctx context.Context, username string) (*models.User, error) {

	// Try cache first
	m.usersMu.RLock()
	if user, exists := m.users[username]; exists {
		m.usersMu.RUnlock()
		return user, nil
	}
	m.usersMu.RUnlock()

	// Fall back to repository
	user, err := m.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Update cache
	m.usersMu.Lock()
	m.users[username] = user
	m.usersMu.Unlock()

	return user, nil
}

// GetUserByID retrieves a user by ID
func (m *Module) GetUserByID(ctx context.Context, id string) (*models.User, error) {

	// Try cache first
	m.usersMu.RLock()
	for _, user := range m.users {
		if user.ID == id {
			m.usersMu.RUnlock()
			return user, nil
		}
	}
	m.usersMu.RUnlock()

	// Fall back to repository
	user, err := m.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Update cache
	m.usersMu.Lock()
	m.users[user.Username] = user
	m.usersMu.Unlock()

	return user, nil
}

// UpdateUser updates a user's information
func (m *Module) UpdateUser(ctx context.Context, username string, updates map[string]interface{}) error {

	// Get user
	user, err := m.GetUser(ctx, username)
	if err != nil {
		return err
	}

	// Work on a copy so we never mutate the cached pointer while unlocked
	userCopy := *user
	user = &userCopy

	// Apply updates
	if email, ok := updates["email"].(string); ok {
		user.Email = email
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		user.Enabled = enabled
	}
	if userType, ok := updates["type"].(string); ok {
		user.Type = userType
		user.Permissions = m.getDefaultPermissions(userType)
	}
	if role, ok := updates["role"].(string); ok {
		user.Role = models.UserRole(role)
	}
	// If role is (or became) admin, ensure full permissions — unless the caller
	// explicitly overrides them via the "permissions" key below.
	if user.Role == models.RoleAdmin {
		user.Permissions = models.UserPermissions{
			CanStream:          true,
			CanDownload:        true,
			CanUpload:          true,
			CanDelete:          true,
			CanManage:          true,
			CanViewMature:      true,
			CanCreatePlaylists: true,
		}
	}
	if password, ok := updates["password"].(string); ok && password != "" {
		salt := generateSalt()
		hash, err := bcrypt.GenerateFromPassword([]byte(password+salt), bcrypt.DefaultCost)
		if err == nil {
			user.PasswordHash = string(hash)
			user.Salt = salt
		}
	}
	if permsMap, ok := updates["permissions"].(map[string]interface{}); ok {
		if v, ok := permsMap["can_upload"].(bool); ok {
			user.Permissions.CanUpload = v
		}
		if v, ok := permsMap["can_download"].(bool); ok {
			user.Permissions.CanDownload = v
		}
		if v, ok := permsMap["can_stream"].(bool); ok {
			user.Permissions.CanStream = v
		}
		if v, ok := permsMap["can_delete"].(bool); ok {
			user.Permissions.CanDelete = v
		}
		if v, ok := permsMap["can_manage"].(bool); ok {
			user.Permissions.CanManage = v
		}
		if v, ok := permsMap["can_view_mature"].(bool); ok {
			user.Permissions.CanViewMature = v
		}
		if v, ok := permsMap["can_create_playlists"].(bool); ok {
			user.Permissions.CanCreatePlaylists = v
		}
	}

	// Save to repository
	if err := m.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Update cache
	m.usersMu.Lock()
	m.users[username] = user
	m.usersMu.Unlock()

	m.log.Info("Updated user: %s", username)
	return nil
}

// DeleteUser removes a user.
// Lock ordering: usersMu first, then sessionsMu (never nested) to prevent deadlocks.
func (m *Module) DeleteUser(ctx context.Context, username string) error {

	// Get user to find their ID
	user, err := m.GetUser(ctx, username)
	if err != nil {
		return err
	}

	// Delete from repository (cascade delete will handle sessions via foreign key)
	if err := m.userRepo.Delete(ctx, user.ID); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Update caches
	m.usersMu.Lock()
	delete(m.users, username)
	m.usersMu.Unlock()

	m.sessionsMu.Lock()
	for id, session := range m.sessions {
		if session.Username == username {
			delete(m.sessions, id)
		}
	}
	m.sessionsMu.Unlock()

	m.log.Info("Deleted user: %s", username)
	return nil
}

// ListUsers returns all users (without sensitive data)
func (m *Module) ListUsers(ctx context.Context) []*models.User {

	// Try repository first
	users, err := m.userRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to list users from repository: %v", err)
		// Fall back to cache
		m.usersMu.RLock()
		defer m.usersMu.RUnlock()
		users = make([]*models.User, 0, len(m.users))
		for _, user := range m.users {
			users = append(users, user)
		}
	}

	// Strip sensitive data
	result := make([]*models.User, len(users))
	for i, user := range users {
		userCopy := *user
		userCopy.PasswordHash = ""
		userCopy.Salt = ""
		result[i] = &userCopy
	}
	return result
}

// Authenticate validates credentials and returns a session
func (m *Module) Authenticate(ctx context.Context, username, password, ipAddress, userAgent string) (*models.Session, error) {
	// Check rate limiting
	if m.isLockedOut(ipAddress) {
		m.log.Warn("Login attempt from locked out IP: %s", ipAddress)
		return nil, ErrAccountLocked
	}

	// Try cache first, then fall back to DB
	m.usersMu.RLock()
	user, exists := m.users[username]
	m.usersMu.RUnlock()

	if !exists {
		// Not in cache — try DB directly
		var err error
		user, err = m.userRepo.GetByUsername(ctx, username)
		if err != nil {
			// Perform dummy bcrypt comparison to prevent timing-based username enumeration
			bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			m.recordFailedAttempt(ipAddress)
			m.log.Debug("Login failed - user not found: %s", username)
			return nil, ErrInvalidCredentials
		}
		// Populate cache for next time
		m.usersMu.Lock()
		m.users[username] = user
		m.usersMu.Unlock()
	}

	if !user.Enabled {
		m.log.Debug("Login failed - account disabled: %s", username)
		return nil, ErrAccountDisabled
	}

	// Verify password against cached user
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password+user.Salt))
	if err != nil {
		// Cache might be stale — reload from DB and retry once
		dbUser, dbErr := m.userRepo.GetByUsername(ctx, username)
		if dbErr == nil && dbUser.PasswordHash != "" {
			retryErr := bcrypt.CompareHashAndPassword([]byte(dbUser.PasswordHash), []byte(password+dbUser.Salt))
			if retryErr == nil {
				// DB had the correct password — update cache
				m.log.Warn("Password matched DB but not cache for user %s — refreshing cache", username)
				m.usersMu.Lock()
				m.users[username] = dbUser
				m.usersMu.Unlock()
				user = dbUser
				err = nil
			}
		}
	}

	if err != nil {
		m.recordFailedAttempt(ipAddress)
		m.log.Debug("Login failed - invalid password for: %s", username)
		return nil, ErrInvalidCredentials
	}

	// Clear login attempts on success
	m.clearAttempts(ipAddress)

	// Create session
	session := m.createSession(ctx, user, ipAddress, userAgent)

	// Update last login
	m.usersMu.Lock()
	now := time.Now()
	user.LastLogin = &now
	m.usersMu.Unlock()

	m.log.Info("User logged in: %s from %s", username, ipAddress)
	return session, nil
}

// CreateSessionForUser creates a new session for a user without password verification.
// This should only be called after successful authentication or user creation.
func (m *Module) CreateSessionForUser(ctx context.Context, username, ipAddress, userAgent string) (*models.Session, error) {
	m.usersMu.RLock()
	user, exists := m.users[username]
	m.usersMu.RUnlock()

	if !exists {
		return nil, ErrUserNotFound
	}

	if !user.Enabled {
		return nil, ErrAccountDisabled
	}

	return m.createSession(ctx, user, ipAddress, userAgent), nil
}

// createSession creates a new session for a user
func (m *Module) createSession(ctx context.Context, user *models.User, ipAddress, userAgent string) *models.Session {
	cfg := m.config.Get()

	session := &models.Session{
		ID:           generateSessionID(),
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(cfg.Auth.SessionTimeout),
		LastActivity: time.Now(),
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
	}

	// Save to repository
	if err := m.sessionRepo.Create(ctx, session); err != nil {
		m.log.Warn("Failed to save session to repository: %v", err)
	}

	// Update cache
	m.sessionsMu.Lock()
	m.sessions[session.ID] = session
	m.sessionsMu.Unlock()

	return session
}

// ValidateSession validates a session and returns the associated user
func (m *Module) ValidateSession(ctx context.Context, sessionID string) (*models.Session, *models.User, error) {

	// Try cache first
	m.sessionsMu.RLock()
	session, exists := m.sessions[sessionID]
	m.sessionsMu.RUnlock()

	if !exists {
		// Fall back to repository
		var err error
		session, err = m.sessionRepo.Get(ctx, sessionID)
		if err != nil {
			return nil, nil, ErrSessionNotFound
		}
		// Update cache
		m.sessionsMu.Lock()
		m.sessions[sessionID] = session
		m.sessionsMu.Unlock()
	}

	if session.IsExpired() {
		if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
			m.log.Warn("Failed to delete expired session: %v", err)
		}
		m.sessionsMu.Lock()
		delete(m.sessions, sessionID)
		m.sessionsMu.Unlock()
		return nil, nil, ErrSessionExpired
	}

	user, err := m.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}

	if !user.Enabled {
		return nil, nil, ErrAccountDisabled
	}

	// Update last activity
	session.LastActivity = time.Now()

	return session, user, nil
}

// Logout invalidates a session
func (m *Module) Logout(ctx context.Context, sessionID string) error {

	// Check and delete from cache first — cache is the primary source of truth
	// for active sessions. Admin sessions are tracked separately in adminSessions.
	m.sessionsMu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		m.log.Info("User logged out: %s", session.Username)
		delete(m.sessions, sessionID)
	}
	m.sessionsMu.Unlock()

	// Always attempt repository cleanup regardless of cache hit, so stale DB
	// entries are removed even when the in-memory cache was already cleared.
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

	// Delete from repository
	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete admin session from repository: %v", err)
	}

	m.log.Info("Admin logged out: %s", session.Username)
	delete(m.adminSessions, sessionID)
	return nil
}

// AdminAuthenticate authenticates admin credentials
func (m *Module) AdminAuthenticate(ctx context.Context, username, password, ipAddress, userAgent string) (*models.AdminSession, error) {
	// Check rate limiting
	if m.isLockedOut(ipAddress) {
		m.log.Warn("Admin login attempt from locked out IP: %s", ipAddress)
		return nil, ErrAccountLocked
	}

	cfg := m.config.Get()

	// Skip config-based admin auth entirely if admin is disabled or has no
	// password hash. This allows DB users with the same username to authenticate
	// through normal user auth instead of being blocked.
	if !cfg.Admin.Enabled || cfg.Admin.PasswordHash == "" || username != cfg.Admin.Username {
		// Perform dummy bcrypt comparison to prevent timing-based username enumeration
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrNotAdminUsername
	}

	err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte(password))
	if err != nil {
		m.recordFailedAttempt(ipAddress)
		m.log.Warn("Admin login failed - invalid password for admin from %s", ipAddress)
		return nil, ErrAdminWrongPassword
	}

	m.clearAttempts(ipAddress)

	// Get the admin user to retrieve their actual ID
	adminUser, err := m.GetUser(ctx, username)
	if err != nil {
		m.log.Error("Failed to get admin user record: %v", err)
		return nil, fmt.Errorf("failed to get admin user record: %w", err)
	}

	session := &models.AdminSession{
		Session: models.Session{
			ID:           generateSessionID(),
			UserID:       adminUser.ID, // Use the actual user ID, not the username
			Username:     username,
			Role:         models.RoleAdmin,
			CreatedAt:    time.Now(),
			ExpiresAt:    time.Now().Add(cfg.Admin.SessionTimeout),
			LastActivity: time.Now(),
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		},
	}

	// Save to repository for persistence
	if err := m.sessionRepo.Create(ctx, &session.Session); err != nil {
		m.log.Warn("Failed to persist admin session to repository: %v", err)
	}

	m.sessionsMu.Lock()
	m.adminSessions[session.ID] = session
	m.sessionsMu.Unlock()

	m.log.Info("Admin logged in from %s", ipAddress)
	return session, nil
}

// ValidateAdminSession validates an admin session
func (m *Module) ValidateAdminSession(sessionID string) (*models.AdminSession, error) {
	m.sessionsMu.RLock()
	session, exists := m.adminSessions[sessionID]
	m.sessionsMu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}

	if session.IsExpired() {
		m.sessionsMu.Lock()
		delete(m.adminSessions, sessionID)
		m.sessionsMu.Unlock()
		return nil, ErrSessionExpired
	}

	m.sessionsMu.Lock()
	session.LastActivity = time.Now()
	m.sessionsMu.Unlock()

	return session, nil
}

// Rate limiting helpers
func (m *Module) isLockedOut(ip string) bool {
	m.attemptsMu.RLock()
	defer m.attemptsMu.RUnlock()

	attempt, exists := m.loginAttempts[ip]
	if !exists {
		return false
	}

	cfg := m.config.Get()
	if attempt.LockedAt != nil {
		if time.Since(*attempt.LockedAt) < cfg.Auth.LockoutDuration {
			return true
		}
	}

	return false
}

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

	// Reset if window has passed
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

func (m *Module) clearAttempts(ip string) {
	m.attemptsMu.Lock()
	defer m.attemptsMu.Unlock()
	delete(m.loginAttempts, ip)
}

// UpdatePassword updates a user's password
func (m *Module) UpdatePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	// Verify old password
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword+user.Salt))
	if err != nil {
		m.usersMu.Unlock()
		return ErrInvalidCredentials
	}

	// Hash new password
	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword+salt), bcrypt.DefaultCost)
	if err != nil {
		m.usersMu.Unlock()
		return fmt.Errorf(errHashPasswordFmt, err)
	}

	user.PasswordHash = string(hash)
	user.Salt = salt
	m.usersMu.Unlock()

	m.log.Info("Password updated for user: %s", username)

	// Save via repository - password changes are security-critical and must be persisted immediately
	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after password update: %v", err)
		return fmt.Errorf("password updated in memory but failed to persist: %w", err)
	}

	return nil
}

// SetPassword sets a user's password (admin action, no old password required)
func (m *Module) SetPassword(ctx context.Context, username, newPassword string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	// Hash new password
	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword+salt), bcrypt.DefaultCost)
	if err != nil {
		m.usersMu.Unlock()
		return fmt.Errorf(errHashPasswordFmt, err)
	}

	user.PasswordHash = string(hash)
	user.Salt = salt
	m.usersMu.Unlock()

	m.log.Info("Password set for user: %s (admin action)", username)

	// Save via repository - admin password resets must be persisted immediately
	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after password set: %v", err)
		return fmt.Errorf("password set in memory but failed to persist: %w", err)
	}

	return nil
}

// ChangeAdminPassword verifies the current admin password and replaces it with
// a new one, persisting the new bcrypt hash to config immediately.
func (m *Module) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error {
	cfg := m.config.Get()

	// Verify current admin password against the config hash
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

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password+user.Salt))
	if err != nil {
		return ErrInvalidCredentials
	}

	return nil
}

// UpdateUserPreferences updates and persists user preferences.
func (m *Module) UpdateUserPreferences(ctx context.Context, username string, prefs models.UserPreferences) error {
	// Validate and normalize preferences before storing
	prefs.Validate()

	m.usersMu.Lock()
	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	user.Preferences = prefs
	// When the user opts into mature content, ensure the CanViewMature permission
	// is set so they can actually stream it. This self-grant handles users created
	// before the default was changed to true. Admins can still explicitly revoke
	// CanViewMature via AdminUpdateUser to block specific users.
	if prefs.ShowMature && !user.Permissions.CanViewMature {
		user.Permissions.CanViewMature = true
	}
	m.usersMu.Unlock()

	// Save via repository
	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after preference update: %v", err)
		return err
	}
	return nil
}

// AddToWatchHistory adds or updates an item in the user's watch history.
func (m *Module) AddToWatchHistory(ctx context.Context, username string, item models.WatchHistoryItem) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	// Check if already in history, update if so
	for i, existing := range user.WatchHistory {
		if existing.MediaPath == item.MediaPath {
			user.WatchHistory[i] = item
			m.usersMu.Unlock()
			// Persist updated position via repository
			if err := m.userRepo.Update(ctx, user); err != nil {
				m.log.Error("Failed to save user after watch history update: %v", err)
			}
			return nil
		}
	}

	// Add to front of history (most recent first)
	user.WatchHistory = append([]models.WatchHistoryItem{item}, user.WatchHistory...)

	// Limit history size
	const maxHistory = 100
	if len(user.WatchHistory) > maxHistory {
		user.WatchHistory = user.WatchHistory[:maxHistory]
	}

	m.usersMu.Unlock()

	// Persist new watch history entry via repository
	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after watch history update: %v", err)
		return err
	}

	return nil
}

// ClearWatchHistory clears a user's watch history
func (m *Module) ClearWatchHistory(ctx context.Context, username string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	user.WatchHistory = make([]models.WatchHistoryItem, 0)

	m.usersMu.Unlock()

	// Save via repository to ensure watch history clear is persisted
	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after clearing watch history: %v", err)
		return err
	}

	return nil
}

// RemoveWatchHistoryItem removes a single item from a user's watch history by media path
func (m *Module) RemoveWatchHistoryItem(ctx context.Context, username, mediaPath string) error {
	m.usersMu.Lock()

	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}

	updated := user.WatchHistory[:0]
	for _, item := range user.WatchHistory {
		if item.MediaPath != mediaPath {
			updated = append(updated, item)
		}
	}
	user.WatchHistory = updated

	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, user); err != nil {
		m.log.Error("Failed to save user after removing watch history item: %v", err)
		return err
	}

	return nil
}

// GetWatchHistory returns a user's watch history
func (m *Module) GetWatchHistory(username string) ([]models.WatchHistoryItem, error) {
	m.usersMu.RLock()
	defer m.usersMu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil, ErrUserNotFound
	}

	return user.WatchHistory, nil
}

// GetActiveSessions returns active sessions for a user
func (m *Module) GetActiveSessions(username string) []*models.Session {
	m.sessionsMu.RLock()
	defer m.sessionsMu.RUnlock()

	var sessions []*models.Session
	for _, session := range m.sessions {
		if session.Username == username && !session.IsExpired() {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

// ensureDefaultAdmin ensures the admin password hash is set in config and that
// the admin user record exists in the database so that AdminAuthenticate can
// retrieve the user ID when building a session.
func (m *Module) ensureDefaultAdmin() error {
	cfg := m.config.Get()
	if cfg.Admin.PasswordHash == "" {
		// Generate a random default password instead of a hardcoded one
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
		// Write password to stderr only (not to log files or disk) to avoid persistence of secrets
		_, _ = fmt.Fprintf(os.Stderr, "\n*** Default admin password (change immediately): %s ***\n\n", defaultPassword)
		m.log.Warn("Created default admin with a generated password - check stderr output. PLEASE CHANGE THIS IMMEDIATELY")
	}

	// Ensure the admin user record exists in the database.
	// AdminAuthenticate calls GetUser(username) to obtain the user ID for the
	// session; if the record is absent the very first admin login fails.
	return m.ensureAdminUserRecord()
}

// ensureAdminUserRecord creates the admin user row in the database if it does
// not already exist. The row is only needed so that GetUser() can return an ID;
// admin authentication itself uses the bcrypt hash from config, not this row.
func (m *Module) ensureAdminUserRecord() error {
	cfg := m.config.Get()
	adminUsername := cfg.Admin.Username

	// Check in-memory cache first (populated from DB at Start())
	m.usersMu.RLock()
	_, exists := m.users[adminUsername]
	m.usersMu.RUnlock()
	if exists {
		return nil
	}

	ctx := context.Background()

	// Check the database directly in case the List() at startup failed
	existingUser, err := m.userRepo.GetByUsername(ctx, adminUsername)
	if err == nil {
		// Found in DB — populate cache and return
		m.usersMu.Lock()
		m.users[adminUsername] = existingUser
		m.usersMu.Unlock()
		return nil
	}

	// Create the admin user record
	adminUser := &models.User{
		ID:           generateID(),
		Username:     adminUsername,
		PasswordHash: cfg.Admin.PasswordHash,
		Salt:         "", // Admin auth uses config hash directly; no salt needed
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
			Theme:          "dark",
			ViewMode:       "grid",
			DefaultQuality: "auto",
			AutoPlay:       false,
			PlaybackSpeed:  1.0,
			Volume:         1.0,
			Language:       "en",
		},
		WatchHistory: make([]models.WatchHistoryItem, 0),
	}

	if err := m.userRepo.Create(ctx, adminUser); err != nil {
		return fmt.Errorf("failed to create admin user record: %w", err)
	}

	m.usersMu.Lock()
	m.users[adminUsername] = adminUser
	m.usersMu.Unlock()

	m.log.Info("Created admin user record in database for: %s", adminUsername)
	return nil
}

// generateRandomPassword creates a cryptographically random password of the given length
func generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range b {
		// Use crypto/rand.Int for uniform distribution without modulo bias
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		b[i] = charset[num.Int64()]
	}
	return string(b), nil
}

// GenerateSecurePassword generates a cryptographically secure random password
func (m *Module) GenerateSecurePassword(length int) (string, error) {
	return generateRandomPassword(length)
}

// Helper functions
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// This should never happen in practice, but if crypto/rand fails, we're in trouble
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// This should never happen in practice, but if crypto/rand fails, we're in trouble
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return base64.URLEncoding.EncodeToString(b)
}

func generateSalt() string {
	return generateID()
}
