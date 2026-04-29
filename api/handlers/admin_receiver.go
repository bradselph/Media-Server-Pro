package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/receiver"
	"media-server-pro/pkg/helpers"
)

func (h *Handler) checkDuplicateDetectionEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.duplicates, "Duplicate detection", func() bool {
		return h.config.Get().Features.EnableDuplicateDetection
	})
}

// checkReceiverEnabled gates receiver-side admin endpoints. Mirrors the
// follower's "configured == enabled" rule: the receiver counts as enabled
// when the explicit flag is set OR at least one API key is configured, so a
// fresh install with keys provisioned by env/config doesn't have to also
// toggle a separate flag.
func (h *Handler) checkReceiverEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.receiver, "Media receiver", func() bool {
		rc := h.media.GetConfig().Receiver
		return rc.Enabled || len(rc.APIKeys) > 0
	})
}

// requireReceiverAPIKey validates the receiver API key from X-API-Key header or api_key query param.
func (h *Handler) requireReceiverAPIKey(c *gin.Context) bool {
	apiKey := c.GetHeader("X-API-Key")
	if apiKey == "" {
		apiKey = c.Query("api_key")
	}
	if apiKey == "" {
		writeError(c, http.StatusUnauthorized, "Missing X-API-Key header or api_key query parameter")
		return false
	}
	if !h.receiver.ValidateAPIKey(apiKey) {
		writeError(c, http.StatusForbidden, "Invalid API key")
		return false
	}
	return true
}

// requireReceiverWithAPIKey ensures the receiver feature is enabled and a valid API key is present.
func (h *Handler) requireReceiverWithAPIKey(c *gin.Context) bool {
	return h.checkReceiverEnabled(c) && h.requireReceiverAPIKey(c)
}

// RequireReceiverWithAPIKey returns Gin middleware that enforces receiver enabled + valid X-API-Key.
// Use for route groups that require slave authentication (register, catalog, heartbeat).
func (h *Handler) RequireReceiverWithAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.requireReceiverWithAPIKey(c) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// ReceiverRegisterSlave registers a new slave node with the master.
// POST /api/receiver/register
func (h *Handler) ReceiverRegisterSlave(c *gin.Context) {
	var req receiver.RegisterRequest
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}

	node, err := h.receiver.RegisterSlave(c.Request.Context(), &req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	writeSuccess(c, node)
}

// ReceiverPushCatalog receives a media catalog update from a slave.
// POST /api/receiver/catalog
func (h *Handler) ReceiverPushCatalog(c *gin.Context) {
	// Cap request body at 32 MB to prevent memory exhaustion from huge payloads.
	// The WS path already has wsReadLimit (16 MB); this covers the HTTP REST path.
	const maxCatalogBody = 32 * 1024 * 1024
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCatalogBody)

	var req receiver.CatalogPushRequest
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}

	count, err := h.receiver.PushCatalog(&req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	writeSuccess(c, gin.H{
		"items_received": count,
		"full_sync":      req.Full,
	})
}

// ReceiverHeartbeat processes a heartbeat from a slave.
// POST /api/receiver/heartbeat
func (h *Handler) ReceiverHeartbeat(c *gin.Context) {
	var body struct {
		SlaveID string `json:"slave_id"`
	}
	if !BindJSON(c, &body, errInvalidRequest) {
		return
	}

	if err := h.receiver.Heartbeat(body.SlaveID); err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	writeSuccess(c, gin.H{"status": "ok"})
}

// ReceiverListMedia returns all media from all online slaves.
// GET /api/receiver/media
func (h *Handler) ReceiverListMedia(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}

	query := c.Query("q")
	if query != "" {
		items := h.receiver.SearchMedia(query)
		if items == nil {
			items = []*receiver.MediaItem{}
		}
		writeSuccess(c, items)
		return
	}

	slaveID := c.Query("slave_id")
	if slaveID != "" {
		items := h.receiver.GetSlaveMedia(slaveID)
		if items == nil {
			items = []*receiver.MediaItem{}
		}
		writeSuccess(c, items)
		return
	}

	items := h.receiver.GetAllMedia()
	if items == nil {
		items = []*receiver.MediaItem{}
	}
	writeSuccess(c, items)
}

