// Package suggestions provides content recommendation based on viewing history.
package suggestions

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// pathToID computes the MD5 hash of a path, matching models.MediaItem.ID generation.
func pathToID(path string) string {
	h := md5.Sum([]byte(path))
	return hex.EncodeToString(h[:])
}

// ViewHistory tracks a user's viewing history
type ViewHistory struct {
	MediaPath   string     `json:"-"`
	Category    string     `json:"category"`
	MediaType   string     `json:"media_type"`
	ViewCount   int        `json:"view_count"`
	TotalTime   float64    `json:"total_time"`
	LastViewed  time.Time  `json:"last_viewed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Rating      float64    `json:"rating,omitempty"`
}

// UserProfile holds user preferences derived from viewing history
type UserProfile struct {
	UserID          string             `json:"user_id"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	TypePreferences map[string]float64 `json:"type_preferences"`
	ViewHistory     []ViewHistory      `json:"view_history"`
	TotalViews      int                `json:"total_views"`
	TotalWatchTime  float64            `json:"total_watch_time"`
	LastUpdated     time.Time          `json:"last_updated"`
}

// Suggestion represents a content recommendation.
// MediaPath is excluded from JSON to prevent leaking filesystem paths. Use MediaID instead.
type Suggestion struct {
	MediaID      string   `json:"media_id"`
	MediaPath    string   `json:"-"`
	Title        string   `json:"title"`
	Category     string   `json:"category"`
	MediaType    string   `json:"media_type"`
	Score        float64  `json:"score"`
	Reasons      []string `json:"reasons"`
	ThumbnailURL string   `json:"thumbnail_url,omitempty"`
}

// Module handles content suggestions. RecordView is called from the streaming
// handler (StreamMedia) on each authenticated playback event, integrating
// analytics view events with the suggestion engine for personalized recommendations.
// Suggestion data is stored in MySQL via the SuggestionProfileRepository.
type Module struct {
	config    *config.Manager
	log       *logger.Logger
	repo      repositories.SuggestionProfileRepository
	profiles  map[string]*UserProfile
	mediaData map[string]*MediaInfo
	mu        sync.RWMutex
	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex
}

// MediaInfo holds information about a media file for suggestions
type MediaInfo struct {
	Path      string
	Title     string
	Category  string
	MediaType string
	Tags      []string
	Views     int
	Rating    float64
	AddedAt   time.Time
}

