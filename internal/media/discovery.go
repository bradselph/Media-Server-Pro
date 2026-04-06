// Package media handles media file discovery, management, and metadata.
// It provides scanning, caching, and categorization of media files.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
	"media-server-pro/pkg/storage"
)

// Video file extensions
var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".m4v": true, ".mpg": true, ".mpeg": true,
	".3gp": true, ".ts": true, ".m2ts": true, ".vob": true, ".ogv": true,
}

// Partial/incomplete download extensions to skip during scanning
var partialDownloadExtensions = map[string]bool{
	".filepart": true, ".part": true, ".crdownload": true,
	".download": true, ".partial": true, ".tmp": true,
}

// Pre-compiled category detection patterns
var (
	// TV Show patterns
	tvPatternSxE     = regexp.MustCompile(`s\d{1,2}e\d{1,2}`)
	tvPatternNxN     = regexp.MustCompile(`\d{1,2}x\d{1,2}`)
	tvPatternSeason  = regexp.MustCompile(`season\s*\d`)
	tvPatternEpisode = regexp.MustCompile(`episode\s*\d`)
	tvPatternDir     = regexp.MustCompile(`/tv\s*shows?/`)
	tvPatternSeries  = regexp.MustCompile(`/series/`)

	// Movie patterns
	moviePatternDir     = regexp.MustCompile(`/movies?/`)
	moviePatternFilms   = regexp.MustCompile(`/films?/`)
	moviePatternYear    = regexp.MustCompile(`\(\d{4}\)`)
	moviePatternDotYear = regexp.MustCompile(`\.\d{4}\.`)

	// Music patterns
	musicPatternDir    = regexp.MustCompile(`/music/`)
	musicPatternAlbum  = regexp.MustCompile(`/albums?/`)
	musicPatternArtist = regexp.MustCompile(`/artists?/`)
	musicPatternSong   = regexp.MustCompile(`/songs?/`)
)

// Module implements media discovery and management
type Module struct {
	config       *config.Manager
	log          *logger.Logger
	dbModule     *database.Module
	media        map[string]*models.MediaItem
	mediaByID    map[string]*models.MediaItem // secondary index: ID -> item for O(1) lookups
	categories   map[string]*models.MediaCategory
	metadata     map[string]*Metadata
	metadataRepo repositories.MediaMetadataRepository
	// fingerprintIndex maps content fingerprints to the metadata path that owns them.
	// Built during loadMetadata and updated during scans so that createMediaItem can
	// detect moved/renamed files by matching fingerprint instead of path.
	fingerprintIndex map[string]string // fingerprint -> path
	mu               sync.RWMutex      // protects media, mediaByID, categories, metadata, fingerprintIndex, version, lastScan
	saveMu           sync.Mutex        // serializes concurrent saveMetadata calls to prevent MySQL lock waits
	dataDir          string
	scanning         bool // protected by healthMu; true while Scan() is running
	healthy          bool
	healthMsg        string
	healthMu         sync.RWMutex
	scanTicker       *time.Ticker
	scanDone         chan struct{}
	scanCtx          context.Context    // Canceled on shutdown; used by background saves
	scanCancel       context.CancelFunc // Cancels background scans on shutdown
	version          int64
	lastScan         time.Time
	initialScanDone  bool // true after the first scan attempt completes (success or failure)
	ffprobeAvail     bool
	ffprobePath      string // absolute path, set by checkFFProbe for use under systemd
	// onInitialScanDone is called when the first scan completes (with the current media list).
	// Used by the server to seed the suggestions module without polling.
	onInitialScanDone func([]*models.MediaItem)
	// videoStore, musicStore, uploadStore are optional storage backends.
	// When set, file operations use these instead of direct os.* calls.
	videoStore  storage.Backend
	musicStore  storage.Backend
	uploadStore storage.Backend
}

// SetStores sets the storage backends for media file operations.
func (m *Module) SetStores(video, music, upload storage.Backend) {
	m.videoStore = video
	m.musicStore = music
	m.uploadStore = upload
}

// Metadata holds extended metadata for a media item
type Metadata struct {
	// StableID is a UUID generated on first scan and persisted in the DB.
	// It is the public-facing MediaItem.ID, decoupled from the file path.
	StableID string `json:"stable_id,omitempty"`
	// ContentFingerprint is a SHA-256 of sampled file bytes.
	// Used to detect moved/renamed files and find duplicates.
	ContentFingerprint string             `json:"content_fingerprint,omitempty"`
	Views              int                `json:"views"`
	LastPlayed         *time.Time         `json:"last_played,omitempty"`
	DateAdded          time.Time          `json:"date_added"`
	PlaybackPos        map[string]float64 `json:"playback_positions,omitempty"` // user -> position
	IsMature           bool               `json:"is_mature"`
	MatureScore        float64            `json:"mature_score"`
	MatureReasons      []string           `json:"mature_reasons,omitempty"`
	Tags               []string           `json:"tags,omitempty"`
	Category           string             `json:"category,omitempty"`
	CustomMeta         map[string]string  `json:"custom_meta,omitempty"`
	// ProbeModTime records the file mtime at the time ffprobe was last run.
	// extractMetadata skips ffprobe when the file mtime hasn't advanced,
	// making subsequent hourly scans near-instant for unchanged libraries.
	ProbeModTime time.Time `json:"probe_mod_time,omitempty"`
	// BlurHash is set by the thumbnails module after generation; used for LQIP placeholders
	BlurHash string `json:"blur_hash,omitempty"`
}

// computeContentFingerprint computes a SHA-256 fingerprint of a media file.
// It samples the first 64 KB, the last 64 KB, and the file size. This is fast
// even for very large files (always reads at most 128 KB) while providing
// strong collision resistance. Two files with identical fingerprints can be
// treated as duplicates; a file that is moved or renamed keeps its fingerprint.
func computeContentFingerprint(path string) (string, error) {
	const sampleSize = 64 * 1024 // 64 KB

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	size := info.Size()
	h := sha256.New()

	// Write file size into the hash so that files with identical leading/trailing
	// bytes but different lengths produce different fingerprints.
	_, _ = fmt.Fprintf(h, "size:%d\n", size)

	// Read first 64 KB (or entire file if smaller)
	head := make([]byte, sampleSize)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read head of %s: %w", path, err)
	}
	h.Write(head[:n])

	// Read last 64 KB if the file is larger than one sample
	if size > int64(sampleSize) {
		tail := make([]byte, sampleSize)
		offset := size - int64(sampleSize)
		n, err = f.ReadAt(tail, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read tail of %s: %w", path, err)
		}
		h.Write(tail[:n])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// NewModule creates a new media module.
// The database module is stored but repositories are created during Start().
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for media")
	}

	return &Module{
		config:           cfg,
		log:              logger.New("media"),
		dbModule:         dbModule,
		media:            make(map[string]*models.MediaItem),
		mediaByID:        make(map[string]*models.MediaItem),
		categories:       make(map[string]*models.MediaCategory),
		metadata:         make(map[string]*Metadata),
		fingerprintIndex: make(map[string]string),
		dataDir:          cfg.Get().Directories.Data,
		scanDone:         make(chan struct{}),
	}, nil
}

