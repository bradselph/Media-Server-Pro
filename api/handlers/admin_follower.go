// admin_follower.go exposes the follower (this-server-as-slave) pairing
// settings to the admin UI. Mirrors the receiver/remote admin handlers.
package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"media-server-pro/internal/analytics"
	"media-server-pro/internal/config"
)

// followerSettingsResponse is the JSON shape returned by
// GET /api/admin/follower/settings. The api_key is intentionally redacted —
// admins can see whether a key is configured but not the key itself, so that
// shoulder-surfing the admin panel doesn't leak it.
type followerSettingsResponse struct {
	Enabled           bool   `json:"enabled"`
	MasterURL         string `json:"master_url"`
	APIKeyConfigured  bool   `json:"api_key_configured"`
	SlaveID           string `json:"slave_id"`
	SlaveName         string `json:"slave_name"`
	ScanIntervalSecs  int    `json:"scan_interval_seconds"`
	HeartbeatSecs     int    `json:"heartbeat_interval_seconds"`
	MaxStreams        int    `json:"max_streams"`
	ReconnectBaseSecs int    `json:"reconnect_base_seconds"`
	ReconnectMaxSecs  int    `json:"reconnect_max_seconds"`
}

// followerSettingsRequest is the JSON body for POST /api/admin/follower/settings.
// APIKey is optional on update — leaving it empty preserves the existing key
// so admins can change other fields without re-entering the secret.
type followerSettingsRequest struct {
	Enabled           bool   `json:"enabled"`
	MasterURL         string `json:"master_url"`
	APIKey            string `json:"api_key"`
	SlaveID           string `json:"slave_id"`
	SlaveName         string `json:"slave_name"`
	ScanIntervalSecs  int    `json:"scan_interval_seconds"`
	HeartbeatSecs     int    `json:"heartbeat_interval_seconds"`
	MaxStreams        int    `json:"max_streams"`
	ReconnectBaseSecs int    `json:"reconnect_base_seconds"`
	ReconnectMaxSecs  int    `json:"reconnect_max_seconds"`
}

// GetFollowerSettings returns the current follower pairing configuration with
// the API key redacted. Admin only.
func (h *Handler) GetFollowerSettings(c *gin.Context) {
	if h.follower == nil {
		writeError(c, http.StatusServiceUnavailable, "Follower module not available")
		return
	}
	cfg := h.config.Get().Follower
	writeSuccess(c, followerSettingsResponse{
		Enabled:           cfg.Enabled,
		MasterURL:         cfg.MasterURL,
		APIKeyConfigured:  strings.TrimSpace(cfg.APIKey) != "",
		SlaveID:           cfg.SlaveID,
		SlaveName:         cfg.SlaveName,
		ScanIntervalSecs:  int(cfg.ScanInterval.Seconds()),
		HeartbeatSecs:     int(cfg.HeartbeatInterval.Seconds()),
		MaxStreams:        cfg.MaxStreams,
		ReconnectBaseSecs: int(cfg.ReconnectBase.Seconds()),
		ReconnectMaxSecs:  int(cfg.ReconnectMax.Seconds()),
	})
}

