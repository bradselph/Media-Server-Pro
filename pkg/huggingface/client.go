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

// NewClient creates a new Hugging Face API client. If apiKey is empty, the client
// will return empty results without making requests (graceful degradation).
func NewClient(apiKey, model, endpointURL string, requestsPerMinute int, timeout time.Duration, log *logger.Logger) *Client {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 30
	}
	rps := float64(requestsPerMinute) / 60.0
	if rps < 0.5 {
		rps = 0.5
	}
	rl := rate.NewLimiter(rate.Limit(rps), 2)

	baseURL := defaultBaseURL
	if endpointURL != "" {
		baseURL = strings.TrimSuffix(endpointURL, "/")
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		apiKey:      apiKey,
		model:       model,
		endpointURL: baseURL,
		rateLimiter: rl,
		log:         log,
	}
}

// ClassifyImage sends an image to the Hugging Face Inference API and returns
// generated captions/tags. On error or if the client is not configured (no API key),
// returns an empty result without failing (graceful degradation).
func (c *Client) ClassifyImage(ctx context.Context, imageData []byte) (*ClassificationResult, error) {
	if c.apiKey == "" {
		return &ClassificationResult{Model: c.model}, nil
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return &ClassificationResult{Model: c.model}, nil
	}

	url := c.endpointURL + "/models/" + c.model
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := initialRetryDelay
			for i := 0; i < attempt-1; i++ {
				delay = time.Duration(float64(delay) * retryBackoffFactor)
			}
			select {
			case <-ctx.Done():
				return &ClassificationResult{Model: c.model}, nil
			case <-time.After(delay):
			}
		}

		// New request per attempt: body reader is consumed by Do(), so retries would send empty body otherwise.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(imageData))
		if err != nil {
			c.log.Warn("HF client: failed to create request: %v", err)
			return &ClassificationResult{Model: c.model}, nil
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			c.log.Warn("HF client: request failed: %v", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			result, err := c.parseResponse(body)
			if err != nil {
				c.log.Warn("HF client: parse response: %v", err)
				return &ClassificationResult{Model: c.model}, nil
			}
			result.Model = c.model
			result.Tags = parseTagsFromCaption(result.Caption)
			return result, nil
		}

		if resp.StatusCode == 503 {
			lastErr = fmt.Errorf("model loading (503)")
			c.log.Debug("HF client: model loading (503), retry %d/%d", attempt+1, maxRetries)
			continue
		}

		c.log.Warn("HF client: unexpected status %d: %s", resp.StatusCode, string(body))
		return &ClassificationResult{Model: c.model}, nil
	}

	c.log.Warn("HF client: all retries failed: %v", lastErr)
	return &ClassificationResult{Model: c.model}, nil
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
