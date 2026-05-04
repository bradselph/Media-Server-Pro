// Package analytics provides event tracking, session management, and statistics
// aggregation for the media server. It has a single responsibility: collecting,
// persisting, and reporting analytics data (views, playback, sessions).
package analytics

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/repositories"
	"media-server-pro/internal/repositories/mysql"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const dateFormat = "2006-01-02"

// Module implements analytics tracking.
type Module struct {
	config               *config.Manager
	log                  *logger.Logger
	dbModule             *database.Module
	eventRepo            repositories.AnalyticsRepository
	sessions             map[string]*sessionData
	dailyStats           map[string]*models.DailyStats
	dailyUsers           map[string]map[string]struct{} // keyed by date → set of userIDs
	mediaStats           map[string]*models.ViewStats
	mediaDurationSamples map[string]int                 // playback duration sample count per media (for AvgWatchDuration)
	mediaViewers         map[string]map[string]struct{} // keyed by mediaID → set of userIDs
	sessionsMu           sync.RWMutex
	statsMu              sync.RWMutex
	healthy              bool
	healthMsg            string
	healthMu             sync.RWMutex
	cleanupTicker        *time.Ticker
	flushTicker          *time.Ticker
	done                 chan struct{}
	stopOnce             sync.Once
	bgWg                 sync.WaitGroup
	maxEvents            int
	// dirtyDays records dates whose in-memory stats have changed since the last
	// successful flush to daily_stats. Guarded by dirtyMu, NOT statsMu, so the
	// flush loop can drain it without blocking the hot event path.
	dirtyDays map[string]struct{}
	dirtyMu   sync.Mutex
	// lastFlush records when the last successful flushDirtyDailyStats run
	// completed. Read by AnalyticsHealth so external monitors can detect a
	// stuck flush loop (lag >> 30s ticker = something is wrong).
	lastFlush   time.Time
	lastFlushMu sync.RWMutex
	// aggCache memoises expensive aggregation queries (top-users, top-searches,
	// error-paths, heatmap, devices, etc.) for ~30s so dashboard refreshes
	// don't hammer the analytics_events table with the same scan repeatedly.
	cache *aggCache
	// subs tracks active SSE / live-tail subscribers. Each tracked event is
	// broadcast (non-blocking) to every subscriber channel.
	subs *subscriberRegistry
}

// NewModule creates a new analytics module.
// The database module is stored but repositories are created during Start().
func NewModule(cfg *config.Manager, dbModule *database.Module) (*Module, error) {
	if dbModule == nil {
		return nil, fmt.Errorf("database module is required for analytics")
	}

	return &Module{
		config:               cfg,
		log:                  logger.New("analytics"),
		dbModule:             dbModule,
		sessions:             make(map[string]*sessionData),
		dailyStats:           make(map[string]*models.DailyStats),
		dailyUsers:           make(map[string]map[string]struct{}),
		mediaStats:           make(map[string]*models.ViewStats),
		mediaDurationSamples: make(map[string]int),
		mediaViewers:         make(map[string]map[string]struct{}),
		done:                 make(chan struct{}),
		maxEvents:            cfg.Get().Analytics.MaxReconstructEvents,
		dirtyDays:            make(map[string]struct{}),
		cache:                newAggCache(),
		subs:                 newSubscriberRegistry(),
	}, nil
}

// Name returns the module name.
func (m *Module) Name() string {
	return "analytics"
}

// Start initializes the analytics module.
func (m *Module) Start(_ context.Context) error {
	m.log.Info("Starting analytics module...")

	if !m.dbModule.IsConnected() {
		return fmt.Errorf("database is not connected")
	}
	m.log.Info("Using MySQL repository for analytics")
	m.eventRepo = mysql.NewAnalyticsRepository(m.dbModule.GORM())

	// Rehydrate persisted daily aggregates BEFORE event reconstruction so that
	// raw-event reconstruction (which is capped at maxEvents) tops up same-day
	// activity rather than serving as the only source of truth. Old daily rows
	// outside the retention window are pruned by cleanup, not here.
	m.loadDailyStats()
	m.reconstructStats()

	cfg := m.config.Get()
	if err := os.MkdirAll(cfg.Directories.Analytics, 0o755); err != nil { //nolint:gosec // G301: analytics dir needs world-read for serving
		return fmt.Errorf("failed to create analytics directory: %w", err)
	}
	m.cleanupTicker = time.NewTicker(cfg.Analytics.CleanupInterval)
	// 30-second flush cadence balances durability against DB write pressure.
	// On graceful shutdown we drain the dirty set in Stop(); ungraceful crashes
	// lose at most ~30s of aggregate data (the raw events themselves are still
	// persisted on every TrackEvent call so reconstruction recovers them).
	m.flushTicker = time.NewTicker(30 * time.Second)

	m.bgWg.Add(1)
	go m.backgroundLoop()

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Running"
	m.healthMu.Unlock()
	m.log.Info("Analytics module started")
	return nil
}

