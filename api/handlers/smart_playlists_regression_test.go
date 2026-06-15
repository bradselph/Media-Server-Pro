package handlers

import (
	"testing"

	"media-server-pro/pkg/models"
)

// TestSmartCondition_EqOpSupportedForNumericFields locks in the fix for the bug
// where the smart-playlist UI exposed `eq` as an operator for every field but
// the backend silently returned false for `duration` and `views`, leaving the
// user with an empty playlist and no diagnostic.
//
// The frontend (web/nuxt-ui/pages/playlists.vue) renders one operator picker
// with values ['eq', 'gte', 'lte', 'includes'] applied to every field, so any
// numeric field MUST accept `eq` or the UX silently breaks.
func TestSmartCondition_EqOpSupportedForNumericFields(t *testing.T) {
	cases := []struct {
		name string
		item *models.MediaItem
		cond SmartCondition
		want bool
	}{
		// Duration uses ±0.5s tolerance so the UI can offer integer-second
		// equality without forcing users to think in floats.
		{"duration eq exact match", &models.MediaItem{Duration: 60.0}, SmartCondition{Field: "duration", Op: "eq", Value: "60"}, true},
		{"duration eq within tolerance high", &models.MediaItem{Duration: 60.4}, SmartCondition{Field: "duration", Op: "eq", Value: "60"}, true},
		{"duration eq within tolerance low", &models.MediaItem{Duration: 59.6}, SmartCondition{Field: "duration", Op: "eq", Value: "60"}, true},
		{"duration eq just outside tolerance", &models.MediaItem{Duration: 61.0}, SmartCondition{Field: "duration", Op: "eq", Value: "60"}, false},
		{"duration eq invalid value", &models.MediaItem{Duration: 60.0}, SmartCondition{Field: "duration", Op: "eq", Value: "notanumber"}, false},

		// Views is an integer field — exact equality.
		{"views eq match", &models.MediaItem{Views: 100}, SmartCondition{Field: "views", Op: "eq", Value: "100"}, true},
		{"views eq mismatch", &models.MediaItem{Views: 99}, SmartCondition{Field: "views", Op: "eq", Value: "100"}, false},
		{"views eq invalid value", &models.MediaItem{Views: 100}, SmartCondition{Field: "views", Op: "eq", Value: "notanumber"}, false},

		// Sanity: gte/lte still work after the change.
		{"duration gte still works", &models.MediaItem{Duration: 120.0}, SmartCondition{Field: "duration", Op: "gte", Value: "60"}, true},
		{"views lte still works", &models.MediaItem{Views: 50}, SmartCondition{Field: "views", Op: "lte", Value: "100"}, true},

		// Category is curated-membership based: cond.Value is a MediaCategory.id and
		// the item matches only when it is a member of that category.
		{"category eq member", &models.MediaItem{ID: "m1"}, SmartCondition{Field: "category", Op: "eq", Value: "cat1"}, true},
		{"category eq non-member", &models.MediaItem{ID: "m2"}, SmartCondition{Field: "category", Op: "eq", Value: "cat1"}, false},
		{"category eq unknown category", &models.MediaItem{ID: "m1"}, SmartCondition{Field: "category", Op: "eq", Value: "nope"}, false},
	}

	// Membership for the curated-category cases above.
	catMembers := map[string]map[string]bool{"cat1": {"m1": true}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchSmartCondition(tc.item, tc.cond, catMembers)
			if got != tc.want {
				t.Errorf("matchSmartCondition(%+v) = %v, want %v", tc.cond, got, tc.want)
			}
		})
	}
}
