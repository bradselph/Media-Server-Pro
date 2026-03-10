package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

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

	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	canViewMature := h.canViewMatureContent(c)
	contentSuggestions := h.suggestions.GetSuggestions(userID, limit, canViewMature)
	h.enrichSuggestionThumbnails(contentSuggestions)
	writeSuccess(c, contentSuggestions)
}

// GetTrendingSuggestions returns trending content
func (h *Handler) GetTrendingSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	if !h.requireSuggestionsCatalogue(c) {
		return
	}
	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

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

	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

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

	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 50 {
		limit = l
	}

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

	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	canViewMature := h.canViewMatureContent(c)
	contentSuggestions := h.suggestions.GetSuggestions(session.UserID, limit, canViewMature)
	h.enrichSuggestionThumbnails(contentSuggestions)
	writeSuccess(c, contentSuggestions)
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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	absPath, ok := h.resolveMediaByID(c, req.ID)
	if !ok {
		return
	}

	h.suggestions.RecordRating(session.UserID, absPath, req.Rating)
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
