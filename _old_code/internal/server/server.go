// Package server provides the core HTTP server with modular component management.
// It handles graceful startup, shutdown, and component health monitoring.
package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
)

const (
	logSeparator      = "=============================================="
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
)

// Module represents a server component that can be started and stopped
type Module interface {
	// Name returns the module name
	Name() string
	// Start initializes and starts the module
	Start(ctx context.Context) error
	// Stop gracefully stops the module
	Stop(ctx context.Context) error
	// Health returns the module health status
	Health() models.HealthStatus
}

// Server is the main media server
type Server struct {
	config       *config.Manager
	log          *logger.Logger
	httpServer   *http.Server
	router       *mux.Router
	modules      []Module
	modulesMap   map[string]Module
	healthReport *logger.HealthReporter
	startTime    time.Time
	mu           sync.RWMutex
	running      bool
	shutdownCh   chan struct{}
	version      string
}

// CriticalModules lists modules whose startup failure should prevent server start.
// Critical modules are essential for server operation - if they fail to start, the server shuts down.
// Exported for use by main.go during module registration.
var CriticalModules = map[string]bool{
	"database":   true, // MySQL database (required for all persistence)
	"auth":       true, // User and admin authentication
	"security":   true, // Rate limiting, CORS, CSP, IP filtering
	"media":      true, // Media library management and metadata
	"streaming":  true, // File streaming with range support
	"tasks":      true, // Background task scheduler
	"scanner":    true, // Mature content detection
	"thumbnails": true, // Thumbnail generation
	// NOTE: "hls" is intentionally non-critical. When ffmpeg is unavailable,
	// the server starts normally and the frontend falls back to direct streaming.
}

// Options configures the server
type Options struct {
	ConfigPath string
	LogLevel   logger.Level
	Version    string
}

// New creates a new server instance
func New(opts Options) (*Server, error) {
	// Initialize logger - first with defaults (before config is loaded)
	logCfg := logger.DefaultConfig()
	logCfg.MinLevel = opts.LogLevel
	logCfg.ModuleName = "server"
	if err := logger.Init(logCfg); err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}
	log := logger.New("server")

	log.Info(logSeparator)
	log.Info("    Media Server Pro 3 (Go Edition)")
	log.Info(logSeparator)
	log.Info("Initializing server...")

	// Load configuration
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath = "config.json"
	}
	cfgMgr := config.NewManager(cfgPath)
	if err := cfgMgr.Load(); err != nil {
		log.Warn("Failed to load config, using defaults: %v", err)
	}

	// Validate configuration
	if errs := cfgMgr.Validate(); len(errs) > 0 {
		for _, err := range errs {
			log.Error("Configuration error: %v", err)
		}
		return nil, fmt.Errorf("configuration validation failed with %d errors", len(errs))
	}

	// Now that config is loaded, re-initialize logger with config-based file logging.
	// The logger.Init sync.Once has already run, so we apply file logging settings directly.
	appCfg := cfgMgr.Get()

	// Apply JSON format setting if configured (LOG_FORMAT=json or config.json logging.format=json)
	if appCfg.Logging.Format == "json" {
		logger.SetJSONFormat(true)
		log.Info("JSON log format enabled")
	}

	if appCfg.Logging.FileEnabled {
		logDir := appCfg.Directories.Logs
		if logDir == "" {
			logDir = "logs"
		}
		if err := logger.EnableFileLogging(logDir, appCfg.Logging.MaxFileSize, appCfg.Logging.MaxBackups); err != nil {
			log.Warn("Failed to enable file logging: %v", err)
		} else {
			log.Info("File logging enabled: directory=%s, max_size=%dMB, max_backups=%d",
				logDir, appCfg.Logging.MaxFileSize/(1024*1024), appCfg.Logging.MaxBackups)
		}
	}

	// Create necessary directories
	if err := cfgMgr.CreateDirectories(); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	version := opts.Version
	if version == "" {
		version = "3.0.0"
	}

	s := &Server{
		config:       cfgMgr,
		log:          log,
		router:       mux.NewRouter().UseEncodedPath(),
		modules:      make([]Module, 0),
		modulesMap:   make(map[string]Module),
		healthReport: logger.NewHealthReporter(),
		shutdownCh:   make(chan struct{}),
		version:      version,
	}

	// Setup router and middleware
	s.setupRouter()

	return s, nil
}

