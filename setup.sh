#!/usr/bin/env bash
# setup.sh — Interactive first-time setup wizard for Media Server Pro.
#
# Creates configuration files and optionally deploys:
#   .deploy.env — VPS connection + GitHub credentials (local, gitignored)
#   .env        — Full server configuration (deployed to VPS)
#   .slave.env  — Slave node configuration (local, gitignored, optional)
#
# Usage:
#   ./setup.sh          # run the interactive setup wizard
#   ./setup.sh --help   # show this help

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[setup]${RESET} $*"; }
success() { echo -e "${GREEN}[setup]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[setup]${RESET} $*"; }
die()     { echo -e "${RED}[setup] ERROR:${RESET} $*" >&2; exit 1; }
section() { echo ""; echo -e "${BOLD}--- $* ---${RESET}"; }

# ── Input helpers ─────────────────────────────────────────────────────────────
# prompt VARNAME "text" [default]
prompt() {
  local varname="$1" text="$2" default="${3:-}"
  local input
  if [[ -n "$default" ]]; then
    read -rp "  $text [$default]: " input
    printf -v "$varname" '%s' "${input:-$default}"
  else
    while true; do
      read -rp "  $text: " input
      if [[ -n "$input" ]]; then
        printf -v "$varname" '%s' "$input"
        return
      fi
      warn "This field is required."
    done
  fi
}

# prompt_secret VARNAME "text" [default]
prompt_secret() {
  local varname="$1" text="$2" default="${3:-}"
  local input
  if [[ -n "$default" ]]; then
    read -rsp "  $text [****]: " input; echo
    printf -v "$varname" '%s' "${input:-$default}"
  else
    while true; do
      read -rsp "  $text: " input; echo
      if [[ -n "$input" ]]; then
        printf -v "$varname" '%s' "$input"
        return
      fi
      warn "This field is required."
    done
  fi
}

# prompt_yn VARNAME "text" default_letter(y/n)
prompt_yn() {
  local varname="$1" text="$2" default="$3"
  local input display
  if [[ "${default,,}" == "y" ]]; then display="Y/n"; else display="y/N"; fi
  read -rp "  $text [$display]: " input
  input="${input:-$default}"
  case "${input,,}" in
    y|yes) printf -v "$varname" '%s' "true" ;;
    *)     printf -v "$varname" '%s' "false" ;;
  esac
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if [[ "${1:-}" == "--help" ]] || [[ "${1:-}" == "-h" ]]; then
  sed -n '/^# Usage:/,/^$/p' "$0"
  exit 0
fi

# ══════════════════════════════════════════════════════════════════════════════
# Banner
# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo -e "${BOLD}================================================================${RESET}"
echo -e "${BOLD}          Media Server Pro  —  Setup Wizard${RESET}"
echo -e "${BOLD}================================================================${RESET}"
echo ""
echo -e "  This wizard creates all configuration files and optionally"
echo -e "  provisions and deploys your server."
echo ""

# ══════════════════════════════════════════════════════════════════════════════
# Pre-flight checks — verify local tools required by the setup/deploy process
# ══════════════════════════════════════════════════════════════════════════════
PREFLIGHT_MISSING=()
for cmd in ssh scp bash; do
  command -v "$cmd" &>/dev/null || PREFLIGHT_MISSING+=("$cmd")
done

