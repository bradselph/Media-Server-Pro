package downloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"media-server-pro/internal/logger"
)

const (
	errRequestFailed = "request failed: %w"
	fmtHTTPError     = "HTTP %d: %s"
)

// Client is an HTTP client for the standalone downloader service API.
// IMPORTANT: Uses a plain http.Transport (NOT helpers.SafeHTTPTransport)
// because the downloader runs on localhost and the SSRF guard blocks loopback.
type Client struct {
	baseURL       string
	httpClient    *http.Client
	internalToken string
	log           *logger.Logger
}

// NewClient creates a client for the downloader API. When internalToken is
// non-empty it is sent as the `X-MSP-Internal-Token` header on every request,
// letting the downloader trust this caller without a session-cookie round-trip.
func NewClient(baseURL string, timeout time.Duration, internalToken string) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	baseURL = strings.TrimRight(baseURL, "/")
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: dialer.DialContext,
			},
		},
		internalToken: internalToken,
		log:           logger.New("downloader-client"),
	}
}

// HealthResponse holds the downloader's /api/health response.
type HealthResponse struct {
	ActiveDownloads    int     `json:"activeDownloads"`
	QueuedDownloads    int     `json:"queuedDownloads"`
	Uptime             float64 `json:"uptime"`
	AllowServerStorage bool    `json:"allowServerStorage"`
	AnySiteForAdmin    bool    `json:"anySiteForAdmin"` // v1.5.0: admins may download from any URL
	Dependencies       struct {
		YtDlp  *DepInfo `json:"ytdlp"`
		FFmpeg *DepInfo `json:"ffmpeg"`
	} `json:"dependencies"`
}

// DepInfo holds dependency availability info.
type DepInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

// DetectResponse holds the downloader's /api/detect response.
type DetectResponse struct {
	IsYouTube       bool         `json:"isYouTube"`
	IsYouTubeMusic  bool         `json:"isYouTubeMusic"`
	Title           string       `json:"title"`
	Stream          *StreamInfo  `json:"stream"`
	AllStreams      []StreamInfo `json:"allStreams"`
	DetectionMethod string       `json:"detectionMethod"`
	Previews        []string     `json:"previews"`
	PageURL         string       `json:"pageUrl"`
	RelayID         string       `json:"relayId"`
	Engine          string       `json:"engine"`        // v1.5.0: "ytdlp" (hand the page URL to yt-dlp) | "stream"
	AdminUnlocked   bool         `json:"adminUnlocked"` // v1.5.0: true when an admin bypassed the curated-site gate
}

// StreamInfo holds info about a detected stream.
type StreamInfo struct {
	URL        string `json:"url"`
	Type       string `json:"type"`
	Quality    string `json:"quality"`
	Resolution string `json:"resolution"`
	Size       int64  `json:"size"`
	IsAd       bool   `json:"isAd"`
}

// DownloadParams holds parameters for starting a download.
type DownloadParams struct {
	URL            string `json:"url"`
	Title          string `json:"title"`
	SaveLocation   string `json:"saveLocation"`
	ClientID       string `json:"clientId"`
	IsYouTube      bool   `json:"isYouTube"`
	IsYouTubeMusic bool   `json:"isYouTubeMusic"`
	RelayID        string `json:"relayId,omitempty"`

	// v1.5.0 universal-engine options (all optional; omitempty keeps the wire
	// format byte-for-byte when unused).
	AudioOnly    bool   `json:"audioOnly,omitempty"`
	AudioFormat  string `json:"audioFormat,omitempty"`
	AudioQuality *int   `json:"audioQuality,omitempty"` // *int so an explicit 0 (best) is still sent
	Format       string `json:"format,omitempty"`
}

// DownloadResponse holds the downloader's /api/download response.
type DownloadResponse struct {
	Success    bool   `json:"success"`
	DownloadID string `json:"downloadId"`
	StreamURL  string `json:"streamUrl"`
	Message    string `json:"message"`
}

// DownloadFile represents a completed download on the server.
type DownloadFile struct {
	File        string `json:"file"`
	Size        int64  `json:"size"`
	Timestamp   int64  `json:"timestamp"`
	IsAudio     bool   `json:"isAudio"`
	DownloadURL string `json:"downloadUrl"`
}

// DownloadsListResponse holds the response from /api/downloads.
type DownloadsListResponse struct {
	Downloads []DownloadFile `json:"downloads"`
}

// SettingsResponse holds the downloader's /api/settings response.
type SettingsResponse struct {
	AllowServerStorage     bool     `json:"allowServerStorage"`
	SupportedSites         []string `json:"supportedSites"`
	Theme                  string   `json:"theme"`
	AudioFormat            string   `json:"audioFormat"`
	BrowserRelayConfigured bool     `json:"browserRelayConfigured"`
	ProxyPoolSize          int      `json:"proxyPoolSize"`
	AnySiteForAdmin        bool     `json:"anySiteForAdmin"` // v1.5.0
	AudioFormats           []string `json:"audioFormats"`    // v1.5.0: selectable audio formats
	YtDlpAvailable         bool     `json:"ytdlpAvailable"`  // v1.5.0
}

