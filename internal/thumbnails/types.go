// Package thumbnails has a single responsibility: generating and serving image
// thumbnails for media (video frame extraction, audio waveforms, WebP variants,
// responsive sizes, preview strips, BlurHash, and static placeholders). All types
// and functions in this package serve that pipeline.
package thumbnails

import (
	"context"
	"fmt"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/storage"
)

var (
	// ErrThumbnailPending indicates thumbnail is being generated
	ErrThumbnailPending = fmt.Errorf("thumbnail generation pending")

	// Responsive variants (16:9: 160x90, 320x180, 640x360) — single source of truth for width and URL suffix
	responsiveVariants = []ResponsiveVariant{
		{Width: 160, Suffix: "-sm"},
		{Width: 320, Suffix: "-md"},
		{Width: 640, Suffix: "-lg"},
	}
)

// ResponsiveVariant defines a responsive thumbnail size and its file suffix (avoids primitive obsession over []int + map[int]string).
type ResponsiveVariant struct {
	Width  int
	Suffix string
}

// MediaID is the stable identifier for a media item (UUID); used in path lookups to avoid string-heavy function arguments.
type MediaID string

// ThumbnailRequest groups parameters for thumbnail generation to avoid primitive obsession at the API boundary.
type ThumbnailRequest struct {
	MediaPath    string
	MediaID      string
	IsAudio      bool
	HighPriority bool
}

// PreviewThumbnailsRequest groups parameters for preview thumbnails generation.
type PreviewThumbnailsRequest struct {
	MediaPath    string
	MediaID      string
	HighPriority bool
}

// BlurHashUpdater updates BlurHash in metadata storage (e.g. MediaMetadataRepository)
type BlurHashUpdater interface {
	UpdateBlurHash(ctx context.Context, path string, hash string) error
}

// MediaInputResolver converts a stored media path (possibly an S3 key) to a
// form that ffmpeg can read — an absolute local path or a presigned HTTPS URL.
type MediaInputResolver interface {
	ResolveForFFmpeg(ctx context.Context, mediaPath string) (string, error)
}

// MediaIDProvider returns the set of all valid media IDs for orphan detection.
type MediaIDProvider interface {
	GetAllMediaIDs() map[string]bool
}

// Module handles thumbnail generation
type Module struct {
	log             *logger.Logger
	config          *config.Manager
	thumbnailDir    string
	dbModule           *database.Module   // used in Start() to wire blurHashUpdater after DB connects
	store              storage.Backend    // optional storage backend for thumbnail I/O
	mediaInputResolver MediaInputResolver // resolves S3 media keys to ffmpeg-readable URLs
	ffmpegPath      string
	ffprobePath     string
	jobHeap         jobHeap
	jobMu           sync.Mutex
	jobCond         *sync.Cond
	jobCap          int // max queue size
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	stats           Stats
	statsMu         sync.RWMutex
	healthMu        sync.RWMutex
	healthy         bool
	healthMsg       string
	blurHashUpdater BlurHashUpdater
	mediaIDProvider MediaIDProvider
	// inFlight tracks output paths currently queued or being processed to
	// prevent duplicate jobs when the background task and HTTP handlers both
	// call GenerateThumbnail for the same file before it is written to disk.
	// The value stored is a time.Time (enqueue timestamp) so that a background
	// cleanup goroutine can evict entries that are stale (e.g. from a worker
	// that exited without completing its job during shutdown).
	inFlight sync.Map // map[outputPath string]time.Time
}

// ThumbnailJob represents a thumbnail generation task
type ThumbnailJob struct {
	MediaPath   string // stored path (S3 key or local absolute path); used as DB key
	FFmpegInput string // ffmpeg-readable path or URL (set by generateThumbnail); empty until resolved
	OutputPath  string
	Width       int
	Height      int
	Timestamp   float64
	IsAudio     bool
}

// priorityJob wraps ThumbnailJob with priority (0=high/user-triggered, 1=low/background)
type priorityJob struct {
	job      *ThumbnailJob
	priority int
}

// tryGeneratePreviewOpts holds arguments for tryGeneratePreview to avoid string-heavy function arguments.
type tryGeneratePreviewOpts struct {
	MediaPath   string
	PreviewPath string
	PreviewURL  string
	Timestamp   float64
}

// buildPreviewURLListOpts holds arguments for buildPreviewURLList to avoid string-heavy and excess function arguments.
type buildPreviewURLListOpts struct {
	MediaPath string
	MediaID   string
	Count     int
	Duration  float64
	Cfg       *config.Config
}

// queuePreviewThumbnailsLoopOpts holds arguments for queuePreviewThumbnailsLoop to avoid string-heavy and excess function arguments.
type queuePreviewThumbnailsLoopOpts struct {
	MediaPath      string
	MediaID        string
	PreviewCount   int
	StartOffset    float64
	UsableDuration float64
	HighPriority   bool
}

// generateThumbnailRequest holds arguments for generateThumbnailFromRequest to avoid string-heavy function arguments.
type generateThumbnailRequest struct {
	MediaPath    string
	MediaID      string
	IsAudio      bool
	HighPriority bool
}

// generatePreviewThumbnailsRequest holds arguments for generatePreviewThumbnailsFromRequest to avoid string-heavy function arguments.
type generatePreviewThumbnailsRequest struct {
	MediaPath    string
	MediaID      string
	HighPriority bool
}

// ThumbnailSyncRequest groups parameters for synchronous thumbnail generation (avoids string-heavy function arguments).
type ThumbnailSyncRequest struct {
	MediaPath string
	MediaID   string
	IsAudio   bool
}

// getPreviewURLsRequest holds arguments for getPreviewURLsFromRequest to avoid string-heavy function arguments.
type getPreviewURLsRequest struct {
	MediaPath string
	MediaID   string
	Count     int
}

// queueMainPreviewThumbnailOpts holds arguments for queueMainPreviewThumbnail to avoid string-heavy function arguments.
type queueMainPreviewThumbnailOpts struct {
	MediaPath    string
	MainPath     string
	Timestamp    float64
	HighPriority bool
}

// buildPreviewJobOpts holds arguments for buildPreviewJob to avoid string-heavy function arguments.
type buildPreviewJobOpts struct {
	MediaPath  string
	OutputPath string
	Timestamp  float64
}

// webPFromAudioOpts holds arguments for generateWebPFromAudio to avoid string-heavy function arguments.
type webPFromAudioOpts struct {
	MediaPath  string
	OutputPath string
	Width      int
	Height     int
}

// jobHeap implements heap.Interface for priority queue (lower priority value = higher priority)
type jobHeap []*priorityJob

func (h *jobHeap) Len() int           { return len(*h) }
func (h *jobHeap) Less(i, j int) bool { return (*h)[i].priority < (*h)[j].priority }
func (h *jobHeap) Swap(i, j int)      { (*h)[i], (*h)[j] = (*h)[j], (*h)[i] }
func (h *jobHeap) Push(x interface{}) { *h = append(*h, x.(*priorityJob)) }
func (h *jobHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

// Stats holds thumbnail generation statistics
type Stats struct {
	Generated int64
	Failed    int64
	Pending   int64
	TotalSize int64
	// Cleanup stats
	OrphansRemoved int64
	ExcessRemoved  int64
	CorruptRemoved int64
	LastCleanup    time.Time
}

// CleanupResult holds the result of a single cleanup run.
type CleanupResult struct {
	OrphansRemoved int
	ExcessRemoved  int
	CorruptRemoved int
	BytesFreed     int64
}
