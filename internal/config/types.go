package config

import "time"

// Config holds all server configuration
type Config struct {
	Server        ServerConfig        `json:"server"`
	Directories   DirectoriesConfig   `json:"directories"`
	Streaming     StreamingConfig     `json:"streaming"`
	Download      DownloadConfig      `json:"download"`
	Thumbnails    ThumbnailsConfig    `json:"thumbnails"`
	Analytics     AnalyticsConfig     `json:"analytics"`
	Uploads       UploadsConfig       `json:"uploads"`
	Security      SecurityConfig      `json:"security"`
	Admin         AdminConfig         `json:"admin"`
	Auth          AuthConfig          `json:"auth"`
	HLS           HLSConfig           `json:"hls"`
	RemoteMedia   RemoteMediaConfig   `json:"remote_media"`
	Receiver      ReceiverConfig      `json:"receiver"`
	Extractor     ExtractorConfig     `json:"extractor"`
	Crawler       CrawlerConfig       `json:"crawler"`
	Backup        BackupConfig        `json:"backup"`
	MatureScanner MatureScannerConfig `json:"mature_scanner"`
	HuggingFace   HuggingFaceConfig   `json:"huggingface"`
	Logging       LoggingConfig       `json:"logging"`
	Features      FeaturesConfig      `json:"features"`
	Database      DatabaseConfig      `json:"database"`
	Updater       UpdaterConfig       `json:"updater"`
	AgeGate       AgeGateConfig       `json:"age_gate"`
	UI            UIConfig            `json:"ui"`
	Downloader    DownloaderConfig    `json:"downloader"`
	Storage       StorageConfig       `json:"storage"`
	Claude        ClaudeConfig        `json:"claude"`
}

// ClaudeConfig holds settings for the Claude-powered admin assistant module.
// The module gives authorized admins a live, tool-equipped assistant that can
// read logs and config, run allowlisted shell commands, edit files in
// configured paths, and (in Autonomous mode) act without per-step approval.
type ClaudeConfig struct {
	Enabled bool `json:"enabled"`

	// APIKey is the Anthropic API key. Set via env (CLAUDE_API_KEY) or
	// config.json; never echoed back to clients.
	APIKey string `json:"api_key"`

	// WebLoginToken is an Anthropic OAuth / user-access token (Authorization:
	// Bearer). Useful for admins who authenticate via claude.ai rather than
	// holding a direct API key. Mutually exclusive with APIKey; APIKey takes
	// precedence when both are set. Never echoed back to clients.
	WebLoginToken string `json:"web_login_token"`

	// Model selects the Claude model (e.g. "claude-opus-4-7",
	// "claude-sonnet-4-6", "claude-haiku-4-5-20251001"). Defaults to
	// the latest Sonnet when empty.
	Model string `json:"model"`

	// Mode controls execution behavior: "advisory", "interactive", or
	// "autonomous".
	Mode string `json:"mode"`

	// MaxTokens caps output tokens per turn.
	MaxTokens int `json:"max_tokens"`

	// SystemPrompt is appended to the built-in operational system prompt.
	SystemPrompt string `json:"system_prompt"`

	// AllowedTools is the explicit allowlist of Claude tool names. Empty = all
	// registered tools are available.
	AllowedTools []string `json:"allowed_tools"`

	// AllowedShellCommands is the exact-match allowlist of program names the
	// shell tool may invoke. Non-empty required for shell tool to function.
	AllowedShellCommands []string `json:"allowed_shell_commands"`

	// AllowedPaths restricts file reads/writes to absolute path prefixes
	// (or paths under the server working directory). Empty disables file tools.
	AllowedPaths []string `json:"allowed_paths"`

	// AllowedServices lists systemd/service names the service-restart tool
	// may target. Empty disables service restart.
	AllowedServices []string `json:"allowed_services"`

	// RequireConfirmForWrites forces admin confirmation for write-type tools
	// (file_write, shell_exec, service_restart) regardless of mode. Highly
	// recommended to leave enabled in production.
	RequireConfirmForWrites bool `json:"require_confirm_for_writes"`

	// MaxToolCallsPerTurn caps how many tools Claude can invoke before the
	// server forces a stop. Defaults to 16.
	MaxToolCallsPerTurn int `json:"max_tool_calls_per_turn"`

	// RateLimitPerMinute limits how many chat turns any admin can send; 0 = no limit.
	RateLimitPerMinute int `json:"rate_limit_per_minute"`

	// KillSwitch disables all chat + tool execution when true, regardless of Enabled.
	KillSwitch bool `json:"kill_switch"`

	// RequestTimeout bounds each API call. Default 120s.
	RequestTimeout time.Duration `json:"request_timeout"`

	// HistoryRetentionDays prunes conversations older than this. 0 = keep forever.
	HistoryRetentionDays int `json:"history_retention_days"`
}

