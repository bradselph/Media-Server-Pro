package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// parseAuditLimit returns a clamped limit from query string, or defaultVal if invalid.
func parseAuditLimit(q string, defaultVal, max int) int {
	l, err := strconv.Atoi(q)
	if err != nil || l <= 0 || l > max {
		return defaultVal
	}
	return l
}

// parseAuditOffset returns a non-negative offset from query string, or defaultVal if invalid.
func parseAuditOffset(q string, defaultVal int) int {
	o, err := strconv.Atoi(q)
	if err != nil || o < 0 {
		return defaultVal
	}
	return o
}

// AdminGetAuditLog returns audit log, optionally filtered by user_id query param.
func (h *Handler) AdminGetAuditLog(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	limit := parseAuditLimit(c.Query("limit"), 100, 1000)
	offset := parseAuditOffset(c.Query("offset"), 0)
	userID := strings.TrimSpace(c.Query("user_id"))

	log := h.admin.GetAuditLog(c.Request.Context(), limit, offset, userID)
	writeSuccess(c, log)
}

// AdminExportAuditLog exports the audit log as a CSV file download
func (h *Handler) AdminExportAuditLog(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	filename, err := h.admin.ExportAuditLog(c.Request.Context())
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	// TODO: The Content-Disposition header uses fmt.Sprintf with %q (Go-style quoting) which
	// adds backslash escapes for special characters. HTTP Content-Disposition expects RFC 6266
	// quoting. Should use safeContentDisposition() instead (defined in response.go), which
	// properly sanitizes the filename. The analytics export (AdminExportAnalytics) uses
	// safeContentDisposition correctly, but this handler does not.
	c.Header(headerContentDisposition, fmt.Sprintf("attachment; filename=%q", filepath.Base(filename)))
	c.Header(headerContentType, "text/csv")
	http.ServeFile(c.Writer, c.Request, filename)
}
