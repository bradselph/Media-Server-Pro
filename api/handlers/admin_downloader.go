package handlers

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/downloader"
	"media-server-pro/pkg/helpers"
)

// downloaderUserErrors are configuration/usage problems the admin can fix, so
// the handler maps them to 400 (Bad Request) rather than 500 — a 500 reads as
// "the server broke" and sends the admin chasing the wrong thing.
var downloaderUserErrors = []error{
	downloader.ErrDownloadsDirNotConfigured,
	downloader.ErrNoImportDestination,
	downloader.ErrDestinationReadOnly,
	downloader.ErrUnknownDestination,
	downloader.ErrInvalidSubfolder,
}

func isDownloaderUserError(err error) bool {
	return slices.ContainsFunc(downloaderUserErrors, func(target error) bool {
		return errors.Is(err, target)
	})
}

const (
	msgDownloaderOffline = "Downloader service is offline"
	// maxDownloaderURLLength caps detect/download request URLs so a forged
	// caller can't push the SSRF validator and DNS lookup through huge inputs.
	// 2048 covers every browser-emitted URL with substantial headroom.
	maxDownloaderURLLength = 2048
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
	result := map[string]any{
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
			result["anySiteForAdmin"] = health.AnySiteForAdmin
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
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
		return
	}

	var req struct {
		URL string `json:"url" binding:"required"`
	}
	if !BindJSON(c, &req, "URL is required") {
		return
	}

	if len(req.URL) > maxDownloaderURLLength {
		writeError(c, http.StatusBadRequest, "URL is too long")
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

	// Map to frontend shape: url (original page URL for best-quality downloads),
	// streams (all detected streams for user to pick from)
	streams := result.AllStreams
	if len(streams) == 0 && result.Stream != nil {
		streams = []downloader.StreamInfo{*result.Stream}
	}
	writeSuccess(c, map[string]any{
		"url":            req.URL,
		"title":          result.Title,
		"isYouTube":      result.IsYouTube,
		"isYouTubeMusic": result.IsYouTubeMusic,
		"streams":        streams,
		"relayId":        result.RelayID,
		"engine":         result.Engine,        // v1.5.0: "ytdlp" | "stream"
		"adminUnlocked":  result.AdminUnlocked, // v1.5.0: any-site download unlocked for this admin
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
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
		return
	}

	var req struct {
		URL            string `json:"url" binding:"required"`
		Title          string `json:"title"`
		ClientID       string `json:"clientId" binding:"required"`
		IsYouTube      bool   `json:"isYouTube"`
		IsYouTubeMusic bool   `json:"isYouTubeMusic"`
		RelayID        string `json:"relayId"`
		// v1.5.0 universal-engine options
		AudioOnly    bool   `json:"audioOnly"`
		AudioFormat  string `json:"audioFormat"`
		AudioQuality *int   `json:"audioQuality"`
		Format       string `json:"format"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	if len(req.URL) > maxDownloaderURLLength {
		writeError(c, http.StatusBadRequest, "URL is too long")
		return
	}
	if err := helpers.ValidateURLForSSRF(req.URL); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid URL: "+err.Error())
		return
	}

	// Forward the admin's session ID when available so the downloader can
	// optionally roundtrip-verify it. The shared internal token (attached by
	// the client for every request) is the primary trust mechanism — admins
	// authenticated via bearer token have no session cookie but the token
	// still vouches for them, so server-side storage works either way.
	var sessionID string
	if session := getSession(c); session != nil {
		sessionID = session.ID
	}

	params := downloader.DownloadParams{
		URL:            req.URL,
		Title:          req.Title,
		SaveLocation:   "server", // Always server — admin downloads are for import
		ClientID:       req.ClientID,
		IsYouTube:      req.IsYouTube,
		IsYouTubeMusic: req.IsYouTubeMusic,
		RelayID:        req.RelayID,
		AudioOnly:      req.AudioOnly,
		AudioFormat:    req.AudioFormat,
		AudioQuality:   req.AudioQuality,
		Format:         req.Format,
	}

	result, err := h.downloader.GetClient().Download(params, sessionID)
	if err != nil {
		writeError(c, http.StatusBadGateway, "Download failed: "+err.Error())
		return
	}

	h.trackServerEvent(c, analytics.EventDownloaderJobCreate, map[string]any{
		"url":              req.URL,
		"is_youtube":       req.IsYouTube,
		"is_youtube_music": req.IsYouTubeMusic,
	})
	writeSuccess(c, result)
}

// AdminDownloaderCancel cancels an active download.
func (h *Handler) AdminDownloaderCancel(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
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

	h.trackServerEvent(c, analytics.EventDownloaderJobCancel, map[string]any{"job_id": id})
	writeSuccess(c, map[string]string{"status": "canceled"})
}

// AdminDownloaderListDownloads returns completed downloads from the downloader.
func (h *Handler) AdminDownloaderListDownloads(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
		return
	}

	resp, err := h.downloader.GetClient().ListDownloads()
	if err != nil {
		writeError(c, http.StatusBadGateway, "Failed to list downloads: "+err.Error())
		return
	}

	// Map to frontend shape: filename, size, created (unix), url
	files := make([]map[string]any, 0, len(resp.Downloads))
	for _, f := range resp.Downloads {
		files = append(files, map[string]any{
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
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
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

	h.trackServerEvent(c, analytics.EventDownloaderJobCancel, map[string]any{"filename": filename, "scope": "delete"})
	writeSuccess(c, map[string]string{"status": "deleted"})
}

// AdminDownloaderSettings returns the downloader's current settings.
func (h *Handler) AdminDownloaderSettings(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
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
	audioFormats := resp.AudioFormats
	if audioFormats == nil {
		audioFormats = []string{}
	}
	writeSuccess(c, map[string]any{
		"allowServerStorage":     resp.AllowServerStorage,
		"audioFormat":            resp.AudioFormat,
		"audioFormats":           audioFormats,
		"supportedSites":         sites,
		"theme":                  resp.Theme,
		"browserRelayConfigured": resp.BrowserRelayConfigured,
		"downloadsDir":           h.config.Get().Downloader.DownloadsDir,
		"proxyPoolSize":          resp.ProxyPoolSize,
		"anySiteForAdmin":        resp.AnySiteForAdmin,
		"ytdlpAvailable":         resp.YtDlpAvailable,
	})
}

// AdminDownloaderImportable lists files ready to import from the downloader.
func (h *Handler) AdminDownloaderImportable(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}

	files, err := h.downloader.ListImportable()
	if err != nil {
		if isDownloaderUserError(err) {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, "Failed to list importable files: "+err.Error())
		return
	}

	writeSuccess(c, files)
}

// AdminDownloaderDestinations lists the library locations a download can be
// imported into (library roots + their sub-directories, including a HiDrive
// mount grafted under videos). The frontend uses this to prompt for a target.
func (h *Handler) AdminDownloaderDestinations(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	writeSuccess(c, h.downloader.ImportDestinations())
}

// AdminDownloaderImport moves a completed download into MSP's media library.
func (h *Handler) AdminDownloaderImport(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}

	var req struct {
		Filename     string `json:"filename" binding:"required"`
		Destination  string `json:"destination"`
		Subfolder    string `json:"subfolder"`
		DeleteSource bool   `json:"delete_source"`
		TriggerScan  bool   `json:"trigger_scan"`
	}
	if !BindJSON(c, &req, "") {
		return
	}

	destPath, sourceDeleted, err := h.downloader.Import(req.Filename, req.Destination, req.Subfolder, req.DeleteSource, req.TriggerScan)
	if err != nil {
		if isDownloaderUserError(err) {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, "Import failed: "+err.Error())
		return
	}

	h.trackServerEvent(c, analytics.EventPlaylistImport, map[string]any{
		"scope":          "downloader",
		"source":         req.Filename,
		"destination":    destPath,
		"scan_triggered": req.TriggerScan,
		"source_deleted": sourceDeleted,
	})
	writeSuccess(c, map[string]any{
		"source":        req.Filename,
		"destination":   destPath,
		"scanTriggered": req.TriggerScan,
		"sourceDeleted": sourceDeleted,
	})
}

// AdminDownloaderBatchDownload queues multiple URLs in one request (v1.5.0).
func (h *Handler) AdminDownloaderBatchDownload(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
		return
	}

	var req struct {
		ClientID string                         `json:"clientId" binding:"required"`
		URLs     []downloader.BatchDownloadItem `json:"urls" binding:"required"`
	}
	if !BindJSON(c, &req, "clientId and urls are required") {
		return
	}
	if len(req.URLs) == 0 {
		writeError(c, http.StatusBadRequest, "urls must be a non-empty array")
		return
	}

	// SSRF-validate every URL before forwarding.
	for _, item := range req.URLs {
		if len(item.URL) > maxDownloaderURLLength {
			writeError(c, http.StatusBadRequest, "URL is too long")
			return
		}
		if err := helpers.ValidateURLForSSRF(item.URL); err != nil {
			writeError(c, http.StatusBadRequest, "Invalid URL: "+err.Error())
			return
		}
	}

	var sessionID string
	if session := getSession(c); session != nil {
		sessionID = session.ID
	}

	resp, err := h.downloader.GetClient().BatchDownload(downloader.BatchDownloadParams{
		URLs:         req.URLs,
		ClientID:     req.ClientID,
		SaveLocation: "server",
	}, sessionID)
	if err != nil {
		writeError(c, http.StatusBadGateway, "Batch download failed: "+err.Error())
		return
	}

	h.trackServerEvent(c, analytics.EventDownloaderJobCreate, map[string]any{
		"scope": "batch",
		"count": resp.Queued,
	})
	writeSuccess(c, resp)
}

// AdminDownloaderQueue returns the downloader's active + queued jobs (v1.5.0).
func (h *Handler) AdminDownloaderQueue(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	if !h.downloader.IsOnline() {
		writeError(c, http.StatusServiceUnavailable, msgDownloaderOffline)
		return
	}

	resp, err := h.downloader.GetClient().Queue()
	if err != nil {
		writeError(c, http.StatusBadGateway, "Failed to get queue: "+err.Error())
		return
	}
	writeSuccess(c, resp)
}

// AdminDownloaderWebSocket upgrades to WebSocket and proxies to the downloader.
func (h *Handler) AdminDownloaderWebSocket(c *gin.Context) {
	if !h.checkDownloaderEnabled(c) {
		return
	}
	h.downloader.HandleWebSocket(c.Writer, c.Request)
}
