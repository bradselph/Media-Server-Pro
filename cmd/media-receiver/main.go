// media-receiver is a slave node that scans local media directories, connects to
// a Media Server Pro master instance via WebSocket, and pushes the catalog so the
// master can proxy media streams to users on demand. No public IP or port forwarding
// is needed — the slave initiates all connections.
//
// Usage:
//
//	media-receiver -master https://yourdomain.com -api-key YOUR_KEY -dirs ./videos,./music
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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	ID          string  `json:"id"`
	Path        string  `json:"path"`
	Name        string  `json:"name"`
	MediaType   string  `json:"media_type"`
	Size        int64   `json:"size"`
	Duration    float64 `json:"duration"`
	ContentType string  `json:"content_type"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
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

func main() {
	cfg := parseFlags()

	if cfg.MasterURL == "" {
		fmt.Fprintln(os.Stderr, "Error: master URL is required (-master or MASTER_URL)")
		os.Exit(1)
	}
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: API key is required (-api-key or RECEIVER_API_KEY)")
		os.Exit(1)
	}
	if len(cfg.MediaDirs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: at least one media directory required (-dirs or MEDIA_DIRS)")
		os.Exit(1)
	}

	fmt.Printf("Media Receiver (Slave) starting\n")
	fmt.Printf("  Master:     %s\n", cfg.MasterURL)
	fmt.Printf("  Slave ID:   %s\n", cfg.SlaveID)
	fmt.Printf("  Name:       %s\n", cfg.SlaveName)
	fmt.Printf("  Media:      %s\n", strings.Join(cfg.MediaDirs, ", "))
	fmt.Printf("  Scan:       %s\n", cfg.ScanInterval)
	fmt.Printf("  Heartbeat:  %s\n", cfg.HeartbeatInterval)
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

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}
	defer conn.Close()
	fmt.Println("Connected to master via WebSocket")

	// Send registration
	if err := sendWSJSON(conn, "register", map[string]string{
		"slave_id": cfg.SlaveID,
		"name":     cfg.SlaveName,
	}); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	fmt.Println("Registered with master")

	// Initial scan and catalog push
	items := scanMediaDirs(cfg.MediaDirs)
	fmt.Printf("Found %d media files\n", len(items))
	if err := sendWSJSON(conn, "catalog", map[string]interface{}{
		"slave_id": cfg.SlaveID,
		"items":    items,
		"full":     true,
	}); err != nil {
		return fmt.Errorf("catalog push failed: %w", err)
	}
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
	if req.Range != "" {
		start, end, err := parseRange(req.Range, stat.Size())
		if err == nil {
			if _, seekErr := file.Seek(start, io.SeekStart); seekErr == nil {
				length := end - start + 1
				body = io.LimitReader(file, length)
				statusCode = 206
				contentLength = length
				extraHeaders = map[string]string{
					"Content-Range":  fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size()),
					"Accept-Ranges":  "bytes",
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

	if extraHeaders != nil {
		for k, v := range extraHeaders {
			httpReq.Header.Set(k, v)
		}
	}

	client := &http.Client{Timeout: 0} // no timeout for streaming
	resp, err := client.Do(httpReq)
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

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

// sendWSJSON sends a typed JSON message over the WebSocket.
func sendWSJSON(conn *websocket.Conn, msgType string, data interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	msg := wsMessage{Type: msgType, Data: raw}
	return conn.WriteJSON(msg)
}

// buildWSURL converts a master HTTP URL to a WebSocket URL.
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

func parseFlags() *slaveConfig {
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

	return cfg
}

// resolveAndValidate ensures the path is within one of the allowed directories.
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

			items = append(items, catalogItem{
				ID:          id,
				Path:        relPath,
				Name:        info.Name(),
				MediaType:   mediaType,
				Size:        info.Size(),
				ContentType: contentType,
			})

			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: scan error in %s: %v\n", dir, err)
		}
	}

	return items
}

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

func generateFileID(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:16])
}