// Name returns the module name
func (m *Module) Name() string {
	return "media"
}

// Start initializes the media module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting media module...")

	// Initialize MySQL repository (database is required)
	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}
	m.metadataRepo = mysql.NewMediaMetadataRepository(m.dbModule.GORM())
	m.log.Info("Using MySQL repository for media metadata")

	// Check for ffprobe
	m.checkFFProbe()

	// Create cancellable context for background scans
	ctx, cancel := context.WithCancel(context.Background())
	m.scanCtx = ctx
	m.scanCancel = cancel

	// Mark module as healthy immediately so server startup is not blocked.
	// Metadata loading and the initial file-system scan both run in a single
	// background goroutine: loadMetadata first (fast after the N+1 fix), then
	// Scan so that freshly-loaded metadata is available during file discovery.
	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Initializing (loading metadata)"
	m.healthMu.Unlock()

	m.log.Info("Starting metadata load and initial scan in background...")
	go func() {
		select {
		case <-ctx.Done():
			m.log.Info("Initial media start canceled before metadata load")
			return
		default:
		}

		// Load persisted metadata first so createMediaItem can populate views,
		// tags, etc. without extra DB round trips during the directory walk.
		if err := m.loadMetadata(); err != nil {
			m.log.Warn("Failed to load metadata, starting fresh: %v", err)
		}

		select {
		case <-ctx.Done():
			m.log.Info("Initial media scan cancelled after metadata load")
			return
		default:
		}

		m.healthMu.Lock()
		m.healthMsg = "Initializing (scan in progress)"
		m.healthMu.Unlock()

		if err := m.Scan(); err != nil {
			m.log.Error("Initial scan failed: %v", err)
			m.healthMu.Lock()
			m.healthMsg = fmt.Sprintf("Scan failed: %v (retrying on next interval)", err)
			m.initialScanDone = true // Mark ready even on failure so handlers stop returning 503
			m.healthMu.Unlock()
		} else {
			m.mu.RLock()
			count := len(m.media)
			m.mu.RUnlock()
			m.healthMu.Lock()
			m.healthMsg = fmt.Sprintf("Running (%d items)", count)
			m.initialScanDone = true
			m.healthMu.Unlock()
			m.log.Info("Initial media scan completed with %d items", count)
		}
		if cb := m.onInitialScanDone; cb != nil {
			items := m.ListMedia(Filter{})
			cb(items)
		}
	}()

	// Start background scan loop
	cfg := m.config.Get()
	if cfg.Features.EnableAutoDiscovery {
		m.scanTicker = time.NewTicker(1 * time.Hour)
		go m.scanLoop()
	}

	m.log.Info("Media module started (metadata load and scan running in background)")
	return nil
}

