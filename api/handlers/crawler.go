package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// --- Crawler Target Handlers ---

// AddCrawlerTarget adds a new crawl target.
// POST /api/admin/crawler/targets  { "url": "https://...", "name": "..." }
// TODO: Same SSRF concern as AddExtractorItem — the URL is not validated against internal
// addresses. The crawler will fetch the URL to discover M3U8 streams, potentially
// reaching internal services.
func (h *Handler) AddCrawlerTarget(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	var req struct {
		URL  string `json:"url" binding:"required"`
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "url is required")
		return
	}

	target, err := h.crawler.AddTarget(req.Name, req.URL)
	if err != nil {
		h.log.Error("Failed to add crawler target: %v", err)
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	writeSuccess(c, target)
}

// ListCrawlerTargets returns all crawl targets.
// GET /api/admin/crawler/targets
func (h *Handler) ListCrawlerTargets(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	targets, err := h.crawler.GetTargets()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, targets)
}

// RemoveCrawlerTarget removes a crawl target.
// DELETE /api/admin/crawler/targets/:id
func (h *Handler) RemoveCrawlerTarget(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	id := c.Param("id")
	if err := h.crawler.RemoveTarget(id); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, map[string]string{"status": "removed"})
}

// CrawlTarget triggers a crawl for a specific target.
// POST /api/admin/crawler/targets/:id/crawl
func (h *Handler) CrawlTarget(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	id := c.Param("id")
	newCount, err := h.crawler.CrawlTarget(id)
	if err != nil {
		h.log.Error("Crawl failed: %v", err)
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	writeSuccess(c, map[string]interface{}{
		"new_discoveries": newCount,
	})
}

// --- Crawler Discovery Handlers ---

// ListCrawlerDiscoveries returns discoveries for admin review.
// GET /api/admin/crawler/discoveries?status=pending
func (h *Handler) ListCrawlerDiscoveries(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	status := c.Query("status")
	discoveries, err := h.crawler.GetDiscoveries(status)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, discoveries)
}

// ApproveCrawlerDiscovery approves a discovery and adds it to the extractor.
// POST /api/admin/crawler/discoveries/:id/approve
func (h *Handler) ApproveCrawlerDiscovery(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	id := c.Param("id")
	user := getUser(c)
	reviewedBy := ""
	if user != nil {
		reviewedBy = user.Username
	}

	disc, err := h.crawler.ApproveDiscovery(id, reviewedBy)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, disc)
}

// IgnoreCrawlerDiscovery marks a discovery as ignored.
// POST /api/admin/crawler/discoveries/:id/ignore
func (h *Handler) IgnoreCrawlerDiscovery(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	id := c.Param("id")
	user := getUser(c)
	reviewedBy := ""
	if user != nil {
		reviewedBy = user.Username
	}

	if err := h.crawler.IgnoreDiscovery(id, reviewedBy); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeSuccess(c, map[string]string{"status": "ignored"})
}

// DeleteCrawlerDiscovery permanently deletes a discovery.
// DELETE /api/admin/crawler/discoveries/:id
func (h *Handler) DeleteCrawlerDiscovery(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}

	id := c.Param("id")
	if err := h.crawler.DeleteDiscovery(id); err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(c, map[string]string{"status": "deleted"})
}

// GetCrawlerStats returns crawler statistics.
// GET /api/admin/crawler/stats
func (h *Handler) GetCrawlerStats(c *gin.Context) {
	if !h.checkCrawlerEnabled(c) {
		return
	}
	writeSuccess(c, h.crawler.GetStats())
}