if [[ ${#PREFLIGHT_MISSING[@]} -gt 0 ]]; then
  die "Required tools not found: ${PREFLIGHT_MISSING[*]}. Please install them first."
fi

# openssl is optional — used for API key generation, with fallbacks
if ! command -v openssl &>/dev/null; then
  warn "openssl not found — API keys will be generated using a fallback method."
fi

# ══════════════════════════════════════════════════════════════════════════════
# Mode selection
# ══════════════════════════════════════════════════════════════════════════════
echo -e "  ${BOLD}What would you like to set up?${RESET}"
echo ""
echo -e "    ${CYAN}1)${RESET} Master Server  — deploy to a Linux VPS"
echo -e "    ${CYAN}2)${RESET} Slave Node     — connect local media to an existing master"
echo -e "    ${CYAN}3)${RESET} Both           — master server + slave node"
echo ""
read -rp "  Choose [1]: " SETUP_MODE
SETUP_MODE="${SETUP_MODE:-1}"

SETUP_MASTER=false
SETUP_SLAVE=false
case "$SETUP_MODE" in
  1) SETUP_MASTER=true ;;
  2) SETUP_SLAVE=true ;;
  3) SETUP_MASTER=true; SETUP_SLAVE=true ;;
  *) die "Invalid choice: $SETUP_MODE" ;;
esac

# ══════════════════════════════════════════════════════════════════════════════
# Master Server Setup
# ══════════════════════════════════════════════════════════════════════════════
if $SETUP_MASTER; then

  # ── VPS Connection ──────────────────────────────────────────────────────────
  section "VPS Connection"
  echo -e "  ${DIM}SSH credentials for your Linux server.${RESET}"
  echo ""

  prompt VPS_HOST    "VPS hostname or IP"
  prompt VPS_USER    "SSH user"                "root"
  prompt VPS_PORT    "SSH port"                "22"
  prompt KEY_FILE    "SSH private key path"    "$HOME/.ssh/id_ed25519"
  prompt DEPLOY_DIR  "Deploy directory on VPS" "/opt/media-server"
  prompt SERVICE     "Systemd service name"    "media-server"

  # ── Repository ──────────────────────────────────────────────────────────────
  section "Repository"
  echo -e "  ${DIM}GitHub access for cloning the repository on the VPS.${RESET}"
  echo ""

  prompt_secret GITHUB_TOKEN  "GitHub Personal Access Token"
  prompt        REPO_URL      "Repository URL (without https://)" "github.com/bradselph/Media-Server-Pro.git"
  prompt        DEPLOY_BRANCH "Branch to deploy"                  "main"

  # ── Write .deploy.env ──────────────────────────────────────────────────────
  info "Writing .deploy.env..."
  cat > "$SCRIPT_DIR/.deploy.env" <<EOF
# Media Server Pro — Deployment Configuration
# Generated by setup.sh on $(date)

VPS_HOST=$VPS_HOST
VPS_USER=$VPS_USER
VPS_PORT=$VPS_PORT
KEY_FILE=$KEY_FILE
DEPLOY_DIR=$DEPLOY_DIR
SERVICE=$SERVICE

