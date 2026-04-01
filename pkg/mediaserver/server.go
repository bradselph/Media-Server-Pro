package mediaserver

import (
	"errors"
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
	"media-server-pro/internal/downloader"
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

// Server is the embeddable media server. Construct one with New and start it
// with ListenAndServe. The zero value is not usable.
type Server struct {
	inner  *server.Server
	cfg    *config.Manager
	log    *logger.Logger
	srvCfg *serverConfig

	// Exposed module references so consumers can interact with them after
	// construction (e.g. register custom tasks, query media items).
	Database      *database.Module
	Auth          *auth.Module
	Media         *media.Module
	Streaming     *streaming.Module
	HLS           *hls.Module
	Tasks         *tasks.Module
	Scanner       *scanner.Module
	Thumbnails    *thumbnails.Module
	Security      *security.Module
	Analytics     *analytics.Module
	Playlist      *playlist.Module
	Admin         *admin.Module
	Upload        *upload.Module
	Validator     *validator.Module
	Backup        *backup.Module
	AutoDiscovery *autodiscovery.Module
	Suggestions   *suggestions.Module
	Categorizer   *categorizer.Module
	Updater       *updater.Module
	Remote        *remote.Module
	Duplicates    *duplicates.Module
	Receiver      *receiver.Module
	Downloader    *downloader.Module
	Extractor     *extractor.Module
	Crawler       *crawler.Module
}

// New constructs a fully-wired media server ready to start. It loads
// configuration, constructs the selected modules, registers background tasks,
// and wires up all HTTP routes.
//
// Call ListenAndServe to start accepting connections, or access the exported
// module fields to interact with subsystems programmatically.
func New(opts ...Option) (*Server, error) {
	sc := defaultServerConfig()
	for _, o := range opts {
		o(sc)
	}

	// Load .env if present
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		// non-fatal
		fmt.Fprintf(os.Stderr, "Warning: loading .env: %v\n", err)
	}

	inner, err := server.New(server.Options{
		ConfigPath: sc.configPath,
		LogLevel:   sc.logLevel,
		Version:    sc.version,
		BuildDate:  sc.buildDate,
	})
	if err != nil {
		return nil, fmt.Errorf("mediaserver: failed to create server: %w", err)
	}

	cfg := inner.Config()
	log := logger.New("mediaserver")

	s := &Server{
		inner:  inner,
		cfg:    cfg,
		log:    log,
		srvCfg: sc,
	}

	if err := s.constructModules(); err != nil {
		return nil, err
	}

	s.registerTasks()
	s.wireRoutes()

	return s, nil
}

// ListenAndServe starts all modules, binds the HTTP listener, and blocks until
// the server shuts down (via signal or Shutdown call). This is the primary
// entry point for consumers who want to run the server.
func (s *Server) ListenAndServe() error {
	return s.inner.Start()
}

// Shutdown gracefully drains connections and stops all modules.
func (s *Server) Shutdown() {
	s.inner.Shutdown()
}

// Config returns the configuration manager for runtime config access.
func (s *Server) Config() *config.Manager {
	return s.cfg
}

// want returns true if the given module is in the selected module set.
func (s *Server) want(id ModuleID) bool {
	return moduleSetContains(s.srvCfg.moduleSet, id)
}

// register is a shorthand for registering a module with the inner server.
func (s *Server) register(m server.Module) error {
	if err := s.inner.RegisterModule(m); err != nil {
		return fmt.Errorf("mediaserver: failed to register module %s: %w", m.Name(), err)
	}
	return nil
}