// NewModule creates a new suggestions module
func NewModule(cfg *config.Manager, repo repositories.SuggestionProfileRepository) *Module {
	return &Module{
		config:    cfg,
		log:       logger.New("suggestions"),
		repo:      repo,
		profiles:  make(map[string]*UserProfile),
		mediaData: make(map[string]*MediaInfo),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "suggestions"
}

// Start initializes the module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting suggestions module...")

	// Load existing profiles
	if err := m.loadProfiles(); err != nil {
		m.log.Warn("Failed to load user profiles: %v", err)
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Suggestions module started")
	return nil
}

// Stop gracefully stops the module
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping suggestions module...")

	// Save profiles
	if err := m.saveProfiles(); err != nil {
		m.log.Error("Failed to save user profiles: %v", err)
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

// RecordView records a view for a user
func (m *Module) RecordView(userID, mediaId, category, mediaType string, duration float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile, ok := m.profiles[userID]
	if !ok {
		profile = &UserProfile{
			UserID:          userID,
			CategoryScores:  make(map[string]float64),
			TypePreferences: make(map[string]float64),
			ViewHistory:     make([]ViewHistory, 0),
		}
		m.profiles[userID] = profile
	}

	// Update or add view history entry
	found := false
	for i, vh := range profile.ViewHistory {
		if vh.MediaPath == mediaId {
			profile.ViewHistory[i].ViewCount++
			profile.ViewHistory[i].TotalTime += duration
			profile.ViewHistory[i].LastViewed = time.Now()
			found = true
			break
		}
	}

	if !found {
		profile.ViewHistory = append(profile.ViewHistory, ViewHistory{
			MediaPath:  mediaId,
			Category:   category,
			MediaType:  mediaType,
			ViewCount:  1,
			TotalTime:  duration,
			LastViewed: time.Now(),
		})
		// Cap view history to prevent unbounded growth
		const maxViewHistory = 500
		if len(profile.ViewHistory) > maxViewHistory {
			profile.ViewHistory = profile.ViewHistory[len(profile.ViewHistory)-maxViewHistory:]
		}
	}

	// Update category scores
	profile.CategoryScores[category] += 1.0
	profile.TypePreferences[mediaType] += 1.0
	profile.TotalViews++
	profile.TotalWatchTime += duration
	profile.LastUpdated = time.Now()

	m.log.Debug("Recorded view for user %s: %s (category: %s)", userID, mediaId, category)
}

// RecordCompletion marks a media item as completed
func (m *Module) RecordCompletion(userID, mediaId string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile, ok := m.profiles[userID]
	if !ok {
		return
	}

	for i, vh := range profile.ViewHistory {
		if vh.MediaPath == mediaId {
			completedAt := time.Now()
			profile.ViewHistory[i].CompletedAt = &completedAt
			break
		}
	}
}

// RecordRating records a user rating for a media item
func (m *Module) RecordRating(userID, mediaId string, rating float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile, ok := m.profiles[userID]
	if !ok {
		return
	}

	for i, vh := range profile.ViewHistory {
		if vh.MediaPath == mediaId {
			profile.ViewHistory[i].Rating = rating
			break
		}
	}

	m.log.Debug("Recorded rating %.1f for %s by user %s", rating, mediaId, userID)
}

// UpdateMediaData atomically replaces the in-memory media catalogue used for suggestions.
// Builds the new map outside the lock then swaps in one operation to eliminate the
// window where mediaData is empty (IC-05).
func (m *Module) UpdateMediaData(items []*MediaInfo) {
	newData := make(map[string]*MediaInfo, len(items))
	for _, item := range items {
		newData[item.Path] = item
	}

	m.mu.Lock()
	m.mediaData = newData
	m.mu.Unlock()

	m.log.Info("Updated media data: %d items", len(items))
}

// GetSuggestions returns personalized suggestions for a user
func (m *Module) GetSuggestions(userID string, limit int) []*Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	// Get user profile (may be nil for new users without history)
	profile := m.profiles[userID]

	// Split viewed history: items viewed in last 14 days are excluded,
	// older items are eligible for re-watch suggestions with a score penalty.
	recentlyViewed := make(map[string]bool)
	oldViewedAt := make(map[string]time.Time)
	if profile != nil {
		for _, vh := range profile.ViewHistory {
			if time.Since(vh.LastViewed) < 14*24*time.Hour {
				recentlyViewed[vh.MediaPath] = true
			} else {
				oldViewedAt[vh.MediaPath] = vh.LastViewed
			}
		}
	}

	var suggestions []*Suggestion
	for _, media := range m.mediaData {
		if recentlyViewed[media.Path] {
			continue // Skip recently viewed
		}

		score, reasons := m.scoreMedia(profile, media)

		// Apply re-watch penalty for previously-seen items (penalty fades over 90 days)
		if lastViewed, ok := oldViewedAt[media.Path]; ok {
			daysSince := time.Since(lastViewed).Hours() / 24
			penalty := 0.5 * (1 - math.Min(1, daysSince/90))
			score *= 1 - penalty
			reasons = append(reasons, "Watch again")
		}

		// Add ±15% score jitter so results rotate between calls
		score *= 1.0 + (rand.Float64()*0.30 - 0.15)

		if score > 0 {
			suggestions = append(suggestions, &Suggestion{
				MediaID:   pathToID(media.Path),
				MediaPath: media.Path,
				Title:     media.Title,
				Category:  media.Category,
				MediaType: media.MediaType,
				Score:     score,
				Reasons:   reasons,
			})
		}
	}

	// Sort by jittered score
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	// Apply category diversity: at most ceil(limit/2) items per category,
	// then pad with remaining top-scored items to reach limit.
	const maxPerCategory = 3
	suggestions = diversify(suggestions, limit, maxPerCategory)

	return suggestions
}

// diversify picks up to limit items with at most maxPerCategory per category,
// then pads with the best remaining if needed.
func diversify(sorted []*Suggestion, limit, maxPerCategory int) []*Suggestion {
	categoryCounts := make(map[string]int)
	picked := make([]*Suggestion, 0, limit)
	deferred := make([]*Suggestion, 0)

	for _, s := range sorted {
		if len(picked) >= limit {
			break
		}
		if categoryCounts[s.Category] < maxPerCategory {
			picked = append(picked, s)
			categoryCounts[s.Category]++
		} else {
			deferred = append(deferred, s)
		}
	}
	// Pad with any deferred items if we still need more
	for _, s := range deferred {
		if len(picked) >= limit {
			break
		}
		picked = append(picked, s)
	}
	return picked
}

// scoreMedia calculates a suggestion score for a media item
func (m *Module) scoreMedia(profile *UserProfile, media *MediaInfo) (float64, []string) {
	score, reasons := scoreMediaBase(media)

	if profile != nil {
		profileScore, profileReasons := scoreMediaForProfile(profile, media)
		score += profileScore
		reasons = append(reasons, profileReasons...)
	}

	return score, reasons
}

// scoreMediaBase calculates the base score from popularity, recency, and rating.
func scoreMediaBase(media *MediaInfo) (float64, []string) {
	var score float64
	var reasons []string

	popularityScore := math.Log10(float64(media.Views+1)) * 0.1
	score += popularityScore
	if popularityScore > 0.2 {
		reasons = append(reasons, "Popular content")
	}

	daysSinceAdded := time.Since(media.AddedAt).Hours() / 24
	if daysSinceAdded < 7 {
		newScore := 0.3 * (1 - daysSinceAdded/7)
		score += newScore
		reasons = append(reasons, "New addition")
	}

	if media.Rating > 4 {
		ratingBoost := (media.Rating - 3) * 0.1
		score += ratingBoost
		reasons = append(reasons, "Highly rated")
	}

	return score, reasons
}

// scoreMediaForProfile calculates the personalized score based on a user profile.
func scoreMediaForProfile(profile *UserProfile, media *MediaInfo) (float64, []string) {
	var score float64
	var reasons []string

	score += scoreCategoryPreference(profile, media, &reasons)
	score += scoreTypePreference(profile, media)
	score += scoreRecentlyViewed(profile, media, &reasons)

	return score, reasons
}

// scoreCategoryPreference calculates the category preference boost.
func scoreCategoryPreference(profile *UserProfile, media *MediaInfo, reasons *[]string) float64 {
	categoryScore, ok := profile.CategoryScores[media.Category]
	if !ok {
		return 0
	}
	totalCategoryScore := 0.0
	for _, s := range profile.CategoryScores {
		totalCategoryScore += s
	}
	if totalCategoryScore <= 0 {
		return 0
	}
	normalizedCategoryScore := (categoryScore / totalCategoryScore) * 0.5
	if normalizedCategoryScore > 0.1 {
		*reasons = append(*reasons, "Matches your interests in "+media.Category)
	}
	return normalizedCategoryScore
}

// scoreTypePreference calculates the media type preference boost.
func scoreTypePreference(profile *UserProfile, media *MediaInfo) float64 {
	typeScore, ok := profile.TypePreferences[media.MediaType]
	if !ok {
		return 0
	}
	totalTypeScore := 0.0
	for _, s := range profile.TypePreferences {
		totalTypeScore += s
	}
	if totalTypeScore <= 0 {
		return 0
	}
	return (typeScore / totalTypeScore) * 0.3
}

// scoreRecentlyViewed adds a boost if the user recently viewed content in the same category.
func scoreRecentlyViewed(profile *UserProfile, media *MediaInfo, reasons *[]string) float64 {
	for _, vh := range profile.ViewHistory {
		if vh.Category == media.Category && time.Since(vh.LastViewed) < 7*24*time.Hour {
			*reasons = append(*reasons, "Similar to recently viewed")
			return 0.1
		}
	}
	return 0
}

// GetTrendingSuggestions returns trending content
func (m *Module) GetTrendingSuggestions(limit int) []*Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	var suggestions []*Suggestion

	for _, media := range m.mediaData {
		score := float64(media.Views) * math.Max(media.Rating, 1)

		// Boost recent content
		daysSinceAdded := time.Since(media.AddedAt).Hours() / 24
		if daysSinceAdded < 30 {
			score *= 1.5
		}

		// Add ±20% jitter for variety
		score *= 1.0 + (rand.Float64()*0.40 - 0.20)

		suggestions = append(suggestions, &Suggestion{
			MediaID:   pathToID(media.Path),
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  media.Category,
			MediaType: media.MediaType,
			Score:     score,
			Reasons:   []string{"Trending"},
		})
	}

	// Sort by jittered score
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	return diversify(suggestions, limit, 3)
}

