// Package auth provides authentication and session management for the media server:
// user and admin login, session lifecycle, password and preference management, and watch history.
// See session.go, user.go, authenticate.go, password.go, watch_history.go, bootstrap.go, and helpers.go.
package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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

	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}

	m.log.Info("Using MySQL repositories for auth")
	m.userRepo = mysql.NewUserRepository(m.dbModule.GORM())
	m.sessionRepo = mysql.NewSessionRepository(m.dbModule.GORM())

	m.loadUsersIntoMap(ctx)
	m.loadSessionsFromRepo(ctx)

	if err := m.ensureDefaultAdminWithHealth(ctx); err != nil {
		return err
	}

	m.startCleanupLoop()
	m.setHealth(true, "Running")
	m.log.Info("Authentication module started with %d users", len(m.users))
	return nil
}

// loadUsersIntoMap loads users from the repository into the in-memory map (logs on error).
func (m *Module) loadUsersIntoMap(ctx context.Context) {
	users, err := m.userRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to load users from repository: %v", err)
		return
	}
	m.usersMu.Lock()
	for _, user := range users {
		m.users[user.Username] = user
	}
	m.usersMu.Unlock()
}

// loadSessionsFromRepo loads sessions from the repository into in-memory maps and cleans invalid ones.
func (m *Module) loadSessionsFromRepo(ctx context.Context) {
	sessions, err := m.sessionRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to load sessions from repository: %v", err)
		return
	}
	invalidCount := m.loadSessionsIntoMaps(ctx, sessions)
	if invalidCount > 0 {
		m.log.Info("Cleaned up %d old sessions with invalid format - users will need to log in again", invalidCount)
	}
}

// loadSessionsIntoMaps loads sessions into in-memory maps, deletes invalid sessions
// (user_id stored as username), and returns the count of deleted invalid sessions.
func (m *Module) loadSessionsIntoMaps(ctx context.Context, sessions []*models.Session) (invalidCount int) {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()
	for _, session := range sessions {
		if session.UserID == session.Username {
			m.log.Info("Deleting old session %s with invalid user_id format (username instead of UUID)", session.ID)
			if err := m.sessionRepo.Delete(ctx, session.ID); err != nil {
				m.log.Warn("Failed to delete invalid session: %v", err)
			}
			invalidCount++
			continue
		}
		if session.Role == models.RoleAdmin {
			m.adminSessions[session.ID] = &models.AdminSession{Session: *session}
		} else {
			m.sessions[session.ID] = session
		}
	}
	return invalidCount
}

// setHealth updates the module health status (caller holds no locks).
func (m *Module) setHealth(healthy bool, msg string) {
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	m.healthy = healthy
	m.healthMsg = msg
}

// ensureDefaultAdminWithHealth ensures default admin exists and sets unhealthy on error.
func (m *Module) ensureDefaultAdminWithHealth(_ context.Context) error {
	if err := m.ensureDefaultAdmin(); err != nil {
		m.log.Error("Failed to create default admin: %v", err)
		m.setHealth(false, fmt.Sprintf("Failed to create default admin: %v", err))
		return err
	}
	return nil
}

// startCleanupLoop starts the background session cleanup ticker and goroutine.
func (m *Module) startCleanupLoop() {
	m.cleanupTicker = time.NewTicker(5 * time.Minute)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("Session cleanup loop panic recovered: %v", r)
				m.setHealth(false, fmt.Sprintf("Cleanup loop panicked: %v", r))
			}
		}()
		m.cleanupLoop()
	}()
}

// Stop gracefully stops the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping authentication module...")

	m.stopOnce.Do(func() {
		if m.cleanupTicker != nil {
			m.cleanupTicker.Stop()
			close(m.cleanupDone)
		}
	})

	m.log.Debug("Auth module stopped (repositories handle persistence)")

	m.setHealth(false, "Stopped")
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
