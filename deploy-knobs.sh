#!/usr/bin/env bash
# deploy-knobs.sh — single source of truth for every config knob the
# deploy pipeline cares about for Media Server Pro.
#
# Sourced (not executed) by deploy.sh and deploy-configure.sh. Adding
# a new knob requires touching ONE place: append it to KNOB_ORDER and
# fill in its description / default / scope / section here. The
# interactive prompter picks it up automatically and flags it as
# ★ NEW until the operator has reviewed it once, so a release that
# adds a new knob can never silently slip past on the next deploy.
# internal/config/knob_registry_test.go fails the build when a server
# env var has no knob here (or a runtime knob is read by nothing), so
# the registry cannot silently drift from the code.
#
# Scopes:
#   vps       — consumed locally by deploy.sh (SSH coords, paths,
#               systemd service name, repo URL). Never forwarded to
#               the VPS; lives in .deploy.env on the deploy machine.
#   toolchain — version pins for Go / Node. Empty = auto-detect from
#               go.mod / web/nuxt-ui/package.json. Lives in .deploy.env.
#   runtime   — appended/upserted into $DEPLOY_DIR/.env on every deploy.
#               Non-empty values overwrite anything the operator put on
#               the VPS by hand; empty values are skipped so an empty
#               entry in .deploy.env doesn't clobber a value the
#               operator hand-set on the VPS as a fallback.
#               How a runtime knob then takes effect depends on the
#               setting (see DEPLOY.md "How knobs take effect"):
#                 * paths, database, object storage and admin login are
#                   re-read from .env on every start;
#                 * everything else is owned by config.json once the
#                   server has seeded it, so deploy.sh runs
#                   `server -apply-knobs` while the service is stopped:
#                   each such knob whose value CHANGED since the last
#                   deploy is written into config.json. Unchanged knobs
#                   are left alone, so admin-UI edits survive deploys
#                   until you change the knob itself. The FIRST deploy on
#                   an install only records a baseline and lists where
#                   .deploy.env disagrees with config.json;
#                   `./deploy.sh --reapply-knobs` re-asserts all of them.
#   build     — exported into the on-VPS shell that runs `npm run
#               build` for the Nuxt frontend. Baked into the bundle
#               (Nuxt runtimeConfig.public via NUXT_PUBLIC_*). Must
#               re-deploy to change.
#
# Sensitivity (KNOB_SENSITIVE=true) only affects display in the
# prompter — values are still stored verbatim in .deploy.env. The
# deploy machine is the trust boundary; rotate via the upstream
# provider (GitHub PAT, Hugging Face, etc.) if .deploy.env
# leaks.
#
# Sections are display-only — they group prompts in the interactive
# walk. They do not affect runtime behaviour.

KNOB_ORDER=(
  # ── VPS connection ───────────────────────────────────────────────
  VPS_HOST
  VPS_USER
  VPS_PORT
  KEY_FILE
  # ── Deploy paths ─────────────────────────────────────────────────
  DEPLOY_DIR
  SERVICE
  # ── Repository ───────────────────────────────────────────────────
  REPO_URL
  GITHUB_TOKEN
  # ── Toolchain pins ───────────────────────────────────────────────
  MSP_GO_VERSION
  MSP_NODE_MAJOR
  # ── Federated peer (master URL for --setup-receiver) ─────────────
  MASTER_URL
  # ── Server runtime ───────────────────────────────────────────────
  SERVER_PORT
  SERVER_HOST
  SERVER_ENABLE_HTTPS
  SERVER_CERT_FILE
  SERVER_KEY_FILE
  SERVER_READ_HEADER_TIMEOUT
  SERVER_READ_TIMEOUT
  SERVER_WRITE_TIMEOUT
  SERVER_IDLE_TIMEOUT
  SERVER_SHUTDOWN_TIMEOUT
  SERVER_MAX_HEADER_BYTES
  SERVER_MEMORY_LIMIT_PERCENT
  GOMEMLIMIT
  GOGC
  LOG_LEVEL
  # ── Logging ──────────────────────────────────────────────────────
  LOG_FORMAT
  LOG_FILE_ENABLED
  LOG_COLOR_ENABLED
  LOG_FILE_ROTATION
  LOG_MAX_FILE_SIZE
  LOG_MAX_BACKUPS
  # ── Data paths ───────────────────────────────────────────────────
  VIDEOS_DIR
  MUSIC_DIR
  UPLOADS_DIR
  THUMBNAILS_DIR
  PLAYLISTS_DIR
  HLS_CACHE_DIR
  ANALYTICS_DIR
  DATA_DIR
  LOGS_DIR
  TEMP_DIR
  BACKUP_DIR
  # ── Admin login ──────────────────────────────────────────────────
  ADMIN_ENABLED
  ADMIN_USERNAME
  ADMIN_PASSWORD
  ADMIN_PASSWORD_HASH
  ADMIN_SESSION_TIMEOUT_HOURS
  ADMIN_QUERY_TIMEOUT_SECONDS
  # ── Auth / public exposure ───────────────────────────────────────
  AUTH_ENABLED
  AUTH_ALLOW_GUESTS
  AUTH_ALLOW_REGISTRATION
  AUTH_SESSION_TIMEOUT_HOURS
  AUTH_MAX_LOGIN_ATTEMPTS
  AUTH_LOCKOUT_DURATION_MINUTES
  AUTH_SECURE_COOKIES
  AUTH_DEFAULT_USER_TYPE
  # ── HTTP security headers ────────────────────────────────────────
  CSP_ENABLED
  CSP_POLICY
  HSTS_ENABLED
  HSTS_MAX_AGE
  CORS_ENABLED
  CORS_ORIGINS
  SECURITY_TRUSTED_PROXY_CIDRS
  # ── Rate limits / IP filtering ───────────────────────────────────
  RATE_LIMIT_ENABLED
  RATE_LIMIT_REQUESTS
  RATE_LIMIT_WINDOW_SECONDS
  SECURITY_BURST_LIMIT
  SECURITY_BURST_WINDOW_SECONDS
  AUTH_RATE_LIMIT
  AUTH_BURST_LIMIT
  SECURITY_VIOLATIONS_FOR_BAN
  BAN_DURATION_MINUTES
  SECURITY_ENABLE_IP_WHITELIST
  SECURITY_IP_WHITELIST
  SECURITY_ENABLE_IP_BLACKLIST
  SECURITY_IP_BLACKLIST
  SECURITY_MAX_FILE_SIZE_MB
  # ── Age gate ─────────────────────────────────────────────────────
  AGE_GATE_ENABLED
  AGE_GATE_BYPASS_IPS
  AGE_GATE_IP_VERIFY_TTL_HOURS
  AGE_GATE_COOKIE_NAME
  AGE_GATE_COOKIE_MAX_AGE
  # ── Feature flags ────────────────────────────────────────────────
  FEATURE_HLS
  FEATURE_THUMBNAILS
  FEATURE_UPLOADS
  FEATURE_PLAYLISTS
  FEATURE_ANALYTICS
  FEATURE_SUGGESTIONS
  FEATURE_AUTO_DISCOVERY
  FEATURE_DUPLICATE_DETECTION
  FEATURE_MATURE_SCANNER
  FEATURE_USER_AUTH
  FEATURE_ADMIN_PANEL
  FEATURE_EXTRACTOR
  # ── Streaming / uploads ──────────────────────────────────────────
  STREAMING_ADAPTIVE
  STREAMING_REQUIRE_AUTH
  STREAMING_UNAUTH_STREAM_LIMIT
  STREAMING_CHUNK_SIZE
  STREAMING_MAX_CHUNK_SIZE
  STREAMING_BUFFER_SIZE
  STREAMING_MOBILE_OPTIMIZATION
  STREAMING_MOBILE_CHUNK_SIZE
  STREAMING_KEEP_ALIVE_ENABLED
  STREAMING_KEEP_ALIVE_TIMEOUT_SECONDS
  DOWNLOAD_ENABLED
  DOWNLOAD_REQUIRE_AUTH
  DOWNLOAD_CHUNK_SIZE_KB
  UPLOADS_ENABLED
  UPLOADS_MAX_FILE_SIZE
  UPLOADS_ALLOWED_EXTENSIONS
  UPLOADS_REQUIRE_AUTH
  UPLOADS_SCAN_FOR_MATURE
  # ── HLS transcoding ──────────────────────────────────────────────
  HLS_AUTO_GENERATE
  HLS_CONCURRENT_LIMIT
  HLS_HARDWARE_ACCEL
  HLS_QUALITIES
  HLS_SEGMENT_DURATION
  HLS_LAZY_TRANSCODE
  HLS_PRE_GENERATE_INTERVAL_HOURS
  HLS_MAX_CONSECUTIVE_FAILURES
  HLS_PROBE_TIMEOUT_SECONDS
  HLS_STALE_LOCK_THRESHOLD_HOURS
  HLS_CDN_BASE_URL
  HLS_CLEANUP_ENABLED
  HLS_CLEANUP_INTERVAL_MINUTES
  HLS_RETENTION_MINUTES
  # ── Thumbnails ───────────────────────────────────────────────────
  THUMBNAILS_AUTO_GENERATE
  THUMBNAILS_GENERATE_ON_ACCESS
  THUMBNAILS_WORKER_COUNT
  THUMBNAILS_QUEUE_SIZE
  THUMBNAILS_WIDTH
  THUMBNAILS_HEIGHT
  THUMBNAILS_QUALITY
  THUMBNAILS_VIDEO_INTERVAL
  THUMBNAILS_PREVIEW_COUNT
  THUMBNAILS_INFLIGHT_EVICTION_TIMEOUT_MINUTES
  THUMBNAILS_INFLIGHT_SCAN_INTERVAL_SECONDS
  # ── Analytics ────────────────────────────────────────────────────
  ANALYTICS_TRACK_VIEWS
  ANALYTICS_TRACK_PLAYBACK
  ANALYTICS_RETENTION_DAYS
  ANALYTICS_SESSION_TIMEOUT_MINUTES
  ANALYTICS_VIEW_COOLDOWN_MINUTES
  ANALYTICS_CLEANUP_INTERVAL_MINUTES
  ANALYTICS_MAX_RECONSTRUCT_EVENTS
  # ── Mature-content scanner ───────────────────────────────────────
  MATURE_SCANNER_AUTO_FLAG
  MATURE_SCANNER_REQUIRE_REVIEW
  MATURE_SCANNER_HIGH_CONFIDENCE_THRESHOLD
  MATURE_SCANNER_MEDIUM_CONFIDENCE_THRESHOLD
  MATURE_SCANNER_HIGH_CONFIDENCE_KEYWORDS
  MATURE_SCANNER_MEDIUM_CONFIDENCE_KEYWORDS
  # ── Federation (receiver / follower / remote media / extractor) ──
  FEATURE_REMOTE_MEDIA
  FEATURE_RECEIVER
  RECEIVER_ENABLED
  RECEIVER_API_KEY
  RECEIVER_MAX_PROXY_CONNS
  RECEIVER_PROXY_TIMEOUT_SECONDS
  RECEIVER_HEALTH_CHECK_SECONDS
  RECEIVER_WS_READ_LIMIT
  RECEIVER_WS_READ_DEADLINE_SECONDS
  RECEIVER_WS_PING_INTERVAL_SECONDS
  RECEIVER_PENDING_STREAM_TTL_SECONDS
  RECEIVER_HEARTBEAT_DB_DEBOUNCE_SECONDS
  FOLLOWER_ENABLED
  FOLLOWER_MASTER_URL
  FOLLOWER_API_KEY
  FOLLOWER_SLAVE_ID
  FOLLOWER_SLAVE_NAME
  FOLLOWER_SCAN_INTERVAL_SECONDS
  FOLLOWER_HEARTBEAT_INTERVAL_SECONDS
  FOLLOWER_MAX_STREAMS
  FOLLOWER_RECONNECT_BASE_SECONDS
  FOLLOWER_RECONNECT_MAX_SECONDS
  REMOTE_MEDIA_SYNC_INTERVAL_MINUTES
  REMOTE_MEDIA_CACHE_ENABLED
  REMOTE_MEDIA_CACHE_SIZE
  REMOTE_MEDIA_CACHE_TTL_HOURS
  REMOTE_MEDIA_HTTP_TIMEOUT_SECONDS
  REMOTE_MEDIA_MAX_CONCURRENT_DOWNLOADS
  EXTRACTOR_PROXY_TIMEOUT_SECONDS
  EXTRACTOR_MAX_ITEMS
  # ── Hugging Face (mature content classification) ─────────────────
  FEATURE_HUGGINGFACE
  HUGGINGFACE_ENABLED
  HUGGINGFACE_API_KEY
  HUGGINGFACE_MODEL
  HUGGINGFACE_ENDPOINT_URL
  HUGGINGFACE_MAX_FRAMES
  HUGGINGFACE_TIMEOUT_SECS
  HUGGINGFACE_RATE_LIMIT
  HUGGINGFACE_MAX_CONCURRENT
  # ── Downloader integration ───────────────────────────────────────
  FEATURE_DOWNLOADER
  DOWNLOADER_ENABLED
  DOWNLOADER_URL
  DOWNLOADER_DOWNLOADS_DIR
  DOWNLOADER_IMPORT_DIR
  DOWNLOADER_INTERNAL_TOKEN
  DOWNLOADER_HEALTH_INTERVAL_SECONDS
  DOWNLOADER_REQUEST_TIMEOUT_SECONDS
  # ── Hub (BETA external embed catalog) ────────────────────────────
  FEATURE_HUB
  HUB_SOURCE_URL
  HUB_AUTO_IMPORT
  HUB_CSV_PATH
  HUB_WORK_DIR
  HUB_PAGE_SIZE
  HUB_PROXY_IMAGES
  HUB_PROXY_ENABLED
  HUB_PROXY_ALL_USERS
  HUB_PROXY_RESOLVERS
  HUB_PROXY_CACHE_TTL_SECONDS
  HUB_PROXY_MAX_CONCURRENT_RESOLVES
  HUB_PROXY_IMAGE_CACHE_MB
  # ── HiDrive WebDAV cold-tier mount ───────────────────────────────
  HIDRIVE_ENABLED
  HIDRIVE_WEBDAV_URL
  HIDRIVE_USER
  HIDRIVE_PASS
  HIDRIVE_REMOTE_PATH
  HIDRIVE_LIBRARY_SUBDIR
  HIDRIVE_READONLY
  HIDRIVE_BUFFER_SIZE
  HIDRIVE_VFS_CACHE_MODE
  HIDRIVE_VFS_CACHE_MAX_SIZE
  # ── Database ─────────────────────────────────────────────────────
  DATABASE_ENABLED
  DATABASE_HOST
  DATABASE_PORT
  DATABASE_NAME
  DATABASE_USERNAME
  DATABASE_PASSWORD
  DATABASE_TLS_MODE
  DATABASE_MAX_OPEN_CONNS
  DATABASE_MAX_IDLE_CONNS
  DATABASE_CONN_MAX_LIFETIME
  DATABASE_TIMEOUT
  DATABASE_MAX_RETRIES
  DATABASE_RETRY_INTERVAL
  DATABASE_SLOW_QUERY_THRESHOLD_MS
  DATABASE_HEARTBEAT_ENABLED
  DATABASE_HEARTBEAT_INTERVAL
  DATABASE_HEARTBEAT_THRESHOLD
  DATABASE_RECOVERY_ENABLED
  DATABASE_RECOVERY_COMMAND
  DATABASE_RECOVERY_ARGS
  DATABASE_RECOVERY_COOLDOWN
  DATABASE_RECOVERY_MAX_ATTEMPTS
  # ── Object storage (S3-compatible) ───────────────────────────────
  STORAGE_BACKEND
  S3_ENDPOINT
  S3_REGION
  S3_BUCKET
  S3_ACCESS_KEY_ID
  S3_SECRET_ACCESS_KEY
  S3_USE_PATH_STYLE
  # ── Backups ──────────────────────────────────────────────────────
  BACKUP_RETENTION_COUNT
  # ── UI paging / feeds ────────────────────────────────────────────
  UI_ITEMS_PER_PAGE
  UI_MOBILE_ITEMS_PER_PAGE
  UI_MOBILE_GRID_COLUMNS
  UI_FEED_DEFAULT_ITEMS
  UI_FEED_MAX_ITEMS
  # ── In-app updater ───────────────────────────────────────────────
  UPDATER_BRANCH
  UPDATER_METHOD
  UPDATER_APP_DIR
  UPDATER_DEPLOY_KEY_PATH
  UPDATER_GITHUB_USERNAME
  UPDATER_GITHUB_TOKEN
  # ── Frontend (build-time, baked into Nuxt bundle) ────────────────
  NUXT_PUBLIC_GA_ID
  NUXT_PUBLIC_BUILD_ID
  NUXT_PUBLIC_API_BASE
  # ── Brand / public-site identity (baked into Nuxt bundle) ────────
  NUXT_PUBLIC_BRAND_NAME
  NUXT_PUBLIC_BRAND_TAGLINE
  NUXT_PUBLIC_BRAND_GRADIENT
  # ── Adult-site legal compliance (baked into Nuxt bundle) ─────────
  NUXT_PUBLIC_COMPLIANCE_EMAIL
  NUXT_PUBLIC_COMPLIANCE_ADDRESS
  NUXT_PUBLIC_DMCA_AGENT_NAME
  NUXT_PUBLIC_DMCA_EMAIL
  NUXT_PUBLIC_DMCA_ADDRESS
)

