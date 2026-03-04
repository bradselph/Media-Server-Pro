package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/categorizer"
)

// CategorizeFile categorizes a single file and propagates the result to the media module.
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

	result := h.categorizer.CategorizeFile(req.Path)
	if result != nil && string(result.Category) != "" {
		if err := h.media.UpdateMetadata(req.Path, map[string]interface{}{
			"category": string(result.Category),
		}); err != nil {
			h.log.Warn("Categorizer: failed to update media metadata for %s: %v", req.Path, err)
		}
	}
	writeSuccess(c, result)
}

// CategorizeDirectory categorizes all files in a directory.
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

	results, err := h.categorizer.CategorizeDirectory(req.Directory)
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

// SetMediaCategory manually sets a category for a file
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

	// TODO: SetCategory only updates the categorizer's in-memory store and MySQL categorized_items
	// table. It does NOT call h.media.UpdateMetadata to propagate the new category to MediaItem.Category
	// in the in-memory media catalog. This creates a stale-data inconsistency: the category set here
	// will not appear in ListMedia or GetMedia responses until the next full media scan.
	// Fix: add h.media.UpdateMetadata(req.Path, map[string]interface{}{"category": string(req.Category)})
	// after h.categorizer.SetCategory(), as CategorizeFile (above) already does correctly.
	h.categorizer.SetCategory(req.Path, req.Category)
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