// setupRouter configures the HTTP router with middleware
func (s *Server) setupRouter() {
	cfg := s.config.Get()

	// Create middleware chain
	var middlewares []middleware.Middleware

	// Request ID (always first)
	middlewares = append(middlewares, middleware.RequestID())

	// Recovery middleware
	middlewares = append(middlewares, middleware.Recovery(s.log))

	// Logging middleware
	middlewares = append(middlewares, middleware.Logger(s.log))

	// CORS middleware
	if cfg.Security.CORSEnabled {
		middlewares = append(middlewares, middleware.CORS(
			cfg.Security.CORSOrigins,
			[]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			[]string{headerContentType, "Authorization", "X-Requested-With"},
		))
	}

	// Security headers
	if cfg.Security.CSPEnabled || cfg.Security.HSTSEnabled {
		var hstsMaxAge int
		if cfg.Security.HSTSEnabled {
			hstsMaxAge = cfg.Security.HSTSMaxAge
		}
		var cspPolicy string
		if cfg.Security.CSPEnabled {
			cspPolicy = cfg.Security.CSPPolicy
		}
		middlewares = append(middlewares, middleware.SecurityHeaders(cspPolicy, hstsMaxAge))
	}

	// IP filtering
	if cfg.Security.EnableIPWhitelist || cfg.Security.EnableIPBlacklist {
		ipFilter := middleware.NewIPFilter(
			cfg.Security.IPWhitelist,
			cfg.Security.IPBlacklist,
			s.log,
		)
		middlewares = append(middlewares, ipFilter.Middleware())
	}

	// Rate limiting is handled by the security module's Middleware() in api/routes/routes.go
	// No need for duplicate rate limiter here

	// Apply middleware chain
	chain := middleware.Chain(middlewares...)
	s.router.Use(func(next http.Handler) http.Handler {
		return chain(next)
	})

	// Setup base routes
	s.setupBaseRoutes()
}

// setupBaseRoutes sets up core routes that are always available
func (s *Server) setupBaseRoutes() {
	// Health check endpoint
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")

	// System status endpoint
	s.router.HandleFunc("/api/status", s.handleStatus).Methods("GET")

	// Module health endpoints
	s.router.HandleFunc("/api/modules", s.handleModules).Methods("GET")
	s.router.HandleFunc("/api/modules/{name}/health", s.handleModuleHealth).Methods("GET")
}

// RegisterModule adds a module to the server
func (s *Server) RegisterModule(module Module) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := module.Name()
	if _, exists := s.modulesMap[name]; exists {
		return fmt.Errorf("module already registered: %s", name)
	}

	s.modules = append(s.modules, module)
	s.modulesMap[name] = module
	s.log.Info("Registered module: %s", name)

	return nil
}

// GetModule returns a module by name
func (s *Server) GetModule(name string) (Module, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.modulesMap[name]
	return m, ok
}

// Router returns the server's router for adding routes
func (s *Server) Router() *mux.Router {
	return s.router
}

// Config returns the configuration manager
func (s *Server) Config() *config.Manager {
	return s.config
}