declare -A KNOB_DESCRIPTION
declare -A KNOB_DEFAULT
declare -A KNOB_SCOPE
declare -A KNOB_SECTION
declare -A KNOB_SENSITIVE

# ── VPS connection ─────────────────────────────────────────────────────
KNOB_DESCRIPTION[VPS_HOST]="SSH host of the VPS (e.g. xmodsxtreme.com or an IPv4)."
KNOB_DEFAULT[VPS_HOST]=""
KNOB_SCOPE[VPS_HOST]="vps"
KNOB_SECTION[VPS_HOST]="VPS connection"

KNOB_DESCRIPTION[VPS_USER]="SSH user on the VPS."
KNOB_DEFAULT[VPS_USER]="root"
KNOB_SCOPE[VPS_USER]="vps"
KNOB_SECTION[VPS_USER]="VPS connection"

KNOB_DESCRIPTION[VPS_PORT]="SSH port."
KNOB_DEFAULT[VPS_PORT]="22"
KNOB_SCOPE[VPS_PORT]="vps"
KNOB_SECTION[VPS_PORT]="VPS connection"

KNOB_DESCRIPTION[KEY_FILE]="Path to the SSH private key (auto-generated on first use if missing)."
KNOB_DEFAULT[KEY_FILE]="\$HOME/.ssh/id_ed25519"
KNOB_SCOPE[KEY_FILE]="vps"
KNOB_SECTION[KEY_FILE]="VPS connection"

# ── Deploy paths ──────────────────────────────────────────────────────
KNOB_DESCRIPTION[DEPLOY_DIR]="Where Media Server Pro lives on the VPS."
KNOB_DEFAULT[DEPLOY_DIR]="/opt/media-server"
KNOB_SCOPE[DEPLOY_DIR]="vps"
KNOB_SECTION[DEPLOY_DIR]="Deploy paths"

KNOB_DESCRIPTION[SERVICE]="systemd unit name (becomes /etc/systemd/system/<name>.service)."
KNOB_DEFAULT[SERVICE]="media-server"
KNOB_SCOPE[SERVICE]="vps"
KNOB_SECTION[SERVICE]="Deploy paths"

# ── Repository ────────────────────────────────────────────────────────
KNOB_DESCRIPTION[REPO_URL]="Git repository to deploy (host/path, no scheme)."
KNOB_DEFAULT[REPO_URL]="github.com/bradselph/Media-Server-Pro.git"
KNOB_SCOPE[REPO_URL]="vps"
KNOB_SECTION[REPO_URL]="Repository"

KNOB_DESCRIPTION[GITHUB_TOKEN]="GitHub PAT for cloning a private repo (required for this repo)."
KNOB_DEFAULT[GITHUB_TOKEN]=""
KNOB_SCOPE[GITHUB_TOKEN]="vps"
KNOB_SECTION[GITHUB_TOKEN]="Repository"
KNOB_SENSITIVE[GITHUB_TOKEN]="true"

# ── Toolchain pins ────────────────────────────────────────────────────
KNOB_DESCRIPTION[MSP_GO_VERSION]="Pin Go version (e.g. 1.26.2). Empty = auto-detect from go.mod."
KNOB_DEFAULT[MSP_GO_VERSION]=""
KNOB_SCOPE[MSP_GO_VERSION]="toolchain"
KNOB_SECTION[MSP_GO_VERSION]="Toolchain pins"

KNOB_DESCRIPTION[MSP_NODE_MAJOR]="Pin Node major (e.g. 22). Empty = auto-detect from web/nuxt-ui/package.json."
KNOB_DEFAULT[MSP_NODE_MAJOR]=""
KNOB_SCOPE[MSP_NODE_MAJOR]="toolchain"
KNOB_SECTION[MSP_NODE_MAJOR]="Toolchain pins"

# ── Federated peer (master URL for --setup-receiver) ─────────────────
KNOB_DESCRIPTION[MASTER_URL]="Public URL of the master node, used by --setup-receiver when handing out the receiver API key. Empty = http://<VPS_HOST>."
KNOB_DEFAULT[MASTER_URL]=""
KNOB_SCOPE[MASTER_URL]="vps"
KNOB_SECTION[MASTER_URL]="Federated peer"

# ── Server runtime ───────────────────────────────────────────────────
KNOB_DESCRIPTION[SERVER_PORT]="HTTP port the Go server binds on the VPS. A reverse proxy (Caddy/Traefik/nginx) usually fronts it — point the proxy at the new port before changing this. deploy.sh health-checks the port in config.json."
KNOB_DEFAULT[SERVER_PORT]="3000"
KNOB_SCOPE[SERVER_PORT]="runtime"
KNOB_SECTION[SERVER_PORT]="Server"

KNOB_DESCRIPTION[SERVER_HOST]="Interface to bind. 127.0.0.1 behind a reverse proxy; 0.0.0.0 for direct access."
KNOB_DEFAULT[SERVER_HOST]="127.0.0.1"
KNOB_SCOPE[SERVER_HOST]="runtime"
KNOB_SECTION[SERVER_HOST]="Server"

KNOB_DESCRIPTION[LOG_LEVEL]="Log verbosity (debug | info | warn | error). Bump to debug while triaging."
KNOB_DEFAULT[LOG_LEVEL]="info"
KNOB_SCOPE[LOG_LEVEL]="runtime"
KNOB_SECTION[LOG_LEVEL]="Server"

