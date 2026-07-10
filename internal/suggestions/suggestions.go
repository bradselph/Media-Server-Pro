// Package suggestions provides content recommendation based on viewing history.
package suggestions

import (
	"context"
	"fmt"
	"maps"
	"math"
	"math/rand" // Go 1.20+ auto-seeds the default source
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
	dirty           bool               // true when profile has unsaved mutations; protected by module mu (writers hold Lock, saveProfiles holds RLock)
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
	Duration     float64  `json:"duration,omitempty"`
	ThumbnailURL string   `json:"thumbnail_url,omitempty"`
	// UserRating is the requesting user's own rating (1-5), populated by the
	// handler layer per request; nil (omitted) when unrated or anonymous.
	UserRating *float64 `json:"user_rating,omitempty"`
}

// Module handles content suggestions. RecordView is called from the streaming
// handler (StreamMedia) on each authenticated playback event, integrating
// analytics view events with the suggestion engine for personalized recommendations.
// Suggestion data is stored in MySQL via the SuggestionProfileRepository.
type Module struct {
	config          *config.Manager
	log             *logger.Logger
	dbModule        *database.Module
	repo            repositories.SuggestionProfileRepository
	profiles        map[string]*UserProfile
	mediaData       map[string]*MediaInfo // keyed by filesystem path
	mediaByID       map[string]*MediaInfo // keyed by StableID (secondary index)
	catalogueSeeded bool                  // true after first non-empty UpdateMediaData
	mu              sync.RWMutex
	healthy         bool
	healthMsg       string
	healthMu        sync.RWMutex
	// ctx/cancel drive the background profile-eviction goroutine.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// MediaInfo holds information about a media file for suggestions
type MediaInfo struct {
	Path     string
	StableID string // UUID assigned by the media module; used as the public-facing MediaID
	Title    string
	// CategoryIDs are the curated MediaCategory IDs this item belongs to
	// (from media_category_items). Replaces the retired single path-detected
	// category string; personalization scores against these.
	CategoryIDs []string
	MediaType   string
	Tags        []string
	Views       int
	Rating      float64
	Duration    float64
	AddedAt     time.Time
	IsMature    bool // flagged by the scanner — used to exclude from public suggestions
}

// primaryCategory returns the first curated category ID for grouping/diversity,
// or "" when the item belongs to no category.
func primaryCategory(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// hasCommonCategory reports whether two curated-category-ID slices intersect.
func hasCommonCategory(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, y := range b {
		if _, ok := set[y]; ok {
			return true
		}
	}
	return false
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

// profileSaveInterval controls how often in-memory profiles are flushed to
// MySQL.  This provides crash resilience — without periodic saves, profile
// updates accumulated in memory would be lost if the server crashes before
// a graceful Stop().
const profileSaveInterval = 10 * time.Minute

// Start initializes the module
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting suggestions module...")

	m.repo = mysqlrepo.NewSuggestionProfileRepository(m.dbModule.GORM())

	// Load existing profiles
	if err := m.loadProfiles(); err != nil {
		m.log.Warn("Failed to load user profiles: %v", err)
	}

	// Start background goroutines:
	// - periodic save: flushes in-memory profiles to MySQL for crash resilience
	// - profile eviction: removes stale profiles from memory (DB copy preserved)
	bgCtx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel stored in m.cancel, called by Stop()
	m.ctx = bgCtx
	m.cancel = cancel
	m.wg.Add(2)
	go m.periodicSave(bgCtx)       //nolint:gosec // G118: bgCtx is canceled by Stop()
	go m.evictStaleProfiles(bgCtx) //nolint:gosec // G118: bgCtx is canceled by Stop()

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

// periodicSave flushes in-memory profiles to MySQL at regular intervals.
// This ensures that profile updates are not lost if the server crashes
// before a graceful Stop().
func (m *Module) periodicSave(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(profileSaveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.saveProfiles(); err != nil {
				m.log.Warn("Periodic profile save failed: %v", err)
			} else {
				m.log.Debug("Periodic profile save complete")
			}
		}
	}
}

// evictStaleProfiles removes in-memory profiles not updated within profileEvictAfter; each is saved to MySQL before eviction.
// Evicted users get a new empty profile on next activity (no reload from DB).
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
			var toEvict []*UserProfile
			for _, profile := range m.profiles {
				if profile.LastUpdated.Before(cutoff) {
					// Snapshot under the lock: saveOneProfile reads the profile's maps
					// and slices without locking, so it must operate on a copy rather
					// than the live profile that concurrent RecordView/RecordRating
					// callers mutate. Mirrors the snapshot pattern in saveProfiles.
					toEvict = append(toEvict, m.snapshotProfile(profile))
				}
			}
			m.mu.Unlock()

			// Save each stale profile to MySQL before evicting from memory.
			bgCtx := context.Background()
			for _, profile := range toEvict {
				_ = m.saveOneProfile(bgCtx, profile)
			}

			// Re-check LastUpdated under write lock before evicting (TOCTOU: profile may have been updated).
			if len(toEvict) > 0 {
				m.mu.Lock()
				actuallyEvicted := 0
				for _, profile := range toEvict {
					if p, ok := m.profiles[profile.UserID]; ok && p.LastUpdated.Before(cutoff) {
						delete(m.profiles, profile.UserID)
						actuallyEvicted++
					}
				}
				m.mu.Unlock()
				if actuallyEvicted > 0 {
					m.log.Info("Evicted %d stale user profiles (inactive > %v)", actuallyEvicted, profileEvictAfter)
				}
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
// categoryIDs are the curated MediaCategory IDs the viewed item belongs to; each
// gets a score bump so personalization reflects real categories. The item's
// primary category is stored on the ViewHistory entry for "recently viewed"
// similarity matching.
func (m *Module) RecordView(userID, mediaPath string, categoryIDs []string, mediaType string, duration float64) {
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
		if vh.MediaPath != mediaPath {
			continue
		}
		profile.ViewHistory[i].ViewCount++
		profile.ViewHistory[i].TotalTime += duration
		profile.ViewHistory[i].LastViewed = time.Now()
		// Refresh the stored primary category so a pre-migration entry holding a
		// stale path-detected bucket string is lazily corrected to a curated id
		// (otherwise scoreRecentlyViewed could never match it).
		profile.ViewHistory[i].Category = primaryCategory(categoryIDs)
		found = true
		break
	}

	if !found {
		profile.ViewHistory = append(profile.ViewHistory, ViewHistory{
			MediaPath:  mediaPath,
			Category:   primaryCategory(categoryIDs),
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

	// Bump the score for every curated category the item belongs to.
	for _, cid := range categoryIDs {
		profile.CategoryScores[cid] += 1.0
	}
	profile.TypePreferences[mediaType] += 1.0
	profile.TotalViews++
	profile.TotalWatchTime += duration
	profile.LastUpdated = time.Now()
	profile.dirty = true

	m.log.Debug("Recorded view for user %s: %s (categories: %v)", userID, mediaPath, categoryIDs)
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
			profile.ViewHistory[i].CompletedAt = new(time.Now())
			profile.dirty = true
			break
		}
	}
}

// RecordRating records a user rating for a media item.
// mediaPath is the filesystem path used to match ViewHistory entries.
// A rating-only ViewHistory entry is created if none exists yet, so that ratings
// made before a first view (e.g. browsing) are not silently dropped.
// The updated profile is persisted immediately rather than waiting for the next
// periodic save (up to 10 minutes).
func (m *Module) RecordRating(userID, mediaPath string, rating float64) {
	m.mu.Lock()

	profile, ok := m.profiles[userID]
	if !ok {
		// No in-memory profile (server restart or eviction). Create a minimal one.
		profile = &UserProfile{
			UserID:          userID,
			CategoryScores:  make(map[string]float64),
			TypePreferences: make(map[string]float64),
			ViewHistory:     make([]ViewHistory, 0),
		}
		m.profiles[userID] = profile
	}

	for i, vh := range profile.ViewHistory {
		if vh.MediaPath != mediaPath {
			continue
		}
		profile.ViewHistory[i].Rating = rating
		profile.LastUpdated = time.Now()
		profile.dirty = true
		m.log.Debug("Recorded rating %.1f for %s by user %s", rating, mediaPath, userID)
		snap := m.snapshotProfile(profile)
		m.mu.Unlock()
		m.persistRating(userID, snap)
		return
	}

	// No ViewHistory entry for this item (user rated without watching).
	// Create a rating-only record so the rating is persisted.
	profile.ViewHistory = append(profile.ViewHistory, ViewHistory{
		MediaPath:  mediaPath,
		Rating:     rating,
		LastViewed: time.Now(),
	})
	const maxViewHistory = 500
	if len(profile.ViewHistory) > maxViewHistory {
		profile.ViewHistory = profile.ViewHistory[len(profile.ViewHistory)-maxViewHistory:]
	}
	profile.LastUpdated = time.Now()
	profile.dirty = true
	m.log.Debug("Recorded rating %.1f for %s by user %s (new entry)", rating, mediaPath, userID)
	snap := m.snapshotProfile(profile)
	m.mu.Unlock()
	m.persistRating(userID, snap)
}

// snapshotProfile returns a deep copy of a UserProfile safe to use after releasing m.mu.
func (m *Module) snapshotProfile(profile *UserProfile) *UserProfile {
	cp := *profile
	cp.CategoryScores = make(map[string]float64, len(profile.CategoryScores))
	maps.Copy(cp.CategoryScores, profile.CategoryScores)
	cp.TypePreferences = make(map[string]float64, len(profile.TypePreferences))
	maps.Copy(cp.TypePreferences, profile.TypePreferences)
	cp.ViewHistory = make([]ViewHistory, len(profile.ViewHistory))
	copy(cp.ViewHistory, profile.ViewHistory)
	return &cp
}

// persistRating immediately saves a profile snapshot to the DB in a background goroutine.
// Errors are logged but not returned — rating persistence is best-effort relative to the
// HTTP response, but no longer delayed by the periodic save interval.
func (m *Module) persistRating(userID string, snap *UserProfile) {
	if m.repo == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.saveOneProfile(ctx, snap); err != nil {
			m.log.Warn("RecordRating: failed to persist rating for %s: %v", userID, err)
		}
	}()
}

// UpdateMediaData atomically replaces the in-memory media catalog used for suggestions.
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
	// Mark catalog ready after first update (even if empty) so API returns 200 with []
	// instead of 503 while the UI can show "no suggestions yet" or retry.
	m.catalogueSeeded = true
	m.mu.Unlock()

	m.log.Info("Updated media data: %d items", len(items))
}

// IsCatalogueReady reports whether the media catalog has been seeded at least
// once with a non-empty data set. Before this returns true, all suggestion
// endpoints will return empty results. Handlers use this to return 503 with
// Retry-After instead of misleading empty arrays.
func (m *Module) IsCatalogueReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.catalogueSeeded
}

