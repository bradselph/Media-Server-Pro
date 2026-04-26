package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/receiver"
	"media-server-pro/pkg/helpers"
)

func (h *Handler) checkDuplicateDetectionEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.duplicates, "Duplicate detection", func() bool {
		return h.config.Get().Features.EnableDuplicateDetection
	})
}

func (h *Handler) checkReceiverEnabled(c *gin.Context) bool {
	return checkFeatureEnabled(c, h.receiver, "Media receiver", func() bool {
		return h.media.GetConfig().Receiver.Enabled
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
