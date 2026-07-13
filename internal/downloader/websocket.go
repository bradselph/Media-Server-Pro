package downloader

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"media-server-pro/internal/logger"
)

// wsRelayWriteDeadline bounds how long any WebSocket frame can take to flush
// before we give up. Without it WriteMessage can hang the proxy goroutine
// indefinitely when one side stops reading (TCP backpressure on a stuck peer).
const wsRelayWriteDeadline = 10 * time.Second

// writeMessageWithDeadline applies a write deadline so a stalled peer cannot
// hang the proxy goroutine on WriteMessage.
func writeMessageWithDeadline(conn *websocket.Conn, msgType int, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(wsRelayWriteDeadline))
	defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
	return conn.WriteMessage(msgType, data)
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Non-browser clients (curl, etc.) don't send Origin
		}
		// Validate Origin matches the Host header to prevent cross-site WebSocket hijacking
		host := r.Host
		if host == "" {
			host = r.Header.Get("Host")
		}
		// Strip scheme from origin to compare with host
		origin = strings.TrimPrefix(origin, "https://")
		origin = strings.TrimPrefix(origin, "http://")
		return strings.EqualFold(origin, host)
	},
}

// HandleWebSocket upgrades an admin HTTP connection to WebSocket and proxies
// messages bidirectionally to the downloader's WebSocket. This enables
// real-time download progress in the MSP admin panel.
func (m *Module) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	log := logger.New("downloader-ws")

	cfg := m.config.Get()
	if !cfg.Downloader.Enabled {
		http.Error(w, "Downloader is disabled", http.StatusServiceUnavailable)
		return
	}

	if !m.IsOnline() {
		http.Error(w, "Downloader service is offline", http.StatusServiceUnavailable)
		return
	}

	// Upgrade the admin connection
	adminConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn("Admin WS upgrade failed: %v", err)
		return
	}

	// Construct the downloader WS URL from the HTTP base URL
	dlURL := cfg.Downloader.URL
	dlURL = strings.Replace(dlURL, "https://", "wss://", 1)
	dlURL = strings.Replace(dlURL, "http://", "ws://", 1)

	// Connect to the downloader's WebSocket (10s handshake timeout)
	wsDialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	dlConn, httpResp, err := wsDialer.Dial(dlURL, nil)
	if err != nil {
		if httpResp != nil {
			_ = httpResp.Body.Close()
		}
		log.Warn("Downloader WS dial failed: %v", err)
		_ = writeMessageWithDeadline(adminConn, websocket.TextMessage, []byte(`{"type":"error","message":"Cannot connect to downloader"}`))
		_ = adminConn.Close()
		return
	}

	// Generate a clientId and register with the downloader
	clientID := "msp_" + time.Now().Format("20060102150405") + "_" + randomSuffix()
	registerMsg, err := json.Marshal(map[string]string{
		"type":     "register",
		"clientId": clientID,
	})
	if err != nil {
		log.Warn("Failed to marshal register message: %v", err)
		_ = dlConn.Close()
		_ = adminConn.Close()
		return
	}
	if err := writeMessageWithDeadline(dlConn, websocket.TextMessage, registerMsg); err != nil {
		log.Warn("Failed to register clientId with downloader: %v", err)
		_ = dlConn.Close()
		_ = adminConn.Close()
		return
	}

	// Send the clientId to the admin so they can include it in download requests
	connectedMsg, err := json.Marshal(map[string]string{
		"type":     "connected",
		"clientId": clientID,
	})
	if err != nil {
		log.Warn("Failed to marshal connected message: %v", err)
		_ = dlConn.Close()
		_ = adminConn.Close()
		return
	}
	if err := writeMessageWithDeadline(adminConn, websocket.TextMessage, connectedMsg); err != nil {
		log.Warn("Failed to send connected message to admin: %v", err)
		_ = dlConn.Close()
		_ = adminConn.Close()
		return
	}

	// Limit incoming message size on both connections to prevent memory exhaustion.
	const wsRelayReadLimit = 1 * 1024 * 1024 // 1 MB
	const wsRelayReadDeadline = 60 * time.Second
	adminConn.SetReadLimit(wsRelayReadLimit)
	dlConn.SetReadLimit(wsRelayReadLimit)
	// Set initial read deadlines; pong handlers extend them to prevent stalled-client goroutine leaks.
	_ = adminConn.SetReadDeadline(time.Now().Add(wsRelayReadDeadline))
	_ = dlConn.SetReadDeadline(time.Now().Add(wsRelayReadDeadline))
	adminConn.SetPongHandler(func(string) error {
		return adminConn.SetReadDeadline(time.Now().Add(wsRelayReadDeadline))
	})
	dlConn.SetPongHandler(func(string) error {
		return dlConn.SetReadDeadline(time.Now().Add(wsRelayReadDeadline))
	})

	log.Info("WS proxy established (clientId: %s)", clientID)

	// Bidirectional relay
	var once sync.Once
	done := make(chan struct{})
	closeAll := func() {
		once.Do(func() {
			close(done)
			_ = adminConn.Close()
			_ = dlConn.Close()
			log.Info("WS proxy closed (clientId: %s)", clientID)
		})
	}
	defer closeAll()

	// Keepalive: nothing in this relay ever sends Ping frames, so the PongHandlers
	// above would never fire and both read deadlines would expire ~60s after
	// connect — killing every download's progress WS mid-transfer (a paused/quiet
	// download sends no data to refresh the deadline on its own). Send periodic
	// Pings well under wsRelayReadDeadline so the PongHandlers refresh the
	// deadlines. WriteControl is safe to call concurrently with the relay writers.
	const wsRelayPingInterval = wsRelayReadDeadline / 2
	go func() {
		ticker := time.NewTicker(wsRelayPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				deadline := time.Now().Add(wsRelayWriteDeadline)
				if err := adminConn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
					closeAll()
					return
				}
				if err := dlConn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
					closeAll()
					return
				}
			}
		}
	}()

	// Admin → Downloader
	go func() {
		defer closeAll()
		for {
			msgType, data, err := adminConn.ReadMessage()
			if err != nil {
				return
			}
			// Belt-and-suspenders: any received frame is proof of life, so extend
			// this side's read deadline directly (in addition to the ping/pong).
			_ = adminConn.SetReadDeadline(time.Now().Add(wsRelayReadDeadline))
			if err := writeMessageWithDeadline(dlConn, msgType, data); err != nil {
				return
			}
		}
	}()

	// Downloader → Admin
	for {
		msgType, data, err := dlConn.ReadMessage()
		if err != nil {
			return
		}
		_ = dlConn.SetReadDeadline(time.Now().Add(wsRelayReadDeadline))
		if err := writeMessageWithDeadline(adminConn, msgType, data); err != nil {
			return
		}
	}
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			// Fallback to time-based if crypto/rand fails (should never happen)
			b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
			continue
		}
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
