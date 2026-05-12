package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/repositories"
)

// savedSearchView is the JSON shape sent to the SPA. Tags are sent as a
// slice (more idiomatic on the wire than the CSV used at storage).
type savedSearchView struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Query      string   `json:"query"`
	Tags       []string `json:"tags"`
	TagMode    string   `json:"tag_mode"`
	MediaType  string   `json:"media_type"`
	CreatedAt  string   `json:"created_at"`
	LastSeenAt string   `json:"last_seen_at"`
}

func recordToView(rec *repositories.SavedSearchRecord) savedSearchView {
	tags := rec.Tags
	if tags == nil {
		tags = []string{}
	}
	return savedSearchView{
		ID:         rec.ID,
		Name:       rec.Name,
		Query:      rec.Query,
		Tags:       tags,
		TagMode:    rec.TagMode,
		MediaType:  rec.MediaType,
		CreatedAt:  rec.CreatedAt.Format(timeFormatRFC3339Ext),
		LastSeenAt: rec.LastSeenAt.Format(timeFormatRFC3339Ext),
	}
}

// ListSavedSearches returns the user's saved searches.
// GET /api/preferences/saved_searches
func (h *Handler) ListSavedSearches(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	recs, err := h.auth.ListSavedSearches(c.Request.Context(), session.UserID)
	if err != nil {
		h.log.Error("ListSavedSearches: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to list saved searches")
		return
	}
	views := make([]savedSearchView, len(recs))
	for i, r := range recs {
		views[i] = recordToView(r)
	}
	writeSuccess(c, views)
}

// CreateSavedSearch persists a search definition for the user.
// POST /api/preferences/saved_searches
// Body: { name, query, tags [], tag_mode "and"|"or", media_type }
func (h *Handler) CreateSavedSearch(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	var req struct {
		Name      string   `json:"name"`
		Query     string   `json:"query"`
		Tags      []string `json:"tags"`
		TagMode   string   `json:"tag_mode"`
		MediaType string   `json:"media_type"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	// Normalise tags — drop empties, trim spaces.
	cleanTags := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		if t = strings.TrimSpace(t); t != "" {
			cleanTags = append(cleanTags, t)
		}
	}
	rec, err := h.auth.SaveSearch(c.Request.Context(), &repositories.SavedSearchRecord{
		UserID:    session.UserID,
		Name:      req.Name,
		Query:     req.Query,
		Tags:      cleanTags,
		TagMode:   req.TagMode,
		MediaType: req.MediaType,
	})
	if err != nil {
		h.log.Error("CreateSavedSearch: %v", err)
		// Surface user-actionable error messages (e.g. limit reached) so the
		// SPA can show them; internal storage failures still get a generic 500.
		if strings.Contains(err.Error(), "limit reached") || err.Error() == "userID and name are required" {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, "Failed to save search")
		return
	}
	writeSuccess(c, recordToView(rec))
}

// DeleteSavedSearch removes a saved search owned by the user.
// DELETE /api/preferences/saved_searches/:id
func (h *Handler) DeleteSavedSearch(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	if err := h.auth.DeleteSavedSearch(c.Request.Context(), id, session.UserID); err != nil {
		h.log.Error("DeleteSavedSearch: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to delete saved search")
		return
	}
	writeSuccess(c, map[string]string{"deleted": id})
}

// TouchSavedSearch updates the last-seen timestamp so the "new since" diff
// resets after the user reviews the matches. Called by the SPA when it
// renders the saved-search row on the homepage.
// POST /api/preferences/saved_searches/:id/seen
func (h *Handler) TouchSavedSearch(c *gin.Context) {
	session := RequireSession(c)
	if session == nil {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	if err := h.auth.TouchSavedSearch(c.Request.Context(), id, session.UserID); err != nil {
		h.log.Error("TouchSavedSearch: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to update saved search")
		return
	}
	writeSuccess(c, nil)
}
