package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

// stubSavedSearchRepo implements repositories.SavedSearchRepository for
// DeleteUser purge tests. Only DeleteAllByUser is exercised.
type stubSavedSearchRepo struct {
	deleteAllByUserCalls []string
	deleteAllErr         error
}

func (s *stubSavedSearchRepo) Create(context.Context, *repositories.SavedSearchRecord) error {
	return nil
}
func (s *stubSavedSearchRepo) Delete(context.Context, string, string) error { return nil }
func (s *stubSavedSearchRepo) List(context.Context, string) ([]*repositories.SavedSearchRecord, error) {
	return nil, nil
}
func (s *stubSavedSearchRepo) Get(context.Context, string, string) (*repositories.SavedSearchRecord, error) {
	return nil, nil
}
func (s *stubSavedSearchRepo) UpdateLastSeen(context.Context, string, string, time.Time) error {
	return nil
}
func (s *stubSavedSearchRepo) DeleteAllByUser(_ context.Context, userID string) error {
	s.deleteAllByUserCalls = append(s.deleteAllByUserCalls, userID)
	return s.deleteAllErr
}

// TestPurgeUnfederatedUserData_NoReposConfiguredIsNoop verifies purge doesn't
// panic or error when a Module has no dbModule/savedSearchRepo wired up
// (e.g. the hand-built Modules used across this file's other tests), so
// DeleteUser keeps working for callers/tests that don't set up the full
// dependency graph.
func TestPurgeUnfederatedUserData_NoReposConfiguredIsNoop(t *testing.T) {
	m := &Module{log: logger.New("auth-test")}
	if err := m.purgeUnfederatedUserData(context.Background(), "user-1"); err != nil {
		t.Fatalf("purgeUnfederatedUserData with no repos configured: got %v, want nil", err)
	}
}

// TestDeleteUser_PurgesSavedSearches verifies R07: deleting a user also
// erases their saved_searches rows (which carry no FK cascade to users(id)),
// not just the account row.
func TestDeleteUser_PurgesSavedSearches(t *testing.T) {
	ctx := context.Background()
	viewer := &models.User{ID: "u1", Username: "viewer1", Role: models.RoleViewer, Enabled: true}
	m, _ := testModuleWithUsers(t, []*models.User{viewer})
	savedSearches := &stubSavedSearchRepo{}
	m.savedSearchRepo = savedSearches

	if err := m.DeleteUser(ctx, "viewer1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if len(savedSearches.deleteAllByUserCalls) != 1 || savedSearches.deleteAllByUserCalls[0] != "u1" {
		t.Fatalf("DeleteAllByUser calls = %v, want a single call with user id %q", savedSearches.deleteAllByUserCalls, "u1")
	}
}

// TestDeleteUser_SavedSearchPurgeFailureIsNotSwallowed verifies that DeleteUser
// reports an error (rather than a false "deleted" success) when purging a
// dependent store fails, per R07's "handle errors" requirement.
func TestDeleteUser_SavedSearchPurgeFailureIsNotSwallowed(t *testing.T) {
	ctx := context.Background()
	viewer := &models.User{ID: "u1", Username: "viewer1", Role: models.RoleViewer, Enabled: true}
	m, repo := testModuleWithUsers(t, []*models.User{viewer})
	wantErr := errors.New("saved_searches delete failed")
	m.savedSearchRepo = &stubSavedSearchRepo{deleteAllErr: wantErr}

	err := m.DeleteUser(ctx, "viewer1")
	if err == nil {
		t.Fatal("DeleteUser: got nil error, want a non-nil error surfacing the purge failure")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("DeleteUser error = %v, want it to wrap %v", err, wantErr)
	}
	// The account row is still removed even though dependent-data cleanup
	// failed, matching the existing best-effort behavior of session eviction
	// (see evictSessionsForUser's error handling in DeleteUser).
	if repo.deletedID != "u1" {
		t.Fatalf("deletedID = %q, want u1 (user row should still be deleted)", repo.deletedID)
	}
}