// constructModules builds and registers all selected modules in dependency order.
func (s *Server) constructModules() error {
	cfg := s.cfg

	// ── Critical modules (always required) ──────────────────────────────

	// Database
	if !s.want(ModDatabase) {
		return errors.New("mediaserver: database module is required")
	}
	s.Database = database.NewModule(cfg)
	if err := s.register(s.Database); err != nil {
		return err
	}

	// Security
	if s.want(ModSecurity) {
		s.Security = security.NewModule(cfg, s.Database)
		if err := s.register(s.Security); err != nil {
			return err
		}
	}

	// Auth
	if !s.want(ModAuth) {
		return errors.New("mediaserver: auth module is required")
	}
	am, err := auth.NewModule(cfg, s.Database)
	if err != nil {
		return fmt.Errorf("mediaserver: auth: %w", err)
	}
	s.Auth = am
	if err := s.register(s.Auth); err != nil {
		return err
	}

	// Media
	if !s.want(ModMedia) {
		return errors.New("mediaserver: media module is required")
	}
	mm, err := media.NewModule(cfg, s.Database)
	if err != nil {
		return fmt.Errorf("mediaserver: media: %w", err)
	}
	s.Media = mm
	if err := s.register(s.Media); err != nil {
		return err
	}

	// Streaming
	if !s.want(ModStreaming) {
		return errors.New("mediaserver: streaming module is required")
	}
	s.Streaming = streaming.NewModule(cfg)
	if err := s.register(s.Streaming); err != nil {
		return err
	}

	// Tasks
	if s.want(ModTasks) {
		s.Tasks = tasks.NewModule(cfg)
		if err := s.register(s.Tasks); err != nil {
			return err
		}
	}

	// Scanner
	if s.want(ModScanner) {
		sm, err := scanner.NewModule(cfg, s.Database)
		if err != nil {
			s.log.Warn("Scanner module unavailable: %v", err)
		} else {
			s.Scanner = sm
			if err := s.register(s.Scanner); err != nil {
				return err
			}
			// Wire HuggingFace client if configured
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
					Log:               s.log,
				})
				s.Scanner.SetHFClient(hfClient)
				s.log.Info("Hugging Face visual classification enabled (model: %s)", hfCfg.Model)
			}
		}
	}

	// Thumbnails
	if s.want(ModThumbnails) {
		metadataRepo := mysql.NewMediaMetadataRepository(s.Database.GORM())
		s.Thumbnails = thumbnails.NewModule(cfg, metadataRepo)
		if err := s.register(s.Thumbnails); err != nil {
			return err
		}
	}

	// ── Non-critical modules ────────────────────────────────────────────

	if s.want(ModHLS) {
		s.HLS = hls.NewModule(cfg, s.Database)
		if err := s.register(s.HLS); err != nil {
			return err
		}
	}

	if s.want(ModAnalytics) {
		if am, err := analytics.NewModule(cfg, s.Database); err != nil {
			s.log.Warn("Analytics module unavailable: %v", err)
		} else {
			s.Analytics = am
			_ = s.register(s.Analytics)
		}
	}

	if s.want(ModPlaylist) {
		if pm, err := playlist.NewModule(cfg, s.Database); err != nil {
			s.log.Warn("Playlist module unavailable: %v", err)
		} else {
			s.Playlist = pm
			_ = s.register(s.Playlist)
		}
	}

	if s.want(ModAdmin) {
		if adm, err := admin.NewModule(cfg, s.Database); err != nil {
			s.log.Warn("Admin module unavailable: %v", err)
		} else {
			s.Admin = adm
			_ = s.register(s.Admin)
		}
	}

	if s.want(ModUpload) {
		s.Upload = upload.NewModule(cfg)
		_ = s.register(s.Upload)
	}

	if s.want(ModValidator) {
		s.Validator = validator.NewModule(cfg, s.Database)
		_ = s.register(s.Validator)
	}

	if s.want(ModBackup) {
		s.Backup = backup.NewModule(cfg, s.Database)
		_ = s.register(s.Backup)
	}

	if s.want(ModAutoDiscovery) && cfg.Get().Features.EnableAutoDiscovery {
		s.AutoDiscovery = autodiscovery.NewModule(cfg, s.Database)
		_ = s.register(s.AutoDiscovery)
	}

	if s.want(ModSuggestions) {
		s.Suggestions = suggestions.NewModule(cfg, s.Database)
		_ = s.register(s.Suggestions)
	}

	if s.want(ModCategorizer) {
		s.Categorizer = categorizer.NewModule(cfg, s.Database)
		_ = s.register(s.Categorizer)
	}

	if s.want(ModUpdater) {
		s.Updater = updater.NewModule(cfg, s.srvCfg.version)
		_ = s.register(s.Updater)
	}

	if s.want(ModRemote) {
		s.Remote = remote.NewModule(cfg, s.Database)
		_ = s.register(s.Remote)
	}

	if s.want(ModDuplicates) {
		s.Duplicates = duplicates.NewModule(cfg, s.Database)
		_ = s.register(s.Duplicates)
	}

	if s.want(ModReceiver) {
		s.Receiver = receiver.NewModule(cfg, s.Database)
		if s.Duplicates != nil {
			s.Receiver.SetDuplicatesModule(s.Duplicates)
		}
		_ = s.register(s.Receiver)
	}

	if s.want(ModDownloader) {
		s.Downloader = downloader.NewModule(cfg)
		if s.Media != nil {
			s.Downloader.SetMediaModule(s.Media)
		}
		_ = s.register(s.Downloader)
	}

	if s.want(ModExtractor) {
		s.Extractor = extractor.NewModule(cfg, s.Database)
		_ = s.register(s.Extractor)
	}

	if s.want(ModCrawler) {
		s.Crawler = crawler.NewModule(cfg, s.Database, s.Extractor)
		_ = s.register(s.Crawler)
	}

	return nil
}

// wireRoutes constructs the HTTP handler and registers all API routes.
func (s *Server) wireRoutes() {
	appCfg := s.cfg.Get()
	ageGate := middleware.NewAgeGate(appCfg.AgeGate)

	h := handlers.NewHandler(handlers.HandlerDeps{
		BuildInfo: handlers.BuildInfo{
			Version:   s.srvCfg.version,
			BuildDate: s.srvCfg.buildDate,
		},
		Core: handlers.HandlerCoreDeps{
			Config:    s.cfg,
			Media:     s.Media,
			Streaming: s.Streaming,
			HLS:       s.HLS,
			Auth:      s.Auth,
			Database:  s.Database,
		},
		Optional: handlers.HandlerOptionalDeps{
			Admin:         s.Admin,
			Tasks:         s.Tasks,
			Upload:        s.Upload,
			Scanner:       s.Scanner,
			Thumbnails:    s.Thumbnails,
			Validator:     s.Validator,
			Backup:        s.Backup,
			Autodiscovery: s.AutoDiscovery,
			Suggestions:   s.Suggestions,
			Security:      s.Security,
			Categorizer:   s.Categorizer,
			Updater:       s.Updater,
			Remote:        s.Remote,
			Receiver:      s.Receiver,
			Extractor:     s.Extractor,
			Crawler:       s.Crawler,
			Duplicates:    s.Duplicates,
			Analytics:     s.Analytics,
			Playlist:      s.Playlist,
			Downloader:    s.Downloader,
		},
		ShutdownFunc: s.inner.Shutdown,
	})

	routes.Setup(s.inner.Engine(), s.inner, h, s.Auth, s.Security, s.cfg, ageGate)

	// Seed suggestions when the initial media scan completes
	if s.Suggestions != nil && s.Media != nil {
		s.Media.SetOnInitialScanDone(func(items []*models.MediaItem) {
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
			s.Suggestions.UpdateMediaData(mediaInfos)
			if len(mediaInfos) > 0 {
				s.log.Info("Seeded suggestions with %d items from initial media scan", len(mediaInfos))
			}
		})
	}
}
