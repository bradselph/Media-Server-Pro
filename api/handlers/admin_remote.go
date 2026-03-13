package handlers

import (
	"encoding/json"
	"net/http"

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
	// TODO: API Contract Mismatch - safeState.LastSync is typed as interface{} instead of time.Time.
	// The remote module's SourceState.LastSync is time.Time, and assigning it to interface{}
	// causes json.Marshal to produce either a valid RFC3339 string (non-zero) or the string
	// "0001-01-01T00:00:00Z" (zero value) — NOT JSON null. This happens to match the
	// TypeScript RemoteSourceState.last_sync: string, but it is a fragile implicit contract:
	// if the remote module changes LastSync to a pointer (*time.Time), a nil value would
	// serialize as JSON null, breaking the TypeScript type which expects a string.
	// Change LastSync to time.Time to make the contract explicit and type-safe.
	// Consumer: web/frontend/src/api/types.ts RemoteSourceState.last_sync (line ~624).
	//
	// TODO: API Contract Mismatch - safeState has no "media" field, but the TypeScript type
	// RemoteSourceState (web/frontend/src/api/types.ts) declares "media?: RemoteMediaItem[]".
	// The frontend field will always be undefined — backend never serializes a "media" array
	// in this response. If per-source inline media listing is needed, add a Media []remoteMediaJSON
	// field here and populate it from h.remote.GetSourceMedia(s.Source.Name).
	// Consumer: adminApi.getRemoteSources() in web/frontend/src/api/endpoints.ts:687.
	type safeState struct {
		Source     safeSource  `json:"source"`
		Status     string      `json:"status"`
		LastSync   interface{} `json:"last_sync"`
		MediaCount int         `json:"media_count"`
		Error      string      `json:"error,omitempty"`
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
			LastSync:   s.LastSync,
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

	if source.Name == "" || source.URL == "" {
		writeError(c, http.StatusBadRequest, "name and url are required")
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

	// TODO: API Contract Mismatch - Response shape does not match the TypeScript RemoteSource type.
	// Frontend adminApi.createRemoteSource() (endpoints.ts:690) is typed to return RemoteSource
	// (web/frontend/src/api/types.ts:591-597) which includes an optional "password?" field.
	// This handler correctly omits password from the response (security: never echo credentials),
	// but the TypeScript return type RemoteSource implies password might be present on the response.
	// The frontend type should use RemoteSourceResponse (types.ts:601-606) — the read-only shape
	// without the password field — as the return type for createRemoteSource() instead of RemoteSource.
	// Currently the "username" field is emitted unconditionally as a string (empty string when unset);
	// RemoteSourceResponse.username is optional (omitempty on backend). An empty-string username
	// should either be omitted (use omitempty equivalent) or the TypeScript type must accept "".
	// Caller: adminApi.createRemoteSource() in web/frontend/src/api/endpoints.ts:690.
	writeSuccess(c, map[string]interface{}{
		"name":     source.Name,
		"url":      source.URL,
		"username": source.Username,
		"enabled":  source.Enabled,
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
	if c.ShouldBindJSON(&req) != nil {
		writeError(c, http.StatusBadRequest, errInvalidRequest)
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
// This endpoint is public (no admin auth) so the frontend player can use it directly.
// TODO: SSRF risk — the "url" query parameter is a user-controlled URL that the server
// will fetch and proxy. An authenticated user (requireAuth in routes.go) can make the
// server fetch any URL, including internal services (localhost, private IPs, cloud metadata
// endpoints like 169.254.169.254). Should validate the URL against a configured allowlist
// of remote source base URLs, or restrict to URLs belonging to a known remote source.
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
