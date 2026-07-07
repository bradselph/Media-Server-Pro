package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
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

	if item, err := h.media.GetMedia(absPath); err == nil && item != nil {
		if !h.checkMatureAccess(c, item.IsMature) {
			return
		}
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
	response := map[string]any{
		"id":         job.ID,
		"job_id":     job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  checkQualities,
		"started_at": job.StartedAt,
		"error":      job.Error,
	}

	applyHLSCompletionFields(response, job)

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
func buildGenerateHLSResponse(job *models.HLSJob) map[string]any {
	qualities := job.Qualities
	if qualities == nil {
		qualities = []string{}
	}
	resp := map[string]any{
		"job_id":     job.ID,
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  qualities,
		"error":      job.Error,
		"fail_count": job.FailCount,
		"started_at": job.StartedAt,
	}
	applyHLSCompletionFields(resp, job)
	return resp
}

// applyHLSCompletionFields stamps the shared completion-related fields onto an
// HLS job response map: availability, the master playlist URL, and (when the
// job has finished) the completion timestamp.
func applyHLSCompletionFields(resp map[string]any, job *models.HLSJob) {
	resp["available"] = job.Status == models.HLSStatusCompleted
	if job.Status == models.HLSStatusCompleted {
		resp["hls_url"] = fmt.Sprintf(fmtHLSMasterURL, job.ID)
	} else {
		resp["hls_url"] = ""
	}
	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt
	}
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
	// Gate mature content before kicking off transcoding, mirroring
	// CheckHLSAvailability. Without this a user lacking CanViewMature/ShowMature
	// could POST a known mature media ID and start real ffmpeg jobs (and poll
	// their status) for content GetMedia would 403.
	if item, err := h.media.GetMedia(absPath); err == nil && item != nil {
		if !h.checkMatureAccess(c, item.IsMature) {
			return
		}
	}
	job, err := h.hls.GenerateHLS(c.Request.Context(), &hls.GenerateHLSParams{MediaPath: absPath, MediaID: id, Qualities: qualities})
	if err != nil {
		h.log.Error("%v", err)
		// Track HLS request errors so dashboards surface a transcoder problem
		// before users start complaining.
		h.trackServerEvent(c, "hls_error", map[string]any{
			"media_id": id,
			"error":    err.Error(),
		})
		writeError(c, http.StatusInternalServerError, errInternalServer)
		return
	}
	h.trackServerEvent(c, "hls_start", map[string]any{
		"media_id":  id,
		"job_id":    job.ID,
		"qualities": qualities,
	})
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
	// Apply the same mature gate as the serve/availability paths: the job ID is
	// the media's stable UUID, so status (progress/qualities/error/completion)
	// must not leak for a mature item to a caller who can't view it.
	if !h.checkMatureAccess(c, h.hlsJobIsMature(job)) {
		return
	}

	statusQualities := job.Qualities
	if statusQualities == nil {
		statusQualities = []string{}
	}
	response := map[string]any{
		"id":         job.ID,
		"status":     job.Status,
		"progress":   job.Progress,
		"qualities":  statusQualities,
		"started_at": job.StartedAt,
		"error":      job.Error,
		"fail_count": job.FailCount,
	}

	applyHLSCompletionFields(response, job)

	writeSuccess(c, response)
}

// hlsJobIsMature resolves whether an HLS job's source media is mature. It keys
// the lookup on the job's stable ID (job.ID == the media's StableID, by design
// so HLS cache survives moves/renames) rather than job.MediaPath, which is a
// snapshot taken at job-creation time and is never refreshed when the catalog
// re-keys on RenameMedia/MoveMedia — a path lookup would spuriously miss after
// any rename. A completed job whose source is genuinely no longer indexed (the
// file was deleted/rescanned away) fails closed: it was gate-eligible at gen
// time, so verified users still pass checkMatureAccess while anonymous/unverified
// callers are blocked. A pending/running job with no index entry yet (startup
// scan race) is treated as non-mature so an in-progress, not-yet-flagged item
// isn't gated.
func (h *Handler) hlsJobIsMature(job *models.HLSJob) bool {
	if item, err := h.media.GetMediaByID(job.ID); err == nil && item != nil {
		return item.IsMature
	}
	return job.Status == models.HLSStatusCompleted
}

// resolveHLSJobForServe loads an HLS job by ID, checks mature access, and records access. On success returns (job, true). On failure writes the error response and returns (nil, false).
func (h *Handler) resolveHLSJobForServe(c *gin.Context, jobID string) (*models.HLSJob, bool) {
	job, err := h.hls.GetJobStatus(jobID)
	if err != nil {
		writeError(c, http.StatusNotFound, "HLS job not found")
		return nil, false
	}
	// Honour Streaming.RequireAuth here too — the HLS segment routes carry no auth
	// middleware, so without this an anonymous holder of a job UUID could stream
	// the full ladder while StreamMedia/DownloadMedia reject them (media.go).
	if getSession(c) == nil && h.config.Get().Streaming.RequireAuth {
		writeError(c, http.StatusUnauthorized, "Authentication required to stream media")
		return nil, false
	}
	if !h.checkMatureAccess(c, h.hlsJobIsMature(job)) {
		return nil, false
	}
	// Only refresh the access timestamp for jobs that are still usable.
	// Terminal failure states (Failed, Canceled) must NOT be kept alive by
	// access timestamps — CleanInactiveJobs uses LastAccess as the sole gate
	// for removal, so recording access on a terminal-failure job would prevent
	// it from ever being cleaned up. The allowlist below is intentional: any
	// new HLSStatus constant should be considered explicitly.
	switch job.Status {
	case models.HLSStatusRunning, models.HLSStatusPending, models.HLSStatusCompleted:
		h.hls.RecordAccess(jobID)
	case models.HLSStatusFailed, models.HLSStatusCanceled:
		// terminal failure — do not extend lifetime
	}
	return job, true
}

// withResolvedHLSJob resolves the HLS job then runs serveFn. Errors are written to c.
func (h *Handler) withResolvedHLSJob(c *gin.Context, jobID, notFoundMsg string, serveFn func() error) {
	if _, ok := h.resolveHLSJobForServe(c, jobID); !ok {
		return
	}
	if err := serveFn(); err != nil {
		if c.Writer.Written() {
			// Headers/body already flushed; cannot change status code.
			return
		}
		switch {
		case errors.Is(err, hls.ErrNotReady):
			writeError(c, http.StatusServiceUnavailable, "HLS transcoding in progress, retry shortly")
		case errors.Is(err, os.ErrNotExist):
			writeError(c, http.StatusNotFound, notFoundMsg)
		default:
			h.log.Error("HLS serve error: %v", err)
			writeError(c, http.StatusInternalServerError, "HLS serve failed")
		}
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
	writeSuccess(c, map[string]any{
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
