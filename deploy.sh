#!/usr/bin/env bash
# deploy.sh — Deploy Media Server Pro to a VPS via git clone/pull + remote build.
#
# Usage:
#   ./deploy.sh                         # pull latest + build + restart
#   ./deploy.sh --full                  # also rebuild React frontend
#   ./deploy.sh --branch main           # deploy specific branch
#   ./deploy.sh --setup                 # first-time VPS setup (install deps, clone repo)
#   ./deploy.sh --fix-env               # patch .env on VPS (port, host, TLS)
#   ./deploy.sh --rollback              # restore server.bak on VPS
#   ./deploy.sh --dry-run               # preview commands without executing
#   ./deploy.sh --help                  # show help
#
# Environment variables (set in shell or .deploy.env):
#   VPS_HOST       SSH host          (required)
#   VPS_USER       SSH user          (default: root)
#   VPS_PORT       SSH port          (default: 22)
#   KEY_FILE       SSH private key   (default: ~/.ssh/id_ed25519)
#   DEPLOY_DIR     Remote app dir    (default: /opt/media-server)
#   SERVICE        systemd service   (default: media-server)
#   GITHUB_TOKEN   GitHub PAT        (required for private repos)
#   REPO_URL       Repository URL    (default: github.com/bradselph/Media-Server-Pro.git)

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[deploy]${RESET} $*"; }
success() { echo -e "${GREEN}[deploy]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[deploy]${RESET} $*"; }
die()     { echo -e "${RED}[deploy] ERROR:${RESET} $*" >&2; exit 1; }

# ── Load .deploy.env if present ──────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "$SCRIPT_DIR/.deploy.env" ]] && source "$SCRIPT_DIR/.deploy.env"

# ── Defaults ──────────────────────────────────────────────────────────────────
VPS_HOST="${VPS_HOST:-}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/id_ed25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/media-server}"
SERVICE="${SERVICE:-media-server}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
REPO_URL="${REPO_URL:-github.com/bradselph/Media-Server-Pro.git}"

BUILD_REACT=false
DRY_RUN=false
FIX_ENV=false
ROLLBACK=false
SETUP=false
BRANCH="main"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --full)       BUILD_REACT=true ; shift ;;
    --dry-run)    DRY_RUN=true     ; shift ;;
    --fix-env)    FIX_ENV=true     ; shift ;;
    --rollback)   ROLLBACK=true    ; shift ;;
    --setup)      SETUP=true       ; shift ;;
    --branch)     BRANCH="$2"      ; shift 2 ;;
    --help|-h)
      sed -n '/^# Usage:/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

[[ -z "$VPS_HOST" ]] && die "VPS_HOST is not set. Export it or add to .deploy.env"
[[ -z "$GITHUB_TOKEN" ]] && die "GITHUB_TOKEN is not set. Export it or add to .deploy.env"

# SSH options
SSH_OPTS=(-p "$VPS_PORT" -i "$KEY_FILE" -o "StrictHostKeyChecking=accept-new" -o "BatchMode=yes")

vps() { ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "$@"; }

run_or_dry() {
  if $DRY_RUN; then
    info "[dry-run] $*"
  else
    "$@"
  fi
}

CLONE_URL="https://${GITHUB_TOKEN}@${REPO_URL}"

echo -e "\n${BOLD}=== Media Server Pro — Deploy ===${RESET}\n"
info "VPS        : $VPS_USER@$VPS_HOST:$VPS_PORT"
info "Deploy dir : $DEPLOY_DIR"
info "Service    : $SERVICE"
info "Branch     : $BRANCH"
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