// Stop gracefully stops the module. Safe to call multiple times.
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping analytics module...")

	m.stopOnce.Do(func() {
		if m.cleanupTicker != nil {
			m.cleanupTicker.Stop()
		}
		if m.flushTicker != nil {
			m.flushTicker.Stop()
		}
		close(m.done)
		m.bgWg.Wait()
		// One last flush so any stats touched between the final tick and
		// shutdown reach disk. Use a fresh context so cancellation of the
		// caller's ctx does not abort the cleanup writes.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.flushDirtyDailyStats(ctx)
		// Cleanly close any live SSE subscribers so handlers exit fast
		// instead of waiting for the request context cancellation.
		m.closeAllSubscribers()
	})

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()
	return nil
}

// Health returns the module health status.
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	healthy := m.healthy
	msg := m.healthMsg
	m.healthMu.RUnlock()
	return models.HealthStatus{
		Name:      m.Name(),
		Status:    helpers.StatusString(healthy),
		Message:   msg,
		CheckedAt: time.Now(),
	}
}

// backgroundLoop handles periodic cleanup and daily-stats flushing.
func (m *Module) backgroundLoop() {
	defer m.bgWg.Done()
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanup()
		case <-m.flushTicker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			m.flushDirtyDailyStats(ctx)
			cancel()
		case <-m.done:
			return
		}
	}
}

// markDailyDirty records that the daily aggregate for the given date has
// changed and needs to be persisted on the next flush. Cheap enough to call
// from the hot event path: a single map assignment under a tiny mutex.
func (m *Module) markDailyDirty(date string) {
	m.dirtyMu.Lock()
	m.dirtyDays[date] = struct{}{}
	m.dirtyMu.Unlock()
}

// flushDirtyDailyStats writes every dirty daily aggregate to the database.
// Each row is captured under statsMu (read lock) and then written outside the
// lock so the hot event path is never blocked on DB I/O. Failed writes leave
// the date marked dirty so the next tick retries.
func (m *Module) flushDirtyDailyStats(ctx context.Context) {
	if m.eventRepo == nil {
		return
	}
	m.dirtyMu.Lock()
	if len(m.dirtyDays) == 0 {
		m.dirtyMu.Unlock()
		return
	}
	dates := make([]string, 0, len(m.dirtyDays))
	for d := range m.dirtyDays {
		dates = append(dates, d)
	}
	// Clear the set optimistically; failed writes are re-marked below.
	m.dirtyDays = make(map[string]struct{})
	m.dirtyMu.Unlock()

	for _, date := range dates {
		m.statsMu.RLock()
		ds, ok := m.dailyStats[date]
		var snapshot models.DailyStats
		if ok {
			snapshot = *ds
		}
		m.statsMu.RUnlock()
		if !ok {
			continue
		}
		if err := m.eventRepo.UpsertDailyStats(ctx, &snapshot); err != nil {
			m.log.Warn("Failed to persist daily stats for %s: %v", date, err)
			// Re-mark so we retry on the next tick instead of silently losing.
			m.markDailyDirty(date)
		}
	}
	m.lastFlushMu.Lock()
	m.lastFlush = time.Now()
	m.lastFlushMu.Unlock()
}

// loadDailyStats hydrates the in-memory dailyStats map from the database.
// Called once during Start, before reconstructStats. Rows older than the
// retention cutoff are skipped — the cleanup loop is responsible for pruning
// them at the SQL layer; loading them here would just inflate memory.
func (m *Module) loadDailyStats() {
	if m.eventRepo == nil {
		return
	}
	cfg := m.config.Get()
	cutoff := time.Now().AddDate(0, 0, -cfg.Analytics.RetentionDays).Format(dateFormat)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := m.eventRepo.ListDailyStatsBetween(ctx, cutoff, "")
	if err != nil {
		m.log.Warn("Failed to load persisted daily stats: %v", err)
		return
	}
	m.statsMu.Lock()
	for _, ds := range rows {
		// Defensive copy — repo returns pointers into a result slice.
		row := *ds
		m.dailyStats[row.Date] = &row
	}
	m.statsMu.Unlock()
	m.log.Info("Loaded %d persisted daily stats rows", len(rows))
}
