// media-receiver is a slave node tool that scans local media directories and
// pushes the catalog to a Media Server Pro master instance. The master then
// proxies media streams to end users without storing files locally.
//
// Usage:
//
//	media-receiver -master http://master:8080 -api-key YOUR_KEY -dirs ./videos,./music
//	media-receiver -master http://master:8080 -api-key YOUR_KEY -dirs ./videos -public-url http://192.168.1.50:9090
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
//	LISTEN_ADDR         — address to listen on for media serving (e.g. ":9090")
//	PUBLIC_URL          — publicly reachable URL of this slave (overrides hostname resolution)
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
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

type slaveConfig struct {
	MasterURL         string
	APIKey            string
	SlaveID           string
	SlaveName         string
	MediaDirs         []string
	ScanInterval      time.Duration
	HeartbeatInterval time.Duration
	ListenAddr        string
	PublicURL         string // externally reachable URL, e.g. "http://192.168.1.50:9090"
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

	publicURL := cfg.PublicURL
	if publicURL == "" {
		publicURL = autoDetectPublicURL(cfg.ListenAddr, cfg.MasterURL)
	}

	fmt.Printf("Media Receiver (Slave) starting\n")
	fmt.Printf("  Master:     %s\n", cfg.MasterURL)
	fmt.Printf("  Slave ID:   %s\n", cfg.SlaveID)
	fmt.Printf("  Name:       %s\n", cfg.SlaveName)
	fmt.Printf("  Media:      %s\n", strings.Join(cfg.MediaDirs, ", "))
	fmt.Printf("  Scan:       %s\n", cfg.ScanInterval)
	fmt.Printf("  Heartbeat:  %s\n", cfg.HeartbeatInterval)
	fmt.Printf("  Listen:     %s\n", cfg.ListenAddr)
	fmt.Printf("  Public URL: %s\n", publicURL)
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start local media server for the master to proxy from
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startMediaServer(ctx, cfg); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Media server error: %v\n", err)
		}
	}()

	// Wait a moment for the server to start
	time.Sleep(200 * time.Millisecond)

	// Register with master
	if err := registerWithMaster(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register with master: %v\n", err)
		fmt.Fprintln(os.Stderr, "Will retry on next scan cycle...")
	} else {
		fmt.Println("Registered with master successfully")
	}

	// Initial scan and push
	items := scanMediaDirs(cfg.MediaDirs)
	fmt.Printf("Found %d media files\n", len(items))
	if err := pushCatalog(cfg, items, true); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to push catalog: %v\n", err)
	} else {
		fmt.Printf("Pushed %d items to master\n", len(items))
	}

	// Start periodic scan/push loop and heartbeat
	scanTicker := time.NewTicker(cfg.ScanInterval)
	heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer scanTicker.Stop()
	defer heartbeatTicker.Stop()

	// consecutiveHeartbeatFailures tracks how many heartbeats in a row have failed.
	// After 3 consecutive failures we attempt re-registration (master may have restarted).
	consecutiveHeartbeatFailures := 0

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-scanTicker.C:
			items := scanMediaDirs(cfg.MediaDirs)
			if err := pushCatalog(cfg, items, true); err != nil {
				fmt.Fprintf(os.Stderr, "Catalog push failed: %v\n", err)
				// Try re-registering in case the master lost our registration
				if err := registerWithMaster(cfg); err != nil {
					fmt.Fprintf(os.Stderr, "Re-registration failed: %v\n", err)
				}
			} else {
				consecutiveHeartbeatFailures = 0 // successful catalog push = master is alive
				fmt.Printf("[%s] Pushed %d items to master\n", time.Now().Format("15:04:05"), len(items))
			}

		case <-heartbeatTicker.C:
			if err := sendHeartbeat(cfg); err != nil {
				consecutiveHeartbeatFailures++
				fmt.Fprintf(os.Stderr, "Heartbeat failed (%d): %v\n", consecutiveHeartbeatFailures, err)
				// After 3 consecutive failures, try re-registering (master may have restarted)
				if consecutiveHeartbeatFailures >= 3 {
					fmt.Println("Re-registering with master after repeated heartbeat failures...")
					if err := registerWithMaster(cfg); err != nil {
						fmt.Fprintf(os.Stderr, "Re-registration failed: %v\n", err)
					} else {
						consecutiveHeartbeatFailures = 0
					}
				}
			} else {
				consecutiveHeartbeatFailures = 0
			}

		case sig := <-sigCh:
			fmt.Printf("\nReceived %s, shutting down...\n", sig)
			cancel()
			wg.Wait()
			return
		}
	}
}

