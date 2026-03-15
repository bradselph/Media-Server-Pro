package receiver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"media-server-pro/internal/logger"
)

// WebSocket protocol message types exchanged between master and slave.
const (
	// Slave → Master
	msgTypeRegister  = "register"
	msgTypeCatalog   = "catalog"
	msgTypeHeartbeat = "heartbeat"

	// Master → Slave
	msgTypeStreamRequest = "stream_request"
)

// wsMessage is the envelope for all WebSocket JSON messages.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// wsRegisterData is sent by slave on connect.
type wsRegisterData struct {
	SlaveID string `json:"slave_id"`
	Name    string `json:"name"`
}

// wsCatalogData is sent by slave with media catalog.
type wsCatalogData struct {
	SlaveID string         `json:"slave_id"`
	Items   []*CatalogItem `json:"items"`
	Full    bool           `json:"full"`
}

// wsHeartbeatData is sent periodically by slave.
type wsHeartbeatData struct {
	SlaveID string `json:"slave_id"`
}

// wsStreamRequestData is sent master → slave to request a file stream.
type wsStreamRequestData struct {
	Token string `json:"token"`
	Path  string `json:"path"`
	Range string `json:"range,omitempty"`
}

// PendingStream holds state for a stream delivery in progress.
// The master creates one when a user requests media from a slave,
// sends a stream_request over WS, and waits for the slave to deliver
// the file data via HTTP POST to /api/receiver/stream-push/:token.
type PendingStream struct {
	MediaID   string
	SlaveID   string
	Path      string
	Range     string
	Ready     chan *StreamDelivery // slave posts delivery here
	CreatedAt time.Time
	readyOnce sync.Once // guards close(Ready) to prevent double-close panic
}

// StreamDelivery is the data the slave sends back for a pending stream.
type StreamDelivery struct {
	StatusCode  int
	ContentType string
	Headers     http.Header
	Body        io.ReadCloser
}

// slaveWS represents an active WebSocket connection from a slave.
type slaveWS struct {
	slaveID string
	conn    *websocket.Conn
	mu      sync.Mutex // protects writes to conn
	log     *logger.Logger
	done    chan struct{} // closed on disconnect to stop the ping goroutine
}

// sendJSON sends a typed JSON message to the slave.
func (s *slaveWS) sendJSON(msgType string, data interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	msg := wsMessage{Type: msgType, Data: raw}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteJSON(msg)
}

// setReadDeadline sets the read deadline and logs if it fails.
func setReadDeadline(conn *websocket.Conn, d time.Duration, log *logger.Logger) {
	if err := conn.SetReadDeadline(time.Now().Add(d)); err != nil && log != nil {
		log.Warn("SetReadDeadline failed: %v", err)
	}
}

// upgrader accepts WebSocket connections from any origin. Access control is enforced
// by API key validation in HandleWebSocket (X-API-Key header or api_key query).
// If the WS endpoint is exposed without that gate, it would be an open relay.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// HandleWebSocket upgrades an HTTP connection to a WebSocket for a slave node.
// The slave authenticates via X-API-Key header or api_key query parameter.
// All registration, catalog pushes, and heartbeats flow through this connection.
func (m *Module) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("api_key")
	}
	if !m.ValidateAPIKey(apiKey) {
		http.Error(w, "Invalid API key", http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		m.log.Warn("WebSocket upgrade failed: %v", err)
		return
	}

	sw := &slaveWS{
		conn: conn,
		log:  m.log,
		done: make(chan struct{}),
	}

	// Limit incoming message size to prevent memory exhaustion from malicious slaves.
	// 16 MB accommodates large catalog pushes while bounding memory usage.
	conn.SetReadLimit(16 * 1024 * 1024)

	// Configure keep-alive via ping/pong
	setReadDeadline(conn, 60*time.Second, m.log)
	conn.SetPongHandler(func(string) error {
		setReadDeadline(conn, 60*time.Second, m.log)
		return nil
	})

	// Start ping ticker — stopped when done channel is closed on disconnect
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sw.mu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				sw.mu.Unlock()
				if err != nil {
					return
				}
			case <-sw.done:
				return
			}
		}
	}()

	defer func() {
		close(sw.done)
		conn.Close()
		if sw.slaveID != "" {
			m.removeSlaveWS(sw.slaveID)
			m.log.Info("Slave %s WebSocket disconnected", sw.slaveID)
		}
	}()

	m.log.Info("New WebSocket connection from %s", r.RemoteAddr)

	// Read loop — process messages from slave
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				m.log.Warn("Slave WS read error: %v", err)
			}
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			m.log.Warn("Invalid WS message: %v", err)
			continue
		}

		switch msg.Type {
		case msgTypeRegister:
			var data wsRegisterData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				m.log.Warn("Invalid register data: %v", err)
				continue
			}

			node, err := m.RegisterSlave(&RegisterRequest{
				SlaveID: data.SlaveID,
				Name:    data.Name,
				BaseURL: "ws-connected", // marker — slave doesn't expose an HTTP server
			})
			if err != nil {
				m.log.Warn("WS register failed for %s: %v", data.SlaveID, err)
				continue
			}
			// Use the authoritative ID assigned by RegisterSlave (which may
			// generate a UUID when the client sends an empty SlaveID).
			sw.slaveID = node.ID
			m.setSlaveWS(node.ID, sw)
			m.log.Info("Slave %s registered via WebSocket (name: %s)", node.ID, node.Name)

			// Reset read deadline after successful registration
			setReadDeadline(conn, 60*time.Second, m.log)

		case msgTypeCatalog:
			var data wsCatalogData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				m.log.Warn("Invalid catalog data: %v", err)
				continue
			}
			// Enforce that catalog pushes can only target the authenticated slave's own ID
			if sw.slaveID != "" && data.SlaveID != sw.slaveID {
				m.log.Warn("Catalog push SlaveID mismatch: connection=%s message=%s", sw.slaveID, data.SlaveID)
				continue
			}
			count, err := m.PushCatalog(&CatalogPushRequest{
				SlaveID: data.SlaveID,
				Items:   data.Items,
				Full:    data.Full,
			})
			if err != nil {
				m.log.Warn("WS catalog push failed for %s: %v", data.SlaveID, err)
			} else {
				m.log.Info("Slave %s pushed %d items via WS", data.SlaveID, count)
			}

			// Reset read deadline
			setReadDeadline(conn, 60*time.Second, m.log)

		case msgTypeHeartbeat:
			var data wsHeartbeatData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				m.log.Warn("Invalid heartbeat data: %v", err)
				continue
			}
			// Enforce that heartbeats can only target the authenticated slave's own ID
			if sw.slaveID != "" && data.SlaveID != sw.slaveID {
				m.log.Warn("Heartbeat SlaveID mismatch: connection=%s message=%s", sw.slaveID, data.SlaveID)
				continue
			}
			if err := m.Heartbeat(data.SlaveID); err != nil {
				m.log.Warn("WS heartbeat failed for %s: %v", data.SlaveID, err)
			}

			// Reset read deadline
			setReadDeadline(conn, 60*time.Second, m.log)

		default:
			m.log.Debug("Unknown WS message type: %s", msg.Type)
		}
	}
}

