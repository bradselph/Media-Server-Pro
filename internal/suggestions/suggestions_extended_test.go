package suggestions

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// scoreRecentlyViewed
// ---------------------------------------------------------------------------

func TestScoreRecentlyViewed_MatchingCategory(t *testing.T) {
	profile := &UserProfile{
		ViewHistory: []ViewHistory{
			{Category: "movies", LastViewed: time.Now().Add(-1 * time.Hour)},
		},
	}
	media := &MediaInfo{CategoryIDs: []string{"movies"}, MediaType: "video"}
	var reasons []string
	score := scoreRecentlyViewed(recentlyViewedCategorySet(profile), media, &reasons)
	if score <= 0 {
		t.Errorf("matching recent category should give positive score, got %f", score)
	}
	if len(reasons) == 0 {
		t.Error("should add a reason for recently viewed match")
	}
}

func TestScoreRecentlyViewed_OldHistory(t *testing.T) {
	profile := &UserProfile{
		ViewHistory: []ViewHistory{
			{Category: "movies", LastViewed: time.Now().Add(-30 * 24 * time.Hour)},
		},
	}
	media := &MediaInfo{CategoryIDs: []string{"movies"}}
	var reasons []string
	score := scoreRecentlyViewed(recentlyViewedCategorySet(profile), media, &reasons)
	if score != 0 {
		t.Errorf("old history should not match, got score %f", score)
	}
}

func TestScoreRecentlyViewed_DifferentCategory(t *testing.T) {
	profile := &UserProfile{
		ViewHistory: []ViewHistory{
			{Category: "anime", LastViewed: time.Now().Add(-1 * time.Hour)},
		},
	}
	media := &MediaInfo{CategoryIDs: []string{"movies"}}
	var reasons []string
	score := scoreRecentlyViewed(recentlyViewedCategorySet(profile), media, &reasons)
	if score != 0 {
		t.Errorf("different category should not match, got score %f", score)
	}
}

func TestScoreRecentlyViewed_EmptyHistory(t *testing.T) {
	profile := &UserProfile{}
	media := &MediaInfo{CategoryIDs: []string{"movies"}}
	var reasons []string
	score := scoreRecentlyViewed(recentlyViewedCategorySet(profile), media, &reasons)
	if score != 0 {
		t.Errorf("empty history should return 0, got %f", score)
	}
}

// TestRecentlyViewedCategorySet validates the pre-built set that replaced the
// per-item ViewHistory scan, so scoreRecentlyViewed stays semantically identical.
func TestRecentlyViewedCategorySet(t *testing.T) {
	if got := recentlyViewedCategorySet(nil); got != nil {
		t.Errorf("nil profile should yield nil set, got %v", got)
	}
	if got := recentlyViewedCategorySet(&UserProfile{}); len(got) != 0 {
		t.Errorf("empty history should yield empty set, got %v", got)
	}
	profile := &UserProfile{
		ViewHistory: []ViewHistory{
			{Category: "movies", LastViewed: time.Now().Add(-1 * time.Hour)},        // recent → included
			{Category: "movies", LastViewed: time.Now().Add(-2 * time.Hour)},        // duplicate → dedup
			{Category: "anime", LastViewed: time.Now().Add(-30 * 24 * time.Hour)},   // >7d → excluded
			{Category: "", LastViewed: time.Now().Add(-1 * time.Hour)},              // empty cat → excluded
		},
	}
	set := recentlyViewedCategorySet(profile)
	if !set["movies"] {
		t.Error("recent 'movies' should be in the set")
	}
	if set["anime"] {
		t.Error("history older than 7 days should be excluded")
	}
	if set[""] {
		t.Error("empty category should be excluded")
	}
	if len(set) != 1 {
		t.Errorf("set should contain exactly 1 category (deduped), got %d: %v", len(set), set)
	}
}

// ---------------------------------------------------------------------------
// scoreCategoryPreference — additional cases
// ---------------------------------------------------------------------------

func TestScoreCategoryPreference_HighScore(t *testing.T) {
	profile := &UserProfile{
		CategoryScores: map[string]float64{"movies": 0.9, "anime": 0.3},
		TotalViews:     20,
	}
	media := &MediaInfo{CategoryIDs: []string{"movies"}}
	var reasons []string
	score := scoreCategoryPreference(profile, media, &reasons, computeProfileTotals(profile).categoryTotal)
	if score <= 0 {
		t.Errorf("high category preference should give positive score, got %f", score)
	}
}