// GetSuggestions returns personalized suggestions for a user.
// canViewMature: when true, mature items are included; when false, they are excluded.
func (m *Module) GetSuggestions(userID string, limit int, canViewMature bool) []*Suggestion {
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

	// Pre-build the set of categories the user viewed in the last 7 days once,
	// instead of rescanning the full ViewHistory for every catalog item inside
	// scoreRecentlyViewed (which made the pass O(catalog × history)).
	recentCategorySet := recentlyViewedCategorySet(profile)

	// Sum the profile's category/type preference maps once, not per catalog item.
	totals := computeProfileTotals(profile)

	var suggestions []*Suggestion
	for _, media := range m.mediaData {
		if media.IsMature && !canViewMature {
			continue // Exclude mature items when user has not enabled mature content
		}
		if recentlyViewed[media.Path] {
			continue // Skip recently viewed
		}

		score, reasons := m.scoreMedia(profile, media, recentCategorySet, totals)

		// Apply re-watch penalty for previously-seen items (penalty fades over 90 days)
		if lastViewed, ok := oldViewedAt[media.Path]; ok {
			daysSince := time.Since(lastViewed).Hours() / 24
			penalty := 0.5 * (1 - math.Min(1, daysSince/90))
			score *= 1 - penalty
			reasons = append(reasons, "Watch again")
		}

		// Add ±40% score jitter so results rotate meaningfully between calls
		score *= 1.0 + (rand.Float64()*0.80 - 0.40) //nolint:gosec // G404: math/rand is fine for suggestion jitter, not security

		suggestions = append(suggestions, &Suggestion{
			MediaID:   media.StableID,
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  primaryCategory(media.CategoryIDs),
			MediaType: media.MediaType,
			Score:     score,
			Reasons:   reasons,
			Duration:  media.Duration,
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

// scoreMedia calculates a suggestion score for a media item. recentCategorySet
// is the pre-built set of categories the profile viewed in the last 7 days
// (see recentlyViewedCategorySet), threaded through to avoid an O(history)
// rescan per item in scoreRecentlyViewed.
// profileTotals holds sums over a profile's preference maps that are constant for
// the duration of one scoring pass. Precomputing them once per GetSuggestions call
// (instead of re-summing inside scoreCategoryPreference/scoreTypePreference for every
// catalog item) turns an O(catalog x profile-size) cost into O(catalog + profile-size).
type profileTotals struct {
	categoryTotal float64
	typeTotal     float64
}

// computeProfileTotals sums the profile's category and type preference maps once.
func computeProfileTotals(profile *UserProfile) profileTotals {
	var t profileTotals
	if profile == nil {
		return t
	}
	for _, s := range profile.CategoryScores {
		t.categoryTotal += s
	}
	for _, s := range profile.TypePreferences {
		t.typeTotal += s
	}
	return t
}

func (m *Module) scoreMedia(profile *UserProfile, media *MediaInfo, recentCategorySet map[string]bool, totals profileTotals) (score float64, reasons []string) {
	score, reasons = scoreMediaBase(media)

	if profile != nil {
		profileScore, profileReasons := scoreMediaForProfile(profile, media, recentCategorySet, totals)
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
func scoreMediaBase(media *MediaInfo) (score float64, reasons []string) {
	score = 0.05 // exploration baseline — every non-mature item can appear

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
	poolSize := min(n*4, len(sorted))
	pool := make([]*Suggestion, poolSize)
	copy(pool, sorted[:poolSize])
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:n]
}

// scoreMediaForProfile calculates the personalized score based on a user profile.
// totals carries the pre-summed category/type preference totals so the sub-scorers
// don't re-sum the whole profile for every catalog item.
func scoreMediaForProfile(profile *UserProfile, media *MediaInfo, recentCategorySet map[string]bool, totals profileTotals) (score float64, reasons []string) {
	score += scoreCategoryPreference(profile, media, &reasons, totals.categoryTotal)
	score += scoreTypePreference(profile, media, totals.typeTotal)
	score += scoreRecentlyViewed(recentCategorySet, media, &reasons)

	return score, reasons
}

// scoreCategoryPreference calculates the category preference boost. An item can
// belong to several curated categories; it is scored by its strongest matching
// category against the user's accumulated category scores.
func scoreCategoryPreference(profile *UserProfile, media *MediaInfo, reasons *[]string, totalCategoryScore float64) float64 {
	if len(media.CategoryIDs) == 0 {
		return 0
	}
	bestScore := 0.0
	for _, cid := range media.CategoryIDs {
		if s, ok := profile.CategoryScores[cid]; ok && s > bestScore {
			bestScore = s
		}
	}
	if bestScore <= 0 {
		return 0
	}
	if totalCategoryScore <= 0 {
		return 0
	}
	normalizedCategoryScore := (bestScore / totalCategoryScore) * 0.5
	if normalizedCategoryScore > 0.1 {
		// Category IDs are opaque UUIDs, so the reason stays generic rather than
		// leaking an ID into a user-facing string.
		*reasons = append(*reasons, "Matches your interests")
	}
	return normalizedCategoryScore
}

// scoreTypePreference calculates the media type preference boost. totalTypeScore is
// the pre-summed total of the profile's type preferences (constant across a scoring
// pass), passed in rather than re-summed per catalog item.
func scoreTypePreference(profile *UserProfile, media *MediaInfo, totalTypeScore float64) float64 {
	typeScore, ok := profile.TypePreferences[media.MediaType]
	if !ok {
		return 0
	}
	if totalTypeScore <= 0 {
		return 0
	}
	return (typeScore / totalTypeScore) * 0.3
}

// recentlyViewedCategorySet returns the set of primary category IDs the profile
// viewed within the last 7 days. Building this once per GetSuggestions call
// lets scoreRecentlyViewed do an O(len(CategoryIDs)) set lookup per item instead
// of rescanning the whole ViewHistory for every catalog item.
func recentlyViewedCategorySet(profile *UserProfile) map[string]bool {
	if profile == nil {
		return nil
	}
	set := make(map[string]bool)
	for _, vh := range profile.ViewHistory {
		if vh.Category != "" && time.Since(vh.LastViewed) < 7*24*time.Hour {
			set[vh.Category] = true
		}
	}
	return set
}

// scoreRecentlyViewed adds a boost if the candidate shares a curated category
// with something the user viewed in the last 7 days. recentCategorySet is the
// pre-built set from recentlyViewedCategorySet; this is semantically identical
// to scanning ViewHistory per item (the boost is a constant 0.1 on first match)
// but O(len(CategoryIDs)) instead of O(len(ViewHistory)).
func scoreRecentlyViewed(recentCategorySet map[string]bool, media *MediaInfo, reasons *[]string) float64 {
	if len(media.CategoryIDs) == 0 || len(recentCategorySet) == 0 {
		return 0
	}
	for _, cid := range media.CategoryIDs {
		if recentCategorySet[cid] {
			*reasons = append(*reasons, "Similar to recently viewed")
			return 0.1
		}
	}
	return 0
}

// GetTrendingSuggestions returns trending content.
// canViewMature: when true, mature items are included; when false, they are excluded.
func (m *Module) GetTrendingSuggestions(limit int, canViewMature bool) []*Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	var suggestions []*Suggestion

	for _, media := range m.mediaData {
		if media.IsMature && !canViewMature {
			continue // Exclude mature items when user has not enabled mature content
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
		score *= 1.0 + (rand.Float64()*1.00 - 0.50) //nolint:gosec // G404: math/rand is fine for suggestion jitter, not security

		suggestions = append(suggestions, &Suggestion{
			MediaID:   media.StableID,
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  primaryCategory(media.CategoryIDs),
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
// canViewMature: when true, mature items are included; when false, they are excluded.
func (m *Module) GetSimilarMedia(mediaID string, limit int, canViewMature bool) []*Suggestion {
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

	// Source not found in catalog (not yet scanned or catalog empty):
	// return a random sample from the library so the sidebar is never blank.
	if sourceMedia == nil {
		return m.randomSample(mediaID, limit, canViewMature)
	}

	// Tokenize the source title once — it is invariant across every candidate, so
	// computeTitleSimilarity should not re-lower/re-split it per catalog item.
	sourceWords := titleWords(sourceMedia.Title)

	var suggestions []*Suggestion

	for _, media := range m.mediaData {
		if media.StableID == sourceMedia.StableID || media.Path == sourceMedia.Path {
			continue
		}
		if media.IsMature && !canViewMature {
			continue // Exclude mature items when user has not enabled mature content
		}

		score, reasons := computeSimilarity(sourceMedia, media, sourceWords)

		// Add ±50% score jitter for variety in related-media results
		score *= 1.0 + (rand.Float64()*1.00 - 0.50) //nolint:gosec // G404: math/rand is fine for suggestion jitter, not security

		if score > 0 {
			suggestions = append(suggestions, &Suggestion{
				MediaID:   media.StableID,
				MediaPath: media.Path,
				Title:     media.Title,
				Category:  primaryCategory(media.CategoryIDs),
				MediaType: media.MediaType,
				Score:     score,
				Reasons:   reasons,
			})
		}
	}

	// If we found too few similar items, pad with random library items.
	// Use low scores so random filler doesn't outrank genuinely similar items.
	if len(suggestions) < limit/2 {
		// randomSample only excludes the source item, so it can re-draw items
		// already present as genuine matches. Skip those, otherwise the same
		// MediaID appears twice (once as a real match, once as filler) — nothing
		// downstream (sort/topShuffled/diversify) de-dupes by MediaID.
		seen := make(map[string]bool, len(suggestions))
		for _, s := range suggestions {
			seen[s.MediaID] = true
		}
		filler := m.randomSample(sourceMedia.StableID, limit, canViewMature)
		for _, f := range filler {
			if seen[f.MediaID] {
				continue
			}
			f.Score *= 0.1 // scale down so filler stays below real matches
			suggestions = append(suggestions, f)
		}
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

// randomSample returns a random selection from the catalog, excluding the item
// with the given StableID. Mature items are excluded when canViewMature is false.
func (m *Module) randomSample(excludeID string, n int, canViewMature bool) []*Suggestion {
	pool := make([]*Suggestion, 0, len(m.mediaData))
	for _, media := range m.mediaData {
		if media.StableID == excludeID {
			continue
		}
		if media.IsMature && !canViewMature {
			continue
		}
		pool = append(pool, &Suggestion{
			MediaID:   media.StableID,
			MediaPath: media.Path,
			Title:     media.Title,
			Category:  primaryCategory(media.CategoryIDs),
			MediaType: media.MediaType,
			Score:     rand.Float64(), //nolint:gosec // G404: math/rand acceptable for non-security suggestions
			Reasons:   []string{"Discover something new"},
		})
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// titleWords lowercases and splits a title into words for overlap scoring.
func titleWords(title string) []string {
	return strings.Fields(strings.ToLower(title))
}

// computeSimilarity calculates how similar two media items are by category, type,
// tags, and title. sourceWords is the pre-tokenized source title (see titleWords),
// passed in so a per-candidate call does not re-tokenize the constant source title.
func computeSimilarity(source, candidate *MediaInfo, sourceWords []string) (score float64, reasons []string) {
	if hasCommonCategory(candidate.CategoryIDs, source.CategoryIDs) {
		score += 0.3
		reasons = append(reasons, "Same category")
	}
	if candidate.MediaType == source.MediaType {
		score += 0.3
	}

	score += computeTagSimilarity(source, candidate, &reasons)
	score += computeTitleSimilarity(sourceWords, candidate)

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

// computeTitleSimilarity calculates a simple word-overlap score between a
// pre-tokenized source title (sourceWords) and a candidate's title.
func computeTitleSimilarity(sourceWords []string, candidate *MediaInfo) float64 {
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

// GetContinueWatching returns items the user started but didn't finish.
// canViewMature: when true, mature items are included; when false, they are excluded.
func (m *Module) GetContinueWatching(userID string, limit int, canViewMature bool) []*Suggestion {
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
			if media.IsMature && !canViewMature {
				continue // Exclude mature items when user has not enabled mature content
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

// GetUserProfile returns a deep copy of the user's suggestion profile, or nil if not found.
// A deep copy is required because CategoryScores and TypePreferences are maps; a shallow
// copy would share the underlying map with the internal profile, allowing callers to
// mutate module state without holding the lock.
func (m *Module) GetUserProfile(userID string) *UserProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	profile, ok := m.profiles[userID]
	if !ok {
		return nil
	}
	return m.snapshotProfile(profile)
}

// GetUserRatingsByPath returns media path → star rating for the user's rated
// view-history entries, built directly under the read lock. This avoids the
// full snapshotProfile deep-copy (two maps + a 500-entry ViewHistory slice copy)
// that GetUserProfile incurs on every authenticated ListMedia request just to
// extract a handful of ratings. Returns nil when the user has no profile so the
// caller can distinguish "anonymous / no profile" from "registered but unrated".
func (m *Module) GetUserRatingsByPath(userID string) map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	profile, ok := m.profiles[userID]
	if !ok {
		return nil
	}
	out := make(map[string]float64)
	for _, vh := range profile.ViewHistory {
		if vh.Rating > 0 && vh.MediaPath != "" {
			out[vh.MediaPath] = vh.Rating
		}
	}
	return out
}

// PurgeMediaPath removes all view history entries for the given media path from every
// in-memory profile and deletes the corresponding rows from the database. Called when
// a media item is deleted so that orphaned view-history rows do not skew suggestions.
func (m *Module) PurgeMediaPath(mediaPath string) {
	if m.repo == nil {
		return
	}
	m.mu.Lock()
	for _, profile := range m.profiles {
		filtered := profile.ViewHistory[:0]
		for _, vh := range profile.ViewHistory {
			if vh.MediaPath != mediaPath {
				filtered = append(filtered, vh)
			}
		}
		if len(filtered) != len(profile.ViewHistory) {
			profile.ViewHistory = filtered
			profile.dirty = true
		}
	}
	m.mu.Unlock()

	ctx := context.Background()
	if err := m.repo.DeleteViewHistoryByMediaPath(ctx, mediaPath); err != nil {
		m.log.Error("failed to purge view history for deleted media %q: %v", mediaPath, err)
	}
}

// RenameMediaPath re-keys view-history entries when a media file is renamed so
// users' ratings and watch history follow the file instead of being orphaned.
// Loaded profiles are re-keyed in memory; the database rows are re-keyed
// directly so users whose profiles are not currently loaded (evicted) migrate
// too. When a profile already has an entry for newPath, the old entry is
// dropped in its favor rather than duplicated.
func (m *Module) RenameMediaPath(oldPath, newPath string) {
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return
	}

	m.mu.Lock()
	for _, profile := range m.profiles {
		oldIdx, newIdx := -1, -1
		for i := range profile.ViewHistory {
			switch profile.ViewHistory[i].MediaPath {
			case oldPath:
				oldIdx = i
			case newPath:
				newIdx = i
			}
		}
		if oldIdx == -1 {
			continue
		}
		if newIdx >= 0 {
			profile.ViewHistory = append(profile.ViewHistory[:oldIdx], profile.ViewHistory[oldIdx+1:]...)
		} else {
			profile.ViewHistory[oldIdx].MediaPath = newPath
		}
		// Not marked dirty: the database is re-keyed below, so memory and DB
		// already agree without another flush.
	}
	m.mu.Unlock()

	if m.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.repo.RenameViewHistoryMediaPath(ctx, oldPath, newPath); err != nil {
		m.log.Error("failed to re-key view history for renamed media %q -> %q: %v", oldPath, newPath, err)
	}
}

// ResetUserProfile clears the in-memory profile and deletes persisted data (profile
// row + view history) from the database for the given user.  After the reset the
// user starts accumulating a fresh profile from their next viewing session.
func (m *Module) ResetUserProfile(userID string) error {
	if m.repo == nil {
		return nil
	}
	m.mu.Lock()
	delete(m.profiles, userID)
	m.mu.Unlock()

	ctx := context.Background()
	if err := m.repo.DeleteProfile(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete suggestion profile: %w", err)
	}
	if err := m.repo.DeleteViewHistory(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete suggestion view history: %w", err)
	}
	return nil
}

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

	// Build complete profile structs (including per-user view history) before
	// acquiring the lock. The original code held m.mu.Lock() across N DB calls
	// (one GetViewHistory per profile), blocking all concurrent callers for the
	// entire startup load. Doing all IO outside the lock mirrors the saveProfiles
	// design and keeps the critical section to a simple map bulk-insert.
	built := make([]*UserProfile, 0, len(profiles))
	for _, rec := range profiles {
		profile := &UserProfile{
			UserID:          rec.UserID,
			CategoryScores:  rec.CategoryScores,
			TypePreferences: rec.TypePreferences,
			TotalViews:      rec.TotalViews,
			TotalWatchTime:  rec.TotalWatchTime,
			LastUpdated:     rec.LastUpdated,
		}
		history, err := m.repo.GetViewHistory(context.Background(), rec.UserID)
		if err != nil {
			m.log.Warn("Failed to load view history for suggestion profile %s: %v", rec.UserID, err)
		} else {
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
		built = append(built, profile)
	}

	// Single critical section: bulk-insert the pre-built profiles.
	m.mu.Lock()
	for _, profile := range built {
		m.profiles[profile.UserID] = profile
	}
	m.mu.Unlock()
	return nil
}

// saveProfiles persists dirty profiles only; continues on error and returns the last error.
// IP-based guest profiles (user_id prefixed with "ip:") are intentionally skipped because
// they have no matching row in the users table and would fail the FK constraint.
//
// Design: the lock is held only for the snapshot phase (RLock) and the dirty-clear phase
// (Lock). DB writes happen without the lock so that long saves do not block RecordView /
// RecordRating callers. dirty is cleared only after a successful save so failed writes are
// retried on the next periodic tick.
func (m *Module) saveProfiles() error {
	// Phase 1 — snapshot all dirty profiles under RLock (safe: read-only on fields).
	type snapEntry struct {
		userID string
		snap   *UserProfile
	}
	m.mu.RLock()
	var snaps []snapEntry
	for _, profile := range m.profiles {
		if !profile.dirty {
			continue
		}
		// Skip transient guest profiles — they are tracked in memory for session-level
		// recommendations but must never be written to the DB (no users row, FK violation).
		if strings.HasPrefix(profile.UserID, "ip:") {
			continue
		}
		snaps = append(snaps, snapEntry{
			userID: profile.UserID,
			snap:   m.snapshotProfile(profile),
		})
	}
	m.mu.RUnlock()

	// Phase 2 — save each snapshot to DB without holding the lock.
	ctx := context.Background()
	var lastErr error
	saved := 0
	for _, entry := range snaps {
		if err := m.saveOneProfile(ctx, entry.snap); err != nil {
			m.log.Warn("Failed to save suggestion profile for %s: %v", entry.userID, err)
			lastErr = err
		} else {
			// Phase 3 — clear dirty flag under Lock now that the save succeeded.
			m.mu.Lock()
			if p, ok := m.profiles[entry.userID]; ok {
				p.dirty = false
			}
			m.mu.Unlock()
			saved++
		}
	}
	if saved > 0 {
		m.log.Debug("Saved %d dirty suggestion profiles", saved)
	}
	return lastErr
}

// saveOneProfile persists a single user profile and its view history to MySQL.
// Uses batch upsert for view history entries instead of individual writes.
func (m *Module) saveOneProfile(ctx context.Context, profile *UserProfile) error {
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
	entries := make([]*repositories.ViewHistoryRecord, len(profile.ViewHistory))
	for i := range profile.ViewHistory {
		vh := &profile.ViewHistory[i]
		entries[i] = &repositories.ViewHistoryRecord{
			MediaPath:   vh.MediaPath,
			Category:    vh.Category,
			MediaType:   vh.MediaType,
			ViewCount:   vh.ViewCount,
			TotalTime:   vh.TotalTime,
			LastViewed:  vh.LastViewed,
			CompletedAt: vh.CompletedAt,
			Rating:      vh.Rating,
		}
	}
	return m.repo.BatchSaveViewHistory(ctx, profile.UserID, entries)
}
