package auth

import (
	"context"
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// storageUserRepo is a minimal UserRepository that records IncrementStorageUsed.
type storageUserRepo struct {
	incrementCalls int
	lastDelta      int64
}

func (r *storageUserRepo) Create(context.Context, *models.User) error { return nil }
func (r *storageUserRepo) GetByID(context.Context, string) (*models.User, error) {
	return nil, repositories.ErrUserNotFound
}
func (r *storageUserRepo) GetByUsername(context.Context, string) (*models.User, error) {
	return nil, repositories.ErrUserNotFound
}
func (r *storageUserRepo) Update(context.Context, *models.User) error { return nil }
func (r *storageUserRepo) UpdatePasswordHash(context.Context, string, string, string) error {
	return nil
}
func (r *storageUserRepo) Delete(context.Context, string) error         { return nil }
func (r *storageUserRepo) List(context.Context) ([]*models.User, error) { return nil, nil }
func (r *storageUserRepo) IncrementStorageUsed(_ context.Context, _ string, delta int64) error {
	r.incrementCalls++
	r.lastDelta = delta
	return nil
}

// TestAddStorageUsed_UpdatesInMemoryCache guards the quota-bypass fix: GetUser*
// short-circuits on the cache, so AddStorageUsed must mirror the delta into the
// cached user (clamped at 0) instead of only writing the DB.
func TestAddStorageUsed_UpdatesInMemoryCache(t *testing.T) {
	repo := &storageUserRepo{}
	user := &models.User{ID: "u1", Username: "alice", StorageUsed: 1000}
	// users and usersByID share the same *User pointer, exactly as cacheUser stores it.
	m := &Module{
		log:       logger.New("auth-test"),
		userRepo:  repo,
		users:     map[string]*models.User{user.Username: user},
		usersByID: map[string]*models.User{user.ID: user},
	}

	if err := m.AddStorageUsed(context.Background(), "u1", 500); err != nil {
		t.Fatalf("AddStorageUsed: %v", err)
	}
	if repo.incrementCalls != 1 || repo.lastDelta != 500 {
		t.Fatalf("repo increment calls=%d delta=%d, want 1/500", repo.incrementCalls, repo.lastDelta)
	}

	// GetUserByID short-circuits on the cache; it must now see the new total.
	got, err := m.GetUserByID(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.StorageUsed != 1500 {
		t.Errorf("cached StorageUsed = %d, want 1500", got.StorageUsed)
	}

	// A large negative delta clamps at 0, mirroring the DB GREATEST(used+delta, 0).
	if err := m.AddStorageUsed(context.Background(), "u1", -100000); err != nil {
		t.Fatalf("AddStorageUsed(negative): %v", err)
	}
	got, err = m.GetUserByID(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.StorageUsed != 0 {
		t.Errorf("cached StorageUsed after large negative delta = %d, want 0 (clamped)", got.StorageUsed)
	}
}
