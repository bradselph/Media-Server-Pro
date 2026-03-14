package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/receiver"
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
	if !BindJSON(c, &req, "Invalid request") {
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
	var req receiver.CatalogPushRequest
	if !BindJSON(c, &req, "Invalid request") {
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
	if !BindJSON(c, &body, "Invalid request") {
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
// On success, writeSuccess is called so the slave receives a JSON body.
func (h *Handler) ReceiverStreamPush(c *gin.Context) {
	if !h.checkReceiverEnabled(c) {
		return
	}
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
	writeSuccess(c, gin.H{"status": "ok"})
}
