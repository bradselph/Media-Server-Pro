package handlers

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/downloader"
	"media-server-pro/pkg/helpers"
)

func (h *Handler) checkDownloaderEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.downloader, "Downloader", func() bool {
		return h.config.Get().Features.EnableDownloader
	})
}

// AdminDownloaderHealth returns the downloader's online status.
// Always returns 200 so the frontend can show online/offline indicator.
func (h *Handler) AdminDownloaderHealth(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}

	online := h.downloader.IsOnline()
	result := map[string]interface{}{
		"online": online,
	}

	if online {
		health, err := h.downloader.GetClient().Health()
		if err != nil {
			result["online"] = false
			result["error"] = err.Error()
		} else {
			result["activeDownloads"] = health.ActiveDownloads
			result["queuedDownloads"] = health.QueuedDownloads
			result["uptime"] = health.Uptime
			// Frontend expects dependencies as Record<string, string> (name -> version)
			deps := make(map[string]string)
			if health.Dependencies.YtDlp != nil && health.Dependencies.YtDlp.Available {
				deps["yt-dlp"] = health.Dependencies.YtDlp.Version
			}
			if health.Dependencies.FFmpeg != nil && health.Dependencies.FFmpeg.Available {
				deps["ffmpeg"] = health.Dependencies.FFmpeg.Version
			}
			result["dependencies"] = deps
		}
	}

	writeSuccess(c, result)
}

// AdminDownloaderVerify is a lightweight endpoint that returns 200 if the
// caller has a valid admin session. The downloader calls this to verify
// admin identity before allowing server-side storage.
func (h *Handler) AdminDownloaderVerify(c *gin.Context) {
	// If we reach here, the request passed sessionAuth + adminAuth middleware
	writeSuccess(c, map[string]bool{"valid": true})
}

// AdminDownloaderDetect proxies stream detection to the downloader.
func (h *Handler) AdminDownloaderDetect(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, "Downloader service is offline")
		return
	}

	var req struct {
		URL string `json:"url" binding:"required"`
	}
	if !BindJSON(c, &req, "URL is required") {
		return
	}

	if err := helpers.ValidateURLForSSRF(req.URL); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid URL: "+err.Error())
		return
	}

	result, err := h.downloader.GetClient().Detect(req.URL)
	if err != nil {
		writeError(c, http.StatusBadGateway, "Detection failed: "+err.Error())
		return
	}

	// Map to frontend shape: url (page or stream), streams (allStreams or single stream)
	streams := result.AllStreams
	if len(streams) == 0 && result.Stream != nil {
		streams = []downloader.StreamInfo{*result.Stream}
	}
	pageURL := result.PageURL
	if pageURL == "" && result.Stream != nil {
		pageURL = result.Stream.URL
	}
	writeSuccess(c, map[string]interface{}{
		"url":          pageURL,
		"title":        result.Title,
		"isYouTube":    result.IsYouTube,
		"isYouTubeMusic": result.IsYouTubeMusic,
		"streams":      streams,
		"relayId":      result.RelayID,
	})
}

