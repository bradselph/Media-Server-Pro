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
#   build     — exported into the on-VPS shell that runs `npm run
#               build` for the Nuxt frontend. Baked into the bundle
#               (Nuxt runtimeConfig.public via NUXT_PUBLIC_*). Must
#               re-deploy to change.
#
# Sensitivity (KNOB_SENSITIVE=true) only affects display in the
# prompter — values are still stored verbatim in .deploy.env. The
# deploy machine is the trust boundary; rotate via the upstream
# provider (GitHub PAT, Hugging Face, Anthropic, etc.) if .deploy.env
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
  LOG_LEVEL
  # ── Admin login ──────────────────────────────────────────────────
  ADMIN_ENABLED
  ADMIN_USERNAME
  ADMIN_PASSWORD
  ADMIN_PASSWORD_HASH
  # ── Auth / public exposure ───────────────────────────────────────
  AUTH_ENABLED
  AUTH_ALLOW_GUESTS
  AUTH_ALLOW_REGISTRATION
  AUTH_SESSION_TIMEOUT_HOURS
  # ── HTTP security headers ────────────────────────────────────────
  CSP_ENABLED
  HSTS_ENABLED
  CORS_ENABLED
  CORS_ORIGINS
  # ── Rate limit ───────────────────────────────────────────────────
  RATE_LIMIT_ENABLED
  RATE_LIMIT_REQUESTS
  RATE_LIMIT_WINDOW_SECONDS
  # ── Age gate ─────────────────────────────────────────────────────
  AGE_GATE_ENABLED
  AGE_GATE_BYPASS_IPS
  # ── Streaming / uploads ──────────────────────────────────────────
  DOWNLOAD_ENABLED
  UPLOADS_ENABLED
  UPLOADS_MAX_FILE_SIZE
  # ── Feature flags ────────────────────────────────────────────────
  FEATURE_REMOTE_MEDIA
  FEATURE_RECEIVER
  RECEIVER_ENABLED
  # ── Hugging Face (mature content classification) ─────────────────
  FEATURE_HUGGINGFACE
  HUGGINGFACE_ENABLED
  HUGGINGFACE_API_KEY
  HUGGINGFACE_MODEL
  # ── Downloader integration ───────────────────────────────────────
  FEATURE_DOWNLOADER
  DOWNLOADER_ENABLED
  DOWNLOADER_URL
  DOWNLOADER_DOWNLOADS_DIR
  DOWNLOADER_INTERNAL_TOKEN
  # ── Claude assistant (admin-only) ────────────────────────────────
  FEATURE_CLAUDE
  ANTHROPIC_API_KEY
  CLAUDE_MODEL
  CLAUDE_MODE
  # ── Database ─────────────────────────────────────────────────────
  DATABASE_HOST
  DATABASE_PORT
  DATABASE_NAME
  DATABASE_USERNAME
  DATABASE_PASSWORD
  DATABASE_TLS_MODE
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
KNOB_DESCRIPTION[SERVER_PORT]="HTTP port the Go server binds on the VPS. Reverse proxy (Caddy/nginx) usually fronts this."
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
KNOB_DESCRIPTION[ADMIN_ENABLED]="Enable the admin login UI (true | false)."
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
KNOB_DESCRIPTION[AUTH_ENABLED]="Master switch for the user-auth system (true | false). Off = single-tenant mode."
KNOB_DEFAULT[AUTH_ENABLED]="true"
KNOB_SCOPE[AUTH_ENABLED]="runtime"
KNOB_SECTION[AUTH_ENABLED]="Auth"

KNOB_DESCRIPTION[AUTH_ALLOW_GUESTS]="Allow unauthenticated browse access (true | false)."
KNOB_DEFAULT[AUTH_ALLOW_GUESTS]="true"
KNOB_SCOPE[AUTH_ALLOW_GUESTS]="runtime"
KNOB_SECTION[AUTH_ALLOW_GUESTS]="Auth"

KNOB_DESCRIPTION[AUTH_ALLOW_REGISTRATION]="Allow public sign-up (true | false). Closed by default."
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

KNOB_DESCRIPTION[CORS_ORIGINS]="Comma-separated allowed origins or '*'. Tighten to your public origin in prod."
KNOB_DEFAULT[CORS_ORIGINS]="*"
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

KNOB_DESCRIPTION[UPLOADS_ENABLED]="Allow uploads at all (true | false). Per-user can_upload flag still gates."
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

