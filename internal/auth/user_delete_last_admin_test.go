package auth

import (
	"context"
	"errors"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// stubUserRepo implements repositories.UserRepository for DeleteUser last-admin tests.
type stubUserRepo struct {
	listUsers []*models.User
	deletedID string
}

func (s *stubUserRepo) Create(context.Context, *models.User) error { return nil }
func (s *stubUserRepo) GetByID(context.Context, string) (*models.User, error) {
	return nil, repositories.ErrUserNotFound
}
func (s *stubUserRepo) GetByUsername(context.Context, string) (*models.User, error) {
	return nil, repositories.ErrUserNotFound
}
func (s *stubUserRepo) Update(context.Context, *models.User) error { return nil }
func (s *stubUserRepo) Delete(_ context.Context, id string) error {
	s.deletedID = id
	return nil
}
func (s *stubUserRepo) List(context.Context) ([]*models.User, error) { return s.listUsers, nil }
func (s *stubUserRepo) IncrementStorageUsed(context.Context, string, int64) error {
	return nil
}

func testModuleWithUsers(t *testing.T, users []*models.User) (*Module, *stubUserRepo) {
	t.Helper()
	repo := &stubUserRepo{listUsers: users}
	m := &Module{
		log:      logger.New("auth-test"),
		userRepo: repo,
		users:    make(map[string]*models.User),
	}
	for _, u := range users {
		m.users[u.Username] = new(*u)
	}
	return m, repo
}

func TestDeleteUser_LastEnabledAdminRejected(t *testing.T) {
	ctx := context.Background()
	sole := &models.User{
		ID: "a1", Username: "admin1", Role: models.RoleAdmin, Enabled: true,
	}
	m, repo := testModuleWithUsers(t, []*models.User{sole})

	err := m.DeleteUser(ctx, "admin1")
	if !errors.Is(err, ErrCannotDemoteLastAdmin) {
		t.Fatalf("DeleteUser sole admin: got %v, want ErrCannotDemoteLastAdmin", err)
	}
	if repo.deletedID != "" {
		t.Fatalf("expected no DB delete, got deleted id %q", repo.deletedID)
	}
}

func TestDeleteUser_TwoAdminsDeletesOne(t *testing.T) {
	ctx := context.Background()
	a := &models.User{
		ID: "a1", Username: "admin1", Role: models.RoleAdmin, Enabled: true,
	}
	b := &models.User{
		ID: "a2", Username: "admin2", Role: models.RoleAdmin, Enabled: true,
	}
	m, repo := testModuleWithUsers(t, []*models.User{a, b})

	if err := m.DeleteUser(ctx, "admin1"); err != nil {
		t.Fatalf("DeleteUser with two admins: %v", err)
	}
	if repo.deletedID != "a1" {
		t.Fatalf("deleted id = %q, want a1", repo.deletedID)
	}
}
