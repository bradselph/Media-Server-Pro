// Package testutil provides shared helpers for unit and integration tests.
// Import this package in test files (not production code) to get common fixtures.
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"media-server-pro/internal/auth"
	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/media"
	"media-server-pro/internal/playlist"
	"media-server-pro/internal/security"
	"media-server-pro/internal/streaming"
	"media-server-pro/pkg/models"
)

// TestConfig creates a config Manager backed by a temp file.
// No real config file needs to exist on disk beforehand.
func TestConfig(t *testing.T) *config.Manager {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	return config.NewManager(cfgPath)
}

// TestDBModule creates and starts a database module using environment variables.
// It skips the test when the required env vars are missing or the database
// is unreachable. The module is stopped automatically when the test ends.
//
// Environment variables:
//
//	TEST_DB_HOST (default "127.0.0.1")
//	TEST_DB_PORT (default "3306")
//	TEST_DB_USER (required)
//	TEST_DB_PASS (default "")
//	TEST_DB_NAME (required)
func TestDBModule(t *testing.T, cfg *config.Manager) *database.Module {
	t.Helper()

	user := os.Getenv("TEST_DB_USER")
	name := os.Getenv("TEST_DB_NAME")
	if user == "" || name == "" {
		t.Skip("skipping: TEST_DB_USER and TEST_DB_NAME env vars required for database tests")
	}

	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	portStr := os.Getenv("TEST_DB_PORT")
	if portStr == "" {
		portStr = "3306"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("testutil.TestDBModule: invalid TEST_DB_PORT %q: %v", portStr, err)
	}
	pass := os.Getenv("TEST_DB_PASS")

	if err := cfg.Update(func(c *config.Config) {
		c.Database.Enabled = true
		c.Database.Host = host
		c.Database.Port = port
		c.Database.Username = user
		c.Database.Password = pass
		c.Database.Name = name
	}); err != nil {
		t.Fatalf("testutil.TestDBModule: config update failed: %v", err)
	}

	dbModule := database.NewModule(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := dbModule.Start(ctx); err != nil {
		t.Skipf("skipping: database unavailable: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = dbModule.Stop(stopCtx)
	})

	return dbModule
}

// TestEnv bundles the modules most commonly needed for integration tests.
type TestEnv struct {
	Config    *config.Manager
	DB        *database.Module
	Auth      *auth.Module
	Media     *media.Module
	Streaming *streaming.Module
	HLS       *hls.Module
	Playlist  *playlist.Module
	Security  *security.Module
}

// NewTestEnv creates a fully wired TestEnv with all core modules started.
// It skips the test if the database is unavailable.
// All modules are stopped in reverse order when the test ends.
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	cfg := TestConfig(t)
	dbModule := TestDBModule(t, cfg)

	// Point directories at a temp dir so modules don't touch real paths.
	tmpDir := t.TempDir()
	if err := cfg.Update(func(c *config.Config) {
		c.Directories.Videos = filepath.Join(tmpDir, "videos")
		c.Directories.Music = filepath.Join(tmpDir, "music")
		c.Directories.Data = filepath.Join(tmpDir, "data")
		c.Directories.Thumbnails = filepath.Join(tmpDir, "thumbnails")
		c.Directories.HLSCache = filepath.Join(tmpDir, "hls_cache")
		c.Directories.Playlists = filepath.Join(tmpDir, "playlists")
		c.Directories.Uploads = filepath.Join(tmpDir, "uploads")
		c.Directories.Temp = filepath.Join(tmpDir, "temp")
		c.Directories.Logs = filepath.Join(tmpDir, "logs")
		c.Directories.Analytics = filepath.Join(tmpDir, "analytics")
	}); err != nil {
		t.Fatalf("testutil.NewTestEnv: config update failed: %v", err)
	}

	// Create the temp subdirectories so modules that check them don't fail.
	for _, sub := range []string{"videos", "music", "data", "thumbnails", "hls_cache", "playlists", "uploads", "temp", "logs", "analytics"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, sub), 0o755); err != nil {
			t.Fatalf("testutil.NewTestEnv: mkdir %s: %v", sub, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Security module (non-critical, no error return).
	securityModule := security.NewModule(cfg, dbModule)
	if err := securityModule.Start(ctx); err != nil {
		t.Fatalf("testutil.NewTestEnv: security start: %v", err)
	}
	t.Cleanup(func() {
		c, cl := context.WithTimeout(context.Background(), 5*time.Second)
		defer cl()
		_ = securityModule.Stop(c)
	})

	// Auth module (critical, returns error).
	authModule, err := auth.NewModule(cfg, dbModule)
	if err != nil {
		t.Fatalf("testutil.NewTestEnv: auth new: %v", err)
	}
	if err := authModule.Start(ctx); err != nil {
		t.Fatalf("testutil.NewTestEnv: auth start: %v", err)
	}
	t.Cleanup(func() {
		c, cl := context.WithTimeout(context.Background(), 5*time.Second)
		defer cl()
		_ = authModule.Stop(c)
	})

	// Media module (critical, returns error).
	mediaModule, err := media.NewModule(cfg, dbModule)
	if err != nil {
		t.Fatalf("testutil.NewTestEnv: media new: %v", err)
	}
	if err := mediaModule.Start(ctx); err != nil {
		t.Fatalf("testutil.NewTestEnv: media start: %v", err)
	}
	t.Cleanup(func() {
		c, cl := context.WithTimeout(context.Background(), 5*time.Second)
		defer cl()
		_ = mediaModule.Stop(c)
	})

	// Streaming module (no DB, no error return).
	streamingModule := streaming.NewModule(cfg)
	if err := streamingModule.Start(ctx); err != nil {
		t.Fatalf("testutil.NewTestEnv: streaming start: %v", err)
	}
	t.Cleanup(func() {
		c, cl := context.WithTimeout(context.Background(), 5*time.Second)
		defer cl()
		_ = streamingModule.Stop(c)
	})

	// HLS module (no error return).
	hlsModule := hls.NewModule(cfg, dbModule)
	if err := hlsModule.Start(ctx); err != nil {
		t.Fatalf("testutil.NewTestEnv: hls start: %v", err)
	}
	t.Cleanup(func() {
		c, cl := context.WithTimeout(context.Background(), 5*time.Second)
		defer cl()
		_ = hlsModule.Stop(c)
	})

	// Playlist module (critical, returns error).
	playlistModule, err := playlist.NewModule(cfg, dbModule)
	if err != nil {
		t.Fatalf("testutil.NewTestEnv: playlist new: %v", err)
	}
	if err := playlistModule.Start(ctx); err != nil {
		t.Fatalf("testutil.NewTestEnv: playlist start: %v", err)
	}
	t.Cleanup(func() {
		c, cl := context.WithTimeout(context.Background(), 5*time.Second)
		defer cl()
		_ = playlistModule.Stop(c)
	})

	return &TestEnv{
		Config:    cfg,
		DB:        dbModule,
		Auth:      authModule,
		Media:     mediaModule,
		Streaming: streamingModule,
		HLS:       hlsModule,
		Playlist:  playlistModule,
		Security:  securityModule,
	}
}

// CreateTestUser creates a user via the auth module and returns the User model.
// The test fails immediately if user creation fails.
func (te *TestEnv) CreateTestUser(t *testing.T, username, password string) *models.User {
	t.Helper()
	user, err := te.Auth.CreateUser(context.Background(), auth.CreateUserParams{
		Username: username,
		Password: password,
		Email:    username + "@test.local",
		UserType: "standard",
		Role:     models.RoleViewer,
	})
	if err != nil {
		t.Fatalf("testutil.CreateTestUser(%s): %v", username, err)
	}
	return user
}

// CreateTestAdmin creates an admin user via the auth module and returns the User model.
func (te *TestEnv) CreateTestAdmin(t *testing.T, username, password string) *models.User {
	t.Helper()
	user, err := te.Auth.CreateUser(context.Background(), auth.CreateUserParams{
		Username: username,
		Password: password,
		Email:    username + "@test.local",
		UserType: "admin",
		Role:     models.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("testutil.CreateTestAdmin(%s): %v", username, err)
	}
	return user
}

// LoginUser authenticates a user and returns the session ID.
// The test fails immediately if authentication fails.
func (te *TestEnv) LoginUser(t *testing.T, username, password string) string {
	t.Helper()
	session, err := te.Auth.Authenticate(context.Background(), &auth.AuthRequest{
		Username:  username,
		Password:  password,
		IPAddress: "127.0.0.1",
		UserAgent: "testutil",
	})
	if err != nil {
		t.Fatalf("testutil.LoginUser(%s): %v", username, err)
	}
	return session.ID
}