// ReceiverGetMedia returns a single media item by ID.
// GET /api/receiver/media/:id
func (h *Handler) ReceiverGetMedia(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}

	mediaID := c.Param("id")
	item := h.receiver.GetMediaItem(mediaID)
	if item == nil {
		writeError(c, http.StatusNotFound, fmt.Sprintf("media not found: %s", mediaID))
		return
	}

	writeSuccess(c, item)
}

// receiverAdminSettings is the body of GET /api/admin/receiver/settings.
// Unlike the follower endpoint (which redacts api_key), this surfaces the
// configured keys in plain text — admins need them to paste into the
// Follower form on a paired server. The route is admin-only and the keys
// are already configured on this server, so no escalation is granted.
type receiverAdminSettings struct {
	Enabled bool     `json:"enabled"`
	APIKeys []string `json:"api_keys"`
}

// AdminReceiverGetSettings returns the configured receiver API keys so the
// admin can copy one into another VPS's follower pairing form.
// GET /api/admin/receiver/settings
func (h *Handler) AdminReceiverGetSettings(c *gin.Context) {
	rc := h.config.Get().Receiver
	keys := append([]string(nil), rc.APIKeys...)
	writeSuccess(c, receiverAdminSettings{
		Enabled: rc.Enabled || len(rc.APIKeys) > 0,
		APIKeys: keys,
	})
}

// receiverPairRequest is the body for POST /api/receiver/pair. Mirrors the
// admin-side follower settings form but is invoked cross-server: the calling
// server tells THIS server "follow me at master_url using api_key so your
// catalog flows to me." Authentication is the receiver API key (X-API-Key
// header), enforced by the route group's RequireReceiverWithAPIKey middleware.
//
// This is what makes the user-facing "add a peer to pull from" flow possible
// without forcing the operator to log into both servers and manually configure
// the follower form on the source side. The caller, who is the consumer of
// content, configures the producer remotely.
type receiverPairRequest struct {
	MasterURL string `json:"master_url"`
	APIKey    string `json:"api_key"`
}

// ReceiverPair configures THIS server's follower to push its catalog to the
// master_url specified in the request body. Auth is the X-API-Key the route
// group already validates. Used by the cross-server "pair to pull" flow.
// POST /api/receiver/pair
func (h *Handler) ReceiverPair(c *gin.Context) {
	if h.follower == nil {
		writeError(c, http.StatusServiceUnavailable, "Follower module not available")
		return
	}
	var req receiverPairRequest
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	masterURL := strings.TrimSpace(req.MasterURL)
	apiKey := strings.TrimSpace(req.APIKey)
	if masterURL == "" || apiKey == "" {
		writeError(c, http.StatusBadRequest, "master_url and api_key are required")
		return
	}
	u, err := url.Parse(masterURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(c, http.StatusBadRequest, "master_url must be a valid http(s) URL")
		return
	}

	// Reject pairings that would point this server back at itself — protects
	// against accidental loops where the user supplied their own URL.
	if u.Host == c.Request.Host {
		writeError(c, http.StatusBadRequest, "master_url points to this server (self-pairing not supported)")
		return
	}

	if err := h.config.Update(func(cfg *config.Config) {
		cfg.Follower.MasterURL = masterURL
		cfg.Follower.APIKey = apiKey
		cfg.Follower.Enabled = true
	}); err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to save pairing: "+err.Error())
		return
	}

	reloadCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := h.follower.Reload(reloadCtx); err != nil {
		// Settings saved but the loop didn't restart cleanly. Surface the reload
		// error so the caller knows the WS may still be using stale config.
		writeSuccess(c, gin.H{
			"saved":         true,
			"reload_error":  err.Error(),
			"reload_status": h.follower.GetStatus(),
		})
		return
	}
	writeSuccess(c, gin.H{
		"saved":         true,
		"reload_status": h.follower.GetStatus(),
	})
}

