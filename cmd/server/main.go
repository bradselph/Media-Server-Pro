package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"media-server-pro/api/handlers"
	"media-server-pro/api/routes"
	"media-server-pro/internal/admin"
	"media-server-pro/internal/analytics"
	"media-server-pro/internal/auth"
	"media-server-pro/internal/autodiscovery"
	"media-server-pro/internal/backup"
	"media-server-pro/internal/categorizer"
	"media-server-pro/internal/claude"
	"media-server-pro/internal/config"
	"media-server-pro/internal/crawler"
	"media-server-pro/internal/database"
	"media-server-pro/internal/downloader"
	"media-server-pro/internal/duplicates"
	"media-server-pro/internal/extractor"
	"media-server-pro/internal/hls"
	"media-server-pro/internal/logger"
	"media-server-pro/internal/media"
	"media-server-pro/internal/playlist"
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
	"media-server-pro/pkg/huggingface"
	"media-server-pro/pkg/middleware"
	"media-server-pro/pkg/models"
	"media-server-pro/pkg/storage"
	"media-server-pro/pkg/storage/local"
	"media-server-pro/pkg/storage/s3compat"
)

// Version and BuildDate are set at build time via -ldflags.
// deploy.sh reads the VERSION file; CI workflows set them automatically.
//
//	go build -ldflags "-X main.Version=$(cat VERSION) -X main.BuildDate=$(date +%Y-%m-%d)" ./cmd/server
var (
	Version   = "1.6.1"
	BuildDate = "dev"
)

// fatalExit logs an error, flushes the logger, and exits with code 1.
func fatalExit(log *logger.Logger, format string, args ...interface{}) {
	log.Error(format, args...)
	logger.Shutdown()
	os.Exit(1)
}

