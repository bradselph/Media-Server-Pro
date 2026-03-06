// Package autodiscovery provides smart media naming and organization suggestions.
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
	suggestions map[string]*models.AutoDiscoverySuggestion
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
		suggestions: make(map[string]*models.AutoDiscoverySuggestion),
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
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping auto-discovery module...")

	// Save suggestions
	if err := m.saveSuggestions(); err != nil {
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
func (m *Module) ScanDirectory(dir string) ([]*models.AutoDiscoverySuggestion, error) {
	var suggestions []*models.AutoDiscoverySuggestion

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on error
		}
		if info.IsDir() {
			return nil
		}

		// Only process media files
		ext := strings.ToLower(filepath.Ext(path))
		if !helpers.IsMediaExtension(ext) {
			return nil
		}

		suggestion := m.generateSuggestion(path)
		if suggestion != nil {
			suggestions = append(suggestions, suggestion)
			m.mu.Lock()
			m.suggestions[path] = suggestion
			m.mu.Unlock()
		}

		return nil
	})

	if err == nil && len(suggestions) > 0 {
		if saveErr := m.saveSuggestions(); saveErr != nil {
			m.log.Warn("Failed to persist suggestions after scan: %v", saveErr)
		}
	}

	return suggestions, err
}

// generateSuggestion generates a naming suggestion for a file
func (m *Module) generateSuggestion(path string) *models.AutoDiscoverySuggestion {
	filename := filepath.Base(path)
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))

	suggestion := &models.AutoDiscoverySuggestion{
		OriginalPath: path,
		Metadata:     make(map[string]string),
	}

	// Try to detect TV show
	if tvInfo := m.detectTVShow(nameWithoutExt); tvInfo != nil {
		suggestion.Type = "tv_episode"
		suggestion.SuggestedName = m.formatTVShowName(tvInfo, filepath.Ext(filename))
		suggestion.Confidence = tvInfo.confidence
		suggestion.Metadata["show"] = tvInfo.showName
		suggestion.Metadata["season"] = tvInfo.season
		suggestion.Metadata["episode"] = tvInfo.episode
		suggestion.SuggestedPath = m.suggestTVShowPath(path, tvInfo)
		return suggestion
	}

	// Try to detect movie
	if movieInfo := m.detectMovie(nameWithoutExt); movieInfo != nil {
		suggestion.Type = "movie"
		suggestion.SuggestedName = m.formatMovieName(movieInfo, filepath.Ext(filename))
		suggestion.Confidence = movieInfo.confidence
		suggestion.Metadata["title"] = movieInfo.title
		suggestion.Metadata["year"] = movieInfo.year
		suggestion.SuggestedPath = m.suggestMoviePath(path, movieInfo)
		return suggestion
	}

	// Try to detect music — only for audio file extensions to avoid misclassifying video files
	if helpers.IsAudioExtension(filepath.Ext(filename)) {
		if musicInfo := m.detectMusic(nameWithoutExt, path); musicInfo != nil {
			suggestion.Type = "music"
			suggestion.SuggestedName = m.formatMusicName(musicInfo, filepath.Ext(filename))
			suggestion.Confidence = musicInfo.confidence
			if musicInfo.artist != "" {
				suggestion.Metadata["artist"] = musicInfo.artist
			}
			if musicInfo.album != "" {
				suggestion.Metadata["album"] = musicInfo.album
			}
			if musicInfo.track != "" {
				suggestion.Metadata["track"] = musicInfo.track
			}
			return suggestion
		}
	}

	// No specific type detected, just clean up the name
	cleanedName := m.cleanName(nameWithoutExt)
	if cleanedName != nameWithoutExt {
		suggestion.Type = "unknown"
		suggestion.SuggestedName = cleanedName + filepath.Ext(filename)
		suggestion.Confidence = 0.3
		return suggestion
	}

	return nil // No suggestion needed
}

type tvShowInfo struct {
	showName   string
	season     string
	episode    string
	confidence float64
}

