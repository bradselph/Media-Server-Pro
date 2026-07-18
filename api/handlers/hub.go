package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/hub"
)

// ─── Public (BETA Hub embed catalog) ────────────────────────────────────────
// Every handler gates on requireHub first (feature + module presence). Because
// the entire Hub catalog is mature content, the public read endpoints also gate
// on checkMatureAccess — only logged-in users who have enabled mature viewing
// (permission + preference) can browse it, consistent with the age-gate model.

// ListHubEmbeds returns a paginated, optionally filtered page of Hub embeds.
// GET /api/hub/embeds?limit=&offset=&search=&category=&tag=&sort=
func (h *Handler) ListHubEmbeds(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	if !h.checkMatureAccess(c, true) {
		return
	}
	c.Header(headerCacheControl, "private, max-age=120")

	pageSize := h.config.Get().Hub.PageSize
	if pageSize <= 0 {
		pageSize = 60
	}
	limit, offset := ParseLimitOffset(c, LimitOffsetOpts{
		DefaultLimit: pageSize, MaxLimit: 200, DefaultOffset: 0, MaxOffset: 100000,
	})

	filter := hub.Filter{
		Search:   truncateQuery(c.Query("search"), 200),
		Category: truncateQuery(c.Query("category"), 100),
		Tag:      truncateQuery(c.Query("tag"), 100),
		SortBy:   c.Query("sort"),
	}

	items, total, err := h.hub.GetEmbeds(c.Request.Context(), filter, limit, offset)
	if err != nil {
		h.log.Error("Hub: list embeds failed: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeSuccess(c, gin.H{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetHubEmbed returns a single embed by its provider embed id.
// GET /api/hub/embeds/:id
func (h *Handler) GetHubEmbed(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	if !h.checkMatureAccess(c, true) {
		return
	}
	item, err := h.hub.GetEmbedByID(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Hub: get embed failed: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	if item == nil {
		writeError(c, http.StatusNotFound, "Hub embed not found")
		return
	}
	writeSuccess(c, item)
}

// ListHubCategories returns the distinct category facet list for the filter UI.
// GET /api/hub/categories
func (h *Handler) ListHubCategories(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	if !h.checkMatureAccess(c, true) {
		return
	}
	cats, err := h.hub.ListCategories(c.Request.Context())
	if err != nil {
		h.log.Error("Hub: list categories failed: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	c.Header(headerCacheControl, "private, max-age=600")
	writeSuccess(c, cats)
}

// ─── Admin (import management) ───────────────────────────────────────────────

// AdminTriggerHubImport starts a streaming CSV import in the background from the
// server-configured hub.csv_path. The path is intentionally NOT accepted from the
// request body — it is config/env only (see configFieldDenyList) so a remote admin
// can't redirect the importer at an arbitrary server file.
// POST /api/admin/hub/import
func (h *Handler) AdminTriggerHubImport(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	if err := h.hub.TriggerImport(""); err != nil {
		writeError(c, http.StatusConflict, err.Error())
		return
	}
	h.logAdminAction(c, &adminLogActionParams{Action: "hub_import", Target: "hub"})
	writeSuccess(c, gin.H{"status": "started"})
}

// AdminHubStatus returns the current row count and import job progress.
// GET /api/admin/hub/status
func (h *Handler) AdminHubStatus(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	writeSuccess(c, h.hub.ImportStatus())
}

// AdminClearHub truncates the hub_embeds table.
// POST /api/admin/hub/clear
func (h *Handler) AdminClearHub(c *gin.Context) {
	if !h.requireHub(c) {
		return
	}
	if err := h.hub.ClearAll(c.Request.Context()); err != nil {
		h.log.Error("Hub: clear failed: %v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	h.logAdminAction(c, &adminLogActionParams{Action: "hub_clear", Target: "hub"})
	writeSuccess(c, gin.H{"status": "cleared"})
}
