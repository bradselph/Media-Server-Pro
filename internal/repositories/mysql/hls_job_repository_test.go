package mysql

import (
	"testing"
	"time"

	"media-server-pro/pkg/models"
)

func TestNewHLSJobRepository_NilDBPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewHLSJobRepository(nil) should panic")
		}
	}()
	NewHLSJobRepository(nil)
}

func TestHLSJobRow_TableName(t *testing.T) {
	row := hlsJobRow{}
	if row.TableName() != "hls_jobs" {
		t.Errorf("TableName() = %q, want %q", row.TableName(), "hls_jobs")
	}
}

func TestJobToRow_MinimalJob(t *testing.T) {
	repo := &HLSJobRepository{}
	job := &models.HLSJob{
		ID:        "job-1",
		MediaPath: "/video.mp4",
		OutputDir: "/hls/job-1",
		Status:    models.HLSStatusPending,
		Qualities: []string{"720p"},
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	row, err := repo.jobToRow(job)
	if err != nil {
		t.Fatalf("jobToRow error: %v", err)
	}
	if row.ID != "job-1" {
		t.Errorf("ID = %q, want %q", row.ID, "job-1")
	}
	if row.Status != "pending" {
		t.Errorf("Status = %q, want %q", row.Status, "pending")
	}
	if row.Qualities != `["720p"]` {
		t.Errorf("Qualities = %q, want %q", row.Qualities, `["720p"]`)
	}
	if row.Error != nil {
		t.Error("Error should be nil for empty string")
	}
	if row.HLSUrl != nil {
		t.Error("HLSUrl should be nil for empty string")
	}
	if row.CompletedAt != nil {
		t.Error("CompletedAt should be nil")
	}
}

func TestJobToRow_WithOptionalFields(t *testing.T) {
	repo := &HLSJobRepository{}
	completedAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	lastAccess := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	job := &models.HLSJob{
		ID:             "job-2",
		MediaPath:      "/video.mp4",
		Status:         models.HLSStatusCompleted,
		Qualities:      []string{"720p", "1080p"},
		StartedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CompletedAt:    &completedAt,
		LastAccessedAt: &lastAccess,
		Error:          "some error",
		HLSUrl:         "https://example.com/hls",
		FailCount:      3,
		Available:      true,
	}

	row, err := repo.jobToRow(job)
	if err != nil {
		t.Fatalf("jobToRow error: %v", err)
	}
	if row.CompletedAt == nil || !row.CompletedAt.Equal(completedAt) {
		t.Error("CompletedAt mismatch")
	}
	if row.LastAccessedAt == nil || !row.LastAccessedAt.Equal(lastAccess) {
		t.Error("LastAccessedAt mismatch")
	}
	if row.Error == nil || *row.Error != "some error" {
		t.Errorf("Error = %v, want 'some error'", row.Error)
	}
	if row.HLSUrl == nil || *row.HLSUrl != "https://example.com/hls" {
		t.Errorf("HLSUrl = %v, want 'https://example.com/hls'", row.HLSUrl)
	}
	if row.FailCount != 3 {
		t.Errorf("FailCount = %d, want 3", row.FailCount)
	}
	if !row.Available {
		t.Error("Available should be true")
	}
}

func TestRowToJob_MinimalRow(t *testing.T) {
	repo := &HLSJobRepository{}
	row := &hlsJobRow{
		ID:        "job-1",
		MediaPath: "/video.mp4",
		Status:    "pending",
		Qualities: `["720p"]`,
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	job, err := repo.rowToJob(row)
	if err != nil {
		t.Fatalf("rowToJob error: %v", err)
	}
	if job.ID != "job-1" {
		t.Errorf("ID = %q, want %q", job.ID, "job-1")
	}
	if job.Status != models.HLSStatusPending {
		t.Errorf("Status = %q, want %q", job.Status, models.HLSStatusPending)
	}
	if len(job.Qualities) != 1 || job.Qualities[0] != "720p" {
		t.Errorf("Qualities = %v, want [720p]", job.Qualities)
	}
	if job.Error != "" {
		t.Errorf("Error = %q, want empty", job.Error)
	}
}

func TestRowToJob_EmptyQualities(t *testing.T) {
	repo := &HLSJobRepository{}
	row := &hlsJobRow{
		ID:        "job-1",
		Status:    "pending",
		Qualities: "",
	}

	job, err := repo.rowToJob(row)
	if err != nil {
		t.Fatalf("rowToJob error: %v", err)
	}
	if job.Qualities == nil {
		t.Fatal("Qualities should not be nil, should be empty slice")
	}
	if len(job.Qualities) != 0 {
		t.Errorf("Qualities = %v, want empty slice", job.Qualities)
	}
}

func TestRowToJob_InvalidQualitiesJSON(t *testing.T) {
	repo := &HLSJobRepository{}
	row := &hlsJobRow{
		ID:        "job-1",
		Status:    "pending",
		Qualities: `{not valid json`,
	}

	_, err := repo.rowToJob(row)
	if err == nil {
		t.Fatal("rowToJob should return error for invalid JSON")
	}
}

func TestRowToJob_WithOptionalFields(t *testing.T) {
	repo := &HLSJobRepository{}
	errMsg := "transcode failed"
	hlsURL := "https://cdn.example.com/hls/master.m3u8"
	row := &hlsJobRow{
		ID:        "job-3",
		Status:    "failed",
		Qualities: `[]`,
		Error:     &errMsg,
		HLSUrl:    &hlsURL,
		FailCount: 5,
	}

	job, err := repo.rowToJob(row)
	if err != nil {
		t.Fatalf("rowToJob error: %v", err)
	}
	if job.Error != errMsg {
		t.Errorf("Error = %q, want %q", job.Error, errMsg)
	}
	if job.HLSUrl != hlsURL {
		t.Errorf("HLSUrl = %q, want %q", job.HLSUrl, hlsURL)
	}
}

func TestJobToRow_RoundTrip(t *testing.T) {
	repo := &HLSJobRepository{}
	completedAt := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	original := &models.HLSJob{
		ID:          "roundtrip-1",
		MediaPath:   "/test/video.mkv",
		OutputDir:   "/hls/roundtrip-1",
		Status:      models.HLSStatusCompleted,
		Progress:    100.0,
		Qualities:   []string{"480p", "720p", "1080p"},
		StartedAt:   time.Date(2026, 6, 15, 11, 0, 0, 0, time.UTC),
		CompletedAt: &completedAt,
		HLSUrl:      "https://example.com/hls/master.m3u8",
		FailCount:   1,
		Available:   true,
	}

	row, err := repo.jobToRow(original)
	if err != nil {
		t.Fatalf("jobToRow error: %v", err)
	}
	result, err := repo.rowToJob(&row)
	if err != nil {
		t.Fatalf("rowToJob error: %v", err)
	}

	if result.ID != original.ID {
		t.Errorf("ID: got %q, want %q", result.ID, original.ID)
	}
	if result.Status != original.Status {
		t.Errorf("Status: got %q, want %q", result.Status, original.Status)
	}
	if len(result.Qualities) != len(original.Qualities) {
		t.Fatalf("Qualities length: got %d, want %d", len(result.Qualities), len(original.Qualities))
	}
	for i := range original.Qualities {
		if result.Qualities[i] != original.Qualities[i] {
			t.Errorf("Qualities[%d]: got %q, want %q", i, result.Qualities[i], original.Qualities[i])
		}
	}
	if result.HLSUrl != original.HLSUrl {
		t.Errorf("HLSUrl: got %q, want %q", result.HLSUrl, original.HLSUrl)
	}
}

func TestSave_NilJobReturnsError(t *testing.T) {
	repo := &HLSJobRepository{}
	err := repo.Save(t.Context(), nil)
	if err == nil {
		t.Fatal("Save(nil) should return an error")
	}
}