// setSlaveWS stores a WebSocket connection for a slave.
func (m *Module) setSlaveWS(slaveID string, sw *slaveWS) {
	m.wsMu.Lock()
	defer m.wsMu.Unlock()
	// Close any existing connection
	if old, ok := m.wsConns[slaveID]; ok {
		old.conn.Close()
	}
	m.wsConns[slaveID] = sw
}

// removeSlaveWS removes a slave's WebSocket connection and marks it offline.
func (m *Module) removeSlaveWS(slaveID string) {
	m.wsMu.Lock()
	delete(m.wsConns, slaveID)
	m.wsMu.Unlock()

	// Mark slave as offline
	m.mu.Lock()
	if node, ok := m.slaves[slaveID]; ok {
		node.Status = "offline"
	}
	m.mu.Unlock()
}

// getSlaveWS returns the WebSocket connection for a slave, or nil.
func (m *Module) getSlaveWS(slaveID string) *slaveWS {
	m.wsMu.RLock()
	defer m.wsMu.RUnlock()
	return m.wsConns[slaveID]
}

// RequestStream sends a stream request to a slave via its WebSocket connection
// and waits for the slave to deliver the file data via HTTP POST.
// Returns a channel that will receive the delivery, or an error if the slave is not connected.
func (m *Module) RequestStream(slaveID, token, path, rangeHeader string) (*PendingStream, error) {
	sw := m.getSlaveWS(slaveID)
	if sw == nil {
		return nil, fmt.Errorf("slave %s is not connected via WebSocket", slaveID)
	}

	ps := &PendingStream{
		SlaveID:   slaveID,
		Path:      path,
		Range:     rangeHeader,
		Ready:     make(chan *StreamDelivery, 1),
		CreatedAt: time.Now(),
	}

	m.pendingMu.Lock()
	m.pendingStreams[token] = ps
	m.pendingMu.Unlock()

	// Send stream request to slave
	if err := sw.sendJSON(msgTypeStreamRequest, wsStreamRequestData{
		Token: token,
		Path:  path,
		Range: rangeHeader,
	}); err != nil {
		m.pendingMu.Lock()
		delete(m.pendingStreams, token)
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("failed to send stream request: %w", err)
	}

	return ps, nil
}

// DeliverStream is called when a slave POSTs file data to /api/receiver/stream-push/:token.
// It looks up the pending stream and signals the waiting proxy handler.
func (m *Module) DeliverStream(token string) (*PendingStream, bool) {
	m.pendingMu.Lock()
	ps, ok := m.pendingStreams[token]
	if ok {
		delete(m.pendingStreams, token)
	}
	m.pendingMu.Unlock()
	return ps, ok
}

// cleanupStalePending removes pending streams older than 30 seconds.
// Uses readyOnce to prevent double-close panic if the channel is closed elsewhere.
func (m *Module) cleanupStalePending() {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	for token, ps := range m.pendingStreams {
		if time.Since(ps.CreatedAt) > 30*time.Second {
			ps.readyOnce.Do(func() { close(ps.Ready) })
			delete(m.pendingStreams, token)
		}
	}
}