// StorageConfig selects and configures the file storage backend.
type StorageConfig struct {
	// Backend selects the storage backend: "local" (default) or "s3".
	Backend string `json:"backend"`

	// S3 holds S3-compatible storage settings (Backblaze B2, AWS S3, MinIO, etc.).
	// Only used when Backend is "s3".
	S3 S3StorageConfig `json:"s3"`
}

// S3StorageConfig holds credentials and settings for S3-compatible storage.
type S3StorageConfig struct {
	Endpoint        string            `json:"endpoint"`          // e.g., "s3.us-west-004.backblazeb2.com"
	Region          string            `json:"region"`            // e.g., "us-west-004"
	AccessKeyID     string            `json:"access_key_id"`     // B2 application key ID
	SecretAccessKey string            `json:"secret_access_key"` // B2 application key
	Bucket          string            `json:"bucket"`            // bucket name
	UsePathStyle    bool              `json:"use_path_style"`    // true for B2 and MinIO
	Prefixes        map[string]string `json:"prefixes"`          // per-role key prefixes
}

// DownloaderConfig holds settings for the external media downloader integration.
type DownloaderConfig struct {
	Enabled        bool          `json:"enabled"`
	URL            string        `json:"url"`
	DownloadsDir   string        `json:"downloads_dir"`
	ImportDir      string        `json:"import_dir"`
	HealthInterval time.Duration `json:"health_interval"`
	RequestTimeout time.Duration `json:"request_timeout"`
}

// UIConfig holds frontend display defaults
type UIConfig struct {
	ItemsPerPage       int `json:"items_per_page"`
	MobileItemsPerPage int `json:"mobile_items_per_page"`
	MobileGridColumns  int `json:"mobile_grid_columns"`
	FeedMaxItems       int `json:"feed_max_items"`     // hard cap on Atom/RSS feed entries; default 50
	FeedDefaultItems   int `json:"feed_default_items"` // default count when no limit is requested; default 20
}

// AgeGateConfig holds age verification gate settings.
type AgeGateConfig struct {
	Enabled      bool          `json:"enabled"`
	BypassIPs    []string      `json:"bypass_ips"`
	IPVerifyTTL  time.Duration `json:"ip_verify_ttl"`
	CookieName   string        `json:"cookie_name"`
	CookieMaxAge int           `json:"cookie_max_age"`
}

// UpdaterConfig holds settings for the source-based updater.
type UpdaterConfig struct {
	AppDir         string `json:"app_dir"`
	DeployKeyPath  string `json:"deploy_key_path"`
	GitHubToken    string `json:"github_token"`
	GitHubUsername string `json:"github_username"`
	Branch         string `json:"branch"`
	UpdateMethod   string `json:"update_method"`
}