// Start starts the server and all registered modules
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("server already running")
	}
	s.running = true
	s.startTime = time.Now()
	s.mu.Unlock()

	ctx := context.Background()
	cfg := s.config.Get()

	// Start all modules with timeout; critical module failures are fatal
	s.log.Info("Starting %d modules...", len(s.modules))
	for _, module := range s.modules {
		s.log.Info("Starting module: %s", module.Name())
		startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
		err := module.Start(startCtx)
		startCancel()

		if err != nil {
			s.healthReport.Report(module.Name(), false, err, "Failed to start")
			s.log.Error("Failed to start module %s: %v", module.Name(), err)

			if CriticalModules[module.Name()] {
				return fmt.Errorf("critical module %s failed to start: %w", module.Name(), err)
			}
			s.log.Warn("Module %s will be unavailable", module.Name())
		} else {
			s.healthReport.Report(module.Name(), true, nil, "Started successfully")
			s.log.Info("Module %s started successfully", module.Name())
		}
	}

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	s.httpServer = &http.Server{
		Addr:           addr,
		Handler:        s.router,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		IdleTimeout:    cfg.Server.IdleTimeout,
		MaxHeaderBytes: cfg.Server.MaxHeaderBytes,
	}

	// Setup signal handling for graceful shutdown
	go s.handleSignals()

	// Start HTTP server
	s.log.Info(logSeparator)
	if cfg.Server.EnableHTTPS {
		s.log.Info("  Starting HTTPS server on https://%s", addr)
		s.log.Info(logSeparator)
		return s.startHTTPS()
	}
	s.log.Info("  Starting HTTP server on http://%s", addr)
	s.log.Info(logSeparator)
	return s.startHTTP()
}

// startWatchdog sends periodic WATCHDOG=1 pings to systemd so that the service
// is killed and restarted if the process deadlocks or becomes unresponsive.
// The interval is half the WatchdogSec value systemd passes in WATCHDOG_USEC.
// This goroutine exits when ctx is cancelled (i.e., on graceful shutdown).
func (s *Server) startWatchdog(ctx context.Context) {
	watchdogUSec := os.Getenv("WATCHDOG_USEC")
	if watchdogUSec == "" {
		return // not running under systemd watchdog
	}
	var usec int64
	if _, err := fmt.Sscanf(watchdogUSec, "%d", &usec); err != nil || usec <= 0 {
		return
	}
	interval := time.Duration(usec/2) * time.Microsecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := helpers.SDNotify("WATCHDOG=1"); err != nil {
				s.log.Warn("sd_notify watchdog: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) startHTTP() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("HTTP listen error: %w", err)
	}
	// Port is bound — signal systemd that we are ready to handle requests.
	ctx, cancel := context.WithCancel(context.Background())
	go s.startWatchdog(ctx)
	status := "STATUS=Serving HTTP on " + s.httpServer.Addr
	_ = helpers.SDNotify("READY=1\n" + status)
	defer func() {
		cancel()
		_ = helpers.SDNotify("STOPPING=1")
	}()
	if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server error: %w", err)
	}
	return nil
}

