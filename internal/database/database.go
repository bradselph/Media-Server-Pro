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
	dbMu      sync.RWMutex // protects db and sqlDB against concurrent access during Stop
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

	// Create the database if it does not exist yet (first-run / fresh deployment).
	if err := ensureDatabase(ctx, cfg.Database, m.log); err != nil {
		m.log.Error("Failed to ensure database exists: %v", err)
		m.setHealth(false, fmt.Sprintf("Failed to create database: %v", err))
		return fmt.Errorf("database setup failed: %w", err)
	}

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
	dsnCfg.Params = map[string]string{"charset": "utf8mb4", "multiStatements": "false"}
	dsnCfg.ParseTime = true
	dsnCfg.Timeout = db.Timeout
	if db.TLSMode != "" && db.TLSMode != "false" {
		dsnCfg.TLSConfig = db.TLSMode
	} else {
		dsnCfg.TLSConfig = "false"
	}
	return dsnCfg.FormatDSN()
}

// ensureDatabase connects to MySQL without specifying a database and runs
// CREATE DATABASE IF NOT EXISTS so a fresh deployment works without manual DB provisioning.
// MySQL grants ALL PRIVILEGES ON `db`.* to a user allow that user to create that specific
// database even when it does not yet exist, so no root credentials are required.
// If the CREATE DATABASE command fails (e.g. insufficient privileges), a clear error is
// returned so the operator knows to create the database manually.
func ensureDatabase(ctx context.Context, dbCfg config.DatabaseConfig, log *logger.Logger) error {
	noCfg := driverMysql.NewConfig()
	noCfg.User = dbCfg.Username
	noCfg.Passwd = dbCfg.Password
	noCfg.Net = "tcp"
	noCfg.Addr = fmt.Sprintf("%s:%d", dbCfg.Host, dbCfg.Port)
	noCfg.ParseTime = true
	noCfg.Timeout = dbCfg.Timeout
	if dbCfg.TLSMode != "" && dbCfg.TLSMode != "false" {
		noCfg.TLSConfig = dbCfg.TLSMode
	} else {
		noCfg.TLSConfig = "false"
	}

	connector, err := driverMysql.NewConnector(noCfg)
	if err != nil {
		return fmt.Errorf("build connector: %w", err)
	}
	rawDB := sql.OpenDB(connector)
	defer func() { _ = rawDB.Close() }()

	ctxTimeout, cancel := context.WithTimeout(ctx, dbCfg.Timeout)
	defer cancel()

	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbCfg.Name)
	if _, err := rawDB.ExecContext(ctxTimeout, query); err != nil {
		return fmt.Errorf(
			"cannot create database %q (Error: %w). "+
				"Create it manually: CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; "+
				"then GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'; FLUSH PRIVILEGES;",
			dbCfg.Name, err, dbCfg.Name, dbCfg.Name, dbCfg.Username,
		)
	}
	log.Info("Database %q ready (created if not exists)", dbCfg.Name)
	return nil
}

// safeDSNString returns a log-safe DSN (password redacted).
func safeDSNString(db config.DatabaseConfig) string {
	return fmt.Sprintf("%s:***@tcp(%s:%d)/%s", db.Username, db.Host, db.Port, db.Name)
}

// newGORMLogger creates a GORM logger that writes to the module logger (Error level).
// slowThreshold is sourced from DatabaseConfig.SlowQueryThreshold; 0 disables slow-query logging.
func newGORMLogger(log *logger.Logger, slowThreshold time.Duration) gormlogger.Interface {
	if slowThreshold <= 0 {
		slowThreshold = 500 * time.Millisecond
	}
	return gormlogger.New(
		&gormLogWriter{log: log},
		gormlogger.Config{
			SlowThreshold:             slowThreshold,
			LogLevel:                  gormlogger.Error,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

// tryConnect opens GORM and pings once; returns (db, sqlDB, nil) on success.
func tryConnect(ctx context.Context, dsn string, gormLog gormlogger.Interface, timeout time.Duration) (db *gorm.DB, sqlDB *sql.DB, err error) {
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormLog})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err = db.DB()
	if err != nil {
		return nil, nil, err
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	err = sqlDB.PingContext(ctxTimeout)
	cancel()
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return db, sqlDB, nil
}

// connectWithRetry opens a GORM connection with retries and ping; returns gorm.DB, sql.DB, and error.
func connectWithRetry(ctx context.Context, dsn string, dbCfg config.DatabaseConfig, log *logger.Logger) (db *gorm.DB, sqlDB *sql.DB, err error) {
	gormLog := newGORMLogger(log, dbCfg.SlowQueryThreshold)
	maxRetries := dbCfg.MaxRetries
	if maxRetries < 1 {
		maxRetries = 1
	}
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		db, sqlDB, err := tryConnect(ctx, dsn, gormLog, dbCfg.Timeout)
		if err == nil {
			return db, sqlDB, nil
		}
		lastErr = err
		log.Warn("Database connection attempt %d/%d failed: %v", i+1, maxRetries, err)
		if i < maxRetries-1 {
			timer := time.NewTimer(dbCfg.RetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-timer.C:
			}
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

func (w *gormLogWriter) Printf(format string, args ...any) {
	w.log.Info(format, args...)
}

// Stop closes the database connection
// Stop closes the database connection.
func (m *Module) Stop(_ context.Context) error {
	m.log.Info("Stopping database module...")

	m.dbMu.Lock()
	sqlDB := m.sqlDB
	m.sqlDB = nil
	m.db = nil
	m.dbMu.Unlock()

	if sqlDB != nil {
		if err := sqlDB.Close(); err != nil {
			m.log.Error("Failed to close database: %v", err)
			return err
		}
	}

	m.setHealth(false, "Stopped")
	return nil
}

// Health returns the current health status of the database module.
// Performs a live ping to detect connection loss between health checks.
func (m *Module) Health() models.HealthStatus {
	m.healthMu.RLock()
	healthy := m.healthy
	msg := m.healthMsg
	m.healthMu.RUnlock()

	// Live-ping the database if we think we're healthy, to detect silent disconnects.
	m.dbMu.RLock()
	sqlDB := m.sqlDB
	m.dbMu.RUnlock()
	if healthy && sqlDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			healthy = false
			msg = fmt.Sprintf("Ping failed: %v", err)
			m.setHealth(false, msg)
		}
	}

	status := models.StatusHealthy
	if !healthy {
		status = models.StatusUnhealthy
	}

	return models.HealthStatus{
		Name:      m.Name(),
		Status:    status,
		Message:   msg,
		CheckedAt: time.Now(),
	}
}

// DB returns the underlying *sql.DB connection for use with database/sql callers
// (e.g. migrations, health checks). All repository implementations now use GORM().
func (m *Module) DB() *sql.DB {
	m.dbMu.RLock()
	defer m.dbMu.RUnlock()
	return m.sqlDB
}

// GORM returns the GORM database instance
func (m *Module) GORM() *gorm.DB {
	m.dbMu.RLock()
	defer m.dbMu.RUnlock()
	return m.db
}

// IsConnected returns true if the database is connected and healthy
func (m *Module) IsConnected() bool {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	return m.healthy
}