// Stop gracefully stops the module. When EnableAutoDiscovery is false, scanDone is not closed (scanTicker is nil).
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping media module...")

	// Cancel any running background scans (also cancels in-flight background saves)
	if m.scanCancel != nil {
		m.scanCancel()
	}

	// Stop periodic scan loop
	if m.scanTicker != nil {
		m.scanTicker.Stop()
		close(m.scanDone)
	}

	// Save metadata using a background context with a generous deadline.
	// The shared server shutdown ctx has a 30s timeout, but saving 200+ items to a
	// remote MySQL host at ~300ms each takes ~60-90s — far exceeding that budget.
	// Using a background context lets the save complete without racing the deadline;
	// the server waits for Stop() to return, so all data is persisted before exit.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer saveCancel()
	if err := m.saveMetadata(saveCtx); err != nil {
		m.log.Error("Failed to save metadata during shutdown: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(m.healthy),
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// IsScanning reports whether a scan is currently running.
// Used by the ListMedia handler to include a scanning hint in the response.
func (m *Module) IsScanning() bool {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return m.scanning
}

// IsReady reports whether the initial media scan has completed at least once.
// Before this returns true, the mediaByID map is empty and all ID-based lookups
// will fail. Handlers use this to return 503 "initializing" instead of 404.
func (m *Module) IsReady() bool {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return m.initialScanDone
}

// SetOnInitialScanDone sets a callback invoked when the first scan completes with the current media list.
// Must be called before Start(). Used by the server to seed the suggestions module.
func (m *Module) SetOnInitialScanDone(fn func([]*models.MediaItem)) {
	m.onInitialScanDone = fn
}

// checkFFProbe checks if ffprobe is available
func (m *Module) checkFFProbe() {
	path, err := helpers.FindBinary("ffprobe")
	m.ffprobeAvail = err == nil
	if m.ffprobeAvail {
		m.ffprobePath = path
		m.log.Info("ffprobe found at %s, extended metadata extraction enabled", path)
	} else {
		m.log.Warn("ffprobe not found, extended metadata extraction disabled")
	}
}

// scanLoop runs periodic media scans
func (m *Module) scanLoop() {
	for {
		select {
		case <-m.scanTicker.C:
			if err := m.Scan(); err != nil {
				m.log.Error("Periodic scan failed: %v", err)
			}
		case <-m.scanDone:
			return
		}
	}
}

// Scan scans configured directories for media files
func (m *Module) Scan() error {
	m.healthMu.Lock()
	m.scanning = true
	m.healthMu.Unlock()
	defer func() {
		m.healthMu.Lock()
		m.scanning = false
		m.healthMu.Unlock()
	}()

	m.log.Info("Starting media scan...")
	start := time.Now()

	cfg := m.config.Get()
	newMedia := make(map[string]*models.MediaItem)

	// Scan video directory
	if cfg.Directories.Videos != "" {
		m.log.Debug("Scanning video directory: %s", cfg.Directories.Videos)
		if err := m.scanDirectory(m.scanCtx, cfg.Directories.Videos, models.MediaTypeVideo, newMedia, m.videoStore); err != nil {
			m.log.Error("Failed to scan videos directory: %v", err)
		}
	}

	// Scan music directory
	if cfg.Directories.Music != "" {
		m.log.Debug("Scanning music directory: %s", cfg.Directories.Music)
		if err := m.scanDirectory(m.scanCtx, cfg.Directories.Music, models.MediaTypeAudio, newMedia, m.musicStore); err != nil {
			m.log.Error("Failed to scan music directory: %v", err)
		}
	}

	// Scan uploads directory
	if cfg.Directories.Uploads != "" && cfg.Features.EnableUploads {
		m.log.Debug("Scanning uploads directory: %s", cfg.Directories.Uploads)
		if err := m.scanDirectory(m.scanCtx, cfg.Directories.Uploads, models.MediaTypeUnknown, newMedia, m.uploadStore); err != nil {
			m.log.Error("Failed to scan uploads directory: %v", err)
		}
	}

	// ── Local dedup by content fingerprint ─────────────────────────────
	// When identical file content exists at multiple filesystem paths
	// (e.g. copied into two configured directories), keep only one
	// representative entry.  The winner is the copy with the most views;
	// ties are broken by earliest DateAdded so the original discovery
	// date is preserved.
	// Dedup: fp used for logging is truncated to 12 chars with a bounds check below.
	var dupsRemoved int
	m.mu.RLock()
	fpWinner := make(map[string]string, len(newMedia)) // fingerprint -> winning path
	for path := range newMedia {
		meta := m.metadata[path]
		if meta == nil || meta.ContentFingerprint == "" {
			continue
		}
		fp := meta.ContentFingerprint
		existing, seen := fpWinner[fp]
		if !seen {
			fpWinner[fp] = path
			continue
		}
		// Pick the better of the two entries.
		existingMeta := m.metadata[existing]
		keepExisting := true
		if meta.Views > existingMeta.Views {
			keepExisting = false
		} else if meta.Views == existingMeta.Views && meta.DateAdded.Before(existingMeta.DateAdded) {
			keepExisting = false
		}
		fpLog := fp
		if len(fp) > 12 {
			fpLog = fp[:12]
		}
		if keepExisting {
			delete(newMedia, path)
			m.log.Info("Dedup: skipping %s (duplicate of %s, fingerprint %s…)", path, existing, fpLog)
		} else {
			delete(newMedia, existing)
			fpWinner[fp] = path
			m.log.Info("Dedup: skipping %s (duplicate of %s, fingerprint %s…)", existing, path, fpLog)
		}
		dupsRemoved++
	}
	m.mu.RUnlock()
	if dupsRemoved > 0 {
		m.log.Info("Dedup: removed %d local duplicate(s) by content fingerprint", dupsRemoved)
	}

	// Build ID index for O(1) lookups by ID
	newMediaByID := make(map[string]*models.MediaItem, len(newMedia))
	for _, item := range newMedia {
		newMediaByID[item.ID] = item
	}

	// Extract metadata for all items using a bounded worker pool.
	// This runs BEFORE the map swap so that Duration, Width, Height, etc. are
	// populated before any API request can see the new items.
	if m.ffprobeAvail {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)
		for _, item := range newMedia {
			wg.Add(1)
			sem <- struct{}{}
			go func(it *models.MediaItem) {
				defer wg.Done()
				defer func() { <-sem }()
				m.extractMetadata(it)
			}(item)
		}
		wg.Wait()
	}

	// Update media cache while preserving concurrent metadata updates.
	// Merge strategy: if an item exists in the old cache with the same ID,
	// preserve its runtime-updated fields (Views, LastPlayed from IncrementViews).
	// Also carry over ffprobe data (Duration, Width, Height, Codec, etc.) from
	// old items when extractMetadata skipped ffprobe for unchanged files.
	m.mu.Lock()
	oldMediaByID := m.mediaByID
	for id, newItem := range newMediaByID {
		if oldItem, existed := oldMediaByID[id]; existed {
			// Preserve runtime playback metadata
			if oldItem.Views > newItem.Views {
				newItem.Views = oldItem.Views
			}
			if oldItem.LastPlayed != nil && (newItem.LastPlayed == nil || oldItem.LastPlayed.After(*newItem.LastPlayed)) {
				newItem.LastPlayed = oldItem.LastPlayed
			}
			if newItem.ThumbnailURL == "" && oldItem.ThumbnailURL != "" {
				newItem.ThumbnailURL = oldItem.ThumbnailURL
			}
			// Preserve ffprobe-extracted fields when extractMetadata skipped
			// (unchanged file — ProbeModTime matched). Without this, Duration,
			// dimensions, codec, etc. reset to zero on every hourly re-scan.
			if newItem.Duration == 0 && oldItem.Duration > 0 {
				newItem.Duration = oldItem.Duration
			}
			if newItem.Bitrate == 0 && oldItem.Bitrate > 0 {
				newItem.Bitrate = oldItem.Bitrate
			}
			if newItem.Width == 0 && oldItem.Width > 0 {
				newItem.Width = oldItem.Width
				newItem.Height = oldItem.Height
			}
			if newItem.Codec == "" && oldItem.Codec != "" {
				newItem.Codec = oldItem.Codec
			}
			if newItem.Container == "" && oldItem.Container != "" {
				newItem.Container = oldItem.Container
			}
			if len(newItem.Metadata) == 0 && len(oldItem.Metadata) > 0 {
				newItem.Metadata = oldItem.Metadata
			}
		}
	}
	m.media = newMedia
	m.mediaByID = newMediaByID
	m.version++
	m.lastScan = time.Now()
	m.mu.Unlock()

	// Update categories
	m.updateCategories()

	duration := time.Since(start)
	m.log.Info("Media scan complete: %d items found in %v", len(newMedia), duration)
	m.healthMu.Lock()
	m.healthMsg = fmt.Sprintf("Running (%d items)", len(newMedia))
	m.healthMu.Unlock()

	// Save metadata in background; concurrent scans can start overlapping saves (saveMu serializes DB writes).
	go func() {
		if err := m.saveMetadata(m.scanCtx); err != nil {
			m.log.Error("Failed to save metadata: %v", err)
		}
	}()

	return nil
}

// scanDirectory recursively scans a directory for media files.
// When store is non-nil and remote (S3/B2), the backend's Walk is used and
// paths in result are the backend's AbsPath keys. When store is nil or local,
// the local filesystem is walked.
func (m *Module) scanDirectory(ctx context.Context, dir string, defaultType models.MediaType, result map[string]*models.MediaItem, store storage.Backend) error {
	if store != nil && !store.IsLocal() {
		return m.scanRemoteStore(ctx, store, dir, defaultType, result)
	}

	// Local filesystem walk.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	_, err = os.Stat(absDir)
	if os.IsNotExist(err) {
		m.log.Debug("Directory does not exist, skipping: %s", absDir)
		return nil
	}

	return filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		// Check for cancellation periodically
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			m.log.Warn("Error accessing path %s: %v", path, err)
			return nil // Continue scanning
		}

		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // skip symlinks — they may point outside the media directory
		}

		ext := strings.ToLower(filepath.Ext(path))

		// Skip partial/incomplete downloads
		if partialDownloadExtensions[ext] {
			return nil
		}

		var mediaType models.MediaType

		if videoExtensions[ext] {
			mediaType = models.MediaTypeVideo
		} else if helpers.IsAudioExtension(ext) {
			mediaType = models.MediaTypeAudio
		} else if defaultType != models.MediaTypeUnknown {
			mediaType = defaultType
		} else {
			return nil // Not a media file
		}

		item := m.createMediaItem(path, info, mediaType)
		result[path] = item

		return nil
	})
}

