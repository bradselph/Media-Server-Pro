package handlers

import (
	"media-server-pro/internal/media"
	"media-server-pro/internal/receiver"
	"media-server-pro/pkg/models"
)

// Federated-media helpers. "Federated" (a.k.a. slave / receiver) media is media
// imported from a paired peer. The goal is that a federated item behaves no
// differently than a local one across the API. These helpers centralize the
// local->receiver fallback so every consumer treats federated media uniformly.

// receiverSyntheticPath is the internal Path used for a federated item. It is
// never a real filesystem path (models.MediaItem.Path is json:"-", so it is not
// exposed) — it exists so per-user state (ratings, playback positions, favorites)
// keys consistently, matching resolveMediaPathOrReceiver which uses "receiver:"+id.
func receiverSyntheticPath(id string) string { return "receiver:" + id }

// receiverItemToModel converts a receiver (slave) MediaItem into the unified
// models.MediaItem shape, combining the slave's own mature flag with the master's
// fingerprint-based detection. Centralizes the conversion the /api/media listing,
// batch lookup, and single-item resolvers all use.
func (h *Handler) receiverItemToModel(ri *receiver.MediaItem) *models.MediaItem {
	return &models.MediaItem{
		ID:           ri.ID,
		Name:         ri.Name,
		Path:         receiverSyntheticPath(ri.ID),
		Type:         models.MediaType(ri.MediaType),
		Size:         ri.Size,
		Duration:     ri.Duration,
		Width:        ri.Width,
		Height:       ri.Height,
		Category:     ri.Category,
		Tags:         ri.Tags,
		BlurHash:     ri.BlurHash,
		DateAdded:    ri.DateAdded,
		DateModified: ri.DateModified,
		IsMature:     ri.IsMature || h.isReceiverItemMature(ri.ContentFingerprint),
	}
}

// appendReceiverItems merges federated (slave) media into items, applying the
// same fingerprint/id de-duplication and filter the /api/media listing uses:
// skip IDs already present, skip receiver items whose fingerprint matches a local
// item (show the local copy) or another already-added slave item. Returns the
// extended slice and whether any item was added; seenIDs is updated in place.
func (h *Handler) appendReceiverItems(items []*models.MediaItem, seenIDs map[string]bool, filter media.Filter) ([]*models.MediaItem, bool) {
	if h.receiver == nil {
		return items, false
	}
	added := false
	seenFP := make(map[string]bool)
	for _, ri := range h.receiver.GetAllMedia() {
		if seenIDs[ri.ID] {
			continue
		}
		if ri.ContentFingerprint != "" {
			if h.media.HasFingerprint(ri.ContentFingerprint) {
				continue
			}
			if seenFP[ri.ContentFingerprint] {
				continue
			}
			seenFP[ri.ContentFingerprint] = true
		}
		item := h.receiverItemToModel(ri)
		if !filter.Matches(item) {
			continue
		}
		items = append(items, item)
		seenIDs[ri.ID] = true
		added = true
	}
	return items, added
}

// appendExtractorItems merges active extractor items into items (id de-dup +
// filter), mirroring the /api/media listing. Returns the extended slice and
// whether any item was added; seenIDs is updated in place.
func (h *Handler) appendExtractorItems(items []*models.MediaItem, seenIDs map[string]bool, filter media.Filter) ([]*models.MediaItem, bool) {
	if h.extractor == nil || !h.config.Get().Extractor.Enabled {
		return items, false
	}
	added := false
	for _, ei := range h.extractor.GetAllItems() {
		if ei.Status != "active" || seenIDs[ei.ID] {
			continue
		}
		item := &models.MediaItem{ID: ei.ID, Name: ei.Title, Type: models.MediaTypeVideo}
		if !filter.Matches(item) {
			continue
		}
		items = append(items, item)
		seenIDs[ei.ID] = true
		added = true
	}
	return items, added
}

// mergedMediaList returns the full unified media list (local + receiver +
// extractor) with the same de-dup/filter/sort the GET /api/media listing applies.
// Handlers that build a candidate set (smart-playlist matching, recent/new-since
// rows) use this so federated media is included, not just local.
func (h *Handler) mergedMediaList(filter media.Filter) []*models.MediaItem {
	items := h.media.ListMedia(filter)
	seenIDs := make(map[string]bool, len(items))
	for _, it := range items {
		seenIDs[it.ID] = true
	}
	items, addedR := h.appendReceiverItems(items, seenIDs, filter)
	items, addedE := h.appendExtractorItems(items, seenIDs, filter)
	if addedR || addedE {
		filter.SortItems(items)
	}
	return items
}

// isFederatedMedia reports whether id refers to a federated (slave) item rather
// than a local one. Used to gracefully degrade features that need a local source
// file (HLS transcode, hover-frame previews) or that don't apply to federated
// media (master-side metadata editing).
func (h *Handler) isFederatedMedia(id string) bool {
	if id == "" || h.receiver == nil {
		return false
	}
	if _, err := h.media.GetMediaByID(id); err == nil {
		return false // local media takes precedence
	}
	return h.receiver.GetMediaItem(id) != nil
}

// resolveMediaItemOrReceiver resolves a single media ID to a unified
// models.MediaItem — local first, then federated (receiver) — so single-item
// handlers (batch lookup, chapters, reports, categories, ...) treat federated
// media like local. Returns (nil, false) when the ID is unknown.
func (h *Handler) resolveMediaItemOrReceiver(id string) (*models.MediaItem, bool) {
	if item, err := h.media.GetMediaByID(id); err == nil && item != nil {
		return item, true
	}
	if h.receiver != nil {
		if ri := h.receiver.GetMediaItem(id); ri != nil {
			return h.receiverItemToModel(ri), true
		}
	}
	return nil, false
}
