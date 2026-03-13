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

// TODO: UNBOUNDED CACHE — fpCache grows without bound as files are discovered.
// If files are deleted from the media directories, their entries remain in the
// cache forever, leaking memory. Consider pruning entries for paths that no longer
// exist during each scan cycle, or switching to an LRU cache with a max size.
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

// streamHTTPClient is reused across all stream deliveries to benefit from
// connection pooling (keep-alive) to the master.
// TODO: NO TLS VERIFICATION — The Transport does not configure TLS certificate
// verification explicitly, so it uses Go's default (verify against system roots).
// This is correct, but there is no option for users to provide a custom CA
// certificate for self-signed master servers, which is a common deployment
// scenario. Consider adding a -tls-skip-verify flag or -ca-cert flag.
// Also, Timeout=0 means there is no overall request timeout. If the master
// hangs after accepting the connection, the POST will block forever. Consider
// adding a per-stream timeout or using context-based cancellation.
var streamHTTPClient = &http.Client{
	Timeout: 0, // no overall timeout for streaming
	Transport: &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// lastCatalogHash stores a SHA-256 of the last pushed catalog items so we can
// skip redundant full pushes when nothing has changed.
// TODO: GLOBAL STATE — lastCatalogHash is a package-level global that is read/written
// from the main goroutine (in the scan ticker select case). While currently safe because
// it's only accessed from one goroutine, this is fragile — if the reconnect logic changes
// or concurrency is introduced, it would become a data race. Consider moving it into the
// connectAndRun function scope or the slaveConfig struct.
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
// If the connection drops, it waits and reconnects automatically.
func runSlaveLoop(ctx context.Context, cfg *slaveConfig) {
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
		}

		fmt.Println("Reconnecting in 5 seconds...")
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// connectAndRun establishes one WebSocket connection, sends registration + catalog,
// and runs the heartbeat/scan/stream-request loop until the connection drops.
func connectAndRun(ctx context.Context, cfg *slaveConfig) error {
	wsURL := buildWSURL(cfg.MasterURL, cfg.APIKey)
	fmt.Printf("Connecting to %s...\n", maskKey(wsURL))

	// TODO: NO TLS CONFIG — The dialer uses the default TLS configuration,
	// which means self-signed certificates on the master will cause connection
	// failures. Consider adding a -tls-skip-verify flag and/or -ca-cert flag
	// to the CLI for non-production or custom CA deployments.
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
	// TODO: BUG — RACE CONDITION — The PingHandler calls conn.WriteMessage() to send a
	// pong, but this handler runs on the read goroutine. Meanwhile, the main goroutine
	// writes heartbeats and catalog pushes via sendWSJSON() protected by writeMu. However,
	// the PingHandler does NOT acquire writeMu, so a pong write can race with a heartbeat
	// or catalog write on the same conn, causing corrupted frames or panics. gorilla/websocket
	// documents that "Connections support one concurrent reader and one concurrent writer."
	// Fix by acquiring writeMu in the PingHandler before calling WriteMessage, or by
	// using conn.WriteControl() which is safe for concurrent use.
	conn.SetPingHandler(func(data string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
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
				// Handle stream delivery in background
				// TODO: UNBOUNDED GOROUTINES — Each stream request spawns a new goroutine
				// with no concurrency limit. A burst of stream requests (or a malicious
				// master) could spawn thousands of goroutines, each opening a file and
				// performing an HTTP POST, exhausting file descriptors and memory. Consider
				// adding a semaphore or worker pool to limit concurrent stream deliveries
				// (e.g., to MaxIdleConns=10 to match the HTTP client's connection pool).
				wg.Add(1)
				go func() {
					defer wg.Done()
					deliverStream(streamCtx, cfg, req)
				}()
			}
		}
	}()

	// Heartbeat and scan tickers
	scanTicker := time.NewTicker(cfg.ScanInterval)
	heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer scanTicker.Stop()
	defer heartbeatTicker.Stop()

	var writeMu sync.Mutex

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
			// TODO: MISSING ERROR HANDLING — The error from conn.WriteMessage is ignored.
			// If the connection is already broken, this will fail silently, which is
			// acceptable during shutdown. However, there is no write deadline set before
			// this write, so if the master is unresponsive, this could block indefinitely
			// and prevent clean shutdown. Set a short write deadline before this call.
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
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

	// Handle Range requests
	// TODO: SILENT FAILURE — If parseRange returns an error (malformed Range header),
	// the error is silently ignored and the full file is served with status 200 instead
	// of returning HTTP 416 Range Not Satisfiable. Similarly, if file.Seek fails, the
	// error is silently ignored and the file is served from the current offset (likely
	// position 0) with incorrect content. Both error cases should be logged and ideally
	// reported back to the master.
	if req.Range != "" {
		start, end, err := parseRange(req.Range, stat.Size())
		if err == nil {
			if _, seekErr := file.Seek(start, io.SeekStart); seekErr == nil {
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
		n := parseInt64(parts[1])
		start = fileSize - n
		if start < 0 {
			start = 0
		}
		end = fileSize - 1
	} else if parts[1] == "" {
		// Open-ended: 500- means from byte 500 to end
		start = parseInt64(parts[0])
		end = fileSize - 1
	} else {
		start = parseInt64(parts[0])
		end = parseInt64(parts[1])
	}

	if start < 0 || start >= fileSize || end < start || end >= fileSize {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

// TODO: BUG — SILENT PARSE FAILURE — fmt.Sscanf silently returns 0 on unparseable
// input (e.g., "abc" or ""), and the error is discarded. This means a malformed
// Range header like "bytes=abc-def" will be parsed as start=0, end=0, which passes
// the bounds check and serves the first byte. Use strconv.ParseInt instead and
// propagate the error to the caller (parseRange) so it can return an error.
func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

// sendWSJSON sends a typed JSON message over the WebSocket.
// TODO: NOT THREAD-SAFE — This function writes to the WebSocket without any
// synchronization. Callers in connectAndRun use writeMu to serialize writes from
// the scan and heartbeat tickers, but the initial registration and catalog writes
// (before the tickers start) call sendWSJSON without writeMu. This is safe only
// because those writes happen before the reader goroutine starts, so there is no
// concurrent writer yet. However, the PingHandler also writes without writeMu
// (see the race condition TODO above). Consider making this function accept a
// *sync.Mutex parameter, or moving write serialization into sendWSJSON itself.
func sendWSJSON(conn *websocket.Conn, msgType string, data interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	msg := wsMessage{Type: msgType, Data: raw}
	return conn.WriteJSON(msg)
}

// buildWSURL converts a master HTTP URL to a WebSocket URL.
// TODO: SECURITY — The API key is passed as a query parameter (?api_key=...), which
// means it appears in server access logs, proxy logs, and browser history. While
// WebSocket upgrade requests are not typically cached, intermediary proxies or load
// balancers may log the full URL. The master already supports X-API-Key header
// authentication; consider passing the key in the websocket.Dialer.Header instead
// of the query string. This would require updating the dialer call in connectAndRun.
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

// deriveMasterURL builds a master URL from host, port, and HTTPS flag.
// TODO: INCONSISTENT DEFAULT — The default HTTP port here is 8080, but the
// master server's actual default port (in internal/config/defaults.go) may be
// different. If the master is configured to use a different port and PORT is
// not explicitly set, this function will derive the wrong URL. Consider reading
// the master's actual default from a shared constant or documenting the assumption.
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

// autoDiscover fills in missing slaveConfig values from local config files.
// Discovery order: .deploy.env → .slave.env → .env → config.json.
// Only values that are still empty are filled — explicit flags/env always win.
// TODO: MISLEADING AUTO-DISCOVERY — This reads the master's config files (config.json,
// .env) to derive the master URL and API keys, which only works when the slave binary
// is run from the master's project directory. In production deployments, the slave runs
// on a separate machine where these files don't exist, making auto-discovery silently
// ineffective. The doc comment and usage tip mention "run from the project root" but
// this is a development convenience that may confuse production deployments. Consider
// logging when auto-discovery finds no files, and documenting that production slaves
// should always use explicit flags or environment variables.
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

// resolveAndValidate ensures the path is within one of the allowed directories.
// TODO: WEAK VALIDATION — The path traversal check (strings.Contains(path, ".."))
// is a blocklist approach that may miss edge cases on certain OS/filesystem
// combinations (e.g., symbolic links that escape the allowed directory). The
// filepath.Rel check below is more robust and catches ".." in the resolved path,
// but the initial string check is redundant with it and gives a false sense of
// additional security. Consider removing the string check and relying solely on
// the filepath.Rel-based containment check. Additionally, on Windows, paths with
// alternate data streams (e.g., "file.txt::$DATA") or UNC paths are not validated.
func resolveAndValidate(path string, allowedDirs []string) (string, error) {
	// Prevent path traversal
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path traversal detected")
	}

	// Try to find the file in allowed directories
	for _, dir := range allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}

		var fullPath string
		if filepath.IsAbs(path) {
			fullPath = path
		} else {
			fullPath = filepath.Join(absDir, path)
		}

		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			continue
		}

		// Check path is within allowed directory
		rel, err := filepath.Rel(absDir, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		// Check file exists
		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", fmt.Errorf("file not found in allowed directories")
}

// scanMediaDirs scans all configured directories for media files.
// TODO: NO SYMLINK PROTECTION — filepath.Walk follows symbolic links, which could
// cause infinite loops if a symlink points to a parent directory. Consider using
// filepath.WalkDir (available since Go 1.16) which provides DirEntry and can detect
// symlinks via d.Type()&fs.ModeSymlink, or track visited inodes to detect cycles.
func scanMediaDirs(dirs []string) []catalogItem {
	var items []catalogItem

	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot resolve %s: %v\n", dir, err)
			continue
		}

		err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				// TODO: SILENT ERROR — Walk errors (permission denied, broken symlinks, etc.)
				// are silently swallowed. Consider logging them at debug/warn level so
				// operators can diagnose missing files in the catalog.
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

	return items
}

// TODO: BUG — EXTENSION MISMATCH — The extension lists here do not match the master's
// lists in internal/media/discovery.go. Missing video extensions: ".3gp", ".m2ts",
// ".vob", ".ogv". Missing audio extensions: ".aiff", ".ape", ".mka". This means the
// slave will not discover files with these extensions, causing them to be missing from
// the catalog pushed to the master. Keep these lists in sync with the master, or
// extract them into a shared package (e.g. pkg/mediaext/) that both binaries import.
func classifyFile(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".webm", ".flv", ".m4v", ".ts", ".mpg", ".mpeg":
		return "video"
	case ".mp3", ".flac", ".wav", ".aac", ".ogg", ".m4a", ".wma", ".opus", ".alac":
		return "audio"
	default:
		return ""
	}
}

