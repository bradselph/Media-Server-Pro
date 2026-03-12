package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/categorizer"
)

// CategorizeFile categorizes a single file and propagates the result to the media module.
// TODO(feature-gap): The path is passed directly to the categorizer without validation. There is no
// check that the path is within allowed media directories (unlike ClassifyFile which uses
// resolvePathForAdmin). Implement path validation against allowed media dirs so admins cannot
// categorize arbitrary filesystem paths.
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
// TODO(feature-gap): Same path validation issue as CategorizeFile — the directory path is not validated
// against allowed media directories. Use resolvePathForAdmin or similar before calling categorizer.
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
// TODO(feature-gap): req.Path is not validated against allowed directories; same gap as CategorizeFile.
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

	h.categorizer.SetCategory(req.Path, req.Category)
	// Propagate the new category to the in-memory media catalog immediately so
	// ListMedia/GetMedia reflect the change without waiting for the next scan.
	if string(req.Category) != "" {
		if updateErr := h.media.UpdateMetadata(req.Path, map[string]interface{}{
			"category": string(req.Category),
		}); updateErr != nil {
			h.log.Warn("Categorizer: failed to update media metadata for %s: %v", req.Path, updateErr)
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
