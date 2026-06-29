package handlers

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/repositories"
	repoMysql "media-server-pro/internal/repositories/mysql"
)

const errMediaReportsUnavailable = "Media report service unavailable"

// validReportReasons enumerates the moderation categories the SPA may
// submit. Anything else is normalized to "other".
var validReportReasons = map[string]struct{}{
	"inappropriate": {},
	"broken":        {},
	"spam":          {},
	"copyright":     {},
	"other":         {},
}

const (
	maxReportNotesLen  = 1000
	maxReportReasonLen = 64
)

// requireMediaReportRepo lazy-initializes the report repository on first
// call so that GORM() is not captured before the database module's Start()
// runs (which would leave r.db nil and cause a panic).
func (h *Handler) requireMediaReportRepo(c *gin.Context) bool {
	if h.mediaReports == nil {
		h.mediaReportsMu.Lock()
		if h.mediaReports == nil {
			db := h.database.GORM()
			if db == nil {
				h.mediaReportsMu.Unlock()
				writeError(c, http.StatusServiceUnavailable, errMediaReportsUnavailable)
				return false
			}
			h.mediaReports = repoMysql.NewMediaReportRepository(db)
		}
		h.mediaReportsMu.Unlock()
	}
	return true
}

// SubmitMediaReport accepts a moderation report on a single media item.
// Authenticated callers get their user ID attached; guests are recorded
// by IP only.
//
// POST /api/media/:id/report
// Body: { reason: "inappropriate"|"broken"|"spam"|"copyright"|"other", notes: string }
func (h *Handler) SubmitMediaReport(c *gin.Context) {
	if !h.requireMediaReportRepo(c) {
		return
	}
	mediaID, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	// Make sure the media item actually exists before persisting a report
	// against it — keeps the table clean and rejects scrapers spraying IDs.
	if _, err := h.media.GetMediaByID(mediaID); err != nil {
		writeError(c, http.StatusNotFound, "Media not found")
		return
	}

	var req struct {
		Reason string `json:"reason"`
		Notes  string `json:"notes"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	reason := strings.ToLower(strings.TrimSpace(req.Reason))
	if reason == "" {
		reason = "other"
	}
	if _, valid := validReportReasons[reason]; !valid {
		reason = "other"
	}
	if len(reason) > maxReportReasonLen {
		reason = reason[:maxReportReasonLen]
	}

	notes := strings.TrimSpace(req.Notes)
	// Truncate by rune count, not byte length: slicing at a byte offset can split
	// a multi-byte UTF-8 codepoint and store invalid UTF-8 in the notes column.
	if utf8.RuneCountInString(notes) > maxReportNotesLen {
		notes = string([]rune(notes)[:maxReportNotesLen])
	}

	reporterID := ""
	if s := getSession(c); s != nil {
		reporterID = s.UserID
	}

	rec := &repositories.MediaReportRecord{
		ID:         newReportID(),
		MediaID:    mediaID,
		ReporterID: reporterID,
		Reason:     reason,
		Notes:      notes,
		Status:     "open",
		IPAddress:  c.ClientIP(),
	}
	if err := h.mediaReports.Create(c.Request.Context(), rec); err != nil {
		h.log.Error("SubmitMediaReport: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to submit report")
		return
	}
	writeSuccess(c, map[string]string{
		"id":         rec.ID,
		"status":     rec.Status,
		"created_at": rec.CreatedAt.Format(timeFormatRFC3339Ext),
	})
}

// ListMediaReports returns paginated reports for the admin moderation UI.
// Admins can filter by status ("open"|"resolved"|"dismissed").
//
// GET /api/admin/media/reports?status=open&limit=50&offset=0
func (h *Handler) ListMediaReports(c *gin.Context) {
	if !h.requireMediaReportRepo(c) {
		return
	}
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.Query("offset"))
	if offset < 0 {
		offset = 0
	}
	recs, err := h.mediaReports.List(c.Request.Context(), status, limit, offset)
	if err != nil {
		h.log.Error("ListMediaReports: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to list reports")
		return
	}
	openCount, err := h.mediaReports.CountByStatus(c.Request.Context(), "open")
	if err != nil {
		h.log.Warn("Failed to count open media reports: %v", err)
	}
	out := make([]map[string]any, len(recs))
	for i, r := range recs {
		v := map[string]any{
			"id":          r.ID,
			"media_id":    r.MediaID,
			"reporter_id": r.ReporterID,
			"reason":      r.Reason,
			"notes":       r.Notes,
			"status":      r.Status,
			"created_at":  r.CreatedAt.Format(timeFormatRFC3339Ext),
			"ip_address":  r.IPAddress,
		}
		if r.ResolvedAt != nil {
			v["resolved_at"] = r.ResolvedAt.Format(timeFormatRFC3339Ext)
		}
		if r.ResolvedBy != "" {
			v["resolved_by"] = r.ResolvedBy
		}
		out[i] = v
	}
	writeSuccess(c, map[string]any{
		"reports":    out,
		"open_count": openCount,
	})
}

// UpdateMediaReportStatus marks a report as resolved or dismissed.
//
// PATCH /api/admin/media/reports/:id
// Body: { status: "resolved"|"dismissed"|"open" }
func (h *Handler) UpdateMediaReportStatus(c *gin.Context) {
	if !h.requireMediaReportRepo(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	switch status {
	case "open", "resolved", "dismissed":
	default:
		writeError(c, http.StatusBadRequest, "Invalid status")
		return
	}
	adminSession := getSession(c)
	resolvedBy := ""
	if adminSession != nil {
		resolvedBy = adminSession.Username
	}
	if err := h.mediaReports.UpdateStatus(c.Request.Context(), id, status, resolvedBy); err != nil {
		h.log.Error("UpdateMediaReportStatus: %v", err)
		writeError(c, http.StatusInternalServerError, "Failed to update report")
		return
	}
	writeSuccess(c, map[string]string{"id": id, "status": status})
}

func newReportID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		// Falls back to a zeroed ID — extremely unlikely under normal
		// operation; the DB primary-key constraint will reject duplicates
		// rather than silently overwrite.
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}