// adminPeerConnectRequest is the body for POST /api/admin/peer/connect.
// Drives the cross-server pairing flow: this admin asks a remote peer to
// configure ITS follower to push to us. peer_url + peer_api_key authenticate
// against the peer's receiver. our_url defaults to the request's Host so the
// admin doesn't have to type their own URL; can be overridden when the
// public URL differs from the host the admin reaches the panel through.
type adminPeerConnectRequest struct {
	PeerURL    string `json:"peer_url"`
	PeerAPIKey string `json:"peer_api_key"`
	OurURL     string `json:"our_url,omitempty"`
}

// AdminPeerConnect tells a remote peer to start pushing its catalog to this
// server. Authenticates with peer_api_key against the peer's
// /api/receiver/pair endpoint and hands it (our_url, one of our receiver
// keys) so the peer's follower can reach us. Admin-only.
//
// This is the user-facing complement to /api/receiver/pair: the admin does
// not have to log into both servers — they configure the pairing on the
// receiving side ("I want to pull from this peer") and this handler does
// the cross-server call.
//
// POST /api/admin/peer/connect
func (h *Handler) AdminPeerConnect(c *gin.Context) {
	if h.receiver == nil {
		writeError(c, http.StatusServiceUnavailable, "Receiver module not available")
		return
	}
	var req adminPeerConnectRequest
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}
	peerURL := strings.TrimRight(strings.TrimSpace(req.PeerURL), "/")
	peerKey := strings.TrimSpace(req.PeerAPIKey)
	if peerURL == "" || peerKey == "" {
		writeError(c, http.StatusBadRequest, "peer_url and peer_api_key are required")
		return
	}
	pu, err := url.Parse(peerURL)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		writeError(c, http.StatusBadRequest, "peer_url must be a valid http(s) URL")
		return
	}
	if err := helpers.ValidateURLForSSRF(peerURL); err != nil {
		writeError(c, http.StatusBadRequest, "peer_url rejected: "+err.Error())
		return
	}

	rc := h.config.Get().Receiver
	if len(rc.APIKeys) == 0 {
		writeError(c, http.StatusBadRequest, "No receiver API keys configured on this server. Set RECEIVER_API_KEYS first.")
		return
	}
	ourKey := rc.APIKeys[0]

	ourURL := strings.TrimRight(strings.TrimSpace(req.OurURL), "/")
	if ourURL == "" {
		// Derive from the request the admin hit. Trust the proxy's forwarded
		// proto when present so we don't downgrade to http behind TLS-terminating
		// load balancers.
		scheme := "http"
		if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		ourURL = scheme + "://" + c.Request.Host
	}
	ouru, err := url.Parse(ourURL)
	if err != nil || (ouru.Scheme != "http" && ouru.Scheme != "https") || ouru.Host == "" {
		writeError(c, http.StatusBadRequest, "our_url must be a valid http(s) URL")
		return
	}
	if pu.Host == ouru.Host {
		writeError(c, http.StatusBadRequest, "peer_url and our_url match — self-pairing not supported")
		return
	}

	body, err := json.Marshal(receiverPairRequest{
		MasterURL: ourURL,
		APIKey:    ourKey,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to encode pair request: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, peerURL+"/api/receiver/pair", strings.NewReader(string(body)))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to build pair request: "+err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", peerKey)

	client := &http.Client{Transport: helpers.SafeHTTPTransport(), Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		writeError(c, http.StatusBadGateway, "Failed to reach peer: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		// Forward the peer's error so the admin sees what went wrong (auth,
		// validation, etc.) without having to inspect the peer's logs.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		writeError(c, http.StatusBadGateway, fmt.Sprintf("Peer rejected pairing (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes))))
		return
	}

	writeSuccess(c, gin.H{
		"paired":   true,
		"peer_url": peerURL,
		"our_url":  ourURL,
	})
}

// AdminReceiverListSlaves lists all registered slave nodes.
// GET /api/admin/receiver/slaves
func (h *Handler) AdminReceiverListSlaves(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}
	writeSuccess(c, h.receiver.GetSlaves())
}

