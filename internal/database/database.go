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

	m.log.Info("Database config: Host=%s, Port=%d, Name=%s, User=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.Name, cfg.Database.Username)

	dsn := buildDSN(cfg.Database)
	m.log.Info("Connecting to: %s", safeDSNString(cfg.Database))

	db, sqlDB, err := connectWithRetry(ctx, dsn, cfg.Database, m.log)
	if err != nil {
		m.log.Error("Failed to connect to database: %v", err)
		m.setHealth(false, fmt.Sprintf("Connection failed: %v", err))
		return fmt.Errorf("database connection failed: %w", err)
	}

	m.db = db
	m.sqlDB = sqlDB
	configurePool(m.sqlDB, cfg.Database)

	m.log.Info("Database connected: %s:%d/%s", cfg.Database.Host, cfg.Database.Port, cfg.Database.Name)

	if err := m.ensureSchema(ctx); err != nil {
		return fmt.Errorf("schema setup failed: %w", err)
	}

	m.setHealth(true, "Connected")
	m.log.Info("Database module started successfully")
	return nil
}

// buildDSN builds a MySQL DSN from config. Uses FormatDSN so passwords with
// special characters (@, :, /) are properly URL-encoded.
func buildDSN(db config.DatabaseConfig) string {
	dsnCfg := driverMysql.NewConfig()
	dsnCfg.User = db.Username
	dsnCfg.Passwd = db.Password
	dsnCfg.Net = "tcp"
	dsnCfg.Addr = fmt.Sprintf("%s:%d", db.Host, db.Port)
	dsnCfg.DBName = db.Name
	dsnCfg.Params = map[string]string{"charset": "utf8mb4"}
	dsnCfg.ParseTime = true
	dsnCfg.Timeout = db.Timeout
	if db.TLSMode != "" && db.TLSMode != "false" {
		dsnCfg.TLSConfig = db.TLSMode
	} else {
		dsnCfg.TLSConfig = "false"
	}
	return dsnCfg.FormatDSN()
}

// safeDSNString returns a log-safe DSN (password redacted).
func safeDSNString(db config.DatabaseConfig) string {
	return fmt.Sprintf("%s:***@tcp(%s:%d)/%s", db.Username, db.Host, db.Port, db.Name)
}

// newGORMLogger creates a GORM logger that writes to the module logger (Error level).
func newGORMLogger(log *logger.Logger) gormlogger.Interface {
	return gormlogger.New(
		&gormLogWriter{log: log},
		gormlogger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  gormlogger.Error,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

// tryConnect opens GORM and pings once; returns (db, sqlDB, nil) on success.
func tryConnect(ctx context.Context, dsn string, gormLog gormlogger.Interface, timeout time.Duration) (*gorm.DB, *sql.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormLog})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	err = sqlDB.PingContext(ctxTimeout)
	cancel()
	if err != nil {
		return nil, nil, err
	}
	return db, sqlDB, nil
}

// connectWithRetry opens a GORM connection with retries and ping; returns gorm.DB, sql.DB, and error.
// TODO: Bug — the retry loop uses time.Sleep which blocks the goroutine and ignores the
// context. If the parent context is cancelled (e.g., during shutdown), the retries will
// continue sleeping for the full retry duration instead of aborting immediately. Should
// use a select on ctx.Done() and a timer instead of time.Sleep.
func connectWithRetry(ctx context.Context, dsn string, dbCfg config.DatabaseConfig, log *logger.Logger) (*gorm.DB, *sql.DB, error) {
	gormLog := newGORMLogger(log)
	var lastErr error
	for i := 0; i < dbCfg.MaxRetries; i++ {
		db, sqlDB, err := tryConnect(ctx, dsn, gormLog, dbCfg.Timeout)
		if err == nil {
			return db, sqlDB, nil
		}
		lastErr = err
		log.Warn("Database connection attempt %d/%d failed: %v", i+1, dbCfg.MaxRetries, err)
		if i < dbCfg.MaxRetries-1 {
			time.Sleep(dbCfg.RetryInterval)
		}
	}
	return nil, nil, lastErr
}

// configurePool sets connection pool limits on the underlying sql.DB.
func configurePool(sqlDB *sql.DB, dbCfg config.DatabaseConfig) {
	sqlDB.SetMaxOpenConns(dbCfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(dbCfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(dbCfg.ConnMaxLifetime)
}

// setHealth updates healthy and healthMsg under healthMu.
func (m *Module) setHealth(healthy bool, msg string) {
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	m.healthy = healthy
	m.healthMsg = msg
}

// gormLogWriter adapts our logger to GORM's logger interface
type gormLogWriter struct {
	log *logger.Logger
}

func (w *gormLogWriter) Printf(format string, args ...interface{}) {
	w.log.Info(format, args...)
}

// Stop closes the database connection
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping database module...")

	if m.sqlDB != nil {
		if err := m.sqlDB.Close(); err != nil {
			m.log.Error("Failed to close database: %v", err)
			return err
		}
		m.sqlDB = nil
		m.db = nil
	}

	m.setHealth(false, "Stopped")
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
