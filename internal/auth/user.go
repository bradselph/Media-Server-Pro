// User CRUD and preferences.
package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/pkg/models"
)

// CreateUserParams holds arguments for creating a user (reduces function arity).
type CreateUserParams struct {
	Username string
	Password string
	Email    string
	UserType string
	Role     models.UserRole
}

// CreateUser creates a new user.
func (m *Module) CreateUser(ctx context.Context, p CreateUserParams) (*models.User, error) {
	if len(p.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	m.usersMu.RLock()
	_, exists := m.users[p.Username]
	m.usersMu.RUnlock()
	if exists {
		return nil, ErrUserExists
	}

	user, err := m.buildNewUser(p)
	if err != nil {
		return nil, err
	}

	if err := m.userRepo.Create(ctx, user); err != nil {
		if strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry") {
			return nil, ErrUserExists
		}
		m.log.Error("Failed to create user %s: %v", p.Username, err)
		return nil, fmt.Errorf("failed to create user")
	}

	m.cacheUser(user)
	m.log.Info("Created user: %s (type: %s, role: %s)", p.Username, p.UserType, p.Role)
	return user, nil
}

func (m *Module) buildNewUser(p CreateUserParams) (*models.User, error) {
	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password+salt), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf(errHashPasswordFmt, err)
	}

	user := &models.User{
		ID:           generateID(),
		Username:     p.Username,
		PasswordHash: string(hash),
		Salt:         salt,
		Email:        p.Email,
		Role:         p.Role,
		Type:         p.UserType,
		Enabled:      true,
		CreatedAt:    time.Now(),
		Permissions:  m.getDefaultPermissions(p.UserType),
		Preferences:  defaultUserPreferences(),
		WatchHistory: make([]models.WatchHistoryItem, 0),
	}

	if p.Role == models.RoleAdmin {
		user.Permissions = adminPermissions()
	}
	return user, nil
}

func defaultUserPreferences() models.UserPreferences {
	return models.UserPreferences{
		Theme:                "dark",
		ViewMode:             "grid",
		DefaultQuality:       "auto",
		AutoPlay:             false,
		PlaybackSpeed:        1.0,
		Volume:               1.0,
		Language:             "en",
		ResumePlayback:       true,
		ShowAnalytics:        true,
		ShowContinueWatching: true,
		ShowRecommended:      true,
		ShowTrending:         true,
	}
}

func adminPermissions() models.UserPermissions {
	return models.UserPermissions{
		CanStream: true, CanDownload: true, CanUpload: true, CanDelete: true,
		CanManage: true, CanViewMature: true, CanCreatePlaylists: true,
	}
}

// cacheUser stores a user in the in-memory cache (must hold no locks).
func (m *Module) cacheUser(user *models.User) {
	m.usersMu.Lock()
	m.users[user.Username] = user
	m.usersMu.Unlock()
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

// GetUser retrieves a user by username.
func (m *Module) GetUser(ctx context.Context, username string) (*models.User, error) {
	if u := m.getUserFromCacheByUsername(username); u != nil {
		return u, nil
	}
	return m.loadUserAndCache(func() (*models.User, error) { return m.userRepo.GetByUsername(ctx, username) })
}

// GetUserByID retrieves a user by ID.
func (m *Module) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	if u := m.getUserFromCacheByID(id); u != nil {
		return u, nil
	}
	return m.loadUserAndCache(func() (*models.User, error) { return m.userRepo.GetByID(ctx, id) })
}

// loadUserAndCache runs the loader; on success caches the user and returns it, on error returns ErrUserNotFound.
func (m *Module) loadUserAndCache(load func() (*models.User, error)) (*models.User, error) {
	user, err := load()
	if err != nil {
		return nil, ErrUserNotFound
	}
	m.cacheUser(user)
	return user, nil
}

func (m *Module) getUserFromCacheByUsername(username string) *models.User {
	m.usersMu.RLock()
	defer m.usersMu.RUnlock()
	return m.users[username]
}

