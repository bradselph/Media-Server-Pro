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
	done                 chan struct{}
	stopOnce             sync.Once
	bgWg                 sync.WaitGroup
	maxEvents            int
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
		maxEvents:            2000, // enough for accurate stat reconstruction; 10000 caused 500ms+ startup queries
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

	m.reconstructStats()

	cfg := m.config.Get()
	if err := os.MkdirAll(cfg.Directories.Analytics, 0755); err != nil {
		return fmt.Errorf("failed to create analytics directory: %w", err)
	}
	m.cleanupTicker = time.NewTicker(cfg.Analytics.CleanupInterval)

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
		close(m.done)
		m.bgWg.Wait()
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

// backgroundLoop handles periodic cleanup.
func (m *Module) backgroundLoop() {
	defer m.bgWg.Done()
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanup()
		case <-m.done:
			return
		}
	}
}
