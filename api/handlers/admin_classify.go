package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
)

// ClassifyStatus returns the Hugging Face integration status (configured, model, rate limit).
// GET /api/admin/classify/status
func (h *Handler) ClassifyStatus(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	cfg := h.config.Get().HuggingFace
	writeSuccess(c, map[string]interface{}{
		"configured":     h.scanner.HasHuggingFace(),
		"enabled":        cfg.Enabled,
		"model":          cfg.Model,
		"rate_limit":     cfg.RateLimit,
		"max_frames":     cfg.MaxFrames,
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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, "path is required")
		return
	}
	absPath, ok := h.resolvePathForAdmin(c, req.Path, false)
	if !ok {
		return
	}
	tags, err := h.scanner.ClassifyMatureContent(c.Request.Context(), absPath)
	if err != nil {
		h.log.Warn("ClassifyFile failed for %s: %v", absPath, err)
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if len(tags) > 0 {
		if err := h.media.UpdateTags(absPath, tags); err != nil {
			h.log.Warn("Failed to update tags for %s: %v", absPath, err)
			writeError(c, http.StatusInternalServerError, "classification succeeded but failed to save tags: "+err.Error())
			return
		}
	}
	writeSuccess(c, map[string]interface{}{
		"path": absPath,
		"tags": tags,
	})
}

// validateClassifyDirectoryRequest checks scanner, Hugging Face, request body, and path.
// Returns (dirPath, true) when valid; otherwise writes an error and returns ("", false).
func (h *Handler) validateClassifyDirectoryRequest(c *gin.Context) (dirPath string, ok bool) {
	if !h.requireScanner(c) {
		return "", false
	}
	if !h.scanner.HasHuggingFace() {
		writeError(c, http.StatusServiceUnavailable, "Hugging Face classification is not configured (set HUGGINGFACE_API_KEY and enable the feature)")
		return "", false
	}
	var req struct {
		Path string `json:"path"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, "path is required")
		return "", false
	}
	path, resolved := h.resolvePathForAdmin(c, req.Path, true)
	if !resolved {
		return "", false
	}
	return path, true
}

// runClassifyDirectoryBackground runs classification on mature-flagged files in dirPath (in background).
func (h *Handler) runClassifyDirectoryBackground(dirPath string) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Warn("ClassifyDirectory panic for %s: %v", dirPath, r)
		}
	}()
	ctx := context.Background()
	results, err := h.scanner.ClassifyMatureDirectory(ctx, dirPath)
	if err != nil {
		h.log.Warn("ClassifyDirectory failed for %s: %v", dirPath, err)
		return
	}
	for path, tags := range results {
		if len(tags) > 0 {
			if err := h.media.UpdateTags(path, tags); err != nil {
				h.log.Warn("Failed to update tags for %s: %v", path, err)
			}
		}
	}
	h.log.Info("ClassifyDirectory completed for %s: %d files", dirPath, len(results))
}

// ClassifyDirectory runs visual classification on all mature-flagged files in a directory.
// It returns 202 Accepted immediately and runs the work in the background so long-running
// jobs do not hit proxy/client timeouts or produce truncated JSON.
// POST /api/admin/classify/directory — body: { "path": "/absolute/path/to/directory" }
func (h *Handler) ClassifyDirectory(c *gin.Context) {
	dirPath, ok := h.validateClassifyDirectoryRequest(c)
	if !ok {
		return
	}
	go h.runClassifyDirectoryBackground(dirPath)
	c.JSON(http.StatusAccepted, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"message":   "Classification started in background. This may take several minutes.",
			"directory": dirPath,
		},
	})
}
