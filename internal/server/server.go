// Package server provides the core HTTP server with modular component management.
// It handles graceful startup, shutdown, and component health monitoring.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/helpers"
	"media-server-pro/pkg/models"
)

const logSeparator = "=============================================="

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
	engine       *gin.Engine
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
	BuildDate  string
}

// initServerLogger initializes the base logger before configuration is loaded.
func initServerLogger(opts Options) (*logger.Logger, error) {
	logCfg := logger.DefaultConfig()
	logCfg.MinLevel = opts.LogLevel
	logCfg.ModuleName = "server"
	if err := logger.Init(logCfg); err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}
	return logger.New("server"), nil
}

// loadConfigManager loads the configuration file and returns its manager.
// It falls back to defaults when loading fails.
func loadConfigManager(log *logger.Logger, configPath string) *config.Manager {
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = "config.json"
	}
	cfgMgr := config.NewManager(cfgPath)
	if err := cfgMgr.Load(); err != nil {
		log.Warn("Failed to load config, using defaults: %v", err)
	}
	return cfgMgr
}

// configureLoggingFromConfig applies logging-related settings from the loaded configuration.
func configureLoggingFromConfig(log *logger.Logger, cfgMgr *config.Manager) {
	appCfg := cfgMgr.Get()

	if appCfg.Logging.Format == "json" {
		logger.SetJSONFormat(true)
	}

	if !appCfg.Logging.FileEnabled {
		return
	}

	logDir := appCfg.Directories.Logs
	if logDir == "" {
		logDir = "logs"
	}

	maxSize := appCfg.Logging.MaxFileSize
	if !appCfg.Logging.FileRotation {
		maxSize = 0
	}

	if err := logger.EnableFileLogging(logDir, maxSize, appCfg.Logging.MaxBackups); err != nil {
		log.Warn("Failed to enable file logging: %v", err)
		return
	}

	log.Info("File logging enabled: directory=%s, max_size=%dMB, max_backups=%d, rotation=%v",
		logDir, appCfg.Logging.MaxFileSize/(1024*1024), appCfg.Logging.MaxBackups, appCfg.Logging.FileRotation)
}

// validateAndPrepareConfig validates the configuration and ensures required directories exist.
func validateAndPrepareConfig(cfgMgr *config.Manager, log *logger.Logger) error {
	if errs := cfgMgr.Validate(); len(errs) > 0 {
		for _, err := range errs {
			log.Error("Configuration error: %v", err)
		}
		return fmt.Errorf("configuration validation failed with %d errors", len(errs))
	}

	if err := cfgMgr.CreateDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	return nil
}

// createTLSConfig validates certificate/key files and constructs a TLS configuration.
func createTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	if certFile == "" {
		return nil, fmt.Errorf("HTTPS enabled but SERVER_CERT_FILE is not configured")
	}
	if keyFile == "" {
		return nil, fmt.Errorf("HTTPS enabled but SERVER_KEY_FILE is not configured")
	}
	if _, err := os.Stat(certFile); err != nil {
		return nil, fmt.Errorf("TLS certificate file not found or not readable (%s): %w", certFile, err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		return nil, fmt.Errorf("TLS key file not found or not readable (%s): %w", keyFile, err)
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate/key pair is invalid: %w", err)
	}

	return &tls.Config{
		Certificates:     []tls.Certificate{cert},
		MinVersion:       tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}, nil
}

// New creates a new server instance
func New(opts Options) (*Server, error) {
	log, err := initServerLogger(opts)
	if err != nil {
		return nil, err
	}

	log.Info(logSeparator)
	log.Info("    Media Server Pro")
	log.Info(logSeparator)
	log.Info("Initializing server...")

	cfgMgr := loadConfigManager(log, opts.ConfigPath)
	configureLoggingFromConfig(log, cfgMgr)
	if err := validateAndPrepareConfig(cfgMgr, log); err != nil {
		return nil, err
	}

	version := opts.Version
	if version == "" {
		version = "4.0.0"
	}

	// Set Gin to release mode before creating the engine to suppress debug output
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// Attach Gin's built-in logger and recovery middleware
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	s := &Server{
		config:       cfgMgr,
		log:          log,
		engine:       engine,
		modules:      make([]Module, 0),
		modulesMap:   make(map[string]Module),
		healthReport: logger.NewHealthReporter(),
		shutdownCh:   make(chan struct{}),
		version:      version,
	}

	// Setup router middleware and base routes
	s.setupRouter()

	return s, nil
}

