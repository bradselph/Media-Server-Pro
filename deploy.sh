#!/usr/bin/env bash
# deploy.sh — Build locally and deploy to VPS via SSH + systemd.
#
# Usage:
#   ./deploy.sh                         # build + scp + restart
#   ./deploy.sh --no-build              # scp existing binary + restart
#   ./deploy.sh --full                  # build with React frontend
#   ./deploy.sh --branch main           # deploy specific branch
#   ./deploy.sh --dry-run               # preview commands without executing
#   ./deploy.sh --fix-env               # patch .env on VPS (port, host, TLS)
#   ./deploy.sh --rollback              # restore server.bak on VPS
#   ./deploy.sh --help                  # show help
#
# Environment variables:
#   VPS_HOST     SSH host          (default: your-vps-ip)
#   VPS_USER     SSH user          (default: root)
#   VPS_PORT     SSH port          (default: 22)
#   KEY_FILE     SSH private key   (default: ~/.ssh/id_ed25519)
#   DEPLOY_DIR   Remote app dir    (default: /opt/media-server)
#   SERVICE      systemd service   (default: media-server)

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[deploy]${RESET} $*"; }
success() { echo -e "${GREEN}[deploy]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[deploy]${RESET} $*"; }
die()     { echo -e "${RED}[deploy] ERROR:${RESET} $*" >&2; exit 1; }

# ── Defaults ──────────────────────────────────────────────────────────────────
VPS_HOST="${VPS_HOST:-your-vps-ip}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/id_ed25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/media-server}"
SERVICE="${SERVICE:-media-server}"

BUILD_REACT=false
NO_BUILD=false
DRY_RUN=false
FIX_ENV=false
ROLLBACK=false
BRANCH=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --full)       BUILD_REACT=true ; shift ;;
    --no-build)   NO_BUILD=true    ; shift ;;
    --dry-run)    DRY_RUN=true     ; shift ;;
    --fix-env)    FIX_ENV=true     ; shift ;;
    --rollback)   ROLLBACK=true    ; shift ;;
    --branch)     BRANCH="$2"      ; shift 2 ;;
    --help|-h)
      sed -n '/^# Usage:/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$SCRIPT_DIR/server"

# SSH options
SSH_OPTS=(-p "$VPS_PORT" -i "$KEY_FILE" -o "StrictHostKeyChecking=accept-new" -o "BatchMode=yes")

vps() { ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "$@"; }
scp_up() { scp -P "$VPS_PORT" -i "$KEY_FILE" -o "StrictHostKeyChecking=accept-new" "$@"; }

run_or_dry() {
  if $DRY_RUN; then
    info "[dry-run] $*"
  else
    "$@"
  fi
}

echo -e "\n${BOLD}=== Media Server Pro 4 — Deploy ===${RESET}\n"
info "VPS        : $VPS_USER@$VPS_HOST:$VPS_PORT"
info "Deploy dir : $DEPLOY_DIR"
info "Service    : $SERVICE"
$DRY_RUN && warn "DRY RUN — no commands will execute"
echo ""

# ── Rollback ──────────────────────────────────────────────────────────────────
if $ROLLBACK; then
  info "Rolling back to server.bak..."
  vps "
    if [ ! -f '$DEPLOY_DIR/server.bak' ]; then
      echo 'ERROR: no server.bak found'; exit 1
    fi
    sudo systemctl stop '$SERVICE' 2>/dev/null || true
    mv '$DEPLOY_DIR/server.bak' '$DEPLOY_DIR/server'
    chmod +x '$DEPLOY_DIR/server'
    sudo systemctl start '$SERVICE'
    echo 'Rollback complete'
  "
  exit 0
fi

