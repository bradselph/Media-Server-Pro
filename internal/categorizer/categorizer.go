// Package categorizer provides automatic media categorization based on patterns.
// Categorization results are propagated to the media module via the handler layer
// (api/handlers/handlers.go CategorizeFile/CategorizeDirectory) so that
// MediaItem.Category is updated in the media metadata store.
package categorizer

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// Category represents a media category
type Category string

const (
	CategoryMovies        Category = "Movies"
	CategoryTVShows       Category = "TV Shows"
	CategoryDocumentaries Category = "Documentaries"
	CategoryAnime         Category = "Anime"
	CategoryMusic         Category = "Music"
	CategoryPodcasts      Category = "Podcasts"
	CategoryAudiobooks    Category = "Audiobooks"
	CategoryUncategorized Category = "Uncategorized"
)

// CategorizedItem represents a categorized media item
type CategorizedItem struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Path           string     `json:"-"`
	Category       Category   `json:"category"`
	Confidence     float64    `json:"confidence"`
	DetectedInfo   *MediaInfo `json:"detected_info,omitempty"`
	CategorizedAt  time.Time  `json:"categorized_at"`
	ManualOverride bool       `json:"manual_override"`
}

// MediaInfo holds detected media information
type MediaInfo struct {
	Title    string `json:"title,omitempty"`
	Year     int    `json:"year,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	ShowName string `json:"show_name,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
}

// Module handles media categorization
type Module struct {
	config    *config.Manager
	log       *logger.Logger
	dbModule  *database.Module
	repo      repositories.CategorizedItemRepository
	items     map[string]*CategorizedItem
	mu        sync.RWMutex
	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex
	patterns  *categoryPatterns
}

// PathContext holds path components used for categorization detection.
// Replaces primitive obsession with multiple filename/dirPath/fullPath string arguments.
type PathContext struct {
	Filename string // base name, typically lowercased
	DirPath  string // directory path, typically lowercased
	FullPath string // full path, typically lowercased
}

// NewPathContext builds a PathContext from an absolute file path.
func NewPathContext(path string) PathContext {
	return PathContext{
		Filename: strings.ToLower(filepath.Base(path)),
		DirPath:  strings.ToLower(filepath.Dir(path)),
		FullPath: strings.ToLower(path),
	}
}

// categoryPatterns holds compiled regex patterns for detection
type categoryPatterns struct {
	tvShowPatterns      []*regexp.Regexp
	animePatterns       []*regexp.Regexp
	docPatterns         []*regexp.Regexp
	musicPatterns       []*regexp.Regexp
	podcastPatterns     []*regexp.Regexp
	audiobookPatterns   []*regexp.Regexp
	movieYearMatch      *regexp.Regexp // IC-06: precompiled — was compiled per detectMovie call
	movieYearStripTitle *regexp.Regexp // IC-06: precompiled — was compiled per extractMovieTitle call
}

// NewModule creates a new categorizer module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:   cfg,
		log:      logger.New("categorizer"),
		dbModule: dbModule,
		items:    make(map[string]*CategorizedItem),
		patterns: compilePatterns(),
	}
}