GITHUB_TOKEN=$GITHUB_TOKEN
REPO_URL=$REPO_URL
UPDATER_BRANCH=$DEPLOY_BRANCH
EOF
  success ".deploy.env written"

  # ── Database ────────────────────────────────────────────────────────────────
  section "Database (MySQL)"
  echo -e "  ${DIM}MySQL connection details. The database must already exist.${RESET}"
  echo ""

  prompt        DB_HOST "Database host"     "localhost"
  prompt        DB_PORT "Database port"     "3306"
  prompt        DB_NAME "Database name"     "mediaserver"
  prompt        DB_USER "Database username" "mediaserver"
  prompt_secret DB_PASS "Database password"

  DB_TLS=""
  if [[ "$DB_HOST" != "localhost" ]] && [[ "$DB_HOST" != "127.0.0.1" ]]; then
    DB_TLS="skip-verify"
    info "Remote database detected — TLS mode set to skip-verify"
  fi

  # ── Server ──────────────────────────────────────────────────────────────────
  section "Server"

  prompt    SRV_PORT  "Server port"                    "8080"
  prompt    SRV_HOST  "Bind address"                   "0.0.0.0"
  prompt_yn SRV_HTTPS "Enable HTTPS (requires certs)?" "n"

  SRV_CERT=""
  SRV_KEY=""
  if [[ "$SRV_HTTPS" == "true" ]]; then
    prompt SRV_CERT "TLS certificate file path"
    prompt SRV_KEY  "TLS private key file path"
  fi

  # ── Admin Account ──────────────────────────────────────────────────────────
  section "Admin Account"

  prompt        ADMIN_USER "Admin username" "admin"
  prompt_secret ADMIN_PASS "Admin password"

  # ── Features ────────────────────────────────────────────────────────────────
  section "Features"
  echo -e "  ${DIM}Enable or disable major features.${RESET}"
  echo ""

  prompt_yn FEAT_HLS         "HLS adaptive streaming?"          "y"
  prompt_yn FEAT_ANALYTICS   "Analytics and watch tracking?"    "y"
  prompt_yn FEAT_UPLOADS     "File uploads?"                    "y"
  prompt_yn FEAT_SUGGESTIONS "Content suggestions?"             "y"
  prompt_yn FEAT_RECEIVER    "Receiver (accept slave nodes)?"   "n"
  prompt_yn FEAT_REMOTE      "Remote media proxy?"              "n"

  RECV_API_KEY=""
  if [[ "$FEAT_RECEIVER" == "true" ]]; then
    RECV_API_KEY=$(openssl rand -hex 32 2>/dev/null \
      || python3 -c "import secrets; print(secrets.token_hex(32))" 2>/dev/null \
      || date +%s%N | sha256sum | head -c 64)
    success "Generated receiver API key"
    # Save to .deploy.env so slave setup can auto-fill
    echo "" >> "$SCRIPT_DIR/.deploy.env"
    echo "RECEIVER_API_KEY=$RECV_API_KEY" >> "$SCRIPT_DIR/.deploy.env"
  fi

  # ── Authentication ──────────────────────────────────────────────────────────
  section "Authentication"

  prompt_yn AUTH_GUEST  "Allow guest access (browse without login)?" "y"

  # ── Logging ─────────────────────────────────────────────────────────────────
  section "Logging"

  prompt LOG_LEVEL "Log level (debug / info / warn / error)" "info"

  # ══════════════════════════════════════════════════════════════════════════
  # Generate .env content
  # ══════════════════════════════════════════════════════════════════════════
  info "Generating server configuration..."

  ENV_TEMP="$SCRIPT_DIR/.env.generated"
  cat > "$ENV_TEMP" <<ENVFILE
# ═══════════════════════════════════════════════════════════════
#  Media Server Pro — Server Configuration
#  Generated by setup.sh on $(date)
# ═══════════════════════════════════════════════════════════════

# ── Server ────────────────────────────────────────────────────
SERVER_HOST=$SRV_HOST
SERVER_PORT=$SRV_PORT
SERVER_READ_TIMEOUT=30
SERVER_WRITE_TIMEOUT=60
SERVER_IDLE_TIMEOUT=120
SERVER_SHUTDOWN_TIMEOUT=30
SERVER_MAX_HEADER_BYTES=1048576
SERVER_ENABLE_HTTPS=$SRV_HTTPS
SERVER_CERT_FILE=$SRV_CERT
SERVER_KEY_FILE=$SRV_KEY

# ── Directories ───────────────────────────────────────────────
VIDEOS_DIR=./videos
MUSIC_DIR=./music
THUMBNAILS_DIR=./thumbnails
PLAYLISTS_DIR=./playlists
UPLOADS_DIR=./uploads
ANALYTICS_DIR=./analytics
HLS_CACHE_DIR=./cache/hls
DATA_DIR=./data
LOGS_DIR=./logs
TEMP_DIR=./temp
BACKUP_DIR=./backups