// UpdateFollowerSettings persists pairing changes and reloads the WS loop so
// they take effect without a server restart. Admin only.
func (h *Handler) UpdateFollowerSettings(c *gin.Context) {
	if h.follower == nil {
		writeError(c, http.StatusServiceUnavailable, "Follower module not available")
		return
	}
	var req followerSettingsRequest
	if !BindJSON(c, &req, errInvalidRequest) {
		return
	}

	masterURL := strings.TrimSpace(req.MasterURL)
	if masterURL != "" {
		u, err := url.Parse(masterURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(c, http.StatusBadRequest, "master_url must be a valid http(s) URL")
			return
		}
	}

	if err := h.config.Update(func(cfg *config.Config) {
		// Enabled is auto-derived from configuration completeness: pairing turns
		// on as soon as both master_url and api_key are populated, off otherwise.
		// The form's req.Enabled is intentionally ignored.
		cfg.Follower.MasterURL = masterURL
		// Empty api_key in the request means "keep existing" — required so
		// admins can edit settings without re-typing the secret every time.
		if newKey := strings.TrimSpace(req.APIKey); newKey != "" {
			cfg.Follower.APIKey = newKey
		}
		cfg.Follower.SlaveID = strings.TrimSpace(req.SlaveID)
		cfg.Follower.SlaveName = strings.TrimSpace(req.SlaveName)
		if req.ScanIntervalSecs > 0 {
			cfg.Follower.ScanInterval = time.Duration(req.ScanIntervalSecs) * time.Second
		}
		if req.HeartbeatSecs > 0 {
			cfg.Follower.HeartbeatInterval = time.Duration(req.HeartbeatSecs) * time.Second
		}
		if req.MaxStreams > 0 {
			cfg.Follower.MaxStreams = req.MaxStreams
		}
		if req.ReconnectBaseSecs > 0 {
			cfg.Follower.ReconnectBase = time.Duration(req.ReconnectBaseSecs) * time.Second
		}
		if req.ReconnectMaxSecs > 0 {
			cfg.Follower.ReconnectMax = time.Duration(req.ReconnectMaxSecs) * time.Second
		}
		cfg.Follower.Enabled = cfg.Follower.MasterURL != "" && strings.TrimSpace(cfg.Follower.APIKey) != ""
	}); err != nil {
		writeError(c, http.StatusInternalServerError, "Failed to save settings: "+err.Error())
		return
	}

	// Reload the follower loop so the new settings take effect immediately.
	// Bound the wait so a stuck loop can't hang the admin request.
	reloadCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := h.follower.Reload(reloadCtx); err != nil {
		// Settings are saved; only the reload failed. Surface a 200 with a
		// non-error reload note so the UI can show the status without
		// implying the save itself failed.
		h.trackServerEvent(c, analytics.EventFollowerSettingsUpdate, map[string]any{
			"master_url":   masterURL,
			"reload_error": err.Error(),
		})
		writeSuccess(c, gin.H{
			"saved":         true,
			"reload_error":  err.Error(),
			"reload_status": h.follower.GetStatus(),
		})
		return
	}
	h.trackServerEvent(c, analytics.EventFollowerSettingsUpdate, map[string]any{
		"master_url": masterURL,
	})
	writeSuccess(c, gin.H{
		"saved":         true,
		"reload_status": h.follower.GetStatus(),
	})
}

// GetFollowerStatus returns the live pairing status (connected, last error,
// last catalog push, etc.) for the admin UI.
func (h *Handler) GetFollowerStatus(c *gin.Context) {
	if h.follower == nil {
		writeError(c, http.StatusServiceUnavailable, "Follower module not available")
		return
	}
	writeSuccess(c, h.follower.GetStatus())
}

// followerTestRequest carries a one-shot connection test from the UI. Lets
// admins validate URL + key before saving the settings.
type followerTestRequest struct {
	MasterURL string `json:"master_url"`
	APIKey    string `json:"api_key"`
}

// TestFollowerPairing opens a one-shot WebSocket dial against the supplied
// master URL with the supplied API key and reports whether the handshake
// succeeded. Used by the admin UI's "Test connection" button so admins can
// verify pairing credentials before flipping Enabled to true.
func (h *Handler) TestFollowerPairing(c *gin.Context) {
	var req followerTestRequest
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
	if err != nil || u.Host == "" {
		writeError(c, http.StatusBadRequest, "master_url must be a valid URL")
		return
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		writeError(c, http.StatusBadRequest, "master_url scheme must be http or https")
		return
	}
	u.Path = "/ws/receiver"
	u.RawQuery = ""

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	headers := http.Header{}
	headers.Set("X-API-Key", apiKey)

	conn, resp, err := dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
		}
		writeSuccess(c, gin.H{
			"ok":          false,
			"error":       err.Error(),
			"http_status": status,
		})
		return
	}
	_ = conn.Close()
	writeSuccess(c, gin.H{"ok": true})
}