// GetSimilarMedia returns media similar to a given item
func (m *Module) GetSimilarMedia(mediaPath string, limit int) []*Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	sourceMedia, ok := m.mediaData[mediaPath]
	if !ok {
		return nil
	}

	var suggestions []*Suggestion

	for _, media := range m.mediaData {
		if media.Path == mediaPath {
			continue
		}

		score, reasons := computeSimilarity(sourceMedia, media)

		// Add ±20% score jitter for variety in related-media results
		score *= 1.0 + (rand.Float64()*0.40 - 0.20)

		if score > 0 {
			suggestions = append(suggestions, &Suggestion{
				MediaID:   pathToID(media.Path),
				MediaPath: media.Path,
				Title:     media.Title,
				Category:  media.Category,
				MediaType: media.MediaType,
				Score:     score,
				Reasons:   reasons,
			})
		}
	}

	// Sort by jittered score
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	// Allow up to 60% of results from the same category as source,
	// blend in others for variety
	sameCategory := limit*6/10 + 1
	return diversify(suggestions, limit, sameCategory)
}

// computeSimilarity calculates how similar two media items are by category, type, tags, and title.
func computeSimilarity(source, candidate *MediaInfo) (float64, []string) {
	var score float64
	var reasons []string

	if candidate.Category == source.Category {
		score += 0.5
		reasons = append(reasons, "Same category")
	}
	if candidate.MediaType == source.MediaType {
		score += 0.3
	}

	score += computeTagSimilarity(source, candidate, &reasons)
	score += computeTitleSimilarity(source, candidate)

	return score, reasons
}

