package scanner

import (
	"testing"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// TestMatureScanner_RenamePath_ReKeysReviewQueue guards the fix for pending
// mature reviews being orphaned when a file is renamed/moved. The review queue
// and scan-result maps are path-keyed and ReviewItem looks up by the current
// path, so without a re-key an admin's approve/reject after a rename fails with
// "item not found in review queue". scanRepo is nil here so only the in-memory
// re-key runs (the DB path is a no-op).
func TestMatureScanner_RenamePath_ReKeysReviewQueue(t *testing.T) {
	s := &MatureScanner{
		log:         logger.New("test"),
		results:     map[string]*ScanResult{},
		reviewQueue: map[string]*models.MatureReviewItem{},
	}
	const oldPath = "/videos/raw.mp4"
	const newPath = "/videos/Clean Title (2020).mp4"
	s.results[oldPath] = &ScanResult{Path: oldPath, NeedsReview: true}
	s.reviewQueue[oldPath] = &models.MatureReviewItem{
		ID: stableReviewID(oldPath), Name: "raw.mp4", MediaPath: oldPath,
	}

	s.RenamePath(oldPath, newPath)

	if _, ok := s.reviewQueue[oldPath]; ok {
		t.Error("old path must be removed from the review queue")
	}
	item, ok := s.reviewQueue[newPath]
	if !ok {
		t.Fatal("review item must be re-keyed to the new path (approve/reject looks it up by current path)")
	}
	if item.MediaPath != newPath {
		t.Errorf("item.MediaPath = %q, want %q", item.MediaPath, newPath)
	}
	if item.ID != stableReviewID(newPath) {
		t.Error("review item ID should reflect the new path")
	}
	if res, ok := s.results[newPath]; !ok || res.Path != newPath {
		t.Error("scan result must be re-keyed to the new path")
	}
	if _, ok := s.results[oldPath]; ok {
		t.Error("old path must be removed from results")
	}
}
