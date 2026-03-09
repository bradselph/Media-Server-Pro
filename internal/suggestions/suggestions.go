// Package suggestions provides content recommendation based on viewing history.
package suggestions

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	mysqlrepo "media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

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
	dbModule  *database.Module
	repo      repositories.SuggestionProfileRepository
	profiles  map[string]*UserProfile
	mediaData map[string]*MediaInfo // keyed by filesystem path
	mediaByID map[string]*MediaInfo // keyed by StableID (secondary index)
	mu        sync.RWMutex
	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex
	// ctx/cancel drive the background profile-eviction goroutine.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// MediaInfo holds information about a media file for suggestions
type MediaInfo struct {
	Path      string
	StableID  string // UUID assigned by the media module; used as the public-facing MediaID
	Title     string
	Category  string
	MediaType string
	Tags      []string
	Views     int
	Rating    float64
	AddedAt   time.Time
	IsMature  bool // flagged by the scanner — used to exclude from public suggestions
}

// NewModule creates a new suggestions module
func NewModule(cfg *config.Manager, dbModule *database.Module) *Module {
	return &Module{
		config:    cfg,
		log:       logger.New("suggestions"),
		dbModule:  dbModule,
		profiles:  make(map[string]*UserProfile),
		mediaData: make(map[string]*MediaInfo),
		mediaByID: make(map[string]*MediaInfo),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "suggestions"
}

// profileEvictAfter is how long a user profile may remain unused before it is
// evicted from the in-memory map.  Profiles are persisted to MySQL so eviction
// only removes the in-memory copy; the data is reloaded on the next access.
const profileEvictAfter = 30 * 24 * time.Hour // 30 days

// Start initializes the module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting suggestions module...")

	m.repo = mysqlrepo.NewSuggestionProfileRepository(m.dbModule.GORM())

	// Load existing profiles
	if err := m.loadProfiles(); err != nil {
		m.log.Warn("Failed to load user profiles: %v", err)
	}

	// Start background profile-eviction goroutine so that profiles for
	// long-inactive or deleted users do not accumulate indefinitely.
	bgCtx, cancel := context.WithCancel(context.Background())
	m.ctx = bgCtx
	m.cancel = cancel
	m.wg.Add(1)
	go m.evictStaleProfiles(bgCtx)

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

	// Signal and wait for background goroutines.
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

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

// evictStaleProfiles removes in-memory profiles that have not been updated
// within profileEvictAfter.  The profiles remain persisted in MySQL; they will
// be reloaded on the next access if the user becomes active again.
func (m *Module) evictStaleProfiles(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-profileEvictAfter)
			m.mu.Lock()
			evicted := 0
			for id, profile := range m.profiles {
				if profile.LastUpdated.Before(cutoff) {
					delete(m.profiles, id)
					evicted++
				}
			}
			m.mu.Unlock()
			if evicted > 0 {
				m.log.Info("Evicted %d stale user profiles (inactive > %v)", evicted, profileEvictAfter)
			}
		}
	}
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

