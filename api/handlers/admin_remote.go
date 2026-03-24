package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
)

// GetRemoteSources returns all configured remote media sources
func (h *Handler) GetRemoteSources(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	sources := h.remote.GetSources()

	// Strip passwords before sending to the frontend.
	type safeSource struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username,omitempty"`
		Enabled  bool   `json:"enabled"`
	}
	type safeState struct {
		Source     safeSource `json:"source"`
		Status     string     `json:"status"`
		LastSync   string     `json:"last_sync"`
		MediaCount int        `json:"media_count"`
		Error      string     `json:"error,omitempty"`
	}
	safe := make([]safeState, len(sources))
	for i, s := range sources {
		safe[i] = safeState{
			Source: safeSource{
				Name:     s.Source.Name,
				URL:      s.Source.URL,
				Username: s.Source.Username,
				Enabled:  s.Source.Enabled,
			},
			Status:     s.Status,
			LastSync:   s.LastSync.Format(time.RFC3339),
			MediaCount: s.MediaCount,
			Error:      s.Error,
		}
	}
	writeSuccess(c, safe)
}

// CreateRemoteSource adds a new remote source at runtime and persists it to config
func (h *Handler) CreateRemoteSource(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}

	var source config.RemoteSource
	if err := json.NewDecoder(c.Request.Body).Decode(&source); err != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
		return
	}

	source.Name = strings.TrimSpace(source.Name)
	source.URL = strings.TrimSpace(source.URL)
	if source.Name == "" || source.URL == "" {
		writeError(c, http.StatusBadRequest, "name and url are required")
		return
	}
	u, err := url.Parse(source.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(c, http.StatusBadRequest, "url must be a valid http or https URL")
		return
	}
	source.Enabled = true

	if err := h.remote.AddSource(source); err != nil {
		writeError(c, http.StatusConflict, "Source already exists")
		return
	}

	if err := h.config.Update(func(cfg *config.Config) {
		cfg.RemoteMedia.Sources = append(cfg.RemoteMedia.Sources, source)
	}); err != nil {
		h.log.Warn("Failed to persist new remote source to config: %v", err)
		// Source is active in memory but won't survive a restart
	}

	type createRemoteSourceResponse struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username,omitempty"`
		Enabled  bool   `json:"enabled"`
	}
	writeSuccess(c, createRemoteSourceResponse{
		Name:     source.Name,
		URL:      source.URL,
		Username: source.Username,
		Enabled:  source.Enabled,
	})
}

// GetRemoteStats returns remote media statistics
func (h *Handler) GetRemoteStats(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	stats := h.remote.GetStats()
	writeSuccess(c, stats)
}

// GetRemoteMedia returns all remote media from all sources
func (h *Handler) GetRemoteMedia(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	remoteMedia := h.remote.GetAllRemoteMedia()
	writeSuccess(c, remoteMedia)
}

// GetRemoteSourceMedia returns media from a specific source
func (h *Handler) GetRemoteSourceMedia(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	sourceName := c.Param("source")

	sourceMedia, err := h.remote.GetSourceMedia(sourceName)
	if err != nil {
		writeError(c, http.StatusNotFound, "Source not found")
		return
	}

	writeSuccess(c, sourceMedia)
}

// SyncRemoteSource triggers a sync for a specific remote source
func (h *Handler) SyncRemoteSource(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	sourceName := c.Param("source")

	sources := h.remote.GetSources()
	var found bool
	for _, s := range sources {
		if s.Source.Name == sourceName {
			found = true
			break
		}
	}

	if !found {
		writeError(c, http.StatusNotFound, "Source not found")
		return
	}

	h.log.Info("Triggering background sync for remote source: %s", sourceName)
	go func() {
		if err := h.remote.SyncSource(sourceName); err != nil {
			h.log.Warn("Background sync failed for source %s: %v", sourceName, err)
		} else {
			h.log.Info("Background sync completed for remote source: %s", sourceName)
		}
	}()

	writeSuccess(c, map[string]string{"status": "sync_started"})
}

// DeleteRemoteSource removes a remote source
func (h *Handler) DeleteRemoteSource(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	sourceName := c.Param("source")

	if sourceName == "" {
		writeError(c, http.StatusBadRequest, "source name required")
		return
	}

	if err := h.remote.RemoveSource(sourceName); err != nil {
		writeError(c, http.StatusNotFound, "Source not found")
		return
	}

	if err := h.config.Update(func(cfg *config.Config) {
		filtered := make([]config.RemoteSource, 0, len(cfg.RemoteMedia.Sources))
		for _, s := range cfg.RemoteMedia.Sources {
			if s.Name != sourceName {
				filtered = append(filtered, s)
			}
		}
		cfg.RemoteMedia.Sources = filtered
	}); err != nil {
		h.log.Warn("Failed to persist remote source deletion to config: %v", err)
	}

	writeSuccess(c, map[string]string{"message": "Source removed"})
}

// CacheRemoteMedia caches a remote media file locally
func (h *Handler) CacheRemoteMedia(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	var req struct {
		URL        string `json:"url"`
		SourceName string `json:"source_name"`
	}
	if !BindJSON(c, &req, "") {
		return
	}
	if req.URL == "" {
		writeError(c, http.StatusBadRequest, "url is required")
		return
	}

	cached, err := h.remote.CacheMedia(req.URL, req.SourceName)
	if err != nil {
		h.log.Error("%v", err)
		writeError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeSuccess(c, cached)
}

// CleanRemoteCache cleans the remote media cache
func (h *Handler) CleanRemoteCache(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}
	removed := h.remote.CleanCache()
	writeSuccess(c, map[string]int{"removed": removed})
}

// StreamRemoteMedia proxies and optionally caches a remote media stream.
// The "url" query parameter is user-controlled; remote.ProxyRemoteWithCache calls
// StreamRemote which validates the URL for SSRF (rejects private/loopback IPs) before fetching.
func (h *Handler) StreamRemoteMedia(c *gin.Context) {
	if !h.checkRemoteMediaEnabled(c) {
		return
	}

	remoteURL := c.Query("url")
	sourceName := c.Query("source")

	if remoteURL == "" {
		writeError(c, http.StatusBadRequest, "url parameter required")
		return
	}

	if err := h.remote.ProxyRemoteWithCache(c.Writer, c.Request, remoteURL, sourceName); err != nil {
		h.log.Error("Remote stream error: %v", err)
		// Only write error response if headers haven't been sent yet
		if !c.Writer.Written() {
			writeError(c, http.StatusBadGateway, "Failed to stream from remote")
		}
	}
}