// TODO: FRAGILE ID GENERATION — File IDs are derived from the absolute path,
// which means moving a file to a different directory (or renaming it) produces a
// different ID. The master uses stable UUIDs (stored in media_metadata.stable_id)
// that persist across renames. This ID is used as the catalog item's ID field, and
// the master stores it in receiver_media. If the slave restarts from a different
// working directory (changing the absolute path), all IDs change and the master
// sees them as new files, potentially creating duplicates. Consider using the
// content fingerprint as the ID instead, or persisting assigned IDs locally.
func generateFileID(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:16])
}

// hashCatalog produces a deterministic hash of the catalog so we can detect
// when nothing has changed between scans and skip redundant pushes.
// TODO: NON-DETERMINISTIC — The hash is computed by iterating over the items slice
// in the order returned by filepath.Walk (alphabetical within each directory).
// This IS deterministic for a single directory, but if cfg.MediaDirs contains
// multiple directories, the relative ordering of items from different directories
// depends on the iteration order of the dirs slice, which is stable. However,
// the hash does not include the item's ID, MediaType, or ContentType fields —
// if only those fields change (without a file rename/resize), the hash stays
// the same and the updated catalog is not pushed.
func hashCatalog(items []catalogItem) string {
	h := sha256.New()
	for _, item := range items {
		fmt.Fprintf(h, "%s|%s|%d|%s\n", item.Path, item.Name, item.Size, item.ContentFingerprint)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// computeContentFingerprint computes a SHA-256 fingerprint of a media file.
// This MUST match the algorithm in internal/media/discovery.go so the master
// can detect duplicates between local and slave media. It samples the first
// 64 KB, the last 64 KB, and the file size — fast even for very large files.
// TODO: DUPLICATION — This function is a copy of internal/media/discovery.go's
// computeContentFingerprint(). The two implementations must stay in sync or
// duplicate detection between master and slave breaks silently. Extract into a
// shared package (e.g. pkg/fingerprint/) that both cmd/server and cmd/media-receiver
// can import. Note the return signature differs: the master returns (string, error)
// while this version returns string (empty on error), so the shared version should
// use the (string, error) signature and this caller should handle the error.
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