// RecordView records a view for a user.
// mediaPath is the filesystem path used to key ViewHistory entries.
func (m *Module) RecordView(userID, mediaPath, category, mediaType string, duration float64) {
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
		if vh.MediaPath == mediaPath {
			profile.ViewHistory[i].ViewCount++
			profile.ViewHistory[i].TotalTime += duration
			profile.ViewHistory[i].LastViewed = time.Now()
			found = true
			break
		}
	}

	if !found {
		profile.ViewHistory = append(profile.ViewHistory, ViewHistory{
			MediaPath:  mediaPath,
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

	m.log.Debug("Recorded view for user %s: %s (category: %s)", userID, mediaPath, category)
}

// RecordCompletion marks a media item as completed.
// mediaPath is the filesystem path used to match ViewHistory entries.
func (m *Module) RecordCompletion(userID, mediaPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile, ok := m.profiles[userID]
	if !ok {
		return
	}

	for i, vh := range profile.ViewHistory {
		if vh.MediaPath == mediaPath {
			completedAt := time.Now()
			profile.ViewHistory[i].CompletedAt = &completedAt
			break
		}
	}
}

// RecordRating records a user rating for a media item.
// mediaPath is the filesystem path used to match ViewHistory entries.
func (m *Module) RecordRating(userID, mediaPath string, rating float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile, ok := m.profiles[userID]
	if !ok {
		return
	}

	for i, vh := range profile.ViewHistory {
		if vh.MediaPath == mediaPath {
			profile.ViewHistory[i].Rating = rating
			break
		}
	}

	m.log.Debug("Recorded rating %.1f for %s by user %s", rating, mediaPath, userID)
}

// UpdateMediaData atomically replaces the in-memory media catalogue used for suggestions.
// Builds both indexes outside the lock then swaps in one operation to eliminate the
// window where mediaData is empty (IC-05).
func (m *Module) UpdateMediaData(items []*MediaInfo) {
	newData := make(map[string]*MediaInfo, len(items))
	newByID := make(map[string]*MediaInfo, len(items))
	for _, item := range items {
		newData[item.Path] = item
		if item.StableID != "" {
			newByID[item.StableID] = item
		}
	}

	m.mu.Lock()
	m.mediaData = newData
	m.mediaByID = newByID
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
		if media.IsMature {
			continue // Never surface mature items in public suggestions
		}
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

		// Add ±40% score jitter so results rotate meaningfully between calls
		score *= 1.0 + (rand.Float64()*0.80 - 0.40)

		suggestions = append(suggestions, &Suggestion{
			MediaID:   media.StableID,
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  media.Category,
			MediaType: media.MediaType,
			Score:     score,
			Reasons:   reasons,
		})
	}

	// Sort by jittered score, then randomly sample from the top pool for variety
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	const maxPerCategory = 3
	// Sample from top candidates then apply diversity so results vary on each call
	candidates := topShuffled(suggestions, limit*3)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	suggestions = diversify(candidates, limit, maxPerCategory)

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
// All items receive a minimum exploration baseline of 0.05 so that library items
// with no views/rating/recency still have a chance to surface in suggestions.
//
// Score budget (approximate maximums):
//   - Exploration baseline: 0.05
//   - Popularity:           ~0.30 (log10-scaled, 1000+ views)
//   - Recency:              0.10  (items < 7 days old)
//   - Rating:               0.20  (rating > 4)
//
// The recency boost is intentionally small so that profile-based scoring
// (up to ~0.50) and popularity can outweigh it.  This prevents the
// "Recommended" section from degenerating into a "Recently Added" list.
func scoreMediaBase(media *MediaInfo) (float64, []string) {
	score := 0.05 // exploration baseline — every non-mature item can appear
	var reasons []string

	popularityScore := math.Log10(float64(media.Views+1)) * 0.1
	score += popularityScore
	if popularityScore > 0.2 {
		reasons = append(reasons, "Popular content")
	}

	daysSinceAdded := time.Since(media.AddedAt).Hours() / 24
	if daysSinceAdded < 7 {
		newScore := 0.10 * (1 - daysSinceAdded/7)
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

// topShuffled takes the top min(len, n*poolFactor) items by score (already sorted
// descending) and returns a randomly shuffled selection of n items.  This ensures
// high-scored items dominate the candidate pool while still producing varied results
// on every call.
func topShuffled(sorted []*Suggestion, n int) []*Suggestion {
	if len(sorted) <= n {
		result := make([]*Suggestion, len(sorted))
		copy(result, sorted)
		rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
		return result
	}
	poolSize := n * 4
	if poolSize > len(sorted) {
		poolSize = len(sorted)
	}
	pool := make([]*Suggestion, poolSize)
	copy(pool, sorted[:poolSize])
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:n]
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
		if media.IsMature {
			continue // Never surface mature items in public suggestions
		}

		// Trending score is primarily driven by view count.  Items with
		// zero views get a tiny baseline so they don't dominate via the
		// recency multiplier alone.
		viewScore := float64(media.Views)
		score := viewScore * math.Max(media.Rating, 1)

		// Give a modest boost to recent content (< 30 days), but only
		// as a tiebreaker — not enough to override popularity.
		daysSinceAdded := time.Since(media.AddedAt).Hours() / 24
		if daysSinceAdded < 30 {
			score *= 1.2
		}

		// Add ±50% jitter for variety in trending results
		score *= 1.0 + (rand.Float64()*1.00 - 0.50)

		suggestions = append(suggestions, &Suggestion{
			MediaID:   media.StableID,
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  media.Category,
			MediaType: media.MediaType,
			Score:     score,
			Reasons:   []string{"Trending"},
		})
	}

	// Sort by jittered score, then sample from top pool for variety
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	candidates := topShuffled(suggestions, limit*3)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return diversify(candidates, limit, 3)
}

// GetSimilarMedia returns media similar to a given item.
// mediaID is the StableID (UUID) of the source item; path-based lookup is used
// as a fallback for items not yet indexed by ID.
func (m *Module) GetSimilarMedia(mediaID string, limit int) []*Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	// Look up source by StableID first, then by path for backward compatibility.
	sourceMedia := m.mediaByID[mediaID]
	if sourceMedia == nil {
		sourceMedia = m.mediaData[mediaID]
	}

	// Source not found in catalogue (not yet scanned or catalogue empty):
	// return a random sample from the library so the sidebar is never blank.
	if sourceMedia == nil {
		return m.randomSample(mediaID, limit)
	}

	var suggestions []*Suggestion

	for _, media := range m.mediaData {
		if media.StableID == sourceMedia.StableID || media.Path == sourceMedia.Path || media.IsMature {
			continue
		}

		score, reasons := computeSimilarity(sourceMedia, media)

		// Add ±50% score jitter for variety in related-media results
		score *= 1.0 + (rand.Float64()*1.00 - 0.50)

		if score > 0 {
			suggestions = append(suggestions, &Suggestion{
				MediaID:   media.StableID,
				MediaPath: media.Path,
				Title:     media.Title,
				Category:  media.Category,
				MediaType: media.MediaType,
				Score:     score,
				Reasons:   reasons,
			})
		}
	}

	// If we found too few similar items, pad with random library items.
	// Use low scores so random filler doesn't outrank genuinely similar items.
	if len(suggestions) < limit/2 {
		filler := m.randomSample(sourceMedia.StableID, limit)
		for _, f := range filler {
			f.Score *= 0.1 // scale down so filler stays below real matches
		}
		suggestions = append(suggestions, filler...)
	}

	// Sort by jittered score, then sample from top pool for variety
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Score > suggestions[j].Score
	})

	// Allow up to 60% of results from the same category as source
	sameCategory := limit*6/10 + 1
	candidates := topShuffled(suggestions, limit*3)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return diversify(candidates, limit, sameCategory)
}