KNOB_DESCRIPTION[RECEIVER_ENABLED]="Mirror of FEATURE_RECEIVER for older config readers. Keep these in sync."
KNOB_DEFAULT[RECEIVER_ENABLED]="true"
KNOB_SCOPE[RECEIVER_ENABLED]="runtime"
KNOB_SECTION[RECEIVER_ENABLED]="Federation"

# ── Hugging Face (mature content classification) ─────────────────────
KNOB_DESCRIPTION[FEATURE_HUGGINGFACE]="Enable HF visual-classification module (true | false)."
KNOB_DEFAULT[FEATURE_HUGGINGFACE]="false"
KNOB_SCOPE[FEATURE_HUGGINGFACE]="runtime"
KNOB_SECTION[FEATURE_HUGGINGFACE]="Hugging Face"

KNOB_DESCRIPTION[HUGGINGFACE_ENABLED]="Legacy mirror of FEATURE_HUGGINGFACE. Keep these in sync."
KNOB_DEFAULT[HUGGINGFACE_ENABLED]="false"
KNOB_SCOPE[HUGGINGFACE_ENABLED]="runtime"
KNOB_SECTION[HUGGINGFACE_ENABLED]="Hugging Face"

KNOB_DESCRIPTION[HUGGINGFACE_API_KEY]="Hugging Face API token (hf_...). Read scope is enough. https://huggingface.co/settings/tokens"
KNOB_DEFAULT[HUGGINGFACE_API_KEY]=""
KNOB_SCOPE[HUGGINGFACE_API_KEY]="runtime"
KNOB_SECTION[HUGGINGFACE_API_KEY]="Hugging Face"
KNOB_SENSITIVE[HUGGINGFACE_API_KEY]="true"

KNOB_DESCRIPTION[HUGGINGFACE_MODEL]="HF model id used for image captioning."
KNOB_DEFAULT[HUGGINGFACE_MODEL]="Salesforce/blip-image-captioning-large"
KNOB_SCOPE[HUGGINGFACE_MODEL]="runtime"
KNOB_SECTION[HUGGINGFACE_MODEL]="Hugging Face"

# ── Downloader integration ───────────────────────────────────────────
KNOB_DESCRIPTION[FEATURE_DOWNLOADER]="Show the Downloader tab and route requests to it (true | false)."
KNOB_DEFAULT[FEATURE_DOWNLOADER]="false"
KNOB_SCOPE[FEATURE_DOWNLOADER]="runtime"
KNOB_SECTION[FEATURE_DOWNLOADER]="Downloader"

KNOB_DESCRIPTION[DOWNLOADER_ENABLED]="Backend toggle for the downloader proxy (true | false). Mirrors FEATURE_DOWNLOADER."
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

# ── Claude assistant (admin-only) ────────────────────────────────────
KNOB_DESCRIPTION[FEATURE_CLAUDE]="Enable the Claude admin assistant module (true | false). Admin-only."
KNOB_DEFAULT[FEATURE_CLAUDE]="false"
KNOB_SCOPE[FEATURE_CLAUDE]="runtime"
KNOB_SECTION[FEATURE_CLAUDE]="Claude assistant"

KNOB_DESCRIPTION[ANTHROPIC_API_KEY]="Anthropic API key (sk-ant-...). Optional when the host's claude CLI is logged in."
KNOB_DEFAULT[ANTHROPIC_API_KEY]=""
KNOB_SCOPE[ANTHROPIC_API_KEY]="runtime"
KNOB_SECTION[ANTHROPIC_API_KEY]="Claude assistant"
KNOB_SENSITIVE[ANTHROPIC_API_KEY]="true"

KNOB_DESCRIPTION[CLAUDE_MODEL]="Anthropic model id used by the admin assistant."
KNOB_DEFAULT[CLAUDE_MODEL]="claude-sonnet-4-6"
KNOB_SCOPE[CLAUDE_MODEL]="runtime"
KNOB_SECTION[CLAUDE_MODEL]="Claude assistant"

KNOB_DESCRIPTION[CLAUDE_MODE]="advisory | interactive | autonomous."
KNOB_DEFAULT[CLAUDE_MODE]="autonomous"
KNOB_SCOPE[CLAUDE_MODE]="runtime"
KNOB_SECTION[CLAUDE_MODE]="Claude assistant"

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