// Health checks the downloader's health endpoint.
func (c *Client) Health() (*HealthResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	var resp HealthResponse
	if err := c.get("/api/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Detect sends a URL to the downloader for stream detection.
func (c *Client) Detect(rawURL string) (*DetectResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	body := map[string]string{"url": rawURL}
	var resp DetectResponse
	if err := c.post("/api/detect", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Download starts a download on the downloader service.
func (c *Client) Download(params DownloadParams, mspSessionID string) (*DownloadResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	var resp DownloadResponse
	if err := c.postWithSession("/api/download", params, &resp, mspSessionID); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelDownload cancels an active download.
func (c *Client) CancelDownload(downloadID string) error {
	if c == nil {
		return fmt.Errorf("downloader client not initialized")
	}
	return c.post("/api/cancel/"+url.PathEscape(downloadID), nil, nil)
}

// ListDownloads returns completed downloads on the server.
func (c *Client) ListDownloads() (*DownloadsListResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	var resp DownloadsListResponse
	if err := c.get("/api/downloads", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteDownload removes a downloaded file.
func (c *Client) DeleteDownload(filename string) error {
	if c == nil {
		return fmt.Errorf("downloader client not initialized")
	}
	return c.del("/api/download/" + url.PathEscape(filename))
}

// GetSettings returns the downloader's settings.
func (c *Client) GetSettings() (*SettingsResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	var resp SettingsResponse
	if err := c.get("/api/settings", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BatchDownloadItem is one URL plus optional per-job overrides (v1.5.0).
type BatchDownloadItem struct {
	URL          string `json:"url"`
	Title        string `json:"title,omitempty"`
	AudioOnly    bool   `json:"audioOnly,omitempty"`
	AudioFormat  string `json:"audioFormat,omitempty"`
	AudioQuality *int   `json:"audioQuality,omitempty"`
	Format       string `json:"format,omitempty"`
}

// BatchDownloadParams queues many URLs in one /api/download/batch call (v1.5.0).
type BatchDownloadParams struct {
	URLs         []BatchDownloadItem `json:"urls"`
	ClientID     string              `json:"clientId"`
	SaveLocation string              `json:"saveLocation,omitempty"`
}

// BatchAccepted is one queued job from a batch request.
type BatchAccepted struct {
	URL        string `json:"url"`
	DownloadID string `json:"downloadId"`
}

// BatchRejected is one URL the downloader refused, with a reason.
type BatchRejected struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// BatchDownloadResponse holds the downloader's /api/download/batch response.
type BatchDownloadResponse struct {
	Success  bool            `json:"success"`
	Queued   int             `json:"queued"`
	Accepted []BatchAccepted `json:"accepted"`
	Rejected []BatchRejected `json:"rejected"`
}

// QueueActiveItem is a job the downloader is actively processing.
type QueueActiveItem struct {
	DownloadID string `json:"downloadId"`
	Type       string `json:"type"`
	ClientID   string `json:"clientId"`
}

// QueueQueuedItem is a job waiting in the downloader's queue.
type QueueQueuedItem struct {
	DownloadID   string `json:"downloadId"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	SaveLocation string `json:"saveLocation"`
	ClientID     string `json:"clientId"`
	AudioOnly    bool   `json:"audioOnly"`
}

// QueueResponse holds the downloader's /api/queue response (v1.5.0).
type QueueResponse struct {
	Success       bool              `json:"success"`
	Processing    int               `json:"processing"`
	MaxConcurrent int               `json:"maxConcurrent"`
	Active        []QueueActiveItem `json:"active"`
	Queued        []QueueQueuedItem `json:"queued"`
}

// BatchDownload queues many URLs in one call. Forwards the admin session ID
// (like Download) for parity; the internal token is the primary trust.
func (c *Client) BatchDownload(params BatchDownloadParams, mspSessionID string) (*BatchDownloadResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	var resp BatchDownloadResponse
	if err := c.postWithSession("/api/download/batch", params, &resp, mspSessionID); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Queue returns the downloader's active + queued jobs. Uses get(), which already
// attaches X-MSP-Internal-Token, so an admin-configured client is authorized.
func (c *Client) Queue() (*QueueResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("downloader client not initialized")
	}
	var resp QueueResponse
	if err := c.get("/api/queue", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attachAuth adds the shared MSP internal token (when configured) and an
// optional admin session ID. Callers should invoke this on every outbound
// request that mutates downloader state.
func (c *Client) attachAuth(req *http.Request, mspSessionID string) {
	if c.internalToken != "" {
		req.Header.Set("X-MSP-Internal-Token", c.internalToken)
	}
	if mspSessionID != "" {
		req.Header.Set("X-MSP-Session", mspSessionID)
	}
}

func (c *Client) get(path string, result any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.attachAuth(req, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(errRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf(fmtHTTPError, resp.StatusCode, string(body))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) post(path string, body, result any) error {
	return c.postWithSession(path, body, result, "")
}

func (c *Client) postWithSession(path string, body, result any, mspSessionID string) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.attachAuth(req, mspSessionID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(errRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf(fmtHTTPError, resp.StatusCode, string(respBody))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) del(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.attachAuth(req, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(errRequestFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf(fmtHTTPError, resp.StatusCode, string(body))
	}
	return nil
}