func compilePatterns() *categoryPatterns {
	return &categoryPatterns{
		tvShowPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)[Ss](\d{1,2})[Ee](\d{1,2})`), // S01E01
			regexp.MustCompile(`(?i)(\d{1,2})x(\d{1,2})`),        // 1x01
			regexp.MustCompile(`(?i)season\s*(\d+)`),             // Season 1
			regexp.MustCompile(`(?i)episode\s*(\d+)`),            // Episode 1
			regexp.MustCompile(`(?i)\.S(\d{2})\.`),               // .S01.
			regexp.MustCompile(`(?i)\[(\d{1,2})x(\d{1,2})]`),     // [1x01]
		},
		animePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\[(\d{2,4})]`),     // [01] or [0001]
			regexp.MustCompile(`(?i)ep\.?\s*(\d+)`),    // Ep 01 or Ep.01
			regexp.MustCompile(`(?i)\s-\s(\d{2,3})\s`), // - 01 - or - 001 -
			regexp.MustCompile(`(?i)(subbed|dubbed|raw|fansub|hardsub)`),
			regexp.MustCompile(`(?i)(720p|1080p|480p).*?(hevc|h\.?264|x264|x265)`),
		},
		docPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(documentary|docu|doku)`),
			regexp.MustCompile(`(?i)(national\s*geographic|discovery|bbc\s*earth|nature)`),
			regexp.MustCompile(`(?i)(history|science|wildlife|planet\s*earth)`),
		},
		musicPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\.(mp3|flac|m4a|aac|ogg|wma|wav)$`),
			regexp.MustCompile(`(?i)(album|single|ep|mixtape|soundtrack)`),
			regexp.MustCompile(`(?i)(\d{4})\s*-?\s*(album|single|ep)`),
		},
		podcastPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(podcast|pod|episode\s*\d+)`),
			regexp.MustCompile(`(?i)(interview|talk\s*show|radio)`),
		},
		audiobookPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(audiobook|audio\s*book|narrated)`),
			regexp.MustCompile(`(?i)(unabridged|abridged|read\s*by)`),
			regexp.MustCompile(`(?i)(chapter|ch\.?\s*\d+|part\s*\d+)`),
		},
		movieYearMatch:      regexp.MustCompile(`(?i)[.\s(]?(19|20)\d{2}[.\s)]?`),
		movieYearStripTitle: regexp.MustCompile(`[.\s(]?(19|20)\d{2}[.\s)]?.*$`),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "categorizer"
}