// ServerConfig holds server-specific settings
type ServerConfig struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	ReadTimeout     time.Duration `json:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"`
	IdleTimeout     time.Duration `json:"idle_timeout"`
	MaxHeaderBytes  int           `json:"max_header_bytes"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
	EnableHTTPS     bool          `json:"enable_https"`
	CertFile        string        `json:"cert_file"`
	KeyFile         string        `json:"key_file"`
}

// DirectoriesConfig holds directory paths
type DirectoriesConfig struct {
	Videos     string `json:"videos"`
	Music      string `json:"music"`
	Thumbnails string `json:"thumbnails"`
	Playlists  string `json:"playlists"`
	Uploads    string `json:"uploads"`
	Analytics  string `json:"analytics"`
	HLSCache   string `json:"hls_cache"`
	Data       string `json:"data"`
	Logs       string `json:"logs"`
	Temp       string `json:"temp"`
}

// StreamingConfig holds streaming settings
type StreamingConfig struct {
	DefaultChunkSize   int64         `json:"default_chunk_size"`
	MaxChunkSize       int64         `json:"max_chunk_size"`
	BufferSize         int           `json:"buffer_size"`
	KeepAliveEnabled   bool          `json:"keep_alive_enabled"`
	KeepAliveTimeout   time.Duration `json:"keep_alive_timeout"`
	Adaptive           bool          `json:"adaptive"` // if false, disable HLS auto-activate and fall back to direct stream
	MobileOptimization bool          `json:"mobile_optimization"`
	MobileChunkSize    int64         `json:"mobile_chunk_size"`
	RequireAuth        bool          `json:"require_auth"`        // if true, reject unauthenticated streaming (DoS mitigation)
	UnauthStreamLimit  int           `json:"unauth_stream_limit"` // max concurrent streams per IP when unauth; 0 = no limit
}

// DownloadConfig holds file download settings
type DownloadConfig struct {
	Enabled     bool `json:"enabled"`
	ChunkSizeKB int  `json:"chunk_size_kb"`
	RequireAuth bool `json:"require_auth"`
}

// ThumbnailsConfig holds thumbnail generation settings
type ThumbnailsConfig struct {
	Enabled          bool `json:"enabled"`
	AutoGenerate     bool `json:"auto_generate"`
	Width            int  `json:"width"`
	Height           int  `json:"height"`
	Quality          int  `json:"quality"`
	VideoInterval    int  `json:"video_interval"`
	PreviewCount     int  `json:"preview_count"`
	GenerateOnAccess bool `json:"generate_on_access"`
	QueueSize        int  `json:"queue_size"`
	WorkerCount      int  `json:"worker_count"`

	// In-flight job eviction — prevents permanent stalls when a worker exits mid-job.
	InFlightEvictionTimeout time.Duration `json:"inflight_eviction_timeout"` // how long before a stuck job is evicted; default 5m
	InFlightScanInterval    time.Duration `json:"inflight_scan_interval"`    // how often the eviction loop runs; default 1m
}

// AnalyticsConfig holds analytics settings
type AnalyticsConfig struct {
	Enabled         bool          `json:"enabled"`
	RetentionDays   int           `json:"retention_days"`
	SessionTimeout  time.Duration `json:"session_timeout"`
	CleanupInterval time.Duration `json:"cleanup_interval"`
	TrackPlayback   bool          `json:"track_playback"`
	TrackViews      bool          `json:"track_views"`
	ViewCooldown    time.Duration `json:"view_cooldown"` // min gap between counting repeated views of the same item; default 5m
	// MaxReconstructEvents caps how many events are loaded at startup for in-memory
	// stat reconstruction. Higher values improve accuracy but increase startup time.
	// Default 2000 (10000 caused 500ms+ queries on moderate datasets).
	MaxReconstructEvents int `json:"max_reconstruct_events"`
}

// UploadsConfig holds upload settings
type UploadsConfig struct {
	Enabled           bool     `json:"enabled"`
	MaxFileSize       int64    `json:"max_file_size"`
	AllowedExtensions []string `json:"allowed_extensions"`
	RequireAuth       bool     `json:"require_auth"`
	ScanForMature     bool     `json:"scan_for_mature"`
}

// SecurityConfig holds security settings
type SecurityConfig struct {
	// TrustedProxyCIDRs lists CIDR ranges whose X-Forwarded-For headers are trusted.
	// When empty, the built-in RFC-1918 + loopback defaults are used.
	TrustedProxyCIDRs []string      `json:"trusted_proxy_cidrs"`
	EnableIPWhitelist bool          `json:"enable_ip_whitelist"`
	IPWhitelist       []string      `json:"ip_whitelist"`
	EnableIPBlacklist bool          `json:"enable_ip_blacklist"`
	IPBlacklist       []string      `json:"ip_blacklist"`
	RateLimitEnabled  bool          `json:"rate_limit_enabled"`
	RateLimitRequests int           `json:"rate_limit_requests"`
	RateLimitWindow   time.Duration `json:"rate_limit_window"`
	BurstLimit        int           `json:"burst_limit"`
	BurstWindow       time.Duration `json:"burst_window"`
	ViolationsForBan  int           `json:"violations_for_ban"`
	BanDuration       time.Duration `json:"ban_duration"`
	AuthRateLimit     int           `json:"auth_rate_limit"`
	AuthBurstLimit    int           `json:"auth_burst_limit"`
	MaxFileSizeMB     int           `json:"max_file_size_mb"`
	CSPEnabled        bool          `json:"csp_enabled"` // if false, suppress the Content-Security-Policy header
	CSPPolicy         string        `json:"csp_policy"`
	HSTSEnabled       bool          `json:"hsts_enabled"` // if false, suppress HSTS even when hsts_max_age > 0
	HSTSMaxAge        int           `json:"hsts_max_age"`
	CORSEnabled       bool          `json:"cors_enabled"`
	CORSOrigins       []string      `json:"cors_origins"`
}

// AdminConfig holds admin panel settings
type AdminConfig struct {
	Enabled        bool          `json:"enabled"`
	Username       string        `json:"username"`
	PasswordHash   string        `json:"password_hash"`
	SessionTimeout time.Duration `json:"session_timeout"`
	QueryTimeout   time.Duration `json:"query_timeout"`
	MaxQueryRows   int           `json:"max_query_rows"`
}

// AuthConfig holds user authentication settings
type AuthConfig struct {
	Enabled           bool          `json:"enabled"`
	SessionTimeout    time.Duration `json:"session_timeout"`
	SecureCookies     bool          `json:"secure_cookies"`
	MaxLoginAttempts  int           `json:"max_login_attempts"`
	LockoutDuration   time.Duration `json:"lockout_duration"`
	AllowGuests       bool          `json:"allow_guests"`
	AllowRegistration bool          `json:"allow_registration"`
	DefaultUserType   string        `json:"default_user_type"`
	UserTypes         []UserType    `json:"user_types"`
}

// UserType defines permissions and limits for a user type
type UserType struct {
	Name                 string `json:"name"`
	StorageQuota         int64  `json:"storage_quota"`
	MaxConcurrentStreams int    `json:"max_concurrent_streams"`
	AllowDownloads       bool   `json:"allow_downloads"`
	AllowUploads         bool   `json:"allow_uploads"`
	AllowPlaylists       bool   `json:"allow_playlists"`
}

// HLSConfig holds HLS streaming settings
type HLSConfig struct {
	Enabled                  bool          `json:"enabled"`
	SegmentDuration          int           `json:"segment_duration"`
	PlaylistLength           int           `json:"playlist_length"`
	CleanupEnabled           bool          `json:"cleanup_enabled"`
	CleanupInterval          time.Duration `json:"cleanup_interval"`
	RetentionMinutes         int           `json:"retention_minutes"`
	AutoGenerate             bool          `json:"auto_generate"`
	PreGenerateIntervalHours int           `json:"pre_generate_interval_hours"`
	QualityProfiles          []HLSQuality  `json:"quality_profiles"`
	ConcurrentLimit          int           `json:"concurrent_limit"`
	CDNBaseURL               string        `json:"cdn_base_url"`
	LazyTranscode            bool          `json:"lazy_transcode"`

	// Reliability and probe tuning.
	MaxConsecutiveFailures int           `json:"max_consecutive_failures"` // retries before a job is abandoned; default 3
	ProbeTimeout           time.Duration `json:"probe_timeout"`            // ffprobe/ffmpeg probe deadline; default 30s
	StaleLockThreshold     time.Duration `json:"stale_lock_threshold"`     // how old a lock must be before it is considered stale; default 2h

	// Migration flag: set to true once migrateHLSQualityEnabled has run so
	// the migration is not repeated when a user deliberately disables all profiles.
	QualityProfilesMigrated bool `json:"quality_profiles_migrated"`
}

// HLSQuality defines an HLS quality profile
type HLSQuality struct {
	Name         string `json:"name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Bitrate      int    `json:"bitrate"`
	AudioBitrate int    `json:"audio_bitrate"`
	Enabled      bool   `json:"enabled"`
}

