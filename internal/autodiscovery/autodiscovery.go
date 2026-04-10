// Package autodiscovery provides smart media naming and organization suggestions.
// It uses domain types (FilePath, SuggestionKey, Confidence, fileExtension) instead
// of raw primitives to clarify intent and avoid primitive obsession at API boundaries.
package autodiscovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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

// SuggestionKey identifies a suggestion by its original file path.
type SuggestionKey string

// FilePath represents a filesystem path (file or directory) in auto-discovery.
// Using a dedicated type avoids primitive obsession and clarifies intent at API boundaries.
type FilePath string

// Confidence is a 0–1 score for how reliable a suggestion is.
type Confidence float64

// Float64 returns the numeric value for JSON/serialization.
func (c Confidence) Float64() float64 { return float64(c) }

// Patterns for detecting media types
var (
	// TV Show patterns: S01E02, 1x02, Season 1, Episode 2
	tvPatterns = []*regexp.Regexp{
		regexp.MustCompile(`[Ss](\d{1,2})[Ee](\d{1,2})`),                     // S01E02
		regexp.MustCompile(`(\d{1,2})[xX](\d{1,2})`),                         // 1x02
		regexp.MustCompile(`[Ss]eason\s*(\d{1,2})\s*[Ee]pisode\s*(\d{1,2})`), // Season 1, Episode 2
		regexp.MustCompile(`[Ee]pisode\s*(\d{1,3})`),                         // Episode 1
	}

	// Movie patterns: Name (Year), Name.Year
	moviePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(.+?)\s*\((\d{4})\)`), // Name (2020)
		regexp.MustCompile(`(.+?)\.(\d{4})\.`),    // Name.2020.
		regexp.MustCompile(`(.+?)\s*\[(\d{4})]`),  // Name [2020]
	}

	// Clean patterns (to remove)
	cleanPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[.*?]`), // [anything]
		// Note: We don't remove (year) patterns here as they're handled by moviePatterns
		regexp.MustCompile(`(?i)\.?(1080p|720p|480p|2160p|4K|HDR|HDTV|BluRay|WEB-?DL|DVDRip|BRRip)\.?`),
		regexp.MustCompile(`(?i)\.?(x264|x265|HEVC|AAC|AC3|DTS|FLAC)\.?`),
		regexp.MustCompile(`(?i)\.?(REPACK|PROPER|EXTENDED|UNRATED)\.?`),
	}

	// Track number pattern for music files (e.g. "01 - Song Title", "3.Song Title")
	trackPattern = regexp.MustCompile(`^(\d{1,2})[\s\-_.]+(.+)$`)
)

// Module handles auto-discovery and naming suggestions. Suggestions are persisted
// to a JSON file (loaded on Start, saved after each mutation) so they survive
// server restarts. A background scan task can be registered in cmd/server/main.go.
type Module struct {
	config      *config.Manager
	log         *logger.Logger
	dbModule    *database.Module
	repo        repositories.AutoDiscoverySuggestionRepository
	suggestions map[SuggestionKey]*models.AutoDiscoverySuggestion
	mu          sync.RWMutex
	healthy     bool
	healthMsg   string
	healthMu    sync.RWMutex
}

// NewModule creates a new auto-discovery module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:      cfg,
		log:         logger.New("autodiscovery"),
		dbModule:    dbModule,
		suggestions: make(map[SuggestionKey]*models.AutoDiscoverySuggestion),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "autodiscovery"
}