func main() {
	configPath, logLevel, showVer := parseFlags()

	if showVer {
		showVersion()
		os.Exit(0)
	}

	// Create server (initializes logger + config)
	srv, err := server.New(server.Options{
		ConfigPath: configPath,
		LogLevel:   mapLogLevel(logLevel),
		Version:    Version,
		BuildDate:  BuildDate,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	cfg := srv.Config()
	log := logger.New("main")

	// ── Storage backends ───────────────────────────────────────────────────
	stores := initStorage(cfg, log)

	// ── Startup security checks ────────────────────────────────────────────
	validateSecrets(cfg, log)

	// ── Module construction ────────────────────────────────────────────────
	mods := initModules(srv, cfg, log, stores)

	// ── Age gate middleware ────────────────────────────────────────────────
	ageGate := setupAgeGate(cfg, mods.analytics)

	// ── Register background tasks ──────────────────────────────────────────
	registerTasks(mods.tasks, mods.media, mods.scanner, mods.thumbnails,
		mods.auth, mods.backup, mods.suggestions, mods.duplicates, mods.admin, mods.hls, cfg, log)

	// ── Wire up routes ─────────────────────────────────────────────────────
	setupRoutes(srv, cfg, mods, ageGate)

	// Seed suggestions when the media module's initial scan completes
	wireSuggestionsSeeding(mods.media, mods.suggestions, log)

	// ── Start server (blocks until graceful shutdown) ──────────────────────
	if err := srv.Start(); err != nil {
		fatalExit(log, "Server error: %v", err)
	}
}

func parseFlags() (string, string, bool) {
	var (
		configPath = flag.String("config", "config.json", "Path to config file")
		logLevel   = flag.String("log-level", "info", "Log level: debug, info, warn, error")
		showVer    = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()
	return *configPath, *logLevel, *showVer
}

func showVersion() {
	versionStr := fmt.Sprintf("Media Server Pro v%s", Version)
	versionStr += fmt.Sprintf(" (built %s)", BuildDate)
	fmt.Println(versionStr)
}

func mapLogLevel(levelStr string) logger.Level {
	switch levelStr {
	case "debug":
		return logger.DEBUG
	case "warn":
		return logger.WARN
	case "error":
		return logger.ERROR
	default:
		return logger.INFO
	}
}

type storageBackends struct {
	videos     storage.Backend
	music      storage.Backend
	thumbnails storage.Backend
	uploads    storage.Backend
	hlsCache   storage.Backend
}

func initStorage(cfg *config.Manager, log *logger.Logger) storageBackends {
	storageFactory := &storage.BackendFactory{
		Config: storage.StorageConfig{
			Backend: cfg.Get().Storage.Backend,
			S3: storage.S3Config{
				Endpoint:        cfg.Get().Storage.S3.Endpoint,
				Region:          cfg.Get().Storage.S3.Region,
				AccessKeyID:     cfg.Get().Storage.S3.AccessKeyID,
				SecretAccessKey: cfg.Get().Storage.S3.SecretAccessKey,
				Bucket:          cfg.Get().Storage.S3.Bucket,
				UsePathStyle:    cfg.Get().Storage.S3.UsePathStyle,
				Prefixes:        cfg.Get().Storage.S3.Prefixes,
			},
		},
		NewLocal: func(root string) (storage.Backend, error) {
			return local.New(root)
		},
		NewS3: func(ctx context.Context, endpoint, region, keyID, secret, bucket, prefix string, pathStyle bool) (storage.Backend, error) {
			return s3compat.New(ctx, s3compat.Config{
				Endpoint:        endpoint,
				Region:          region,
				AccessKeyID:     keyID,
				SecretAccessKey: secret,
				Bucket:          bucket,
				Prefix:          prefix,
				UsePathStyle:    pathStyle,
			})
		},
	}

	// Use a 30-second timeout for storage backend init. Without a deadline,
	// an unreachable S3/B2 endpoint causes the server to hang forever on startup.
	initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dirs := cfg.Get().Directories

	videoStore, err := storageFactory.NewBackend(initCtx, "videos", dirs.Videos)
	if err != nil {
		fatalExit(log, "Failed to create video storage backend: %v", err)
	}
	musicStore, err := storageFactory.NewBackend(initCtx, "music", dirs.Music)
	if err != nil {
		fatalExit(log, "Failed to create music storage backend: %v", err)
	}
	thumbnailStore, err := storageFactory.NewBackend(initCtx, "thumbnails", dirs.Thumbnails)
	if err != nil {
		fatalExit(log, "Failed to create thumbnail storage backend: %v", err)
	}
	uploadStore, err := storageFactory.NewBackend(initCtx, "uploads", dirs.Uploads)
	if err != nil {
		fatalExit(log, "Failed to create upload storage backend: %v", err)
	}
	hlsStore, err := storageFactory.NewBackend(initCtx, "hls_cache", dirs.HLSCache)
	if err != nil {
		fatalExit(log, "Failed to create HLS storage backend: %v", err)
	}

	log.Info("Storage backend: %s", cfg.Get().Storage.Backend)

	return storageBackends{
		videos:     videoStore,
		music:      musicStore,
		thumbnails: thumbnailStore,
		uploads:    uploadStore,
		hlsCache:   hlsStore,
	}
}

type modules struct {
	database      *database.Module
	security      *security.Module
	auth          *auth.Module
	media         *media.Module
	streaming     *streaming.Module
	tasks         *tasks.Module
	scanner       *scanner.Module
	thumbnails    *thumbnails.Module
	hls           *hls.Module
	analytics     *analytics.Module
	playlist      *playlist.Module
	admin         *admin.Module
	upload        *upload.Module
	validator     *validator.Module
	backup        *backup.Module
	autodiscovery *autodiscovery.Module
	suggestions   *suggestions.Module
	categorizer   *categorizer.Module
	updater       *updater.Module
	remote        *remote.Module
	duplicates    *duplicates.Module
	receiver      *receiver.Module
	downloader    *downloader.Module
	extractor     *extractor.Module
	crawler       *crawler.Module
	claude        *claude.Module
}

func initModules(srv *server.Server, cfg *config.Manager, log *logger.Logger, stores storageBackends) modules {
	var m modules
	var err error

	// Database (critical)
	m.database = database.NewModule(cfg)
	mustRegister(srv, m.database)

	// Security (critical — requires database for IP list persistence)
	m.security = security.NewModule(cfg, m.database)
	mustRegister(srv, m.security)

	// Auth (critical — requires database)
	m.auth, err = auth.NewModule(cfg, m.database)
	if err != nil {
		fatalExit(log, "Failed to create auth module: %v", err)
	}
	mustRegister(srv, m.auth)

	// Media (critical — requires database, uses storage backends)
	m.media, err = media.NewModule(cfg, m.database)
	if err != nil {
		fatalExit(log, "Failed to create media module: %v", err)
	}
	m.media.SetStores(stores.videos, stores.music, stores.uploads)
	mustRegister(srv, m.media)

	// Streaming (critical — uses storage backend for S3 support)
	m.streaming = streaming.NewModule(cfg)
	m.streaming.SetStore(stores.videos)
	mustRegister(srv, m.streaming)

	// Tasks scheduler (critical)
	m.tasks = tasks.NewModule(cfg)
	mustRegister(srv, m.tasks)

	// Scanner (critical — requires database)
	m.scanner, err = scanner.NewModule(cfg, m.database)
	if err != nil {
		fatalExit(log, "Failed to create scanner module: %v", err)
	}
	mustRegister(srv, m.scanner)

	// Hugging Face client for visual classification (optional)
	if hfCfg := cfg.Get().HuggingFace; hfCfg.Enabled && hfCfg.APIKey != "" {
		setupHFClient(hfCfg, m.scanner, log)
	}

	// Thumbnails (critical — BlurHash repo is wired inside Start() after DB connects)
	m.thumbnails = thumbnails.NewModule(cfg, m.database)
	m.thumbnails.SetMediaIDProvider(m.media)
	m.thumbnails.SetStore(stores.thumbnails)
	m.thumbnails.SetMediaInputResolver(m.media)
	m.media.SetThumbnailQueuer(m.thumbnails)
	mustRegister(srv, m.thumbnails)

	// ── Non-critical modules ───────────────────────────────────────────────

	// HLS (non-critical — falls back gracefully if ffmpeg unavailable, uses storage backend)
	m.hls = hls.NewModule(cfg, m.database)
	m.hls.SetStore(stores.hlsCache)
	m.hls.SetMediaInputResolver(m.media)
	mustRegister(srv, m.hls)

	// Analytics (non-critical — requires database)
	if am, err := analytics.NewModule(cfg, m.database); err != nil {
		log.Warn("Analytics module unavailable: %v", err)
	} else {
		m.analytics = am
		mustRegister(srv, m.analytics)
	}

	// Playlist (non-critical — requires database)
	if pm, err := playlist.NewModule(cfg, m.database); err != nil {
		log.Warn("Playlist module unavailable: %v", err)
	} else {
		m.playlist = pm
		mustRegister(srv, m.playlist)
	}

	// Admin (non-critical — requires database)
	if adm, err := admin.NewModule(cfg, m.database); err != nil {
		log.Warn("Admin module unavailable: %v", err)
	} else {
		m.admin = adm
		mustRegister(srv, m.admin)
	}

	// Upload (non-critical — uses storage backend)
	m.upload = upload.NewModule(cfg)
	m.upload.SetStore(stores.uploads)
	mustRegister(srv, m.upload)

	// Validator (non-critical — requires database for validation results)
	m.validator = validator.NewModule(cfg, m.database)
	mustRegister(srv, m.validator)

	// Backup (non-critical — requires database for manifest storage)
	m.backup = backup.NewModule(cfg, m.database)
	mustRegister(srv, m.backup)

	// Auto-discovery (non-critical — gated by feature flag at construction; nil when disabled)
	if cfg.Get().Features.EnableAutoDiscovery {
		m.autodiscovery = autodiscovery.NewModule(cfg, m.database)
		mustRegister(srv, m.autodiscovery)
	}

	// Suggestions (non-critical — requires database for user profiles)
	m.suggestions = suggestions.NewModule(cfg, m.database)
	mustRegister(srv, m.suggestions)

	// Categorizer (non-critical — requires database for categorization data)
	m.categorizer = categorizer.NewModule(cfg, m.database)
	mustRegister(srv, m.categorizer)

	// Updater (non-critical — needs version string)
	m.updater = updater.NewModule(cfg, Version)
	mustRegister(srv, m.updater)

	// Remote (non-critical — requires database for cache index)
	m.remote = remote.NewModule(cfg, m.database)
	mustRegister(srv, m.remote)

	// Duplicates (non-critical — independent duplicate detection for local and receiver media)
	m.duplicates = duplicates.NewModule(cfg, m.database)
	m.duplicates.SetMediaModule(m.media)
	mustRegister(srv, m.duplicates)

	// Receiver (non-critical — requires database for slave registry and media catalog)
	m.receiver = receiver.NewModule(cfg, m.database)
	m.receiver.SetDuplicatesModule(m.duplicates)
	mustRegister(srv, m.receiver)

	// Downloader (non-critical — proxy to external downloader service, gated by feature flag)
	m.downloader = downloader.NewModule(cfg)
	m.downloader.SetMediaModule(m.media)
	mustRegister(srv, m.downloader)

	// Extractor (non-critical — requires database for item persistence)
	m.extractor = extractor.NewModule(cfg, m.database)
	mustRegister(srv, m.extractor)

	m.crawler = crawler.NewModule(cfg, m.database, m.extractor)
	mustRegister(srv, m.crawler)

	// Claude admin assistant (non-critical — gated by config; requires admin module for audit).
	if cm, err := claude.NewModule(claude.Deps{Config: cfg, DB: m.database, Admin: m.admin}); err != nil {
		log.Warn("Claude admin assistant unavailable: %v", err)
	} else {
		m.claude = cm
		claude.RegisterDefaultTools(m.claude)
		mustRegister(srv, m.claude)
	}

	return m
}

func setupHFClient(hfCfg config.HuggingFaceConfig, scannerModule *scanner.Module, log *logger.Logger) {
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
		MaxConcurrent:     hfCfg.MaxConcurrent,
		Timeout:           timeout,
		Log:               log,
	})
	scannerModule.SetHFClient(hfClient)
	log.Info("Hugging Face visual classification enabled (model: %s)", hfCfg.Model)
}

func setupAgeGate(cfg *config.Manager, analyticsModule *analytics.Module) *middleware.AgeGate {
	appCfg := cfg.Get()
	ageGate := middleware.NewAgeGate(appCfg.AgeGate)
	// Wire age gate analytics callback so passage events are tracked
	if analyticsModule != nil {
		ageGate.OnVerify = func(ip, userAgent string) {
			analyticsModule.TrackTrafficEvent(context.Background(), analytics.TrafficEventParams{
				Type: analytics.EventAgeGatePass, IPAddress: ip, UserAgent: userAgent,
			})
		}
	}
	return ageGate
}

func setupRoutes(srv *server.Server, cfg *config.Manager, mods modules, ageGate *middleware.AgeGate) {
	h := handlers.NewHandler(handlers.HandlerDeps{
		BuildInfo: handlers.BuildInfo{Version: Version, BuildDate: BuildDate},
		Core: handlers.HandlerCoreDeps{
			Config:    cfg,
			Media:     mods.media,
			Streaming: mods.streaming,
			HLS:       mods.hls,
			Auth:      mods.auth,
			Database:  mods.database,
		},
		Optional: handlers.HandlerOptionalDeps{
			Admin:         mods.admin,
			Tasks:         mods.tasks,
			Upload:        mods.upload,
			Scanner:       mods.scanner,
			Thumbnails:    mods.thumbnails,
			Validator:     mods.validator,
			Backup:        mods.backup,
			Autodiscovery: mods.autodiscovery,
			Suggestions:   mods.suggestions,
			Security:      mods.security,
			Categorizer:   mods.categorizer,
			Updater:       mods.updater,
			Remote:        mods.remote,
			Receiver:      mods.receiver,
			Extractor:     mods.extractor,
			Crawler:       mods.crawler,
			Duplicates:    mods.duplicates,
			Analytics:     mods.analytics,
			Playlist:      mods.playlist,
			Downloader:    mods.downloader,
			Claude:        mods.claude,
		},
		ShutdownFunc: srv.Shutdown, // P1-9: drain connections and stop modules before exit
	})
	routes.Setup(srv.Engine(), srv, h, mods.auth, mods.security, cfg, ageGate)
}

func wireSuggestionsSeeding(mediaModule *media.Module, suggestionsModule *suggestions.Module, log *logger.Logger) {
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
				Duration:  item.Duration,
				AddedAt:   item.DateAdded,
				IsMature:  item.IsMature,
			})
		}
		suggestionsModule.UpdateMediaData(mediaInfos)
		if len(mediaInfos) > 0 {
			log.Info("Seeded suggestions with %d items from initial media scan", len(mediaInfos))
		}
	})
}

