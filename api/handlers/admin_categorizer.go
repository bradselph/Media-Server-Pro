package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/categorizer"
)

const (
	fmtCategorizerUpdateFailed = "Categorizer: failed to update media metadata for %s: %v"
)

// CategorizeFile categorizes a single file and propagates the result to the media module.
// Path must be under allowed media directories (validated via resolvePathForAdmin).
func (h *Handler) CategorizeFile(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	absPath, ok := h.resolvePathForAdmin(c, req.Path, false)
	if !ok {
		return
	}

	result, saveErr := h.categorizer.CategorizeFile(absPath)
	if saveErr != nil {
		h.log.Error("CategorizeFile: DB persist failed for %s: %v", absPath, saveErr)
		writeError(c, http.StatusInternalServerError, "Categorization failed to persist")
		return
	}
	if result != nil && string(result.Category) != "" {
		if err := h.media.UpdateMetadata(absPath, map[string]any{
			"category": string(result.Category),
		}); err != nil {
			h.log.Warn(fmtCategorizerUpdateFailed, absPath, err)
		}
	}
	writeSuccess(c, result)
}

// CategorizeDirectory categorizes all files in a directory.
// Directory must be under allowed media directories (validated via resolvePathForAdmin).
func (h *Handler) CategorizeDirectory(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	var req struct {
		Directory string `json:"directory"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	absDir, ok := h.resolvePathForAdmin(c, req.Directory, true)
	if !ok {
		return
	}

	results, err := h.categorizer.CategorizeDirectory(absDir)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	for _, item := range results {
		if item != nil && string(item.Category) != "" {
			if updateErr := h.media.UpdateMetadata(item.Path, map[string]any{
				"category": string(item.Category),
			}); updateErr != nil {
				h.log.Warn(fmtCategorizerUpdateFailed, item.Path, updateErr)
			}
		}
	}

	h.trackServerEvent(c, analytics.EventCategorizerRun, map[string]any{
		"directory": absDir,
		"count":     len(results),
	})
	writeSuccess(c, results)
}

// GetCategoryStats returns categorization statistics
func (h *Handler) GetCategoryStats(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	stats := h.categorizer.GetStats()
	writeSuccess(c, stats)
}

// SetMediaCategory manually sets a category for a file.
// Path must be under allowed media directories (validated via resolvePathForAdmin).
func (h *Handler) SetMediaCategory(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	var req struct {
		Path     string               `json:"path"`
		Category categorizer.Category `json:"category"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	absPath, ok := h.resolvePathForAdmin(c, req.Path, false)
	if !ok {
		return
	}

	if err := h.categorizer.SetCategory(absPath, req.Category); err != nil {
		h.log.Warn("SetCategory: DB persist failed for %s: %v", absPath, err)
	}
	// Propagate the new category to the in-memory media catalog immediately so
	// ListMedia/GetMedia reflect the change without waiting for the next scan.
	if string(req.Category) != "" {
		if updateErr := h.media.UpdateMetadata(absPath, map[string]any{
			"category": string(req.Category),
		}); updateErr != nil {
			h.log.Warn(fmtCategorizerUpdateFailed, absPath, updateErr)
		}
	}
	h.trackServerEvent(c, analytics.EventCategorizerRun, map[string]any{
		"scope":    "single",
		"path":     absPath,
		"category": string(req.Category),
	})
	writeSuccess(c, map[string]string{"message": "Category set"})
}

// GetByCategory returns all items in a category
func (h *Handler) GetByCategory(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	category := categorizer.Category(c.Query("category"))
	items := h.categorizer.GetByCategory(category)
	writeSuccess(c, items)
}

// CleanStaleCategories removes entries for deleted files
func (h *Handler) CleanStaleCategories(c *gin.Context) {
	if !h.requireCategorizer(c) {
		return
	}
	removed := h.categorizer.CleanStale()
	h.trackServerEvent(c, analytics.EventCategorizerRun, map[string]any{
		"scope":   "clean_stale",
		"removed": removed,
	})
	writeSuccess(c, map[string]int{"removed": removed})
}