// computeTagSimilarity calculates the tag overlap score between two media items.
func computeTagSimilarity(source, candidate *MediaInfo, reasons *[]string) float64 {
	var score float64
	for _, tag := range candidate.Tags {
		for _, sourceTag := range source.Tags {
			if strings.EqualFold(tag, sourceTag) {
				score += 0.2
				*reasons = append(*reasons, "Similar tags")
				break
			}
		}
	}
	return score
}

// computeTitleSimilarity calculates a simple word-overlap score between two media titles.
func computeTitleSimilarity(source, candidate *MediaInfo) float64 {
	sourceWords := strings.Fields(strings.ToLower(source.Title))
	mediaWords := strings.Fields(strings.ToLower(candidate.Title))
	var score float64
	for _, sw := range sourceWords {
		if len(sw) < 3 {
			continue
		}
		for _, mw := range mediaWords {
			if sw == mw {
				score += 0.1
			}
		}
	}
	return score
}

// GetContinueWatching returns items the user started but didn't finish
func (m *Module) GetContinueWatching(userID string, limit int) []*Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	profile, ok := m.profiles[userID]
	if !ok {
		return nil
	}

	var suggestions []*Suggestion

	for _, vh := range profile.ViewHistory {
		// Skip completed items
		if vh.CompletedAt != nil {
			continue
		}

		// Include items viewed in the last 30 days
		if time.Since(vh.LastViewed) > 30*24*time.Hour {
			continue
		}

		media := m.mediaData[vh.MediaPath]
		title := vh.MediaPath
		if media != nil {
			title = media.Title
		}

		suggestions = append(suggestions, &Suggestion{
			MediaID:   pathToID(vh.MediaPath),
			MediaPath: vh.MediaPath,
			Title:     title,
			Category:  vh.Category,
			MediaType: vh.MediaType,
			Score:     float64(30*24 - int(time.Since(vh.LastViewed).Hours())),
			Reasons:   []string{"Continue watching"},
		})
	}

	// Sort by most recent
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}

	return suggestions
}

