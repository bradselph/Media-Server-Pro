package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/pkg/models"
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

	job, err := h.hls.CheckOrGenerateHLS(c.Request.Context(), absPath)
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
		response["hls_url"] = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	} else {
		response["available"] = false
		response["hls_url"] = ""
	}

	writeSuccess(c, response)
}

// GenerateHLS starts HLS generation for a media file
func (h *Handler) GenerateHLS(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	var req struct {
		ID        string   `json:"id"`
		Qualities []string `json:"qualities"`
		Quality   string   `json:"quality"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		h.log.Debug("Invalid JSON in GenerateHLS request: %v", err)
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if len(req.Qualities) == 0 && req.Quality != "" {
		req.Qualities = []string{req.Quality}
	}

	absPath, ok := h.resolveMediaByID(c, req.ID)
	if !ok {
		return
	}

	job, err := h.hls.GenerateHLS(c.Request.Context(), absPath, req.Qualities)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	jobQualities := job.Qualities
	if jobQualities == nil {
		jobQualities = []string{}
	}
	resp := map[string]interface{}{
		"job_id":     job.ID,
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  jobQualities,
		"error":      job.Error,
		"fail_count": job.FailCount,
		"started_at": job.StartedAt,
		"available":  job.Status == models.HLSStatusCompleted,
		"hls_url":    "",
	}
	if job.Status == models.HLSStatusCompleted {
		resp["hls_url"] = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	}
	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt
	}
	writeSuccess(c, resp)
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
		response["hls_url"] = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	} else {
		response["available"] = false
		response["hls_url"] = ""
	}

	writeSuccess(c, response)
}

// ServeMasterPlaylist serves the HLS master playlist
func (h *Handler) ServeMasterPlaylist(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")

	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(c, http.StatusNotFound, "HLS job not found")
		return
	}

	if !h.checkMatureAccess(c, job.MediaPath) {
		return
	}

	h.hls.RecordAccess(jobID)

	if err := h.hls.ServeMasterPlaylist(c.Writer, c.Request, jobID); err != nil {
		writeError(c, http.StatusNotFound, "HLS playlist not found")
		return
	}
}

// ServeVariantPlaylist serves an HLS variant playlist
func (h *Handler) ServeVariantPlaylist(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")
	quality := c.Param("quality")

	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(c, http.StatusNotFound, "HLS job not found")
		return
	}

	if !h.checkMatureAccess(c, job.MediaPath) {
		return
	}

	h.hls.RecordAccess(jobID)

	if err := h.hls.ServeVariantPlaylist(c.Writer, c.Request, jobID, quality); err != nil {
		writeError(c, http.StatusNotFound, "HLS variant playlist not found")
		return
	}
}

// ServeSegment serves an HLS segment
func (h *Handler) ServeSegment(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	jobID := c.Param("id")
	quality := c.Param("quality")
	segment := c.Param("segment")

	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(c, http.StatusNotFound, "HLS job not found")
		return
	}

	if !h.checkMatureAccess(c, job.MediaPath) {
		return
	}

	h.hls.RecordAccess(jobID)

	if err := h.hls.ServeSegment(c.Writer, c.Request, jobID, quality, segment); err != nil {
		writeError(c, http.StatusNotFound, "HLS segment not found")
		return
	}
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
		writeError(c, http.StatusInternalServerError, "Internal server error")
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

// CleanHLSInactive removes inactive HLS content
func (h *Handler) CleanHLSInactive(c *gin.Context) {
	if !h.requireHLS(c) {
		return
	}
	threshold := 24 * time.Hour

	var bodyReq struct {
		MaxAgeHours    int `json:"max_age_hours"`
		ThresholdHours int `json:"threshold_hours"`
	}
	if json.NewDecoder(c.Request.Body).Decode(&bodyReq) == nil {
		if bodyReq.MaxAgeHours > 0 {
			threshold = time.Duration(bodyReq.MaxAgeHours) * time.Hour
		} else if bodyReq.ThresholdHours > 0 {
			threshold = time.Duration(bodyReq.ThresholdHours) * time.Hour
		}
	} else if thresholdStr := c.Query("threshold_hours"); thresholdStr != "" {
		if hours, err := strconv.Atoi(thresholdStr); err == nil && hours > 0 {
			threshold = time.Duration(hours) * time.Hour
		}
	}

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