// Start initializes the module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting categorizer module...")

	m.repo = mysqlrepo.NewCategorizedItemRepository(m.dbModule.GORM())

	if err := m.loadItems(); err != nil {
		m.log.Warn("Failed to load categorized items: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Categorizer module started (%d items loaded)", len(m.items))
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping categorizer module...")

	// Use a bounded context for the DB flush so shutdown cannot block indefinitely.
	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := m.saveItems(stopCtx); err != nil {
		m.log.Error("Failed to save categorized items: %v", err)
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

// CategorizeFile categorizes a single file
func (m *Module) CategorizeFile(path string) *CategorizedItem {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already categorized with manual override
	if existing, ok := m.items[path]; ok && existing.ManualOverride {
		return existing
	}

	item := &CategorizedItem{
		ID:            uuid.New().String(),
		Name:          filepath.Base(path),
		Path:          path,
		CategorizedAt: time.Now(),
	}

	ctx := NewPathContext(path)

	// Detect category based on patterns
	category, confidence, info := m.detectCategory(ctx)

	item.Category = category
	item.Confidence = confidence
	item.DetectedInfo = info

	m.items[path] = item
	m.saveItem(path, item)
	// Return a copy to prevent caller from mutating the stored item
	return copyItem(item)
}

// copyItem creates a deep copy of a CategorizedItem
func copyItem(src *CategorizedItem) *CategorizedItem {
	if src == nil {
		return nil
	}
	dst := &CategorizedItem{
		ID:             src.ID,
		Name:           src.Name,
		Path:           src.Path,
		Category:       src.Category,
		Confidence:     src.Confidence,
		CategorizedAt:  src.CategorizedAt,
		ManualOverride: src.ManualOverride,
	}
	if src.DetectedInfo != nil {
		dst.DetectedInfo = &MediaInfo{
			Title:    src.DetectedInfo.Title,
			Year:     src.DetectedInfo.Year,
			Season:   src.DetectedInfo.Season,
			Episode:  src.DetectedInfo.Episode,
			ShowName: src.DetectedInfo.ShowName,
			Artist:   src.DetectedInfo.Artist,
			Album:    src.DetectedInfo.Album,
		}
	}
	return dst
}

// detectCategory determines the category for a file
func (m *Module) detectCategory(ctx PathContext) (Category, float64, *MediaInfo) {
	info := &MediaInfo{}

	if cat, conf, ok := m.detectTVShow(ctx, info); ok {
		return cat, conf, info
	}
	if cat, conf, ok := m.detectAnime(ctx, info); ok {
		return cat, conf, info
	}
	if cat, conf, ok := m.detectDocumentary(ctx); ok {
		return cat, conf, info
	}
	if cat, conf, ok := m.detectAudiobook(ctx); ok {
		return cat, conf, info
	}
	if cat, conf, ok := m.detectPodcast(ctx); ok {
		return cat, conf, info
	}
	if cat, conf, ok := m.detectMusic(ctx, info); ok {
		return cat, conf, info
	}
	if cat, conf, ok := m.detectMovie(ctx, info); ok {
		return cat, conf, info
	}

	return CategoryUncategorized, 0.0, info
}

// detectTVShow checks for TV show patterns in the filename and directory path.
func (m *Module) detectTVShow(ctx PathContext, info *MediaInfo) (Category, float64, bool) {
	for _, pattern := range m.patterns.tvShowPatterns {
		if matches := pattern.FindStringSubmatch(ctx.Filename); len(matches) > 0 {
			info.ShowName = m.extractShowName(ctx.Filename, pattern)
			if len(matches) > 1 {
				info.Season = parseNumber(matches[1])
			}
			if len(matches) > 2 {
				info.Episode = parseNumber(matches[2])
			}
			return CategoryTVShows, 0.9, true
		}
	}
	if strings.Contains(ctx.DirPath, "tv") || strings.Contains(ctx.DirPath, "series") ||
		strings.Contains(ctx.DirPath, "shows") {
		return CategoryTVShows, 0.7, true
	}
	return "", 0, false
}

// detectAnime checks for anime patterns in the filename, directory path, and full path.
func (m *Module) detectAnime(ctx PathContext, _ *MediaInfo) (Category, float64, bool) {
	animeScore := 0.0
	for _, pattern := range m.patterns.animePatterns {
		if pattern.MatchString(ctx.Filename) || pattern.MatchString(ctx.FullPath) {
			animeScore += 0.3
		}
	}
	if strings.Contains(ctx.DirPath, "anime") {
		animeScore += 0.5
	}
	if animeScore > 1.0 {
		animeScore = 1.0
	}
	if animeScore >= 0.5 {
		return CategoryAnime, animeScore, true
	}
	return "", 0, false
}

// detectDocumentary checks for documentary patterns in the filename, directory path, and full path.
func (m *Module) detectDocumentary(ctx PathContext) (Category, float64, bool) {
	for _, pattern := range m.patterns.docPatterns {
		if pattern.MatchString(ctx.Filename) || pattern.MatchString(ctx.FullPath) {
			return CategoryDocumentaries, 0.8, true
		}
	}
	if strings.Contains(ctx.DirPath, "documentary") || strings.Contains(ctx.DirPath, "documentaries") ||
		strings.Contains(ctx.DirPath, "docs") {
		return CategoryDocumentaries, 0.7, true
	}
	return "", 0, false
}

// detectAudiobook checks for audiobook patterns in the filename, directory path, and full path.
func (m *Module) detectAudiobook(ctx PathContext) (Category, float64, bool) {
	for _, pattern := range m.patterns.audiobookPatterns {
		if pattern.MatchString(ctx.Filename) || pattern.MatchString(ctx.FullPath) {
			return CategoryAudiobooks, 0.8, true
		}
	}
	if strings.Contains(ctx.DirPath, "audiobook") || strings.Contains(ctx.DirPath, "audio book") {
		return CategoryAudiobooks, 0.7, true
	}
	return "", 0, false
}

// detectPodcast checks for podcast patterns in the filename, directory path, and full path.
func (m *Module) detectPodcast(ctx PathContext) (Category, float64, bool) {
	for _, pattern := range m.patterns.podcastPatterns {
		if pattern.MatchString(ctx.Filename) || pattern.MatchString(ctx.FullPath) {
			return CategoryPodcasts, 0.7, true
		}
	}
	if strings.Contains(ctx.DirPath, "podcast") {
		return CategoryPodcasts, 0.8, true
	}
	return "", 0, false
}

// detectMusic checks for music patterns in the filename and directory path.
func (m *Module) detectMusic(ctx PathContext, info *MediaInfo) (Category, float64, bool) {
	for _, pattern := range m.patterns.musicPatterns {
		if pattern.MatchString(ctx.Filename) {
			info.Artist, info.Album = m.extractMusicInfo(ctx.Filename)
			return CategoryMusic, 0.8, true
		}
	}
	if strings.Contains(ctx.DirPath, "music") || strings.Contains(ctx.DirPath, "albums") ||
		strings.Contains(ctx.DirPath, "artists") {
		return CategoryMusic, 0.7, true
	}
	return "", 0, false
}

// detectMovie checks for movie patterns in the filename and directory path.
// matches[0] is the full regex match (e.g. "2020", ".2020.", "(2020)"); parseNumber extracts the year.
func (m *Module) detectMovie(ctx PathContext, info *MediaInfo) (Category, float64, bool) {
	if matches := m.patterns.movieYearMatch.FindStringSubmatch(ctx.Filename); len(matches) > 0 {
		info.Title = m.extractMovieTitle(ctx.Filename)
		info.Year = parseNumber(matches[0])
		return CategoryMovies, 0.7, true
	}
	if strings.Contains(ctx.DirPath, "movie") || strings.Contains(ctx.DirPath, "films") {
		info.Title = m.extractMovieTitle(ctx.Filename)
		return CategoryMovies, 0.6, true
	}
	return "", 0, false
}

func (m *Module) extractShowName(filename string, pattern *regexp.Regexp) string {
	// Get everything before the season/episode pattern
	loc := pattern.FindStringIndex(filename)
	if len(loc) == 0 || loc[0] == 0 {
		return ""
	}
	name := filename[:loc[0]]
	name = strings.Trim(name, ".-_ ")
	name = strings.ReplaceAll(name, ".", " ")
	return cases.Title(language.English).String(name)
}

func (m *Module) extractMovieTitle(filename string) string {
	// Remove extension
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)

	// Remove year and quality info
	name = m.patterns.movieYearStripTitle.ReplaceAllString(name, "")

	// Clean up
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.Trim(name, " -_")

	return cases.Title(language.English).String(name)
}

func (m *Module) extractMusicInfo(filename string) (artist, album string) {
	// Try "Artist - Album" format
	parts := strings.SplitN(filename, " - ", 2)
	if len(parts) == 2 {
		artist = strings.TrimSpace(parts[0])
		album = strings.TrimSuffix(strings.TrimSpace(parts[1]), filepath.Ext(parts[1]))
	}
	return
}

// parseNumber extracts the leading numeric portion from a string.
// Returns 0 if no leading digits are found.
// Handles formats strconv.Atoi cannot: "2020abc" -> 2020, "S01" -> 0 (R-04: NOT equivalent to strconv.Atoi).
func parseNumber(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	// Find the first digit and extract the leading numeric portion
	startIdx := -1
	for i, c := range s {
		if c >= '0' && c <= '9' {
			if startIdx == -1 {
				startIdx = i
			}
		} else if startIdx != -1 {
			// Stop at first non-digit after we've started collecting digits
			break
		}
	}

	if startIdx == -1 {
		return 0
	}

	// Extract the numeric portion
	var n int
	for i := startIdx; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}

// CategorizeDirectory categorizes all files in a directory (one lock and DB upsert per file).
func (m *Module) CategorizeDirectory(dir string) ([]*CategorizedItem, error) {
	var results []*CategorizedItem

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			m.log.Warn("CategorizeDirectory: skipping %s: %v", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // skip symlinks — they may point outside the media directory
		}

		// Only process media files
		if !helpers.IsMediaExtension(strings.ToLower(filepath.Ext(path))) {
			return nil
		}

		item := m.CategorizeFile(path)
		results = append(results, item)
		return nil
	})

	if err != nil {
		return results, err
	}

	// Items are persisted individually in CategorizeFile
	return results, nil
}

