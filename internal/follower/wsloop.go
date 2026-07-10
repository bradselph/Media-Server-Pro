package follower

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"media-server-pro/internal/config"
	"media-server-pro/internal/media"
	"media-server-pro/pkg/helpers"
)

// wsMessage is the envelope all follower↔master messages share.
// Identical shape to receiver.wsMessage on the master side.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// catalogItem mirrors receiver.CatalogItem (master side). Re-declared here so
// the follower package doesn't import internal/receiver and create a cycle.
type catalogItem struct {
	ID                 string    `json:"id"`
	Path               string    `json:"path"`
	Name               string    `json:"name"`
	MediaType          string    `json:"media_type"`
	Size               int64     `json:"size"`
	Duration           float64   `json:"duration"`
	ContentType        string    `json:"content_type"`
	ContentFingerprint string    `json:"content_fingerprint,omitempty"`
	Width              int       `json:"width"`
	Height             int       `json:"height"`
	Category           string    `json:"category,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
	BlurHash           string    `json:"blur_hash,omitempty"`
	DateAdded          time.Time `json:"date_added,omitzero"`
	DateModified       time.Time `json:"date_modified,omitzero"`
	IsMature           bool      `json:"is_mature,omitempty"`
}

// streamRequest is sent master → follower when a user wants to stream a file.
type streamRequest struct {
	Token string `json:"token"`
	Path  string `json:"path"`
	Range string `json:"range,omitempty"`
}

// thumbRequest is sent master → follower to fetch the thumbnail for a media
// item by the slave's local ID. The slave resolves it under its configured
// thumbnails directory; master never names a path so this can't be coerced
// into reading arbitrary files.
type thumbRequest struct {
	Token      string `json:"token"`
	RemoteID   string `json:"remote_id"`
	PreferWebP bool   `json:"prefer_webp,omitempty"`
}

// sameErrInfoEvery controls how often a repeating identical error is re-surfaced
// at INFO level after the first WARN. At the default 2-minute reconnect_max, 30
// repeats is roughly one hourly heartbeat so a stale/dead master pairing still
// shows up in operator logs without flooding the journal.
const sameErrInfoEvery = 30

// run is the top-level reconnect loop. It dials the master, runs one session,
// and on disconnect waits with exponential backoff before reconnecting. Exits
// only when ctx is canceled. Identical repeating errors are demoted to debug
// after the first occurrence to keep the journal readable when a paired master
// has been retired or temporarily moved.
func (m *Module) run(ctx context.Context) {
	cfg := m.config.Get().Follower
	baseDelay := cfg.ReconnectBase
	if baseDelay <= 0 {
		baseDelay = 2 * time.Second
	}
	maxDelay := cfg.ReconnectMax
	if maxDelay <= 0 {
		maxDelay = 2 * time.Minute
	}
	delay := baseDelay

	var lastErrMsg string
	var sameErrCount int

	for {
		if ctx.Err() != nil {
			return
		}

		err := m.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			msg := err.Error()
			if msg == lastErrMsg {
				sameErrCount++
				if sameErrCount%sameErrInfoEvery == 0 {
					m.log.Info("Follower still failing after %d consecutive attempts: %v", sameErrCount, err)
				} else {
					m.log.Debug("Follower session ended (repeat #%d): %v", sameErrCount, err)
				}
			} else {
				sameErrCount = 1
				lastErrMsg = msg
				m.log.Warn("Follower session ended: %v", err)
			}
			m.recordError(msg)
		} else {
			lastErrMsg = ""
			sameErrCount = 0
		}
		m.recordConnected(false)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		if err != nil {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		} else {
			delay = baseDelay
		}
	}
}

// connectAndRun opens one WS session to the master and processes messages
// until the connection drops or ctx is canceled. Returns the disconnect
// reason for logging/backoff.
func (m *Module) connectAndRun(ctx context.Context) error {
	cfg := m.config.Get().Follower
	wsURL, err := buildWSURL(cfg.MasterURL)
	if err != nil {
		return fmt.Errorf("invalid master URL: %w", err)
	}

	// NetDialContext re-validates the resolved IP at connection time (rejects
	// private/loopback/reserved), so a master hostname that passed the one-time
	// ValidateURLForSSRF check at save/test time can't later be DNS-rebound to an
	// internal address and receive the shared X-API-Key header below.
	dial := m.dialContext
	if dial == nil {
		dial = helpers.SafeDialContext
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		NetDialContext:   dial,
	}
	headers := http.Header{}
	headers.Set("X-API-Key", cfg.APIKey)

	if m.lastConnectAttemptOK() {
		m.log.Info("Connecting to master at %s", wsURL)
	} else {
		m.log.Debug("Connecting to master at %s", wsURL)
	}
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		// HTTP status on a failed websocket upgrade is the operator's biggest
		// clue: 200/HTML means the master URL points at a static frontend
		// (e.g. retired peer behind nginx), 401/403 means the API key is
		// stale, 404 means the master is on a path without the receiver
		// route mounted. Surface it instead of just "bad handshake".
		status := ""
		if resp != nil {
			status = fmt.Sprintf(" (HTTP %d)", resp.StatusCode)
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		return fmt.Errorf("websocket dial%s: %w", status, err)
	}
	defer func() { _ = conn.Close() }()
	m.log.Info("Connected to master %s as slave %s", cfg.MasterURL, m.resolveSlaveID(cfg))
	m.recordConnected(true)
	m.recordError("") // clear any previous error on successful connect

	const wsReadDeadline = 90 * time.Second
	_ = conn.SetReadDeadline(time.Now().Add(wsReadDeadline))

	var writeMu sync.Mutex
	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
		defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
		return conn.WriteMessage(websocket.PongMessage, []byte(data))
	})

	// 1. Register
	if err := sendJSON(conn, &writeMu, "register", map[string]string{
		"slave_id": m.resolveSlaveID(cfg),
		"name":     m.resolveSlaveName(cfg),
	}); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// 2. Initial catalog push (full).
	// Capture the catalog version BEFORE building so a mutation racing the build is
	// re-evaluated on the next tick rather than skipped.
	lastSeenVersion := m.catalogVersion()
	items := m.buildCatalog()
	if err := sendJSON(conn, &writeMu, "catalog", map[string]any{
		"slave_id": m.resolveSlaveID(cfg),
		"items":    items,
		"full":     true,
	}); err != nil {
		return fmt.Errorf("initial catalog push: %w", err)
	}
	m.recordCatalogPush(len(items))
	m.log.Info("Pushed %d items to master", len(items))
	// lastSentByID maps each pushed item ID to a hash of its master-visible fields,
	// so later ticks can push only the items that actually changed (a delta) instead
	// of re-marshaling and re-sending the whole catalog on every change.
	lastSentByID := catalogByID(items)
	var rawTicks int

	// 3. Read loop in a goroutine; main loop drives heartbeat + catalog timers.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	streamSem := make(chan struct{}, capOrDefault(cfg.MaxStreams, 10))
	readErr := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Go(func() {
		// Wait on streamCtx (a child of ctx) rather than ctx: every exit path calls
		// streamCancel() before wg.Wait(), and streamCtx is also canceled when ctx
		// is. Waiting on ctx alone would leave this goroutine blocked after a
		// streamCancel()-only error path, deadlocking wg.Wait().
		<-streamCtx.Done()
		_ = conn.Close()
	})

	wg.Go(func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if streamCtx.Err() == nil {
					readErr <- err
				}
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(wsReadDeadline))

			var msg wsMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				m.log.Warn("Invalid message from master: %v", err)
				continue
			}
			switch msg.Type {
			case "stream_request":
				var req streamRequest
				if err := json.Unmarshal(msg.Data, &req); err != nil {
					m.log.Warn("Invalid stream_request: %v", err)
					continue
				}
				wg.Go(func() {
					select {
					case <-streamCtx.Done():
						return
					case streamSem <- struct{}{}:
						defer func() { <-streamSem }()
						m.deliverStream(streamCtx, cfg, req)
					}
				})
			case "thumb_request":
				var req thumbRequest
				if err := json.Unmarshal(msg.Data, &req); err != nil {
					m.log.Warn("Invalid thumb_request: %v", err)
					continue
				}
				wg.Go(func() {
					select {
					case <-streamCtx.Done():
						return
					case streamSem <- struct{}{}:
						defer func() { <-streamSem }()
						m.deliverThumbnail(streamCtx, cfg, req)
					}
				})
			default:
				m.log.Debug("Unknown message type from master: %q", msg.Type)
			}
		}
	})

	scanInterval := capDuration(cfg.ScanInterval, 5*time.Minute)
	heartbeatInterval := capDuration(cfg.HeartbeatInterval, 15*time.Second)
	scanTicker := time.NewTicker(scanInterval)
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer scanTicker.Stop()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-scanTicker.C:
			rawTicks++
			// Every fullResyncEveryTicks, force a full catalog replace as a self-heal
			// against any drift (a dropped delta, a master restart) even if only
			// additions/changes occurred since the last push.
			forceFull := rawTicks%fullResyncEveryTicks == 0

			// #24: skip the O(catalog) rebuild+diff entirely when the media module's
			// monotonic version hasn't advanced since we last processed it — the
			// steady-state common case — unless a periodic full resync is due.
			v := m.catalogVersion()
			if v == lastSeenVersion && !forceFull {
				continue
			}
			lastSeenVersion = v

			current := m.buildCatalog()
			currentByID := catalogByID(current)

			// Items whose hashed fields differ from what we last sent (new or changed).
			var changed []*catalogItem
			for _, it := range current {
				if lastSentByID[it.ID] != currentByID[it.ID] {
					changed = append(changed, it)
				}
			}
			// A removal (item present last time, gone now) can only be reflected by a
			// full replace: the master's incremental path upserts and never deletes.
			removed := false
			for id := range lastSentByID {
				if _, ok := currentByID[id]; !ok {
					removed = true
					break
				}
			}

			switch {
			case removed || forceFull:
				if err := sendJSON(conn, &writeMu, "catalog", map[string]any{
					"slave_id": m.resolveSlaveID(cfg),
					"items":    current,
					"full":     true,
				}); err != nil {
					streamCancel()
					wg.Wait()
					return fmt.Errorf("catalog push: %w", err)
				}
				m.recordCatalogPush(len(current))
				m.log.Info("Re-pushed full catalog (%d items) to master", len(current))
			case len(changed) > 0:
				// Incremental upsert: send only the changed items. The master merges
				// these via UpsertBatch without touching the rest of the catalog.
				if err := sendJSON(conn, &writeMu, "catalog", map[string]any{
					"slave_id": m.resolveSlaveID(cfg),
					"items":    changed,
					"full":     false,
				}); err != nil {
					streamCancel()
					wg.Wait()
					return fmt.Errorf("catalog delta push: %w", err)
				}
				m.recordCatalogPush(len(changed))
				m.log.Info("Pushed %d changed item(s) to master", len(changed))
			default:
				// Version advanced but no master-visible field changed — nothing to send.
			}
			lastSentByID = currentByID

		case <-heartbeatTicker.C:
			if err := sendJSON(conn, &writeMu, "heartbeat", map[string]string{
				"slave_id": m.resolveSlaveID(cfg),
			}); err != nil {
				streamCancel()
				wg.Wait()
				return fmt.Errorf("heartbeat: %w", err)
			}

		case err := <-readErr:
			streamCancel()
			wg.Wait()
			return fmt.Errorf("websocket read: %w", err)

		case <-ctx.Done():
			streamCancel()
			writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
			_ = conn.Close()
			writeMu.Unlock()
			wg.Wait()
			return nil
		}
	}
}

// catalogVersion returns the media module's monotonic catalog version, or 0 when
// no media module is wired (e.g. in tests). Used to cheaply skip rebuilding the
// catalog on a scan tick when nothing has changed.
func (m *Module) catalogVersion() int64 {
	if m.media == nil {
		return 0
	}
	return m.media.GetVersion()
}

// buildCatalog snapshots the local media library and converts it into the
// receiver.CatalogItem shape the master expects. Paths are made relative to
// one of the configured media directories so the master's path-traversal
// guard accepts them; absolute paths and ".." segments are rejected master-side.
func (m *Module) buildCatalog() []*catalogItem {
	if m.media == nil {
		return nil
	}
	cfg := m.config.Get()

	// Allowed roots: anything declared as a media root in config. The follower
	// only exposes media that lives under one of these dirs — items outside
	// (e.g. uploaded files in a tmp area) are skipped to keep the path-resolve
	// path on the receive side simple and safe.
	roots := collectMediaRoots(cfg.Directories)
	if len(roots) == 0 {
		return nil
	}

	items := m.media.ListMedia(media.Filter{})
	out := make([]*catalogItem, 0, len(items))
	for _, item := range items {
		if item == nil || item.Path == "" {
			continue
		}
		relPath, ok := relativizeUnderRoot(item.Path, roots)
		if !ok {
			continue
		}
		fp := m.media.GetContentFingerprint(item.Path)

		out = append(out, &catalogItem{
			ID:                 item.ID,
			Path:               relPath,
			Name:               item.Name,
			MediaType:          string(item.Type),
			Size:               item.Size,
			Duration:           item.Duration,
			ContentType:        helpers.MediaContentType(item.Name),
			ContentFingerprint: fp,
			Width:              item.Width,
			Height:             item.Height,
			Category:           item.Category,
			Tags:               item.Tags,
			BlurHash:           item.BlurHash,
			DateAdded:          item.DateAdded,
			DateModified:       item.DateModified,
			IsMature:           item.IsMature,
		})
	}
	return out
}

// fullResyncEveryTicks is how many scan ticks pass between forced full-catalog
// resyncs. At the default 5-minute scan interval this is roughly hourly — often
// enough to self-heal any drift from a dropped delta or a master restart, rare
// enough that the steady state is cheap incremental (or skipped) pushes.
const fullResyncEveryTicks = 12

// itemHash hashes the master-visible fields of one catalog item (the same fields
// the previous whole-catalog hash compared). Used to detect which items changed
// between scans so only the delta is pushed.
func itemHash(it *catalogItem) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%d|%s", it.Path, it.Name, it.Size, it.ContentFingerprint)
	return hex.EncodeToString(h.Sum(nil))
}

// catalogByID builds an item-ID → item-hash map for delta detection.
func catalogByID(items []*catalogItem) map[string]string {
	byID := make(map[string]string, len(items))
	for _, it := range items {
		byID[it.ID] = itemHash(it)
	}
	return byID
}

// wsWriteDeadline bounds how long any single WebSocket frame can take to
// flush before we give up. Without this WriteJSON has no inherent timeout
// and a stalled master can hang the follower's writer goroutine forever.
const wsWriteDeadline = 10 * time.Second

// sendJSON serializes a typed message and writes it under writeMu so multiple
// goroutines never interleave writes on the same WS connection.
func sendJSON(conn *websocket.Conn, writeMu *sync.Mutex, msgType string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	msg := wsMessage{Type: msgType, Data: raw}
	writeMu.Lock()
	defer writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
	defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
	return conn.WriteJSON(msg)
}

// buildWSURL converts an http(s) master URL to a ws(s) URL for /ws/receiver.
func buildWSURL(masterURL string) (string, error) {
	u, err := url.Parse(strings.TrimRight(masterURL, "/"))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("master URL has no host")
	}
	u.Path = "/ws/receiver"
	u.RawQuery = ""
	return u.String(), nil
}

func capOrDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func capDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// collectMediaRoots returns the absolute, symlink-resolved paths that the
// follower is willing to expose. Mirrors the slave's "allowed dirs" semantics.
func collectMediaRoots(dirs config.DirectoriesConfig) []string {
	var roots []string
	if dirs.Videos != "" {
		roots = append(roots, dirs.Videos)
	}
	if dirs.Music != "" {
		roots = append(roots, dirs.Music)
	}
	return roots
}
