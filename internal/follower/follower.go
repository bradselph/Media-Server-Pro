// Package follower lets a full Media Server Pro instance act as a slave
// node to another Media Server Pro instance.
//
// When enabled, the follower opens a persistent WebSocket connection to a
// remote master, registers as a slave, pushes its local media catalog
// (sourced from the existing media.Module — no separate scan), and serves
// stream-push requests by reading files from the local filesystem. From the
// remote master's perspective the connection is indistinguishable from the
// standalone cmd/media-receiver binary, so no master-side changes are needed.
//
// This eliminates the need to deploy and maintain a separate receiver binary
// when both sides are running the full server. Pairing is configured through
// the admin UI (see api/handlers/admin_follower.go).
package follower

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

// Module implements server.Module for the in-server follower.
type Module struct {
	config *config.Manager
	log    *logger.Logger
	media  *media.Module

	// healthMu protects the health snapshot returned by Health().
	healthMu  sync.RWMutex
	healthy   bool
	healthMsg string

	// statusMu protects the live status fields exposed via /api/admin/follower/status.
	statusMu        sync.RWMutex
	connected       bool
	lastConnectedAt time.Time
	lastError       string
	lastErrorAt     time.Time
	lastCatalogPush time.Time
	lastCatalogSize int

	// Lifecycle: cancel stops the WS loop; loopDone signals the loop has exited.
	loopMu   sync.Mutex
	cancel   context.CancelFunc
	loopDone chan struct{}
}

// NewModule constructs the follower module. media is required because the
// follower's catalog is sourced from the existing local media library — there
// is no separate scan loop.
func NewModule(cfg *config.Manager, mediaMod *media.Module) *Module {
	return &Module{
		config: cfg,
		log:    logger.New("follower"),
		media:  mediaMod,
	}
}

// Name implements server.Module.
func (m *Module) Name() string { return "follower" }

// Start implements server.Module. Spawns the WS loop in a goroutine when the
// required URL/key are present; otherwise marks healthy (idle) and returns nil
// so the server still starts cleanly when the operator hasn't paired this
// instance yet. The follower auto-enables on configuration completeness — a
// separate Enabled toggle would diverge from "fill in the form, it pairs".
func (m *Module) Start(_ context.Context) error {
	cfg := m.config.Get().Follower
	if !m.hasRequiredConfig(cfg) {
		m.setHealth(true, "Disabled — not configured (master_url + api_key required)")
		return nil
	}
	if err := m.startLoop(); err != nil {
		m.setHealth(false, fmt.Sprintf("Failed to start: %v", err))
		// Returning nil keeps follower non-critical: a misconfigured pairing
		// shouldn't take the whole server down.
		m.log.Warn("Follower start failed: %v", err)
		return nil
	}
	m.setHealth(true, "Running")
	return nil
}

// Stop implements server.Module. Cancels the WS loop and waits up to ctx's
// deadline for the goroutine to exit.
func (m *Module) Stop(ctx context.Context) error {
	m.stopLoop(ctx)
	m.setHealth(false, "Stopped")
	return nil
}

// Health implements server.Module.
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

func (m *Module) setHealth(healthy bool, msg string) {
	m.healthMu.Lock()
	m.healthy = healthy
	m.healthMsg = msg
	m.healthMu.Unlock()
}

