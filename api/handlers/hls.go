package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/hls"
	"media-server-pro/pkg/models"
)

const (
	fmtHLSMasterURL = "/hls/%s/master.m3u8"
)

// GetHLSCapabilities returns whether HLS transcoding is available and its configuration.
func (h *Handler) GetHLSCapabilities(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	caps := h.hls.GetCapabilities()
	writeSuccess(c, caps)
}

// CheckHLSAvailability checks HLS status by media ID and auto-generates if configured
func (h *Handler) CheckHLSAvailability(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	id := c.Query("id")
	absPath, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}

	job, err := h.hls.CheckOrGenerateHLS(c.Request.Context(), &hls.CheckOrGenerateHLSParams{MediaPath: absPath, MediaID: id})
	if err != nil {
		h.log.Debug("HLS check/generate failed for media %s: %v", id, err)
		writeError(c, http.StatusNotFound, "HLS stream not available")
		return
	}

	checkQualities := job.Qualities
	if checkQualities == nil {
		checkQualities = []string{}
	}
	response := map[string]interface{}{
		"id":         job.ID,
		"job_id":     job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  checkQualities,
		"started_at": job.StartedAt,
		"error":      job.Error,
	}

	if job.CompletedAt != nil {
		response["completed_at"] = job.CompletedAt
	}

	if job.Status == models.HLSStatusCompleted {
		response["available"] = true
		response["hls_url"] = fmt.Sprintf(fmtHLSMasterURL, job.ID)
	} else {
		response["available"] = false
		response["hls_url"] = ""
	}

	writeSuccess(c, response)
}

// generateHLSRequest is the request body for GenerateHLS.
type generateHLSRequest struct {
	ID        string   `json:"id"`
	Qualities []string `json:"qualities"`
	Quality   string   `json:"quality"`
}

// parseGenerateHLSRequest decodes the request body and returns media ID and qualities. Returns ok=false after writing an error.
func (h *Handler) parseGenerateHLSRequest(c *gin.Context) (id string, qualities []string, ok bool) {
	var req generateHLSRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		h.log.Debug("Invalid JSON in GenerateHLS request: %v", err)
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return "", nil, false
	}
	if len(req.Qualities) == 0 && req.Quality != "" {
		req.Qualities = []string{req.Quality}
	}
	return req.ID, req.Qualities, true
}

// buildGenerateHLSResponse builds the JSON response map for a GenerateHLS job (fail_count always present).
func buildGenerateHLSResponse(job *models.HLSJob) map[string]interface{} {
	qualities := job.Qualities
	if qualities == nil {
		qualities = []string{}
	}
	resp := map[string]interface{}{
		"job_id":     job.ID,
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  qualities,
		"error":      job.Error,
		"fail_count": job.FailCount,
		"started_at": job.StartedAt,
		"available":  job.Status == models.HLSStatusCompleted,
		"hls_url":    "",
	}
	if job.Status == models.HLSStatusCompleted {
		resp["hls_url"] = fmt.Sprintf(fmtHLSMasterURL, job.ID)
	}
	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt
	}
	return resp
}

// GenerateHLS starts HLS generation for a media file
func (h *Handler) GenerateHLS(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	id, qualities, ok := h.parseGenerateHLSRequest(c)
	if !ok {
		return
	}
	absPath, ok := h.resolveMediaByID(c, id)
	if !ok {
		return
	}
	job, err := h.hls.GenerateHLS(c.Request.Context(), &hls.GenerateHLSParams{MediaPath: absPath, MediaID: id, Qualities: qualities})
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeSuccess(c, buildGenerateHLSResponse(job))
}

// GetHLSStatus returns HLS generation status by job ID.
func (h *Handler) GetHLSStatus(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")

	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(c, http.StatusNotFound, "Job not found")
		return
	}

	statusQualities := job.Qualities
	if statusQualities == nil {
		statusQualities = []string{}
	}
	response := map[string]interface{}{
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  statusQualities,
		"started_at": job.StartedAt,
		"error":      job.Error,
		"fail_count": job.FailCount,
	}

	if job.CompletedAt != nil {
		response["completed_at"] = job.CompletedAt
	}

	if job.Status == models.HLSStatusCompleted {
		response["available"] = true
		response["hls_url"] = fmt.Sprintf(fmtHLSMasterURL, job.ID)
	} else {
		response["available"] = false
		response["hls_url"] = ""
	}

	writeSuccess(c, response)
}

// resolveHLSJobForServe loads an HLS job by ID, checks mature access, and records access. On success returns (job, true). On failure writes the error response and returns (nil, false).
func (h *Handler) resolveHLSJobForServe(c *gin.Context, jobID string) (*models.HLSJob, bool) {
	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(c, http.StatusNotFound, "HLS job not found")
		return nil, false
	}
	if !h.checkMatureAccess(c, job.MediaPath) {
		return nil, false
	}
	h.hls.RecordAccess(jobID)
	return job, true
}

