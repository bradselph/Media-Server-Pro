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
	"media-server-pro/internal/crawler"
	"media-server-pro/internal/database"
	"media-server-pro/internal/duplicates"
	"media-server-pro/internal/extractor"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/internal/playlist"
	"media-server-pro/internal/receiver"
	"media-server-pro/internal/remote"
	"media-server-pro/internal/repositories/mysql"
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
	"media-server-pro/pkg/huggingface"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
)

// Version and BuildDate are set at build time via -ldflags:
//
//	go build -ldflags "-X main.Version=4.1.0 -X main.BuildDate=2026-02-26" ./cmd/server
var (
	Version   = "0.93.0"
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

	// ── Startup security checks ────────────────────────────────────────────
	// TODO: validateSecrets only checks Receiver.APIKeys and CORS origins. It does not
	// validate other security-critical secrets such as auth session signing keys, database
	// credentials, or HuggingFace API keys. Consider expanding to cover all secrets that
	// would leave the server insecure or non-functional if missing/weak.
	validateSecrets(cfg, log)

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

	// Hugging Face client for visual classification (optional)
	if hfCfg := cfg.Get().HuggingFace; hfCfg.Enabled && hfCfg.APIKey != "" {
		timeout := 30 * time.Second
		if hfCfg.TimeoutSecs > 0 {
			timeout = time.Duration(hfCfg.TimeoutSecs) * time.Second
		}
		rateLimit := hfCfg.RateLimit
		if rateLimit <= 0 {
			rateLimit = 30
		}
		hfClient := huggingface.NewClient(huggingface.ClientConfig{
			APIKey:            hfCfg.APIKey,
			Model:             hfCfg.Model,
			EndpointURL:       hfCfg.EndpointURL,
			RequestsPerMinute: rateLimit,
			Timeout:           timeout,
			Log:               log,
		})
		scannerModule.SetHFClient(hfClient)
		log.Info("Hugging Face visual classification enabled (model: %s)", hfCfg.Model)
	}

	// Thumbnails (critical — optional BlurHash storage via metadata repo)
	// TODO: BUG — metadataRepo is created with dbModule.GORM() which returns nil here because
	// the database module's Start() has not been called yet (Start is called later by
	// srv.Start()). The GORM field is only set during Start(). This means the
	// MediaMetadataRepository is constructed with a nil *gorm.DB, and any BlurHash update
	// calls will panic or fail silently. Fix by either (a) deferring repo creation until
	// after Start(), (b) having thumbnails.NewModule accept dbModule and create the repo
	// in its own Start(), or (c) passing dbModule instead of the repo and calling GORM()
	// lazily. Other modules (security, auth, media, etc.) avoid this by accepting dbModule
	// directly and creating repositories in their Start() methods.
	metadataRepo := mysql.NewMediaMetadataRepository(dbModule.GORM())
	thumbnailsModule := thumbnails.NewModule(cfg, metadataRepo)
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
	// TODO: INCONSISTENCY — autodiscovery is the only module gated by a feature flag at
	// construction time. All other optional modules (receiver, extractor, crawler, etc.)
	// are always constructed and registered, then check their Enabled config internally
	// during Start(). This inconsistency means autodiscoveryModule is nil when the
	// feature is disabled, which is handled downstream (handlers check for nil), but
	// differs from the pattern used everywhere else. Consider aligning with the other
	// modules: always construct and register, let Start() handle the disabled state.
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

	// Duplicates (non-critical — independent duplicate detection for local and receiver media)
	duplicatesModule := duplicates.NewModule(cfg, dbModule)
	mustRegister(srv, duplicatesModule)

	// Receiver (non-critical — requires database for slave registry and media catalog)
	receiverModule := receiver.NewModule(cfg, dbModule)
	receiverModule.SetDuplicatesModule(duplicatesModule)
	mustRegister(srv, receiverModule)

	// Extractor (non-critical — requires database for item persistence)
	extractorModule := extractor.NewModule(cfg, dbModule)
	mustRegister(srv, extractorModule)

	crawlerModule := crawler.NewModule(cfg, dbModule, extractorModule)
	mustRegister(srv, crawlerModule)

	// ── Age gate middleware ────────────────────────────────────────────────
	// TODO: STALE CONFIG — ageGate is constructed once with the current config snapshot
	// via cfg.Get(). If configuration is reloaded at runtime (hot-reload), the age gate
	// will continue using the stale AgeGate config captured here. Consider passing
	// the config.Manager to AgeGate so it can read the latest config on each request,
	// or rebuild the middleware on config change.
	appCfg := cfg.Get()
	ageGate := middleware.NewAgeGate(appCfg.AgeGate)

	// ── Register background tasks ──────────────────────────────────────────
	registerTasks(tasksModule, mediaModule, scannerModule, thumbnailsModule,
		authModule, backupModule, suggestionsModule, duplicatesModule, adminModule, cfg, log)

	// ── Wire up routes ─────────────────────────────────────────────────────
	h := handlers.NewHandler(handlers.HandlerDeps{
		BuildInfo: handlers.BuildInfo{Version: Version, BuildDate: BuildDate},
		Core: handlers.HandlerCoreDeps{
			Config:    cfg,
			Media:     mediaModule,
			Streaming: streamingModule,
			HLS:       hlsModule,
			Auth:      authModule,
			Database:  dbModule,
		},
		Optional: handlers.HandlerOptionalDeps{
			Admin:         adminModule,
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
			Duplicates:    duplicatesModule,
			Analytics:     analyticsModule,
			Playlist:      playlistModule,
		},
	})

	routes.Setup(srv.Engine(), h, authModule, securityModule, cfg, ageGate)

	// Seed suggestions when the media module's initial scan completes (callback runs
	// inside the media module's goroutine so no polling or race).
	// TODO: DUPLICATION — The MediaItem-to-MediaInfo conversion logic below is
	// duplicated verbatim in the "media-scan" task's callback (see registerTasks,
	// lines ~412-427). Extract this into a shared helper function (e.g.,
	// convertMediaItemsToSuggestionInfos) to keep both call sites in sync and
	// reduce the risk of divergence when fields are added.
	mediaModule.SetOnInitialScanDone(func(items []*models.MediaItem) {
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
		if len(mediaInfos) > 0 {
			log.Info("Seeded suggestions with %d items from initial media scan", len(mediaInfos))
		}
	})

	// ── Start server (blocks until graceful shutdown) ──────────────────────
	if err := srv.Start(); err != nil {
		log.Error("Server error: %v", err)
		os.Exit(1)
	}
}

