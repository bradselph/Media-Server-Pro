package suggestions

import (
	"testing"
)

const testMediaPath = "/a.mp4"

// ---------------------------------------------------------------------------
// scoreMediaBase
// ---------------------------------------------------------------------------

func TestScoreMediaBase_NewMedia(t *testing.T) {
	media := &MediaInfo{Path: testMediaPath, Views: 0, Rating: 0}
	score, reasons := scoreMediaBase(media)
	if score <= 0 {
		t.Error("even new media should have some base score")
	}
	_ = reasons // reasons may or may not be populated for base score
}

func TestScoreMediaBase_PopularMedia(t *testing.T) {
	media := &MediaInfo{Path: testMediaPath, Views: 100, Rating: 4.5}
	score, _ := scoreMediaBase(media)
	if score <= 0 {
		t.Error("popular media should have positive score")
	}
}

// ---------------------------------------------------------------------------
// scoreMediaForProfile
// ---------------------------------------------------------------------------

func TestScoreMediaForProfile_EmptyProfile(t *testing.T) {
	profile := &UserProfile{}
	media := &MediaInfo{Path: testMediaPath, Category: "movies", MediaType: "video"}
	score, _ := scoreMediaForProfile(profile, media)
	if score < 0 {
		t.Error("score should not be negative")
	}
}

func TestScoreMediaForProfile_MatchingCategory(t *testing.T) {
	profile := &UserProfile{
		CategoryScores:  map[string]float64{"movies": 0.8},
		TypePreferences: map[string]float64{"video": 0.9},
		TotalViews:      12,
	}
	media := &MediaInfo{Path: testMediaPath, Category: "movies", MediaType: "video"}
	score, _ := scoreMediaForProfile(profile, media)
	if score <= 0 {
		t.Error("matching category should give positive score")
	}
}

// ---------------------------------------------------------------------------
// scoreCategoryPreference
// ---------------------------------------------------------------------------

func TestScoreCategoryPreference_Match(t *testing.T) {
	profile := &UserProfile{
		CategoryScores: map[string]float64{"movies": 0.8},
		TotalViews:     10,
	}
	media := &MediaInfo{Category: "movies"}
	var reasons []string
	score := scoreCategoryPreference(profile, media, &reasons)
	if score <= 0 {
		t.Error("matching category should give positive score")
	}
}

func TestScoreCategoryPreference_NoMatch(t *testing.T) {
	profile := &UserProfile{
		CategoryScores: map[string]float64{"movies": 0.8},
		TotalViews:     10,
	}
	media := &MediaInfo{Category: "music"}
	var reasons []string
	score := scoreCategoryPreference(profile, media, &reasons)
	if score != 0 {
		t.Errorf("non-matching category should give 0, got %f", score)
	}
}

