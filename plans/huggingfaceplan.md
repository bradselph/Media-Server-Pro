# Plan: Hugging Face Integration for Adult Media Classification
 
## Goal
Integrate Hugging Face Inference API to visually classify and auto-tag adult media content that has already been detected as mature by the existing keyword-based scanner. This adds visual analysis (image captioning / zero-shot classification) to generate descriptive tags for organizing adult content.
 
## Architecture Overview
 
The integration adds a new `pkg/huggingface/` client package and extends the existing scanner module to perform visual classification after keyword-based mature detection. Tags are stored in the existing `MediaMetadata.Tags` field (already persisted in the `media_tags` table).
 
**Flow:**
1. Existing scanner detects file as mature (keyword-based) → `ScanResult.IsMature = true`
2. New visual classifier extracts frames from the video using ffmpeg
3. Frames are sent to Hugging Face Inference API (image-to-text model like BLIP)
4. Generated captions/labels are parsed into tags
5. Tags are stored via `MediaMetadata.Tags` and propagated to the media module
 
## Implementation Steps
 
### Step 1: Configuration (`internal/config/config.go`)
 
Add `HuggingFaceConfig` struct to `Config`:
 
```go
type HuggingFaceConfig struct {
    Enabled       bool   `json:"enabled"`
    APIKey        string `json:"api_key"`        // HF API token (also via HUGGINGFACE_API_KEY env)
    Model         string `json:"model"`          // Default: "Salesforce/blip-image-captioning-large"
    EndpointURL   string `json:"endpoint_url"`   // Override for self-hosted inference endpoints
    MaxFrames     int    `json:"max_frames"`     // Frames to extract per video (default: 3)
    TimeoutSecs   int    `json:"timeout_secs"`   // HTTP timeout (default: 30)
    RateLimit     int    `json:"rate_limit"`      // Max requests per minute (default: 30)
    MaxConcurrent int    `json:"max_concurrent"` // Concurrent API calls (default: 2)
}
```
 
Add to `Config` struct, `FeaturesConfig` (add `EnableHuggingFace`), and `DefaultConfig()`.
 
Env var mappings:
- `HUGGINGFACE_API_KEY` → APIKey
- `HUGGINGFACE_MODEL` → Model
- `HUGGINGFACE_ENDPOINT_URL` → EndpointURL
- `HUGGINGFACE_MAX_FRAMES` → MaxFrames
- `FEATURES_ENABLE_HUGGINGFACE` → feature flag
 
### Step 2: HuggingFace API Client (`pkg/huggingface/client.go`)
 
New package with:
 
```go
// Client handles communication with HuggingFace Inference API
type Client struct {
    httpClient  *http.Client
    apiKey      string
    model       string
    endpointURL string
    rateLimiter *rate.Limiter
    log         *logger.Logger
}
 
// ClassifyImage sends an image to HF and returns generated tags/captions
func (c *Client) ClassifyImage(ctx context.Context, imageData []byte) (*ClassificationResult, error)
 
// ClassificationResult holds the API response
type ClassificationResult struct {
    Caption    string   // Raw generated text from image-to-text model
    Tags       []string // Parsed tags from caption
    Confidence float64  // Model confidence (if available)
    Model      string   // Model used
}
```
 
Key implementation details:
- POST image bytes to `https://api-inference.huggingface.co/models/{model}` with `Authorization: Bearer {key}`
- Parse response JSON (format depends on model type — image-to-text returns `[{"generated_text": "..."}]`)
- Extract tags by splitting caption into meaningful keywords
- Rate limiting via `golang.org/x/time/rate`
- Retry with backoff on 503 (model loading) responses
- Graceful degradation: return empty result on error, don't fail the scan
 
### Step 3: Frame Extraction (`pkg/huggingface/frames.go`)
 
```go
// ExtractFrames extracts N evenly-spaced frames from a video file using ffmpeg
func ExtractFrames(ctx context.Context, videoPath string, count int, tempDir string) ([]string, error)
```
 
- Uses `ffprobe` to get video duration
- Calculates timestamps at evenly-spaced intervals (e.g., 25%, 50%, 75% for 3 frames)
- Runs `ffmpeg -ss {time} -i {file} -frames:v 1 -q:v 2 {output}.jpg` for each
- Returns paths to extracted frame images in temp directory
- Caller responsible for cleanup
- For image files (jpg/png), just return the original path (no extraction needed)
 
### Step 4: Scanner Module Extension (`internal/scanner/mature.go`)
 
