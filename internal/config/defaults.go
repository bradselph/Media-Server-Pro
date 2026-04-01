package config

import "time"

// DefaultConfig returns the default configuration.
// Port 8080 is used as default to avoid requiring root/Administrator privileges.
func DefaultConfig() *Config {
	return &Config{
		Server: defaultServerConfig(),
		Directories: DirectoriesConfig{
			Videos:     "./videos",
			Music:      "./music",
			Thumbnails: "./thumbnails",
			Playlists:  "./playlists",
			Uploads:    "./uploads",
			Analytics:  "./analytics",
			HLSCache:   "./hls_cache",
			Data:       "./data",
			Logs:       "./logs",
			Temp:       "./temp",
		},
		Streaming:     defaultStreamingConfig(),
		Download:      defaultDownloadConfig(),
		Thumbnails:    defaultThumbnailsConfig(),
		Analytics:     defaultAnalyticsConfig(),
		Uploads:       defaultUploadsConfig(),
		Security:      defaultSecurityConfig(),
		Admin:         defaultAdminConfig(),
		Auth:          defaultAuthConfig(),
		HLS:           defaultHLSConfig(),
		RemoteMedia:   defaultRemoteMediaConfig(),
		Receiver:      defaultReceiverConfig(),
		Extractor:     defaultExtractorConfig(),
		Crawler:       defaultCrawlerConfig(),
		MatureScanner: defaultMatureScannerConfig(),
		HuggingFace:   defaultHuggingFaceConfig(),
		Backup:        BackupConfig{RetentionCount: 10},
		Logging:       defaultLoggingConfig(),
		Features:      defaultFeaturesConfig(),
		Database:      defaultDatabaseConfig(),
		Updater:       UpdaterConfig{Branch: "main", UpdateMethod: "source"},
		AgeGate:       defaultAgeGateConfig(),
		UI:            UIConfig{ItemsPerPage: 48, MobileItemsPerPage: 24, MobileGridColumns: 2},
		Downloader:    defaultDownloaderConfig(),
	}
}

func defaultServerConfig() ServerConfig {
	return ServerConfig{
		Host:            "0.0.0.0",
		Port:            8080,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    0, // no limit; long media streams would be cut off after 60s
		IdleTimeout:     120 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 30 * time.Second,
		EnableHTTPS:     false,
	}
}

func defaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		DefaultChunkSize:   1024 * 1024,
		MaxChunkSize:       10 * 1024 * 1024,
		BufferSize:         32 * 1024,
		KeepAliveEnabled:   true,
		KeepAliveTimeout:   60 * time.Second,
		MobileOptimization: true,
		MobileChunkSize:    512 * 1024,
		RequireAuth:        false, // allow anonymous streaming by default
		UnauthStreamLimit:  3,     // max concurrent streams per IP for unauthenticated users (DoS mitigation)
	}
}

func defaultDownloadConfig() DownloadConfig {
	return DownloadConfig{
		Enabled:     true,
		ChunkSizeKB: 512,
		RequireAuth: false,
	}
}

func defaultThumbnailsConfig() ThumbnailsConfig {
	return ThumbnailsConfig{
		Enabled:          true,
		AutoGenerate:     true,
		Width:            320,
		Height:           180,
		Quality:          80,
		VideoInterval:    30,
		PreviewCount:     10,
		GenerateOnAccess: true,
		QueueSize:        1000,
		WorkerCount:      4,
	}
}

func defaultAnalyticsConfig() AnalyticsConfig {
	return AnalyticsConfig{
		Enabled:         true,
		RetentionDays:   30,
		SessionTimeout:  30 * time.Minute,
		CleanupInterval: 12 * time.Hour,
		TrackPlayback:   true,
		TrackViews:      true,
	}
}

func defaultUploadsConfig() UploadsConfig {
	return UploadsConfig{
		Enabled:     true,
		MaxFileSize: 5 * 1024 * 1024 * 1024,
		AllowedExtensions: []string{
			".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm",
			".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a",
		},
		RequireAuth: true,
	}
}

func defaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		EnableIPWhitelist: false,
		EnableIPBlacklist: false,
		RateLimitEnabled:  true,
		RateLimitRequests: 300,
		RateLimitWindow:   time.Minute,
		BurstLimit:        60,
		BurstWindow:       5 * time.Second,
		ViolationsForBan:  10,
		BanDuration:       15 * time.Minute,
		AuthRateLimit:     20,
		AuthBurstLimit:    5,
		MaxFileSizeMB:     0,
		CSPEnabled:        true,
		CSPPolicy:         "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; font-src 'self' https://cdn.jsdelivr.net https://fonts.gstatic.com; img-src 'self' data: blob:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self' blob: https://api.iconify.design",
		HSTSEnabled:       false,
		HSTSMaxAge:        31536000,
		CORSEnabled:       false,
		CORSOrigins:       []string{},
	}
}

func defaultAdminConfig() AdminConfig {
	return AdminConfig{
		Enabled:        true,
		Username:       "admin",
		SessionTimeout: 24 * time.Hour,
		QueryTimeout:   30 * time.Second,
		MaxQueryRows:   1000,
	}
}

