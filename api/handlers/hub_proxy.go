package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/hub"
	"media-server-pro/pkg/models"
)

// Server-side Hub playback and artwork.
//
// The catalog is third-party embeds, so by default the provider sees each
// viewer's IP — and where the provider blocks a region, nothing plays. These
// endpoints let the server fetch the media instead and stream it on, so the
// provider only ever sees this server.
//
// Rollout is config-driven rather than route-driven: every endpoint registers
// under plain session auth and does its own admin check, so widening access from
// admins to all users is a config flip (hub.proxy_all_users) with no route
// change, no redeploy, and no frontend rebuild.

// requireHubProxyAccess gates video playback through the proxy. Order matters:
// module/feature, then the proxy switch, then the age gate, then the rollout
// phase — so a disabled feature never reveals whether an id exists.
func (h *Handler) requireHubProxyAccess(c *gin.Context) bool {
	if !h.requireHub(c) {
		return false
	}
	cfg := h.config.Get()
	if !cfg.Hub.ProxyEnabled {
		writeError(c, http.StatusNotFound, "Server-side playback is not enabled")
		return false
	}
	// The catalog is entirely 18+, so the same gate as every other Hub read.
	if !h.checkMatureAccess(c, true) {
		return false
	}
	if !cfg.Hub.ProxyAllUsers && !isAdminUser(c) {
		writeError(c, http.StatusForbidden, "Server-side playback is currently limited to administrators")
		return false
	}
	return true
}

// requireHubImageAccess gates catalog artwork. Artwork proxying is deliberately
// separate from and looser than video: it is cheap and cached, and it exists so
// blocked viewers see a working grid rather than broken images. It still
// requires the same login + mature permission as browsing the catalog at all.
func (h *Handler) requireHubImageAccess(c *gin.Context) bool {
	if !h.requireHub(c) {
		return false
	}
	if !h.config.Get().Hub.ProxyImages {
		writeError(c, http.StatusNotFound, "Hub image proxying is not enabled")
		return false
	}
	return h.checkMatureAccess(c, true)
}

// isAdminUser reports whether the caller is an enabled admin. Used for in-handler
// gating where the route itself must stay open to all authenticated users.
func isAdminUser(c *gin.Context) bool {
	u := getUser(c)
	return u != nil && u.Enabled && u.Role == models.RoleAdmin
}

// GetHubEmbedPlayback reports whether an embed can be played through the server
// and which player the frontend should attach.
//
// Expected "cannot play" outcomes are 200 with available:false, not errors: the
// frontend's answer to them is simply to keep showing the iframe, and a 5xx
// would turn a normal fallback into console noise.
// GET /api/hub/embeds/:id/playback
func (h *Handler) GetHubEmbedPlayback(c *gin.Context) {
	if !h.requireHubProxyAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}

	info, err := h.hub.CheckPlayback(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, hub.ErrNotInCatalog) {
			writeError(c, http.StatusNotFound, "Hub embed not found")
			return
		}
		h.log.Debug("Hub: playback check failed for %s: %v", id, err)
		writeSuccess(c, hub.StreamInfo{Available: false, Reason: hubPlaybackReason(err)})
		return
	}

	h.trackHubEvent(c, analytics.EventHubProxyPlay, map[string]any{
		"embed_id": id,
		"kind":     info.Kind,
	})
	writeSuccess(c, info)
}

// hubPlaybackReason turns a resolution failure into a short, non-leaking
// explanation for the UI. Upstream error text is deliberately not passed
// through — it can contain the resolved CDN URL.
func hubPlaybackReason(err error) string {
	switch {
	case errors.Is(err, hub.ErrNoResolver):
		return "No stream resolver is available on this server"
	case errors.Is(err, hub.ErrNoStream):
		return "No playable stream was found for this item"
	default:
		return "This item could not be prepared for server playback"
	}
}

// HubProxyMaster serves the rewritten HLS master playlist.
// GET /hub/proxy/:id/master.m3u8
func (h *Handler) HubProxyMaster(c *gin.Context) {
	if !h.requireHubProxyAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	h.writeHubProxyResult(c, h.hub.ProxyHLSMaster(c.Writer, c.Request, id))
}

// HubProxyVariant serves one rendition's rewritten media playlist.
// GET /hub/proxy/:id/:quality/playlist.m3u8
func (h *Handler) HubProxyVariant(c *gin.Context) {
	if !h.requireHubProxyAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	idx, err := strconv.Atoi(c.Param("quality"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "Invalid rendition")
		return
	}
	h.writeHubProxyResult(c, h.hub.ProxyHLSVariant(c.Writer, c.Request, id, idx))
}

// HubProxyAsset serves one segment, encryption key, or init segment.
// GET /hub/proxy/:id/:quality/:asset
func (h *Handler) HubProxyAsset(c *gin.Context) {
	if !h.requireHubProxyAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	idx, err := strconv.Atoi(c.Param("quality"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "Invalid rendition")
		return
	}
	h.writeHubProxyResult(c, h.hub.ProxyHLSAsset(c.Writer, c.Request, id, idx, c.Param("asset")))
}

// HubProxyStream serves a progressive file with byte-range support.
// GET /hub/proxy/:id/stream
func (h *Handler) HubProxyStream(c *gin.Context) {
	if !h.requireHubProxyAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	h.writeHubProxyResult(c, h.hub.ProxyMP4(c.Writer, c.Request, id))
}

// HubProxyThumb serves a catalog thumbnail.
// GET /hub/img/:id/t
func (h *Handler) HubProxyThumb(c *gin.Context) {
	if !h.requireHubImageAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	h.writeHubProxyResult(c, h.hub.ProxyImage(c.Writer, c.Request, id, "t", 0))
}

// HubProxyPreview serves one hover-preview frame.
// GET /hub/img/:id/p/:idx
func (h *Handler) HubProxyPreview(c *gin.Context) {
	if !h.requireHubImageAccess(c) {
		return
	}
	id, ok := RequireParamID(c, "id")
	if !ok {
		return
	}
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "Invalid preview index")
		return
	}
	h.writeHubProxyResult(c, h.hub.ProxyImage(c.Writer, c.Request, id, "p", idx))
}

// writeHubProxyResult maps a proxy error onto a status code, but only while the
// response is still uncommitted — once bytes are on the wire the only honest
// thing to do is log and let the connection end.
func (h *Handler) writeHubProxyResult(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if c.Writer.Written() {
		h.log.Debug("Hub proxy: error after response started: %v", err)
		return
	}
	switch {
	case errors.Is(err, hub.ErrNotInCatalog):
		writeError(c, http.StatusNotFound, "Hub embed not found")
	case errors.Is(err, hub.ErrNoResolver), errors.Is(err, hub.ErrNoStream):
		h.log.Debug("Hub proxy: unavailable: %v", err)
		writeError(c, http.StatusServiceUnavailable, "Server playback is unavailable for this item")
	default:
		h.log.Warn("Hub proxy: upstream failure: %v", err)
		writeError(c, http.StatusBadGateway, "Failed to stream from the source")
	}
}
