package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
	"media-server-pro/pkg/models"
)

type scannerCommitRepo struct {
	saveErr   error
	markErr   error
	markCalls int
}

func (r *scannerCommitRepo) Save(context.Context, *repositories.ScanResult) error { return r.saveErr }
func (r *scannerCommitRepo) Get(context.Context, string) (*repositories.ScanResult, error) {
	return nil, repositories.ErrScanResultNotFound
}
func (r *scannerCommitRepo) GetByPaths(context.Context, []string) (map[string]*repositories.ScanResult, error) {
	return nil, nil
}
func (r *scannerCommitRepo) GetPendingReview(context.Context) ([]*repositories.ScanResult, error) {
	return nil, nil
}
func (r *scannerCommitRepo) MarkReviewed(context.Context, string, string, string) error {
	r.markCalls++
	return r.markErr
}
func (r *scannerCommitRepo) ClearPendingReview(context.Context) error { return nil }
func (r *scannerCommitRepo) Delete(context.Context, string) error     { return nil }

func newCommitScanner(t *testing.T, repo repositories.ScanResultRepository) (*MatureScanner, string) {
	t.Helper()
	mgr := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	s := NewMatureScanner(mgr)
	s.scanRepo = repo
	path := filepath.Join(t.TempDir(), "review.mp4")
	now := time.Now()
	s.results[path] = &ScanResult{
		Path:        path,
		Confidence:  0.7,
		Reasons:     []string{"test"},
		NeedsReview: true,
		ScannedAt:   now,
	}
	s.reviewQueue[path] = &models.MatureReviewItem{
		ID:         stableReviewID(path),
		Name:       filepath.Base(path),
		MediaPath:  path,
		DetectedAt: now,
		Confidence: 0.7,
		Reasons:    []string{"test"},
	}
	return s, path
}

func TestReviewCommitRollsBackWhenScannerPersistenceFails(t *testing.T) {
	repo := &scannerCommitRepo{markErr: errors.New("database unavailable")}
	s, path := newCommitScanner(t, repo)
	externalState := false

	err := s.ReviewItemWithCommit(context.Background(), path, "admin", "approve", func(*ScanResult) (func() error, error) {
		externalState = true
		return func() error {
			externalState = false
			return nil
		}, nil
	})
	if err == nil {
		t.Fatal("review unexpectedly succeeded")
	}
	if externalState {
		t.Fatal("external media state was not rolled back")
	}
	if repo.markCalls != 1 {
		t.Fatalf("MarkReviewed calls = %d, want 1", repo.markCalls)
	}
	if queue := s.GetReviewQueue(); len(queue) != 1 {
		t.Fatalf("failed review was removed from queue: %#v", queue)
	}
	if result, _ := s.GetScanResult(path); result == nil || result.ReviewDecision != "" || !result.NeedsReview {
		t.Fatalf("failed review mutated cached result: %#v", result)
	}
}

func TestReviewCommitFailureDoesNotTouchScannerRepository(t *testing.T) {
	repo := &scannerCommitRepo{}
	s, path := newCommitScanner(t, repo)
	want := errors.New("media write failed")
	err := s.ReviewItemWithCommit(context.Background(), path, "admin", "reject", func(*ScanResult) (func() error, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("ReviewItemWithCommit error = %v, want wrapped %v", err, want)
	}
	if repo.markCalls != 0 {
		t.Fatalf("MarkReviewed called %d times after external commit failure", repo.markCalls)
	}
}

func TestUnpersistedFreshResultIsWithheldUntilSaveRetrySucceeds(t *testing.T) {
	repo := &scannerCommitRepo{saveErr: errors.New("database unavailable")}
	s, path := newCommitScanner(t, repo)
	// Model a freshly computed but not-yet-persisted result. The retry path must
	// not require the source file to still exist because no rescan is needed.
	s.reviewQueue = make(map[string]*models.MatureReviewItem)
	s.results[path].dirty = true

	first := s.ScanFile(path)
	if first == nil || !first.NeedsReview {
		t.Fatalf("first retry result = %#v", first)
	}
	if queue := s.GetReviewQueue(); len(queue) != 0 {
		t.Fatalf("unpersisted result became actionable: %#v", queue)
	}
	s.mu.RLock()
	dirtyAfterFailure := s.results[path].dirty
	s.mu.RUnlock()
	if !dirtyAfterFailure {
		t.Fatal("failed repository save was treated as durable")
	}

	repo.saveErr = nil
	s.ResetRepoState()
	second := s.ScanFile(path)
	if second == nil {
		t.Fatal("successful retry returned nil")
	}
	if queue := s.GetReviewQueue(); len(queue) != 1 {
		t.Fatalf("durably saved review was not published: %#v", queue)
	}
	s.mu.RLock()
	dirtyAfterSuccess := s.results[path].dirty
	s.mu.RUnlock()
	if dirtyAfterSuccess {
		t.Fatal("successful repository retry remained dirty")
	}
}