// scanRemoteStore walks the storage backend and registers discovered media.
// Paths in result are backend AbsPath keys (e.g. S3 keys).
func (m *Module) scanRemoteStore(ctx context.Context, store storage.Backend, _ string, defaultType models.MediaType, result map[string]*models.MediaItem) error {
	return store.Walk(ctx, "", func(relPath string, info storage.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			m.log.Warn("Error walking remote store at %s: %v", relPath, err)
			return nil
		}
		if info.IsDir {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		if partialDownloadExtensions[ext] {
			return nil
		}

		var mediaType models.MediaType
		if videoExtensions[ext] {
			mediaType = models.MediaTypeVideo
		} else if helpers.IsAudioExtension(ext) {
			mediaType = models.MediaTypeAudio
		} else if defaultType != models.MediaTypeUnknown {
			mediaType = defaultType
		} else {
			return nil
		}

		absKey := store.AbsPath(relPath)
		item := m.createMediaItemFromStorageInfo(absKey, info, mediaType)
		result[absKey] = item
		return nil
	})
}

// createMediaItemFromStorageInfo creates a MediaItem from a storage.FileInfo.
// Used when scanning remote backends where os.FileInfo is unavailable.
func (m *Module) createMediaItemFromStorageInfo(absKey string, info storage.FileInfo, mediaType models.MediaType) *models.MediaItem {
	item := &models.MediaItem{
		Path:         absKey,
		Name:         info.Name,
		Type:         mediaType,
		Size:         info.Size,
		DateModified: info.ModTime,
		Metadata:     make(map[string]string),
	}

	// Hold the write lock for the full check-and-set to prevent a race where
	// two concurrent goroutines (e.g. the scan walk and an ffprobe worker) both
	// observe hasMeta==false for the same key and assign different stable IDs.
	m.mu.Lock()
	meta, hasMeta := m.metadata[absKey]
	if hasMeta {
		m.mu.Unlock()
		item.ID = meta.StableID
		item.Views = meta.Views
		item.IsMature = meta.IsMature
		item.Tags = meta.Tags
		if meta.Category != "" {
			item.Category = meta.Category
		} else {
			item.Category = string(mediaType)
		}
		if meta.LastPlayed != nil {
			item.LastPlayed = new(*meta.LastPlayed)
		}
		item.DateAdded = meta.DateAdded
	} else {
		item.ID = uuid.New().String()
		item.Category = string(mediaType)
		item.DateAdded = time.Now()
		m.metadata[absKey] = &Metadata{
			StableID:    item.ID,
			DateAdded:   item.DateAdded,
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.mu.Unlock()
	}

	if item.ID == "" {
		item.ID = uuid.New().String()
	}

	return item
}

// createMediaItem creates a MediaItem from file info.
// The MediaItem.ID is a stable UUID loaded from the database (generated on
// first encounter and persisted). This decouples the public ID from the
// filesystem path so that IDs survive file moves and config changes.
func (m *Module) createMediaItem(path string, info os.FileInfo, mediaType models.MediaType) *models.MediaItem {
	item := &models.MediaItem{
		Path:         path,
		Name:         info.Name(),
		Type:         mediaType,
		Size:         info.Size(),
		DateModified: info.ModTime(),
		Metadata:     make(map[string]string),
	}

	// Get existing metadata (includes StableID loaded from DB at startup)
	m.mu.RLock()
	meta, hasMeta := m.metadata[path]
	m.mu.RUnlock()

	if !hasMeta {
		// No metadata at this path. Compute a content fingerprint and check
		// whether we already know this file under a different (old) path.
		// This detects files that were moved or renamed between scans.
		fp, fpErr := computeContentFingerprint(path)
		if fpErr != nil {
			m.log.Debug("Could not fingerprint %s: %v", path, fpErr)
		}

		if fp != "" {
			m.mu.RLock()
			oldPath, found := m.fingerprintIndex[fp]
			var oldMeta *Metadata
			if found && oldPath != path {
				oldMeta = m.metadata[oldPath]
			}
			m.mu.RUnlock()

			if oldMeta != nil {
				// Before assuming a move, verify the old path no longer exists.
				// If both paths are present on disk the file was copied (duplicate),
				// not moved.  In that case treat this as a new file and let the
				// post-scan dedup pass pick the winner.
				if _, statErr := os.Stat(oldPath); statErr != nil && os.IsNotExist(statErr) {
					// Old path gone — genuine move/rename.
					m.log.Info("Detected moved file: %s -> %s (fingerprint %s…)", oldPath, path, fp[:12])
					m.mu.Lock()
					m.metadata[path] = oldMeta
					delete(m.metadata, oldPath)
					m.fingerprintIndex[fp] = path
					m.mu.Unlock()
					meta = oldMeta
					hasMeta = true
				} else {
					// Old path still exists — duplicate file, not a move.
					m.log.Debug("Duplicate content at %s and %s (fingerprint %s…)", oldPath, path, fp[:12])
				}
			} else if found {
				// Same file at same path — just use existing metadata normally.
				// This happens when the fingerprint was computed for the first time
				// on a file that already had metadata by path.
			} else {
				// Truly new file — record fingerprint for future move detection
				m.log.Debug("New content fingerprint for %s: %s…", path, fp[:12])
			}

			// Store the fingerprint regardless (new or migrated)
			if !hasMeta {
				now := time.Now()
				item.DateAdded = now
				m.mu.Lock()
				m.metadata[path] = &Metadata{
					ContentFingerprint: fp,
					DateAdded:          now,
					PlaybackPos:        make(map[string]float64),
					CustomMeta:         make(map[string]string),
				}
				meta = m.metadata[path]
				m.fingerprintIndex[fp] = path
				m.mu.Unlock()
			} else if meta.ContentFingerprint == "" {
				m.mu.Lock()
				meta.ContentFingerprint = fp
				m.fingerprintIndex[fp] = path
				m.mu.Unlock()
			}
		} else {
			// Fingerprint failed — create basic metadata entry
			now := time.Now()
			item.DateAdded = now
			m.mu.Lock()
			m.metadata[path] = &Metadata{
				DateAdded:   now,
				PlaybackPos: make(map[string]float64),
				CustomMeta:  make(map[string]string),
			}
			meta = m.metadata[path]
			m.mu.Unlock()
		}
	}

	if hasMeta {
		item.Views = meta.Views
		item.LastPlayed = meta.LastPlayed
		item.DateAdded = meta.DateAdded
		item.IsMature = meta.IsMature
		item.MatureScore = meta.MatureScore
		item.Tags = meta.Tags
		item.BlurHash = meta.BlurHash

		if meta.StableID != "" {
			item.ID = meta.StableID
		}

		// Compute fingerprint for existing files that predate fingerprint support
		if meta.ContentFingerprint == "" {
			if fp, err := computeContentFingerprint(path); err == nil && fp != "" {
				m.mu.Lock()
				meta.ContentFingerprint = fp
				m.fingerprintIndex[fp] = path
				m.mu.Unlock()
			}
		}
	}

	// Assign a stable UUID if not already set (new or pre-stable-ID file)
	if item.ID == "" {
		newID := uuid.New().String()
		item.ID = newID
		m.mu.Lock()
		meta.StableID = newID
		m.mu.Unlock()
	}

	// Use stored category from DB when available (from categorizer or admin), else auto-detect from path and persist
	if hasMeta && meta.Category != "" {
		item.Category = meta.Category
	} else {
		item.Category = m.detectCategory(path)
		// Persist auto-detected category into metadata so it is saved to DB and used on next load
		m.mu.Lock()
		if m.metadata[path] != nil {
			m.metadata[path].Category = item.Category
		}
		m.mu.Unlock()
	}

	// Check whether a thumbnail already exists on disk and pre-populate the URL.
	// Thumbnail files are named by stable UUID; the public URL uses the same ID
	// so the handler can apply mature-content checks.
	cfg := m.config.Get()
	thumbFile := filepath.Join(cfg.Directories.Thumbnails, item.ID+".jpg")
	if _, err := os.Stat(thumbFile); err == nil {
		item.ThumbnailURL = "/thumbnail?id=" + item.ID
	}

	return item
}

// ffprobeResult holds parsed ffprobe output for metadata extraction.
type ffprobeResult struct {
	Format struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

// extractMetadata extracts metadata using ffprobe.
// It skips ffprobe when the file mtime matches the stored ProbeModTime so
// that subsequent hourly scans are near-instant for unchanged media files.
func (m *Module) extractMetadata(item *models.MediaItem) {
	// Check whether we already have up-to-date probe data for this file.
	m.mu.RLock()
	meta, hasMeta := m.metadata[item.Path]
	m.mu.RUnlock()
	if hasMeta && !meta.ProbeModTime.IsZero() && !item.DateModified.After(meta.ProbeModTime) {
		m.log.Debug("Skipping ffprobe for unchanged file: %s", item.Path)
		return
	}

	ctx, cancel := context.WithTimeout(m.scanCtx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		item.Path,
	)

	output, err := cmd.Output()
	if err != nil {
		m.log.Debug("Failed to extract metadata for %s: %v", item.Path, err)
		return
	}

	var probe ffprobeResult
	if err := json.Unmarshal(output, &probe); err != nil {
		m.log.Debug("Failed to parse ffprobe output for %s: %v", item.Path, err)
		return
	}

	// Apply probe data directly to the item and record the mtime.
	// The item pointer is the same one in newMedia, so this populates
	// Duration/Width/Height/etc. before the map swap makes it visible.
	m.mu.Lock()
	m.applyProbeData(item, &probe)
	if m.metadata[item.Path] != nil {
		m.metadata[item.Path].ProbeModTime = item.DateModified
	}
	m.mu.Unlock()
}

// applyProbeData applies parsed ffprobe data to a media item.
// Must be called with m.mu held.
func (m *Module) applyProbeData(current *models.MediaItem, probe *ffprobeResult) {
	if probe.Format.Duration != "" {
		if dur, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			current.Duration = dur
		}
	}
	if probe.Format.BitRate != "" {
		if rate, err := strconv.ParseInt(probe.Format.BitRate, 10, 64); err == nil {
			current.Bitrate = rate
		}
	}
	applyStreamData(current, probe)
	for k, v := range probe.Format.Tags {
		current.Metadata[k] = v
	}
}

// applyStreamData extracts codec and dimension info from probe streams.
func applyStreamData(current *models.MediaItem, probe *ffprobeResult) {
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			current.Width = stream.Width
			current.Height = stream.Height
			current.Codec = stream.CodecName
			return
		}
		if stream.CodecType == "audio" && current.Codec == "" {
			current.Codec = stream.CodecName
		}
	}
}