// validateSecrets checks critical configuration values and logs actionable
// warnings for any that are absent or obviously insecure. A fatal error is
// logged (and the process exits) only for conditions that would render the
// server completely insecure or non-functional.
func validateSecrets(cfg *config.Manager, log *logger.Logger) {
	appCfg := cfg.Get()

	// Receiver: API keys are the sole authentication mechanism for slave nodes.
	if appCfg.Receiver.Enabled && len(appCfg.Receiver.APIKeys) == 0 {
		fatalExit(log, "FATAL: receiver is enabled but no API keys are configured. "+
			"Set RECEIVER_API_KEYS in .env or receiver.api_keys in config.json, then restart.")
	}

	// Enforce minimum length and warn on known-weak receiver API key values.
	const minAPIKeyLen = 32
	weakKeys := map[string]bool{
		"changeme": true, "secret": true, "password": true,
		"test": true, "default": true, "apikey": true, "api-key": true,
	}
	for _, key := range appCfg.Receiver.APIKeys {
		if len(key) < minAPIKeyLen {
			log.Warn("Receiver API key is shorter than %d characters — use at least 32 for production", minAPIKeyLen)
		}
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

	// Admin panel: warn when enabled without a password hash so the operator
	// knows the panel is inaccessible (or worse, open to default credentials).
	if appCfg.Admin.Enabled {
		if appCfg.Admin.PasswordHash == "" {
			log.Warn("Admin panel is enabled but ADMIN_PASSWORD_HASH is not set — " +
				"admin login will fail until a bcrypt hash is configured.")
		}
		if appCfg.Admin.Username == "" {
			log.Warn("Admin panel is enabled but ADMIN_USERNAME is not set — " +
				"admin login will fail until a username is configured.")
		}
	}

	// S3 storage: warn when selected but required credentials are absent.
	if appCfg.Storage.Backend == "s3" {
		s3 := appCfg.Storage.S3
		if s3.AccessKeyID == "" || s3.SecretAccessKey == "" {
			log.Warn("S3 storage backend is selected but access credentials are missing. " +
				"Set S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY (or config.json storage.s3 fields).")
		}
		if s3.Bucket == "" {
			log.Warn("S3 storage backend is selected but storage.s3.bucket is not set.")
		}
		if s3.Endpoint == "" {
			log.Warn("S3 storage backend is selected but storage.s3.endpoint is not set.")
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
	hlsModule *hls.Module,
	cfg *config.Manager,
	log *logger.Logger,
) {
	// Media library scan — discovers new/removed files every hour
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "media-scan",
		Name:        "Media Library Scan",
		Description: "Scans configured directories for new and removed media files",
		Schedule:    1 * time.Hour,
		Func: func(_ context.Context) error {
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
						Duration:  item.Duration,
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
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "metadata-cleanup",
		Name:        "Metadata Cleanup",
		Description: "Removes metadata entries for media files that no longer exist on disk",
		Schedule:    24 * time.Hour,
		Func: func(_ context.Context) error {
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
			if !cfg.Get().Thumbnails.AutoGenerate {
				return nil
			}
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
					_, err := thumbnailsModule.GenerateThumbnailRequest(&thumbnails.ThumbnailRequest{MediaPath: item.Path, MediaID: item.ID, IsAudio: isAudio, HighPriority: false})
					if err == nil || errors.Is(err, thumbnails.ErrThumbnailPending) {
						queued++
					} else {
						log.Debug("Thumbnail generation skipped for %s: %v", item.Name, err)
					}
				}
			}
			if queued > 0 {
				log.Info("Queued %d thumbnail generation jobs", queued)
			}
			return nil
		},
	})

	// Thumbnail cleanup — removes orphans, excess previews, and corrupt files every 6h
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "thumbnail-cleanup",
		Name:        "Thumbnail Cleanup",
		Description: "Removes orphaned, excess, and corrupt thumbnail files",
		Schedule:    6 * time.Hour,
		Func: func(_ context.Context) error {
			result, err := thumbnailsModule.Cleanup()
			if err != nil {
				return err
			}
			total := result.OrphansRemoved + result.ExcessRemoved + result.CorruptRemoved
			if total > 0 {
				log.Info("Thumbnail cleanup: %d orphans, %d excess, %d corrupt removed",
					result.OrphansRemoved, result.ExcessRemoved, result.CorruptRemoved)
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
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "backup-cleanup",
		Name:        "Backup Cleanup",
		Description: "Removes old backups beyond the configured retention count",
		Schedule:    24 * time.Hour,
		Func: func(_ context.Context) error {
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
		Func: func(_ context.Context) error {
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

	// Health check — periodic diagnostics (dir existence; no disk space metrics)
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "health-check",
		Name:        "Health Check",
		Description: "Performs system diagnostics and monitors disk space",
		Schedule:    5 * time.Minute,
		Func: func(_ context.Context) error {
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

	// HLS pre-generation — generates HLS content for video media that doesn't have it yet.
	// Interval is configurable via hls.pre_generate_interval_hours (default: 1).
	// Each cycle queues at most ConcurrentLimit jobs and skips entirely when existing
	// jobs are already in flight, so the system is never overloaded.
	pregenInterval := time.Duration(cfg.Get().HLS.PreGenerateIntervalHours) * time.Hour
	if pregenInterval < 15*time.Minute {
		pregenInterval = 15 * time.Minute
	}
	scheduler.RegisterTask(tasks.TaskRegistration{
		ID:          "hls-pregenerate",
		Name:        "HLS Pre-generation",
		Description: "Pre-generates HLS streaming content for video files that don't have it yet",
		Schedule:    pregenInterval,
		Func: func(ctx context.Context) error {
			if !hlsModule.IsAvailable() {
				return nil
			}
			if !cfg.Get().HLS.AutoGenerate {
				return nil
			}

			// Skip this cycle entirely if jobs are already running/pending
			active := hlsModule.ActiveJobCount()
			if active > 0 {
				log.Debug("HLS pre-generation: %d jobs already active, skipping this cycle", active)
				return nil
			}

			// Queue at most ConcurrentLimit jobs per cycle so we never flood the system
			batchLimit := cfg.Get().HLS.ConcurrentLimit
			if batchLimit <= 0 {
				batchLimit = 2
			}

			items := mediaModule.ListMedia(media.Filter{})
			queued := 0
			for _, item := range items {
				if ctx.Err() != nil {
					break
				}
				if queued >= batchLimit {
					break
				}
				if item.Type != "video" {
					continue
				}
				if hlsModule.HasHLS(item.Path) {
					continue
				}
				if _, err := hlsModule.GenerateHLS(ctx, &hls.GenerateHLSParams{
					MediaPath: item.Path,
					MediaID:   item.ID,
				}); err != nil {
					log.Debug("HLS pre-generation skipped for %s: %v", item.Name, err)
					continue
				}
				queued++
			}
			if queued > 0 {
				log.Info("Queued %d HLS pre-generation jobs (batch limit: %d)", queued, batchLimit)
			}
			return nil
		},
	})

	// Re-apply the interval when it is changed via the admin config panel so that
	// a server restart is not required to pick up a new pre-generation schedule.
	cfg.OnChange(func(newCfg *config.Config) {
		newInterval := time.Duration(newCfg.HLS.PreGenerateIntervalHours) * time.Hour
		if newInterval < 15*time.Minute {
			newInterval = 15 * time.Minute
		}
		if err := scheduler.UpdateSchedule("hls-pregenerate", newInterval); err != nil {
			log.Warn("Failed to update HLS pre-generation schedule: %v", err)
		}
	})
}
