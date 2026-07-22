package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/hub"
)

// trackHubEvent records a Hub (BETA) engagement analytics event, attaching the
// caller's session/IP/User-Agent via trackServerEvent. It is skipped entirely
// for private sessions so a user browsing incognito does not appear in per-user
// drill-downs — mirroring SubmitEvent's private-session handling. eventType must
// be one of the analytics EventHub* constants so it maps to a daily_stats column.
func (h *Handler) trackHubEvent(c *gin.Context, eventType string, data map[string]any) {
	if isPrivateSession(c) {
		return
	}
	h.trackServerEvent(c, eventType, data)
}

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

	// Track catalog engagement server-side (forge-resistant). A browse is one
	// catalog page load; a non-empty search additionally increments the dedicated
	// hub_searches counter. Hub searches are kept separate from the local library's
	// top-searches/content-gaps panels (those query the "search" event type only) —
	// the Hub is a distinct external catalog, so conflating the two signals would
	// mislead. The empty-result flag is still recorded on the event for any future
	// Hub-specific content-gap view.
	page := 1
	if limit > 0 {
		page = offset/limit + 1
	}
	h.trackHubEvent(c, analytics.EventHubBrowse, map[string]any{
		"category": filter.Category,
		"tag":      filter.Tag,
		"sort":     filter.SortBy,
		"page":     page,
		"results":  total,
	})
	if filter.Search != "" {
		h.trackHubEvent(c, analytics.EventHubSearch, map[string]any{
			"query": filter.Search,
			"empty": total == 0,
		})
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
	// A single-embed fetch is the point at which a user actually opens an item to
	// watch it — both the /hub grid modal and the full player load the embed this
	// way — so it is the accurate, forge-resistant place to record a Hub "view".
	h.trackHubEvent(c, analytics.EventHubView, map[string]any{"embed_id": id})
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
	// Dedicated event (in addition to the admin_action logAdminAction emits) so the
	// Hub panel can count imports without string-matching admin_action payloads.
	h.trackServerEvent(c, analytics.EventHubImport, map[string]any{"source": "admin"})
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
	h.trackServerEvent(c, analytics.EventHubClear, nil)
	writeSuccess(c, gin.H{"status": "cleared"})
}

// AdminGetHubAnalytics returns a single-round-trip rollup for the admin
// dashboard's Hub (BETA) panel: catalog size, current import job state, today's
// engagement counters, all-time totals per Hub event type, and short view/browse
// timelines for sparklines. Returns {"enabled": false} (never an error) when the
// feature is off, so the dashboard can simply hide the panel.
// GET /api/admin/analytics/hub?days=
func (h *Handler) AdminGetHubAnalytics(c *gin.Context) {
	if h.hub == nil || !h.config.Get().Hub.Enabled {
		writeSuccess(c, gin.H{"enabled": false})
		return
	}
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}

	ctx := c.Request.Context()
	catalogSize, err := h.hub.CountAll(ctx)
	if err != nil {
		h.log.Warn("Hub analytics: catalog count failed: %v", err)
	}

	payload := gin.H{
		"enabled":      true,
		"catalog_size": catalogSize,
		"import":       h.hub.ImportStatus(),
	}

	if h.analytics != nil {
		summary := h.analytics.GetSummary(ctx)
		counts := h.analytics.GetEventTypeCounts(ctx)
		payload["today"] = gin.H{
			"browses":       summary.TodayHubBrowses,
			"views":         summary.TodayHubViews,
			"searches":      summary.TodayHubSearches,
			"playlist_adds": summary.TodayHubPlaylistAdds,
		}
		payload["totals"] = gin.H{
			"browses":       counts[analytics.EventHubBrowse],
			"views":         counts[analytics.EventHubView],
			"searches":      counts[analytics.EventHubSearch],
			"playlist_adds": counts[analytics.EventHubPlaylistAdd],
			"imports":       counts[analytics.EventHubImport],
			"clears":        counts[analytics.EventHubClear],
		}
		payload["views_timeline"] = h.analytics.GetMetricTimeline("hub_views", days)
		payload["browses_timeline"] = h.analytics.GetMetricTimeline("hub_browses", days)
	}
	writeSuccess(c, payload)
}