func defaultAuthConfig() AuthConfig {
	return AuthConfig{
		Enabled:           true,
		SessionTimeout:    7 * 24 * time.Hour,
		SecureCookies:     false,
		MaxLoginAttempts:  5,
		LockoutDuration:   15 * time.Minute,
		AllowGuests:       true,
		AllowRegistration: true,
		DefaultUserType:   "standard",
		UserTypes: []UserType{
			{Name: "premium", StorageQuota: 100 * 1024 * 1024 * 1024, MaxConcurrentStreams: 5, AllowDownloads: true, AllowUploads: true, AllowPlaylists: true},
			{Name: "standard", StorageQuota: 10 * 1024 * 1024 * 1024, MaxConcurrentStreams: 2, AllowDownloads: true, AllowUploads: false, AllowPlaylists: true},
			{Name: "basic", StorageQuota: 1 * 1024 * 1024 * 1024, MaxConcurrentStreams: 1, AllowDownloads: false, AllowUploads: false, AllowPlaylists: false},
			{Name: "guest", StorageQuota: 0, MaxConcurrentStreams: 1, AllowDownloads: false, AllowUploads: false, AllowPlaylists: false},
		},
	}
}

func defaultHLSConfig() HLSConfig {
	return HLSConfig{
		Enabled:          true,
		SegmentDuration:  6,
		PlaylistLength:   0,
		CleanupEnabled:   true,
		CleanupInterval:  1 * time.Hour,
		RetentionMinutes: 60,
		AutoGenerate:             false,
		PreGenerateIntervalHours: 1,
		ConcurrentLimit:          2,
		QualityProfiles: []HLSQuality{
			{Name: "1080p", Width: 1920, Height: 1080, Bitrate: 5000000, AudioBitrate: 192000},
			{Name: "720p", Width: 1280, Height: 720, Bitrate: 2500000, AudioBitrate: 128000},
			{Name: "480p", Width: 854, Height: 480, Bitrate: 1000000, AudioBitrate: 128000},
			{Name: "360p", Width: 640, Height: 360, Bitrate: 500000, AudioBitrate: 96000},
		},
	}
}

func defaultRemoteMediaConfig() RemoteMediaConfig {
	return RemoteMediaConfig{
		Enabled:      false,
		SyncInterval: 1 * time.Hour,
		CacheEnabled: true,
		CacheSize:    1024 * 1024 * 1024,
		CacheTTL:     7 * 24 * time.Hour,
	}
}

func defaultReceiverConfig() ReceiverConfig {
	return ReceiverConfig{
		Enabled:       false,
		ProxyTimeout:  60 * time.Second,
		HealthCheck:   30 * time.Second,
		MaxProxyConns: 50,
		BufferSize:    64 * 1024,
	}
}

func defaultExtractorConfig() ExtractorConfig {
	return ExtractorConfig{
		Enabled:      false,
		ProxyTimeout: 30 * time.Second,
		MaxItems:     500,
	}
}

func defaultCrawlerConfig() CrawlerConfig {
	return CrawlerConfig{
		Enabled:        false,
		BrowserEnabled: true,
		MaxPages:       20,
		CrawlTimeout:   5 * time.Minute,
	}
}

func defaultMatureScannerConfig() MatureScannerConfig {
	return MatureScannerConfig{
		Enabled:                   true,
		AutoFlag:                  true,
		HighConfidenceThreshold:   0.35,
		MediumConfidenceThreshold: 0.15,
		HighConfidenceKeywords:    []string{"xxx", "porn", "adult", "nsfw"},
		MediumConfidenceKeywords:  []string{"mature", "explicit", "18+"},
		RequireReview:             true,
	}
}

// defaultHuggingFaceConfig returns defaults for Hugging Face visual classification.
// Model must be an image-classification or image-to-text model from the Hub (e.g.
// Falconsai/nsfw_image_detection or Salesforce/blip-image-captioning-large).
// Token needs "Inference Providers" or serverless inference permission.
func defaultHuggingFaceConfig() HuggingFaceConfig {
	return HuggingFaceConfig{
		Enabled:       false,
		Model:         "Falconsai/nsfw_image_detection",
		MaxFrames:     3,
		TimeoutSecs:   30,
		RateLimit:     30,
		MaxConcurrent: 2,
	}
}

func defaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:        "info",
		Format:       "text",
		FileEnabled:  true,
		FileRotation: true,
		MaxFileSize:  100 * 1024 * 1024,
		MaxBackups:   5,
		ColorEnabled: true,
	}
}

func defaultFeaturesConfig() FeaturesConfig {
	return FeaturesConfig{
		EnableHLS:                true,
		EnableAnalytics:          true,
		EnablePlaylists:          true,
		EnableUploads:            true,
		EnableThumbnails:         true,
		EnableMatureScanner:      true,
		EnableRemoteMedia:        false,
		EnableUserAuth:           true,
		EnableAdminPanel:         true,
		EnableSuggestions:        true,
		EnableAutoDiscovery:      true,
		EnableReceiver:           false,
		EnableExtractor:          false,
		EnableCrawler:            false,
		EnableDuplicateDetection: true,
		EnableHuggingFace:        false,
		EnableDownloader:         false,
	}
}

func defaultDownloaderConfig() DownloaderConfig {
	return DownloaderConfig{
		Enabled:        false,
		URL:            "http://localhost:3000",
		DownloadsDir:   "",
		ImportDir:      "",
		HealthInterval: 30 * time.Second,
		RequestTimeout: 30 * time.Second,
	}
}

func defaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		Enabled:         true,
		Host:            "localhost",
		Port:            3306,
		Name:            "mediaserver",
		Username:        "mediaserver",
		Password:        "",
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 1 * time.Hour,
		Timeout:         10 * time.Second,
		MaxRetries:      3,
		RetryInterval:   2 * time.Second,
	}
}

func defaultAgeGateConfig() AgeGateConfig {
	return AgeGateConfig{
		Enabled:      false,
		IPVerifyTTL:  24 * time.Hour,
		CookieName:   "age_verified",
		CookieMaxAge: 365 * 24 * 60 * 60,
	}
}