// randomSample returns a random selection of non-mature items from the catalogue,
// excluding the item with the given StableID.  Used as a fallback when similarity
// scoring cannot produce enough results.
func (m *Module) randomSample(excludeID string, n int) []*Suggestion {
	pool := make([]*Suggestion, 0, len(m.mediaData))
	for _, media := range m.mediaData {
		if media.StableID == excludeID || media.IsMature {
			continue
		}
		pool = append(pool, &Suggestion{
			MediaID:   media.StableID,
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  media.Category,
			MediaType: media.MediaType,
			Score:     rand.Float64(),
			Reasons:   []string{"Discover something new"},
		})
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// computeSimilarity calculates how similar two media items are by category, type, tags, and title.
func computeSimilarity(source, candidate *MediaInfo) (float64, []string) {
	var score float64
	var reasons []string

	if candidate.Category == source.Category {
		score += 0.3
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
		mediaID := ""
		if media != nil {
			if media.IsMature {
				continue // Never surface mature items
			}
			title = media.Title
			mediaID = media.StableID
		}
		// Skip items we can't resolve to a stable ID (file may have been removed)
		if mediaID == "" {
			continue
		}

		suggestions = append(suggestions, &Suggestion{
			MediaID:   mediaID,
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
					MediaPath:   h.MediaPath,
					Category:    h.Category,
					MediaType:   h.MediaType,
					ViewCount:   h.ViewCount,
					TotalTime:   h.TotalTime,
					LastViewed:  h.LastViewed,
					CompletedAt: h.CompletedAt,
					Rating:      h.Rating,
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