func TestScoreCategoryPreference_ZeroTotal(t *testing.T) {
	profile := &UserProfile{
		CategoryScores: map[string]float64{},
		TotalViews:     0,
	}
	media := &MediaInfo{Category: "movies"}
	var reasons []string
	score := scoreCategoryPreference(profile, media, &reasons)
	if score != 0 {
		t.Errorf("zero total views should give 0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// scoreTypePreference
// ---------------------------------------------------------------------------

func TestScoreTypePreference_Match(t *testing.T) {
	profile := &UserProfile{
		TypePreferences: map[string]float64{"video": 0.8},
		TotalViews:      10,
	}
	media := &MediaInfo{MediaType: "video"}
	score := scoreTypePreference(profile, media)
	if score <= 0 {
		t.Error("matching type should give positive score")
	}
}

func TestScoreTypePreference_NoMatch(t *testing.T) {
	profile := &UserProfile{
		TypePreferences: map[string]float64{"video": 0.9},
		TotalViews:      10,
	}
	media := &MediaInfo{MediaType: "audio"}
	score := scoreTypePreference(profile, media)
	if score != 0 {
		t.Errorf("non-matching type should give 0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// computeSimilarity
// ---------------------------------------------------------------------------

func TestComputeSimilarity_SameCategory(t *testing.T) {
	source := &MediaInfo{Path: "/s.mp4", Category: "movies", MediaType: "video", Tags: []string{"action"}}
	candidate := &MediaInfo{Path: "/c.mp4", Category: "movies", MediaType: "video", Tags: []string{"action"}}
	score, _ := computeSimilarity(source, candidate)
	if score <= 0 {
		t.Error("same category+type should give positive similarity")
	}
}

// ---------------------------------------------------------------------------
// computeTagSimilarity
// ---------------------------------------------------------------------------

func TestComputeTagSimilarity_FullOverlap(t *testing.T) {
	source := &MediaInfo{Tags: []string{"action", "thriller"}}
	candidate := &MediaInfo{Tags: []string{"action", "thriller"}}
	var reasons []string
	score := computeTagSimilarity(source, candidate, &reasons)
	if score <= 0 {
		t.Error("full tag overlap should give positive score")
	}
}

func TestComputeTagSimilarity_NoOverlap(t *testing.T) {
	source := &MediaInfo{Tags: []string{"action", "thriller"}}
	candidate := &MediaInfo{Tags: []string{"comedy", "romance"}}
	var reasons []string
	score := computeTagSimilarity(source, candidate, &reasons)
	if score != 0 {
		t.Errorf("no tag overlap should give 0, got %f", score)
	}
}

func TestComputeTagSimilarity_NilTags(t *testing.T) {
	source := &MediaInfo{Tags: nil}
	candidate := &MediaInfo{Tags: nil}
	var reasons []string
	score := computeTagSimilarity(source, candidate, &reasons)
	if score != 0 {
		t.Errorf("nil tags should give 0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// computeTitleSimilarity
// ---------------------------------------------------------------------------

func TestComputeTitleSimilarity_Overlap(t *testing.T) {
	source := &MediaInfo{Title: "The Dark Knight Returns"}
	candidate := &MediaInfo{Title: "Dark Knight Rises"}
	score := computeTitleSimilarity(source, candidate)
	if score <= 0 {
		t.Error("overlapping title words should give positive score")
	}
}

// ---------------------------------------------------------------------------
// topShuffled
// ---------------------------------------------------------------------------

func TestTopShuffled_LessThanN(t *testing.T) {
	input := []*Suggestion{{MediaID: "1"}, {MediaID: "2"}}
	got := topShuffled(input, 5)
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}
}

func TestTopShuffled_MoreThanN(t *testing.T) {
	input := make([]*Suggestion, 10)
	for i := range input {
		input[i] = &Suggestion{MediaID: string(rune('0' + i))}
	}
	got := topShuffled(input, 5)
	if len(got) != 5 {
		t.Errorf("expected 5, got %d", len(got))
	}
}

func TestTopShuffled_Empty(t *testing.T) {
	got := topShuffled(nil, 5)
	if len(got) != 0 {
		t.Errorf("expected 0 for nil, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// diversify
// ---------------------------------------------------------------------------

func TestDiversify_LimitReduced(t *testing.T) {
	input := make([]*Suggestion, 10)
	for i := range input {
		input[i] = &Suggestion{MediaID: string(rune('0' + i)), Category: "movies"}
	}
	got := diversify(input, 5, 3)
	if len(got) > 5 {
		t.Errorf("expected at most 5, got %d", len(got))
	}
}

func TestDiversify_Empty(t *testing.T) {
	got := diversify(nil, 5, 3)
	if len(got) != 0 {
		t.Errorf("expected 0 for nil, got %d", len(got))
	}
}

func TestDiversify_MixedCategories(t *testing.T) {
	input := []*Suggestion{
		{MediaID: "1", Category: "movies"},
		{MediaID: "2", Category: "movies"},
		{MediaID: "3", Category: "music"},
		{MediaID: "4", Category: "music"},
		{MediaID: "5", Category: "tv"},
	}
	got := diversify(input, 10, 2)
	if len(got) > 6 {
		t.Errorf("unexpected count: %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Module basics
// ---------------------------------------------------------------------------

func TestModuleName(t *testing.T) {
	m := &Module{}
	if m.Name() != "suggestions" {
		t.Errorf("Name() = %q, want %q", m.Name(), "suggestions")
	}
}
