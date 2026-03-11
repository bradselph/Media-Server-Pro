package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ClassifyStatus returns the Hugging Face integration status (configured, model, rate limit).
// GET /api/admin/classify/status
func (h *Handler) ClassifyStatus(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	cfg := h.config.Get().HuggingFace
	writeSuccess(c, map[string]interface{}{
		"configured":   h.scanner.HasHuggingFace(),
		"enabled":      cfg.Enabled,
		"model":        cfg.Model,
		"rate_limit":   cfg.RateLimit,
		"max_frames":   cfg.MaxFrames,
		"max_concurrent": cfg.MaxConcurrent,
	})
}

// ClassifyFile runs visual classification on a single file.
// POST /api/admin/classify/file — body: { "path": "/absolute/path/to/file" }
func (h *Handler) ClassifyFile(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	if !h.scanner.HasHuggingFace() {
		writeError(c, http.StatusServiceUnavailable, "Hugging Face classification is not configured (set HUGGINGFACE_API_KEY and enable the feature)")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Path == "" {
		writeError(c, http.StatusBadRequest, "path is required")
		return
	}
	tags, err := h.scanner.ClassifyMatureContent(c.Request.Context(), req.Path)
	if err != nil {
		h.log.Warn("ClassifyFile failed for %s: %v", req.Path, err)
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if len(tags) > 0 {
		if err := h.media.UpdateTags(req.Path, tags); err != nil {
			h.log.Warn("Failed to update tags for %s: %v", req.Path, err)
			writeError(c, http.StatusInternalServerError, "classification succeeded but failed to save tags: "+err.Error())
			return
		}
	}
	writeSuccess(c, map[string]interface{}{
		"path": req.Path,
		"tags": tags,
	})
}

// ClassifyDirectory runs visual classification on all mature-flagged files in a directory.
// POST /api/admin/classify/directory — body: { "path": "/absolute/path/to/directory" }
func (h *Handler) ClassifyDirectory(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	if !h.scanner.HasHuggingFace() {
		writeError(c, http.StatusServiceUnavailable, "Hugging Face classification is not configured (set HUGGINGFACE_API_KEY and enable the feature)")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Path == "" {
		writeError(c, http.StatusBadRequest, "path is required")
		return
	}
	results, err := h.scanner.ClassifyMatureDirectory(c.Request.Context(), req.Path)
	if err != nil {
		h.log.Warn("ClassifyDirectory failed for %s: %v", req.Path, err)
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	// Apply tags to media module
	for path, tags := range results {
		if len(tags) > 0 {
			if err := h.media.UpdateTags(path, tags); err != nil {
				h.log.Warn("Failed to update tags for %s: %v", path, err)
			}
		}
	}
	writeSuccess(c, map[string]interface{}{
		"directory": req.Path,
		"results":   results,
	})
}