# ── Admin login ──────────────────────────────────────────────────────
KNOB_DESCRIPTION[ADMIN_ENABLED]="Superseded by FEATURE_ADMIN_PANEL: the server copies that flag over this one on every start, so this value never takes effect. Kept for older .deploy.env files — set FEATURE_ADMIN_PANEL instead."
KNOB_DEFAULT[ADMIN_ENABLED]="true"
KNOB_SCOPE[ADMIN_ENABLED]="runtime"
KNOB_SECTION[ADMIN_ENABLED]="Admin login"

KNOB_DESCRIPTION[ADMIN_USERNAME]="Admin login username."
KNOB_DEFAULT[ADMIN_USERNAME]="admin"
KNOB_SCOPE[ADMIN_USERNAME]="runtime"
KNOB_SECTION[ADMIN_USERNAME]="Admin login"

KNOB_DESCRIPTION[ADMIN_PASSWORD]="Plaintext admin password. Prefer ADMIN_PASSWORD_HASH in production. Leave blank when using a hash."
KNOB_DEFAULT[ADMIN_PASSWORD]=""
KNOB_SCOPE[ADMIN_PASSWORD]="runtime"
KNOB_SECTION[ADMIN_PASSWORD]="Admin login"
KNOB_SENSITIVE[ADMIN_PASSWORD]="true"

KNOB_DESCRIPTION[ADMIN_PASSWORD_HASH]="Bcrypt hash of the admin password (e.g. \$2b\$10\$...). Takes precedence over ADMIN_PASSWORD when set."
KNOB_DEFAULT[ADMIN_PASSWORD_HASH]=""
KNOB_SCOPE[ADMIN_PASSWORD_HASH]="runtime"
KNOB_SECTION[ADMIN_PASSWORD_HASH]="Admin login"
KNOB_SENSITIVE[ADMIN_PASSWORD_HASH]="true"

# ── Auth / public exposure ───────────────────────────────────────────
# auth.* is denylisted from the admin config API (api/handlers/admin_config.go),
# so these knobs are the only way to change them short of hand-editing
# config.json: change the value in .deploy.env and deploy — deploy.sh writes a
# changed knob into config.json via `server -apply-knobs` (see the header).
KNOB_DESCRIPTION[AUTH_ENABLED]="Superseded by FEATURE_USER_AUTH: the server copies that flag over this one on every start, so this value never takes effect. Kept for older .deploy.env files — set FEATURE_USER_AUTH instead."
KNOB_DEFAULT[AUTH_ENABLED]="true"
KNOB_SCOPE[AUTH_ENABLED]="runtime"
KNOB_SECTION[AUTH_ENABLED]="Auth"

KNOB_DESCRIPTION[AUTH_ALLOW_GUESTS]="Allow unauthenticated browse access (true | false)."
KNOB_DEFAULT[AUTH_ALLOW_GUESTS]="true"
KNOB_SCOPE[AUTH_ALLOW_GUESTS]="runtime"
KNOB_SECTION[AUTH_ALLOW_GUESTS]="Auth"

KNOB_DESCRIPTION[AUTH_ALLOW_REGISTRATION]="Allow public sign-up (true | false). The binary defaults to OPEN; set false to close it."
KNOB_DEFAULT[AUTH_ALLOW_REGISTRATION]="false"
KNOB_SCOPE[AUTH_ALLOW_REGISTRATION]="runtime"
KNOB_SECTION[AUTH_ALLOW_REGISTRATION]="Auth"

KNOB_DESCRIPTION[AUTH_SESSION_TIMEOUT_HOURS]="Session cookie lifetime (hours). 168 = 7 days."
KNOB_DEFAULT[AUTH_SESSION_TIMEOUT_HOURS]="168"
KNOB_SCOPE[AUTH_SESSION_TIMEOUT_HOURS]="runtime"
KNOB_SECTION[AUTH_SESSION_TIMEOUT_HOURS]="Auth"

# ── HTTP security headers ────────────────────────────────────────────
KNOB_DESCRIPTION[CSP_ENABLED]="Emit Content-Security-Policy headers (true | false)."
KNOB_DEFAULT[CSP_ENABLED]="true"
KNOB_SCOPE[CSP_ENABLED]="runtime"
KNOB_SECTION[CSP_ENABLED]="HTTP security"

KNOB_DESCRIPTION[HSTS_ENABLED]="Emit Strict-Transport-Security header (true | false). Only enable once HTTPS is permanent — browsers remember the policy."
KNOB_DEFAULT[HSTS_ENABLED]="false"
KNOB_SCOPE[HSTS_ENABLED]="runtime"
KNOB_SECTION[HSTS_ENABLED]="HTTP security"

KNOB_DESCRIPTION[CORS_ENABLED]="Emit CORS headers (true | false)."
KNOB_DEFAULT[CORS_ENABLED]="true"
KNOB_SCOPE[CORS_ENABLED]="runtime"
KNOB_SECTION[CORS_ENABLED]="HTTP security"

KNOB_DESCRIPTION[CORS_ORIGINS]="Comma-separated full origins, e.g. https://example.com (a bare '*' set via env is rejected; set wildcard through the admin UI). Tighten to your public origin in prod."
KNOB_DEFAULT[CORS_ORIGINS]=""
KNOB_SCOPE[CORS_ORIGINS]="runtime"
KNOB_SECTION[CORS_ORIGINS]="HTTP security"

# ── Rate limit ───────────────────────────────────────────────────────
KNOB_DESCRIPTION[RATE_LIMIT_ENABLED]="Per-IP request rate limiter (true | false)."
KNOB_DEFAULT[RATE_LIMIT_ENABLED]="false"
KNOB_SCOPE[RATE_LIMIT_ENABLED]="runtime"
KNOB_SECTION[RATE_LIMIT_ENABLED]="Rate limits"

KNOB_DESCRIPTION[RATE_LIMIT_REQUESTS]="Max requests per RATE_LIMIT_WINDOW_SECONDS per IP."
KNOB_DEFAULT[RATE_LIMIT_REQUESTS]="1000"
KNOB_SCOPE[RATE_LIMIT_REQUESTS]="runtime"
KNOB_SECTION[RATE_LIMIT_REQUESTS]="Rate limits"

KNOB_DESCRIPTION[RATE_LIMIT_WINDOW_SECONDS]="Window size for the per-IP rate limiter."
KNOB_DEFAULT[RATE_LIMIT_WINDOW_SECONDS]="60"
KNOB_SCOPE[RATE_LIMIT_WINDOW_SECONDS]="runtime"
KNOB_SECTION[RATE_LIMIT_WINDOW_SECONDS]="Rate limits"

# ── Age gate ─────────────────────────────────────────────────────────
KNOB_DESCRIPTION[AGE_GATE_ENABLED]="Show the age-verification gate on first visit (true | false)."
KNOB_DEFAULT[AGE_GATE_ENABLED]="true"
KNOB_SCOPE[AGE_GATE_ENABLED]="runtime"
KNOB_SECTION[AGE_GATE_ENABLED]="Age gate"

KNOB_DESCRIPTION[AGE_GATE_BYPASS_IPS]="Comma-separated IPs that skip the age gate (e.g. 127.0.0.1)."
KNOB_DEFAULT[AGE_GATE_BYPASS_IPS]="127.0.0.1"
KNOB_SCOPE[AGE_GATE_BYPASS_IPS]="runtime"
KNOB_SECTION[AGE_GATE_BYPASS_IPS]="Age gate"

# ── Streaming / uploads ──────────────────────────────────────────────
KNOB_DESCRIPTION[DOWNLOAD_ENABLED]="Allow direct file downloads (true | false). Per-user can_download flag still gates."
KNOB_DEFAULT[DOWNLOAD_ENABLED]="true"
KNOB_SCOPE[DOWNLOAD_ENABLED]="runtime"
KNOB_SECTION[DOWNLOAD_ENABLED]="Streaming / uploads"

KNOB_DESCRIPTION[UPLOADS_ENABLED]="Superseded by FEATURE_UPLOADS: the server copies that flag over this one on every start, so this value never takes effect. Kept for older .deploy.env files — set FEATURE_UPLOADS instead."
KNOB_DEFAULT[UPLOADS_ENABLED]="true"
KNOB_SCOPE[UPLOADS_ENABLED]="runtime"
KNOB_SECTION[UPLOADS_ENABLED]="Streaming / uploads"

KNOB_DESCRIPTION[UPLOADS_MAX_FILE_SIZE]="Per-file upload cap in bytes. 5368709120 = 5 GiB."
KNOB_DEFAULT[UPLOADS_MAX_FILE_SIZE]="5368709120"
KNOB_SCOPE[UPLOADS_MAX_FILE_SIZE]="runtime"
KNOB_SECTION[UPLOADS_MAX_FILE_SIZE]="Streaming / uploads"

# ── Feature flags ────────────────────────────────────────────────────
KNOB_DESCRIPTION[FEATURE_REMOTE_MEDIA]="Enable remote-media proxy (true | false). Set true on a follower node that pulls from a master."
KNOB_DEFAULT[FEATURE_REMOTE_MEDIA]="true"
KNOB_SCOPE[FEATURE_REMOTE_MEDIA]="runtime"
KNOB_SECTION[FEATURE_REMOTE_MEDIA]="Federation"

KNOB_DESCRIPTION[FEATURE_RECEIVER]="Enable receiver endpoints (true | false). Set true on a master that accepts pushes from peers."
KNOB_DEFAULT[FEATURE_RECEIVER]="true"
KNOB_SCOPE[FEATURE_RECEIVER]="runtime"
KNOB_SECTION[FEATURE_RECEIVER]="Federation"

KNOB_DESCRIPTION[RECEIVER_ENABLED]="Deprecated: superseded by FEATURE_RECEIVER (syncFeatureToggles copies it onto Receiver.Enabled every load). Set FEATURE_RECEIVER instead."
KNOB_DEFAULT[RECEIVER_ENABLED]="true"
KNOB_SCOPE[RECEIVER_ENABLED]="runtime"
KNOB_SECTION[RECEIVER_ENABLED]="Federation"

# ── Hugging Face (mature content classification) ─────────────────────
KNOB_DESCRIPTION[FEATURE_HUGGINGFACE]="Enable HF visual-classification module (true | false)."
KNOB_DEFAULT[FEATURE_HUGGINGFACE]="false"
KNOB_SCOPE[FEATURE_HUGGINGFACE]="runtime"
KNOB_SECTION[FEATURE_HUGGINGFACE]="Hugging Face"

KNOB_DESCRIPTION[HUGGINGFACE_ENABLED]="Deprecated: superseded by FEATURE_HUGGINGFACE (syncFeatureToggles copies it onto HuggingFace.Enabled every load). Set FEATURE_HUGGINGFACE instead."
KNOB_DEFAULT[HUGGINGFACE_ENABLED]="false"
KNOB_SCOPE[HUGGINGFACE_ENABLED]="runtime"
KNOB_SECTION[HUGGINGFACE_ENABLED]="Hugging Face"

KNOB_DESCRIPTION[HUGGINGFACE_API_KEY]="Hugging Face API token (hf_...). Read scope is enough. https://huggingface.co/settings/tokens"
KNOB_DEFAULT[HUGGINGFACE_API_KEY]=""
KNOB_SCOPE[HUGGINGFACE_API_KEY]="runtime"
KNOB_SECTION[HUGGINGFACE_API_KEY]="Hugging Face"
KNOB_SENSITIVE[HUGGINGFACE_API_KEY]="true"

