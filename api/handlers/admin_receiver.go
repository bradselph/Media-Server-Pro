package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/receiver"
)

// checkReceiverEnabled returns true if the receiver module is available and enabled.
func (h *Handler) checkReceiverEnabled(c *gin.Context) bool {
	if h.receiver == nil {
		writeError(c, http.StatusServiceUnavailable, "Media receiver is not available")
		return false
	}
	cfg := h.media.GetConfig()
	if !cfg.Features.EnableReceiver || !cfg.Receiver.Enabled {
		writeError(c, http.StatusNotFound, "Media receiver feature is disabled")
		return false
	}
	return true
}

// requireReceiverAPIKey validates the X-API-Key header against configured receiver keys.
func (h *Handler) requireReceiverAPIKey(c *gin.Context) bool {
	apiKey := c.GetHeader("X-API-Key")
	if apiKey == "" {
		writeError(c, http.StatusUnauthorized, "Missing X-API-Key header")
		return false
	}
	if !h.receiver.ValidateAPIKey(apiKey) {
		writeError(c, http.StatusForbidden, "Invalid API key")
		return false
	}
	return true
}

// ReceiverRegisterSlave registers a new slave node with the master.
// POST /api/receiver/register
func (h *Handler) ReceiverRegisterSlave(c *gin.Context) {
	if !h.checkReceiverEnabled(c) || !h.requireReceiverAPIKey(c) {
		return
	}

	var req receiver.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	node, err := h.receiver.RegisterSlave(&req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	writeSuccess(c, node)
}

// ReceiverPushCatalog receives a media catalog update from a slave.
// POST /api/receiver/catalog
func (h *Handler) ReceiverPushCatalog(c *gin.Context) {
	if !h.checkReceiverEnabled(c) || !h.requireReceiverAPIKey(c) {
		return
	}

	var req receiver.CatalogPushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
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
	if !h.checkReceiverEnabled(c) || !h.requireReceiverAPIKey(c) {
		return
	}

	var body struct {
		SlaveID string `json:"slave_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	if err := h.receiver.Heartbeat(body.SlaveID); err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}

	writeSuccess(c, gin.H{"status": "ok"})
}

// ReceiverProxyStream proxies a media stream from a slave to the client.
// GET /receiver/stream/:id
func (h *Handler) ReceiverProxyStream(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}

	mediaID := c.Param("id")
	if mediaID == "" {
		writeError(c, http.StatusBadRequest, "media ID required")
		return
	}

	if err := h.receiver.ProxyStream(c.Writer, c.Request, mediaID); err != nil {
		// Don't write error if headers already sent (stream started)
		if !c.Writer.Written() {
			if isClientDisconnect(err) {
				return
			}
			writeError(c, http.StatusBadGateway, err.Error())
		}
		return
	}
}

// ReceiverListMedia returns all media from all online slaves.
// GET /api/receiver/media
func (h *Handler) ReceiverListMedia(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}

	query := c.Query("q")
	if query != "" {
		writeSuccess(c, h.receiver.SearchMedia(query))
		return
	}

	slaveID := c.Query("slave_id")
	if slaveID != "" {
		writeSuccess(c, h.receiver.GetSlaveMedia(slaveID))
		return
	}

	writeSuccess(c, h.receiver.GetAllMedia())
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
func (h *Handler) ReceiverStreamPush(c *gin.Context) {
	if !h.requireReceiverAPIKey(c) {
		return
	}

	token := c.Param("token")
	if token == "" {
		writeError(c, http.StatusBadRequest, "token required")
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
		if parsed, err := strconv.Atoi(status); err == nil && parsed > 0 {
			statusCode = parsed
		}
	}

	pr, pw := io.Pipe()
	delivery := &receiver.StreamDelivery{
		StatusCode:  statusCode,
		ContentType: c.GetHeader("Content-Type"),
		Headers:     c.Request.Header.Clone(),
		Body:        pr,
	}

	// Signal the waiting proxy handler
	ps.Ready <- delivery

	// Copy the slave's request body into the pipe. This blocks until
	// ProxyStream (the consumer) finishes reading or the pipe is closed.
	_, copyErr := io.Copy(pw, c.Request.Body)
	pw.CloseWithError(copyErr) // signals EOF (or error) to the reader
}