// withResolvedHLSJob resolves the HLS job then runs serveFn. Errors are written to c.
func (h *Handler) withResolvedHLSJob(c *gin.Context, jobID, notFoundMsg string, serveFn func() error) {
	if _, ok := h.resolveHLSJobForServe(c, jobID); !ok {
		return
	}
	if err := serveFn(); err != nil {
		writeError(c, http.StatusNotFound, notFoundMsg)
	}
}

// ServeMasterPlaylist serves the HLS master playlist
func (h *Handler) ServeMasterPlaylist(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")
	h.withResolvedHLSJob(c, jobID, "HLS playlist not found", func() error {
		return h.hls.ServeMasterPlaylist(c.Writer, c.Request, jobID)
	})
}

// ServeVariantPlaylist serves an HLS variant playlist
func (h *Handler) ServeVariantPlaylist(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")
	quality := c.Param("quality")
	h.withResolvedHLSJob(c, jobID, "HLS variant playlist not found", func() error {
		return h.hls.ServeVariantPlaylist(c.Writer, c.Request, hls.VariantPlaylistParams{JobID: jobID, Quality: quality})
	})
}

// ServeSegment serves an HLS segment
func (h *Handler) ServeSegment(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")
	quality := c.Param("quality")
	segment := c.Param("segment")
	h.withResolvedHLSJob(c, jobID, "HLS segment not found", func() error {
		return h.hls.ServeSegment(c.Writer, c.Request, hls.SegmentParams{JobID: jobID, Quality: quality, Segment: segment})
	})
}

// GetHLSStats returns HLS module statistics
func (h *Handler) GetHLSStats(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	stats := h.hls.GetStats()
	writeSuccess(c, stats)
}

// ValidateHLS validates an HLS job's playlists and segments
func (h *Handler) ValidateHLS(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")

	result, err := h.hls.ValidateMasterPlaylist(jobID)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}

	writeSuccess(c, result)
}

// CleanHLSStaleLocks removes stale HLS generation locks
func (h *Handler) CleanHLSStaleLocks(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	removed := h.hls.CleanStaleLocks()
	writeSuccess(c, map[string]int{"removed": removed})
}

// cleanHLSThresholdHoursFromBody returns hours from JSON body (max_age_hours or threshold_hours); 0 if missing or invalid.
func cleanHLSThresholdHoursFromBody(c *gin.Context) int {
	var body struct {
		MaxAgeHours    int `json:"max_age_hours"`
		ThresholdHours int `json:"threshold_hours"`
	}
	if json.NewDecoder(c.Request.Body).Decode(&body) != nil {
		return 0
	}
	if body.MaxAgeHours > 0 {
		return body.MaxAgeHours
	}
	if body.ThresholdHours > 0 {
		return body.ThresholdHours
	}
	return 0
}

// cleanHLSThresholdHoursFromQuery returns hours from query "threshold_hours"; 0 if missing or invalid.
func cleanHLSThresholdHoursFromQuery(c *gin.Context) int {
	s := c.Query("threshold_hours")
	if s == "" {
		return 0
	}
	h, err := strconv.Atoi(s)
	if err != nil || h <= 0 {
		return 0
	}
	return h
}

// parseCleanHLSInactiveThreshold reads threshold from JSON body or query; default 24h.
func parseCleanHLSInactiveThreshold(c *gin.Context) time.Duration {
	hours := cleanHLSThresholdHoursFromBody(c)
	if hours == 0 {
		hours = cleanHLSThresholdHoursFromQuery(c)
	}
	if hours > 0 {
		return time.Duration(hours) * time.Hour
	}
	return 24 * time.Hour
}

// CleanHLSInactive removes inactive HLS content
func (h *Handler) CleanHLSInactive(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	threshold := parseCleanHLSInactiveThreshold(c)
	removed := h.hls.CleanInactiveJobs(threshold)
	writeSuccess(c, map[string]interface{}{
		"removed":   removed,
		"threshold": threshold.String(),
	})
}

// ListHLSJobs returns all HLS jobs
func (h *Handler) ListHLSJobs(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobs := h.hls.ListJobs()
	for _, j := range jobs {
		if j != nil && j.Qualities == nil {
			j.Qualities = []string{}
		}
	}
	writeSuccess(c, jobs)
}

// DeleteHLSJob removes an HLS job and its files
func (h *Handler) DeleteHLSJob(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")

	if err := h.hls.DeleteJob(jobID); err != nil {
		writeError(c, http.StatusNotFound, "HLS job not found")
		return
	}

	writeSuccess(c, map[string]string{"deleted": jobID})
}