# ── Database (MySQL) ─────────────────────────────────────────
DATABASE_ENABLED=true
DATABASE_HOST=$DB_HOST
DATABASE_PORT=$DB_PORT
DATABASE_NAME=$DB_NAME
DATABASE_USERNAME=$DB_USER
DATABASE_PASSWORD=$DB_PASS
DATABASE_TLS_MODE=$DB_TLS
DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=10
DATABASE_CONN_MAX_LIFETIME=1h
DATABASE_TIMEOUT=10s
DATABASE_MAX_RETRIES=3
DATABASE_RETRY_INTERVAL=2s

# ── Streaming ─────────────────────────────────────────────────
STREAMING_CHUNK_SIZE=1048576
STREAMING_MOBILE_OPTIMIZATION=true
DOWNLOAD_ENABLED=true

# ── HLS ───────────────────────────────────────────────────────
HLS_ENABLED=$FEAT_HLS
HLS_SEGMENT_DURATION=6
HLS_CONCURRENT_LIMIT=2
HLS_AUTO_GENERATE=false
HLS_QUALITIES=480p,720p,1080p
HLS_CDN_BASE_URL=
HLS_LAZY_TRANSCODE=false

# ── Thumbnails ────────────────────────────────────────────────
THUMBNAILS_ENABLED=true
THUMBNAILS_AUTO_GENERATE=true
THUMBNAILS_WIDTH=320
THUMBNAILS_HEIGHT=180
THUMBNAILS_PREVIEW_COUNT=5

# ── Analytics ─────────────────────────────────────────────────
ANALYTICS_ENABLED=$FEAT_ANALYTICS
ANALYTICS_RETENTION_DAYS=90

# ── Uploads ───────────────────────────────────────────────────
UPLOADS_ENABLED=$FEAT_UPLOADS
UPLOADS_MAX_FILE_SIZE=10737418240
UPLOADS_ALLOWED_EXTENSIONS=.mp4,.mkv,.webm,.avi,.mov,.mp3,.flac,.wav,.aac,.ogg

# ── Security ──────────────────────────────────────────────────
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW_SECONDS=60
CORS_ENABLED=true
CORS_ORIGINS=*
CSP_ENABLED=false
CSP_POLICY=
HSTS_ENABLED=false
HSTS_MAX_AGE=31536000
SECURITY_ENABLE_IP_WHITELIST=false
SECURITY_ENABLE_IP_BLACKLIST=false
SECURITY_IP_WHITELIST=
SECURITY_IP_BLACKLIST=

# ── Authentication ────────────────────────────────────────────
AUTH_SESSION_TIMEOUT_HOURS=24
AUTH_MAX_LOGIN_ATTEMPTS=5
AUTH_LOCKOUT_DURATION_MINUTES=15
AUTH_ALLOW_GUESTS=$AUTH_GUEST
AUTH_SECURE_COOKIES=$SRV_HTTPS

# ── Admin ─────────────────────────────────────────────────────
ADMIN_ENABLED=true
ADMIN_USERNAME=$ADMIN_USER
ADMIN_PASSWORD=$ADMIN_PASS

# ── Age Gate ──────────────────────────────────────────────────
AGE_GATE_ENABLED=false
AGE_GATE_COOKIE_NAME=age_verified
AGE_GATE_COOKIE_MAX_AGE=31536000
AGE_GATE_IP_VERIFY_TTL_HOURS=24
AGE_GATE_BYPASS_IPS=127.0.0.1,::1

# ── Mature Content Scanner ───────────────────────────────────
MATURE_SCANNER_ENABLED=true
MATURE_SCANNER_HIGH_CONFIDENCE_THRESHOLD=0.85
MATURE_SCANNER_AUTO_FLAG=false
MATURE_SCANNER_REQUIRE_REVIEW=true

# ── Logging ───────────────────────────────────────────────────
LOG_LEVEL=$LOG_LEVEL
LOG_FORMAT=text
LOG_FILE_ENABLED=true
LOG_MAX_FILE_SIZE=104857600
LOG_MAX_BACKUPS=5

