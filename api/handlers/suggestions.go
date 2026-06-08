package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/internal/suggestions"
	"media-server-pro/internal/thumbnails"
)

// requireSuggestionsCatalogue checks that the suggestions module's media
// catalog has been seeded. Returns 503 with Retry-After if the catalog
// is empty (server just started, initial scan still in progress).
func (h *Handler) requireSuggestionsCatalogue(c *gin.Context) bool {
	if !h.suggestions.IsCatalogueReady() {
		c.Header(headerRetryAfter, "3")
		writeError(c, http.StatusServiceUnavailable, "Suggestions are loading — media catalog scan in progress, please try again shortly")
		return false
	}
	return true
}

// enrichSuggestionUserRatings stamps each suggestion with the requesting
// user's own rating, mirroring the user_ratings map ListMedia returns.
// No-op for anonymous requests or users without a suggestion profile.
func (h *Handler) enrichSuggestionUserRatings(items []*suggestions.Suggestion, userID string) {
	if userID == "" || h.suggestions == nil || len(items) == 0 {
		return
	}
	profile := h.suggestions.GetUserProfile(userID)
	if profile == nil {
		return
	}
	ratingsByPath := make(map[string]float64, len(profile.ViewHistory))
	for _, vh := range profile.ViewHistory {
		if vh.Rating > 0 && vh.MediaPath != "" {
			ratingsByPath[vh.MediaPath] = vh.Rating
		}
	}
	if len(ratingsByPath) == 0 {
		return
	}
	for _, s := range items {
		if r, ok := ratingsByPath[s.MediaPath]; ok {
			s.UserRating = new(r)
		}
	}
}

// userIDFromSession returns the authenticated user's ID, or "" for anonymous
// requests. For optional-auth endpoints that personalize when possible.
func userIDFromSession(c *gin.Context) string {
	if session := getSession(c); session != nil {
		return session.UserID
	}
	return ""
}

// respondSuggestions fetches personalized suggestions and writes the response.
func (h *Handler) respondSuggestions(c *gin.Context, userID string, defaultLimit, maxLimit int) {
	// Responses carry the requesting user's own ratings — keep shared caches out
	// (same policy as ListMedia).
	c.Header(headerCacheControl, "private, max-age=300")
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: defaultLimit, Min: 1, Max: maxLimit})
	canViewMature := h.canViewMatureContent(c)
	suggestions := h.suggestions.GetSuggestions(userID, limit, canViewMature)
	h.enrichSuggestionThumbnails(suggestions)
	h.enrichSuggestionUserRatings(suggestions, userID)
	writeSuccess(c, suggestions)
}

// GetSuggestions returns personalized content suggestions
func (h *Handler) GetSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	if !h.requireSuggestionsCatalogue(c) {
		return
	}
	session := getSession(c)
	var userID string
	if session != nil {
		userID = session.UserID
	}
	h.respondSuggestions(c, userID, 10, 100)
}

// GetTrendingSuggestions returns trending content
func (h *Handler) GetTrendingSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	if !h.requireSuggestionsCatalogue(c) {
		return
	}
	// Personalized via user_rating when a session is present — must not be
	// stored by shared caches.
	c.Header(headerCacheControl, "private, max-age=300")
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 10, Min: 1, Max: 100})
	canViewMature := h.canViewMatureContent(c)
	trending := h.suggestions.GetTrendingSuggestions(limit, canViewMature)
	h.enrichSuggestionThumbnails(trending)
	h.enrichSuggestionUserRatings(trending, userIDFromSession(c))
	writeSuccess(c, trending)
}

// GetSimilarMedia returns similar media to a given item
func (h *Handler) GetSimilarMedia(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	id := c.Query("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, errIDRequired)
		return
	}

	// Personalized via user_rating when a session is present — must not be
	// stored by shared caches.
	c.Header(headerCacheControl, "private, max-age=300")
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 10, Min: 1, Max: 100})

	// Pass the StableID directly to the suggestions module which has its own
	// catalog indexed by ID. No need to validate via the media module —
	// the suggestions engine handles unknown IDs gracefully (returns random sample).
	canViewMature := h.canViewMatureContent(c)
	similar := h.suggestions.GetSimilarMedia(id, limit, canViewMature)
	h.enrichSuggestionThumbnails(similar)
	h.enrichSuggestionUserRatings(similar, userIDFromSession(c))
	writeSuccess(c, similar)
}

// GetContinueWatching returns items the user started but didn't finish
func (h *Handler) GetContinueWatching(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}

	c.Header(headerCacheControl, "private, max-age=300")
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 10, Min: 1, Max: 50})
	canViewMature := h.canViewMatureContent(c)
	items := h.suggestions.GetContinueWatching(session.UserID, limit, canViewMature)
	h.enrichSuggestionThumbnails(items)
	h.enrichSuggestionUserRatings(items, session.UserID)
	writeSuccess(c, items)
}

// GetPersonalizedSuggestions returns personalized suggestions (auth-gated alias for GetSuggestions).
func (h *Handler) GetPersonalizedSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	if !h.requireSuggestionsCatalogue(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}
	h.respondSuggestions(c, session.UserID, 10, 100)
}

