// Package huggingface provides a client for the Hugging Face Inference API
// for image captioning and visual classification of media content.
package huggingface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"media-server-pro/internal/logger"
)

const (
	defaultBaseURL     = "https://api-inference.huggingface.co"
	maxRetries         = 3
	initialRetryDelay  = 2 * time.Second
	retryBackoffFactor = 2.0
	maxResponseSize    = 10 * 1024 * 1024 // 10MB cap for HF API responses
)

// ClassificationResult holds the result of an image classification/caption request.
type ClassificationResult struct {
	Caption    string   // Raw generated text from image-to-text model
	Tags       []string // Parsed tags from caption
	Confidence float64  // Model confidence (if available)
	Model      string   // Model used
}

// hfImageCaptionResponse is the JSON response from HF image-to-text/captioning models.
// Format: [{"generated_text": "..."}]
type hfImageCaptionResponse []struct {
	GeneratedText string `json:"generated_text"`
}

// Client handles communication with the Hugging Face Inference API.
type Client struct {
	httpClient  *http.Client
	apiKey      string
	model       string
	endpointURL string
	rateLimiter *rate.Limiter
	log         *logger.Logger
}

// ClientConfig holds options for creating a Hugging Face API client.
type ClientConfig struct {
	APIKey            string
	Model             string
	EndpointURL       string
	RequestsPerMinute int
	Timeout           time.Duration
	Log               *logger.Logger
}

// NewClient creates a new Hugging Face API client. If APIKey is empty, the client
// will return empty results without making requests (graceful degradation).
func NewClient(cfg ClientConfig) *Client {
	rpm := cfg.RequestsPerMinute
	if rpm <= 0 {
		rpm = 30
	}
	rps := float64(rpm) / 60.0
	if rps < 0.5 {
		rps = 0.5
	}
	rl := rate.NewLimiter(rate.Limit(rps), 2)

	baseURL := defaultBaseURL
	if cfg.EndpointURL != "" {
		baseURL = strings.TrimSuffix(cfg.EndpointURL, "/")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		endpointURL: baseURL,
		rateLimiter: rl,
		log:         cfg.Log,
	}
}

// ClassifyImage sends an image to the Hugging Face Inference API and returns
// generated captions/tags. On error or if the client is not configured (no API key),
// returns an empty result without failing (graceful degradation).
func (c *Client) ClassifyImage(ctx context.Context, imageData []byte) (*ClassificationResult, error) {
	empty := &ClassificationResult{Model: c.model}
	if c.apiKey == "" {
		return empty, nil
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return empty, nil
	}
	url := c.endpointURL + "/models/" + c.model
	return c.runWithRetry(ctx, url, imageData)
}

// runWithRetry runs doOneRequest up to maxRetries times and returns result or empty.
func (c *Client) runWithRetry(ctx context.Context, url string, imageData []byte) (*ClassificationResult, error) {
	empty := &ClassificationResult{Model: c.model}
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 && !c.sleepBeforeRetry(ctx, attempt) {
			return empty, nil
		}
		result, retry, retryErr, fatalErr := c.doOneRequest(ctx, url, imageData, attempt)
		if fatalErr != nil {
			return nil, fatalErr
		}
		if result != nil {
			return result, nil
		}
		if !retry {
			return empty, nil
		}
		lastErr = retryErr
	}
	c.log.Warn("HF client: all retries failed: %v", lastErr)
	return empty, nil
}

// TODO: Bug — on attempt=1 (second attempt, first retry), the loop runs
// `for i := 0; i < 0; i++` which executes zero times, so the delay is always
// initialRetryDelay (2s) for the first retry. On attempt=2, the loop runs once,
// giving 4s. This means retries are: 2s, 4s — which is correct exponential backoff
// starting from attempt 1. However, the loop condition `i < attempt-1` is confusing
// and could be simplified to `delay = initialRetryDelay * 2^(attempt-1)` for clarity.

// sleepBeforeRetry sleeps for the backoff delay. Returns false if context is done.
func (c *Client) sleepBeforeRetry(ctx context.Context, attempt int) bool {
	delay := initialRetryDelay
	for i := 0; i < attempt-1; i++ {
		delay = time.Duration(float64(delay) * retryBackoffFactor)
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// doOneRequest performs a single HTTP request. Returns (result, retry, retryErr, fatalErr).
// result != nil: success. fatalErr != nil: caller must return (nil, fatalErr).
// retry true: caller should try again and may log retryErr; false: return empty result.
func (c *Client) doOneRequest(ctx context.Context, url string, imageData []byte, attempt int) (*ClassificationResult, bool, error, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(imageData))
	if err != nil {
		c.log.Warn("HF client: failed to create request: %v", err)
		return nil, false, nil, nil
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Warn("HF client: request failed: %v", err)
		return nil, true, err, nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	_ = resp.Body.Close()

	return c.handleResponse(body, resp.StatusCode, attempt)
}

// handleResponse interprets HTTP response. Returns (result, retry, retryErr, fatalErr).
func (c *Client) handleResponse(body []byte, statusCode int, attempt int) (*ClassificationResult, bool, error, error) {
	if statusCode == http.StatusOK {
		result, err := c.parseResponse(body)
		if err != nil {
			c.log.Warn("HF client: parse response: %v", err)
			return nil, false, nil, nil
		}
		result.Model = c.model
		result.Tags = parseTagsFromCaption(result.Caption)
		return result, false, nil, nil
	}
	if statusCode == 503 {
		c.log.Debug("HF client: model loading (503), retry %d/%d", attempt+1, maxRetries)
		return nil, true, fmt.Errorf("model loading (503)"), nil
	}
	if statusCode == http.StatusUnauthorized {
		c.log.Warn("HF client: invalid API key (401)")
		return nil, false, nil, fmt.Errorf("hugging face API key invalid or expired (401)")
	}
	if statusCode == 429 {
		c.log.Debug("HF client: rate limit (429), retry %d/%d", attempt+1, maxRetries)
		return nil, true, fmt.Errorf("rate limit (429)"), nil
	}
	c.log.Warn("HF client: unexpected status %d: %s", statusCode, string(body))
	return nil, false, nil, nil
}

func (c *Client) parseResponse(body []byte) (*ClassificationResult, error) {
	var parsed hfImageCaptionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return &ClassificationResult{}, nil
	}
	return &ClassificationResult{
		Caption: strings.TrimSpace(parsed[0].GeneratedText),
	}, nil
}

// parseTagsFromCaption turns a caption string into a list of normalized tags
// (lowercase, alphanumeric + spaces, split on punctuation).
var tagWordRegex = regexp.MustCompile(`[a-zA-Z0-9]+`)

func parseTagsFromCaption(caption string) []string {
	if caption == "" {
		return nil
	}
	lower := strings.ToLower(caption)
	words := tagWordRegex.FindAllString(lower, -1)
	seen := make(map[string]bool)
	var tags []string
	for _, w := range words {
		if len(w) < 2 {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		tags = append(tags, w)
	}
	return tags
}