// detectCategory auto-detects media category from path
func (m *Module) detectCategory(path string) string {
	// Path already contains the full file path, just normalize it
	lower := strings.ToLower(path)
	// Normalize path separators to forward slash for regex matching
	lower = strings.ReplaceAll(lower, "\\", "/")

	// TV Show patterns
	tvPatterns := []*regexp.Regexp{
		tvPatternSxE, tvPatternNxN, tvPatternSeason,
		tvPatternEpisode, tvPatternDir, tvPatternSeries,
	}
	for _, re := range tvPatterns {
		if re.MatchString(lower) {
			return "tv_shows"
		}
	}

	// Movie patterns
	moviePatterns := []*regexp.Regexp{
		moviePatternDir, moviePatternFilms, moviePatternYear, moviePatternDotYear,
	}
	for _, re := range moviePatterns {
		if re.MatchString(lower) {
			return "movies"
		}
	}

	// Music patterns
	musicPatterns := []*regexp.Regexp{
		musicPatternDir, musicPatternAlbum, musicPatternArtist, musicPatternSong,
	}
	for _, re := range musicPatterns {
		if re.MatchString(lower) {
			return "music"
		}
	}

	// Default based on type
	// Use filepath.Rel to check if path is contained within music directory
	// This prevents false matches like "/media/music" matching "/media/musicvideos"
	musicDir := m.config.Get().Directories.Music
	if musicDir != "" {
		rel, err := filepath.Rel(musicDir, path)
		// If path is inside musicDir, rel will not start with ".." and will not error
		if err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !strings.HasPrefix(rel, "..") {
			return "music"
		}
	}

	return "uncategorized"
}

// updateCategories updates category counts
func (m *Module) updateCategories() {
	m.mu.Lock()
	defer m.mu.Unlock()

	counts := make(map[string]int)
	for _, item := range m.media {
		counts[item.Category]++
	}

	m.categories = make(map[string]*models.MediaCategory)
	for name, count := range counts {
		m.categories[name] = &models.MediaCategory{
			Name:        name,
			DisplayName: cases.Title(language.English).String(strings.ReplaceAll(name, "_", " ")),
			Count:       count,
		}
	}
}

// GetMedia returns a media item by path
func (m *Module) GetMedia(path string) (*models.MediaItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.media[path]
	if !exists {
		return nil, fmt.Errorf("media not found: %s", path)
	}
	return item, nil
}

// GetMediaByID returns a media item by ID using the secondary index for O(1) lookups.
func (m *Module) GetMediaByID(id string) (*models.MediaItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if item, exists := m.mediaByID[id]; exists {
		return item, nil
	}
	return nil, fmt.Errorf("media not found with ID: %s", id)
}

// GetAllMediaIDs returns the set of all known media IDs for bulk operations
// like thumbnail cleanup. Callers must not mutate the returned map.
func (m *Module) GetAllMediaIDs() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make(map[string]bool, len(m.mediaByID))
	for id := range m.mediaByID {
		ids[id] = true
	}
	return ids
}

// GetMediaNamesByIDs returns a map of stable-ID → display-name for the given IDs.
// IDs not found in the library are omitted from the result. Uses a single lock
// acquisition for the entire batch instead of one per ID.
func (m *Module) GetMediaNamesByIDs(ids []string) map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make(map[string]string, len(ids))
	for _, id := range ids {
		if item, exists := m.mediaByID[id]; exists {
			names[id] = item.Name
		}
	}
	return names
}