KNOB_DESCRIPTION[HUGGINGFACE_MODEL]="HF model id for mature-content scoring (image-classification or image-to-text model)."
KNOB_DEFAULT[HUGGINGFACE_MODEL]="Falconsai/nsfw_image_detection"
KNOB_SCOPE[HUGGINGFACE_MODEL]="runtime"
KNOB_SECTION[HUGGINGFACE_MODEL]="Hugging Face"

# ── Downloader integration ───────────────────────────────────────────
KNOB_DESCRIPTION[FEATURE_DOWNLOADER]="Show the Downloader tab and route requests to it (true | false)."
KNOB_DEFAULT[FEATURE_DOWNLOADER]="false"
KNOB_SCOPE[FEATURE_DOWNLOADER]="runtime"
KNOB_SECTION[FEATURE_DOWNLOADER]="Downloader"

KNOB_DESCRIPTION[DOWNLOADER_ENABLED]="Superseded by FEATURE_DOWNLOADER: the server copies that flag over this one on every start, so this value never takes effect. Kept for older .deploy.env files — set FEATURE_DOWNLOADER instead."
KNOB_DEFAULT[DOWNLOADER_ENABLED]="false"
KNOB_SCOPE[DOWNLOADER_ENABLED]="runtime"
KNOB_SECTION[DOWNLOADER_ENABLED]="Downloader"

KNOB_DESCRIPTION[DOWNLOADER_URL]="Base URL of the standalone downloader service."
KNOB_DEFAULT[DOWNLOADER_URL]="http://localhost:4000"
KNOB_SCOPE[DOWNLOADER_URL]="runtime"
KNOB_SECTION[DOWNLOADER_URL]="Downloader"

KNOB_DESCRIPTION[DOWNLOADER_DOWNLOADS_DIR]="Absolute path on the VPS to the downloader's downloads folder, used by file import. Empty = file import disabled."
KNOB_DEFAULT[DOWNLOADER_DOWNLOADS_DIR]=""
KNOB_SCOPE[DOWNLOADER_DOWNLOADS_DIR]="runtime"
KNOB_SECTION[DOWNLOADER_DOWNLOADS_DIR]="Downloader"

KNOB_DESCRIPTION[DOWNLOADER_INTERNAL_TOKEN]="Shared secret with the downloader service. The same value must be set on the downloader as MSP_INTERNAL_TOKEN so admin requests (including bearer-token admins) can be vouched for without a session-cookie callback. Auto-generated by --fix-env when empty."
KNOB_DEFAULT[DOWNLOADER_INTERNAL_TOKEN]=""
KNOB_SCOPE[DOWNLOADER_INTERNAL_TOKEN]="runtime"
KNOB_SECTION[DOWNLOADER_INTERNAL_TOKEN]="Downloader"
KNOB_SENSITIVE[DOWNLOADER_INTERNAL_TOKEN]="true"

# ── Hub (BETA external embed catalog) ────────────────────────────────
# The Hub tab lets users browse an age-gated catalog of external video embeds
# imported from a pipe-delimited CSV into the hub_embeds table. BETA + off by
# default; fully inert when FEATURE_HUB=false (no routes, no tab, no DB use).
# With a source URL + auto-import the server fetches the zipped catalog, streams
# the CSV straight into the DB, and bulk-imports it once (only when empty). All
# of these are owned by config.json / the admin UI (System Settings → Hub
# Catalog) once seeded; a knob changed here is written into config.json by
# the next deploy, and an unchanged one never overrides an admin-UI edit.
KNOB_DESCRIPTION[FEATURE_HUB]="Enable the BETA Hub external-embed catalog tab (true | false). Off = fully inert."
KNOB_DEFAULT[FEATURE_HUB]="false"
KNOB_SCOPE[FEATURE_HUB]="runtime"
KNOB_SECTION[FEATURE_HUB]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_SOURCE_URL]="URL of a zipped catalog CSV to fetch + stream-import for a one-time bootstrap (http(s) only). Empty = no auto-fetch."
KNOB_DEFAULT[HUB_SOURCE_URL]=""
KNOB_SCOPE[HUB_SOURCE_URL]="runtime"
KNOB_SECTION[HUB_SOURCE_URL]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_AUTO_IMPORT]="Bootstrap-import the catalog once at startup when hub_embeds is empty (true | false). Uses HUB_SOURCE_URL or HUB_CSV_PATH."
KNOB_DEFAULT[HUB_AUTO_IMPORT]="false"
KNOB_SCOPE[HUB_AUTO_IMPORT]="runtime"
KNOB_SECTION[HUB_AUTO_IMPORT]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_CSV_PATH]="Absolute path to a pre-placed pipe-delimited catalog CSV on the server (alternative to HUB_SOURCE_URL). Empty = none."
KNOB_DEFAULT[HUB_CSV_PATH]=""
KNOB_SCOPE[HUB_CSV_PATH]="runtime"
KNOB_SECTION[HUB_CSV_PATH]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_WORK_DIR]="Scratch dir for the catalog zip download (needs room for the archive). Empty = OS temp dir."
KNOB_DEFAULT[HUB_WORK_DIR]=""
KNOB_SCOPE[HUB_WORK_DIR]="runtime"
KNOB_SECTION[HUB_WORK_DIR]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PAGE_SIZE]="Embeds per page in the Hub browse grid."
KNOB_DEFAULT[HUB_PAGE_SIZE]="60"
KNOB_SCOPE[HUB_PAGE_SIZE]="runtime"
KNOB_SECTION[HUB_PAGE_SIZE]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_IMAGES]="Serve Hub thumbnails and hover previews through this server instead of the provider CDN (true | false). Stops viewer IPs reaching the provider and fixes broken artwork where the provider blocks the viewer."
KNOB_DEFAULT[HUB_PROXY_IMAGES]="true"
KNOB_SCOPE[HUB_PROXY_IMAGES]="runtime"
KNOB_SECTION[HUB_PROXY_IMAGES]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_ENABLED]="Server-side video playback: the server resolves the real stream and proxies the bytes, so the provider sees this server not the viewer (true | false). Costs upstream bandwidth twice."
KNOB_DEFAULT[HUB_PROXY_ENABLED]="false"
KNOB_SCOPE[HUB_PROXY_ENABLED]="runtime"
KNOB_SECTION[HUB_PROXY_ENABLED]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_ALL_USERS]="Who may use server-side playback: false = administrators only (rollout default), true = every logged-in viewer with mature content enabled."
KNOB_DEFAULT[HUB_PROXY_ALL_USERS]="false"
KNOB_SCOPE[HUB_PROXY_ALL_USERS]="runtime"
KNOB_SECTION[HUB_PROXY_ALL_USERS]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_RESOLVERS]="Ordered, comma-separated resolver chain for turning an embed id into a playable stream; first success wins. sidecar = the downloader service, page = parse the provider embed page directly."
KNOB_DEFAULT[HUB_PROXY_RESOLVERS]="sidecar,page"
KNOB_SCOPE[HUB_PROXY_RESOLVERS]="runtime"
KNOB_SECTION[HUB_PROXY_RESOLVERS]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_CACHE_TTL_SECONDS]="How long a resolved stream URL is reused. A safety net only: an expired URL is detected and re-resolved when the CDN rejects it."
KNOB_DEFAULT[HUB_PROXY_CACHE_TTL_SECONDS]="1800"
KNOB_SCOPE[HUB_PROXY_CACHE_TTL_SECONDS]="runtime"
KNOB_SECTION[HUB_PROXY_CACHE_TTL_SECONDS]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_MAX_CONCURRENT_RESOLVES]="Cap on how many distinct Hub items may be resolving at once. Applied at startup."
KNOB_DEFAULT[HUB_PROXY_MAX_CONCURRENT_RESOLVES]="4"
KNOB_SCOPE[HUB_PROXY_MAX_CONCURRENT_RESOLVES]="runtime"
KNOB_SECTION[HUB_PROXY_MAX_CONCURRENT_RESOLVES]="Hub (BETA)"

KNOB_DESCRIPTION[HUB_PROXY_IMAGE_CACHE_MB]="In-memory budget for cached Hub artwork, in MB. 0 disables the cache (images still proxy, just refetched)."
KNOB_DEFAULT[HUB_PROXY_IMAGE_CACHE_MB]="512"
KNOB_SCOPE[HUB_PROXY_IMAGE_CACHE_MB]="runtime"
KNOB_SECTION[HUB_PROXY_IMAGE_CACHE_MB]="Hub (BETA)"

# ── HiDrive WebDAV cold-tier mount ───────────────────────────────────
# These knobs drive `./deploy.sh --setup-hidrive`, which mounts an IONOS
# HiDrive WebDAV share read-only via rclone + a systemd unit and grafts it
# into the video library as a subfolder. Scope is "vps": the values are
# consumed by deploy.sh to perform the on-VPS mount and live only in the
# local .deploy.env — they are NEVER forwarded into the app's $DEPLOY_DIR/.env
# (the WebDAV password has no business in the server's runtime env; rclone
# stores it obscured in /root/.config/rclone/rclone.conf instead).
KNOB_DESCRIPTION[HIDRIVE_ENABLED]="Mount an IONOS HiDrive WebDAV share as a read-only video source via ./deploy.sh --setup-hidrive (true | false). When false, --setup-hidrive tears any existing mount down."
KNOB_DEFAULT[HIDRIVE_ENABLED]="false"
KNOB_SCOPE[HIDRIVE_ENABLED]="vps"
KNOB_SECTION[HIDRIVE_ENABLED]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_WEBDAV_URL]="HiDrive WebDAV endpoint (SSL). For IONOS HiDrive this is https://webdav.hidrive.ionos.com/."
KNOB_DEFAULT[HIDRIVE_WEBDAV_URL]="https://webdav.hidrive.ionos.com/"
KNOB_SCOPE[HIDRIVE_WEBDAV_URL]="vps"
KNOB_SECTION[HIDRIVE_WEBDAV_URL]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_USER]="HiDrive account/protocol username for WebDAV basic auth."
KNOB_DEFAULT[HIDRIVE_USER]=""
KNOB_SCOPE[HIDRIVE_USER]="vps"
KNOB_SECTION[HIDRIVE_USER]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_PASS]="HiDrive WebDAV password (or per-protocol password if you use HiDrive 2FA). Shipped to the VPS over scp, obscured with 'rclone obscure', and stored only in rclone.conf — never in the app .env."
KNOB_DEFAULT[HIDRIVE_PASS]=""
KNOB_SCOPE[HIDRIVE_PASS]="vps"
KNOB_SECTION[HIDRIVE_PASS]="HiDrive mount"
KNOB_SENSITIVE[HIDRIVE_PASS]="true"

