package main

import (
	"context"
	"errors"
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
	"media-server-pro/internal/crawler"
	"media-server-pro/internal/extractor"
	"media-server-pro/internal/receiver"
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
	Version   = "0.54.0"
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
			fmt.Printf("Media Server Pro v%s (built %s)\n", Version, BuildDate)
		} else {
			fmt.Printf("Media Server Pro v%s\n", Version)
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

	// Security (critical — requires database for IP list persistence)
	securityModule := security.NewModule(cfg, dbModule)
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
	hlsModule := hls.NewModule(cfg, dbModule)
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

	// Validator (non-critical — requires database for validation results)
	validatorModule := validator.NewModule(cfg, dbModule)
	mustRegister(srv, validatorModule)

	// Backup (non-critical — requires database for manifest storage)
	backupModule := backup.NewModule(cfg, dbModule)
	mustRegister(srv, backupModule)

	// Auto-discovery (non-critical — requires database for suggestion persistence)
	var autodiscoveryModule *autodiscovery.Module
	if cfg.Get().Features.EnableAutoDiscovery {
		autodiscoveryModule = autodiscovery.NewModule(cfg, dbModule)
		mustRegister(srv, autodiscoveryModule)
	}

	// Suggestions (non-critical — requires database for user profiles)
	suggestionsModule := suggestions.NewModule(cfg, dbModule)
	mustRegister(srv, suggestionsModule)

	// Categorizer (non-critical — requires database for categorization data)
	categorizerModule := categorizer.NewModule(cfg, dbModule)
	mustRegister(srv, categorizerModule)

	// Updater (non-critical — needs version string)
	updaterModule := updater.NewModule(cfg, Version)
	mustRegister(srv, updaterModule)

	// Remote (non-critical — requires database for cache index)
	remoteModule := remote.NewModule(cfg, dbModule)
	mustRegister(srv, remoteModule)

	// Receiver (non-critical — requires database for slave registry and media catalog)
	receiverModule := receiver.NewModule(cfg, dbModule)
	mustRegister(srv, receiverModule)

	// Extractor (non-critical — requires database for item persistence)
	extractorModule := extractor.NewModule(cfg, dbModule)
	mustRegister(srv, extractorModule)

	crawlerModule := crawler.NewModule(cfg, dbModule, extractorModule)
	mustRegister(srv, crawlerModule)

	// ── Age gate middleware ────────────────────────────────────────────────
	appCfg := cfg.Get()
	ageGate := middleware.NewAgeGate(appCfg.AgeGate)

	// ── Register background tasks ──────────────────────────────────────────
	registerTasks(tasksModule, mediaModule, scannerModule, thumbnailsModule,
		authModule, backupModule, suggestionsModule, cfg, log)

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
		Receiver:      receiverModule,
		Extractor:     extractorModule,
		Crawler:       crawlerModule,
	})

	routes.Setup(srv.Engine(), h, authModule, securityModule, cfg, ageGate)

	// Seed suggestions module immediately after the media module's initial scan
	// completes, so suggestions are available without waiting for the first hourly
	// task to fire (which has a 45-second startup delay + scan time).
	// This goroutine must be launched before srv.Start() which blocks until shutdown.
	go func() {
		for !mediaModule.IsReady() {
			time.Sleep(500 * time.Millisecond)
		}
		items := mediaModule.ListMedia(media.Filter{})
		mediaInfos := make([]*suggestions.MediaInfo, 0, len(items))
		for _, item := range items {
			mediaInfos = append(mediaInfos, &suggestions.MediaInfo{
				Path:      item.Path,
				StableID:  item.ID,
				Title:     item.Name,
				Category:  item.Category,
				MediaType: string(item.Type),
				Tags:      item.Tags,
				Views:     item.Views,
				AddedAt:   item.DateAdded,
				IsMature:  item.IsMature,
			})
		}
		if len(mediaInfos) > 0 {
			suggestionsModule.UpdateMediaData(mediaInfos)
			log.Info("Seeded suggestions with %d items from initial media scan", len(mediaInfos))
		}
	}()

	// ── Start server (blocks until graceful shutdown) ──────────────────────
	if err := srv.Start(); err != nil {
		log.Error("Server error: %v", err)
		os.Exit(1)
	}
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
	thumbnailsModule *thumbnails.Module,
	authModule *auth.Module,
	backupModule *backup.Module,
	suggestionsModule *suggestions.Module,
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
			if err := mediaModule.Scan(); err != nil {
				return err
			}
			// Feed updated media catalog to suggestions module
			if suggestionsModule != nil {
				items := mediaModule.ListMedia(media.Filter{})
				mediaInfos := make([]*suggestions.MediaInfo, 0, len(items))
				for _, item := range items {
					mediaInfos = append(mediaInfos, &suggestions.MediaInfo{
						Path:      item.Path,
						StableID:  item.ID,
						Title:     item.Name,
						Category:  item.Category,
						MediaType: string(item.Type),
						Tags:      item.Tags,
						Views:     item.Views,
						AddedAt:   item.DateAdded,
						IsMature:  item.IsMature,
					})
				}
				suggestionsModule.UpdateMediaData(mediaInfos)
			}
			return nil
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

	// Thumbnail generation — generates missing thumbnails every 30m
	scheduler.RegisterTask(
		"thumbnail-generation",
		"Thumbnail Generation",
		"Generates missing thumbnails for media files",
		30*time.Minute,
		func(ctx context.Context) error {
			items := mediaModule.ListMedia(media.Filter{})
			queued := 0
			for _, item := range items {
				if ctx.Err() != nil {
					break
				}
				if !thumbnailsModule.HasThumbnail(item.ID) {
					isAudio := item.Type == "audio"
					if _, err := thumbnailsModule.GenerateThumbnail(item.Path, item.ID, isAudio); err != nil {
						if !errors.Is(err, thumbnails.ErrThumbnailPending) {
							log.Debug("Thumbnail generation skipped for %s: %v", item.Name, err)
						}
					} else {
						queued++
					}
				}
			}
			if queued > 0 {
				log.Info("Queued %d thumbnail generation jobs", queued)
			}
			return nil
		},
	)

	// Session cleanup — removes expired sessions every hour
	scheduler.RegisterTask(
		"session-cleanup",
		"Session Cleanup",
		"Removes expired user sessions from the database",
		1*time.Hour,
		func(ctx context.Context) error {
			return authModule.CleanupExpiredSessions(ctx)
		},
	)

	// Backup cleanup — removes old backups beyond configured retention every 24h
	scheduler.RegisterTask(
		"backup-cleanup",
		"Backup Cleanup",
		"Removes old backups beyond the configured retention count",
		24*time.Hour,
		func(ctx context.Context) error {
			if backupModule == nil {
				return nil
			}
			keepCount := cfg.Get().Backup.RetentionCount
			if keepCount <= 0 {
				keepCount = 10
			}
			removed, err := backupModule.CleanOldBackups(keepCount)
			if removed > 0 {
				log.Info("Cleaned %d old backups (keeping %d)", removed, keepCount)
			}
			return err
		},
	)

	// Mature content scan — scans all media directories for mature content every 12h
	// and applies auto-flagged results to the media library so ListMedia can filter them.
	scheduler.RegisterTask(
		"mature-content-scan",
		"Mature Content Scan",
		"Scans media directories for mature content using configured detection models",
		12*time.Hour,
		func(ctx context.Context) error {
			dirs := cfg.Get().Directories
			var allResults []*scanner.ScanResult

			for _, dir := range []string{dirs.Videos, dirs.Music, dirs.Uploads} {
				if dir == "" {
					continue
				}
				results, err := scannerModule.ScanDirectory(dir)
				if err != nil {
					log.Error("Mature scan failed for %s: %v", dir, err)
					continue
				}
				allResults = append(allResults, results...)
			}

			// Apply auto-flagged results to the media library
			applied := 0
			for _, result := range allResults {
				if result.AutoFlagged && result.IsMature {
					if err := mediaModule.SetMatureFlag(result.Path, true, result.Confidence, result.Reasons); err != nil {
						log.Error("Failed to set mature flag for %s: %v", result.Path, err)
					} else {
						applied++
					}
				}
			}
			if applied > 0 {
				log.Info("Mature scan complete: %d scanned, %d flagged", len(allResults), applied)
			}
			return nil
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
