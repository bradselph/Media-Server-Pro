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
		return origin == host
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

	// Connect to the downloader's WebSocket
	dlConn, _, err := websocket.DefaultDialer.Dial(dlURL, nil)
	if err != nil {
		log.Warn("Downloader WS dial failed: %v", err)
		adminConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"Cannot connect to downloader"}`))
		adminConn.Close()
		return
	}

	// Generate a clientId and register with the downloader
	clientID := "msp_" + time.Now().Format("20060102150405") + "_" + randomSuffix()
	registerMsg, _ := json.Marshal(map[string]string{
		"type":     "register",
		"clientId": clientID,
	})
	if err := dlConn.WriteMessage(websocket.TextMessage, registerMsg); err != nil {
		log.Warn("Failed to register clientId with downloader: %v", err)
		dlConn.Close()
		adminConn.Close()
		return
	}

	// Send the clientId to the admin so they can include it in download requests
	connectedMsg, _ := json.Marshal(map[string]string{
		"type":     "connected",
		"clientId": clientID,
	})
	if err := adminConn.WriteMessage(websocket.TextMessage, connectedMsg); err != nil {
		log.Warn("Failed to send connected message to admin: %v", err)
		dlConn.Close()
		adminConn.Close()
		return
	}

	log.Info("WS proxy established (clientId: %s)", clientID)

	// Bidirectional relay
	var once sync.Once
	done := make(chan struct{})
	closeAll := func() {
		once.Do(func() {
			close(done)
			adminConn.Close()
			dlConn.Close()
			log.Info("WS proxy closed (clientId: %s)", clientID)
		})
	}
	defer closeAll()

	// Admin → Downloader
	go func() {
		defer closeAll()
		for {
			msgType, data, err := adminConn.ReadMessage()
			if err != nil {
				return
			}
			if err := dlConn.WriteMessage(msgType, data); err != nil {
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
		if err := adminConn.WriteMessage(msgType, data); err != nil {
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