// AdminDownloaderDownload starts a download on the downloader service.
// Forces saveLocation to "server" and injects the admin's MSP session ID
// so the downloader can verify admin identity.
func (h *Handler) AdminDownloaderDownload(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, "Downloader service is offline")
		return
	}

	var req struct {
		URL            string `json:"url" binding:"required"`
		Title          string `json:"title"`
		ClientID       string `json:"clientId" binding:"required"`
		IsYouTube      bool   `json:"isYouTube"`
		IsYouTubeMusic bool   `json:"isYouTubeMusic"`
		RelayID        string `json:"relayId"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	if err := helpers.ValidateURLForSSRF(req.URL); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid URL: "+err.Error())
		return
	}

	// Extract admin's MSP session ID to forward to the downloader
	sessionID, _ := c.Cookie("session_id")

	params := downloader.DownloadParams{
		URL:            req.URL,
		Title:          req.Title,
		SaveLocation:   "server", // Always server — admin downloads are for import
		ClientID:       req.ClientID,
		IsYouTube:      req.IsYouTube,
		IsYouTubeMusic: req.IsYouTubeMusic,
		RelayID:        req.RelayID,
	}

	result, err := h.downloader.GetClient().Download(params, sessionID)
	if err != nil {
		writeError(c, http.StatusBadGateway, "Download failed: "+err.Error())
		return
	}

	writeSuccess(c, result)
}

// AdminDownloaderCancel cancels an active download.
func (h *Handler) AdminDownloaderCancel(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, "Downloader service is offline")
		return
	}

	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}

	if err := h.downloader.GetClient().CancelDownload(id); err != nil {
		writeError(c, http.StatusBadGateway, "Cancel failed: "+err.Error())
		return
	}

	writeSuccess(c, map[string]string{"status": "cancelled"})
}

// AdminDownloaderListDownloads returns completed downloads from the downloader.
func (h *Handler) AdminDownloaderListDownloads(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, "Downloader service is offline")
		return
	}

	resp, err := h.downloader.GetClient().ListDownloads()
	if err != nil {
		writeError(c, http.StatusBadGateway, "Failed to list downloads: "+err.Error())
		return
	}

	// Map to frontend shape: filename, size, created (unix), url
	files := make([]map[string]interface{}, 0, len(resp.Downloads))
	for _, f := range resp.Downloads {
		files = append(files, map[string]interface{}{
			"filename": f.File,
			"size":     f.Size,
			"created":  f.Timestamp,
			"url":      f.DownloadURL,
		})
	}
	writeSuccess(c, files)
}

// AdminDownloaderDeleteDownload removes a file from the downloader's storage.
func (h *Handler) AdminDownloaderDeleteDownload(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, "Downloader service is offline")
		return
	}

	filename, ok := RequireParamID(c, "filename")
	if !ok {
		return
	}
	// Sanitize to prevent path traversal — strip directory components
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." {
		writeError(c, http.StatusBadRequest, "Invalid filename")
		return
	}

	if err := h.downloader.GetClient().DeleteDownload(filename); err != nil {
		writeError(c, http.StatusBadGateway, "Delete failed: "+err.Error())
		return
	}

	writeSuccess(c, map[string]string{"status": "deleted"})
}

// AdminDownloaderSettings returns the downloader's current settings.
func (h *Handler) AdminDownloaderSettings(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, "Downloader service is offline")
		return
	}

	resp, err := h.downloader.GetClient().GetSettings()
	if err != nil {
		writeError(c, http.StatusBadGateway, "Failed to get settings: "+err.Error())
		return
	}

	sites := resp.SupportedSites
	if sites == nil {
		sites = []string{}
	}
	writeSuccess(c, map[string]interface{}{
		"allowServerStorage":     resp.AllowServerStorage,
		"audioFormat":            resp.AudioFormat,
		"supportedSites":         sites,
		"theme":                  resp.Theme,
		"browserRelayConfigured": resp.BrowserRelayConfigured,
		"downloadsDir":           h.config.Get().Downloader.DownloadsDir,
	})
}

// AdminDownloaderImportable lists files ready to import from the downloader.
func (h *Handler) AdminDownloaderImportable(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}

	files, err := h.downloader.ListImportable()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to list importable files: "+err.Error())
		return
	}

	writeSuccess(c, files)
}

// AdminDownloaderImport moves a completed download into MSP's media library.
func (h *Handler) AdminDownloaderImport(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}

	var req struct {
		Filename     string `json:"filename" binding:"required"`
		DeleteSource bool   `json:"delete_source"`
		TriggerScan  bool   `json:"trigger_scan"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	destPath, sourceDeleted, err := h.downloader.Import(req.Filename, req.DeleteSource, req.TriggerScan)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Import failed: "+err.Error())
		return
	}

	writeSuccess(c, map[string]interface{}{
		"source":        req.Filename,
		"destination":   destPath,
		"scanTriggered": req.TriggerScan,
		"sourceDeleted": sourceDeleted,
	})
}

// AdminDownloaderWebSocket upgrades to WebSocket and proxies to the downloader.
func (h *Handler) AdminDownloaderWebSocket(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	h.downloader.HandleWebSocket(c.Writer, c.Request)
}
