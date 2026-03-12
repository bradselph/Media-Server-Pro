package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/scanner"
	"media-server-pro/pkg/models"
)

// scanConfiguredDirectories runs the scanner on Videos, Music, and Uploads dirs from cfg and returns combined results.
func (h *Handler) scanConfiguredDirectories(cfg *config.Config) []*scanner.ScanResult {
	var all []*scanner.ScanResult
	for _, dir := range []string{cfg.Directories.Videos, cfg.Directories.Music, cfg.Directories.Uploads} {
		if dir == "" {
			continue
		}
		results, err := h.scanner.ScanDirectory(dir)
		if err != nil {
			h.log.Error("Failed to scan directory %q: %v", dir, err)
			continue
		}
		all = append(all, results...)
	}
	return all
}

// processScanResults counts autoFlagged/reviewNeeded/clean and optionally applies mature flags; returns the three counts.
func (h *Handler) processScanResults(results []*scanner.ScanResult, autoApply bool) (autoFlagged, reviewNeeded, clean int) {
	for _, r := range results {
		if r.AutoFlagged {
			autoFlagged++
		}
		if r.NeedsReview {
			reviewNeeded++
		}
		if !r.IsMature && !r.NeedsReview {
			clean++
		}
		if autoApply && r.IsMature {
			if err := h.media.SetMatureFlag(r.Path, true, r.Confidence, r.Reasons); err != nil {
				h.log.Error("Failed to set mature flag for %s: %v", r.Path, err)
			}
		}
	}
	return autoFlagged, reviewNeeded, clean
}

// ScanContent scans media files for mature content
func (h *Handler) ScanContent(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	var req struct {
		Path      string `json:"path"`
		AutoApply bool   `json:"auto_apply"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if req.Path != "" {
		result := h.scanner.ScanFile(req.Path)
		writeSuccess(c, result)
		return
	}

	cfg := h.media.GetConfig()
	allResults := h.scanConfiguredDirectories(cfg)
	autoFlagged, reviewNeeded, clean := h.processScanResults(allResults, req.AutoApply)

	writeSuccess(c, map[string]interface{}{
		"stats":              h.scanner.GetStats(),
		"scanned":            len(allResults),
		"auto_flagged_count": autoFlagged,
		"review_queue_count": reviewNeeded,
		"clean":              clean,
		"message":            fmt.Sprintf("Scanned %d files", len(allResults)),
	})
}

// GetScannerStats returns scanner statistics
func (h *Handler) GetScannerStats(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	stats := h.scanner.GetStats()
	writeSuccess(c, stats)
}

// GetReviewQueue returns items pending review as a flat array.
// Enriches each item's ID with the media module's stable UUID so that
// approve/reject handlers can resolve items via resolveMediaByID.
func (h *Handler) GetReviewQueue(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	queue := h.scanner.GetReviewQueue()
	// Build response copies so we don't mutate the scanner's internal state.
	enriched := make([]*models.MatureReviewItem, len(queue))
	for i, item := range queue {
		copy := *item // shallow copy
		if mediaItem, err := h.media.GetMedia(item.MediaPath); err == nil && mediaItem != nil {
			copy.ID = mediaItem.ID // Replace MD5 hash with stable UUID
		}
		enriched[i] = &copy
	}
	writeSuccess(c, enriched)
}

// applyReviewActionToItem runs approve or reject for one item; returns true if the item was updated.
func (h *Handler) applyReviewActionToItem(ctx context.Context, action string, id string) bool {
	item, err := h.media.GetMediaByID(id)
	if err != nil || item == nil {
		return false
	}
	path := item.Path
	if action == "approve" {
		if err := h.scanner.ApproveContent(ctx, path); err != nil {
			return false
		}
		confidence := 0.0
		var reasons []string
		if result, ok := h.scanner.GetScanResult(path); ok {
			confidence = result.Confidence
			reasons = result.Reasons
		}
		if setErr := h.media.SetMatureFlag(path, true, confidence, reasons); setErr != nil {
			h.log.Error("Failed to update media library mature flag for %s: %v", id, setErr)
		}
		return true
	}
	if err := h.scanner.RejectContent(ctx, path); err != nil {
		return false
	}
	if setErr := h.media.SetMatureFlag(path, false, 0, nil); setErr != nil {
		h.log.Error("Failed to update media library mature flag for %s: %v", id, setErr)
	}
	return true
}

// BatchReviewAction applies approve/reject action to multiple review queue items
func (h *Handler) BatchReviewAction(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		writeError(c, http.StatusBadRequest, "Invalid action: must be 'approve' or 'reject'")
		return
	}

	updated := 0
	for _, id := range req.IDs {
		if h.applyReviewActionToItem(c.Request.Context(), req.Action, id) {
			updated++
		}
	}

	writeSuccess(c, map[string]interface{}{
		"updated": updated,
		"total":   len(req.IDs),
	})
}

// ClearReviewQueue clears all items from the scanner review queue
func (h *Handler) ClearReviewQueue(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	h.scanner.ClearReviewQueue()
	writeSuccess(c, map[string]interface{}{
		"message": "Review queue cleared",
	})
}

// ApproveContent approves content from the review queue
func (h *Handler) ApproveContent(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	id := c.Param("id")
	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	if err := h.scanner.ApproveContent(c.Request.Context(), path); err != nil {
		writeError(c, http.StatusNotFound, "Item not found in review queue")
		return
	}

	confidence := 0.0
	var reasons []string
	if result, ok := h.scanner.GetScanResult(path); ok {
		confidence = result.Confidence
		reasons = result.Reasons
	}
	if err := h.media.SetMatureFlag(path, true, confidence, reasons); err != nil {
		h.log.Error("Failed to update media library mature flag: %v", err)
	}

	writeSuccess(c, nil)
}

// RejectContent rejects content from the review queue
func (h *Handler) RejectContent(c *gin.Context) {
	if !h.requireScanner(c) {
		return
	}
	id := c.Param("id")
	path, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	if err := h.scanner.RejectContent(c.Request.Context(), path); err != nil {
		writeError(c, http.StatusNotFound, "Item not found in review queue")
		return
	}

	if err := h.media.SetMatureFlag(path, false, 0, nil); err != nil {
		h.log.Error("Failed to update media library mature flag: %v", err)
	}

	writeSuccess(c, nil)
}