// AdminReceiverGetStats returns receiver statistics.
// GET /api/admin/receiver/stats
func (h *Handler) AdminReceiverGetStats(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}
	writeSuccess(c, h.receiver.GetStats())
}

// AdminReceiverRemoveSlave removes a slave node and its catalog.
// DELETE /api/admin/receiver/slaves/:id
func (h *Handler) AdminReceiverRemoveSlave(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}

	slaveID := c.Param("id")
	if err := h.receiver.UnregisterSlave(slaveID); err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	writeSuccess(c, gin.H{"message": "slave removed"})
}

// ReceiverWebSocket upgrades an HTTP connection to a WebSocket for a slave node.
// GET /ws/receiver — slave authenticates via X-API-Key header or api_key query param.
func (h *Handler) ReceiverWebSocket(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}
	h.receiver.HandleWebSocket(c.Writer, c.Request)
}

// ReceiverStreamPush receives file data from a slave delivering a stream.
// POST /api/receiver/stream-push/:token
// The slave opens this connection in response to a stream_request sent over WebSocket.
// On success, writeSuccess is called so the slave receives a JSON body.
func (h *Handler) ReceiverStreamPush(c *gin.Context) {
	// Auth is enforced by RequireReceiverWithAPIKey group middleware in routes.go.
	token, ok := RequireParamID(c, "token")
	if !ok {
		return
	}
	// Validate token length (UUID tokens are 36 characters with hyphens).
	if len(token) != 36 {
		writeError(c, http.StatusBadRequest, "invalid token format")
		return
	}

	ps, ok := h.receiver.DeliverStream(token)
	if !ok {
		writeError(c, http.StatusNotFound, "no pending stream for token")
		return
	}

	// Build the delivery with the slave's request headers and body.
	// We use a pipe so this handler can block until the ProxyStream consumer
	// finishes reading the body — if we passed c.Request.Body directly, Gin
	// would close it when this handler returns, racing with ProxyStream's read.
	statusCode := http.StatusOK
	if status := c.GetHeader("X-Stream-Status"); status != "" {
		if parsed, err := strconv.Atoi(status); err == nil {
			switch parsed {
			case http.StatusOK, http.StatusPartialContent,
				http.StatusNotFound, http.StatusRequestedRangeNotSatisfiable,
				http.StatusServiceUnavailable:
				statusCode = parsed
			}
		}
	}

	// Filter incoming slave headers to only media-relevant ones before building
	// the delivery.  This prevents accidental leakage of internal headers
	// (X-API-Key, X-Forwarded-For, etc.) into the StreamDelivery that is read
	// by ProxyStream and forwarded to the end user.
	safeHeaders := make(http.Header)
	for key := range helpers.AllowedProxyHeaders {
		if vals := c.Request.Header.Values(key); len(vals) > 0 {
			safeHeaders[key] = vals
		}
	}

	pr, pw := io.Pipe()
	delivery := &receiver.StreamDelivery{
		StatusCode:  statusCode,
		ContentType: c.GetHeader("Content-Type"),
		Headers:     safeHeaders,
		Body:        pr,
	}

	// Signal the waiting proxy handler.
	ps.Ready <- delivery

	// If the consumer gave up (timeout / client disconnect) after we sent the
	// delivery but before it started reading, nobody will drain pr. Watch the
	// consumer context and close pw so io.Copy below returns promptly instead
	// of blocking forever. The watcher is also released on normal completion
	// via the done channel so it does not leak for the duration of the slave's
	// keep-alive connection.
	done := make(chan struct{})
	go func() {
		select {
		case <-ps.ConsumerContext().Done():
		case <-c.Request.Context().Done():
		case <-done:
			return // normal completion — nothing to do
		}
		pw.CloseWithError(fmt.Errorf("stream consumer exited"))
	}()

	// Copy the slave's request body into the pipe. This blocks until
	// ProxyStream (the consumer) finishes reading or the pipe is closed.
	_, copyErr := io.Copy(pw, c.Request.Body)
	close(done)                // unblock the watcher goroutine on normal path
	pw.CloseWithError(copyErr) // signals EOF (or error) to the reader
	writeSuccess(c, gin.H{"status": "ok"})
}
