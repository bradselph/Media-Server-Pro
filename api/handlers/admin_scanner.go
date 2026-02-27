package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/scanner"
)

// ScanContent scans media files for mature content
func (h *Handler) ScanContent(c *gin.Context) {
	var req struct {
		Path         string `json:"path"`
		AutoApply    bool   `json:"auto_apply"`
		ScanMetadata bool   `json:"scan_metadata"`
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
	allResults := make([]*scanner.ScanResult, 0)

	if cfg.Directories.Videos != "" {
		results, err := h.scanner.ScanDirectory(cfg.Directories.Videos)
		if err != nil {
			h.log.Error("Failed to scan videos directory: %v", err)
		} else {
			allResults = append(allResults, results...)
		}
	}

	if cfg.Directories.Music != "" {
		results, err := h.scanner.ScanDirectory(cfg.Directories.Music)
		if err != nil {
			h.log.Error("Failed to scan music directory: %v", err)
		} else {
			allResults = append(allResults, results...)
		}
	}

	if cfg.Directories.Uploads != "" {
		results, err := h.scanner.ScanDirectory(cfg.Directories.Uploads)
		if err != nil {
			h.log.Error("Failed to scan uploads directory: %v", err)
		} else {
			allResults = append(allResults, results...)
		}
	}

	autoFlagged := 0
	reviewNeeded := 0
	clean := 0
	for _, result := range allResults {
		if result.AutoFlagged {
			autoFlagged++
		}
		if result.NeedsReview {
			reviewNeeded++
		}
		if !result.IsMature && !result.NeedsReview {
			clean++
		}

		if req.AutoApply && result.IsMature {
			if err := h.media.SetMatureFlag(result.Path, true, result.Confidence, result.Reasons); err != nil {
				h.log.Error("Failed to set mature flag for %s: %v", result.Path, err)
			}
		}
	}

	stats := h.scanner.GetStats()
	writeSuccess(c, map[string]interface{}{
		"stats":              stats,
		"scanned":            len(allResults),
		"auto_flagged_count": autoFlagged,
		"review_queue_count": reviewNeeded,
		"clean":              clean,
		"message":            fmt.Sprintf("Scanned %d files", len(allResults)),
	})
}

// GetScannerStats returns scanner statistics
func (h *Handler) GetScannerStats(c *gin.Context) {
	stats := h.scanner.GetStats()
	writeSuccess(c, stats)
}

// GetReviewQueue returns items pending review as a flat array
func (h *Handler) GetReviewQueue(c *gin.Context) {
	queue := h.scanner.GetReviewQueue()
	writeSuccess(c, queue)
}

// TODO(api-contract): RESPONSE MISMATCH — BatchReviewAction returns { updated: N, total: N }
// (lines 149-152) but frontend adminApi.batchReview() types the return as Promise<void>
// (web/frontend/src/api/endpoints.ts). Frontend callers cannot inspect the update count.
// Change frontend return type to Promise<{ updated: number; total: number }> to match.
// Frontend: web/frontend/src/api/endpoints.ts adminApi.batchReview().
//
// TODO(api-contract): ACTION VALUE MISMATCH — Backend validates action as "approve" or "reject"
// only (line 118). Frontend adminApi.batchReview() accepts any `action: string` with no type
// narrowing — sending any other value returns 400 Bad Request. Frontend type should be
// narrowed to 'approve' | 'reject'. Frontend: web/frontend/src/api/endpoints.ts adminApi.batchReview().
//
// BatchReviewAction applies approve/reject action to multiple review queue items
func (h *Handler) BatchReviewAction(c *gin.Context) {
	var req struct {
		Action string   `json:"action"`
		Paths  []string `json:"paths"`
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
	for _, path := range req.Paths {
		var err error
		if req.Action == "approve" {
			err = h.scanner.ApproveContent(c.Request.Context(), path)
			if err == nil {
				result, ok := h.scanner.GetScanResult(path)
				if ok {
					if setErr := h.media.SetMatureFlag(path, true, result.Confidence, result.Reasons); setErr != nil {
						h.log.Error("Failed to update media library mature flag for %s: %v", path, setErr)
					}
				}
			}
		} else {
			err = h.scanner.RejectContent(c.Request.Context(), path)
			if err == nil {
				if setErr := h.media.SetMatureFlag(path, false, 0, nil); setErr != nil {
					h.log.Error("Failed to update media library mature flag for %s: %v", path, setErr)
				}
			}
		}
		if err == nil {
			updated++
		}
	}

	writeSuccess(c, map[string]interface{}{
		"updated": updated,
		"total":   len(req.Paths),
	})
}

// ClearReviewQueue clears all items from the scanner review queue
func (h *Handler) ClearReviewQueue(c *gin.Context) {
	h.scanner.ClearReviewQueue()
	writeSuccess(c, map[string]interface{}{
		"message": "Review queue cleared",
	})
}

// ApproveContent approves content from the review queue
func (h *Handler) ApproveContent(c *gin.Context) {
	rawPath := strings.TrimPrefix(c.Param("path"), "/")
	path, _ := url.PathUnescape(rawPath)

	if err := h.scanner.ApproveContent(c.Request.Context(), path); err != nil {
		writeError(c, http.StatusNotFound, "Item not found in review queue")
		return
	}

	result, ok := h.scanner.GetScanResult(path)
	if ok {
		if err := h.media.SetMatureFlag(path, true, result.Confidence, result.Reasons); err != nil {
			h.log.Error("Failed to update media library mature flag: %v", err)
		}
	}

	writeSuccess(c, nil)
}

// RejectContent rejects content from the review queue
func (h *Handler) RejectContent(c *gin.Context) {
	rawPath := strings.TrimPrefix(c.Param("path"), "/")
	path, _ := url.PathUnescape(rawPath)

	if err := h.scanner.RejectContent(c.Request.Context(), path); err != nil {
		writeError(c, http.StatusNotFound, "Item not found in review queue")
		return
	}

	if err := h.media.SetMatureFlag(path, false, 0, nil); err != nil {
		h.log.Error("Failed to update media library mature flag: %v", err)
	}

	writeSuccess(c, nil)
}

// ensure json import is used (ScanContent uses it via ShouldBindJSON fallback)
var _ = json.Marshal