KNOB_DESCRIPTION[HIDRIVE_REMOTE_PATH]="Sub-path inside the HiDrive share to expose (e.g. /users/me/media). Empty = the whole account root."
KNOB_DEFAULT[HIDRIVE_REMOTE_PATH]=""
KNOB_SCOPE[HIDRIVE_REMOTE_PATH]="vps"
KNOB_SECTION[HIDRIVE_REMOTE_PATH]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_LIBRARY_SUBDIR]="Folder name under VIDEOS_DIR where the mount is grafted; the scanner indexes it as a subfolder. rclone mounts straight here (no symlink — the scanner's WalkDir does not follow symlinks, and the storage backend rejects symlinks leaving the videos root)."
KNOB_DEFAULT[HIDRIVE_LIBRARY_SUBDIR]="hidrive"
KNOB_SCOPE[HIDRIVE_LIBRARY_SUBDIR]="vps"
KNOB_SECTION[HIDRIVE_LIBRARY_SUBDIR]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_READONLY]="Mount HiDrive read-only (true | false). true = a pull-only source (safest, streaming via Range reads). false = read-write so the downloader can store imported media on HiDrive (rclone --vfs-cache-mode writes; uploads happen on import)."
KNOB_DEFAULT[HIDRIVE_READONLY]="true"
KNOB_SCOPE[HIDRIVE_READONLY]="vps"
KNOB_SECTION[HIDRIVE_READONLY]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_BUFFER_SIZE]="rclone in-memory read-ahead per open file (--buffer-size, e.g. 32M). Larger rides out WebDAV latency spikes mid-stream without the player stalling; costs that much RAM per file being streamed or transcoded. rclone's own default is 16M. Applied by --setup-hidrive."
KNOB_DEFAULT[HIDRIVE_BUFFER_SIZE]="32M"
KNOB_SCOPE[HIDRIVE_BUFFER_SIZE]="vps"
KNOB_SECTION[HIDRIVE_BUFFER_SIZE]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_VFS_CACHE_MODE]="rclone VFS cache (off | full). off = every read goes to HiDrive. full = keep what has been read on the VPS disk (sparse, under /var/cache/hidrive-media) so seeking back and re-watching are served locally, with 128M extra read-ahead; bounded by HIDRIVE_VFS_CACHE_MAX_SIZE. Applied by --setup-hidrive."
KNOB_DEFAULT[HIDRIVE_VFS_CACHE_MODE]="off"
KNOB_SCOPE[HIDRIVE_VFS_CACHE_MODE]="vps"
KNOB_SECTION[HIDRIVE_VFS_CACHE_MODE]="HiDrive mount"

KNOB_DESCRIPTION[HIDRIVE_VFS_CACHE_MAX_SIZE]="Disk cap for the HiDrive read cache when HIDRIVE_VFS_CACHE_MODE=full (e.g. 20G); least-recently-used data is evicted past it, and anything unread for a week. Ignored when the cache is off."
KNOB_DEFAULT[HIDRIVE_VFS_CACHE_MAX_SIZE]="20G"
KNOB_SCOPE[HIDRIVE_VFS_CACHE_MAX_SIZE]="vps"
KNOB_SECTION[HIDRIVE_VFS_CACHE_MAX_SIZE]="HiDrive mount"

# ── Database ─────────────────────────────────────────────────────────
KNOB_DESCRIPTION[DATABASE_HOST]="MariaDB/MySQL host."
KNOB_DEFAULT[DATABASE_HOST]="127.0.0.1"
KNOB_SCOPE[DATABASE_HOST]="runtime"
KNOB_SECTION[DATABASE_HOST]="Database"

KNOB_DESCRIPTION[DATABASE_PORT]="MariaDB/MySQL port."
KNOB_DEFAULT[DATABASE_PORT]="3306"
KNOB_SCOPE[DATABASE_PORT]="runtime"
KNOB_SECTION[DATABASE_PORT]="Database"

KNOB_DESCRIPTION[DATABASE_NAME]="Database name."
KNOB_DEFAULT[DATABASE_NAME]=""
KNOB_SCOPE[DATABASE_NAME]="runtime"
KNOB_SECTION[DATABASE_NAME]="Database"

KNOB_DESCRIPTION[DATABASE_USERNAME]="DB application user."
KNOB_DEFAULT[DATABASE_USERNAME]=""
KNOB_SCOPE[DATABASE_USERNAME]="runtime"
KNOB_SECTION[DATABASE_USERNAME]="Database"

KNOB_DESCRIPTION[DATABASE_PASSWORD]="DB application password."
KNOB_DEFAULT[DATABASE_PASSWORD]=""
KNOB_SCOPE[DATABASE_PASSWORD]="runtime"
KNOB_SECTION[DATABASE_PASSWORD]="Database"
KNOB_SENSITIVE[DATABASE_PASSWORD]="true"

KNOB_DESCRIPTION[DATABASE_TLS_MODE]="MySQL TLS handshake mode (false | true | skip-verify | preferred). 'skip-verify' for self-signed certs on remote DBs."
KNOB_DEFAULT[DATABASE_TLS_MODE]="false"
KNOB_SCOPE[DATABASE_TLS_MODE]="runtime"
KNOB_SECTION[DATABASE_TLS_MODE]="Database"

KNOB_DESCRIPTION[DATABASE_HEARTBEAT_ENABLED]="Background loop that pings the DB on an interval so a connection lost while the server is idle is noticed immediately instead of on the next user request."
KNOB_DEFAULT[DATABASE_HEARTBEAT_ENABLED]="true"
KNOB_SCOPE[DATABASE_HEARTBEAT_ENABLED]="runtime"
KNOB_SECTION[DATABASE_HEARTBEAT_ENABLED]="Database"

KNOB_DESCRIPTION[DATABASE_HEARTBEAT_INTERVAL]="How often to ping the DB (Go duration, e.g. 30s). Minimum 1s."
KNOB_DEFAULT[DATABASE_HEARTBEAT_INTERVAL]="30s"
KNOB_SCOPE[DATABASE_HEARTBEAT_INTERVAL]="runtime"
KNOB_SECTION[DATABASE_HEARTBEAT_INTERVAL]="Database"

KNOB_DESCRIPTION[DATABASE_HEARTBEAT_THRESHOLD]="Consecutive failed pings before the recovery command runs. With the 30s default interval, 5 means ~2.5 min of sustained failure. One success resets the count."
KNOB_DEFAULT[DATABASE_HEARTBEAT_THRESHOLD]="5"
KNOB_SCOPE[DATABASE_HEARTBEAT_THRESHOLD]="runtime"
KNOB_SECTION[DATABASE_HEARTBEAT_THRESHOLD]="Database"

KNOB_DESCRIPTION[DATABASE_RECOVERY_ENABLED]="Re-run deploy.sh to restart the stack when the heartbeat crosses its failure threshold. OFF by default: it restarts the whole service, which is rarely wanted on a dev box. Requires the unit to be able to run deploy.sh -- see DATABASE_RECOVERY_COMMAND."
KNOB_DEFAULT[DATABASE_RECOVERY_ENABLED]="false"
KNOB_SCOPE[DATABASE_RECOVERY_ENABLED]="runtime"
KNOB_SECTION[DATABASE_RECOVERY_ENABLED]="Database"

KNOB_DESCRIPTION[DATABASE_RECOVERY_COMMAND]="Script to run on recovery. Empty = deploy.sh next to the server binary. Under systemd it is started via systemd-run so the deploy's own 'systemctl stop' does not kill it; note the shipped unit sets NoNewPrivileges=true, which blocks the sudo calls inside deploy.sh."
KNOB_DEFAULT[DATABASE_RECOVERY_COMMAND]=""
KNOB_SCOPE[DATABASE_RECOVERY_COMMAND]="runtime"
KNOB_SECTION[DATABASE_RECOVERY_COMMAND]="Database"

KNOB_DESCRIPTION[DATABASE_RECOVERY_COOLDOWN]="Minimum gap between recovery attempts (Go duration). Keeps a DB that is down for an hour from triggering an hour of back-to-back deploys. Minimum 1m."
KNOB_DEFAULT[DATABASE_RECOVERY_COOLDOWN]="15m"
KNOB_SCOPE[DATABASE_RECOVERY_COOLDOWN]="runtime"
KNOB_SECTION[DATABASE_RECOVERY_COOLDOWN]="Database"

KNOB_DESCRIPTION[DATABASE_RECOVERY_MAX_ATTEMPTS]="Give up after this many recovery attempts and just log (0 = unlimited). The counter resets as soon as a ping succeeds."
KNOB_DEFAULT[DATABASE_RECOVERY_MAX_ATTEMPTS]="3"
KNOB_SCOPE[DATABASE_RECOVERY_MAX_ATTEMPTS]="runtime"
KNOB_SECTION[DATABASE_RECOVERY_MAX_ATTEMPTS]="Database"

# ── Frontend (build-time, baked into Nuxt bundle) ────────────────────
KNOB_DESCRIPTION[NUXT_PUBLIC_GA_ID]="Google Analytics 4 measurement id (G-XXXXXXXXXX). Empty = no GA loaded. Surfaces in the bundle via runtimeConfig.public.gaId; consent gate still applies."
KNOB_DEFAULT[NUXT_PUBLIC_GA_ID]=""
KNOB_SCOPE[NUXT_PUBLIC_GA_ID]="build"
KNOB_SECTION[NUXT_PUBLIC_GA_ID]="Frontend (baked into bundle)"

KNOB_DESCRIPTION[NUXT_PUBLIC_BUILD_ID]="Free-form tag stamped into the bundle (visible to error reporters / debug). Empty = the release workflow's auto-version tag wins."
KNOB_DEFAULT[NUXT_PUBLIC_BUILD_ID]=""
KNOB_SCOPE[NUXT_PUBLIC_BUILD_ID]="build"
KNOB_SECTION[NUXT_PUBLIC_BUILD_ID]="Frontend (baked into bundle)"

KNOB_DESCRIPTION[NUXT_PUBLIC_API_BASE]="Override API base URL baked into the bundle. Empty = same-origin (default)."
KNOB_DEFAULT[NUXT_PUBLIC_API_BASE]=""
KNOB_SCOPE[NUXT_PUBLIC_API_BASE]="build"
KNOB_SECTION[NUXT_PUBLIC_API_BASE]="Frontend (baked into bundle)"

# ── Brand / public-site identity ─────────────────────────────────────
# Resolved by composables/useBrandConfig.ts. Resolution order:
#   1. window.APP_CONFIG (runtime override, not currently injected)
#   2. useRuntimeConfig().public (these knobs, baked at build time)
#   3. app.config.ts defaults
#   4. Hard-coded fallbacks ('Media Server Pro' etc.)
# Empty = falls through to the next layer.

KNOB_DESCRIPTION[NUXT_PUBLIC_BRAND_NAME]="Public site name shown in nav, page titles, and legal copy. Empty = 'Media Server Pro'."
KNOB_DEFAULT[NUXT_PUBLIC_BRAND_NAME]=""
KNOB_SCOPE[NUXT_PUBLIC_BRAND_NAME]="build"
KNOB_SECTION[NUXT_PUBLIC_BRAND_NAME]="Brand"

KNOB_DESCRIPTION[NUXT_PUBLIC_BRAND_TAGLINE]="Tagline under the brand name (10px uppercase). Empty = 'Your Library'."
KNOB_DEFAULT[NUXT_PUBLIC_BRAND_TAGLINE]=""
KNOB_SCOPE[NUXT_PUBLIC_BRAND_TAGLINE]="build"
KNOB_SECTION[NUXT_PUBLIC_BRAND_TAGLINE]="Brand"

KNOB_DESCRIPTION[NUXT_PUBLIC_BRAND_GRADIENT]="CSS linear-gradient for the logo tile (e.g. 'linear-gradient(135deg,#6366f1,#3b82f6)'). Empty = OKLCH gradient derived from --accent-hue."
KNOB_DEFAULT[NUXT_PUBLIC_BRAND_GRADIENT]=""
KNOB_SCOPE[NUXT_PUBLIC_BRAND_GRADIENT]="build"
KNOB_SECTION[NUXT_PUBLIC_BRAND_GRADIENT]="Brand"