# ── Features ──────────────────────────────────────────────────
FEATURE_HLS=$FEAT_HLS
FEATURE_ANALYTICS=$FEAT_ANALYTICS
FEATURE_UPLOADS=$FEAT_UPLOADS
FEATURE_ADMIN_PANEL=true
FEATURE_SUGGESTIONS=$FEAT_SUGGESTIONS
FEATURE_REMOTE_MEDIA=$FEAT_REMOTE
FEATURE_RECEIVER=$FEAT_RECEIVER
FEATURE_PLAYLISTS=true
FEATURE_THUMBNAILS=true
FEATURE_USER_AUTH=true
FEATURE_MATURE_SCANNER=true
FEATURE_AUTO_DISCOVERY=true
FEATURE_DUPLICATE_DETECTION=true
FEATURE_EXTRACTOR=false
FEATURE_CRAWLER=false

# ── Updater ───────────────────────────────────────────────────
UPDATER_GITHUB_TOKEN=
UPDATER_BRANCH=$DEPLOY_BRANCH
UPDATER_METHOD=source

# ── Backup ───────────────────────────────────────────────────
BACKUP_RETENTION_COUNT=10

# ── Remote Media ──────────────────────────────────────────────
REMOTE_MEDIA_ENABLED=$FEAT_REMOTE
REMOTE_MEDIA_CACHE_ENABLED=true
REMOTE_MEDIA_CACHE_SIZE_MB=1024