// Start initializes the module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting auto-discovery module...")

	m.repo = mysqlrepo.NewAutoDiscoverySuggestionRepository(m.dbModule.GORM())

	// Load existing suggestions
	if err := m.loadSuggestions(); err != nil {
		m.log.Warn("Failed to load suggestions: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Auto-discovery module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping auto-discovery module...")

	// Save suggestions with a bounded context so a slow DB cannot block shutdown indefinitely.
	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := m.saveSuggestions(stopCtx); err != nil {
		m.log.Error("Failed to save suggestions: %v", err)
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

// ScanDirectory scans a directory and generates naming suggestions
func (m *Module) ScanDirectory(dir FilePath) ([]*models.AutoDiscoverySuggestion, error) {
	var suggestions []*models.AutoDiscoverySuggestion
	dirStr := string(dir)

	err := filepath.Walk(dirStr, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			m.log.Warn("ScanDirectory: skipping %s: %v", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // skip symlinks — they may point outside the media directory
		}

		// Only process media files
		ext := strings.ToLower(filepath.Ext(path))
		if !helpers.IsMediaExtension(ext) {
			return nil
		}

		suggestion := m.generateSuggestion(FilePath(path))
		if suggestion != nil {
			suggestions = append(suggestions, suggestion)
			m.mu.Lock()
			m.suggestions[SuggestionKey(path)] = suggestion
			m.mu.Unlock()
		}

		return nil
	})

	if err == nil && len(suggestions) > 0 {
		if saveErr := m.saveSuggestions(context.Background()); saveErr != nil {
			m.log.Warn("Failed to persist suggestions after scan: %v", saveErr)
		}
	}

	return suggestions, err
}

// generateSuggestion generates a naming suggestion for a file
func (m *Module) generateSuggestion(path FilePath) *models.AutoDiscoverySuggestion {
	pathStr := string(path)
	filename := filepath.Base(pathStr)
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))
	ext := fileExtension(filepath.Ext(filename))

	if s := m.tryTVSuggestion(path, nameWithoutExt, ext); s != nil {
		return s
	}
	if s := m.tryMovieSuggestion(path, nameWithoutExt, ext); s != nil {
		return s
	}
	if s := m.tryMusicSuggestion(path, nameWithoutExt, filename, ext); s != nil {
		return s
	}
	return m.tryCleanedNameSuggestion(path, nameWithoutExt, ext)
}

func (m *Module) tryTVSuggestion(path FilePath, nameWithoutExt string, ext fileExtension) *models.AutoDiscoverySuggestion {
	tvInfo := m.detectTVShow(nameWithoutExt)
	if tvInfo == nil {
		return nil
	}
	suggestion := &models.AutoDiscoverySuggestion{
		OriginalPath:  string(path),
		Type:          models.SuggestionTypeTVEpisode,
		SuggestedName: m.formatTVShowName(tvInfo, ext),
		Confidence:    tvInfo.confidence.Float64(),
		SuggestedPath: m.suggestTVShowPath(path, tvInfo),
		Metadata: models.SuggestionMetadata{
			models.MetadataKeyShow:    tvInfo.showName,
			models.MetadataKeySeason:  tvInfo.season,
			models.MetadataKeyEpisode: tvInfo.episode,
		},
	}
	return suggestion
}

func (m *Module) tryMovieSuggestion(path FilePath, nameWithoutExt string, ext fileExtension) *models.AutoDiscoverySuggestion {
	movieInfo := m.detectMovie(nameWithoutExt)
	if movieInfo == nil {
		return nil
	}
	suggestion := &models.AutoDiscoverySuggestion{
		OriginalPath:  string(path),
		Type:          models.SuggestionTypeMovie,
		SuggestedName: m.formatMovieName(movieInfo, ext),
		Confidence:    movieInfo.confidence.Float64(),
		SuggestedPath: m.suggestMoviePath(path, movieInfo),
		Metadata: models.SuggestionMetadata{
			models.MetadataKeyTitle: movieInfo.title,
			models.MetadataKeyYear:  movieInfo.year,
		},
	}
	return suggestion
}

func (m *Module) tryMusicSuggestion(path FilePath, nameWithoutExt, filename string, ext fileExtension) *models.AutoDiscoverySuggestion {
	if !helpers.IsAudioExtension(filepath.Ext(filename)) {
		return nil
	}
	musicInfo := m.detectMusic(nameWithoutExt, path)
	if musicInfo == nil {
		return nil
	}
	metadata := models.SuggestionMetadata{}
	if musicInfo.artist != "" {
		metadata[models.MetadataKeyArtist] = musicInfo.artist
	}
	if musicInfo.album != "" {
		metadata[models.MetadataKeyAlbum] = musicInfo.album
	}
	if musicInfo.track != "" {
		metadata[models.MetadataKeyTrack] = musicInfo.track
	}
	return &models.AutoDiscoverySuggestion{
		OriginalPath:  string(path),
		Type:          models.SuggestionTypeMusic,
		SuggestedName: m.formatMusicName(musicInfo, ext),
		Confidence:    musicInfo.confidence.Float64(),
		Metadata:      metadata,
	}
}

