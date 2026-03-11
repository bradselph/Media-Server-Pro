package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/duplicates"
)

// AdminListDuplicates returns detected duplicate media pairs for admin review.
// GET /api/admin/duplicates?status=pending   (default: pending only)
// GET /api/admin/duplicates?status=all       (all records)
func (h *Handler) AdminListDuplicates(c *gin.Context) {
	if !h.checkDuplicateDetectionEnabled(c) {
		return
	}

	statusFilter := c.DefaultQuery("status", "pending")
	groups, err := h.duplicates.ListDuplicates(statusFilter)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to list duplicates: "+err.Error())
		return
	}

	writeSuccess(c, groups)
}

// AdminResolveDuplicate handles an admin action on a detected duplicate pair.
// POST /api/admin/duplicates/:id/resolve
// Body: { "action": "remove_a" | "remove_b" | "keep_both" | "ignore" }
func (h *Handler) AdminResolveDuplicate(c *gin.Context) {
	if !h.checkDuplicateDetectionEnabled(c) {
		return
	}

	id := c.Param("id")
	if id == "" {
		writeError(c, http.StatusBadRequest, "duplicate ID required")
		return
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Action == "" {
		writeError(c, http.StatusBadRequest, "action required (remove_a, remove_b, keep_both, ignore)")
		return
	}

	session := getSession(c)
	userID := ""
	resolvedBy := ""
	if session != nil {
		userID = session.UserID
		resolvedBy = session.Username
	}

	if err := h.duplicates.ResolveDuplicate(duplicates.ResolveDuplicateInput{
		ID:         id,
		Action:     body.Action,
		ResolvedBy: resolvedBy,
	}); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	h.logAdminAction(c, &adminLogActionParams{
		UserID: userID, Username: resolvedBy, Action: "resolve_duplicate",
		Target: id, Details: map[string]interface{}{"action": body.Action},
	})

	writeSuccess(c, gin.H{"message": "duplicate resolved", "action": body.Action})
}