// GetCategory returns the category for a path
func (m *Module) GetCategory(path string) (Category, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if item, ok := m.items[path]; ok {
		return item.Category, true
	}
	return CategoryUncategorized, false
}

// SetCategory manually sets a category (with override)
func (m *Module) SetCategory(path string, category Category) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[path]
	if !ok {
		item = &CategorizedItem{
			ID:            uuid.New().String(),
			Name:          filepath.Base(path),
			Path:          path,
			CategorizedAt: time.Now(),
		}
		m.items[path] = item
	}

	item.Category = category
	item.ManualOverride = true
	item.Confidence = 1.0
	m.saveItem(path, item)

	m.log.Info("Manually set category for %s: %s", path, category)
}

// GetByCategory returns all items in a category
// Returns copies to prevent unsynchronized access to internal items
func (m *Module) GetByCategory(category Category) []*CategorizedItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []*CategorizedItem
	for _, item := range m.items {
		if item.Category == category {
			items = append(items, copyItem(item))
		}
	}
	return items
}

// GetStats returns categorization statistics
func (m *Module) GetStats() CategoryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := CategoryStats{
		TotalItems: len(m.items),
		ByCategory: make(map[Category]int),
	}

	for _, item := range m.items {
		stats.ByCategory[item.Category]++
		if item.ManualOverride {
			stats.ManualOverrides++
		}
	}

	return stats
}

