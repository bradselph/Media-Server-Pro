package mysql_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/internal/testutil"
)

// seedMediaMetadataRow inserts a stub media_metadata row so the
// scan_results.path foreign-key constraint is satisfied. Returns the
// path that was inserted.
func seedMediaMetadataRow(t *testing.T, env *testutil.TestEnv, path string) {
	t.Helper()
	db := env.DB.GORM()
	if db == nil {
		t.Fatal("seedMediaMetadataRow: GORM handle nil")
	}
	if err := db.Exec(
		"INSERT INTO media_metadata (path, views, date_added) VALUES (?, 0, NOW())",
		path,
	).Error; err != nil {
		t.Fatalf("seedMediaMetadataRow: insert %s: %v", path, err)
	}
	t.Cleanup(func() {
		// ON DELETE CASCADE drops the scan_results row too.
		_ = db.Exec("DELETE FROM media_metadata WHERE path = ?", path).Error
	})
}

// TestFND0581_DeleteScanResult_NotFoundReturnsError verifies that Delete
// returns an error when the scan_result path does not exist.
//
// Before fix: Delete silently returned nil even when RowsAffected == 0.
// After fix: Delete checks RowsAffected and returns "scan result not found".
func TestFND0581_DeleteScanResult_NotFoundReturnsError(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewScanResultRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := repo.Delete(ctx, "/does/not/exist.mp4")
	if err == nil {
		t.Fatal("Delete returned nil for non-existent path; FND-0581 regression")
	}
}

// TestFND0581_DeleteScanResult_Success verifies the happy path: a saved
// scan result can be deleted and the call returns nil.
func TestFND0581_DeleteScanResult_Success(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewScanResultRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	path := "/test/media/scan-delete-happy.mp4"
	seedMediaMetadataRow(t, env, path)

	result := &repositories.ScanResult{
		Path:       path,
		IsMature:   true,
		Confidence: 0.95,
		ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		Reasons:    []string{"keyword-match"},
	}
	if err := repo.Save(ctx, result); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := repo.Delete(ctx, path); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := repo.Get(ctx, path); !errors.Is(err, repositories.ErrScanResultNotFound) {
		t.Fatalf("Get after Delete returned %v; want ErrScanResultNotFound", err)
	}
}

// TestFND0581_MarkReviewed_NotFoundReturnsError mirrors the same
// RowsAffected guard on MarkReviewed (the pattern Delete was made
// consistent with).
func TestFND0581_MarkReviewed_NotFoundReturnsError(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewScanResultRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := repo.MarkReviewed(ctx, "/no/such/path.mp4", "admin", "approve")
	if err == nil {
		t.Fatal("MarkReviewed returned nil for non-existent path; should fail with not-found")
	}
}