# ── Adult-site legal compliance ──────────────────────────────────────
# Rendered on /2257 (18 U.S.C. § 2257 record-keeping statement) and
# /dmca (DMCA notice & takedown policy). Shipping these EMPTY in
# production is legally meaningless — operators MUST set them before
# the site goes public. DMCA agent must also be registered with the
# U.S. Copyright Office (copyright.gov/dmca-directory, $6 one-time).

KNOB_DESCRIPTION[NUXT_PUBLIC_COMPLIANCE_EMAIL]="Email for the 2257 records-custodian on /2257. Required for public adult sites with US users."
KNOB_DEFAULT[NUXT_PUBLIC_COMPLIANCE_EMAIL]=""
KNOB_SCOPE[NUXT_PUBLIC_COMPLIANCE_EMAIL]="build"
KNOB_SECTION[NUXT_PUBLIC_COMPLIANCE_EMAIL]="Legal compliance"

KNOB_DESCRIPTION[NUXT_PUBLIC_COMPLIANCE_ADDRESS]="Postal address of the 2257 records-custodian (single line; line breaks won't render). Required for public adult sites."
KNOB_DEFAULT[NUXT_PUBLIC_COMPLIANCE_ADDRESS]=""
KNOB_SCOPE[NUXT_PUBLIC_COMPLIANCE_ADDRESS]="build"
KNOB_SECTION[NUXT_PUBLIC_COMPLIANCE_ADDRESS]="Legal compliance"

KNOB_DESCRIPTION[NUXT_PUBLIC_DMCA_AGENT_NAME]="Name (or 'DMCA Designated Agent') shown on /dmca. Must match the U.S. Copyright Office filing."
KNOB_DEFAULT[NUXT_PUBLIC_DMCA_AGENT_NAME]=""
KNOB_SCOPE[NUXT_PUBLIC_DMCA_AGENT_NAME]="build"
KNOB_SECTION[NUXT_PUBLIC_DMCA_AGENT_NAME]="Legal compliance"

KNOB_DESCRIPTION[NUXT_PUBLIC_DMCA_EMAIL]="Email for the DMCA designated agent on /dmca. Must match the U.S. Copyright Office filing."
KNOB_DEFAULT[NUXT_PUBLIC_DMCA_EMAIL]=""
KNOB_SCOPE[NUXT_PUBLIC_DMCA_EMAIL]="build"
KNOB_SECTION[NUXT_PUBLIC_DMCA_EMAIL]="Legal compliance"

KNOB_DESCRIPTION[NUXT_PUBLIC_DMCA_ADDRESS]="Postal address of the DMCA designated agent. Must match the U.S. Copyright Office filing."
KNOB_DEFAULT[NUXT_PUBLIC_DMCA_ADDRESS]=""
KNOB_SCOPE[NUXT_PUBLIC_DMCA_ADDRESS]="build"
KNOB_SECTION[NUXT_PUBLIC_DMCA_ADDRESS]="Legal compliance"

# ── Server runtime, logging, paths, auth & security knobs ────────────
# Registered with _knob for brevity: NAME SCOPE SECTION DEFAULT DESCRIPTION
# [sensitive]. Same arrays as the explicit blocks above; KNOB_ORDER (top of
# file) still decides position. Defaults are the server's own defaults in the
# knob's units — display/hint only (only values you type are forwarded).
_knob() {
  KNOB_SCOPE[$1]="$2"
  KNOB_SECTION[$1]="$3"
  KNOB_DEFAULT[$1]="$4"
  KNOB_DESCRIPTION[$1]="$5"
  if [[ "${6:-}" == "sensitive" ]]; then
    KNOB_SENSITIVE[$1]="true"
  fi
}

_knob SERVER_ENABLE_HTTPS runtime "Server" "false" "Serve TLS directly from the Go server (true | false). Leave false behind Caddy/Traefik/nginx, which terminate TLS. Needs SERVER_CERT_FILE and SERVER_KEY_FILE."
_knob SERVER_CERT_FILE runtime "Server" "" "TLS certificate (PEM) path for SERVER_ENABLE_HTTPS=true. Relative paths resolve against the deploy dir."
_knob SERVER_KEY_FILE runtime "Server" "" "TLS private key (PEM) path for SERVER_ENABLE_HTTPS=true."
_knob SERVER_READ_HEADER_TIMEOUT runtime "Server" "15" "Seconds a client may take to send request headers (slowloris guard). Does not limit request bodies."
_knob SERVER_READ_TIMEOUT runtime "Server" "0" "Seconds to read a whole request including its body. 0 = no limit — keep 0 so large uploads are not cut off."
_knob SERVER_WRITE_TIMEOUT runtime "Server" "0" "Seconds to write a whole response. 0 = no limit — keep 0: any non-zero value cuts long video streams off mid-playback."
_knob SERVER_IDLE_TIMEOUT runtime "Server" "120" "Seconds an idle keep-alive connection stays open."
_knob SERVER_SHUTDOWN_TIMEOUT runtime "Server" "30" "Seconds to drain connections and stop modules on stop/restart. Keep at or below the systemd TimeoutStopSec (30)."
_knob SERVER_MAX_HEADER_BYTES runtime "Server" "1048576" "Max request header size in bytes (1048576 = 1 MiB)."
_knob SERVER_MEMORY_LIMIT_PERCENT runtime "Server" "0" "Go soft memory limit as a percent of system RAM: 0 = auto (75%), or 10-95. Higher lets the server use more RAM as GC headroom (less CPU spent collecting); lower leaves more for ffmpeg, MySQL and the page cache. Ignored when GOMEMLIMIT is set."
_knob GOMEMLIMIT runtime "Server" "" "Expert override: absolute Go soft memory limit (e.g. 6GiB). Read by the Go runtime at process start (systemd EnvironmentFile); when set, the server's SERVER_MEMORY_LIMIT_PERCENT auto-tuning steps aside. Empty = let the server tune it."
_knob GOGC runtime "Server" "" "Expert override: Go GC target percent, read by the Go runtime at process start. Empty = the server picks 200 while it manages the memory limit (fewer collections, more RAM used)."

_knob LOG_FORMAT runtime "Logging" "text" "Log line format: text | json (json suits log shippers)."
_knob LOG_FILE_ENABLED runtime "Logging" "true" "Also write logs to files under LOGS_DIR (true | false). journald always receives them."
_knob LOG_COLOR_ENABLED runtime "Logging" "true" "ANSI colours in console logs (true | false). Set false if journalctl shows escape codes."
_knob LOG_FILE_ROTATION runtime "Logging" "true" "Rotate log files at LOG_MAX_FILE_SIZE (true | false)."
_knob LOG_MAX_FILE_SIZE runtime "Logging" "104857600" "Rotate a log file at this size in bytes (104857600 = 100 MiB)."
_knob LOG_MAX_BACKUPS runtime "Logging" "5" "Rotated log files to keep."

_knob VIDEOS_DIR runtime "Data paths" "" "Video library root. Empty = keep the VPS .env value, else ./videos under the deploy dir. HiDrive mounts are grafted inside it (HIDRIVE_LIBRARY_SUBDIR)."
_knob MUSIC_DIR runtime "Data paths" "" "Music library root. Empty = VPS .env value, else ./music."
_knob UPLOADS_DIR runtime "Data paths" "" "Where user uploads land. Empty = VPS .env value, else ./uploads."
_knob THUMBNAILS_DIR runtime "Data paths" "" "Generated thumbnails and preview frames. Empty = VPS .env value, else ./thumbnails."
_knob PLAYLISTS_DIR runtime "Data paths" "" "Playlist files. Empty = VPS .env value, else ./playlists."
_knob HLS_CACHE_DIR runtime "Data paths" "" "HLS transcode output — grows large, put it on the biggest disk. Empty = VPS .env value, else ./hls_cache."
_knob ANALYTICS_DIR runtime "Data paths" "" "Analytics data files. Empty = VPS .env value, else ./analytics."
_knob DATA_DIR runtime "Data paths" "" "Server state files. Empty = VPS .env value, else ./data."
_knob LOGS_DIR runtime "Data paths" "" "Log files (LOG_FILE_ENABLED). Empty = VPS .env value, else ./logs."
_knob TEMP_DIR runtime "Data paths" "" "Scratch space for uploads in progress. Empty = VPS .env value, else ./temp."
_knob BACKUP_DIR runtime "Data paths" "" "Where backup archives are written (point it at another disk for redundancy). Empty = VPS .env value, else DATA_DIR/backups. Archives already in the old location are not moved."

_knob ADMIN_SESSION_TIMEOUT_HOURS runtime "Admin login" "24" "Admin session lifetime in hours."
_knob ADMIN_QUERY_TIMEOUT_SECONDS runtime "Admin login" "30" "Timeout in seconds for admin-panel database queries."

_knob AUTH_MAX_LOGIN_ATTEMPTS runtime "Auth" "5" "Failed logins before an account is temporarily locked (>0)."
_knob AUTH_LOCKOUT_DURATION_MINUTES runtime "Auth" "15" "Minutes an account stays locked after AUTH_MAX_LOGIN_ATTEMPTS failures (>0)."
_knob AUTH_SECURE_COOKIES runtime "Auth" "false" "Mark session cookies Secure (HTTPS-only). Set true when the public site is HTTPS (e.g. TLS terminated by Caddy/Traefik)."
_knob AUTH_DEFAULT_USER_TYPE runtime "Auth" "standard" "User type given to new registrations: premium | standard | basic | guest (each caps concurrent streams, downloads, uploads, playlists)."

_knob CSP_POLICY runtime "HTTP security" "" "Full Content-Security-Policy value used when CSP_ENABLED=true. Empty = the built-in policy. Must contain at least one CSP directive or it is ignored."
_knob HSTS_MAX_AGE runtime "HTTP security" "31536000" "Strict-Transport-Security max-age in seconds when HSTS_ENABLED=true (31536000 = 1 year)."
_knob SECURITY_TRUSTED_PROXY_CIDRS runtime "HTTP security" "" "Comma-separated CIDRs of reverse proxies whose X-Forwarded-For is trusted for the real client IP (rate limits, bans, age-gate IP checks). Empty = built-in private + loopback ranges, which covers a proxy on the same host."

_knob SECURITY_BURST_LIMIT runtime "Rate limits" "60" "Requests per IP allowed within SECURITY_BURST_WINDOW_SECONDS before burst throttling (>0). HLS players fetch a segment every few seconds per viewer — keep headroom for several viewers behind one NAT."
_knob SECURITY_BURST_WINDOW_SECONDS runtime "Rate limits" "5" "Burst window in seconds (>0)."
_knob AUTH_RATE_LIMIT runtime "Rate limits" "20" "Login/registration requests per IP per minute (>0)."
_knob AUTH_BURST_LIMIT runtime "Rate limits" "5" "Burst allowance for the auth endpoints per IP (>0)."
_knob SECURITY_VIOLATIONS_FOR_BAN runtime "Rate limits" "10" "Rate-limit violations from one IP before it is temporarily banned (>0)."
_knob BAN_DURATION_MINUTES runtime "Rate limits" "15" "Minutes a banned IP stays banned (>0)."
_knob SECURITY_ENABLE_IP_WHITELIST runtime "Rate limits" "false" "Allow ONLY the IPs in SECURITY_IP_WHITELIST (true | false). Careful: locks everyone else out, including you."
_knob SECURITY_IP_WHITELIST runtime "Rate limits" "" "Comma-separated IPs/CIDRs allowed when the whitelist is enabled. Invalid entries are dropped."
_knob SECURITY_ENABLE_IP_BLACKLIST runtime "Rate limits" "false" "Block the IPs in SECURITY_IP_BLACKLIST (true | false)."
_knob SECURITY_IP_BLACKLIST runtime "Rate limits" "" "Comma-separated IPs/CIDRs blocked when the blacklist is enabled."
_knob SECURITY_MAX_FILE_SIZE_MB runtime "Rate limits" "0" "Refuse direct downloads of files larger than this many MB. 0 = no limit."