// RecordRating records a user rating for a media item
func (h *Handler) RecordRating(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}

	var req struct {
		ID     string  `json:"id"`
		Rating float64 `json:"rating"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	mediaPath, _, ok := h.resolveMediaPathOrReceiver(c, req.ID)
	if !ok {
		return
	}
	// Validate rating is in 0–5 range to avoid corrupting suggestion scoring
	if req.Rating < 0 || req.Rating > 5 {
		writeError(c, http.StatusBadRequest, "Rating must be between 0 and 5")
		return
	}

	h.suggestions.RecordRating(session.UserID, mediaPath, req.Rating)
	h.trackServerEvent(c, "rating_set", map[string]any{
		"media_id": req.ID,
		"rating":   req.Rating,
	})
	writeSuccess(c, nil)
}

// GetMyProfile returns the calling user's suggestion profile (watch stats, category scores).
func (h *Handler) GetMyProfile(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}
	profile := h.suggestions.GetUserProfile(session.UserID)
	if profile == nil {
		// Return an empty profile so the frontend always gets a valid object.
		writeSuccess(c, map[string]any{
			"user_id":          session.UserID,
			"total_views":      0,
			"total_watch_time": 0.0,
			"category_scores":  map[string]float64{},
			"type_preferences": map[string]float64{},
		})
		return
	}
	writeSuccess(c, profile)
}

// ResetMyProfile deletes the calling user's suggestion profile and view history,
// allowing them to start accumulating a fresh recommendation profile.
func (h *Handler) ResetMyProfile(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}
	if err := h.suggestions.ResetUserProfile(session.UserID); err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to reset suggestion profile")
		return
	}
	writeSuccess(c, nil)
}

// GetSuggestionStats returns suggestion module statistics
func (h *Handler) GetSuggestionStats(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	stats := h.suggestions.GetStats()
	writeSuccess(c, stats)
}

// GetMyRatings returns all media items the current user has rated (rating > 0).
func (h *Handler) GetMyRatings(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := RequireSession(c)
	if session == nil {
		return
	}

	profile := h.suggestions.GetUserProfile(session.UserID)
	if profile == nil {
		writeSuccess(c, []any{})
		return
	}

	type ratedItem struct {
		MediaID      string  `json:"media_id"`
		Name         string  `json:"name"`
		Category     string  `json:"category"`
		MediaType    string  `json:"media_type"`
		Rating       float64 `json:"rating"`
		ThumbnailURL string  `json:"thumbnail_url,omitempty"`
	}

	results := make([]ratedItem, 0)
	for _, vh := range profile.ViewHistory {
		if vh.Rating <= 0 {
			continue
		}
		ri := ratedItem{
			Category:  vh.Category,
			MediaType: vh.MediaType,
			Rating:    vh.Rating,
		}
		if h.media != nil && vh.MediaPath != "" {
			if item, err := h.media.GetMedia(vh.MediaPath); err == nil && item != nil {
				ri.MediaID = item.ID
				ri.Name = item.Name
				if h.thumbnails != nil {
					ri.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
				}
			}
		}
		if ri.MediaID == "" {
			continue // skip if media was deleted
		}
		results = append(results, ri)
	}

	writeSuccess(c, results)
}

// GetRecentContent returns media items added within the last N days (default 14).
// Intended for the "Recently Added" home-page row.
func (h *Handler) GetRecentContent(c *gin.Context) {
	days := 14
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	limit := 20
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	all := h.media.ListMedia(media.Filter{SortBy: "date_added", SortDesc: true})

	results := make([]*mediaRecentItem, 0, limit)
	for _, item := range all {
		if item.DateAdded.Before(cutoff) {
			break // items are sorted newest-first; once past cutoff we can stop
		}
		ri := &mediaRecentItem{
			ID:        item.ID,
			Name:      item.Name,
			Type:      string(item.Type),
			Category:  item.Category,
			Duration:  item.Duration,
			DateAdded: item.DateAdded,
		}
		if h.thumbnails != nil && item.ID != "" {
			ri.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
		}
		results = append(results, ri)
		if len(results) >= limit {
			break
		}
	}

	writeSuccess(c, results)
}

// mediaRecentItem is the response shape for GetRecentContent.
type mediaRecentItem struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Category     string    `json:"category"`
	Duration     float64   `json:"duration,omitempty"`
	DateAdded    time.Time `json:"date_added"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"`
}

// GetNewSinceLastVisit returns media added since the user's previous login.
// Requires auth. Falls back to a 7-day window if previous_last_login is not set.
func (h *Handler) GetNewSinceLastVisit(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}

	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 20, Min: 1, Max: 100})

	// Determine the cutoff: the user's previous last login, or 7 days ago as fallback.
	cutoff := time.Now().AddDate(0, 0, -7)
	user, err := h.auth.GetUserByID(c.Request.Context(), session.UserID)
	if err == nil && user != nil && user.PreviousLastLogin != nil {
		cutoff = *user.PreviousLastLogin
	}

	all := h.media.ListMedia(media.Filter{SortBy: "date_added", SortDesc: true})

	results := make([]*mediaRecentItem, 0, limit)
	for _, item := range all {
		if item.DateAdded.Before(cutoff) {
			break // sorted newest-first; stop once past cutoff
		}
		ri := &mediaRecentItem{
			ID:        item.ID,
			Name:      item.Name,
			Type:      string(item.Type),
			Category:  item.Category,
			Duration:  item.Duration,
			DateAdded: item.DateAdded,
		}
		if h.thumbnails != nil && item.ID != "" {
			ri.ThumbnailURL = h.thumbnails.GetThumbnailURL(thumbnails.MediaID(item.ID))
		}
		results = append(results, ri)
		if len(results) >= limit {
			break
		}
	}

	writeSuccess(c, map[string]any{
		"items": results,
		"since": cutoff,
		"total": len(results),
	})
}