// RemoteMediaConfig holds remote media source settings
type RemoteMediaConfig struct {
	Enabled      bool           `json:"enabled"`
	Sources      []RemoteSource `json:"sources"`
	SyncInterval time.Duration  `json:"sync_interval"`
	CacheEnabled bool           `json:"cache_enabled"`
	CacheSize    int64          `json:"cache_size"`
	CacheTTL     time.Duration  `json:"cache_ttl"`

	// HTTP client tuning for fetching remote sources.
	HTTPTimeout            time.Duration `json:"http_timeout"`             // per-request deadline; default 30s
	MaxConcurrentDownloads int           `json:"max_concurrent_downloads"` // background cache download parallelism; default 4
}

// RemoteSource defines a remote media source connection.
type RemoteSource struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// ReceiverConfig holds media receiver (master) settings.
type ReceiverConfig struct {
	Enabled       bool          `json:"enabled"`
	APIKeys       []string      `json:"api_keys"`
	ProxyTimeout  time.Duration `json:"proxy_timeout"`
	HealthCheck   time.Duration `json:"health_check_interval"`
	MaxProxyConns int           `json:"max_proxy_conns"`

	// WebSocket protocol tuning — affects all connected slave nodes.
	WSReadLimit         int64         `json:"ws_read_limit"`         // max WS message bytes; default 16 MB
	WSReadDeadline      time.Duration `json:"ws_read_deadline"`      // idle read timeout; default 60s
	WSPingInterval      time.Duration `json:"ws_ping_interval"`      // server→slave ping cadence; default 25s
	PendingStreamTTL    time.Duration `json:"pending_stream_ttl"`    // how long to wait for a slave to deliver a stream before cleanup; default 30s
	HeartbeatDBDebounce time.Duration `json:"heartbeat_db_debounce"` // min interval between heartbeat DB writes; default 60s
}