# ── First-time setup ─────────────────────────────────────────────────────────
if $SETUP; then
  info "Running first-time VPS setup..."
  run_or_dry vps "
    set -euo pipefail

    # Install Go (if not present)
    if ! command -v go &>/dev/null; then
      echo '[setup] Installing Go...'
      curl -fsSL https://go.dev/dl/go1.24.1.linux-amd64.tar.gz -o /tmp/go.tar.gz
      sudo rm -rf /usr/local/go
      sudo tar -C /usr/local -xzf /tmp/go.tar.gz
      rm /tmp/go.tar.gz
      echo 'export PATH=\$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh
      export PATH=\$PATH:/usr/local/go/bin
      echo \"[setup] Go \$(go version) installed\"
    else
      echo \"[setup] Go already installed: \$(go version)\"
    fi

    # Install Node.js (if not present, for React builds)
    if ! command -v node &>/dev/null; then
      echo '[setup] Installing Node.js 22...'
      curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
      sudo apt-get install -y nodejs
      echo \"[setup] Node \$(node --version) installed\"
    else
      echo \"[setup] Node already installed: \$(node --version)\"
    fi

    # Install ffmpeg (if not present)
    if ! command -v ffmpeg &>/dev/null; then
      echo '[setup] Installing ffmpeg...'
      sudo apt-get install -y ffmpeg
    else
      echo \"[setup] ffmpeg already installed\"
    fi

    # Create service user (if not exists)
    if ! id mediaserver &>/dev/null; then
      echo '[setup] Creating mediaserver user...'
      sudo useradd -r -s /usr/sbin/nologin -d '$DEPLOY_DIR' mediaserver
    fi

    # Clone repository
    if [ ! -d '$DEPLOY_DIR/.git' ]; then
      echo '[setup] Cloning repository...'
      sudo mkdir -p '$(dirname "$DEPLOY_DIR")'
      git clone '$CLONE_URL' '$DEPLOY_DIR'
    else
      echo '[setup] Repository already cloned'
    fi

    # Copy .env template if no .env exists
    if [ ! -f '$DEPLOY_DIR/.env' ]; then
      cp '$DEPLOY_DIR/.env.example' '$DEPLOY_DIR/.env'
      echo '[setup] Created .env from template — edit it with your settings!'
    fi

    # Install systemd service
    if [ -f '$DEPLOY_DIR/systemd/media-server.service' ]; then
      sudo cp '$DEPLOY_DIR/systemd/media-server.service' '/etc/systemd/system/$SERVICE.service'
      sudo systemctl daemon-reload
      sudo systemctl enable '$SERVICE'
      echo '[setup] systemd service installed and enabled'
    fi

    # Set ownership
    sudo chown -R mediaserver:mediaserver '$DEPLOY_DIR'

    echo ''
    echo '[setup] Done! Next steps:'
    echo '  1. Edit $DEPLOY_DIR/.env with your database credentials and settings'
    echo '  2. Run: ./deploy.sh              (to build and start)'
    echo '  3. Run: ./deploy.sh --fix-env    (to auto-patch common settings)'
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

# ── Pull latest code ─────────────────────────────────────────────────────────
info "Pulling latest code on VPS (branch: $BRANCH)..."
run_or_dry vps "
  set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin

  cd '$DEPLOY_DIR'

  # Ensure the remote URL uses the token
  git remote set-url origin '$CLONE_URL'

  git fetch origin '$BRANCH'
  git checkout '$BRANCH'
  git reset --hard 'origin/$BRANCH'

  echo \"[deploy] HEAD is now: \$(git log --oneline -1)\"
"

# ── Build on VPS ──────────────────────────────────────────────────────────────
info "Building on VPS..."
REACT_BUILD_CMD=""
if $BUILD_REACT; then
  REACT_BUILD_CMD="
  echo '[deploy] Building React frontend...'
  cd web/frontend
  npm ci
  npm run build
  cd ../..
  echo '[deploy] React build complete'
  "
else
  REACT_BUILD_CMD="echo '[deploy] Skipping React build (use --full to include)'"
fi

run_or_dry vps "
  set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin:/usr/local/bin

  cd '$DEPLOY_DIR'

  # React frontend (optional)
  $REACT_BUILD_CMD

  # Stop service before replacing binary
  sudo systemctl stop '$SERVICE' 2>/dev/null || true

  # Backup old binary
  [ -f server ] && cp server server.bak && echo '[deploy] Backed up server -> server.bak'

  # Build Go binary
  echo '[deploy] Building Go binary...'
  VERSION=\$(cat VERSION 2>/dev/null || echo 4.0.0)
  go build \\
    -ldflags \"-X main.Version=\$VERSION -X main.BuildDate=\$(date +%Y-%m-%d)\" \\
    -o server ./cmd/server

  echo '[deploy] Build complete'
"

# ── Update systemd unit if changed ────────────────────────────────────────────
run_or_dry vps "
  if [ -f '$DEPLOY_DIR/systemd/media-server.service' ]; then
    sudo cp '$DEPLOY_DIR/systemd/media-server.service' '/etc/systemd/system/$SERVICE.service'
    sudo systemctl daemon-reload
  fi
"

# ── Fix ownership ─────────────────────────────────────────────────────────────
run_or_dry vps "sudo chown -R mediaserver:mediaserver '$DEPLOY_DIR'"

# ── Start & health check ─────────────────────────────────────────────────────
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
      cd '$DEPLOY_DIR'
      mv server.bak server
      chmod +x server
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
