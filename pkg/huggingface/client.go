// Package huggingface provides a client for the Hugging Face Inference API
// for image classification of media content (see https://huggingface.co/docs/inference-providers/index
// and https://huggingface.co/docs/inference-providers/en/tasks/image-classification).
package huggingface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"media-server-pro/internal/logger"
)

const (
	// defaultBaseURL is the official Inference API for image/embedding tasks. The router
	// (router.huggingface.co/v1) is for chat completions only and returns 404 for image tasks.
	defaultBaseURL    = "https://api-inference.huggingface.co"
	maxRetries        = 3
	initialRetryDelay = 2 * time.Second
	maxResponseSize   = 10 * 1024 * 1024 // 10MB cap for HF API responses
)

// ClassificationResult holds the result of an image classification request.
type ClassificationResult struct {
	Labels     []LabelScore // All labels with scores from the model
	Tags       []string     // Label names extracted as tags
	Confidence float64      // Top label confidence score
	Model      string       // Model used
}

// LabelScore is a single label + confidence pair from image classification.
type LabelScore struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// Client handles communication with the Hugging Face Inference Providers API.
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

// ClassifyImage sends an image to the Hugging Face Inference Providers API and returns
// classification labels with confidence scores. On error or if the client is not
// configured (no API key), returns an empty result without failing (graceful degradation).
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

// sleepBeforeRetry sleeps for exponential backoff delay. Returns false if context is done.
// Delay for attempt 1 = 2s, attempt 2 = 4s (initialRetryDelay * 2^(attempt-1)).
func (c *Client) sleepBeforeRetry(ctx context.Context, attempt int) bool {
	delay := initialRetryDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
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

// parseResponse handles both image-classification ([{"label","score"}]) and
// image-to-text ([{"generated_text"}]) response formats from the HF API.
func (c *Client) parseResponse(body []byte) (*ClassificationResult, error) {
	// Try image-classification format first: [{"label": "nsfw", "score": 0.95}]
	var classLabels []LabelScore
	if err := json.Unmarshal(body, &classLabels); err == nil && len(classLabels) > 0 && classLabels[0].Label != "" {
		result := &ClassificationResult{Labels: classLabels}
		for _, ls := range classLabels {
			tag := strings.ToLower(strings.TrimSpace(ls.Label))
			if tag != "" {
				result.Tags = append(result.Tags, tag)
			}
			if ls.Score > result.Confidence {
				result.Confidence = ls.Score
			}
		}
		return result, nil
	}

	// Fallback: image-to-text format: [{"generated_text": "..."}]
	var captions []struct {
		GeneratedText string `json:"generated_text"`
	}
	if err := json.Unmarshal(body, &captions); err != nil {
		return nil, fmt.Errorf("unrecognized HF response format: %w", err)
	}
	if len(captions) == 0 || captions[0].GeneratedText == "" {
		return &ClassificationResult{}, nil
	}
	caption := strings.TrimSpace(captions[0].GeneratedText)
	return &ClassificationResult{
		Tags:       parseWordsAsTags(caption),
		Confidence: 1.0,
	}, nil
}

// parseWordsAsTags extracts lowercase alphanumeric words (2+ chars) from a caption string.
func parseWordsAsTags(caption string) []string {
	lower := strings.ToLower(caption)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	seen := make(map[string]bool)
	var tags []string
	for _, w := range fields {
		if len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		tags = append(tags, w)
	}
	return tags
}