// DEPRECATED: DC-03 — not exposed via any route or handler — safe to delete
func (m *Module) GetUserProfile(userID string) *UserProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[userID]
}

// GetStats returns suggestion module statistics
func (m *Module) GetStats() SuggestionStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := SuggestionStats{
		TotalProfiles: len(m.profiles),
		TotalMedia:    len(m.mediaData),
	}

	for _, profile := range m.profiles {
		stats.TotalViews += profile.TotalViews
		stats.TotalWatchTime += profile.TotalWatchTime
	}

	return stats
}

// SuggestionStats holds suggestion statistics
type SuggestionStats struct {
	TotalProfiles  int     `json:"total_profiles"`
	TotalMedia     int     `json:"total_media"`
	TotalViews     int     `json:"total_views"`
	TotalWatchTime float64 `json:"total_watch_time"`
}

// Persistence — reads/writes via MySQL repository

func (m *Module) loadProfiles() error {
	profiles, err := m.repo.ListProfiles(context.Background())
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rec := range profiles {
		profile := &UserProfile{
			UserID:          rec.UserID,
			CategoryScores:  rec.CategoryScores,
			TypePreferences: rec.TypePreferences,
			TotalViews:      rec.TotalViews,
			TotalWatchTime:  rec.TotalWatchTime,
			LastUpdated:     rec.LastUpdated,
		}
		// Load view history for this user
		history, err := m.repo.GetViewHistory(context.Background(), rec.UserID)
		if err == nil {
			for _, h := range history {
				vh := ViewHistory{
					MediaPath: h.MediaPath,
					Category:  h.Category,
					MediaType: h.MediaType,
					ViewCount: h.ViewCount,
					TotalTime: h.TotalTime,
					LastViewed: h.LastViewed,
					CompletedAt: h.CompletedAt,
					Rating:    h.Rating,
				}
				profile.ViewHistory = append(profile.ViewHistory, vh)
			}
		}
		m.profiles[rec.UserID] = profile
	}
	return nil
}

func (m *Module) saveProfiles() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ctx := context.Background()
	for _, profile := range m.profiles {
		rec := &repositories.SuggestionProfileRecord{
			UserID:          profile.UserID,
			CategoryScores:  profile.CategoryScores,
			TypePreferences: profile.TypePreferences,
			TotalViews:      profile.TotalViews,
			TotalWatchTime:  profile.TotalWatchTime,
			LastUpdated:     profile.LastUpdated,
		}
		if err := m.repo.SaveProfile(ctx, rec); err != nil {
			return err
		}
		// Save view history
		for i := range profile.ViewHistory {
			vh := &profile.ViewHistory[i]
			entry := &repositories.ViewHistoryRecord{
				MediaPath:   vh.MediaPath,
				Category:    vh.Category,
				MediaType:   vh.MediaType,
				ViewCount:   vh.ViewCount,
				TotalTime:   vh.TotalTime,
				LastViewed:  vh.LastViewed,
				CompletedAt: vh.CompletedAt,
				Rating:      vh.Rating,
			}
			if err := m.repo.SaveViewHistory(ctx, profile.UserID, entry); err != nil {
				return err
			}
		}
	}
	return nil
}