// ListMedia returns all media items with optional filtering
func (m *Module) ListMedia(filter Filter) []*models.MediaItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []*models.MediaItem
	for _, item := range m.media {
		if filter.Matches(item) {
			items = append(items, item)
		}
	}

	filter.SortItems(items)
	return items
}

// ListMediaPaginated returns a page of media items using DB-level filtering and pagination.
// Use this for admin or large libraries to avoid loading the full catalog. Total is the
// total matching rows in the DB; items may be fewer if some paths are no longer in the
// current scan (e.g. deleted files not yet pruned). Type and Tags are filtered at DB level.
func (m *Module) ListMediaPaginated(ctx context.Context, filter Filter, limit, offset int) (items []*models.MediaItem, total int64, err error) {
	if m.metadataRepo == nil {
		return nil, 0, fmt.Errorf("metadata repository not available")
	}

	repoFilter := repositories.MediaFilter{
		Category: filter.Category,
		IsMature: filter.IsMature,
		Search:   filter.Search,
		Type:     string(filter.Type),
		Tags:     filter.Tags,
		SortDesc: filter.SortDesc,
		Limit:    limit,
		Offset:   offset,
	}
	switch filter.SortBy {
	case "views":
		repoFilter.SortBy = "views"
	case "date_added", "date_modified":
		repoFilter.SortBy = "date_added"
	default:
		repoFilter.SortBy = "path" // name, path, etc.
	}

	metas, total, err := m.metadataRepo.ListFiltered(ctx, repoFilter)
	if err != nil {
		return nil, 0, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, meta := range metas {
		item, ok := m.media[meta.Path]
		if !ok {
			continue // path no longer in catalog (e.g. file deleted)
		}
		if filter.Matches(item) {
			items = append(items, item)
		}
	}

	// ListFiltered already applied sort in DB; re-sort only if we need Type/Tags ordering
	if filter.Type != "" || len(filter.Tags) > 0 {
		filter.SortItems(items)
	}
	return items, total, nil
}

// Filter defines filtering options for media listing.
type Filter struct {
	Type     models.MediaType
	Category string
	Search   string
	Tags     []string
	IsMature *bool
	SortBy   string
	SortDesc bool
}

// SortItems sorts a slice of media items in place according to the filter's
// SortBy and SortDesc fields.  Exported so handlers can re-sort a merged
// (local + receiver) list after appending items.
func (f Filter) SortItems(items []*models.MediaItem) {
	sort.Slice(items, func(i, j int) bool {
		switch f.SortBy {
		case "name":
			return items[i].Name < items[j].Name
		case "date_added":
			return items[i].DateAdded.Before(items[j].DateAdded)
		case "date_modified":
			return items[i].DateModified.Before(items[j].DateModified)
		case "size":
			return items[i].Size < items[j].Size
		case "views":
			return items[i].Views < items[j].Views
		case "duration":
			return items[i].Duration < items[j].Duration
		case "type":
			return items[i].Type < items[j].Type
		case "category":
			return items[i].Category < items[j].Category
		case "bitrate":
			return items[i].Bitrate < items[j].Bitrate
		case "codec":
			if items[i].Codec != items[j].Codec {
				return items[i].Codec < items[j].Codec
			}
			return items[i].Name < items[j].Name
		case "is_mature":
			if items[i].IsMature != items[j].IsMature {
				return !items[i].IsMature // false < true: non-mature first in ascending
			}
			return items[i].Name < items[j].Name
		case "random":
			return items[i].ID < items[j].ID // stable placeholder; shuffled below
		default:
			return items[i].Name < items[j].Name
		}
	})

	if f.SortBy == "random" {
		rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		return
	}

	if f.SortDesc {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
}

// Matches reports whether a media item passes the filter's criteria.
// Exported so handlers can apply the same filter logic to receiver items.
func (f Filter) Matches(item *models.MediaItem) bool {
	if f.Type != "" && f.Type != models.MediaTypeUnknown && item.Type != f.Type {
		return false
	}
	if f.Category != "" && item.Category != f.Category {
		return false
	}
	if !f.matchesSearch(item) {
		return false
	}
	if f.IsMature != nil && item.IsMature != *f.IsMature {
		return false
	}
	if !f.matchesTags(item) {
		return false
	}
	return true
}

// matchesSearch checks whether the item matches the search term in the filter.
// Matches against item name, category, and tags — NOT the filesystem path,
// which is an internal implementation detail hidden from API consumers.
func (f Filter) matchesSearch(item *models.MediaItem) bool {
	if f.Search == "" {
		return true
	}
	search := strings.ToLower(f.Search)
	if strings.Contains(strings.ToLower(item.Name), search) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Category), search) {
		return true
	}
	for _, tag := range item.Tags {
		if strings.Contains(strings.ToLower(tag), search) {
			return true
		}
	}
	return false
}

// matchesTags checks whether the item has at least one of the filter's required tags.
func (f Filter) matchesTags(item *models.MediaItem) bool {
	if len(f.Tags) == 0 {
		return true
	}
	for _, tag := range f.Tags {
		for _, itemTag := range item.Tags {
			if tag == itemTag {
				return true
			}
		}
	}
	return false
}

// GetCategories returns all categories
func (m *Module) GetCategories() []*models.MediaCategory {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cats := make([]*models.MediaCategory, 0, len(m.categories))
	for _, cat := range m.categories {
		cats = append(cats, cat)
	}
	return cats
}

// HasFingerprint reports whether a content fingerprint matches any local media file.
// Used to deduplicate receiver items that exist on both master and slave.
func (m *Module) HasFingerprint(fp string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.fingerprintIndex[fp]
	return ok
}

// IsFingerprintMature reports whether a content fingerprint matches a local media
// file that is flagged as mature. Used to gate access to receiver items that
// correspond to mature local content.
func (m *Module) IsFingerprintMature(fp string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path, ok := m.fingerprintIndex[fp]
	if !ok {
		return false
	}
	item, ok := m.media[path]
	return ok && item.IsMature
}

// GetStats returns media statistics
func (m *Module) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		LastScan: m.lastScan,
		Version:  m.version,
	}

	for _, item := range m.media {
		stats.TotalCount++
		stats.TotalSize += item.Size
		switch item.Type {
		case models.MediaTypeVideo:
			stats.VideoCount++
		case models.MediaTypeAudio:
			stats.AudioCount++
		}
	}

	return stats
}

// Stats holds media statistics
type Stats struct {
	TotalCount int       `json:"total_count"`
	VideoCount int       `json:"video_count"`
	AudioCount int       `json:"audio_count"`
	TotalSize  int64     `json:"total_size"`
	LastScan   time.Time `json:"last_scan"`
	Version    int64     `json:"version"`
}

