package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetSuggestions returns personalized content suggestions
func (h *Handler) GetSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
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

	contentSuggestions := h.suggestions.GetSuggestions(userID, limit)
	h.enrichSuggestionThumbnails(contentSuggestions)
	writeSuccess(c, contentSuggestions)
}

// GetTrendingSuggestions returns trending content
func (h *Handler) GetTrendingSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	trending := h.suggestions.GetTrendingSuggestions(limit)
	h.enrichSuggestionThumbnails(trending)
	writeSuccess(c, trending)
}

// GetSimilarMedia returns similar media to a given item
func (h *Handler) GetSimilarMedia(c *gin.Context) {
	if !h.requireSuggestions(c) {
		return
	}
	id := c.Query("id")
	// Validate the media ID exists before querying the suggestions engine.
	if _, ok := h.resolveMediaByID(c, id); !ok {
		return
	}

	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	// Pass the StableID directly — the suggestions module indexes by ID,
	// avoiding path-mismatch issues when the catalogue hasn't refreshed yet.
	similar := h.suggestions.GetSimilarMedia(id, limit)
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

	items := h.suggestions.GetContinueWatching(session.UserID, limit)
	h.enrichSuggestionThumbnails(items)
	writeSuccess(c, items)
}

// GetPersonalizedSuggestions returns personalized suggestions (auth-gated alias for GetSuggestions).
func (h *Handler) GetPersonalizedSuggestions(c *gin.Context) {
	if !h.requireSuggestions(c) {
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

	contentSuggestions := h.suggestions.GetSuggestions(session.UserID, limit)
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