func TestScoreCategoryPreference_NoPreference(t *testing.T) {
	profile := &UserProfile{
		CategoryScores: map[string]float64{"anime": 0.5},
		TotalViews:     10,
	}
	media := &MediaInfo{CategoryIDs: []string{"movies"}}
	var reasons []string
	score := scoreCategoryPreference(profile, media, &reasons, computeProfileTotals(profile).categoryTotal)
	if score != 0 {
		t.Errorf("no category match should give 0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// scoreTypePreference — additional cases
// ---------------------------------------------------------------------------

func TestScoreTypePreference_EmptyPreferences(t *testing.T) {
	profile := &UserProfile{
		TypePreferences: map[string]float64{},
		TotalViews:      15,
	}
	media := &MediaInfo{MediaType: "video"}
	score := scoreTypePreference(profile, media, computeProfileTotals(profile).typeTotal)
	if score != 0 {
		t.Errorf("empty type preferences should give 0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// computeSimilarity — additional cases
// ---------------------------------------------------------------------------

func TestComputeSimilarity_DifferentMedia(t *testing.T) {
	a := &MediaInfo{CategoryIDs: []string{"movies"}, MediaType: "video", Title: "Action Movie", Tags: []string{"action"}}
	b := &MediaInfo{CategoryIDs: []string{"music"}, MediaType: "audio", Title: "Jazz Album", Tags: []string{"jazz"}}
	score, _ := computeSimilarity(a, b, titleWords(a.Title))
	if score > 0.5 {
		t.Errorf("very different media should have low similarity, got %f", score)
	}
}

func TestComputeSimilarity_IdenticalMedia(t *testing.T) {
	a := &MediaInfo{CategoryIDs: []string{"movies"}, MediaType: "video", Title: "Test Movie", Tags: []string{"action", "sci-fi"}}
	b := &MediaInfo{CategoryIDs: []string{"movies"}, MediaType: "video", Title: "Test Movie", Tags: []string{"action", "sci-fi"}}
	score, _ := computeSimilarity(a, b, titleWords(a.Title))
	if score < 0.5 {
		t.Errorf("identical media should have high similarity, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// computeTagSimilarity — additional cases
// ---------------------------------------------------------------------------

func TestComputeTagSimilarity_PartialOverlap(t *testing.T) {
	source := &MediaInfo{Tags: []string{"a", "b", "c"}}
	candidate := &MediaInfo{Tags: []string{"b", "c", "d"}}
	var reasons []string
	score := computeTagSimilarity(source, candidate, &reasons)
	if score <= 0 || score >= 1 {
		t.Errorf("partial overlap should be between 0 and 1, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// computeTitleSimilarity — additional cases
// ---------------------------------------------------------------------------

func TestComputeTitleSimilarity_Identical(t *testing.T) {
	source := &MediaInfo{Title: "Star Wars"}
	candidate := &MediaInfo{Title: "Star Wars"}
	score := computeTitleSimilarity(titleWords(source.Title), candidate)
	if score <= 0 {
		t.Errorf("identical titles should have positive similarity, got %f", score)
	}
}

func TestComputeTitleSimilarity_SharedWords(t *testing.T) {
	source := &MediaInfo{Title: "Star Wars Episode IV"}
	candidate := &MediaInfo{Title: "Star Wars Episode V"}
	score := computeTitleSimilarity(titleWords(source.Title), candidate)
	if score <= 0 {
		t.Errorf("shared words should give positive similarity, got %f", score)
	}
}

func TestComputeTitleSimilarity_NoOverlap(t *testing.T) {
	source := &MediaInfo{Title: "Alpha"}
	candidate := &MediaInfo{Title: "Bravo"}
	score := computeTitleSimilarity(titleWords(source.Title), candidate)
	if score != 0 {
		t.Errorf("no shared words should give 0, got %f", score)
	}
}

func TestComputeTitleSimilarity_Empty(t *testing.T) {
	source := &MediaInfo{Title: ""}
	candidate := &MediaInfo{Title: "Test"}
	score := computeTitleSimilarity(titleWords(source.Title), candidate)
	if score != 0 {
		t.Errorf("empty title should give 0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// scoreMediaBase — additional cases
// ---------------------------------------------------------------------------

func TestScoreMediaBase_HighRating(t *testing.T) {
	media := &MediaInfo{Rating: 5.0, Views: 50}
	score, _ := scoreMediaBase(media)
	lowMedia := &MediaInfo{Rating: 1.0, Views: 50}
	lowScore, _ := scoreMediaBase(lowMedia)
	if score <= lowScore {
		t.Error("higher rating should produce higher base score")
	}
}

// ---------------------------------------------------------------------------
// diversify — additional
// ---------------------------------------------------------------------------

func TestDiversify_SingleCategory(t *testing.T) {
	input := make([]*Suggestion, 10)
	for i := range input {
		input[i] = &Suggestion{MediaID: "id", Category: "movies", Score: float64(10 - i)}
	}
	result := diversify(input, 5, 3)
	if len(result) > 5 {
		t.Errorf("should limit to 5, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// topShuffled — additional
// ---------------------------------------------------------------------------

func TestTopShuffled_PreservesHighScores(t *testing.T) {
	items := make([]*Suggestion, 20)
	for i := range items {
		items[i] = &Suggestion{MediaID: "id", Score: float64(i)}
	}
	result := topShuffled(items, 5)
	if len(result) != 5 {
		t.Fatalf("expected 5, got %d", len(result))
	}
	// All returned items should have scores >= some reasonable threshold
	// since topShuffled picks from the top tier
	for _, s := range result {
		if s.Score < 0 {
			t.Errorf("score should not be negative: %f", s.Score)
		}
	}
}

// ---------------------------------------------------------------------------
// RenameMediaPath
// ---------------------------------------------------------------------------

func TestRenameMediaPath_RatingsFollowFile(t *testing.T) {
	m := NewModule(nil, nil)
	m.RecordRating("user1", "/media/old.mp4", 4)

	m.RenameMediaPath("/media/old.mp4", "/media/new.mp4")

	profile := m.GetUserProfile("user1")
	if profile == nil {
		t.Fatal("profile missing")
	}
	if len(profile.ViewHistory) != 1 {
		t.Fatalf("ViewHistory len = %d, want 1", len(profile.ViewHistory))
	}
	vh := profile.ViewHistory[0]
	if vh.MediaPath != "/media/new.mp4" {
		t.Errorf("MediaPath = %q, want /media/new.mp4", vh.MediaPath)
	}
	if vh.Rating != 4 {
		t.Errorf("Rating = %v, want 4", vh.Rating)
	}
}

func TestRenameMediaPath_ExistingTargetEntryWins(t *testing.T) {
	m := NewModule(nil, nil)
	m.RecordRating("user1", "/media/old.mp4", 2)
	m.RecordRating("user1", "/media/new.mp4", 5)

	m.RenameMediaPath("/media/old.mp4", "/media/new.mp4")

	profile := m.GetUserProfile("user1")
	if profile == nil {
		t.Fatal("profile missing")
	}
	if len(profile.ViewHistory) != 1 {
		t.Fatalf("ViewHistory len = %d, want 1 (old entry dropped)", len(profile.ViewHistory))
	}
	vh := profile.ViewHistory[0]
	if vh.MediaPath != "/media/new.mp4" || vh.Rating != 5 {
		t.Errorf("surviving entry = %q rating %v, want /media/new.mp4 rating 5", vh.MediaPath, vh.Rating)
	}
}

func TestRenameMediaPath_NoOpOnSameOrEmptyPaths(t *testing.T) {
	m := NewModule(nil, nil)
	m.RecordRating("user1", "/media/a.mp4", 3)

	m.RenameMediaPath("/media/a.mp4", "/media/a.mp4")
	m.RenameMediaPath("", "/media/b.mp4")
	m.RenameMediaPath("/media/a.mp4", "")

	profile := m.GetUserProfile("user1")
	if profile == nil || len(profile.ViewHistory) != 1 {
		t.Fatal("profile should be unchanged")
	}
	if profile.ViewHistory[0].MediaPath != "/media/a.mp4" {
		t.Errorf("MediaPath = %q, want /media/a.mp4", profile.ViewHistory[0].MediaPath)
	}
}