// setupRouter configures the Gin engine base routes.
// CORS and security headers are applied by api/routes/routes.go via the middleware package.
func (s *Server) setupRouter() {
	s.setupBaseRoutes()
}

// setupBaseRoutes registers core routes that are always available.
// /api/status and /api/modules are registered by routes.Setup with adminAuth.
func (s *Server) setupBaseRoutes() {
	// Status/modules routes are protected in routes.Setup
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

// Engine returns the server's Gin engine for adding routes
func (s *Server) Engine() *gin.Engine {
	return s.engine
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

	// Start all modules with timeout; critical module failures are fatal.
	// On critical failure, stop already-started modules to avoid leaking connections/goroutines.
	s.log.Info("Starting %d modules...", len(s.modules))
	var started []Module
	for _, module := range s.modules {
		s.log.Info("Starting module: %s", module.Name())
		startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
		err := module.Start(startCtx)
		startCancel()

		if err != nil {
			s.healthReport.Report(module.Name(), false, err, "Failed to start")
			s.log.Error("Failed to start module %s: %v", module.Name(), err)

			if CriticalModules[module.Name()] {
				s.log.Info("Stopping %d already-started modules...", len(started))
				for i := len(started) - 1; i >= 0; i-- {
					stopCtx, stopCancel := context.WithTimeout(ctx, 15*time.Second)
					if stopErr := started[i].Stop(stopCtx); stopErr != nil {
						s.log.Warn("Failed to stop module %s during rollback: %v", started[i].Name(), stopErr)
					}
					stopCancel()
				}
				return fmt.Errorf("critical module %s failed to start: %w", module.Name(), err)
			}
			s.log.Warn("Module %s will be unavailable", module.Name())
		} else {
			started = append(started, module)
			s.healthReport.Report(module.Name(), true, nil, "Started successfully")
			s.log.Info("Module %s started successfully", module.Name())
		}
	}

	// Create HTTP server — gin.Engine implements http.Handler directly
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	s.httpServer = &http.Server{
		Addr:           addr,
		Handler:        s.engine,
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

	tlsConfig, err := createTLSConfig(cfg.Server.CertFile, cfg.Server.KeyFile)
	if err != nil {
		return err
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

	s.shutdownHTTPServer(ctx)
	s.shutdownModules(ctx)
	s.saveConfigWithRetry()

	s.log.Info("Server shutdown complete")

	// Flush and close log files to ensure all shutdown logs are persisted
	logger.Shutdown()

	close(s.shutdownCh)
}

func (s *Server) shutdownHTTPServer(ctx context.Context) {
	s.log.Info("Stopping HTTP server...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.log.Error("HTTP server shutdown error: %v", err)
	}
}

func (s *Server) shutdownModules(ctx context.Context) {
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
}

func (s *Server) saveConfigWithRetry() {
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
}

// Wait blocks until the server is shut down
func (s *Server) Wait() {
	<-s.shutdownCh
}

// HandleStatus returns server status. Used by routes.Setup with adminAuth.
func (s *Server) HandleStatus(c *gin.Context) {
	s.mu.RLock()
	running := s.running
	startTime := s.startTime
	moduleCount := len(s.modules)
	s.mu.RUnlock()

	c.JSON(http.StatusOK, map[string]interface{}{
		"running":      running,
		"uptime":       time.Since(startTime).String(),
		"start_time":   startTime,
		"version":      s.version,
		"go_version":   runtime.Version(),
		"module_count": moduleCount,
	})
}

// HandleModules returns the list of registered modules. Used by routes.Setup with adminAuth.
func (s *Server) HandleModules(c *gin.Context) {
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
	c.JSON(http.StatusOK, models.APIResponse{Success: true, Data: modules})
}

// HandleModuleHealth returns the health of a specific module. Used by routes.Setup with adminAuth.
func (s *Server) HandleModuleHealth(c *gin.Context) {
	name := c.Param("name")
	module, ok := s.GetModule(name)
	if !ok {
		c.JSON(http.StatusNotFound, models.APIResponse{
			Success: false,
			Error:   "Module not found",
		})
		return
	}
	health := module.Health()
	c.JSON(http.StatusOK, models.APIResponse{Success: true, Data: health})
}