Add visual classification to the existing scanner module:
 
```go
// Add to Module struct:
type Module struct {
    // ... existing fields ...
    hfClient    *huggingface.Client  // nil if HF not configured
    tempDir     string               // for frame extraction
}
```
 
New methods:
```go
// ClassifyMatureContent performs visual classification on a file already detected as mature
func (m *Module) ClassifyMatureContent(ctx context.Context, path string) ([]string, error)
 
// ClassifyMatureDirectory runs visual classification on all mature-flagged files in a directory
func (m *Module) ClassifyMatureDirectory(ctx context.Context, dir string) (map[string][]string, error)
```
 
Flow within `ClassifyMatureContent`:
1. Check if HF client is configured (return nil if not)
2. Determine if file is video or image
3. For video: extract frames using `ExtractFrames()`
4. For image: use file directly
5. Send each frame to `hfClient.ClassifyImage()`
6. Aggregate tags from all frames, deduplicate
7. Return combined tag list
 
### Step 5: Background Task Integration (`cmd/server/main.go`)
 
Modify the existing `mature-content-scan` task to add visual classification step after keyword scan:
 
```go
// After existing keyword-based scan and auto-flagging...
// Run visual classification on newly flagged mature content
if scannerModule.HasHuggingFace() {
    for _, result := range allResults {
        if result.IsMature {
            tags, err := scannerModule.ClassifyMatureContent(ctx, result.Path)
            if err != nil {
                log.Warn("HF classification failed for %s: %v", result.Path, err)
                continue
            }
            if len(tags) > 0 {
                if err := mediaModule.UpdateTags(result.Path, tags); err != nil {
                    log.Warn("Failed to update tags for %s: %v", result.Path, err)
                }
            }
        }
    }
}
```
 
Also register a new optional background task `hf-classification` that runs classification on any mature content that hasn't been tagged yet.
 
### Step 6: Media Module Update (`internal/media/`)
 
Add or verify these methods exist:
```go
// UpdateTags sets tags for a media item (merges with existing tags)
func (m *Module) UpdateTags(path string, tags []string) error
```
 
This should update `MediaMetadata.Tags` via the existing metadata repository. The `media_tags` table already stores per-file tags as rows.
 
### Step 7: API Handlers (`api/handlers/`)
 
Add admin endpoints for triggering visual classification manually:
 
```go
// POST /api/admin/classify/file     — classify a single file
// POST /api/admin/classify/directory — classify all mature files in a directory
// GET  /api/admin/classify/status    — check HF integration status (configured, model, rate limit)
```
 
Handler file: `api/handlers/admin_classify.go`
 
### Step 8: Route Registration (`api/routes/routes.go`)
 
Add routes under admin group:
```go
admin.POST("/classify/file", h.ClassifyFile)
admin.POST("/classify/directory", h.ClassifyDirectory)
admin.GET("/classify/status", h.ClassifyStatus)
```
 
### Step 9: Module Wiring (`cmd/server/main.go`)
 
- Create HF client in main.go if config is enabled and API key is set
- Pass HF client to scanner module (add optional setter or constructor param)
- No new module needed — extends scanner
 
## Files to Create
1. `pkg/huggingface/client.go` — HF Inference API client
2. `pkg/huggingface/frames.go` — ffmpeg frame extraction
3. `api/handlers/admin_classify.go` — classification HTTP handlers
 
## Files to Modify
1. `internal/config/config.go` — add HuggingFaceConfig, defaults, env var parsing
2. `internal/scanner/mature.go` — add HF client field, ClassifyMatureContent methods
3. `cmd/server/main.go` — create HF client, wire to scanner, register background task
4. `api/handlers/handler.go` — add HasHuggingFace() helper or check in handler
5. `api/routes/routes.go` — register classify routes
6. `go.mod` / `go.sum` — add `golang.org/x/time` dependency (for rate limiter)
 
## Graceful Degradation
- If `HUGGINGFACE_API_KEY` is not set → visual classification is silently disabled
- If HF API returns errors → log warning, skip classification, keyword-based detection still works
- If ffmpeg is not available → skip frame extraction, log warning
- If model is loading (503) → retry up to 3 times with exponential backoff
- Rate limiting prevents exceeding HF API quotas
 
## Testing Approach
- Unit tests for tag parsing from captions
- Unit tests for frame timestamp calculation
- Integration test with mock HTTP server simulating HF API responses
- Manual testing with actual HF API (requires API key)
 