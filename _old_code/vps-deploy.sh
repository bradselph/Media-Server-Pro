#!/usr/bin/env bash
# vps-deploy.sh — Pull latest code, build, and restart the service on the VPS.
# Runs entirely remotely over SSH — no local build required.
# Auto-generates SSH key and installs it on the VPS if needed.
#
# Usage:
#   ./vps-deploy.sh                         # pull current branch → build (no React) → restart
#   ./vps-deploy.sh --branch development    # switch to development branch → pull → build → restart
#   ./vps-deploy.sh --branch main           # switch to main branch → pull → build → restart
#   ./vps-deploy.sh --full                  # pull → build WITH React → restart
#   ./vps-deploy.sh --release               # download pre-built binary from latest GitHub release
#   ./vps-deploy.sh --release --tag v3.2.1  # download a specific release version
#   ./vps-deploy.sh --no-build              # just restart the service
#   ./vps-deploy.sh --fix-env               # patch .env: SERVER_PORT/HOST + DATABASE_TLS_MODE
#   ./vps-deploy.sh --dry-run               # show what would be done without executing
#   ./vps-deploy.sh --help                  # show this help
#
# Environment variables:
#   VPS_HOST          SSH host (default: 66.179.136.144)
#   VPS_USER          SSH user (default: root)
#   VPS_PORT          SSH port (default: 22)
#   KEY_FILE          SSH key path (default: $HOME/.ssh/ED_25519)
#   DEPLOY_DIR        Project directory on VPS (default: /home/Media-Server-Pro-3)
#                     For fresh installs via install.sh use: DEPLOY_DIR=/home/mediaserver/app
#   SERVICE           Systemd service name (default: mediaserver)
#   GITHUB_TOKEN      GitHub PAT for private repo access (optional)
#   GITHUB_USERNAME   GitHub username paired with token (optional)
#
# Fresh install (install.sh) users — override defaults:
#   DEPLOY_DIR=/home/mediaserver/app ./vps-deploy.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[deploy]${RESET} $*"; }
success() { echo -e "${GREEN}[deploy]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[deploy]${RESET} $*"; }
die()     { echo -e "${RED}[deploy] ERROR:${RESET} $*" >&2; exit 1; }

VPS_HOST="${VPS_HOST:-66.179.136.144}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/ED_25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/home/Media-Server-Pro-3}"
SERVICE="${SERVICE:-mediaserver}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
GITHUB_USERNAME="${GITHUB_USERNAME:-}"

FULL_BUILD=false
NO_BUILD=false
FIX_ENV=false
DRY_RUN=false
BRANCH=""
USE_RELEASE=false
RELEASE_TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --full)            FULL_BUILD=true   ; shift ;;
    --no-build)        NO_BUILD=true     ; shift ;;
    --fix-env)         FIX_ENV=true      ; shift ;;
    --dry-run)         DRY_RUN=true      ; shift ;;
    --branch)          BRANCH="$2"       ; shift 2 ;;
    --release)         USE_RELEASE=true  ; shift ;;
    --tag)             RELEASE_TAG="$2"  ; shift 2 ;;
    --help|-h)
      sed -n '/^# Usage:/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help for usage)" ;;
  esac
done

echo -e "\n${BOLD}=== Media Server Pro 3 — VPS Deploy ===${RESET}\n"
info "VPS        : $VPS_USER@$VPS_HOST:$VPS_PORT"
info "App dir    : $DEPLOY_DIR"
info "Service    : $SERVICE"
if $USE_RELEASE; then
  info "Method     : GitHub Release binary"
  [[ -n "$RELEASE_TAG" ]] && info "Release tag: $RELEASE_TAG"
else
  info "Method     : Source build"
  [[ -n "$BRANCH" ]] && info "Branch     : $BRANCH"
fi
$DRY_RUN && warn "DRY RUN — no commands will be executed"
echo ""

# Auto-setup SSH key if needed (generates key + installs on VPS with one password prompt)
source "$SCRIPT_DIR/vps-auth.sh"

# Helper for dry-run mode
run_vps() {
  if $DRY_RUN; then
    info "[dry-run] vps :: $1"
    return 0
  fi
  vps "bash -s" <<< "$1"
}