func parseFlags() *slaveConfig {
	master := flag.String("master", "", "Master server URL (e.g. http://master:8080)")
	apiKey := flag.String("api-key", "", "API key for master authentication")
	slaveID := flag.String("id", "", "Unique slave ID (defaults to hostname)")
	slaveName := flag.String("name", "", "Display name for this slave")
	dirs := flag.String("dirs", "", "Comma-separated media directories")
	interval := flag.Duration("interval", 5*time.Minute, "Scan/catalog push interval")
	heartbeat := flag.Duration("heartbeat", 15*time.Second, "Heartbeat ping interval to master")
	listen := flag.String("listen", ":9090", "Listen address for media serving")
	publicURL := flag.String("public-url", "", "Publicly reachable URL of this slave (e.g. http://192.168.1.50:9090)")
	flag.Parse()

	cfg := &slaveConfig{
		MasterURL:         *master,
		APIKey:            *apiKey,
		SlaveID:           *slaveID,
		SlaveName:         *slaveName,
		ScanInterval:      *interval,
		HeartbeatInterval: *heartbeat,
		ListenAddr:        *listen,
		PublicURL:         *publicURL,
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
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
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

// startMediaServer serves local media files so the master can proxy from here.
func startMediaServer(ctx context.Context, cfg *slaveConfig) error {
	mux := http.NewServeMux()

	// Health endpoint for master health checks
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","role":"slave"}`))
	})

	// Media serving endpoint — matches the master's proxy request format
	mux.HandleFunc("/media", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "path parameter required", http.StatusBadRequest)
			return
		}

		// Security: resolve and validate path is within allowed directories
		absPath, err := resolveAndValidate(path, cfg.MediaDirs)
		if err != nil {
			http.Error(w, "access denied", http.StatusForbidden)
			return
		}

		http.ServeFile(w, r, absPath)
	})

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Media server listening on %s\n", cfg.ListenAddr)
	return server.ListenAndServe()
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

func registerWithMaster(cfg *slaveConfig) error {
	baseURL := cfg.PublicURL
	if baseURL == "" {
		baseURL = autoDetectPublicURL(cfg.ListenAddr, cfg.MasterURL)
	}
	body := map[string]string{
		"slave_id": cfg.SlaveID,
		"name":     cfg.SlaveName,
		"base_url": baseURL,
	}
	return postJSON(cfg, "/api/receiver/register", body)
}

func pushCatalog(cfg *slaveConfig, items []catalogItem, full bool) error {
	body := map[string]interface{}{
		"slave_id": cfg.SlaveID,
		"items":    items,
		"full":     full,
	}
	return postJSON(cfg, "/api/receiver/catalog", body)
}

func sendHeartbeat(cfg *slaveConfig) error {
	body := map[string]string{
		"slave_id": cfg.SlaveID,
	}
	return postJSON(cfg, "/api/receiver/heartbeat", body)
}

func postJSON(cfg *slaveConfig, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := strings.TrimRight(cfg.MasterURL, "/") + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", cfg.APIKey)
	req.Header.Set("User-Agent", "MediaServerPro-Slave/4.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
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
				// Skip hidden directories
				if strings.HasPrefix(info.Name(), ".") && path != absDir {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip hidden files
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}

			mediaType := classifyFile(info.Name())
			if mediaType == "" {
				return nil // Not a media file
			}

			// Generate stable ID from path
			id := generateFileID(path)

			// Determine content type
			contentType := mime.TypeByExtension(filepath.Ext(info.Name()))
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			// Use path relative to the media dir for the remote path
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
	return hex.EncodeToString(h[:16]) // 32-char hex ID
}

// autoDetectPublicURL builds the base URL that the master should use to reach this slave.
// It extracts the port from listenAddr and determines the outbound IP by making a UDP
// "connection" to the master — the OS picks which interface it would route through,
// giving us the actual LAN/WAN IP without needing ifconfig or manual config.
func autoDetectPublicURL(listenAddr, masterURL string) string {
	// Extract port from listenAddr (e.g. ":9090" or "0.0.0.0:9090" → "9090")
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		port = "9090"
	}

	// Derive the master's host:port for UDP dialing (strip scheme)
	masterHost := strings.TrimPrefix(masterURL, "https://")
	masterHost = strings.TrimPrefix(masterHost, "http://")
	// Strip any path
	if idx := strings.IndexByte(masterHost, '/'); idx >= 0 {
		masterHost = masterHost[:idx]
	}
	// Ensure there's a port for the UDP dial target
	if _, _, err := net.SplitHostPort(masterHost); err != nil {
		masterHost = masterHost + ":80"
	}

	// UDP dial doesn't actually send anything — OS just selects the right interface
	conn, err := net.Dial("udp", masterHost)
	if err == nil {
		defer conn.Close()
		localIP := conn.LocalAddr().(*net.UDPAddr).IP.String()
		return fmt.Sprintf("http://%s:%s", localIP, port)
	}

	// Fallback: use hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}
	return fmt.Sprintf("http://%s:%s", hostname, port)
}
