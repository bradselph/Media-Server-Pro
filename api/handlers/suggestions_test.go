package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"media-server-pro/internal/suggestions"
)

// newSuggestionsTestHandler builds a Handler with an in-memory suggestions
// module. No database is needed: NewModule defers repository wiring to
// Start(), and persistRating no-ops on a nil repo.
func newSuggestionsTestHandler() *Handler {
	return &Handler{suggestions: suggestions.NewModule(nil, nil)}
}

func TestEnrichSuggestionUserRatings(t *testing.T) {
	h := newSuggestionsTestHandler()
	h.suggestions.RecordRating("user1", "/media/a.mp4", 4)

	items := []*suggestions.Suggestion{
		{MediaID: "a", MediaPath: "/media/a.mp4"},
		{MediaID: "b", MediaPath: "/media/b.mp4"},
	}
	h.enrichSuggestionUserRatings(items, "user1")

	if items[0].UserRating == nil || *items[0].UserRating != 4 {
		t.Errorf("rated item: UserRating = %v, want 4", items[0].UserRating)
	}
	if items[1].UserRating != nil {
		t.Errorf("unrated item: UserRating = %v, want nil", items[1].UserRating)
	}
}

func TestEnrichSuggestionUserRatingsAnonymous(t *testing.T) {
	h := newSuggestionsTestHandler()
	h.suggestions.RecordRating("user1", "/media/a.mp4", 5)

	items := []*suggestions.Suggestion{{MediaID: "a", MediaPath: "/media/a.mp4"}}
	h.enrichSuggestionUserRatings(items, "")

	if items[0].UserRating != nil {
		t.Errorf("anonymous request: UserRating = %v, want nil", items[0].UserRating)
	}
}

func TestEnrichSuggestionUserRatingsNoCrossUserLeak(t *testing.T) {
	h := newSuggestionsTestHandler()
	h.suggestions.RecordRating("user1", "/media/a.mp4", 5)

	items := []*suggestions.Suggestion{{MediaID: "a", MediaPath: "/media/a.mp4"}}
	h.enrichSuggestionUserRatings(items, "user2")

	if items[0].UserRating != nil {
		t.Errorf("other user's request: UserRating = %v, want nil", items[0].UserRating)
	}
}

func TestSuggestionUserRatingJSONOmittedWhenNil(t *testing.T) {
	b, err := json.Marshal(suggestions.Suggestion{MediaID: "a"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "user_rating") {
		t.Errorf("unrated suggestion should omit user_rating, got %s", b)
	}

	b, err = json.Marshal(suggestions.Suggestion{MediaID: "a", UserRating: new(4.0)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"user_rating":4`) {
		t.Errorf("rated suggestion should include user_rating, got %s", b)
	}
}
