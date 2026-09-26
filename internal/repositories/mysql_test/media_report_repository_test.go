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

// TestR19_UpdateMediaReportStatus_NotFoundReturnsError verifies that
// UpdateStatus returns repositories.ErrMediaReportNotFound (instead of a
// silent nil "success") when the report id does not exist.
//
// Before fix: UpdateStatus only checked result.Error, so a PATCH against a
// nonexistent/already-deleted report id returned nil even though
// RowsAffected was 0, and the handler reported HTTP 200 as if the update
// had happened.
// After fix: RowsAffected == 0 is treated as a not-found error.
func TestR19_UpdateMediaReportStatus_NotFoundReturnsError(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewMediaReportRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := repo.UpdateStatus(ctx, "does-not-exist", "resolved", "admin")
	if err == nil {
		t.Fatal("UpdateStatus on a nonexistent report returned nil; want ErrMediaReportNotFound (R19 regression)")
	}
	if !errors.Is(err, repositories.ErrMediaReportNotFound) {
		t.Fatalf("UpdateStatus error = %v, want it to wrap ErrMediaReportNotFound", err)
	}
}

// TestR19_UpdateMediaReportStatus_Success verifies the happy path still
// works: updating an existing report's status returns nil and persists.
func TestR19_UpdateMediaReportStatus_Success(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewMediaReportRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rec := &repositories.MediaReportRecord{
		ID:      "report-success-1",
		MediaID: "media-1",
		Reason:  "other",
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.UpdateStatus(ctx, rec.ID, "resolved", "admin1"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	reports, err := repo.List(ctx, "resolved", 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, r := range reports {
		if r.ID == rec.ID {
			found = true
			if r.ResolvedBy != "admin1" {
				t.Errorf("ResolvedBy = %q, want %q", r.ResolvedBy, "admin1")
			}
		}
	}
	if !found {
		t.Fatalf("updated report %s not found in resolved list", rec.ID)
	}
}

// TestUpdateMediaReportStatus_NoopUpdateOnExistingReportReturnsNil verifies
// that UpdateStatus does not mistake a no-op update for a missing report.
//
// Under this project's MySQL DSN (no CLIENT_FOUND_ROWS), an UPDATE whose
// WHERE clause matches a row but whose new column values equal the existing
// ones reports RowsAffected == 0 — the same value reported when no row
// matches at all. A freshly created report is already status="open",
// resolved_by="", resolved_at=NULL, so reopening it immediately (e.g. a
// retried PATCH after a proxy hiccup) is exactly such a no-op update.
// Before the fix this was misreported as repositories.ErrMediaReportNotFound
// even though the report plainly exists.
func TestUpdateMediaReportStatus_NoopUpdateOnExistingReportReturnsNil(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewMediaReportRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rec := &repositories.MediaReportRecord{
		ID:      "report-noop-1",
		MediaID: "media-1",
		Reason:  "other",
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The report is already status="open"/resolved_by=""/resolved_at=NULL, so
	// this update is a no-op at the row level (RowsAffected == 0) even though
	// the report exists.
	if err := repo.UpdateStatus(ctx, rec.ID, "open", ""); err != nil {
		t.Fatalf("UpdateStatus no-op on existing report returned %v, want nil", err)
	}

	reports, err := repo.List(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, r := range reports {
		if r.ID == rec.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("report %s vanished after a no-op UpdateStatus", rec.ID)
	}
}

// TestR07_AnonymizeReporter_ClearsIdentityKeepsReport verifies that
// AnonymizeReporter strips reporter_id/ip_address for the given user while
// leaving the report row itself (and other users' reports) intact.
func TestR07_AnonymizeReporter_ClearsIdentityKeepsReport(t *testing.T) {
	env := testutil.NewTestEnv(t)
	repo := mysql.NewMediaReportRepository(env.DB.GORM())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mine := &repositories.MediaReportRecord{
		ID:         "report-mine",
		MediaID:    "media-1",
		ReporterID: "user-to-delete",
		Reason:     "spam",
		IPAddress:  "10.0.0.1",
	}
	other := &repositories.MediaReportRecord{
		ID:         "report-other",
		MediaID:    "media-2",
		ReporterID: "some-other-user",
		Reason:     "spam",
		IPAddress:  "10.0.0.2",
	}
	if err := repo.Create(ctx, mine); err != nil {
		t.Fatalf("Create mine: %v", err)
	}
	if err := repo.Create(ctx, other); err != nil {
		t.Fatalf("Create other: %v", err)
	}

	if err := repo.AnonymizeReporter(ctx, "user-to-delete"); err != nil {
		t.Fatalf("AnonymizeReporter: %v", err)
	}

	reports, err := repo.List(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var gotMine, gotOther *repositories.MediaReportRecord
	for _, r := range reports {
		switch r.ID {
		case mine.ID:
			gotMine = r
		case other.ID:
			gotOther = r
		}
	}
	if gotMine == nil {
		t.Fatal("report filed by the deleted user was removed; AnonymizeReporter must keep the report")
	}
	if gotMine.ReporterID != "" {
		t.Errorf("ReporterID after anonymize = %q, want empty", gotMine.ReporterID)
	}
	if gotMine.IPAddress != "" {
		t.Errorf("IPAddress after anonymize = %q, want empty", gotMine.IPAddress)
	}
	if gotOther == nil {
		t.Fatal("other user's report disappeared")
	}
	if gotOther.ReporterID != "some-other-user" {
		t.Errorf("other user's ReporterID was scrubbed: got %q, want unchanged", gotOther.ReporterID)
	}
}