// Status is a JSON-friendly snapshot exposed via the admin API so the UI can
// show real-time pairing state without reaching into module internals.
type Status struct {
	Enabled         bool      `json:"enabled"`
	Configured      bool      `json:"configured"`
	Connected       bool      `json:"connected"`
	MasterURL       string    `json:"master_url,omitempty"`
	SlaveID         string    `json:"slave_id,omitempty"`
	SlaveName       string    `json:"slave_name,omitempty"`
	LastConnectedAt time.Time `json:"last_connected_at,omitempty"`
	LastCatalogPush time.Time `json:"last_catalog_push,omitempty"`
	LastCatalogSize int       `json:"last_catalog_size,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	LastErrorAt     time.Time `json:"last_error_at,omitempty"`
}

// GetStatus returns a snapshot of the follower's live state.
func (m *Module) GetStatus() Status {
	cfg := m.config.Get().Follower
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	configured := m.hasRequiredConfig(cfg)
	return Status{
		Enabled:         configured,
		Configured:      configured,
		Connected:       m.connected,
		MasterURL:       cfg.MasterURL,
		SlaveID:         m.resolveSlaveID(cfg),
		SlaveName:       m.resolveSlaveName(cfg),
		LastConnectedAt: m.lastConnectedAt,
		LastCatalogPush: m.lastCatalogPush,
		LastCatalogSize: m.lastCatalogSize,
		LastError:       m.lastError,
		LastErrorAt:     m.lastErrorAt,
	}
}

// Reload restarts the WS loop with the current configuration. Called by the
// admin handler after the user updates pairing settings so changes take
// effect without a server restart.
func (m *Module) Reload(ctx context.Context) error {
	m.stopLoop(ctx)
	cfg := m.config.Get().Follower
	if !m.hasRequiredConfig(cfg) {
		m.setHealth(true, "Disabled — not configured (master_url + api_key required)")
		return nil
	}
	if err := m.startLoop(); err != nil {
		m.setHealth(false, fmt.Sprintf("Reload failed: %v", err))
		return err
	}
	m.setHealth(true, "Running")
	return nil
}

// hasRequiredConfig validates the minimum pairing fields. SlaveID/SlaveName are
// auto-derived from the hostname when blank, so they are not required here.
func (m *Module) hasRequiredConfig(cfg config.FollowerConfig) bool {
	if strings.TrimSpace(cfg.MasterURL) == "" {
		return false
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return false
	}
	u, err := url.Parse(cfg.MasterURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	return true
}

// startLoop spawns the WS connect-and-run loop. Caller must hold loopMu.
// Returns an error only for synchronous validation failures; runtime
// connection errors are reported asynchronously via the status fields.
func (m *Module) startLoop() error {
	m.loopMu.Lock()
	defer m.loopMu.Unlock()

	if m.cancel != nil {
		// A previous loop is still running. Caller should have invoked
		// stopLoop() first; treat double-start as a no-op rather than
		// leaking goroutines.
		return errors.New("follower loop already running")
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel stored on module, called by stopLoop
	m.cancel = cancel
	m.loopDone = make(chan struct{})

	go func() {
		defer close(m.loopDone)
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("Follower loop panicked: %v", r)
				m.recordError(fmt.Sprintf("loop panic: %v", r))
			}
		}()
		m.run(ctx)
	}()
	return nil
}

// stopLoop cancels the running loop and waits for it to exit. ctx caps the
// shutdown wait so the server's Stop sequence can't be hung by a stuck
// follower goroutine.
func (m *Module) stopLoop(ctx context.Context) {
	m.loopMu.Lock()
	cancel := m.cancel
	done := m.loopDone
	m.cancel = nil
	m.loopDone = nil
	m.loopMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done == nil {
		return
	}
	if ctx == nil {
		<-done
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
		m.log.Warn("Follower loop did not exit within shutdown deadline")
	}
}

// recordError stores the most recent failure for /admin/follower/status.
func (m *Module) recordError(msg string) {
	m.statusMu.Lock()
	m.lastError = msg
	m.lastErrorAt = time.Now()
	m.statusMu.Unlock()
}

func (m *Module) recordConnected(connected bool) {
	m.statusMu.Lock()
	m.connected = connected
	if connected {
		m.lastConnectedAt = time.Now()
	}
	m.statusMu.Unlock()
}

func (m *Module) recordCatalogPush(size int) {
	m.statusMu.Lock()
	m.lastCatalogPush = time.Now()
	m.lastCatalogSize = size
	m.statusMu.Unlock()
}

// resolveSlaveID returns the configured slave ID or, if blank, the hostname
// (so a freshly paired server identifies itself meaningfully without manual
// configuration).
func (m *Module) resolveSlaveID(cfg config.FollowerConfig) string {
	if strings.TrimSpace(cfg.SlaveID) != "" {
		return strings.TrimSpace(cfg.SlaveID)
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "follower-unknown"
	}
	return hostname
}

func (m *Module) resolveSlaveName(cfg config.FollowerConfig) string {
	if strings.TrimSpace(cfg.SlaveName) != "" {
		return strings.TrimSpace(cfg.SlaveName)
	}
	return m.resolveSlaveID(cfg)
}
