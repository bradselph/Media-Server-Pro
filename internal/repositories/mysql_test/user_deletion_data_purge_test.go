package mysql_test

import (
	"context"
	"testing"
	"time"

	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/internal/testutil"
	"media-server-pro/pkg/models"
)

// TestR07_DeleteUser_PurgesAnalyticsAndSavedSearches is an end-to-end
// regression test for R07: approving a data-deletion request (which calls
// auth.Module.DeleteUser) must erase the user's rows from every user-keyed
// store, not just the users table. analytics_events and saved_searches have
// no ON DELETE CASCADE back to users(id), so before the fix these rows
// survived the account deletion indefinitely (including PII in
// analytics_events: ip_address, user_agent).
func TestR07_DeleteUser_PurgesAnalyticsAndSavedSearches(t *testing.T) {
	env := testutil.NewTestEnv(t)
	user := env.CreateTestUser(t, "r07-delete-me", "password123!")

	db := env.DB.GORM()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	analyticsRepo := mysql.NewAnalyticsRepository(db)
	if err := analyticsRepo.Create(ctx, &models.AnalyticsEvent{
		ID:        "r07-evt-1",
		Type:      "view",
		UserID:    user.ID,
		IPAddress: "203.0.113.5",
		UserAgent: "regression-test-agent",
	}); err != nil {
		t.Fatalf("seed analytics event: %v", err)
	}

	savedSearchRepo := mysql.NewSavedSearchRepository(db)
	if err := savedSearchRepo.Create(ctx, &repositories.SavedSearchRecord{
		ID:      "r07-search-1",
		UserID:  user.ID,
		Name:    "my search",
		Query:   "cats",
		TagMode: "or",
	}); err != nil {
		t.Fatalf("seed saved search: %v", err)
	}

	if err := env.Auth.DeleteUser(ctx, user.Username); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	var analyticsCount int64
	if err := db.WithContext(ctx).Table("analytics_events").Where("user_id = ?", user.ID).Count(&analyticsCount).Error; err != nil {
		t.Fatalf("count analytics_events: %v", err)
	}
	if analyticsCount != 0 {
		t.Errorf("analytics_events rows for deleted user = %d, want 0 (R07 regression)", analyticsCount)
	}

	var savedSearchCount int64
	if err := db.WithContext(ctx).Table("saved_searches").Where("user_id = ?", user.ID).Count(&savedSearchCount).Error; err != nil {
		t.Fatalf("count saved_searches: %v", err)
	}
	if savedSearchCount != 0 {
		t.Errorf("saved_searches rows for deleted user = %d, want 0 (R07 regression)", savedSearchCount)
	}
}

// TestR07_DeleteUser_AnonymizesMediaReports verifies that DeleteUser scrubs
// the deleted user's identity from media_reports (reporter_id, ip_address)
// while keeping the report itself as moderation history.
func TestR07_DeleteUser_AnonymizesMediaReports(t *testing.T) {
	env := testutil.NewTestEnv(t)
	user := env.CreateTestUser(t, "r07-reporter", "password123!")

	db := env.DB.GORM()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reportRepo := mysql.NewMediaReportRepository(db)
	if err := reportRepo.Create(ctx, &repositories.MediaReportRecord{
		ID:         "r07-report-1",
		MediaID:    "media-xyz",
		ReporterID: user.ID,
		Reason:     "other",
		IPAddress:  "198.51.100.9",
	}); err != nil {
		t.Fatalf("seed media report: %v", err)
	}

	if err := env.Auth.DeleteUser(ctx, user.Username); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	reports, err := reportRepo.List(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var got *repositories.MediaReportRecord
	for _, r := range reports {
		if r.ID == "r07-report-1" {
			got = r
		}
	}
	if got == nil {
		t.Fatal("media report was removed; DeleteUser must anonymize, not delete, moderation reports")
	}
	if got.ReporterID != "" {
		t.Errorf("ReporterID after DeleteUser = %q, want empty (R07 regression)", got.ReporterID)
	}
	if got.IPAddress != "" {
		t.Errorf("IPAddress after DeleteUser = %q, want empty (R07 regression)", got.IPAddress)
	}
}