# ── Fix env ───────────────────────────────────────────────────────────────────
if $FIX_ENV; then
  info "Patching .env on VPS..."
  vps "
    ENV='$DEPLOY_DIR/.env'
    patch_or_add() {
      local key=\$1 val=\$2
      if grep -q \"^\$key=\" \"\$ENV\" 2>/dev/null; then
        sed -i \"s|^\$key=.*|\$key=\$val|\" \"\$ENV\"
      else
        echo \"\$key=\$val\" >> \"\$ENV\"
      fi
      echo \"  \$key=\$val\"
    }
    patch_or_add SERVER_PORT 8080
    patch_or_add SERVER_HOST 127.0.0.1
    # Add TLS mode for remote databases
    DB_HOST=\$(grep -o 'DATABASE_HOST=[^[:space:]]*' \"\$ENV\" 2>/dev/null | cut -d= -f2 || echo localhost)
    if [ \"\$DB_HOST\" != 'localhost' ] && [ \"\$DB_HOST\" != '127.0.0.1' ]; then
      patch_or_add DATABASE_TLS_MODE skip-verify
    fi
  "
  echo ""
fi

# ── Branch checkout ───────────────────────────────────────────────────────────
if [[ -n "$BRANCH" ]]; then
  info "Checking out branch: $BRANCH"
  run_or_dry git fetch origin "$BRANCH"
  run_or_dry git checkout "$BRANCH"
  run_or_dry git pull origin "$BRANCH"
fi

# ── Build ─────────────────────────────────────────────────────────────────────
if ! $NO_BUILD; then
  cd "$SCRIPT_DIR"

  if $BUILD_REACT; then
    info "Building React frontend..."
    (cd web/frontend && npm ci && npm run build) || die "React build failed"
    success "React bundle built → web/static/react/"
  fi

  info "Downloading Go modules..."
  go mod download || die "go mod download failed"

  info "Building server binary (linux/amd64)..."
  GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.Version=$(cat VERSION 2>/dev/null || echo 4.0.0) -X main.BuildDate=$(date +%Y-%m-%d)" \
    -o "$BINARY" ./cmd/server || die "Go build failed"
  success "Binary built → $BINARY"
else
  [[ -f "$BINARY" ]] || die "Binary $BINARY not found. Run without --no-build first."
fi

# ── Deploy ────────────────────────────────────────────────────────────────────
info "Stopping service on VPS..."
run_or_dry vps "sudo systemctl stop '$SERVICE' 2>/dev/null || true; echo 'Service stopped'"

# Backup old binary
run_or_dry vps "[ -f '$DEPLOY_DIR/server' ] && cp '$DEPLOY_DIR/server' '$DEPLOY_DIR/server.bak' && echo 'Backed up server → server.bak' || true"

info "Uploading binary..."
if ! $DRY_RUN; then
  scp_up "$BINARY" "$VPS_USER@$VPS_HOST:$DEPLOY_DIR/server"
fi

info "Ensuring binary is executable..."
run_or_dry vps "chmod +x '$DEPLOY_DIR/server'"

# Upload systemd unit if present
if [[ -f "$SCRIPT_DIR/systemd/media-server.service" ]]; then
  info "Uploading systemd unit file..."
  if ! $DRY_RUN; then
    scp_up "$SCRIPT_DIR/systemd/media-server.service" \
      "$VPS_USER@$VPS_HOST:/tmp/media-server.service"
    vps "sudo mv /tmp/media-server.service /etc/systemd/system/$SERVICE.service && sudo systemctl daemon-reload"
  fi
fi

# ── Start & health check ──────────────────────────────────────────────────────
info "Starting $SERVICE..."
run_or_dry vps "
  set -euo pipefail
  sudo systemctl daemon-reload
  sudo systemctl enable '$SERVICE' 2>/dev/null || true

  if ! sudo systemctl start '$SERVICE'; then
    echo '[deploy] ERROR: service failed to start'
    journalctl -u '$SERVICE' --no-pager -n 30 2>/dev/null || true

    # Rollback on failure
    if [ -f '$DEPLOY_DIR/server.bak' ]; then
      echo '[deploy] Rolling back...'
      mv '$DEPLOY_DIR/server.bak' '$DEPLOY_DIR/server'
      chmod +x '$DEPLOY_DIR/server'
      sudo systemctl start '$SERVICE' && echo '[deploy] Rollback succeeded' || echo '[deploy] Rollback also failed'
    fi
    exit 1
  fi

  # Poll health endpoint for up to 30s
  PORT=\$(grep -o 'SERVER_PORT=[0-9]*' '$DEPLOY_DIR/.env' 2>/dev/null | cut -d= -f2 || echo 8080)
  HEALTH_URL=\"http://127.0.0.1:\${PORT}/health\"
  echo \"[deploy] Polling \$HEALTH_URL...\"
  OK=false
  for i in \$(seq 1 15); do
    CODE=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \"\$HEALTH_URL\" 2>/dev/null || echo 000)
    if [ \"\$CODE\" = '200' ] || [ \"\$CODE\" = '503' ]; then
      echo \"[deploy] Health check: HTTP \$CODE\"
      OK=true
      break
    fi
    sleep 2
  done
  \$OK || echo '[deploy] WARNING: health endpoint timed out — check logs'
"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
success "Deploy complete."
if ! $DRY_RUN; then
  info "Status: $(vps "systemctl is-active '$SERVICE' 2>/dev/null || echo unknown")"
  info "Logs:   ssh -p $VPS_PORT $VPS_USER@$VPS_HOST 'journalctl -u $SERVICE -f'"
fi
echo ""