// validateSecrets checks critical configuration values and logs actionable
// warnings for any that are absent or obviously insecure. A fatal error is
// logged (and the process exits) only for conditions that would render the
// server completely insecure or non-functional.
func validateSecrets(cfg *config.Manager, log *logger.Logger) {
	appCfg := cfg.Get()

	// Receiver: API keys are the sole authentication mechanism for slave nodes.
	// An empty key list means any client can register as a slave and push a
	// media catalog to the master — no authentication at all.
	// TODO: INCOMPLETE — This validates receiver API keys are non-empty, but does not
	// check key length or entropy. Short API keys (e.g., "ab") pass this check and
	// the weak-key check below but are still trivially brute-forceable. Consider adding
	// a minimum key length check (e.g., 16+ characters).
	if appCfg.Receiver.Enabled && len(appCfg.Receiver.APIKeys) == 0 {
		log.Error("FATAL: receiver is enabled but no API keys are configured. " +
			"Set RECEIVER_API_KEYS in .env or receiver.api_keys in config.json, then restart.")
		os.Exit(1)
	}

	// Warn about known-weak receiver API key values.
	weakKeys := map[string]bool{
		"changeme": true, "secret": true, "password": true,
		"test": true, "default": true, "apikey": true, "api-key": true,
	}
	for _, key := range appCfg.Receiver.APIKeys {
		if weakKeys[key] {
			log.Warn("Receiver API key %q is a known-weak value — replace it in production", key)
		}
	}

	// CORS: wildcard origin in production allows any site to make credentialed
	// requests to the API and exfiltrate session data.
	if appCfg.Security.CORSEnabled {
		for _, origin := range appCfg.Security.CORSOrigins {
			if origin == "*" {
				log.Warn("CORS is configured with wildcard origin '*'. " +
					"This allows any website to make credentialed requests. " +
					"Restrict cors_origins to your frontend domains in production.")
			}
		}
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
const auditLogRetentionDays = 90

func registerTasks(
	scheduler *tasks.Module,
	mediaModule *media.Module,
	scannerModule *scanner.Module,
	thumbnailsModule *thumbnails.Module,
	authModule *auth.Module,
	backupModule *backup.Module,
	suggestionsModule *suggestions.Module,
	duplicatesModule *duplicates.Module,
	adminModule *admin.Module,
	cfg *config.Manager,
	log *logger.Logger,
) {
	// Media library scan — discovers new/removed files every hour
	// TODO: MISSING CANCELLATION — mediaModule.Scan() does not accept a context parameter,
	// so this task cannot be cancelled mid-scan when the server shuts down. The ctx
	// parameter is received but never passed to Scan(). If media directories are large,
	// Scan() could block shutdown for a long time. Consider adding context support to
	// media.Module.Scan() to allow graceful cancellation.
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "media-scan",
		Name:        "Media Library Scan",
		Description: "Scans configured directories for new and removed media files",
		Schedule:    1 * time.Hour,
		Func: func(ctx context.Context) error {
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
	})

	// Metadata cleanup — re-scans to prune orphaned entries every 24h
	// TODO: REDUNDANT — This task calls mediaModule.Scan() which is exactly the same
	// function called by the "media-scan" task above (every 1h). The Scan() function
	// already prunes orphaned metadata entries as part of its normal scan cycle.
	// This means every 24h a redundant duplicate scan runs. Either (a) remove this task
	// entirely if Scan() already handles cleanup, or (b) if metadata cleanup requires
	// different logic (e.g., a dedicated CleanupOrphanedMetadata method), call that
	// instead. As-is, this is a no-op duplicate of the hourly scan.
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "metadata-cleanup",
		Name:        "Metadata Cleanup",
		Description: "Removes metadata entries for media files that no longer exist on disk",
		Schedule:    24 * time.Hour,
		Func: func(ctx context.Context) error {
			return mediaModule.Scan()
		},
	})

	// Thumbnail generation — generates missing thumbnails every 30m
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "thumbnail-generation",
		Name:        "Thumbnail Generation",
		Description: "Generates missing thumbnails for media files",
		Schedule:    30 * time.Minute,
		Func: func(ctx context.Context) error {
			items := mediaModule.ListMedia(media.Filter{})
			queued := 0
			for _, item := range items {
				if ctx.Err() != nil {
					break
				}
				isAudio := item.Type == "audio"
				// For video: regenerate if ANY thumbnail (main or preview) is missing.
				// For audio: only the single waveform thumbnail matters.
				needsGen := (isAudio && !thumbnailsModule.HasThumbnail(thumbnails.MediaID(item.ID))) ||
					(!isAudio && !thumbnailsModule.HasAllPreviewThumbnails(thumbnails.MediaID(item.ID)))
				if needsGen {
					if _, err := thumbnailsModule.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: item.Path, MediaID: item.ID, IsAudio: isAudio, HighPriority: false}); err != nil {
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
	})

	// Session cleanup — removes expired sessions every hour
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "session-cleanup",
		Name:        "Session Cleanup",
		Description: "Removes expired user sessions from the database",
		Schedule:    1 * time.Hour,
		Func: func(ctx context.Context) error {
			return authModule.CleanupExpiredSessions(ctx)
		},
	})

	// Backup cleanup — removes old backups beyond configured retention every 24h
	// TODO: INCONSISTENCY — backupModule is checked for nil here, but it is always
	// non-nil because backup.NewModule() does not return an error (it's in the
	// "without error" group). The nil check is dead code. In contrast, adminModule
	// (which CAN be nil due to its error-returning constructor) is correctly checked
	// in the audit-log-cleanup task below.
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "backup-cleanup",
		Name:        "Backup Cleanup",
		Description: "Removes old backups beyond the configured retention count",
		Schedule:    24 * time.Hour,
		Func: func(ctx context.Context) error {
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
	})

	// Mature content scan — scans all media directories for mature content every 12h
	// and applies auto-flagged results to the media library so ListMedia can filter them.
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "mature-content-scan",
		Name:        "Mature Content Scan",
		Description: "Scans media directories for mature content using configured detection models",
		Schedule:    12 * time.Hour,
		Func: func(ctx context.Context) error {
			dirs := cfg.Get().Directories
			var allResults []*scanner.ScanResult

			// TODO: INCOMPLETE — This only scans Videos, Music, and Uploads directories,
			// but media files could also exist in receiver-proxied paths or other
			// configured directories. Additionally, there is no check for the scanner
			// module's own Enabled config flag (MatureScannerConfig.Enabled), so this
			// task always runs even if mature scanning is disabled in configuration.
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
			// HF visual classification is handled by the dedicated "hf-classification" task
			// (registered below) to avoid duplicate work and keep concerns separated.
			return nil
		},
	})

	// HF classification — runs visual classification on mature content that has no tags yet (e.g. every 12h)
	if scannerModule.HasHuggingFace() {
		scheduler.RegisterTask(tasks.TaskRegistration{
			ID:          "hf-classification",
			Name:        "Hugging Face Classification",
			Description: "Runs visual classification on mature content that has not been tagged yet",
			Schedule:    12 * time.Hour,
			Func: func(ctx context.Context) error {
				items := mediaModule.ListMedia(media.Filter{})
				classified := 0
				for _, item := range items {
					if ctx.Err() != nil {
						break
					}
					if !item.IsMature || len(item.Tags) > 0 {
						continue
					}
					tags, err := scannerModule.ClassifyMatureContent(ctx, item.Path)
					if err != nil {
						log.Warn("HF classification failed for %s: %v", item.Path, err)
						continue
					}
					if len(tags) > 0 {
						if err := mediaModule.UpdateTags(item.Path, tags); err != nil {
							log.Warn("Failed to update tags for %s: %v", item.Path, err)
						} else {
							classified++
						}
					}
				}
				if classified > 0 {
					log.Info("HF classification: tagged %d mature items", classified)
				}
				return nil
			},
		})
	}

	// Duplicate scan — checks local media library for fingerprint collisions every 24h
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "duplicate-scan",
		Name:        "Duplicate Media Scan",
		Description: "Scans local media library for files sharing the same content fingerprint",
		Schedule:    24 * time.Hour,
		Func: func(ctx context.Context) error {
			return duplicatesModule.ScanLocalMedia(ctx)
		},
	})

	// Audit log cleanup — removes entries older than retention (e.g. 90 days) every 24h
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "audit-log-cleanup",
		Name:        "Audit Log Cleanup",
		Description: "Removes audit log entries older than the retention period",
		Schedule:    24 * time.Hour,
		Func: func(ctx context.Context) error {
			if adminModule == nil {
				return nil
			}
			if err := adminModule.CleanupAuditLogOlderThan(ctx, auditLogRetentionDays); err != nil {
				log.Warn("Audit log cleanup failed: %v", err)
				return err
			}
			return nil
		},
	})

	// Health check — periodic diagnostics and disk space check every 5m
	// TODO: INCOMPLETE — The description says "monitors disk space" but the
	// implementation only checks whether directories exist (os.Stat) and logs
	// "dir exists" at debug level. No actual disk space is measured or reported.
	// Either implement disk space monitoring (e.g., syscall.Statfs on Linux,
	// GetDiskFreeSpaceEx on Windows) or update the description. Also, only
	// videos/data/logs directories are checked — consider also checking
	// thumbnails, hls_cache, and uploads directories which can grow large.
	// Additionally, the Music directory is not checked.
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "health-check",
		Name:        "Health Check",
		Description: "Performs system diagnostics and monitors disk space",
		Schedule:    5 * time.Minute,
		Func: func(ctx context.Context) error {
			appCfg := cfg.Get()
			dirs := appCfg.Directories
			for name, dir := range map[string]string{"videos": dirs.Videos, "data": dirs.Data, "logs": dirs.Logs} {
				if dir == "" {
					continue
				}
				var stat os.FileInfo
				if s, err := os.Stat(dir); err != nil {
					log.Warn("Health check: %s path %q not accessible: %v", name, dir, err)
				} else {
					stat = s
				}
				if stat != nil && stat.IsDir() {
					// Log disk usage for monitoring (e.g. Prometheus or log aggregation)
					log.Debug("Health check: %s dir %q exists", name, dir)
				}
			}
			log.Debug("Periodic health check complete")
			return nil
		},
	})
}