# ── Optional: patch .env ──────────────────────────────────────────────────────
if $FIX_ENV; then
  info "Patching .env (SERVER_PORT, SERVER_HOST, DATABASE_TLS_MODE)..."
  run_vps "
ENV_FILE=$DEPLOY_DIR/.env
if grep -q '^SERVER_PORT=' \"\$ENV_FILE\" 2>/dev/null; then
  sed -i 's/^SERVER_PORT=.*/SERVER_PORT=8080/' \"\$ENV_FILE\"
else
  echo 'SERVER_PORT=8080' >> \"\$ENV_FILE\"
fi
if grep -q '^SERVER_HOST=' \"\$ENV_FILE\" 2>/dev/null; then
  sed -i 's/^SERVER_HOST=.*/SERVER_HOST=127.0.0.1/' \"\$ENV_FILE\"
else
  echo 'SERVER_HOST=127.0.0.1' >> \"\$ENV_FILE\"
fi
# Inject DATABASE_TLS_MODE=skip-verify for non-localhost database hosts.
# Many hosted database providers require TLS; without this the connection fails.
DB_HOST=\"\$(grep -oP '(?<=^DATABASE_HOST=).+' \"\$ENV_FILE\" 2>/dev/null | head -1 || echo '')\"
if [ -n \"\$DB_HOST\" ] && [ \"\$DB_HOST\" != 'localhost' ] && [ \"\$DB_HOST\" != '127.0.0.1' ]; then
  if grep -q '^DATABASE_TLS_MODE=' \"\$ENV_FILE\" 2>/dev/null; then
    # Only overwrite if currently empty (don't override a user-set value)
    if grep -qP '^DATABASE_TLS_MODE=\$' \"\$ENV_FILE\" 2>/dev/null; then
      sed -i 's/^DATABASE_TLS_MODE=\$/DATABASE_TLS_MODE=skip-verify/' \"\$ENV_FILE\"
      echo \"  DATABASE_TLS_MODE=skip-verify (updated from empty)\"
    else
      echo \"  DATABASE_TLS_MODE=\$(grep DATABASE_TLS_MODE \$ENV_FILE) (kept existing)\"
    fi
  else
    echo 'DATABASE_TLS_MODE=skip-verify' >> \"\$ENV_FILE\"
    echo \"  DATABASE_TLS_MODE=skip-verify (added)\"
  fi
else
  echo \"  DATABASE_TLS_MODE: skipped (DB host is localhost)\"
fi
echo \"  SERVER_PORT=\$(grep SERVER_PORT \$ENV_FILE)\"
echo \"  SERVER_HOST=\$(grep SERVER_HOST \$ENV_FILE)\"
"
  echo ""
fi

# ══════════════════════════════════════════════════════════════════════════════
# Step 1: ALWAYS stop service first — ensures port is free and binary unlocked
# ══════════════════════════════════════════════════════════════════════════════
info "Stopping $SERVICE..."
run_vps "sudo systemctl stop $SERVICE 2>/dev/null || true; echo '  $SERVICE stopped'"

if $USE_RELEASE; then
  # ════════════════════════════════════════════════════════════════════════════
  # Release binary deployment — download pre-built binary from GitHub Releases
  # ════════════════════════════════════════════════════════════════════════════
  info "Deploying from GitHub Release..."

  # Build GitHub auth header
  GH_AUTH=""
  if [[ -n "$GITHUB_TOKEN" ]]; then
    GH_AUTH="-H 'Authorization: Bearer $GITHUB_TOKEN'"
  fi

  TAG_OPT=""
  if [[ -n "$RELEASE_TAG" ]]; then
    TAG_OPT="$RELEASE_TAG"
  fi

  run_vps "
set -euo pipefail
cd '$DEPLOY_DIR'

ASSET_NAME='server-linux-amd64'
ARCH=\$(uname -m)
if [ \"\$ARCH\" = 'aarch64' ] || [ \"\$ARCH\" = 'arm64' ]; then
  ASSET_NAME='server-linux-arm64'
fi

TAG_OPT='$TAG_OPT'
if [ -n \"\$TAG_OPT\" ]; then
  API_URL=\"https://api.github.com/repos/bradselph/Media-Server-Pro-3/releases/tags/\$TAG_OPT\"
else
  API_URL='https://api.github.com/repos/bradselph/Media-Server-Pro-3/releases/latest'
fi

echo \"[deploy] Fetching release info from \$API_URL\"
RELEASE_JSON=\$(curl -fsSL $GH_AUTH -H 'Accept: application/vnd.github.v3+json' \"\$API_URL\") \\
  || { echo 'ERROR: Failed to fetch release info'; exit 1; }

TAG_NAME=\$(echo \"\$RELEASE_JSON\" | grep -o '\"tag_name\": *\"[^\"]*\"' | head -1 | sed 's/\"tag_name\": *\"//' | sed 's/\"//')
echo \"[deploy] Found release: \$TAG_NAME\"

# Find matching asset — look for server binary, then media-server
DOWNLOAD_URL=''
for PATTERN in \"\$ASSET_NAME\" \"media-server-linux-amd64\"; do
  MATCH=\$(echo \"\$RELEASE_JSON\" | grep -o '\"browser_download_url\": *\"[^\"]*'\"\$PATTERN\"'[^\"]*\"' | head -1 | sed 's/\"browser_download_url\": *\"//' | sed 's/\"//')
  if [ -n \"\$MATCH\" ]; then
    DOWNLOAD_URL=\"\$MATCH\"
    break
  fi
done

if [ -z \"\$DOWNLOAD_URL\" ]; then
  echo 'ERROR: No matching binary asset found in release'
  echo 'Available assets:'
  echo \"\$RELEASE_JSON\" | grep -o '\"name\": *\"[^\"]*\"' | head -10
  exit 1
fi

echo \"[deploy] Downloading binary...\"
TMP_BIN=\$(mktemp)
curl -fsSL $GH_AUTH -L -o \"\$TMP_BIN\" \"\$DOWNLOAD_URL\" \\
  || { echo 'ERROR: Download failed'; rm -f \"\$TMP_BIN\"; exit 1; }

# Validate ELF magic bytes
MAGIC=\$(xxd -l 4 -p \"\$TMP_BIN\" 2>/dev/null || od -A n -t x1 -N 4 \"\$TMP_BIN\" | tr -d ' ')
if [ \"\${MAGIC:0:8}\" != '7f454c46' ]; then
  echo 'ERROR: Downloaded file is not a valid Linux binary'
  rm -f \"\$TMP_BIN\"
  exit 1
fi

# Backup old binary
if [ -f './server' ]; then
  cp './server' './server.bak'
  echo '[deploy] Backed up current binary → server.bak'
fi

mv \"\$TMP_BIN\" './server'
chmod +x './server'
echo \"[deploy] Binary installed from release \$TAG_NAME\"
"

elif ! $NO_BUILD; then
  # ════════════════════════════════════════════════════════════════════════════
  # Source build deployment — git pull + build-ubuntu.sh
  # ════════════════════════════════════════════════════════════════════════════

  # Git auth setup for remote commands
  GIT_AUTH_SETUP=""
  if [[ -n "$GITHUB_TOKEN" ]]; then
    if [[ -n "$GITHUB_USERNAME" ]]; then
      GIT_AUTH_SETUP="
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0='url.https://${GITHUB_USERNAME}:${GITHUB_TOKEN}@github.com/.insteadOf'
export GIT_CONFIG_VALUE_0='https://github.com/'
export GOPRIVATE='github.com/bradselph/*'
export GONOSUMDB='github.com/bradselph/*'
"
    else
      GIT_AUTH_SETUP="
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0='http.https://github.com/.extraheader'
export GIT_CONFIG_VALUE_0='Authorization: Bearer ${GITHUB_TOKEN}'
export GOPRIVATE='github.com/bradselph/*'
export GONOSUMDB='github.com/bradselph/*'
"
    fi
  fi

  # Branch checkout + pull
  if [[ -n "$BRANCH" ]]; then
    info "Checking out branch: $BRANCH"
    run_vps "
${GIT_AUTH_SETUP}
cd '$DEPLOY_DIR'
git fetch origin '$BRANCH'
git checkout '$BRANCH'
git pull origin '$BRANCH'
echo \"[deploy] Now at: \$(git log -1 --format='%h %s')\"
"
  else
    info "Pulling latest changes on current branch..."
    run_vps "
${GIT_AUTH_SETUP}
cd '$DEPLOY_DIR'
git pull --ff-only
echo \"[deploy] Now at: \$(git log -1 --format='%h %s')\"
"
  fi

  info "Fixing script permissions..."
  run_vps "chmod +x '$DEPLOY_DIR'/*.sh 2>/dev/null || true"

  info "Building server..."
  BUILD_FLAGS="--install-deps"
  if ! $FULL_BUILD; then
    BUILD_FLAGS="$BUILD_FLAGS --no-react"
  fi

  run_vps "cd '$DEPLOY_DIR' && bash build-ubuntu.sh $BUILD_FLAGS"

  info "Ensuring binary permissions..."
  run_vps "chmod +x '$DEPLOY_DIR'/server '$DEPLOY_DIR'/hls-pregenerate '$DEPLOY_DIR'/media-doctor 2>/dev/null || true"

else
  # ════════════════════════════════════════════════════════════════════════════
  # No build — just restart
  # ════════════════════════════════════════════════════════════════════════════
  info "No build requested (--no-build), skipping to restart..."
fi

# ══════════════════════════════════════════════════════════════════════════════
# Step 2: Reload systemd, enable, and start the service
# ══════════════════════════════════════════════════════════════════════════════
info "Starting $SERVICE..."
run_vps "
set -euo pipefail
sudo systemctl daemon-reload
sudo systemctl enable '$SERVICE' 2>/dev/null || true

# Start the service — on failure: show journal, restore backup, and exit non-zero
if ! sudo systemctl start '$SERVICE' 2>/dev/null; then
  echo '[deploy] ERROR: $SERVICE failed to start'
  echo '[deploy] Last 30 journal lines:'
  journalctl -u '$SERVICE' --no-pager -n 30 2>/dev/null || true

  # Rollback: restore server.bak if a backup was made during this deploy
  if [ -f '$DEPLOY_DIR/server.bak' ]; then
    echo '[deploy] Rolling back to previous binary (server.bak)...'
    mv '$DEPLOY_DIR/server.bak' '$DEPLOY_DIR/server'
    chmod +x '$DEPLOY_DIR/server'
    if sudo systemctl start '$SERVICE' 2>/dev/null; then
      echo '[deploy] Rollback succeeded — old binary is running'
    else
      echo '[deploy] Rollback also failed — manual intervention required'
    fi
  fi
  exit 1
fi

# Poll the /health endpoint (up to 30s) to confirm the server is actually serving
PORT=\"\$(grep -oP '(?<=^SERVER_PORT=)\d+' '$DEPLOY_DIR/.env' 2>/dev/null | head -1 || echo 8080)\"
HEALTH_URL=\"http://127.0.0.1:\${PORT}/health\"
echo '[deploy] Waiting for health endpoint...'
for i in \$(seq 1 15); do
  CODE=\"\$(curl --silent --output /dev/null --write-out '%{http_code}' \
    --connect-timeout 2 --max-time 4 \"\$HEALTH_URL\" 2>/dev/null || echo '000')\"
  if [ \"\$CODE\" = '200' ] || [ \"\$CODE\" = '503' ]; then
    echo \"[deploy] \$SERVICE is healthy (HTTP \$CODE)\"
    break
  fi
  [ \"\$i\" -lt 15 ] && sleep 2
done
if [ \"\$CODE\" != '200' ] && [ \"\$CODE\" != '503' ]; then
  echo \"[deploy] WARNING: health endpoint returned HTTP \$CODE after 30s\"
  echo \"[deploy] Service may still be starting — check: journalctl -u $SERVICE -f\"
fi
"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}Deploy complete${RESET}"
echo ""
if ! $DRY_RUN; then
  run_vps "
cd '$DEPLOY_DIR'
echo \"  Commit  : \$(git log -1 --format='%h %s' 2>/dev/null || echo 'N/A')\"
echo \"  Branch  : \$(git branch --show-current 2>/dev/null || echo 'N/A')\"
echo \"  Version : \$(./server -version 2>/dev/null | head -1 || echo 'unknown')\"
echo \"  Service : \$(systemctl is-active '$SERVICE' 2>/dev/null || echo 'unknown')\"
"
fi
info "Live log: ssh \${SSH_OPTS[*]} $VPS_USER@$VPS_HOST 'journalctl -u $SERVICE -f'"
echo ""