// IncrementViews increments view count for a media item (DB and in-memory updated separately; not atomic).
func (m *Module) IncrementViews(ctx context.Context, path string) error {
	// Use repository if available
	if m.metadataRepo != nil {
		if err := m.metadataRepo.IncrementViews(ctx, path); err != nil {
			m.log.Error("Failed to increment views via repository: %v", err)
		}
	}

	// Also update in-memory cache
	m.mu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		meta = &Metadata{
			DateAdded:   time.Now(),
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.metadata[path] = meta
	}

	meta.Views++
	meta.LastPlayed = new(time.Now())

	if item, exists := m.media[path]; exists {
		item.Views = meta.Views
		item.LastPlayed = meta.LastPlayed
	}
	m.mu.Unlock()

	return nil
}

// UpdatePlaybackPosition updates playback position for a user
func (m *Module) UpdatePlaybackPosition(ctx context.Context, path, userID string, position float64) error {
	// Use repository if available
	if m.metadataRepo != nil {
		if err := m.metadataRepo.UpdatePlaybackPosition(ctx, path, userID, position); err != nil {
			m.log.Error("Failed to update playback position via repository: %v", err)
		}
	}

	// Also update in-memory cache
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, exists := m.metadata[path]
	if !exists {
		meta = &Metadata{
			DateAdded:   time.Now(),
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.metadata[path] = meta
	}

	if meta.PlaybackPos == nil {
		meta.PlaybackPos = make(map[string]float64)
	}
	meta.PlaybackPos[userID] = position

	return nil
}

// GetPlaybackPosition gets playback position for a user
func (m *Module) GetPlaybackPosition(ctx context.Context, path, userID string) float64 {
	// Try repository first if available
	if m.metadataRepo != nil {
		if position, err := m.metadataRepo.GetPlaybackPosition(ctx, path, userID); err == nil {
			return position
		}
	}

	// Fallback to in-memory cache
	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, exists := m.metadata[path]
	if !exists || meta.PlaybackPos == nil {
		return 0
	}
	return meta.PlaybackPos[userID]
}

// BatchGetPlaybackPositions returns positions for multiple media IDs for a user.
// IDs are resolved to paths using the in-memory index. Returns a map of ID → position.
// IDs with no stored position are omitted from the result.
func (m *Module) BatchGetPlaybackPositions(ctx context.Context, ids []string, userID string) map[string]float64 {
	if len(ids) == 0 || userID == "" {
		return map[string]float64{}
	}

	// Build ID → path mapping from in-memory index.
	m.mu.RLock()
	idToPath := make(map[string]string, len(ids))
	for _, id := range ids {
		if item, ok := m.mediaByID[id]; ok {
			idToPath[id] = item.Path
		}
	}
	m.mu.RUnlock()

	if len(idToPath) == 0 {
		return map[string]float64{}
	}

	paths := make([]string, 0, len(idToPath))
	for _, p := range idToPath {
		paths = append(paths, p)
	}

	result := make(map[string]float64, len(ids))

	if m.metadataRepo != nil {
		pathPositions, err := m.metadataRepo.BatchGetPlaybackPositions(ctx, paths, userID)
		if err == nil {
			// Re-key from path → position to id → position.
			for id, path := range idToPath {
				if pos, ok := pathPositions[path]; ok && pos > 0 {
					result[id] = pos
				}
			}
			return result
		}
	}

	// Fallback: in-memory cache.
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, path := range idToPath {
		if meta, ok := m.metadata[path]; ok && meta.PlaybackPos != nil {
			if pos := meta.PlaybackPos[userID]; pos > 0 {
				result[id] = pos
			}
		}
	}
	return result
}

// ClearPlaybackPosition removes the saved resume position for one user+path pair.
// Called when the user deletes a single watch-history entry so the player
// does not show a stale resume prompt on the next open.
func (m *Module) ClearPlaybackPosition(ctx context.Context, path, userID string) {
	// Reset to 0 in the repository (avoids adding a new Delete method to the interface)
	if m.metadataRepo != nil {
		_ = m.metadataRepo.UpdatePlaybackPosition(ctx, path, userID, 0)
	}

	// Remove from in-memory cache so the fallback path also returns 0
	m.mu.Lock()
	defer m.mu.Unlock()
	if meta, exists := m.metadata[path]; exists && meta.PlaybackPos != nil {
		delete(meta.PlaybackPos, userID)
	}
}

// ClearAllPlaybackPositions removes every saved resume position for a given user (in-memory and DB).
func (m *Module) ClearAllPlaybackPositions(userID string) {
	// Persist deletion to DB so positions do not reappear after restart.
	if m.metadataRepo != nil {
		_ = m.metadataRepo.DeleteAllPlaybackPositionsByUser(context.Background(), userID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, meta := range m.metadata {
		if meta.PlaybackPos != nil {
			delete(meta.PlaybackPos, userID)
		}
	}
}

// SetMatureFlag sets the mature content flag for a media item
func (m *Module) SetMatureFlag(path string, isMature bool, score float64, reasons []string) error {
	m.mu.Lock()

	meta, exists := m.metadata[path]
	if !exists {
		meta = &Metadata{
			DateAdded:   time.Now(),
			PlaybackPos: make(map[string]float64),
			CustomMeta:  make(map[string]string),
		}
		m.metadata[path] = meta
	}

	meta.IsMature = isMature
	meta.MatureScore = score
	meta.MatureReasons = reasons

	if item, exists := m.media[path]; exists {
		item.IsMature = isMature
		item.MatureScore = score
	}
	m.mu.Unlock()

	// Persist only this item (not all 261) so the mature scan task remains
	// responsive to context cancellation and doesn't block for O(n) time.
	if err := m.saveMetadataItem(path); err != nil {
		m.log.Error("Failed to save metadata after SetMatureFlag: %v", err)
		return err
	}

	return nil
}

// GetVersion returns the current media version (changes on each scan)
func (m *Module) GetVersion() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.version
}

// GetConfig returns the current configuration
func (m *Module) GetConfig() *config.Config {
	return m.config.Get()
}

// Persistence functions
func (m *Module) loadMetadata() error {
	ctx := context.Background()
	repoMetadata, err := m.metadataRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to load metadata from database: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Convert repository metadata to internal format and build the fingerprint index
	// so that createMediaItem can detect moved/renamed files by content fingerprint.
	for path, repoMeta := range repoMetadata {
		meta := m.convertRepoToInternal(repoMeta)
		m.metadata[path] = meta
		if meta.ContentFingerprint != "" {
			m.fingerprintIndex[meta.ContentFingerprint] = path
		}
	}

	m.log.Info("Loaded %d metadata entries from database (%d with fingerprints)", len(m.metadata), len(m.fingerprintIndex))
	return nil
}

// saveMetadataItem saves a single item's metadata to the DB. Use this for
// targeted writes (e.g. after SetMatureFlag, UpdateMetadata) instead of
// the full-table saveMetadata() so that single-item operations don't block
// for O(n) time and remain responsive to context cancellation.
func (m *Module) saveMetadataItem(path string) error {
	if m.metadataRepo == nil {
		return nil
	}
	m.mu.RLock()
	meta, ok := m.metadata[path]
	if !ok {
		m.mu.RUnlock()
		return nil
	}
	repoMeta := m.convertInternalToRepo(path, meta)
	m.mu.RUnlock()

	// Use saveMu to serialize with the bulk saveMetadata loop: both code paths
	// delete+insert on media_tags for the same row, causing lock-wait timeouts
	// when they run concurrently against the same file.
	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := m.metadataRepo.Upsert(ctx, path, repoMeta); err != nil {
		m.log.Warn("Failed to save metadata for %s: %v", path, err)
		return err
	}
	return nil
}

func (m *Module) saveMetadata(ctx context.Context) error {
	// Snapshot under the read lock so we don't hold it across slow DB writes.
	// Concurrent saveMetadata calls would each hold RLock for O(n * db_latency),
	// causing MySQL "Lock wait timeout exceeded" when upserts race on the same rows.
	m.mu.RLock()
	snapshot := make(map[string]*repositories.MediaMetadata, len(m.metadata))
	for path, meta := range m.metadata {
		snapshot[path] = m.convertInternalToRepo(path, meta)
	}
	m.mu.RUnlock()

	// Serialize DB writes: prevents concurrent upserts to the same rows from
	// racing and hitting MySQL row-lock timeouts.
	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	consecutiveErrors := 0
	saved := 0
	for path, repoMeta := range snapshot {
		// Bail out if the parent context (shutdown or scan cancellation) is done.
		select {
		case <-ctx.Done():
			m.log.Info("Metadata save interrupted (%d/%d saved): %v", saved, len(snapshot), ctx.Err())
			return ctx.Err()
		default:
		}

		// Use a per-item deadline derived from the parent context so that
		// individual slow rows don't stall shutdown beyond the server timeout.
		itemCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := m.metadataRepo.Upsert(itemCtx, path, repoMeta)
		cancel()
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors <= 2 {
				m.log.Warn("Failed to save metadata for %s (will retry next scan): %v", path, err)
			}
			if consecutiveErrors >= 5 {
				m.log.Error("Aborting metadata save: DB appears unreachable after %d consecutive errors (%d/%d saved)", consecutiveErrors, saved, len(snapshot))
				return fmt.Errorf("metadata save aborted: %d consecutive DB errors", consecutiveErrors)
			}
		} else {
			consecutiveErrors = 0
			saved++
		}
	}

	return nil
}

// convertRepoToInternal converts repository metadata to internal format
func (m *Module) convertRepoToInternal(repoMeta *repositories.MediaMetadata) *Metadata {
	meta := &Metadata{
		StableID:           repoMeta.StableID,
		ContentFingerprint: repoMeta.ContentFingerprint,
		Views:              repoMeta.Views,
		IsMature:           repoMeta.IsMature,
		MatureScore:        repoMeta.MatureScore,
		Tags:               repoMeta.Tags,
		Category:           repoMeta.Category,
		PlaybackPos:        make(map[string]float64),
		CustomMeta:         make(map[string]string),
		MatureReasons:      []string{},
	}

	// Parse DateAdded
	if dateAdded, err := time.Parse(time.RFC3339, repoMeta.DateAdded); err == nil {
		meta.DateAdded = dateAdded
	} else {
		meta.DateAdded = time.Now()
	}

	// Parse LastPlayed
	if repoMeta.LastPlayed != nil {
		if lastPlayed, err := time.Parse(time.RFC3339, *repoMeta.LastPlayed); err == nil {
			meta.LastPlayed = &lastPlayed
		}
	}

	// Restore ProbeModTime so extractMetadata can skip unchanged files across restarts
	if repoMeta.ProbeModTime != nil {
		meta.ProbeModTime = *repoMeta.ProbeModTime
	}
	meta.BlurHash = repoMeta.BlurHash

	return meta
}

// convertInternalToRepo converts internal metadata to repository format
func (m *Module) convertInternalToRepo(path string, meta *Metadata) *repositories.MediaMetadata {
	repoMeta := &repositories.MediaMetadata{
		Path:               path,
		StableID:           meta.StableID,
		ContentFingerprint: meta.ContentFingerprint,
		Views:              meta.Views,
		DateAdded:          meta.DateAdded.Format(time.RFC3339),
		IsMature:           meta.IsMature,
		MatureScore:        meta.MatureScore,
		Category:           meta.Category,
		Tags:               meta.Tags,
		BlurHash:           meta.BlurHash,
	}
	if !meta.ProbeModTime.IsZero() {
		repoMeta.ProbeModTime = new(meta.ProbeModTime)
	}

	if meta.LastPlayed != nil {
		repoMeta.LastPlayed = new(meta.LastPlayed.Format(time.RFC3339))
	}

	return repoMeta
}

// ClassifyStats holds classification progress statistics.
type ClassifyStats struct {
	TotalMedia       int              `json:"total_media"`
	MatureTotal      int              `json:"mature_total"`
	MatureClassified int              `json:"mature_classified"`
	MaturePending    int              `json:"mature_pending"`
	RecentItems      []ClassifiedItem `json:"recent_items"`
}

// ClassifiedItem is a summary of a mature item that has been classified with tags.
type ClassifiedItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Tags         []string `json:"tags"`
	MatureScore  float64  `json:"mature_score"`
	DateModified string   `json:"date_modified"`
}