// CategoryStats holds categorization statistics
type CategoryStats struct {
	TotalItems      int              `json:"total_items"`
	ByCategory      map[Category]int `json:"by_category"`
	ManualOverrides int              `json:"manual_overrides"`
}

// CleanStale removes entries for deleted files
func (m *Module) CleanStale() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	removed := 0
	ctx := context.Background()
	for path := range m.items {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			delete(m.items, path)
			if err := m.repo.Delete(ctx, path); err != nil {
				m.log.Warn("Failed to delete stale categorization entry from DB: %v", err)
			}
			removed++
		}
	}

	if removed > 0 {
		m.log.Info("Cleaned %d stale categorization entries", removed)
	}

	return removed
}

// Persistence — reads/writes via MySQL repository

func (m *Module) loadItems() error {
	records, err := m.repo.List(context.Background())
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rec := range records {
		item := &CategorizedItem{
			ID:             rec.ID,
			Name:           rec.Name,
			Path:           rec.Path,
			Category:       Category(rec.Category),
			Confidence:     rec.Confidence,
			CategorizedAt:  rec.CategorizedAt,
			ManualOverride: rec.ManualOverride,
		}
		if rec.DetectedTitle != "" || rec.DetectedYear != 0 || rec.DetectedArtist != "" {
			item.DetectedInfo = &MediaInfo{
				Title:    rec.DetectedTitle,
				Year:     rec.DetectedYear,
				Season:   rec.DetectedSeason,
				Episode:  rec.DetectedEpisode,
				ShowName: rec.DetectedShow,
				Artist:   rec.DetectedArtist,
				Album:    rec.DetectedAlbum,
			}
		}
		m.items[rec.Path] = item
	}
	return nil
}

func (m *Module) saveItems(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveItemsLocked(ctx)
}

// saveItemsLocked persists all in-memory items to the database.
// Caller must already hold mu (at least RLock).
func (m *Module) saveItemsLocked(ctx context.Context) error {
	for path, item := range m.items {
		if err := m.repo.Upsert(ctx, m.itemToRecord(path, item)); err != nil {
			return err
		}
	}
	return nil
}

// saveItem persists a single item to the database.
func (m *Module) saveItem(path string, item *CategorizedItem) {
	if err := m.repo.Upsert(context.Background(), m.itemToRecord(path, item)); err != nil {
		m.log.Error("Failed to persist categorized item %s: %v", item.ID, err)
	}
}

func (m *Module) itemToRecord(path string, item *CategorizedItem) *repositories.CategorizedItemRecord {
	rec := &repositories.CategorizedItemRecord{
		Path:           path,
		ID:             item.ID,
		Name:           item.Name,
		Category:       string(item.Category),
		Confidence:     item.Confidence,
		CategorizedAt:  item.CategorizedAt,
		ManualOverride: item.ManualOverride,
	}
	if item.DetectedInfo != nil {
		rec.DetectedTitle = item.DetectedInfo.Title
		rec.DetectedYear = item.DetectedInfo.Year
		rec.DetectedSeason = item.DetectedInfo.Season
		rec.DetectedEpisode = item.DetectedInfo.Episode
		rec.DetectedShow = item.DetectedInfo.ShowName
		rec.DetectedArtist = item.DetectedInfo.Artist
		rec.DetectedAlbum = item.DetectedInfo.Album
	}
	return rec
}