# ── Receiver ──────────────────────────────────────────────────
RECEIVER_ENABLED=$FEAT_RECEIVER
RECEIVER_API_KEYS=$RECV_API_KEY
ENVFILE
  success "Server configuration generated"

  # ══════════════════════════════════════════════════════════════════════════
  # Deploy
  # ══════════════════════════════════════════════════════════════════════════
  echo ""
  echo -e "${BOLD}Configuration complete. Ready to deploy.${RESET}"
  echo ""
  prompt_yn DO_DEPLOY "Run first-time VPS setup and deploy now?" "y"

  if [[ "$DO_DEPLOY" == "true" ]]; then
    # Verify deploy.sh exists
    [[ -f "$SCRIPT_DIR/deploy.sh" ]] || die "deploy.sh not found in $SCRIPT_DIR"
    command -v ssh &>/dev/null || die "ssh not found. Please install OpenSSH."
    command -v scp &>/dev/null || die "scp not found. Please install OpenSSH."

    echo ""
    info "Step 1/3: Provisioning VPS..."
    echo ""
    bash "$SCRIPT_DIR/deploy.sh" --setup --branch "$DEPLOY_BRANCH"

    # Upload the generated .env (overwrites the template copied by --setup)
    info "Step 2/3: Uploading server configuration..."

    KEY_FILE_SSH="$KEY_FILE"
    if command -v cygpath &>/dev/null 2>&1; then
      KEY_FILE_SSH="$(cygpath -m "$KEY_FILE" 2>/dev/null || echo "$KEY_FILE")"
    fi
    SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$VPS_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8)
    SCP_OPTS=(-i "$KEY_FILE_SSH" -P "$VPS_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes)

    scp "${SCP_OPTS[@]}" "$ENV_TEMP" "$VPS_USER@$VPS_HOST:/tmp/.env.msp"
    ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" -- "
      sudo mv /tmp/.env.msp '$DEPLOY_DIR/.env'
      sudo chmod 600 '$DEPLOY_DIR/.env'
      sudo chown mediaserver:mediaserver '$DEPLOY_DIR/.env'
    "
    success "Server configuration uploaded to $DEPLOY_DIR/.env"

    # Ensure our receiver API key is correct in .deploy.env
    # (deploy.sh --setup may have written its own)
    if [[ -n "$RECV_API_KEY" ]]; then
      # Re-write .deploy.env cleanly
      cat > "$SCRIPT_DIR/.deploy.env" <<EOF
VPS_HOST=$VPS_HOST
VPS_USER=$VPS_USER
VPS_PORT=$VPS_PORT
KEY_FILE=$KEY_FILE
DEPLOY_DIR=$DEPLOY_DIR
SERVICE=$SERVICE
GITHUB_TOKEN=$GITHUB_TOKEN
REPO_URL=$REPO_URL
UPDATER_BRANCH=$DEPLOY_BRANCH
RECEIVER_API_KEY=$RECV_API_KEY
EOF
    fi

    echo ""
    info "Step 3/3: Building and deploying..."
    echo ""
    bash "$SCRIPT_DIR/deploy.sh" --branch "$DEPLOY_BRANCH"

    echo ""
    success "Master server deployment complete!"

    # Save MASTER_URL for slave setup
    if [[ "$FEAT_RECEIVER" == "true" ]]; then
      if [[ "$SRV_HTTPS" == "true" ]]; then
        MASTER_URL="https://$VPS_HOST"
      else
        MASTER_URL="http://$VPS_HOST:$SRV_PORT"
      fi
      echo "MASTER_URL=$MASTER_URL" >> "$SCRIPT_DIR/.deploy.env"
      echo ""
      warn "MASTER_URL saved as $MASTER_URL"
      warn "If behind a reverse proxy, edit .deploy.env with your public domain"
    fi

    rm -f "$ENV_TEMP"
  else
    info "Skipping deployment. Generated .env saved to: $ENV_TEMP"
    info ""
    info "To deploy later:"
    info "  1. ./deploy.sh --setup"
    info "  2. scp $ENV_TEMP your-vps:$DEPLOY_DIR/.env"
    info "  3. ./deploy.sh"
  fi
fi

# ══════════════════════════════════════════════════════════════════════════════
# Ask about slave if master was set up but slave wasn't selected
# ══════════════════════════════════════════════════════════════════════════════
if ! $SETUP_SLAVE && $SETUP_MASTER; then
  echo ""
  prompt_yn SETUP_SLAVE "Would you like to set up a slave node?" "n"
fi

# ══════════════════════════════════════════════════════════════════════════════
# Slave Node Setup
# ══════════════════════════════════════════════════════════════════════════════
if $SETUP_SLAVE; then
  section "Slave Node"
  echo -e "  ${DIM}A slave node sends its local media catalog to the master.${RESET}"
  echo ""

  # Auto-fill from .deploy.env if available
  DEFAULT_MASTER=""
  DEFAULT_KEY=""
  if [[ -f "$SCRIPT_DIR/.deploy.env" ]]; then
    source "$SCRIPT_DIR/.deploy.env"
    DEFAULT_MASTER="${MASTER_URL:-}"
    DEFAULT_KEY="${RECEIVER_API_KEY:-}"
  fi

  if [[ -n "$DEFAULT_MASTER" ]]; then
    prompt SLAVE_MASTER_URL "Master server URL" "$DEFAULT_MASTER"
  else
    prompt SLAVE_MASTER_URL "Master server URL (e.g. https://yourdomain.com)"
  fi

  if [[ -n "$DEFAULT_KEY" ]]; then
    prompt_secret SLAVE_API_KEY "Receiver API key" "$DEFAULT_KEY"
  else
    prompt_secret SLAVE_API_KEY "Receiver API key"
  fi

  prompt SLAVE_MEDIA_DIRS "Media directories (comma-separated)"
  prompt SLAVE_ID         "Slave ID (unique identifier)" "$(hostname -s 2>/dev/null || hostname)"
  prompt SLAVE_NAME       "Slave display name"           "$SLAVE_ID"

  echo ""
  echo -e "  ${BOLD}How will this slave run?${RESET}"
  echo ""
  echo -e "    ${CYAN}1)${RESET} Local  — run on this machine"
  echo -e "    ${CYAN}2)${RESET} Remote — deploy to another Linux device via SSH"
  echo ""
  read -rp "  Choose [1]: " SLAVE_RUN_MODE
  SLAVE_RUN_MODE="${SLAVE_RUN_MODE:-1}"
  SLAVE_LOCAL=true

  if [[ "$SLAVE_RUN_MODE" == "2" ]]; then
    SLAVE_LOCAL=false
    section "Slave Device Connection"
    prompt SLAVE_HOST "Slave hostname or IP"
    prompt SLAVE_USER "SSH user"                   "pi"
    prompt SLAVE_PORT "SSH port"                   "22"
    prompt SLAVE_DIR  "Install directory on slave" "/opt/media-receiver"
  fi

  # ── Write .slave.env ────────────────────────────────────────────────────────
  info "Writing .slave.env..."
  cat > "$SCRIPT_DIR/.slave.env" <<EOF
# Media Server Pro — Slave Node Configuration
# Generated by setup.sh on $(date)

SLAVE_ID=$SLAVE_ID
SLAVE_NAME=$SLAVE_NAME
MEDIA_DIRS=$SLAVE_MEDIA_DIRS
MASTER_URL=$SLAVE_MASTER_URL
RECEIVER_API_KEY=$SLAVE_API_KEY
SCAN_INTERVAL=5m
HEARTBEAT_INTERVAL=15s
EOF

  if ! $SLAVE_LOCAL; then
    cat >> "$SCRIPT_DIR/.slave.env" <<EOF

# Remote slave device
SLAVE_HOST=$SLAVE_HOST
SLAVE_USER=$SLAVE_USER
SLAVE_PORT=$SLAVE_PORT
SLAVE_DIR=$SLAVE_DIR
EOF
  fi
  success ".slave.env written"

  # ── Deploy slave ────────────────────────────────────────────────────────────
  echo ""
  prompt_yn DO_SLAVE_DEPLOY "Deploy the slave node now?" "y"

  if [[ "$DO_SLAVE_DEPLOY" == "true" ]]; then
    [[ -f "$SCRIPT_DIR/deploy.sh" ]] || die "deploy.sh not found in $SCRIPT_DIR"

    if $SLAVE_LOCAL; then
      info "Starting local slave node..."
      echo ""
      bash "$SCRIPT_DIR/deploy.sh" --slave --local
    else
      info "Running first-time slave setup on $SLAVE_HOST..."
      echo ""
      bash "$SCRIPT_DIR/deploy.sh" --slave --setup
    fi
  else
    info "Skipping slave deployment. To deploy later:"
    if $SLAVE_LOCAL; then
      info "  ./deploy.sh --slave --local"
    else
      info "  ./deploy.sh --slave --setup"
    fi
  fi
fi

# ══════════════════════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo -e "${BOLD}================================================================${RESET}"
echo -e "${BOLD}                    Setup Complete${RESET}"
echo -e "${BOLD}================================================================${RESET}"
echo ""

if $SETUP_MASTER; then
  echo -e "  ${GREEN}Master Server${RESET}"
  echo -e "    Config  : .deploy.env"
  echo -e "    Deploy  : ./deploy.sh"
  echo -e "    Setup   : ./deploy.sh --setup"
  echo -e "    Rollback: ./deploy.sh --rollback"
  echo ""
fi

if $SETUP_SLAVE; then
  echo -e "  ${GREEN}Slave Node${RESET}"
  echo -e "    Config  : .slave.env"
  if $SLAVE_LOCAL; then
    echo -e "    Start   : ./deploy.sh --slave --local"
    echo -e "    Stop    : ./deploy.sh --slave --local --stop"
  else
    echo -e "    Deploy  : ./deploy.sh --slave"
    echo -e "    Setup   : ./deploy.sh --slave --setup"
    echo -e "    Rollback: ./deploy.sh --slave --rollback"
  fi
  echo ""
fi

echo -e "  ${DIM}Edit .deploy.env or .slave.env to adjust settings.${RESET}"
echo -e "  ${DIM}Re-run ./setup.sh at any time to reconfigure.${RESET}"
echo ""
