package mysql_test

import (
	"context"
	"testing"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/internal/testutil"
)

// TestFND0050_DeleteAPIToken_TokenNotFound verifies that Delete() returns
// ErrAPITokenNotFound when attempting to delete a non-existent token.
//
// This is a regression test for FND-0050:
// Before fix: Delete() would return nil (success) even if zero rows were deleted.
// After fix: Delete() returns ErrAPITokenNotFound when RowsAffected == 0.
func TestFND0050_DeleteAPIToken_TokenNotFound(t *testing.T) {
	cfg := testutil.TestConfig(t)
	dbModule := testutil.TestDBModule(t, cfg)

	repo := mysql.NewAPITokenRepository(dbModule.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to delete a token that doesn't exist
	err := repo.Delete(ctx, "nonexistent-token-id", "user-123")

	// After fix: should return ErrAPITokenNotFound (not nil)
	if err == nil {
		t.Fatal("Delete() returned nil for non-existent token; should return ErrAPITokenNotFound (FND-0050 regression)")
	}

	if err != repositories.ErrAPITokenNotFound {
		t.Fatalf("Delete() returned wrong error: %v; expected ErrAPITokenNotFound (FND-0050 regression)", err)
	}

	t.Log("FND-0050 regression test passed: Delete() correctly returns ErrAPITokenNotFound for non-existent token")
}

// TestFND0050_DeleteAPIToken_WrongUserID verifies that Delete() returns
// ErrAPITokenNotFound when attempting to delete a token with the wrong user_id.
//
// This is a regression test for FND-0050:
// The WHERE clause "id=? AND user_id=?" prevents cross-user token deletion.
// Before fix: 0 rows deleted, but nil was returned (authorization bypass)
// After fix: Returns ErrAPITokenNotFound, preventing false success
func TestFND0050_DeleteAPIToken_WrongUserID(t *testing.T) {
	cfg := testutil.TestConfig(t)
	dbModule := testutil.TestDBModule(t, cfg)

	repo := mysql.NewAPITokenRepository(dbModule.GORM())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a token for user1
	user1ID := "user-001"
	tokenID := "token-abc123"
	tokenHash := "hash_of_token_value"
	expiresAt := time.Now().Add(1 * time.Hour)

	token := &repositories.APITokenRecord{
		ID:        tokenID,
		UserID:    user1ID,
		Name:      "Test Token",
		TokenHash: tokenHash,
		ExpiresAt: &expiresAt,
		CreatedAt: time.Now(),
	}

	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify token exists for user1
	tokens, err := repo.ListByUser(ctx, user1ID)
	if err != nil {
		t.Fatalf("ListByUser() failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("Expected 1 token for user1, got %d", len(tokens))
	}

	// Attempt to delete the token as a different user (user2)
	user2ID := "user-002"
	err = repo.Delete(ctx, tokenID, user2ID)

	// After fix: should return ErrAPITokenNotFound (not nil)
	if err == nil {
		t.Fatal("Delete() returned nil when deleting with wrong userID; should return ErrAPITokenNotFound (FND-0050 regression - authorization bypass)")
	}

	if err != repositories.ErrAPITokenNotFound {
		t.Fatalf("Delete() returned wrong error: %v; expected ErrAPITokenNotFound (FND-0050 regression)", err)
	}

	// Verify token still exists for user1 (wasn't deleted)
	tokens, err = repo.ListByUser(ctx, user1ID)
	if err != nil {
		t.Fatalf("ListByUser() after failed delete failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("Token was deleted despite wrong userID; expected 1 token, got %d (FND-0050 regression)", len(tokens))
	}

	t.Log("FND-0050 regression test passed: Delete() correctly prevents cross-user token deletion by returning ErrAPITokenNotFound")
}

// TestFND0050_DeleteAPIToken_Success verifies that Delete() returns nil
// when successfully deleting an existing token with the correct user_id.
//
// This test ensures the fix doesn't break the happy path.
func TestFND0050_DeleteAPIToken_Success(t *testing.T) {
	cfg := testutil.TestConfig(t)
	dbModule := testutil.TestDBModule(t, cfg)

	repo := mysql.NewAPITokenRepository(dbModule.GORM())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a token
	userID := "user-success"
	tokenID := "token-success-123"
	tokenHash := "hash_success"
	expiresAt := time.Now().Add(1 * time.Hour)

	token := &repositories.APITokenRecord{
		ID:        tokenID,
		UserID:    userID,
		Name:      "Success Token",
		TokenHash: tokenHash,
		ExpiresAt: &expiresAt,
		CreatedAt: time.Now(),
	}

	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify token exists
	tokens, err := repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser() failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("Expected 1 token, got %d", len(tokens))
	}

	// Delete with correct userID
	err = repo.Delete(ctx, tokenID, userID)

	// Should succeed and return nil
	if err != nil {
		t.Fatalf("Delete() failed unexpectedly: %v (FND-0050 regression - happy path broken)", err)
	}

	// Verify token was deleted
	tokens, err = repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser() after delete failed: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("Token was not deleted; expected 0 tokens, got %d", len(tokens))
	}

	t.Log("FND-0050 regression test passed: Delete() successfully deletes token with correct userID")
}