_knob AGE_GATE_IP_VERIFY_TTL_HOURS runtime "Age gate" "24" "Hours an IP stays verified after passing the age gate (covers browsers that drop the cookie)."
_knob AGE_GATE_COOKIE_NAME runtime "Age gate" "age_verified" "Name of the age-verification cookie."
_knob AGE_GATE_COOKIE_MAX_AGE runtime "Age gate" "31536000" "Age-verification cookie lifetime in seconds (31536000 = 1 year)."

# ── Feature flags ────────────────────────────────────────────────────
# FEATURE_* are the master switches: on every load syncFeatureToggles copies
# them onto the modules' own enabled flags, so module-level *_ENABLED values
# (HLS_ENABLED, UPLOADS_ENABLED, ADMIN_ENABLED, ...) never win.
_knob FEATURE_HLS runtime "Feature flags" "true" "HLS adaptive streaming and transcoding (true | false)."
_knob FEATURE_THUMBNAILS runtime "Feature flags" "true" "Thumbnail and preview-frame generation (true | false)."
_knob FEATURE_UPLOADS runtime "Feature flags" "true" "User uploads (true | false). This, not UPLOADS_ENABLED, is the switch the server honours."
_knob FEATURE_PLAYLISTS runtime "Feature flags" "true" "Playlists (true | false)."
_knob FEATURE_ANALYTICS runtime "Feature flags" "true" "View and playback analytics (true | false)."
_knob FEATURE_SUGGESTIONS runtime "Feature flags" "true" "Recommendation and similar-item rows (true | false)."
_knob FEATURE_AUTO_DISCOVERY runtime "Feature flags" "true" "Hourly media-library scan for new and removed files (true | false)."
_knob FEATURE_DUPLICATE_DETECTION runtime "Feature flags" "true" "Duplicate-file detection (true | false)."
_knob FEATURE_MATURE_SCANNER runtime "Feature flags" "true" "Keyword-based mature-content scanner (true | false). Tuned in the Mature scanner section."
_knob FEATURE_USER_AUTH runtime "Feature flags" "true" "User accounts and login (true | false). This, not AUTH_ENABLED, is the switch the server honours. Off = single-tenant mode."
_knob FEATURE_ADMIN_PANEL runtime "Feature flags" "true" "Admin panel (true | false). This, not ADMIN_ENABLED, is the switch the server honours."
_knob FEATURE_EXTRACTOR runtime "Feature flags" "false" "Extractor: proxy external M3U8/HLS streams as if they were library items (true | false)."

# ── Streaming / uploads ──────────────────────────────────────────────
_knob STREAMING_ADAPTIVE runtime "Streaming / uploads" "true" "Players switch to HLS automatically when it exists for an item (true | false). false = always direct play."
_knob STREAMING_REQUIRE_AUTH runtime "Streaming / uploads" "false" "Require login to play media, including HLS segments (true | false)."
_knob STREAMING_UNAUTH_STREAM_LIMIT runtime "Streaming / uploads" "3" "Max concurrent streams per IP for logged-out viewers. 0 = no limit."
_knob STREAMING_CHUNK_SIZE runtime "Streaming / uploads" "1048576" "Default bytes per direct-play read (1048576 = 1 MiB)."
_knob STREAMING_MAX_CHUNK_SIZE runtime "Streaming / uploads" "10485760" "Largest direct-play read in bytes (10485760 = 10 MiB)."
_knob STREAMING_BUFFER_SIZE runtime "Streaming / uploads" "1048576" "Per-stream I/O buffer in bytes. The effective buffer is the largest of this and the chunk sizes, capped at 64 MiB; read at startup."
_knob STREAMING_MOBILE_OPTIMIZATION runtime "Streaming / uploads" "true" "Use smaller reads (STREAMING_MOBILE_CHUNK_SIZE) for mobile clients (true | false)."
_knob STREAMING_MOBILE_CHUNK_SIZE runtime "Streaming / uploads" "524288" "Bytes per read for mobile clients (524288 = 512 KiB)."
_knob STREAMING_KEEP_ALIVE_ENABLED runtime "Streaming / uploads" "true" "Send a Keep-Alive header on media responses (true | false)."
_knob STREAMING_KEEP_ALIVE_TIMEOUT_SECONDS runtime "Streaming / uploads" "60" "Keep-Alive timeout advertised on media responses, in seconds."
_knob DOWNLOAD_REQUIRE_AUTH runtime "Streaming / uploads" "false" "Require login to download files (true | false)."
_knob DOWNLOAD_CHUNK_SIZE_KB runtime "Streaming / uploads" "512" "Download read size in KiB."
_knob UPLOADS_ALLOWED_EXTENSIONS runtime "Streaming / uploads" "" "Comma-separated allowed upload extensions including the dot (e.g. .mp4,.mkv,.mp3). Empty = built-in list of common video and audio formats."
_knob UPLOADS_REQUIRE_AUTH runtime "Streaming / uploads" "true" "Require login to upload (true | false)."
_knob UPLOADS_SCAN_FOR_MATURE runtime "Streaming / uploads" "false" "Run the mature-content scanner on new uploads (true | false)."

# ── HLS transcoding ──────────────────────────────────────────────────
_knob HLS_AUTO_GENERATE runtime "HLS transcoding" "false" "Generate HLS automatically — for the whole library in the background (most-viewed first, refilled each time a transcode finishes) and on first play (true | false). Off = only on an explicit Generate. CPU-heavy."
_knob HLS_CONCURRENT_LIMIT runtime "HLS transcoding" "0" "Transcodes allowed at once. 0 = auto (about one per 4 CPUs, 2-8; 2 with a hardware encoder). One slot is always kept for viewer-triggered jobs."
_knob HLS_HARDWARE_ACCEL runtime "HLS transcoding" "auto" "Video encoder: auto | none | nvenc | qsv | vaapi | videotoolbox. auto tries hardware and falls back to software libx264."
_knob HLS_QUALITIES runtime "HLS transcoding" "" "Comma-separated quality profiles to enable, e.g. 720p,480p (the others are disabled, not deleted). Built-in profiles: 1080p, 720p, 480p, 360p. Sources are never upscaled."
_knob HLS_SEGMENT_DURATION runtime "HLS transcoding" "6" "Seconds per HLS segment (1-60). Shorter = faster start and seeking, more requests."
_knob HLS_LAZY_TRANSCODE runtime "HLS transcoding" "false" "Transcode only the first quality up front, the others on first request (true | false). Saves CPU and disk on rarely watched items."
_knob HLS_PRE_GENERATE_INTERVAL_HOURS runtime "HLS transcoding" "1" "Hours between full background-generation sweeps (min 15 min). Sweeps also run whenever a transcode finishes, so this mostly matters right after a restart."
_knob HLS_MAX_CONSECUTIVE_FAILURES runtime "HLS transcoding" "3" "Failed attempts before an item stops being retried automatically (an admin Generate still retries it)."
_knob HLS_PROBE_TIMEOUT_SECONDS runtime "HLS transcoding" "30" "Seconds ffprobe may take to read a source's duration and resolution."
_knob HLS_STALE_LOCK_THRESHOLD_HOURS runtime "HLS transcoding" "2" "Hours after which a lock left by a crashed transcode is cleaned up. Running transcodes are never interrupted by this; a separate watchdog kills ffmpeg only when its output stops growing for 15 min."
_knob HLS_CDN_BASE_URL runtime "HLS transcoding" "" "Serve HLS segments from this base URL (a CDN in front of the server). Empty = same origin."
_knob HLS_CLEANUP_ENABLED runtime "HLS transcoding" "false" "Automatically delete HLS output not watched within HLS_RETENTION_MINUTES (true | false). Off = the cache is only deleted by an admin."
_knob HLS_CLEANUP_INTERVAL_MINUTES runtime "HLS transcoding" "60" "Minutes between cleanup passes when HLS_CLEANUP_ENABLED (the scheduler enforces at least 15)."
_knob HLS_RETENTION_MINUTES runtime "HLS transcoding" "60" "With HLS_CLEANUP_ENABLED, delete HLS output not accessed for this many minutes."

# ── Thumbnails ───────────────────────────────────────────────────────
_knob THUMBNAILS_AUTO_GENERATE runtime "Thumbnails" "true" "Generate thumbnails for new media in the background (true | false)."
_knob THUMBNAILS_GENERATE_ON_ACCESS runtime "Thumbnails" "true" "Generate a missing thumbnail the first time it is requested (true | false)."
_knob THUMBNAILS_WORKER_COUNT runtime "Thumbnails" "0" "Parallel thumbnail workers. 0 = auto (one per CPU, 4-16)."
_knob THUMBNAILS_QUEUE_SIZE runtime "Thumbnails" "1000" "Max queued thumbnail jobs (min 100). Each video queues 1 + THUMBNAILS_PREVIEW_COUNT jobs; raise it for big libraries so a scan backlog is not dropped."
_knob THUMBNAILS_WIDTH runtime "Thumbnails" "320" "Thumbnail width in px."
_knob THUMBNAILS_HEIGHT runtime "Thumbnails" "180" "Thumbnail height in px."
_knob THUMBNAILS_QUALITY runtime "Thumbnails" "80" "Image quality 1-100."
_knob THUMBNAILS_VIDEO_INTERVAL runtime "Thumbnails" "30" "Seconds into a video to grab the main thumbnail frame."
_knob THUMBNAILS_PREVIEW_COUNT runtime "Thumbnails" "10" "Seek-bar preview frames per video."
_knob THUMBNAILS_INFLIGHT_EVICTION_TIMEOUT_MINUTES runtime "Thumbnails" "5" "Minutes before a stuck in-flight thumbnail job is evicted so it can be retried (min 1)."
_knob THUMBNAILS_INFLIGHT_SCAN_INTERVAL_SECONDS runtime "Thumbnails" "60" "Seconds between checks for stuck in-flight jobs (min 1)."

# ── Analytics ────────────────────────────────────────────────────────
_knob ANALYTICS_TRACK_VIEWS runtime "Analytics" "true" "Count item views (true | false)."
_knob ANALYTICS_TRACK_PLAYBACK runtime "Analytics" "true" "Record playback events (true | false)."
_knob ANALYTICS_RETENTION_DAYS runtime "Analytics" "30" "Days of analytics events to keep."
_knob ANALYTICS_SESSION_TIMEOUT_MINUTES runtime "Analytics" "30" "Minutes of inactivity that end an analytics session."
_knob ANALYTICS_VIEW_COOLDOWN_MINUTES runtime "Analytics" "5" "Minutes before the same viewer counts as a new view of an item."
_knob ANALYTICS_CLEANUP_INTERVAL_MINUTES runtime "Analytics" "720" "Minutes between analytics retention passes (720 = 12 h)."
_knob ANALYTICS_MAX_RECONSTRUCT_EVENTS runtime "Analytics" "2000" "Max events replayed when rebuilding a session's stats (>0)."