// ExtractorConfig holds settings for the stream extractor/proxy.
type ExtractorConfig struct {
	Enabled      bool          `json:"enabled"`
	ProxyTimeout time.Duration `json:"proxy_timeout"`
	MaxItems     int           `json:"max_items"`
}

// CrawlerConfig holds settings for the stream crawler.
type CrawlerConfig struct {
	Enabled        bool          `json:"enabled"`
	BrowserEnabled bool          `json:"browser_enabled"`
	MaxPages       int           `json:"max_pages"`
	CrawlTimeout   time.Duration `json:"crawl_timeout"`
}

// BackupConfig holds backup retention settings
type BackupConfig struct {
	RetentionCount int `json:"retention_count"`
}

// MatureScannerConfig holds content scanning settings
type MatureScannerConfig struct {
	Enabled                   bool     `json:"enabled"`
	AutoFlag                  bool     `json:"auto_flag"`
	HighConfidenceThreshold   float64  `json:"high_confidence_threshold"`
	MediumConfidenceThreshold float64  `json:"medium_confidence_threshold"`
	HighConfidenceKeywords    []string `json:"high_confidence_keywords"`
	MediumConfidenceKeywords  []string `json:"medium_confidence_keywords"`
	RequireReview             bool     `json:"require_review"`
}

// HuggingFaceConfig holds settings for Hugging Face Inference API (visual classification).
type HuggingFaceConfig struct {
	Enabled       bool   `json:"enabled"`
	APIKey        string `json:"api_key"`
	Model         string `json:"model"`
	EndpointURL   string `json:"endpoint_url"`
	MaxFrames     int    `json:"max_frames"`
	TimeoutSecs   int    `json:"timeout_secs"`
	RateLimit     int    `json:"rate_limit"`
	MaxConcurrent int    `json:"max_concurrent"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level        string `json:"level"`
	Format       string `json:"format"`
	FileEnabled  bool   `json:"file_enabled"`
	FileRotation bool   `json:"file_rotation"`
	MaxFileSize  int64  `json:"max_file_size"`
	MaxBackups   int    `json:"max_backups"`
	ColorEnabled bool   `json:"color_enabled"`
}

// FeaturesConfig holds feature toggle settings.
type FeaturesConfig struct {
	EnableHLS                bool `json:"enable_hls"`
	EnableAnalytics          bool `json:"enable_analytics"`
	EnablePlaylists          bool `json:"enable_playlists"`
	EnableUploads            bool `json:"enable_uploads"`
	EnableThumbnails         bool `json:"enable_thumbnails"`
	EnableMatureScanner      bool `json:"enable_mature_scanner"`
	EnableRemoteMedia        bool `json:"enable_remote_media"`
	EnableUserAuth           bool `json:"enable_user_auth"`
	EnableAdminPanel         bool `json:"enable_admin_panel"`
	EnableSuggestions        bool `json:"enable_suggestions"`
	EnableAutoDiscovery      bool `json:"enable_auto_discovery"`
	EnableReceiver           bool `json:"enable_receiver"`
	EnableExtractor          bool `json:"enable_extractor"`
	EnableCrawler            bool `json:"enable_crawler"`
	EnableDuplicateDetection bool `json:"enable_duplicate_detection"`
	EnableHuggingFace        bool `json:"enable_huggingface"`
	EnableDownloader         bool `json:"enable_downloader"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Enabled            bool          `json:"enabled"`
	Host               string        `json:"host"`
	Port               int           `json:"port"`
	Name               string        `json:"name"`
	Username           string        `json:"username,omitempty"`
	Password           string        `json:"password,omitempty"`
	MaxOpenConns       int           `json:"max_open_conns"`
	MaxIdleConns       int           `json:"max_idle_conns"`
	ConnMaxLifetime    time.Duration `json:"conn_max_lifetime"`
	Timeout            time.Duration `json:"timeout"`
	MaxRetries         int           `json:"max_retries"`
	RetryInterval      time.Duration `json:"retry_interval"`
	TLSMode            string        `json:"tls_mode,omitempty"`
	SlowQueryThreshold time.Duration `json:"slow_query_threshold"` // GORM slow-query log threshold; 0 = disabled; default 500ms
}