func (s *Server) startHTTPS() error {
	cfg := s.config.Get()

	// Validate cert and key paths before attempting to start the TLS listener,
	// providing clear error messages instead of the cryptic ones from ListenAndServeTLS.
	if cfg.Server.CertFile == "" {
		return fmt.Errorf("HTTPS enabled but SERVER_CERT_FILE is not configured")
	}
	if cfg.Server.KeyFile == "" {
		return fmt.Errorf("HTTPS enabled but SERVER_KEY_FILE is not configured")
	}
	if _, err := os.Stat(cfg.Server.CertFile); err != nil {
		return fmt.Errorf("TLS certificate file not found or not readable (%s): %w", cfg.Server.CertFile, err)
	}
	if _, err := os.Stat(cfg.Server.KeyFile); err != nil {
		return fmt.Errorf("TLS key file not found or not readable (%s): %w", cfg.Server.KeyFile, err)
	}
	// Verify the cert/key pair is valid before binding to the port
	if _, err := tls.LoadX509KeyPair(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil {
		return fmt.Errorf("TLS certificate/key pair is invalid: %w", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		MinVersion:       tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
	s.httpServer.TLSConfig = tlsConfig

	ln, err := tls.Listen("tcp", s.httpServer.Addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("HTTPS listen error: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go s.startWatchdog(ctx)
	status := "STATUS=Serving HTTPS on " + s.httpServer.Addr
	_ = helpers.SDNotify("READY=1\n" + status)
	defer func() {
		cancel()
		_ = helpers.SDNotify("STOPPING=1")
	}()
	if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTPS server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	s.log.Info(logSeparator)
	s.log.Info("  Shutting down server...")
	s.log.Info(logSeparator)

	cfg := s.config.Get()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	// Stop HTTP server first
	s.log.Info("Stopping HTTP server...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.log.Error("HTTP server shutdown error: %v", err)
	}

	// Stop all modules in reverse order
	s.log.Info("Stopping modules...")
	for i := len(s.modules) - 1; i >= 0; i-- {
		module := s.modules[i]
		s.log.Info("Stopping module: %s", module.Name())
		if err := module.Stop(ctx); err != nil {
			s.log.Error("Failed to stop module %s: %v", module.Name(), err)
		} else {
			s.log.Info("Module %s stopped", module.Name())
		}
	}

	// Save configuration with retry logic (graceful degradation on failure)
	// Retry up to 3 times with 100ms delay to handle temporary filesystem issues
	saved := false
	for i := 0; i < 3; i++ {
		if err := s.config.Save(); err != nil {
			s.log.Warn("Failed to save configuration (attempt %d/3): %v", i+1, err)
			if i < 2 {
				time.Sleep(100 * time.Millisecond)
			}
		} else {
			saved = true
			break
		}
	}
	if !saved {
		s.log.Error("Configuration save failed after 3 attempts - changes may be lost on restart")
	}

	s.log.Info("Server shutdown complete")

	// Flush and close log files to ensure all shutdown logs are persisted
	logger.Shutdown()

	close(s.shutdownCh)
}

// Wait blocks until the server is shut down
func (s *Server) Wait() {
	<-s.shutdownCh
}

// handleHealth returns overall server health
func (s *Server) handleHealth(w http.ResponseWriter, _r *http.Request) {
	health := s.GetHealth()

	w.Header().Set(headerContentType, contentTypeJSON)
	if health.Healthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	writeJSON(w, health)
}

// handleStatus returns server status
func (s *Server) handleStatus(w http.ResponseWriter, _r *http.Request) {
	s.mu.RLock()
	running := s.running
	startTime := s.startTime
	s.mu.RUnlock()

	status := map[string]interface{}{
		"running":      running,
		"uptime":       time.Since(startTime).String(),
		"start_time":   startTime,
		"version":      s.version,
		"go_version":   runtime.Version(),
		"module_count": len(s.modules),
	}

	w.Header().Set(headerContentType, contentTypeJSON)
	writeJSON(w, status)
}

// handleModules returns list of registered modules
func (s *Server) handleModules(w http.ResponseWriter, _r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	modules := make([]map[string]interface{}, 0, len(s.modules))
	for _, m := range s.modules {
		health := m.Health()
		modules = append(modules, map[string]interface{}{
			"name":    m.Name(),
			"status":  health.Status,
			"message": health.Message,
		})
	}

	w.Header().Set(headerContentType, contentTypeJSON)
	writeJSON(w, models.APIResponse{
		Success: true,
		Data:    modules,
	})
}

// handleModuleHealth returns health of a specific module
func (s *Server) handleModuleHealth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	module, ok := s.GetModule(name)
	if !ok {
		w.Header().Set(headerContentType, contentTypeJSON)
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, models.APIResponse{
			Success: false,
			Error:   "Module not found",
		})
		return
	}

	health := module.Health()
	w.Header().Set(headerContentType, contentTypeJSON)
	writeJSON(w, models.APIResponse{
		Success: true,
		Data:    health,
	})
}

// GetHealth returns overall system health
func (s *Server) GetHealth() models.SystemHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make([]models.HealthStatus, 0, len(s.modules))
	healthy := true

	for _, m := range s.modules {
		status := m.Health()
		statuses = append(statuses, status)
		if status.Status != models.StatusHealthy {
			healthy = false
		}
	}

	return models.SystemHealth{
		Healthy:    healthy,
		Components: statuses,
		CheckedAt:  time.Now(),
	}
}

// writeJSON writes a JSON response.
// Uses buffered encoding to ensure atomic writes - either the full response succeeds or an error is returned.
func writeJSON(w http.ResponseWriter, data interface{}) {
	// Buffer the JSON encoding to avoid partial writes
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		log := logger.New("server")
		log.Error("JSON encode failed: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Encoding succeeded - write atomically to response
	w.Header().Set(headerContentType, contentTypeJSON)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log := logger.New("server")
		log.Error("Failed to write JSON response: %v", err)
	}
}
