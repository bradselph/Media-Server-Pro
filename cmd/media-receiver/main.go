// media-receiver is a slave node that scans local media directories, connects to
// a Media Server Pro master instance via WebSocket, and pushes the catalog so the
// master can proxy media streams to users on demand. No public IP or port forwarding
// is needed — the slave initiates all connections.
//
// Usage:
//
//	media-receiver -master https://yourdomain.com -api-key YOUR_KEY -dirs ./videos,./music
//
// When run from the project root with no arguments, missing values are auto-discovered
// from local config files (.deploy.env, .slave.env, .env, config.json). Explicit
// flags and environment variables always take priority.
//
// Environment variables (override flags):
//
//	MASTER_URL          — master server URL
//	RECEIVER_API_KEY    — API key for authentication
//	SLAVE_ID            — unique identifier for this slave
//	SLAVE_NAME          — display name for this slave
//	MEDIA_DIRS          — comma-separated list of media directories
//	SCAN_INTERVAL       — catalog rescan interval (e.g. "5m", "1h")
//	HEARTBEAT_INTERVAL  — keepalive ping interval (e.g. "15s", "30s")
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// catalogItem matches the receiver.CatalogItem struct on the master.
type catalogItem struct {
	ID                 string  `json:"id"`
	Path               string  `json:"path"`
	Name               string  `json:"name"`
	MediaType          string  `json:"media_type"`
	Size               int64   `json:"size"`
	Duration           float64 `json:"duration"`
	ContentType        string  `json:"content_type"`
	ContentFingerprint string  `json:"content_fingerprint,omitempty"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
}

// wsMessage is the JSON envelope for WebSocket messages.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// streamRequest is sent master → slave when a user wants to stream a file.
type streamRequest struct {
	Token string `json:"token"`
	Path  string `json:"path"`
	Range string `json:"range,omitempty"`
}

type slaveConfig struct {
	MasterURL         string
	APIKey            string
	SlaveID           string
	SlaveName         string
	MediaDirs         []string
	ScanInterval      time.Duration
	HeartbeatInterval time.Duration
}

// fingerprint cache: avoid recomputing SHA-256 for unchanged files.
// key = absolute path, value = cached result.
type fpCacheEntry struct {
	modTime     time.Time
	size        int64
	fingerprint string
}

var (
	fpCache   = make(map[string]fpCacheEntry)
	fpCacheMu sync.Mutex
)

// getCachedFingerprint returns a cached fingerprint if the file hasn't changed
// (same mtime + size), or computes, caches, and returns a fresh one.
func getCachedFingerprint(path string, info os.FileInfo) string {
	fpCacheMu.Lock()
	defer fpCacheMu.Unlock()

	if cached, ok := fpCache[path]; ok {
		if cached.modTime.Equal(info.ModTime()) && cached.size == info.Size() {
			return cached.fingerprint
		}
	}

	fp := computeContentFingerprint(path)
	fpCache[path] = fpCacheEntry{
		modTime:     info.ModTime(),
		size:        info.Size(),
		fingerprint: fp,
	}
	return fp
}

// pruneFpCache removes cache entries for paths that no longer exist (files deleted since last scan).
func pruneFpCache(keep map[string]bool) {
	fpCacheMu.Lock()
	defer fpCacheMu.Unlock()
	for path := range fpCache {
		if !keep[path] {
			delete(fpCache, path)
		}
	}
}

// streamSem limits concurrent stream deliveries to avoid goroutine/file descriptor exhaustion.
const maxConcurrentStreams = 10

var streamSem = make(chan struct{}, maxConcurrentStreams)

// streamHTTPClient is reused for stream deliveries (default TLS; no request timeout).
var streamHTTPClient = &http.Client{
	Timeout: 0, // no overall timeout for streaming
	Transport: &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// lastCatalogHash stores a SHA-256 of the last pushed catalog to skip redundant pushes.
var lastCatalogHash string

func main() {
	cfg, disc := parseFlags()

	if cfg.MasterURL == "" {
		fmt.Fprintln(os.Stderr, "Error: master URL is required (-master or MASTER_URL)")
		fmt.Fprintln(os.Stderr, "  Tip: run from the project root where .env or config.json lives for auto-discovery")
		os.Exit(1)
	}
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: API key is required (-api-key or RECEIVER_API_KEY)")
		fmt.Fprintln(os.Stderr, "  Tip: run from the project root where .deploy.env or .env lives for auto-discovery")
		os.Exit(1)
	}
	if len(cfg.MediaDirs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: at least one media directory required (-dirs or MEDIA_DIRS)")
		fmt.Fprintln(os.Stderr, "  Tip: run from the project root where .env or config.json lives for auto-discovery")
		os.Exit(1)
	}

	autoTag := func(discovered bool) string {
		if discovered {
			return " [auto]"
		}
		return ""
	}

	fmt.Printf("Media Receiver (Slave) starting\n")
	fmt.Printf("  Master:     %s%s\n", cfg.MasterURL, autoTag(disc.MasterURL))
	keyPreview := cfg.APIKey
	if len(keyPreview) > 4 {
		keyPreview = keyPreview[:4] + "..."
	}
	fmt.Printf("  API Key:    %s%s\n", keyPreview, autoTag(disc.APIKey))
	fmt.Printf("  Slave ID:   %s\n", cfg.SlaveID)
	fmt.Printf("  Name:       %s\n", cfg.SlaveName)
	fmt.Printf("  Media:      %s%s\n", strings.Join(cfg.MediaDirs, ", "), autoTag(disc.MediaDirs))
	fmt.Printf("  Scan:       %s\n", cfg.ScanInterval)
	fmt.Printf("  Heartbeat:  %s\n", cfg.HeartbeatInterval)
	if disc.MasterURL || disc.APIKey || disc.MediaDirs {
		fmt.Printf("  [auto] = auto-discovered from local config files\n")
	}
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("\nReceived %s, shutting down...\n", sig)
		cancel()
	}()

	// Run the WebSocket connection loop — reconnects automatically
	runSlaveLoop(ctx, cfg)
}

// runSlaveLoop connects to the master via WebSocket and handles all communication.
// If the connection drops, it waits and reconnects with exponential backoff.
func runSlaveLoop(ctx context.Context, cfg *slaveConfig) {
	const (
		baseDelay = 2 * time.Second
		maxDelay  = 2 * time.Minute
	)
	delay := baseDelay

	for {
		if ctx.Err() != nil {
			return
		}

		err := connectAndRun(ctx, cfg)
		if ctx.Err() != nil {
			return // shutdown requested
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "WebSocket connection error: %v\n", err)
			fmt.Fprintf(os.Stderr, "Reconnecting in %v...\n", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		} else {
			delay = baseDelay
		}
	}
}

// connectAndRun establishes one WebSocket connection, sends registration + catalog,
// and runs the heartbeat/scan/stream-request loop until the connection drops.
func connectAndRun(ctx context.Context, cfg *slaveConfig) error {
	wsURL := buildWSURL(cfg.MasterURL, cfg.APIKey)
	fmt.Printf("Connecting to %s...\n", maskKey(wsURL))

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}
	defer conn.Close()
	fmt.Println("Connected to master via WebSocket")

	// Set read deadline — extended on every incoming message/ping.
	// If no data arrives within 90s (3x the master's 25s ping interval),
	// the connection is considered dead and we reconnect.
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	var writeMu sync.Mutex
	conn.SetPingHandler(func(data string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.PongMessage, []byte(data))
	})

	// Send registration
	if err := sendWSJSON(conn, "register", map[string]string{
		"slave_id": cfg.SlaveID,
		"name":     cfg.SlaveName,
	}); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	fmt.Println("Registered with master")

	// Initial scan and catalog push (always push on connect)
	items := scanMediaDirs(cfg.MediaDirs)
	fmt.Printf("Found %d media files\n", len(items))
	if err := sendWSJSON(conn, "catalog", map[string]interface{}{
		"slave_id": cfg.SlaveID,
		"items":    items,
		"full":     true,
	}); err != nil {
		return fmt.Errorf("catalog push failed: %w", err)
	}
	lastCatalogHash = hashCatalog(items)
	fmt.Printf("Pushed %d items to master\n", len(items))

	// Start reading stream requests from master in a goroutine
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	var wg sync.WaitGroup
	readErr := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if streamCtx.Err() == nil {
					readErr <- err
				}
				return
			}
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))

			var msg wsMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid message from master: %v\n", err)
				continue
			}

			if msg.Type == "stream_request" {
				var req streamRequest
				if err := json.Unmarshal(msg.Data, &req); err != nil {
					fmt.Fprintf(os.Stderr, "Invalid stream request: %v\n", err)
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					select {
					case streamSem <- struct{}{}:
						defer func() { <-streamSem }()
						deliverStream(streamCtx, cfg, req)
					case <-streamCtx.Done():
						return
					}
				}()
			}
		}
	}()

	// Heartbeat and scan tickers
	scanTicker := time.NewTicker(cfg.ScanInterval)
	heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer scanTicker.Stop()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-scanTicker.C:
			items := scanMediaDirs(cfg.MediaDirs)
			h := hashCatalog(items)
			if h == lastCatalogHash {
				// Catalog unchanged since last push — skip redundant full push.
				continue
			}
			writeMu.Lock()
			err := sendWSJSON(conn, "catalog", map[string]interface{}{
				"slave_id": cfg.SlaveID,
				"items":    items,
				"full":     true,
			})
			writeMu.Unlock()
			if err != nil {
				return fmt.Errorf("catalog push failed: %w", err)
			}
			lastCatalogHash = h
			fmt.Printf("[%s] Pushed %d items to master\n", time.Now().Format("15:04:05"), len(items))

		case <-heartbeatTicker.C:
			writeMu.Lock()
			err := sendWSJSON(conn, "heartbeat", map[string]string{
				"slave_id": cfg.SlaveID,
			})
			writeMu.Unlock()
			if err != nil {
				return fmt.Errorf("heartbeat failed: %w", err)
			}

		case err := <-readErr:
			streamCancel()
			wg.Wait()
			return fmt.Errorf("WebSocket read error: %w", err)

		case <-ctx.Done():
			streamCancel()
			writeMu.Lock()
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
			writeMu.Unlock()
			wg.Wait()
			return nil
		}
	}
}

// deliverStream reads a local file and POSTs it to the master's stream-push endpoint.
func deliverStream(ctx context.Context, cfg *slaveConfig, req streamRequest) {
	// Resolve and validate path
	absPath, err := resolveAndValidate(req.Path, cfg.MediaDirs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Stream request denied (path %q): %v\n", req.Path, err)
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file for stream %s: %v\n", req.Token, err)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to stat file for stream %s: %v\n", req.Token, err)
		return
	}

	// Determine content type
	contentType := mime.TypeByExtension(filepath.Ext(absPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var body io.Reader = file
	statusCode := 200
	contentLength := stat.Size()
	var extraHeaders map[string]string

	// Handle Range requests — on parse or seek failure, deliver full file as 200 (explicit fallback).
	if req.Range != "" {
		start, end, err := parseRange(req.Range, stat.Size())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid range header %q for %s: %v; delivering full file\n", req.Range, req.Token, err)
			// body, statusCode, contentLength, extraHeaders remain full-file 200
		} else if _, seekErr := file.Seek(start, io.SeekStart); seekErr != nil {
			fmt.Fprintf(os.Stderr, "Seek failed for stream %s: %v; delivering full file\n", req.Token, seekErr)
			file.Seek(0, io.SeekStart) // reset to start for full-file delivery
			// body, statusCode, contentLength, extraHeaders remain full-file 200
		} else {
			length := end - start + 1
			body = io.LimitReader(file, length)
			statusCode = 206
			contentLength = length
			extraHeaders = map[string]string{
				"Content-Range": fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size()),
				"Accept-Ranges": "bytes",
			}
		}
	}

	// Build the push URL
	pushURL := strings.TrimRight(cfg.MasterURL, "/") + "/api/receiver/stream-push/" + req.Token

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pushURL, body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create push request for %s: %v\n", req.Token, err)
		return
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("X-API-Key", cfg.APIKey)
	httpReq.Header.Set("X-Stream-Status", fmt.Sprintf("%d", statusCode))
	httpReq.ContentLength = contentLength

	for k, v := range extraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := streamHTTPClient.Do(httpReq)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "Stream push failed for %s: %v\n", req.Token, err)
		}
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "Stream push %s returned HTTP %d\n", req.Token, resp.StatusCode)
	}
}

// parseRange parses a "bytes=start-end" Range header value.
func parseRange(rangeHeader string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("unsupported range format")
	}
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(rangeSpec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range")
	}

	var start, end int64

	if parts[0] == "" {
		// Suffix range: -500 means last 500 bytes
		n, err := parseInt64(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid suffix range: %w", err)
		}
		start = fileSize - n
		if start < 0 {
			start = 0
		}
		end = fileSize - 1
	} else if parts[1] == "" {
		// Open-ended: 500- means from byte 500 to end
		var err error
		start, err = parseInt64(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start: %w", err)
		}
		end = fileSize - 1
	} else {
		var err error
		start, err = parseInt64(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start: %w", err)
		}
		end, err = parseInt64(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end: %w", err)
		}
	}

	if start < 0 || start >= fileSize || end < start || end >= fileSize {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

func parseInt64(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n, err
}

// sendWSJSON sends a typed JSON message over the WebSocket (callers must serialize via writeMu where needed).
func sendWSJSON(conn *websocket.Conn, msgType string, data interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	msg := wsMessage{Type: msgType, Data: raw}
	return conn.WriteJSON(msg)
}

// buildWSURL converts a master HTTP URL to a WebSocket URL (api_key in query; master also supports X-API-Key header).
func buildWSURL(masterURL, apiKey string) string {
	u, err := url.Parse(masterURL)
	if err != nil {
		// Fallback: just string-replace
		ws := strings.Replace(masterURL, "https://", "wss://", 1)
		ws = strings.Replace(ws, "http://", "ws://", 1)
		return strings.TrimRight(ws, "/") + "/ws/receiver?api_key=" + url.QueryEscape(apiKey)
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/ws/receiver"
	q := u.Query()
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()
	return u.String()
}

// maskKey hides the API key in log output.
func maskKey(wsURL string) string {
	if i := strings.Index(wsURL, "api_key="); i >= 0 {
		return wsURL[:i+8] + "***"
	}
	return wsURL
}

func parseFlags() (*slaveConfig, autoDiscovered) {
	master := flag.String("master", "", "Master server URL (e.g. https://yourdomain.com)")
	apiKey := flag.String("api-key", "", "API key for master authentication")
	slaveID := flag.String("id", "", "Unique slave ID (defaults to hostname)")
	slaveName := flag.String("name", "", "Display name for this slave")
	dirs := flag.String("dirs", "", "Comma-separated media directories")
	interval := flag.Duration("interval", 5*time.Minute, "Scan/catalog push interval")
	heartbeat := flag.Duration("heartbeat", 15*time.Second, "Heartbeat interval")
	flag.Parse()

	cfg := &slaveConfig{
		MasterURL:         *master,
		APIKey:            *apiKey,
		SlaveID:           *slaveID,
		SlaveName:         *slaveName,
		ScanInterval:      *interval,
		HeartbeatInterval: *heartbeat,
	}

	if *dirs != "" {
		cfg.MediaDirs = strings.Split(*dirs, ",")
	}

	// Environment overrides
	if v := os.Getenv("MASTER_URL"); v != "" {
		cfg.MasterURL = v
	}
	if v := os.Getenv("RECEIVER_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("SLAVE_ID"); v != "" {
		cfg.SlaveID = v
	}
	if v := os.Getenv("SLAVE_NAME"); v != "" {
		cfg.SlaveName = v
	}
	if v := os.Getenv("MEDIA_DIRS"); v != "" {
		cfg.MediaDirs = strings.Split(v, ",")
	}
	if v := os.Getenv("SCAN_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ScanInterval = d
		}
	}
	if v := os.Getenv("HEARTBEAT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.HeartbeatInterval = d
		}
	}

	// Auto-discover missing values from local config files
	disc := autoDiscover(cfg)

	// Defaults
	if cfg.SlaveID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "slave-unknown"
		}
		cfg.SlaveID = hostname
	}
	if cfg.SlaveName == "" {
		cfg.SlaveName = cfg.SlaveID
	}

	// Trim whitespace from dirs
	for i := range cfg.MediaDirs {
		cfg.MediaDirs[i] = strings.TrimSpace(cfg.MediaDirs[i])
	}

	// Filter out empty dirs
	var filtered []string
	for _, d := range cfg.MediaDirs {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	cfg.MediaDirs = filtered

	return cfg, disc
}

// ---------------------------------------------------------------------------
// Auto-discovery: fill missing config from local master config files
// ---------------------------------------------------------------------------

// autoDiscovered tracks which config values were auto-discovered (vs explicit).
type autoDiscovered struct {
	MasterURL bool
	APIKey    bool
	MediaDirs bool
}

// loadEnvFile reads a .env file and returns a map of key=value pairs.
// Skips comments (#) and blank lines. Handles optional quoting.
func loadEnvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		env[key] = val
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error reading %s: %v\n", path, err)
	}
	return env
}

// configJSONPartial holds only the fields we need from config.json.
type configJSONPartial struct {
	Server struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		EnableHTTPS bool   `json:"enable_https"`
	} `json:"server"`
	Directories struct {
		Videos string `json:"videos"`
		Music  string `json:"music"`
	} `json:"directories"`
	Receiver struct {
		APIKeys []string `json:"api_keys"`
	} `json:"receiver"`
}

// loadConfigJSON reads config.json and returns the partial struct, or nil on error.
func loadConfigJSON(path string) *configJSONPartial {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg configJSONPartial
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// deriveMasterURL builds a master URL from host, port, and HTTPS flag (default HTTP port 8080).
func deriveMasterURL(host string, port string, enableHTTPS string) string {
	if host == "" {
		return ""
	}
	scheme := "http"
	if strings.EqualFold(enableHTTPS, "true") || enableHTTPS == "1" {
		scheme = "https"
	}
	// Default port based on scheme
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "8080"
		}
	}
	// Omit port for standard scheme/port combos
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

// autoDiscover fills in missing slaveConfig from local config files (run from project root for discovery).
func autoDiscover(cfg *slaveConfig) autoDiscovered {
	var disc autoDiscovered

	// Nothing to discover if everything is already set
	if cfg.MasterURL != "" && cfg.APIKey != "" && len(cfg.MediaDirs) > 0 {
		return disc
	}

	// Load env files in priority order and merge (first file wins per key)
	merged := make(map[string]string)
	for _, path := range []string{".deploy.env", ".slave.env", ".env"} {
		env := loadEnvFile(path)
		for k, v := range env {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
	}

	// Try to fill MasterURL from env files
	if cfg.MasterURL == "" {
		if v := merged["MASTER_URL"]; v != "" {
			cfg.MasterURL = v
			disc.MasterURL = true
		} else {
			// Derive from SERVER_HOST + SERVER_PORT + SERVER_ENABLE_HTTPS
			derived := deriveMasterURL(
				merged["SERVER_HOST"],
				merged["SERVER_PORT"],
				merged["SERVER_ENABLE_HTTPS"],
			)
			if derived != "" {
				cfg.MasterURL = derived
				disc.MasterURL = true
			}
		}
	}

	// Try to fill APIKey from env files
	if cfg.APIKey == "" {
		if v := merged["RECEIVER_API_KEY"]; v != "" {
			cfg.APIKey = v
			disc.APIKey = true
		} else if v := merged["RECEIVER_API_KEYS"]; v != "" {
			// Take the first key from a comma-separated list
			if first := strings.SplitN(v, ",", 2)[0]; strings.TrimSpace(first) != "" {
				cfg.APIKey = strings.TrimSpace(first)
				disc.APIKey = true
			}
		}
	}

	// Try to fill MediaDirs from env files
	if len(cfg.MediaDirs) == 0 {
		if v := merged["MEDIA_DIRS"]; v != "" {
			cfg.MediaDirs = strings.Split(v, ",")
			disc.MediaDirs = true
		} else {
			// Combine VIDEOS_DIR + MUSIC_DIR
			var dirs []string
			if v := merged["VIDEOS_DIR"]; v != "" {
				dirs = append(dirs, v)
			}
			if v := merged["MUSIC_DIR"]; v != "" {
				dirs = append(dirs, v)
			}
			if len(dirs) > 0 {
				cfg.MediaDirs = dirs
				disc.MediaDirs = true
			}
		}
	}

	// If anything is still missing, try config.json as a final fallback
	if cfg.MasterURL == "" || cfg.APIKey == "" || len(cfg.MediaDirs) == 0 {
		if jcfg := loadConfigJSON("config.json"); jcfg != nil {
			if cfg.MasterURL == "" {
				port := ""
				if jcfg.Server.Port > 0 {
					port = fmt.Sprintf("%d", jcfg.Server.Port)
				}
				https := "false"
				if jcfg.Server.EnableHTTPS {
					https = "true"
				}
				derived := deriveMasterURL(jcfg.Server.Host, port, https)
				if derived != "" {
					cfg.MasterURL = derived
					disc.MasterURL = true
				}
			}
			if cfg.APIKey == "" && len(jcfg.Receiver.APIKeys) > 0 {
				cfg.APIKey = jcfg.Receiver.APIKeys[0]
				disc.APIKey = true
			}
			if len(cfg.MediaDirs) == 0 {
				var dirs []string
				if jcfg.Directories.Videos != "" {
					dirs = append(dirs, jcfg.Directories.Videos)
				}
				if jcfg.Directories.Music != "" {
					dirs = append(dirs, jcfg.Directories.Music)
				}
				if len(dirs) > 0 {
					cfg.MediaDirs = dirs
					disc.MediaDirs = true
				}
			}
		}
	}

	return disc
}

// resolveAndValidate ensures the path is within allowed directories.
// Uses filepath.Rel for containment (handles .., symlinks, URL-like tricks).
func resolveAndValidate(path string, allowedDirs []string) (string, error) {
	path = filepath.Clean(path)
	if path == "." || strings.HasPrefix(path, "..") {
		return "", fmt.Errorf("path traversal detected")
	}

	for _, dir := range allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		absDir, err = filepath.EvalSymlinks(absDir)
		if err != nil {
			continue
		}

		var fullPath string
		if filepath.IsAbs(path) {
			fullPath = filepath.Clean(path)
		} else {
			fullPath = filepath.Join(absDir, path)
		}

		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			continue
		}
		absPath, err = filepath.EvalSymlinks(absPath)
		if err != nil {
			continue
		}

		// Containment: rel must not start with ..
		rel, err := filepath.Rel(absDir, absPath)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			continue
		}

		// Check file exists
		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", fmt.Errorf("file not found in allowed directories")
}

// scanMediaDirs scans all configured directories for media files (filepath.Walk; follows symlinks).
func scanMediaDirs(dirs []string) []catalogItem {
	var items []catalogItem
	discoveredPaths := make(map[string]bool)

	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot resolve %s: %v\n", dir, err)
			continue
		}

		err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if info.IsDir() {
				if strings.HasPrefix(info.Name(), ".") && path != absDir {
					return filepath.SkipDir
				}
				return nil
			}

			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}

			mediaType := classifyFile(info.Name())
			if mediaType == "" {
				return nil
			}

			id := generateFileID(path)
			contentType := mime.TypeByExtension(filepath.Ext(info.Name()))
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			relPath, err := filepath.Rel(absDir, path)
			if err != nil {
				relPath = info.Name()
			}

			fp := getCachedFingerprint(path, info)
			discoveredPaths[path] = true

			items = append(items, catalogItem{
				ID:                 id,
				Path:               relPath,
				Name:               info.Name(),
				MediaType:          mediaType,
				Size:               info.Size(),
				ContentType:        contentType,
				ContentFingerprint: fp,
			})

			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: scan error in %s: %v\n", dir, err)
		}
	}

	pruneFpCache(discoveredPaths)
	return items
}

// classifyFile returns "video" or "audio" by extension (must match internal/media/discovery.go).
func classifyFile(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".webm", ".flv", ".m4v", ".ts", ".mpg", ".mpeg", ".3gp", ".m2ts", ".vob", ".ogv":
		return "video"
	case ".mp3", ".flac", ".wav", ".aac", ".ogg", ".m4a", ".wma", ".opus", ".alac", ".aiff", ".ape", ".mka":
		return "audio"
	default:
		return ""
	}
}

// generateFileID derives a deterministic ID from the file path (path changes produce new IDs).
func generateFileID(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:16])
}

// hashCatalog produces a hash of the catalog to skip redundant pushes (Path, Name, Size, ContentFingerprint).
func hashCatalog(items []catalogItem) string {
	h := sha256.New()
	for _, item := range items {
		fmt.Fprintf(h, "%s|%s|%d|%s\n", item.Path, item.Name, item.Size, item.ContentFingerprint)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// computeContentFingerprint computes a SHA-256 fingerprint (first/last 64KB + size); must match internal/media/discovery.go.
func computeContentFingerprint(path string) string {
	const sampleSize = 64 * 1024 // 64 KB

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	size := info.Size()
	h := sha256.New()

	// Write file size into the hash
	fmt.Fprintf(h, "size:%d\n", size)

	// Read first 64 KB (or entire file if smaller)
	head := make([]byte, sampleSize)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return ""
	}
	h.Write(head[:n])

	// Read last 64 KB if the file is larger than one sample
	if size > int64(sampleSize) {
		tail := make([]byte, sampleSize)
		offset := size - int64(sampleSize)
		n, err = f.ReadAt(tail, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return ""
		}
		h.Write(tail[:n])
	}

	return hex.EncodeToString(h.Sum(nil))
}
