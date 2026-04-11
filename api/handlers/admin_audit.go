package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminGetAuditLog returns audit log, optionally filtered by user_id query param.
func (h *Handler) AdminGetAuditLog(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	limit := ParseQueryInt(c, "limit", QueryIntOpts{Default: 100, Min: 1, Max: 1000})
	offset := ParseQueryInt(c, "offset", QueryIntOpts{Default: 0, Min: 0, Max: 100000})
	userID := strings.TrimSpace(c.Query("user_id"))

	log := h.admin.GetAuditLog(c.Request.Context(), limit, offset, userID)
	writeSuccess(c, log)
}

// AdminExportAuditLog exports the audit log as a CSV file download
func (h *Handler) AdminExportAuditLog(c *gin.Context) {
	if !h.requireAdminModule(c) {
		return
	}
	filename, err := h.admin.ExportAuditLog(c.Request.Context())
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	defer func() { _ = os.Remove(filename) }() // clean up temp export file after send

	c.Header(headerContentDisposition, safeContentDisposition(filepath.Base(filename)))
	c.Header(headerContentType, "text/csv")
	http.ServeFile(c.Writer, c.Request, filename)
}