func (m *Module) tryCleanedNameSuggestion(path FilePath, nameWithoutExt string, ext fileExtension) *models.AutoDiscoverySuggestion {
	cleanedName := m.cleanName(nameWithoutExt)
	if cleanedName == nameWithoutExt {
		return nil
	}
	return &models.AutoDiscoverySuggestion{
		OriginalPath:  string(path),
		Type:          models.SuggestionTypeUnknown,
		SuggestedName: cleanedName + string(ext),
		Confidence:    Confidence(0.3).Float64(),
		Metadata:      models.SuggestionMetadata{},
	}
}

type tvShowInfo struct {
	showName   string
	season     string
	episode    string
	confidence Confidence
}

// detectTVShow tries to detect TV show information
func (m *Module) detectTVShow(name string) *tvShowInfo {
	for _, pattern := range tvPatterns {
		matches := pattern.FindStringSubmatch(name)
		if matches != nil {
			info := &tvShowInfo{
				confidence: Confidence(0.8),
			}

			// Extract show name (everything before the pattern)
			idx := pattern.FindStringIndex(name)
			if idx != nil {
				showName := strings.TrimSpace(name[:idx[0]])
				showName = m.cleanName(showName)
				showName = strings.ReplaceAll(showName, ".", " ")
				showName = cases.Title(language.English).String(strings.ToLower(showName))
				info.showName = showName
			}

			// Extract season and episode
			if len(matches) >= 3 {
				info.season = matches[1]
				info.episode = matches[2]
			} else if len(matches) >= 2 {
				info.episode = matches[1]
				info.season = "1"
			}

			return info
		}
	}
	return nil
}

type movieInfo struct {
	title      string
	year       string
	confidence Confidence
}

// detectMovie tries to detect movie information
func (m *Module) detectMovie(name string) *movieInfo {
	for _, pattern := range moviePatterns {
		matches := pattern.FindStringSubmatch(name)
		if len(matches) >= 3 {
			title := m.cleanName(matches[1])
			title = strings.ReplaceAll(title, ".", " ")
			title = cases.Title(language.English).String(strings.ToLower(title))

			return &movieInfo{
				title:      strings.TrimSpace(title),
				year:       matches[2],
				confidence: Confidence(0.7),
			}
		}
	}
	return nil
}

type musicInfo struct {
	artist     string
	album      string
	track      string
	title      string
	confidence Confidence
}

// hasUsefulFields reports whether the music info has at least one of artist, album, or track set.
func (info *musicInfo) hasUsefulFields() bool {
	return info.artist != "" || info.album != "" || info.track != ""
}

// fillMusicInfoFromPath populates artist/album from the last 1–2 path segments (skipping "music"/"audio").
func fillMusicInfoFromPath(info *musicInfo, pathStr string) {
	dir := filepath.Dir(pathStr)
	parts := strings.Split(dir, string(os.PathSeparator))
	for i := len(parts) - 1; i >= 0 && i >= len(parts)-2; i-- {
		part := parts[i]
		if strings.EqualFold(part, "music") || strings.EqualFold(part, "audio") {
			continue
		}
		if info.album == "" {
			info.album = part
		} else if info.artist == "" {
			info.artist = part
		}
	}
}

// fillMusicInfoFromFilename sets track and title from filename when it matches "NN - Title" or "NN.Title".
func (m *Module) fillMusicInfoFromFilename(info *musicInfo, name string) {
	matches := trackPattern.FindStringSubmatch(name)
	if matches == nil {
		return
	}
	info.track = matches[1]
	info.title = m.cleanName(matches[2])
	info.confidence = Confidence(0.6)
}

// detectMusic tries to detect music information
func (m *Module) detectMusic(name string, path FilePath) *musicInfo {
	info := &musicInfo{
		title:      m.cleanName(name),
		confidence: Confidence(0.5),
	}
	fillMusicInfoFromPath(info, string(path))
	m.fillMusicInfoFromFilename(info, name)
	if !info.hasUsefulFields() {
		return nil
	}
	return info
}

