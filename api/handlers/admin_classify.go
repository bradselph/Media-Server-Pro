package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/media"
	"media-server-pro/pkg/models"
)

// ClassifyStatus returns the Hugging Face integration status (configured, model, rate limit)
// plus the background task state so the admin can see whether a run is in progress.
// GET /api/admin/classify/status
func (h *Handler) ClassifyStatus(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	cfg := h.config.Get().HuggingFace

	resp := map[string]interface{}{
		"configured":     h.scanner.HasHuggingFace(),
		"enabled":        cfg.Enabled,
		"model":          cfg.Model,
		"rate_limit":     cfg.RateLimit,
		"max_frames":     cfg.MaxFrames,
		"max_concurrent": cfg.MaxConcurrent,
	}

	// Include background task info if the tasks module is available.
	if h.tasks != nil {
		if info, err := h.tasks.GetTask("hf-classification"); err == nil {
			resp["task_running"] = info.Running
			resp["task_last_run"] = info.LastRun
			resp["task_next_run"] = info.NextRun
			resp["task_last_error"] = info.LastError
			resp["task_enabled"] = info.Enabled
		}
	}

	writeSuccess(c, resp)
}

// ClassifyStats returns classification progress: how many mature items have been
// classified (tagged) vs still pending, plus the most recently classified items.
// GET /api/admin/classify/stats
func (h *Handler) ClassifyStats(c *gin.Context) {
	if h.media == nil {
		writeError(c, http.StatusServiceUnavailable, "Media module not available")
		return
	}
	stats := h.media.GetClassifyStats(20)
	writeSuccess(c, stats)
}

// ClassifyRunTask triggers the hf-classification background task immediately.
// POST /api/admin/classify/run-task
func (h *Handler) ClassifyRunTask(c *gin.Context) {
	if h.tasks == nil {
		writeError(c, http.StatusServiceUnavailable, "Tasks module not available")
		return
	}
	if err := h.tasks.RunNow("hf-classification"); err != nil {
		writeError(c, http.StatusConflict, err.Error())
		return
	}
	writeSuccess(c, map[string]interface{}{
		"message": "HF classification task started.",
	})
}

// ClassifyClearTags removes all tags from a specific media item.
// POST /api/admin/classify/clear-tags — body: { "id": "media-uuid" }
func (h *Handler) ClassifyClearTags(c *gin.Context) {
	if h.media == nil {
		writeError(c, http.StatusServiceUnavailable, "Media module not available")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if req.ID == "" {
		writeError(c, http.StatusBadRequest, "id is required")
		return
	}
	item, err := h.media.GetMediaByID(req.ID)
	if err != nil {
		writeError(c, http.StatusNotFound, "Media item not found")
		return
	}
	if err := h.media.SetTags(item.Path, []string{}); err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to clear tags: "+err.Error())
		return
	}
	writeSuccess(c, map[string]interface{}{
		"message": "Tags cleared.",
		"id":      req.ID,
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
	if !BindJSON(c, &req, "path is required") {
		return
	}
	absPath, ok := h.resolvePathForAdmin(c, req.Path, false)
	if !ok {
		return
	}
	tags, err := h.scanner.ClassifyMatureContent(c.Request.Context(), absPath)
	if err != nil {
		h.log.Warn("ClassifyFile failed for %s: %v", absPath, err)
		writeError(c, http.StatusInternalServerError, "Classification failed")
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
	if err := c.ShouldBindJSON(&req); err != nil {
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

// ClassifyAllPending triggers classification on all mature items that have no tags yet.
// POST /api/admin/classify/all-pending
func (h *Handler) ClassifyAllPending(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	if !h.scanner.HasHuggingFace() {
		writeError(c, http.StatusServiceUnavailable, "Hugging Face classification is not configured")
		return
	}
	if h.media == nil {
		writeError(c, http.StatusServiceUnavailable, "Media module not available")
		return
	}

	items := h.media.ListMedia(media.Filter{IsMature: new(true)})
	var pending []string
	for _, item := range items {
		if len(item.Tags) == 0 {
			pending = append(pending, item.Path)
		}
	}

	if len(pending) == 0 {
		writeSuccess(c, map[string]interface{}{
			"message": "No pending items to classify.",
			"count":   0,
		})
		return
	}

	go h.runClassifyAllPendingBackground(pending)

	c.JSON(http.StatusAccepted, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"message": "Classification started for all pending items.",
			"count":   len(pending),
		},
	})
}

// runClassifyAllPendingBackground classifies a list of file paths in the background.
func (h *Handler) runClassifyAllPendingBackground(paths []string) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Warn("ClassifyAllPending panic: %v", r)
		}
	}()
	ctx := context.Background()
	tagged := 0
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		tags, err := h.scanner.ClassifyMatureContent(ctx, path)
		if err != nil {
			h.log.Warn("ClassifyAllPending failed for %s: %v", path, err)
			continue
		}
		if len(tags) > 0 {
			if err := h.media.UpdateTags(path, tags); err != nil {
				h.log.Warn("Failed to update tags for %s: %v", path, err)
			} else {
				tagged++
			}
		}
	}
	h.log.Info("ClassifyAllPending completed: tagged %d of %d items", tagged, len(paths))
}
