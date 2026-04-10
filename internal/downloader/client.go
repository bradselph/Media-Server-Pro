package downloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"media-server-pro/internal/logger"
)

// Client is an HTTP client for the standalone downloader service API.
// IMPORTANT: Uses a plain http.Transport (NOT helpers.SafeHTTPTransport)
// because the downloader runs on localhost and the SSRF guard blocks loopback.
type Client struct {
	baseURL    string
	httpClient *http.Client
	log        *logger.Logger
}

// NewClient creates a client for the downloader API.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{},
		},
		log: logger.New("downloader-client"),
	}
}

// HealthResponse holds the downloader's /api/health response.
type HealthResponse struct {
	ActiveDownloads    int     `json:"activeDownloads"`
	QueuedDownloads    int     `json:"queuedDownloads"`
	Uptime             float64 `json:"uptime"`
	AllowServerStorage bool    `json:"allowServerStorage"`
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
}

// Health checks the downloader's health endpoint.
func (c *Client) Health() (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.get("/api/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Detect sends a URL to the downloader for stream detection.
func (c *Client) Detect(rawURL string) (*DetectResponse, error) {
	body := map[string]string{"url": rawURL}
	var resp DetectResponse
	if err := c.post("/api/detect", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Download starts a download on the downloader service.
func (c *Client) Download(params DownloadParams, mspSessionID string) (*DownloadResponse, error) {
	var resp DownloadResponse
	if err := c.postWithSession("/api/download", params, &resp, mspSessionID); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelDownload cancels an active download.
func (c *Client) CancelDownload(downloadID string) error {
	return c.post("/api/cancel/"+downloadID, nil, nil)
}

// ListDownloads returns completed downloads on the server.
func (c *Client) ListDownloads() (*DownloadsListResponse, error) {
	var resp DownloadsListResponse
	if err := c.get("/api/downloads", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteDownload removes a downloaded file.
func (c *Client) DeleteDownload(filename string) error {
	return c.del("/api/download/" + url.PathEscape(filename))
}

// GetSettings returns the downloader's settings.
func (c *Client) GetSettings() (*SettingsResponse, error) {
	var resp SettingsResponse
	if err := c.get("/api/settings", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) get(path string, result interface{}) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) post(path string, body, result interface{}) error {
	return c.postWithSession(path, body, result, "")
}

func (c *Client) postWithSession(path string, body, result interface{}, mspSessionID string) error {
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
	if mspSessionID != "" {
		req.Header.Set("X-MSP-Session", mspSessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
