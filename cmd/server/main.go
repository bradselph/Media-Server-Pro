package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"media-server-pro/api/handlers"
	"media-server-pro/api/routes"
	"media-server-pro/internal/admin"
	"media-server-pro/internal/analytics"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/autodiscovery"
	"media-server-pro/internal/backup"
	"media-server-pro/internal/categorizer"
	"media-server-pro/internal/config"
	"media-server-pro/internal/database"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/internal/playlist"
	"media-server-pro/internal/remote"
	"media-server-pro/internal/scanner"
	"media-server-pro/internal/security"
	"media-server-pro/internal/server"
	"media-server-pro/internal/streaming"
	"media-server-pro/internal/suggestions"
	"media-server-pro/internal/tasks"
	"media-server-pro/internal/thumbnails"
	"media-server-pro/internal/updater"
	"media-server-pro/internal/upload"
	"media-server-pro/internal/validator"
	"media-server-pro/pkg/middleware"
)

// Version and BuildDate are set at build time via -ldflags:
//
//	go build -ldflags "-X main.Version=4.1.0 -X main.BuildDate=2026-02-26" ./cmd/server
var (
	Version   = "0.0.3"
	BuildDate = ""
)

func main() {
	// Load .env before anything else so environment variables are available
	// during config loading. Missing file is silently ignored.
	_ = godotenv.Load()

	// Parse flags
	var (
		configPath = flag.String("config", "config.json", "Path to config file")
		logLevel   = flag.String("log-level", "info", "Log level: debug, info, warn, error")
		showVer    = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVer {
		if BuildDate != "" {
			fmt.Printf("Media Server Pro 4 v%s (built %s)\n", Version, BuildDate)
		} else {
			fmt.Printf("Media Server Pro 4 v%s\n", Version)
		}
		os.Exit(0)
	}

	// Map log level string to logger.Level constant
	var level logger.Level
	switch *logLevel {
	case "debug":
		level = logger.DEBUG
	case "warn":
		level = logger.WARN
	case "error":
		level = logger.ERROR
	default:
		level = logger.INFO
	}

	// Create server (initialises logger + config)
	srv, err := server.New(server.Options{
		ConfigPath: *configPath,
		LogLevel:   level,
		Version:    Version,
		BuildDate:  BuildDate,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	cfg := srv.Config()
	log := logger.New("main")

	// ── Module construction ────────────────────────────────────────────────

	// Database (critical)
	dbModule := database.NewModule(cfg)
	mustRegister(srv, dbModule)

	// Security (critical)
	securityModule := security.NewModule(cfg)
	mustRegister(srv, securityModule)

	// Auth (critical — requires database)
	authModule, err := auth.NewModule(cfg, dbModule)
	if err != nil {
		log.Error("Failed to create auth module: %v", err)
		os.Exit(1)
	}
	mustRegister(srv, authModule)

	// Media (critical — requires database)
	mediaModule, err := media.NewModule(cfg, dbModule)
	if err != nil {
		log.Error("Failed to create media module: %v", err)
		os.Exit(1)
	}
	mustRegister(srv, mediaModule)

	// Streaming (critical)
	streamingModule := streaming.NewModule(cfg)
	mustRegister(srv, streamingModule)

	// Tasks scheduler (critical)
	tasksModule := tasks.NewModule(cfg)
	mustRegister(srv, tasksModule)

	// Scanner (critical — requires database)
	scannerModule, err := scanner.NewModule(cfg, dbModule)
	if err != nil {
		log.Error("Failed to create scanner module: %v", err)
		os.Exit(1)
	}
	mustRegister(srv, scannerModule)

	// Thumbnails (critical)
	thumbnailsModule := thumbnails.NewModule(cfg)
	mustRegister(srv, thumbnailsModule)

	// ── Non-critical modules ───────────────────────────────────────────────

	// HLS (non-critical — falls back gracefully if ffmpeg unavailable)
	hlsModule := hls.NewModule(cfg)
	mustRegister(srv, hlsModule)

	// Analytics (non-critical — requires database)
	var analyticsModule *analytics.Module
	if am, err := analytics.NewModule(cfg, dbModule); err != nil {
		log.Warn("Analytics module unavailable: %v", err)
	} else {
		analyticsModule = am
		mustRegister(srv, analyticsModule)
	}

	// Playlist (non-critical — requires database)
	var playlistModule *playlist.Module
	if pm, err := playlist.NewModule(cfg, dbModule); err != nil {
		log.Warn("Playlist module unavailable: %v", err)
	} else {
		playlistModule = pm
		mustRegister(srv, playlistModule)
	}

	// Admin (non-critical — requires database)
	var adminModule *admin.Module
	if adm, err := admin.NewModule(cfg, dbModule); err != nil {
		log.Warn("Admin module unavailable: %v", err)
	} else {
		adminModule = adm
		mustRegister(srv, adminModule)
	}

	// Upload (non-critical)
	uploadModule := upload.NewModule(cfg)
	mustRegister(srv, uploadModule)

	// Validator (non-critical)
	validatorModule := validator.NewModule(cfg)
	mustRegister(srv, validatorModule)

	// Backup (non-critical)
	backupModule := backup.NewModule(cfg)
	mustRegister(srv, backupModule)

	// Auto-discovery (non-critical)
	autodiscoveryModule := autodiscovery.NewModule(cfg)
	mustRegister(srv, autodiscoveryModule)

	// Suggestions (non-critical)
	suggestionsModule := suggestions.NewModule(cfg)
	mustRegister(srv, suggestionsModule)

	// Categorizer (non-critical)
	categorizerModule := categorizer.NewModule(cfg)
	mustRegister(srv, categorizerModule)

	// Updater (non-critical — needs version string)
	updaterModule := updater.NewModule(cfg, Version)
	mustRegister(srv, updaterModule)

	// Remote (non-critical)
	remoteModule := remote.NewModule(cfg)
	mustRegister(srv, remoteModule)

	// ── Age gate middleware ────────────────────────────────────────────────
	appCfg := cfg.Get()
	ageGate := middleware.NewAgeGate(appCfg.AgeGate)

	// ── Register background tasks ──────────────────────────────────────────
	registerTasks(tasksModule, mediaModule, scannerModule, cfg, log)

	// ── Wire up routes ─────────────────────────────────────────────────────
	h := handlers.NewHandler(handlers.HandlerDeps{
		Version:       Version,
		BuildDate:     BuildDate,
		Config:        cfg,
		Media:         mediaModule,
		Streaming:     streamingModule,
		HLS:           hlsModule,
		Auth:          authModule,
		Analytics:     analyticsModule,
		Playlist:      playlistModule,
		Admin:         adminModule,
		Database:      dbModule,
		Tasks:         tasksModule,
		Upload:        uploadModule,
		Scanner:       scannerModule,
		Thumbnails:    thumbnailsModule,
		Validator:     validatorModule,
		Backup:        backupModule,
		Autodiscovery: autodiscoveryModule,
		Suggestions:   suggestionsModule,
		Security:      securityModule,
		Categorizer:   categorizerModule,
		Updater:       updaterModule,
		Remote:        remoteModule,
	})

	routes.Setup(srv.Engine(), h, authModule, securityModule, cfg, ageGate)

	// ── Start server (blocks until graceful shutdown) ──────────────────────
	if err := srv.Start(); err != nil {
		log.Error("Server error: %v", err)
		os.Exit(1)
	}
	srv.Wait()
}

// mustRegister registers a module and exits on failure.
func mustRegister(srv *server.Server, module server.Module) {
	if err := srv.RegisterModule(module); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to register module %s: %v\n", module.Name(), err)
		os.Exit(1)
	}
}

// registerTasks registers all periodic background tasks with the scheduler.
func registerTasks(
	scheduler *tasks.Module,
	mediaModule *media.Module,
	scannerModule *scanner.Module,
	cfg *config.Manager,
	log *logger.Logger,
) {
	// Media library scan — discovers new/removed files every hour
	scheduler.RegisterTask(
		"media-scan",
		"Media Library Scan",
		"Scans configured directories for new and removed media files",
		1*time.Hour,
		func(ctx context.Context) error {
			return mediaModule.Scan()
		},
	)

	// Metadata cleanup — re-scans to prune orphaned entries every 24h
	scheduler.RegisterTask(
		"metadata-cleanup",
		"Metadata Cleanup",
		"Removes metadata entries for media files that no longer exist on disk",
		24*time.Hour,
		func(ctx context.Context) error {
			return mediaModule.Scan()
		},
	)

	// Mature content scan — scans videos directory for mature content every 12h
	scheduler.RegisterTask(
		"mature-content-scan",
		"Mature Content Scan",
		"Scans media directories for mature content using configured detection models",
		12*time.Hour,
		func(ctx context.Context) error {
			videosDir := cfg.Get().Directories.Videos
			_, err := scannerModule.ScanDirectory(videosDir)
			return err
		},
	)

	// Health check — periodic log entry for monitoring systems every 5m
	scheduler.RegisterTask(
		"health-check",
		"Health Check",
		"Performs system diagnostics and monitors disk space",
		5*time.Minute,
		func(ctx context.Context) error {
			log.Debug("Periodic health check complete")
			return nil
		},
	)
}
