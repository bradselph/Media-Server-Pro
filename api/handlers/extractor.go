package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/extractor"
	"media-server-pro/pkg/helpers"
)

// AddExtractorItem adds an M3U8 stream URL to the library.
// POST /api/admin/extractor/items  { "url": "https://...m3u8", "title": "..." }
func (h *Handler) AddExtractorItem(c *gin.Context) {
	if !h.checkExtractorEnabled(c) {
		return
	}

	var req struct {
		URL   string `json:"url" binding:"required"`
		Title string `json:"title"`
	}
	if !BindJSON(c, &req, "url is required") {
		return
	}

	if err := helpers.ValidateURLForSSRF(req.URL); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	user := getUser(c)
	addedBy := ""
	if user != nil {
		addedBy = user.Username
	}

	item, err := h.extractor.AddItem(req.URL, req.Title, addedBy)
	if err != nil {
		h.log.Error("Failed to add extractor item: %v", err)
		writeError(c, http.StatusBadRequest, fmt.Sprintf("Failed to add item: %v", err))
		return
	}

	writeSuccess(c, item)
}

// ListExtractorItems returns all proxied stream items.
// GET /api/admin/extractor/items
func (h *Handler) ListExtractorItems(c *gin.Context) {
	if !h.checkExtractorEnabled(c) {
		return
	}
	writeSuccess(c, h.extractor.GetAllItems())
}

// RemoveExtractorItem removes a proxied stream.
// DELETE /api/admin/extractor/items/:id
func (h *Handler) RemoveExtractorItem(c *gin.Context) {
	if !h.checkExtractorEnabled(c) {
		return
	}

	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}

	if err := h.extractor.RemoveItem(id); err != nil {
		if errors.Is(err, extractor.ErrNotFound) {
			writeError(c, http.StatusNotFound, "Item not found")
			return
		}
		h.log.Error("Failed to remove extractor item %s: %v", id, err)
		writeError(c, http.StatusInternalServerError, "Failed to remove item")
		return
	}

	writeSuccess(c, map[string]string{"status": "removed"})
}

// GetExtractorStats returns extractor statistics.
// GET /api/admin/extractor/stats
func (h *Handler) GetExtractorStats(c *gin.Context) {
	if !h.checkExtractorEnabled(c) {
		return
	}
	writeSuccess(c, h.extractor.GetStats())
}

// ExtractorHLSMaster proxies the HLS master playlist for a proxied stream.
// GET /extractor/hls/:id/master.m3u8
func (h *Handler) ExtractorHLSMaster(c *gin.Context) {
	if h.extractor == nil {
		writeError(c, http.StatusNotFound, "extractor module not enabled")
		return
	}

	id := c.Param("id")
	if err := h.extractor.ProxyHLSMaster(c.Writer, c.Request, id); err != nil {
		h.log.Error("Extractor HLS master proxy error: %v", err)
		if !c.Writer.Written() {
			c.Status(http.StatusBadGateway)
		}
	}
}

// ExtractorHLSVariant proxies an HLS variant playlist for a proxied stream.
// GET /extractor/hls/:id/:quality/playlist.m3u8
func (h *Handler) ExtractorHLSVariant(c *gin.Context) {
	if h.extractor == nil {
		writeError(c, http.StatusNotFound, "extractor module not enabled")
		return
	}

	id := c.Param("id")
	qualityStr := c.Param("quality")
	qualityIdx, err := strconv.Atoi(qualityStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if err := h.extractor.ProxyHLSVariant(c.Writer, c.Request, id, qualityIdx); err != nil {
		h.log.Error("Extractor HLS variant proxy error: %v", err)
		if !c.Writer.Written() {
			c.Status(http.StatusBadGateway)
		}
	}
}

// ExtractorHLSSegment proxies an HLS segment for a proxied stream.
// GET /extractor/hls/:id/:quality/:segment
func (h *Handler) ExtractorHLSSegment(c *gin.Context) {
	if h.extractor == nil {
		writeError(c, http.StatusNotFound, "extractor module not enabled")
		return
	}

	id := c.Param("id")
	qualityStr := c.Param("quality")
	segment := c.Param("segment")

	qualityIdx, err := strconv.Atoi(qualityStr)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if err := h.extractor.ProxyHLSSegment(c.Writer, c.Request, id, qualityIdx, segment); err != nil {
		h.log.Debug("Extractor HLS segment proxy error: %v", err)
		if !c.Writer.Written() {
			c.Status(http.StatusBadGateway)
		}
	}
}