// GetClassifyStats returns classification progress: how many mature items have been
// classified (tagged) vs pending. Also returns the most recently modified classified items.
func (m *Module) GetClassifyStats(recentLimit int) ClassifyStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stats ClassifyStats
	stats.TotalMedia = len(m.media)

	type scored struct {
		item *models.MediaItem
		mod  time.Time
	}
	var classified []scored

	for path, item := range m.media {
		if !item.IsMature {
			continue
		}
		stats.MatureTotal++
		if len(item.Tags) > 0 {
			stats.MatureClassified++
			mod := item.DateModified
			if meta, ok := m.metadata[path]; ok && meta != nil && !meta.DateAdded.IsZero() {
				mod = meta.DateAdded
			}
			classified = append(classified, scored{item: item, mod: mod})
		} else {
			stats.MaturePending++
		}
	}

	// Sort by modification time descending to get most recent
	sort.Slice(classified, func(i, j int) bool {
		return classified[i].mod.After(classified[j].mod)
	})
	if len(classified) > recentLimit {
		classified = classified[:recentLimit]
	}

	stats.RecentItems = make([]ClassifiedItem, len(classified))
	for i, c := range classified {
		stats.RecentItems[i] = ClassifiedItem{
			ID:           c.item.ID,
			Name:         c.item.Name,
			Tags:         c.item.Tags,
			MatureScore:  c.item.MatureScore,
			DateModified: c.mod.Format(time.RFC3339),
		}
	}
	return stats
}