# ── Mature-content scanner ───────────────────────────────────────────
_knob MATURE_SCANNER_AUTO_FLAG runtime "Mature scanner" "true" "Flag items as mature automatically on a high-confidence match (true | false)."
_knob MATURE_SCANNER_REQUIRE_REVIEW runtime "Mature scanner" "true" "Queue medium-confidence matches for admin review (true | false)."
_knob MATURE_SCANNER_HIGH_CONFIDENCE_THRESHOLD runtime "Mature scanner" "0.35" "Score (0-1) at or above which a match is high confidence."
_knob MATURE_SCANNER_MEDIUM_CONFIDENCE_THRESHOLD runtime "Mature scanner" "0.15" "Score (0-1) at or above which a match is medium confidence."
_knob MATURE_SCANNER_HIGH_CONFIDENCE_KEYWORDS runtime "Mature scanner" "" "Comma-separated keywords that strongly indicate mature content. Empty = built-in list (xxx, porn, adult, nsfw)."
_knob MATURE_SCANNER_MEDIUM_CONFIDENCE_KEYWORDS runtime "Mature scanner" "" "Comma-separated weaker keywords. Empty = built-in list (mature, explicit, 18+)."

# ── Federation: receiver (master side) ───────────────────────────────
_knob RECEIVER_API_KEY runtime "Federation" "" "API key(s) follower nodes present to this master, comma-separated for several. A changed value REPLACES the whole key list in config.json — include every peer's key. Generated by --setup-receiver / --fix-env." sensitive
_knob RECEIVER_MAX_PROXY_CONNS runtime "Federation" "50" "Max concurrent streams proxied from follower nodes."
_knob RECEIVER_PROXY_TIMEOUT_SECONDS runtime "Federation" "60" "Seconds to wait for a follower to start delivering a proxied stream."
_knob RECEIVER_HEALTH_CHECK_SECONDS runtime "Federation" "30" "Seconds between follower health checks."
_knob RECEIVER_WS_READ_LIMIT runtime "Federation" "16777216" "Max WebSocket message size from followers in bytes (16777216 = 16 MiB; big catalog pushes need room)."
_knob RECEIVER_WS_READ_DEADLINE_SECONDS runtime "Federation" "60" "Seconds without a frame before a follower connection is dropped."
_knob RECEIVER_WS_PING_INTERVAL_SECONDS runtime "Federation" "25" "Seconds between pings to followers (keep below the read deadline)."
_knob RECEIVER_PENDING_STREAM_TTL_SECONDS runtime "Federation" "30" "Seconds a requested follower stream may stay pending before it is abandoned."
_knob RECEIVER_HEARTBEAT_DB_DEBOUNCE_SECONDS runtime "Federation" "60" "Min seconds between persisting a follower heartbeat to the database."
# ── Federation: follower (this server pushes to a master) ────────────
_knob FOLLOWER_ENABLED runtime "Federation" "false" "Run as a follower: push this catalog to a master and serve its streams (true | false)."
_knob FOLLOWER_MASTER_URL runtime "Federation" "" "Master base URL, e.g. https://master.example.com (connects to <url>/ws/receiver)."
_knob FOLLOWER_API_KEY runtime "Federation" "" "Must match one of the master's RECEIVER_API_KEY values." sensitive
_knob FOLLOWER_SLAVE_ID runtime "Federation" "" "This follower's ID on the master. Empty = hostname."
_knob FOLLOWER_SLAVE_NAME runtime "Federation" "" "Display name in the master's admin UI. Empty = FOLLOWER_SLAVE_ID."
_knob FOLLOWER_SCAN_INTERVAL_SECONDS runtime "Federation" "300" "Seconds between catalog re-pushes to the master."
_knob FOLLOWER_HEARTBEAT_INTERVAL_SECONDS runtime "Federation" "15" "Seconds between heartbeats to the master."
_knob FOLLOWER_MAX_STREAMS runtime "Federation" "10" "Max concurrent streams this follower serves to the master."
_knob FOLLOWER_RECONNECT_BASE_SECONDS runtime "Federation" "2" "Initial reconnect backoff in seconds after losing the master."
_knob FOLLOWER_RECONNECT_MAX_SECONDS runtime "Federation" "120" "Max reconnect backoff in seconds."
# ── Federation: remote media / extractor ─────────────────────────────
_knob REMOTE_MEDIA_SYNC_INTERVAL_MINUTES runtime "Federation" "60" "Minutes between remote-source catalog syncs (FEATURE_REMOTE_MEDIA)."
_knob REMOTE_MEDIA_CACHE_ENABLED runtime "Federation" "true" "Cache remote media on local disk (true | false)."
_knob REMOTE_MEDIA_CACHE_SIZE runtime "Federation" "1073741824" "Remote-media disk cache budget in bytes (1073741824 = 1 GiB)."
_knob REMOTE_MEDIA_CACHE_TTL_HOURS runtime "Federation" "168" "Hours a cached remote file is kept (168 = 7 days)."
_knob REMOTE_MEDIA_HTTP_TIMEOUT_SECONDS runtime "Federation" "30" "HTTP timeout in seconds for remote-source requests."
_knob REMOTE_MEDIA_MAX_CONCURRENT_DOWNLOADS runtime "Federation" "4" "Parallel remote downloads."
_knob EXTRACTOR_PROXY_TIMEOUT_SECONDS runtime "Federation" "30" "Timeout in seconds for upstream requests made by the extractor (FEATURE_EXTRACTOR)."
_knob EXTRACTOR_MAX_ITEMS runtime "Federation" "500" "Max extracted stream items (0 = no limit)."

# ── Hugging Face extras ──────────────────────────────────────────────
_knob HUGGINGFACE_ENDPOINT_URL runtime "Hugging Face" "" "Custom inference endpoint URL. Empty = Hugging Face serverless inference."
_knob HUGGINGFACE_MAX_FRAMES runtime "Hugging Face" "3" "Video frames sampled per item for classification."
_knob HUGGINGFACE_TIMEOUT_SECS runtime "Hugging Face" "30" "Per-request timeout in seconds."
_knob HUGGINGFACE_RATE_LIMIT runtime "Hugging Face" "30" "Max classification requests per minute."
_knob HUGGINGFACE_MAX_CONCURRENT runtime "Hugging Face" "2" "Max parallel classification requests."

# ── Downloader extras ────────────────────────────────────────────────
_knob DOWNLOADER_IMPORT_DIR runtime "Downloader" "" "Default destination for imported downloads; must be inside a library root (videos, music or uploads). Empty = the uploads dir."
_knob DOWNLOADER_HEALTH_INTERVAL_SECONDS runtime "Downloader" "30" "Seconds between downloader health checks (>0)."
_knob DOWNLOADER_REQUEST_TIMEOUT_SECONDS runtime "Downloader" "30" "Timeout in seconds for requests to the downloader service (>0)."

# ── Database extras (read on every start, like the rest of Database) ─
_knob DATABASE_ENABLED runtime "Database" "true" "Use MySQL/MariaDB (true | false). Leave true — the server needs its database."
_knob DATABASE_MAX_OPEN_CONNS runtime "Database" "25" "Max open DB connections. Raise on busy sites (MySQL's max_connections defaults to 151)."
_knob DATABASE_MAX_IDLE_CONNS runtime "Database" "10" "Idle connections kept ready. Matching DATABASE_MAX_OPEN_CONNS avoids reconnect churn during traffic bursts."
_knob DATABASE_CONN_MAX_LIFETIME runtime "Database" "1h" "Recycle connections after this long (Go duration, e.g. 30m, 1h)."
_knob DATABASE_TIMEOUT runtime "Database" "10s" "Connection timeout (Go duration)."
_knob DATABASE_MAX_RETRIES runtime "Database" "3" "Connection attempts at startup before giving up."
_knob DATABASE_RETRY_INTERVAL runtime "Database" "2s" "Wait between startup connection attempts (Go duration)."
_knob DATABASE_SLOW_QUERY_THRESHOLD_MS runtime "Database" "500" "Log queries slower than this many milliseconds."
_knob DATABASE_RECOVERY_ARGS runtime "Database" "" "Space-separated arguments for DATABASE_RECOVERY_COMMAND."

# ── Object storage (read on every start) ─────────────────────────────
_knob STORAGE_BACKEND runtime "Object storage" "local" "Media storage backend: local | s3 (any S3-compatible service — Backblaze B2, Wasabi, MinIO, ...)."
_knob S3_ENDPOINT runtime "Object storage" "" "S3 endpoint URL (e.g. https://s3.us-west-004.backblazeb2.com)."
_knob S3_REGION runtime "Object storage" "" "S3 region."
_knob S3_BUCKET runtime "Object storage" "" "Bucket name."
_knob S3_ACCESS_KEY_ID runtime "Object storage" "" "S3 access key id." sensitive
_knob S3_SECRET_ACCESS_KEY runtime "Object storage" "" "S3 secret key." sensitive
_knob S3_USE_PATH_STYLE runtime "Object storage" "false" "Path-style bucket addressing (true for MinIO and most self-hosted S3)."

_knob BACKUP_RETENTION_COUNT runtime "Backups" "10" "Backups to keep; older ones are deleted."

_knob UI_ITEMS_PER_PAGE runtime "UI" "48" "Items per page in library grids."
_knob UI_MOBILE_ITEMS_PER_PAGE runtime "UI" "24" "Items per page on mobile."
_knob UI_MOBILE_GRID_COLUMNS runtime "UI" "2" "Grid columns on mobile."
_knob UI_FEED_DEFAULT_ITEMS runtime "UI" "20" "Entries in the Atom/RSS feed when no limit is requested."
_knob UI_FEED_MAX_ITEMS runtime "UI" "50" "Hard cap on Atom/RSS feed entries."

_knob UPDATER_BRANCH runtime "Updater" "main" "Git branch the in-app updater (admin panel) follows. deploy.sh deploys are unaffected."
_knob UPDATER_METHOD runtime "Updater" "source" "In-app update method: source | binary."
_knob UPDATER_APP_DIR runtime "Updater" "" "Checkout the in-app updater works in. Empty = the server's working dir."
_knob UPDATER_DEPLOY_KEY_PATH runtime "Updater" "" "SSH deploy key the in-app updater uses for git."
_knob UPDATER_GITHUB_USERNAME runtime "Updater" "" "GitHub username for HTTPS git auth (with UPDATER_GITHUB_TOKEN)."
_knob UPDATER_GITHUB_TOKEN runtime "Updater" "" "GitHub token the in-app updater uses for a private repo." sensitive
unset -f _knob

# ── Derived arrays for deploy.sh's payload builders ──────────────────
# FORWARDED_RUNTIME and FORWARDED_BUILD are the two arrays deploy.sh
# walks when generating the on-VPS env file and the npm-build env
# prefix. Knobs with scope=vps and scope=toolchain are consumed
# locally by deploy.sh and never forwarded.
FORWARDED_RUNTIME=()
FORWARDED_BUILD=()
for _knob in "${KNOB_ORDER[@]}"; do
  case "${KNOB_SCOPE[$_knob]:-}" in
    runtime) FORWARDED_RUNTIME+=("$_knob") ;;
    build)   FORWARDED_BUILD+=("$_knob") ;;
  esac
done
unset _knob
