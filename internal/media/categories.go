package media

import (
	"context"
	"slices"

	"media-server-pro/pkg/models"
)

// MediaIDsWithTag returns the set of in-memory media IDs carrying the given tag
// (exact match, mirroring Filter.matchesTags). Used to expand tag-backed
// ("smart") category membership. Returns an empty set for an empty tag.
func (m *Module) MediaIDsWithTag(tag string) map[string]bool {
	set := make(map[string]bool)
	if tag == "" {
		return set
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, item := range m.media {
		if item != nil && slices.Contains(item.Tags, tag) {
			set[id] = true
		}
	}
	return set
}

// GetCategoryMemberIDs returns the set of media IDs that belong to the given
// curated category (the media_category_items membership table). The set is used
// by handlers to populate Filter.CategoryIDSet so /api/media can restrict a
// listing to a single curated category.
//
// On success it always returns a non-nil (possibly empty) map so callers can
// fail closed: an empty set means "category has no members", which Filter.Matches
// treats as "no items pass" rather than "no filter". It returns (nil, nil) only
// when there is nothing to filter by: no DB module wired, the GORM handle is nil,
// or categoryID is empty.
func (m *Module) GetCategoryMemberIDs(ctx context.Context, categoryID string) (map[string]bool, error) {
	if m.dbModule == nil || categoryID == "" {
		return nil, nil
	}
	gdb := m.dbModule.GORM()
	if gdb == nil {
		return nil, nil
	}
	var ids []string
	if err := gdb.WithContext(ctx).
		Model(&models.MediaCategoryItem{}).
		Where("category_id = ?", categoryID).
		Pluck("media_id", &ids).Error; err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	// Tag-backed ("smart") categories: union in every media item carrying the
	// category's tag so membership tracks tags live. A single indexed PK lookup;
	// the tag scan is in-memory.
	var cat models.MediaCategory
	if err := gdb.WithContext(ctx).Select("tag").First(&cat, "id = ?", categoryID).Error; err == nil && cat.Tag != "" {
		for id := range m.MediaIDsWithTag(cat.Tag) {
			set[id] = true
		}
	}
	return set, nil
}

// GetCategoryIDsForItems returns, for each requested media ID, the list of
// curated category IDs that item belongs to. Used to seed the suggestions
// catalogue with curated-category membership so personalization scores against
// real categories instead of the retired path-detected buckets.
//
// The IN clause is chunked so a very large library does not exceed driver
// placeholder limits. The returned map omits items that belong to no category.
func (m *Module) GetCategoryIDsForItems(ctx context.Context, mediaIDs []string) (map[string][]string, error) {
	out := make(map[string][]string)
	if m.dbModule == nil || len(mediaIDs) == 0 {
		return out, nil
	}
	gdb := m.dbModule.GORM()
	if gdb == nil {
		return out, nil
	}
	const chunk = 1000
	for start := 0; start < len(mediaIDs); start += chunk {
		end := min(start+chunk, len(mediaIDs))
		var rows []models.MediaCategoryItem
		if err := gdb.WithContext(ctx).
			Where("media_id IN ?", mediaIDs[start:end]).
			Find(&rows).Error; err != nil {
			return out, err
		}
		for _, r := range rows {
			out[r.MediaID] = append(out[r.MediaID], r.CategoryID)
		}
	}
	return out, nil
}

// GetCategoryIDsForItem returns the curated category IDs a single media item
// belongs to. Convenience wrapper over GetCategoryIDsForItems for per-view
// recording paths; returns nil on error or when the item is uncategorised.
func (m *Module) GetCategoryIDsForItem(ctx context.Context, mediaID string) []string {
	if mediaID == "" {
		return nil
	}
	res, err := m.GetCategoryIDsForItems(ctx, []string{mediaID})
	if err != nil {
		return nil
	}
	return res[mediaID]
}
