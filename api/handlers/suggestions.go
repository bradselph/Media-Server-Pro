package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// parseSuggestionsLimit parses the limit query param; returns defaultVal if missing/invalid.
func parseSuggestionsLimit(c *gin.Context, defaultVal, max int) int {
	l, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		return defaultVal
	}
	if l <= 0 || l > max {
		return defaultVal
	}
	return l
}

// requireSuggestionsCatalogue checks that the suggestions module's media
// catalogue has been seeded. Returns 503 with Retry-After if the catalogue
// is empty (server just started, initial scan still in progress).
func (h *Handler) requireSuggestionsCatalogue(c *gin.Context) bool {
	if !h.suggestions.IsCatalogueReady() {
		c.Header("Retry-After", "3")
		writeError(c, http.StatusServiceUnavailable, "Suggestions are loading — media catalogue scan in progress, please try again shortly")
		return false
	}
	return true
}

// respondSuggestions fetches personalized suggestions and writes the response.
func (h *Handler) respondSuggestions(c *gin.Context, userID string, defaultLimit, maxLimit int) {
	limit := parseSuggestionsLimit(c, defaultLimit, maxLimit)
	canViewMature := h.canViewMatureContent(c)
	suggestions := h.suggestions.GetSuggestions(userID, limit, canViewMature)
	h.enrichSuggestionThumbnails(suggestions)
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
	limit := parseSuggestionsLimit(c, 10, 100)
	canViewMature := h.canViewMatureContent(c)
	trending := h.suggestions.GetTrendingSuggestions(limit, canViewMature)
	h.enrichSuggestionThumbnails(trending)
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

	limit := parseSuggestionsLimit(c, 10, 100)

	// Pass the StableID directly to the suggestions module which has its own
	// catalogue indexed by ID. No need to validate via the media module —
	// the suggestions engine handles unknown IDs gracefully (returns random sample).
	canViewMature := h.canViewMatureContent(c)
	similar := h.suggestions.GetSimilarMedia(id, limit, canViewMature)
	h.enrichSuggestionThumbnails(similar)
	writeSuccess(c, similar)
}

// GetContinueWatching returns items the user started but didn't finish
func (h *Handler) GetContinueWatching(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}

	limit := parseSuggestionsLimit(c, 10, 50)
	canViewMature := h.canViewMatureContent(c)
	items := h.suggestions.GetContinueWatching(session.UserID, limit, canViewMature)
	h.enrichSuggestionThumbnails(items)
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
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
		return
	}
	h.respondSuggestions(c, session.UserID, 10, 100)
}

// RecordRating records a user rating for a media item
func (h *Handler) RecordRating(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	session := getSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, errNotAuthenticated)
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