// cleanName removes common patterns from names
func (m *Module) cleanName(name string) string {
	result := name

	for _, pattern := range cleanPatterns {
		result = pattern.ReplaceAllString(result, " ")
	}

	// Replace dots and underscores with spaces
	result = strings.ReplaceAll(result, ".", " ")
	result = strings.ReplaceAll(result, "_", " ")

	// Remove extra whitespace
	result = strings.Join(strings.Fields(result), " ")

	return strings.TrimSpace(result)
}

// fileExtension represents a file extension (e.g. ".mp4") to avoid passing raw strings.
type fileExtension string

// formatTVShowName formats a TV show filename
func (m *Module) formatTVShowName(info *tvShowInfo, ext fileExtension) string {
	return info.showName + " - S" + padNumber(info.season) + "E" + padNumber(info.episode) + string(ext)
}

// formatMovieName formats a movie filename
func (m *Module) formatMovieName(info *movieInfo, ext fileExtension) string {
	return info.title + " (" + info.year + ")" + string(ext)
}

// formatMusicName formats a music filename
func (m *Module) formatMusicName(info *musicInfo, ext fileExtension) string {
	if info.track != "" {
		return padNumber(info.track) + " - " + info.title + string(ext)
	}
	return info.title + string(ext)
}

func padNumber(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// suggestTVShowPath suggests a path for a TV show
func (m *Module) suggestTVShowPath(_ FilePath, info *tvShowInfo) string {
	cfg := m.config.Get()
	return filepath.Join(cfg.Directories.Videos, "TV Shows", info.showName, "Season "+info.season)
}

// suggestMoviePath suggests a path for a movie, organized into per-movie subdirectories.
func (m *Module) suggestMoviePath(_ FilePath, info *movieInfo) string {
	cfg := m.config.Get()
	return filepath.Join(cfg.Directories.Videos, "Movies", info.title+" ("+info.year+")")
}

// applySuggestionResolveDest resolves destination path and checks it is within allowed dirs.
func (m *Module) applySuggestionResolveDest(pathStr string, suggestion *models.AutoDiscoverySuggestion) (destPath, absDestPath string, err error) {
	destDir := suggestion.SuggestedPath
	if destDir == "" {
		destDir = filepath.Dir(pathStr)
	}
	destPath = filepath.Join(destDir, suggestion.SuggestedName)
	absDestPath, err = filepath.Abs(destPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid destination path: %w", err)
	}
	cfg := m.config.Get()
	allowedDirs := make([]string, 0, 3)
	allowedDirs = append(allowedDirs, cfg.Directories.Videos, cfg.Directories.Music, filepath.Dir(pathStr))
	if !isPathInAllowedDirs(absDestPath, allowedDirs) {
		return "", "", fmt.Errorf("destination path %s is outside allowed media directories", absDestPath)
	}
	return destPath, absDestPath, nil
}

// isPathInAllowedDirs reports whether absPath is under any of the allowed directories.
func isPathInAllowedDirs(absPath string, allowedDirs []string) bool {
	sep := string(filepath.Separator)
	for _, dir := range allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, absDir+sep) || absPath == absDir {
			return true
		}
	}
	return false
}

// applySuggestionPreconditions verifies source exists, dest does not, and ensures dest dir exists.
func applySuggestionPreconditions(pathStr, destPath, suggestedPath string) error {
	if _, err := os.Stat(pathStr); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination already exists, refusing to overwrite: %s", destPath)
	}
	if suggestedPath != "" {
		if err := os.MkdirAll(suggestedPath, 0o755); err != nil { //nolint:gosec // G301: media dirs need world-read for serving
			return err
		}
	}
	return nil
}

