// Package database provides MySQL database connectivity and migration management using GORM.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	driverMysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
	"media-server-pro/pkg/models"
)

// Module handles database connections and migrations
type Module struct {
	config    *config.Manager
	log       *logger.Logger
	db        *gorm.DB
	sqlDB     *sql.DB
	healthy   bool
	healthMsg string
	healthMu  sync.RWMutex
}

// NewModule creates a new database module instance
func NewModule(cfg *config.Manager) *Module {
	return &Module{
		config: cfg,
		log:    logger.New("database"),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "database"
}

// Start initializes the database connection and runs migrations
func (m *Module) Start(ctx context.Context) error {
	m.log.Info("Starting database module with GORM...")

	cfg := m.config.Get()
	if !cfg.Database.Enabled {
		return fmt.Errorf("database is required but disabled in configuration - set DATABASE_ENABLED=true")
	}

	// Build DSN using mysql.Config.FormatDSN() so passwords containing special
	// characters (@, :, /, etc.) are properly URL-encoded. Raw fmt.Sprintf DSN
	// strings break when the password contains these characters.
	m.log.Info("Database config: Host=%s, Port=%d, Name=%s, User=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.Name, cfg.Database.Username)

	dsnCfg := driverMysql.NewConfig() // sets AllowNativePasswords=true, Loc=UTC by default
	dsnCfg.User = cfg.Database.Username
	dsnCfg.Passwd = cfg.Database.Password
	dsnCfg.Net = "tcp"
	dsnCfg.Addr = fmt.Sprintf("%s:%d", cfg.Database.Host, cfg.Database.Port)
	dsnCfg.DBName = cfg.Database.Name
	dsnCfg.Params = map[string]string{"charset": "utf8mb4"}
	dsnCfg.ParseTime = true
	dsnCfg.Timeout = cfg.Database.Timeout
	// TLSMode: "" / "false" = no TLS; "skip-verify" = TLS without cert check;
	// "true" = TLS with system CA. Required for many hosted/remote database providers.
	// Note: driverMysql.NewConfig() defaults TLSConfig to "preferred", which
	// causes failures when the server does not support TLS at all. We must
	// explicitly set "false" when the user has not requested TLS.
	if cfg.Database.TLSMode != "" && cfg.Database.TLSMode != "false" {
		dsnCfg.TLSConfig = cfg.Database.TLSMode
	} else {
		dsnCfg.TLSConfig = "false"
	}
	dsn := dsnCfg.FormatDSN()

	// Log DSN without password
	safeDSN := fmt.Sprintf("%s:***@tcp(%s:%d)/%s",
		cfg.Database.Username, cfg.Database.Host, cfg.Database.Port, cfg.Database.Name)
	m.log.Info("Connecting to: %s", safeDSN)

	// Configure GORM logger
	gormLog := gormlogger.New(
		&gormLogWriter{log: m.log},
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	// Connect with retry logic
	var db *gorm.DB
	var err error
	for i := 0; i < cfg.Database.MaxRetries; i++ {
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger: gormLog,
		})
		if err == nil {
			// Get underlying *sql.DB to configure connection pool
			m.sqlDB, err = db.DB()
			if err == nil {
				// Test connection
				ctxTimeout, cancel := context.WithTimeout(ctx, cfg.Database.Timeout)
				err = m.sqlDB.PingContext(ctxTimeout)
				cancel()
				if err == nil {
					break
				}
			}
		}
		m.log.Warn("Database connection attempt %d/%d failed: %v", i+1, cfg.Database.MaxRetries, err)
		if i < cfg.Database.MaxRetries-1 {
			time.Sleep(cfg.Database.RetryInterval)
		}
	}

	if err != nil {
		m.log.Error("Failed to connect to database: %v", err)
		m.healthMu.Lock()
		m.healthy = false
		m.healthMsg = fmt.Sprintf("Connection failed: %v", err)
		m.healthMu.Unlock()
		return fmt.Errorf("database connection failed: %w", err)
	}

	m.db = db

	// Configure connection pool
	m.sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	m.sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	m.sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	m.log.Info("Database connected: %s:%d/%s", cfg.Database.Host, cfg.Database.Port, cfg.Database.Name)

	// Ensure schema is complete — creates tables that don't exist and adds
	// any missing columns. Idempotent and safe on every startup.
	if err := m.ensureSchema(ctx); err != nil {
		return fmt.Errorf("schema setup failed: %w", err)
	}

	m.healthMu.Lock()
	m.healthy = true
	m.healthMsg = "Connected"
	m.healthMu.Unlock()

	m.log.Info("Database module started successfully")
	return nil
}

// gormLogWriter adapts our logger to GORM's logger interface
type gormLogWriter struct {
	log *logger.Logger
}

func (w *gormLogWriter) Printf(format string, args ...interface{}) {
	w.log.Info(format, args...)
}

// Stop closes the database connection
func (m *Module) Stop(ctx context.Context) error {
	m.log.Info("Stopping database module...")

	if m.sqlDB != nil {
		if err := m.sqlDB.Close(); err != nil {
			m.log.Error("Failed to close database: %v", err)
			return err
		}
	}

	m.healthMu.Lock()
	m.healthy = false
	m.healthMsg = "Stopped"
	m.healthMu.Unlock()

	return nil
}

// Health returns the current health status of the database module
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()

	status := models.StatusHealthy
	if !m.healthy {
		status = models.StatusUnhealthy
	}

	return models.HealthStatus{
		Name:      m.Name(),
		Status:    status,
		Message:   m.healthMsg,
		CheckedAt: time.Now(),
	}
}

// DB returns the underlying *sql.DB connection for use with database/sql callers
// (e.g. migrations, health checks). All repository implementations now use GORM().
func (m *Module) DB() *sql.DB {
	return m.sqlDB
}

// GORM returns the GORM database instance
func (m *Module) GORM() *gorm.DB {
	return m.db
}

// IsConnected returns true if the database is connected and healthy
func (m *Module) IsConnected() bool {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return m.healthy
}