func (m *Module) getUserFromCacheByID(id string) *models.User {
	m.usersMu.RLock()
	defer m.usersMu.RUnlock()
	for _, u := range m.users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

// UpdateUser updates a user's information.
func (m *Module) UpdateUser(ctx context.Context, username string, updates map[string]interface{}) error {
	user, err := m.GetUser(ctx, username)
	if err != nil {
		return err
	}

	wasEnabled := user.Enabled
	oldRole := user.Role

	userCopy := *user
	user = &userCopy

	if err := m.applyUserUpdates(user, updates); err != nil {
		return err
	}

	if err := m.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	m.cacheUser(user)
	m.evictSessionsAfterUpdate(ctx, evictSessionUpdateParams{
		Username:   username,
		WasEnabled: wasEnabled,
		OldRole:    oldRole,
		User:       user,
	})
	m.log.Info("Updated user: %s", username)
	return nil
}

// AddStorageUsed atomically increments storage_used for a user (avoids read-then-write race).
func (m *Module) AddStorageUsed(ctx context.Context, userID string, delta int64) error {
	return m.userRepo.IncrementStorageUsed(ctx, userID, delta)
}

// evictSessionUpdateParams holds arguments for evictSessionsAfterUpdate (reduces function arity).
type evictSessionUpdateParams struct {
	Username   string
	WasEnabled bool
	OldRole    models.UserRole
	User       *models.User
}

// evictSessionsAfterUpdate evicts sessions when the user was disabled or demoted from admin.
func (m *Module) evictSessionsAfterUpdate(ctx context.Context, p evictSessionUpdateParams) {
	if p.WasEnabled && !p.User.Enabled {
		m.evictSessionsForUser(ctx, p.Username, "disabled")
		return
	}
	if p.OldRole == models.RoleAdmin && p.User.Role != models.RoleAdmin {
		m.evictSessionsForUser(ctx, p.Username, "role demoted from admin")
	}
}

func (m *Module) applyUserUpdates(user *models.User, updates map[string]interface{}) error {
	m.applyBasicUserUpdates(user, updates)
	if err := m.applyPasswordUpdateFromMap(user, updates["password"]); err != nil {
		return err
	}
	m.applyPermissionsFromMap(user, updates["permissions"])
	// Admin users must always retain full permissions regardless of explicit
	// overrides in the request — re-enforce after all other mutations.
	if user.Role == models.RoleAdmin {
		user.Permissions = adminPermissions()
	}
	return nil
}

// applyBasicUserUpdates sets email, enabled, type, role, and admin permissions from updates.
func (m *Module) applyBasicUserUpdates(user *models.User, updates map[string]interface{}) {
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
	if user.Role == models.RoleAdmin {
		user.Permissions = adminPermissions()
	}
}

// applyPasswordUpdateFromMap updates user password hash and salt if a non-empty password is present.
func (m *Module) applyPasswordUpdateFromMap(user *models.User, passwordVal interface{}) error {
	password, ok := passwordVal.(string)
	if !ok || password == "" {
		return nil
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	salt := generateSalt()
	hash, err := bcrypt.GenerateFromPassword([]byte(password+salt), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	user.PasswordHash = string(hash)
	user.Salt = salt
	return nil
}

// permissionField maps a permissions map key to a pointer on user.Permissions.
type permissionField struct {
	key  string
	dest *bool
}

func (m *Module) applyPermissionsFromMap(user *models.User, perms interface{}) {
	permsMap, ok := perms.(map[string]interface{})
	if !ok {
		return
	}
	fields := []permissionField{
		{"can_upload", &user.Permissions.CanUpload},
		{"can_download", &user.Permissions.CanDownload},
		{"can_stream", &user.Permissions.CanStream},
		{"can_delete", &user.Permissions.CanDelete},
		{"can_manage", &user.Permissions.CanManage},
		{"can_view_mature", &user.Permissions.CanViewMature},
		{"can_create_playlists", &user.Permissions.CanCreatePlaylists},
	}
	for _, f := range fields {
		if v, ok := permsMap[f.key].(bool); ok {
			*f.dest = v
		}
	}
}

func (m *Module) deleteSessionFromDB(ctx context.Context, sessionID string) {
	if err := m.sessionRepo.Delete(ctx, sessionID); err != nil {
		m.log.Warn("Failed to delete evicted session %s from DB: %v", sessionID, err)
	}
}

func (m *Module) evictSessionsForUser(ctx context.Context, username, reason string) {
	m.sessionsMu.Lock()
	evicted := m.evictUserFromSessionMap(ctx, m.sessions, username)
	evicted += m.evictUserFromAdminSessionMap(ctx, m.adminSessions, username)
	m.sessionsMu.Unlock()
	if evicted > 0 {
		m.log.Info("Evicted %d sessions for user %s (%s)", evicted, username, reason)
	}
}

func (m *Module) evictUserFromSessionMap(ctx context.Context, sessions map[string]*models.Session, username string) int {
	var n int
	for id, session := range sessions {
		if session.Username == username {
			delete(sessions, id)
			m.deleteSessionFromDB(ctx, id)
			n++
		}
	}
	return n
}

func (m *Module) evictUserFromAdminSessionMap(ctx context.Context, sessions map[string]*models.AdminSession, username string) int {
	var n int
	for id, session := range sessions {
		if session.Username == username {
			delete(sessions, id)
			m.deleteSessionFromDB(ctx, id)
			n++
		}
	}
	return n
}

// DeleteUser removes a user and evicts all of their sessions (user + admin) from memory and DB.
func (m *Module) DeleteUser(ctx context.Context, username string) error {
	user, err := m.GetUser(ctx, username)
	if err != nil {
		return err
	}

	if err := m.userRepo.Delete(ctx, user.ID); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	m.usersMu.Lock()
	delete(m.users, username)
	m.usersMu.Unlock()

	m.evictSessionsForUser(ctx, username, "user deleted")

	m.log.Info("Deleted user: %s", username)
	return nil
}

// ListUsers returns all users (without sensitive data)
func (m *Module) ListUsers(ctx context.Context) []*models.User {
	users, err := m.userRepo.List(ctx)
	if err != nil {
		m.log.Warn("Failed to list users from repository: %v", err)
		m.usersMu.RLock()
		defer m.usersMu.RUnlock()
		users = make([]*models.User, 0, len(m.users))
		for _, user := range m.users {
			users = append(users, user)
		}
	}

	result := make([]*models.User, len(users))
	for i, user := range users {
		userCopy := *user
		userCopy.PasswordHash = ""
		userCopy.Salt = ""
		result[i] = &userCopy
	}
	return result
}

// UpdateUserPreferences updates and persists user preferences.
// Works on a copy so a failed DB update does not leave the cache out of sync with the DB.
func (m *Module) UpdateUserPreferences(ctx context.Context, username string, prefs models.UserPreferences) error {
	prefs.Validate()

	m.usersMu.Lock()
	user, exists := m.users[username]
	if !exists {
		m.usersMu.Unlock()
		return ErrUserNotFound
	}
	userCopy := *user
	userCopy.Preferences = prefs
	if prefs.ShowMature && !userCopy.Permissions.CanViewMature {
		userCopy.Permissions.CanViewMature = true
	}
	m.usersMu.Unlock()

	if err := m.userRepo.Update(ctx, &userCopy); err != nil {
		m.log.Error("Failed to save user after preference update: %v", err)
		return err
	}
	m.usersMu.Lock()
	m.users[username] = &userCopy
	m.usersMu.Unlock()
	return nil
}