// detectTVShow tries to detect TV show information
func (m *Module) detectTVShow(name string) *tvShowInfo {
	for _, pattern := range tvPatterns {
		matches := pattern.FindStringSubmatch(name)
		if matches != nil {
			info := &tvShowInfo{
				confidence: 0.8,
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
	confidence float64
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
				confidence: 0.7,
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
	confidence float64
}

// detectMusic tries to detect music information
func (m *Module) detectMusic(name, path string) *musicInfo {
	// Try to get info from directory structure
	dir := filepath.Dir(path)
	parts := strings.Split(dir, string(os.PathSeparator))

	info := &musicInfo{
		title:      m.cleanName(name),
		confidence: 0.5,
	}

	// Look for artist/album pattern in path
	for i := len(parts) - 1; i >= 0 && i >= len(parts)-2; i-- {
		part := parts[i]
		if strings.ToLower(part) == "music" || strings.ToLower(part) == "audio" {
			continue
		}
		if info.album == "" {
			info.album = part
		} else if info.artist == "" {
			info.artist = part
		}
	}

	// Try to extract track number from filename
	if matches := trackPattern.FindStringSubmatch(name); matches != nil {
		info.track = matches[1]
		info.title = m.cleanName(matches[2])
		info.confidence = 0.6
	}

	// Only return if we found something useful
	if info.artist != "" || info.album != "" || info.track != "" {
		return info
	}

	return nil
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

// formatTVShowName formats a TV show filename
func (m *Module) formatTVShowName(info *tvShowInfo, ext string) string {
	return info.showName + " - S" + padNumber(info.season) + "E" + padNumber(info.episode) + ext
}

// formatMovieName formats a movie filename
func (m *Module) formatMovieName(info *movieInfo, ext string) string {
	return info.title + " (" + info.year + ")" + ext
}

// formatMusicName formats a music filename
func (m *Module) formatMusicName(info *musicInfo, ext string) string {
	if info.track != "" {
		return padNumber(info.track) + " - " + info.title + ext
	}
	return info.title + ext
}

func padNumber(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// suggestTVShowPath suggests a path for a TV show
func (m *Module) suggestTVShowPath(_ string, info *tvShowInfo) string {
	cfg := m.config.Get()
	return filepath.Join(cfg.Directories.Videos, "TV Shows", info.showName, "Season "+info.season)
}

// suggestMoviePath suggests a path for a movie, organized into per-movie subdirectories.
func (m *Module) suggestMoviePath(_ string, info *movieInfo) string {
	cfg := m.config.Get()
	return filepath.Join(cfg.Directories.Videos, "Movies", info.title+" ("+info.year+")")
}

// ApplySuggestion applies a naming suggestion
func (m *Module) ApplySuggestion(originalPath string) error {
	m.mu.RLock()
	suggestion, ok := m.suggestions[originalPath]
	m.mu.RUnlock()

	if !ok {
		return nil // No suggestion for this file
	}

	// Determine final path
	destDir := suggestion.SuggestedPath
	if destDir == "" {
		destDir = filepath.Dir(originalPath)
	}
	destPath := filepath.Join(destDir, suggestion.SuggestedName)

	// Resolve to absolute paths to prevent path traversal via ../ in suggestion data
	absDestPath, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// Validate destination is within an allowed media directory
	cfg := m.config.Get()
	allowedDirs := []string{cfg.Directories.Videos, cfg.Directories.Music}
	// Also allow the source file's parent directory (in-place rename)
	allowedDirs = append(allowedDirs, filepath.Dir(originalPath))

	inAllowed := false
	for _, dir := range allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		// Ensure trailing separator for proper prefix matching
		if strings.HasPrefix(absDestPath, absDir+string(filepath.Separator)) || absDestPath == absDir {
			inAllowed = true
			break
		}
	}
	if !inAllowed {
		return fmt.Errorf("destination path %s is outside allowed media directories", absDestPath)
	}

	// Verify source file exists before attempting rename
	if _, err := os.Stat(originalPath); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}

	// Check if destination already exists to prevent silent overwrite and data loss
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination already exists, refusing to overwrite: %s", destPath)
	}

	// Ensure destination directory exists (after precondition checks to avoid
	// creating empty directories when source is missing or dest already exists)
	if suggestion.SuggestedPath != "" {
		if err := os.MkdirAll(suggestion.SuggestedPath, 0755); err != nil {
			return err
		}
	}

	// Rename/move file
	if err := os.Rename(originalPath, destPath); err != nil {
		return err
	}

	m.log.Info("Applied suggestion: %s -> %s", originalPath, destPath)

	// Remove suggestion and persist
	m.mu.Lock()
	delete(m.suggestions, originalPath)
	m.mu.Unlock()

	if saveErr := m.saveSuggestions(); saveErr != nil {
		m.log.Warn("Failed to persist suggestions after apply: %v", saveErr)
	}

	return nil
}

// ApplyAllSuggestions applies all pending suggestions
func (m *Module) ApplyAllSuggestions(minConfidence float64) (int, []error) {
	m.mu.RLock()
	toApply := make([]*models.AutoDiscoverySuggestion, 0)
	for _, s := range m.suggestions {
		if s.Confidence >= minConfidence {
			toApply = append(toApply, s)
		}
	}
	m.mu.RUnlock()

	applied := 0
	var errors []error

	for _, s := range toApply {
		if err := m.ApplySuggestion(s.OriginalPath); err != nil {
			errors = append(errors, err)
		} else {
			applied++
		}
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

// ClearSuggestion removes a suggestion
func (m *Module) ClearSuggestion(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.suggestions, path)
}

// ClearAllSuggestions removes all suggestions
func (m *Module) ClearAllSuggestions() {
	m.mu.Lock()
	m.suggestions = make(map[string]*models.AutoDiscoverySuggestion)
	m.mu.Unlock()

	if saveErr := m.saveSuggestions(); saveErr != nil {
		m.log.Warn("Failed to persist suggestions after clear: %v", saveErr)
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
		m.suggestions[rec.OriginalPath] = &models.AutoDiscoverySuggestion{
			OriginalPath:  rec.OriginalPath,
			SuggestedName: rec.SuggestedName,
			SuggestedPath: rec.SuggestedPath,
			Type:          rec.Type,
			Confidence:    rec.Confidence,
			Metadata:      rec.Metadata,
		}
	}
	return nil
}

func (m *Module) saveSuggestions() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ctx := context.Background()
	for _, s := range m.suggestions {
		rec := &repositories.AutoDiscoveryRecord{
			OriginalPath:  s.OriginalPath,
			SuggestedName: s.SuggestedName,
			SuggestedPath: s.SuggestedPath,
			Type:          s.Type,
			Confidence:    s.Confidence,
			Metadata:      s.Metadata,
		}
		if err := m.repo.Save(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}