// ApplySuggestion applies a naming suggestion
func (m *Module) ApplySuggestion(originalPath FilePath) error {
	pathStr := string(originalPath)
	m.mu.RLock()
	suggestion, ok := m.suggestions[SuggestionKey(pathStr)]
	m.mu.RUnlock()

	if !ok {
		return nil // No suggestion for this file
	}

	destPath, _, err := m.applySuggestionResolveDest(pathStr, suggestion)
	if err != nil {
		return err
	}
	if err := applySuggestionPreconditions(pathStr, destPath, suggestion.SuggestedPath); err != nil {
		return err
	}

	if err := os.Rename(pathStr, destPath); err != nil {
		return err
	}

	m.log.Info("Applied suggestion: %s -> %s", pathStr, destPath)

	m.mu.Lock()
	delete(m.suggestions, SuggestionKey(pathStr))
	m.mu.Unlock()

	if saveErr := m.saveSuggestions(context.Background()); saveErr != nil {
		m.log.Warn("Failed to persist suggestions after apply: %v", saveErr)
	}

	return nil
}

// ApplyAllSuggestions applies all pending suggestions with at least the given confidence.
func (m *Module) ApplyAllSuggestions(minConfidence Confidence) (int, []error) {
	toApply := m.applyAllSuggestionsFilter(minConfidence)
	return m.applyAllSuggestionsRun(toApply)
}

func (m *Module) applyAllSuggestionsFilter(minConfidence Confidence) []*models.AutoDiscoverySuggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()
	minVal := minConfidence.Float64()
	toApply := make([]*models.AutoDiscoverySuggestion, 0)
	for _, s := range m.suggestions {
		if s.Confidence < minVal {
			continue
		}
		toApply = append(toApply, s)
	}
	return toApply
}

func (m *Module) applyAllSuggestionsRun(toApply []*models.AutoDiscoverySuggestion) (int, []error) {
	applied := 0
	var errors []error
	for _, s := range toApply {
		if err := m.ApplySuggestion(FilePath(s.OriginalPath)); err != nil {
			errors = append(errors, err)
			continue
		}
		applied++
	}
	return applied, errors
}

// GetSuggestions returns all pending suggestions
func (m *Module) GetSuggestions() []*models.AutoDiscoverySuggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	suggestions := make([]*models.AutoDiscoverySuggestion, 0, len(m.suggestions))
	for _, s := range m.suggestions {
		suggestions = append(suggestions, s)
	}
	return suggestions
}

// ClearSuggestion removes a suggestion from memory and persists the deletion.
func (m *Module) ClearSuggestion(path FilePath) {
	m.mu.Lock()
	delete(m.suggestions, SuggestionKey(path))
	m.mu.Unlock()

	ctx := context.Background()
	if err := m.repo.Delete(ctx, string(path)); err != nil {
		m.log.Warn("Failed to persist suggestion deletion for %q: %v", path, err)
	}
}

// ClearAllSuggestions removes all suggestions from memory and persists the deletion.
func (m *Module) ClearAllSuggestions() {
	m.mu.Lock()
	m.suggestions = make(map[SuggestionKey]*models.AutoDiscoverySuggestion)
	m.mu.Unlock()

	ctx := context.Background()
	if err := m.repo.DeleteAll(ctx); err != nil {
		m.log.Warn("Failed to delete suggestions from database: %v", err)
		return
	}
}

// Persistence — reads/writes via MySQL repository

func (m *Module) loadSuggestions() error {
	records, err := m.repo.List(context.Background())
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rec := range records {
		metadata := models.SuggestionMetadata{}
		for k, v := range rec.Metadata {
			metadata[models.SuggestionMetadataKey(k)] = v
		}

		m.suggestions[SuggestionKey(rec.OriginalPath)] = &models.AutoDiscoverySuggestion{
			OriginalPath:  rec.OriginalPath,
			SuggestedName: rec.SuggestedName,
			SuggestedPath: rec.SuggestedPath,
			Type:          models.SuggestionType(rec.Type),
			Confidence:    rec.Confidence,
			Metadata:      metadata,
		}
	}
	return nil
}

func (m *Module) saveSuggestions(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.suggestions {
		metadata := make(map[string]string, len(s.Metadata))
		for k, v := range s.Metadata {
			metadata[string(k)] = v
		}

		rec := &repositories.AutoDiscoveryRecord{
			OriginalPath:  s.OriginalPath,
			SuggestedName: s.SuggestedName,
			SuggestedPath: s.SuggestedPath,
			Type:          string(s.Type),
			Confidence:    s.Confidence,
			Metadata:      metadata,
		}
		if err := m.repo.Save(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}
