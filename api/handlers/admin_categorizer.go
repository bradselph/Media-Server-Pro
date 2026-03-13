package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/categorizer"
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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	absPath, ok := h.resolvePathForAdmin(c, req.Path, false)
	if !ok {
		return
	}

	result := h.categorizer.CategorizeFile(absPath)
	if result != nil && string(result.Category) != "" {
		if err := h.media.UpdateMetadata(absPath, map[string]interface{}{
			"category": string(result.Category),
		}); err != nil {
			h.log.Warn("Categorizer: failed to update media metadata for %s: %v", absPath, err)
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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	absDir, ok := h.resolvePathForAdmin(c, req.Directory, true)
	if !ok {
		return
	}

	results, err := h.categorizer.CategorizeDirectory(absDir)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	for _, item := range results {
		if item != nil && string(item.Category) != "" {
			if updateErr := h.media.UpdateMetadata(item.Path, map[string]interface{}{
				"category": string(item.Category),
			}); updateErr != nil {
				h.log.Warn("Categorizer: failed to update media metadata for %s: %v", item.Path, updateErr)
			}
		}
	}

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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	absPath, ok := h.resolvePathForAdmin(c, req.Path, false)
	if !ok {
		return
	}

	h.categorizer.SetCategory(absPath, req.Category)
	// Propagate the new category to the in-memory media catalog immediately so
	// ListMedia/GetMedia reflect the change without waiting for the next scan.
	if string(req.Category) != "" {
		if updateErr := h.media.UpdateMetadata(absPath, map[string]interface{}{
			"category": string(req.Category),
		}); updateErr != nil {
			h.log.Warn("Categorizer: failed to update media metadata for %s: %v", absPath, updateErr)
		}
	}
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
	writeSuccess(c, map[string]int{"removed": removed})
}
