package auth

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// FND-0036: Regression tests for UpdatePassword/SetPassword targeted DB update.
// Before the fix, both methods wrote the full user snapshot to the DB, which meant
// a concurrent Update with a stale snapshot could silently revert the new password_hash.
// After the fix, only password_hash and salt are written via UpdatePasswordHash.

type trackingUserRepo struct {
	mu                  sync.Mutex
	listUsers           []*models.User
	updatePasswordCalls []fnd0036Call
	updateFullUserCalls int
}

type fnd0036Call struct {
	username string
	hash     string
	salt     string
}

func (r *trackingUserRepo) Create(context.Context, *models.User) error { return nil }
func (r *trackingUserRepo) GetByID(context.Context, string) (*models.User, error) {
	return nil, repositories.ErrUserNotFound
}
func (r *trackingUserRepo) GetByUsername(_ context.Context, username string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.listUsers {
		if u.Username == username {
			return new(*u), nil
		}
	}
	return nil, repositories.ErrUserNotFound
}
func (r *trackingUserRepo) Update(_ context.Context, _ *models.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateFullUserCalls++
	return nil
}
func (r *trackingUserRepo) UpdatePasswordHash(_ context.Context, username, hash, salt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updatePasswordCalls = append(r.updatePasswordCalls, fnd0036Call{username, hash, salt})
	return nil
}
func (r *trackingUserRepo) Delete(context.Context, string) error { return nil }
func (r *trackingUserRepo) List(context.Context) ([]*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listUsers, nil
}
func (r *trackingUserRepo) IncrementStorageUsed(context.Context, string, int64) error { return nil }

func fnd0036HashPassword(t *testing.T, password, salt string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password+salt), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func fnd0036NewModule(t *testing.T, user *models.User, repo *trackingUserRepo) *Module {
	t.Helper()
	m := &Module{
		log:      logger.New("auth-test"),
		userRepo: repo,
		users:    map[string]*models.User{user.Username: user},
	}
	return m
}

func TestFND0036_UpdatePassword_UsesTargetedDBWrite(t *testing.T) {
	const pass, salt = "oldpassword1", "oldsalt"
	user := &models.User{
		ID:           "u1",
		Username:     "alice",
		Salt:         salt,
		PasswordHash: fnd0036HashPassword(t, pass, salt),
	}
	repo := &trackingUserRepo{listUsers: []*models.User{user}}
	m := fnd0036NewModule(t, user, repo)

	if err := m.UpdatePassword(context.Background(), "alice", pass, "newpassword99"); err != nil {
		t.Fatalf("UpdatePassword failed: %v (FND-0036 regression)", err)
	}

	repo.mu.Lock()
	passwordCalls := len(repo.updatePasswordCalls)
	fullCalls := repo.updateFullUserCalls
	repo.mu.Unlock()

	if passwordCalls != 1 {
		t.Errorf("expected 1 UpdatePasswordHash call, got %d (FND-0036 regression)", passwordCalls)
	}
	if fullCalls != 0 {
		t.Errorf("expected 0 full Update calls, got %d (FND-0036 regression)", fullCalls)
	}
}

func TestFND0036_SetPassword_UsesTargetedDBWrite(t *testing.T) {
	const pass, salt = "irrelevant", "somesalt"
	user := &models.User{
		ID:           "u2",
		Username:     "bob",
		Salt:         salt,
		PasswordHash: fnd0036HashPassword(t, pass, salt),
	}
	repo := &trackingUserRepo{listUsers: []*models.User{user}}
	m := fnd0036NewModule(t, user, repo)

	if err := m.SetPassword(context.Background(), "bob", "newpassword99"); err != nil {
		t.Fatalf("SetPassword failed: %v (FND-0036 regression)", err)
	}

	repo.mu.Lock()
	passwordCalls := len(repo.updatePasswordCalls)
	fullCalls := repo.updateFullUserCalls
	repo.mu.Unlock()

	if passwordCalls != 1 {
		t.Errorf("expected 1 UpdatePasswordHash call, got %d (FND-0036 regression)", passwordCalls)
	}
	if fullCalls != 0 {
		t.Errorf("expected 0 full Update calls, got %d (FND-0036 regression)", fullCalls)
	}
}

func TestChangeAdminPasswordKeepsConfigDatabaseAndCacheInSync(t *testing.T) {
	const oldPassword = "oldpassword1"
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	configHash := fnd0036HashPassword(t, oldPassword, "")
	if err := mgr.Update(func(c *config.Config) {
		c.Admin.Enabled = true
		c.Admin.Username = "admin"
		c.Admin.PasswordHash = configHash
	}); err != nil {
		t.Fatalf("configure admin: %v", err)
	}
	user := &models.User{ID: "admin-id", Username: "admin", PasswordHash: configHash}
	repo := &trackingUserRepo{listUsers: []*models.User{user}}
	m := fnd0036NewModule(t, user, repo)
	m.config = mgr

	if err := m.ChangeAdminPassword(context.Background(), oldPassword, "newpassword99"); err != nil {
		t.Fatalf("ChangeAdminPassword: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(mgr.Get().Admin.PasswordHash), []byte("newpassword99")); err != nil {
		t.Fatalf("config credential rejects new password: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(mgr.Get().Admin.PasswordHash), []byte(oldPassword)) == nil {
		t.Fatal("config credential still accepts old password")
	}

	repo.mu.Lock()
	if len(repo.updatePasswordCalls) != 1 {
		repo.mu.Unlock()
		t.Fatalf("UpdatePasswordHash calls = %d, want 1", len(repo.updatePasswordCalls))
	}
	call := repo.updatePasswordCalls[0]
	repo.mu.Unlock()
	if call.salt == "" {
		t.Fatal("database admin credential was not given its independent salt")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(call.hash), []byte("newpassword99"+call.salt)); err != nil {
		t.Fatalf("database credential rejects new password: %v", err)
	}
	m.usersMu.RLock()
	cached := new(*m.users["admin"])
	m.usersMu.RUnlock()
	if cached.PasswordHash != call.hash || cached.Salt != call.salt {
		t.Fatal("admin cache diverged from targeted database credential")
	}
}

func TestUpdateUserPasswordRevokesCachedSessions(t *testing.T) {
	const oldPassword, oldSalt = "oldpassword1", "old-salt"
	user := &models.User{
		ID:           "u3",
		Username:     "carol",
		Enabled:      true,
		PasswordHash: fnd0036HashPassword(t, oldPassword, oldSalt),
		Salt:         oldSalt,
	}
	repo := &trackingUserRepo{listUsers: []*models.User{user}}
	m := fnd0036NewModule(t, user, repo)
	m.usersByID = map[string]*models.User{user.ID: user}
	m.sessions = map[string]*models.Session{
		"session-1": {ID: "session-1", UserID: user.ID, Username: user.Username},
	}
	m.adminSessions = make(map[string]*models.AdminSession)

	if err := m.UpdateUser(context.Background(), user.Username, map[string]any{"password": "newpassword99"}); err != nil {
		t.Fatalf("UpdateUser password: %v", err)
	}
	m.sessionsMu.RLock()
	_, stillCached := m.sessions["session-1"]
	m.sessionsMu.RUnlock()
	if stillCached {
		t.Fatal("password changed through UpdateUser without revoking the old session")
	}
}
