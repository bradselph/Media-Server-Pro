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

GO_VERSION="1.26.0"
NODE_MAJOR="22"

DRY_RUN=false
FIX_ENV=false
ROLLBACK=false
SETUP=false
BRANCH="main"

# ── SSH auth setup ────────────────────────────────────────────────────────────
# Generates key if missing, strips any passphrase (needed for BatchMode=yes),
# converts path for Windows OpenSSH, and installs the public key on the VPS
# the first time (one-time password prompt).
setup_ssh_auth() {
  # 1. Generate key if missing
  if [[ ! -f "$KEY_FILE" ]]; then
    info "Generating SSH key at $KEY_FILE..."
    mkdir -p "$(dirname "$KEY_FILE")"
    ssh-keygen -t ed25519 -f "$KEY_FILE" -N "" -C "mediaserver-deploy"
    echo ""
  fi

  # 2. Remove passphrase if the key has one — BatchMode=yes cannot prompt for it
  if ! ssh-keygen -y -P "" -f "$KEY_FILE" &>/dev/null; then
    warn "SSH key has a passphrase — removing it for automated deploys."
    echo "    Enter the CURRENT key passphrase when prompted:"
    ssh-keygen -p -f "$KEY_FILE" -N ""
    echo ""
    info "Passphrase removed."
  fi

  # 3. Convert POSIX path to Windows path for Git Bash / Windows OpenSSH
  #    (Git Bash reports HOME as /c/Users/... but ssh.exe needs C:/Users/...)
  KEY_FILE_SSH="$KEY_FILE"
  if command -v cygpath &>/dev/null 2>&1; then
    KEY_FILE_SSH="$(cygpath -m "$KEY_FILE" 2>/dev/null || echo "$KEY_FILE")"
  fi

  SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$VPS_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8)

  # 4. Test key auth; install on VPS if not yet authorised (one-time password prompt)
  if ! ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "exit 0" 2>/dev/null; then
    info "Key not yet authorised on VPS — installing it now."
    echo "    Enter the VPS password when prompted (one time only)."
    echo ""

    local pub_key
    pub_key="$(cat "${KEY_FILE}.pub")"

    if command -v ssh-copy-id &>/dev/null; then
      ssh-copy-id -i "${KEY_FILE}.pub" -p "$VPS_PORT" "$VPS_USER@$VPS_HOST"
    else
      ssh -p "$VPS_PORT" \
          -o StrictHostKeyChecking=accept-new \
          -o ConnectTimeout=10 \
          "$VPS_USER@$VPS_HOST" \
          "mkdir -p ~/.ssh && chmod 700 ~/.ssh && \
           echo '$pub_key' >> ~/.ssh/authorized_keys && \
           chmod 600 ~/.ssh/authorized_keys && \
           echo 'Key installed OK.'"
    fi

    # Verify the key now works
    if ! ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "exit 0" 2>/dev/null; then
      die "SSH key auth still failing after install. Try manually: ssh -i \"$KEY_FILE_SSH\" $VPS_USER@$VPS_HOST"
    fi

    success "SSH key installed — future runs will connect without a password."
    echo ""
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --full)       shift ;;              # React is always built; kept for backwards compat
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

# SSH_OPTS and vps() are initialised by setup_ssh_auth() below.
# Declare them here so other functions can reference them before the call.
SSH_OPTS=()
vps() { ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" -- "$@"; }

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

# Ensure SSH key is ready and authorised (skipped for dry-runs that never SSH)
if ! $DRY_RUN; then
  setup_ssh_auth
fi

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
    export PATH=\$PATH:/usr/local/go/bin

    # ── System packages ──────────────────────────────────────────────────────
    echo '[setup] Updating apt and installing base packages...'
    sudo apt-get update -qq
    sudo apt-get install -y git curl build-essential ffmpeg

    # ── Go ────────────────────────────────────────────────────────────────────
    if ! command -v go &>/dev/null; then
      echo '[setup] Installing Go ${GO_VERSION}...'
      ARCH=\$(dpkg --print-architecture 2>/dev/null || uname -m)
      case \"\$ARCH\" in
        amd64|x86_64)  GO_ARCH=amd64 ;;
        arm64|aarch64) GO_ARCH=arm64 ;;
        *)             GO_ARCH=amd64 ;;
      esac
      curl -fsSL \"https://go.dev/dl/go${GO_VERSION}.linux-\${GO_ARCH}.tar.gz\" -o /tmp/go.tar.gz
      sudo rm -rf /usr/local/go
      sudo tar -C /usr/local -xzf /tmp/go.tar.gz
      rm /tmp/go.tar.gz
      echo 'export PATH=\$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh > /dev/null
      export PATH=\$PATH:/usr/local/go/bin
      echo \"[setup] Go \$(go version) installed\"
    else
      echo \"[setup] Go already installed: \$(go version)\"
    fi

    # ── Node.js ──────────────────────────────────────────────────────────────
    if ! command -v node &>/dev/null; then
      echo '[setup] Installing Node.js ${NODE_MAJOR}...'
      curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | sudo -E bash - 2>/dev/null
      sudo apt-get install -y nodejs
      echo \"[setup] Node \$(node --version) installed\"
    else
      echo \"[setup] Node already installed: \$(node --version)\"
    fi

    # ── Service user ─────────────────────────────────────────────────────────
    if ! id mediaserver &>/dev/null 2>&1; then
      echo '[setup] Creating mediaserver system user...'
      sudo useradd -r -s /usr/sbin/nologin -d '$DEPLOY_DIR' -m mediaserver
    else
      echo '[setup] mediaserver user already exists'
    fi

    # ── Clone repository ─────────────────────────────────────────────────────
    if [ ! -d '$DEPLOY_DIR/.git' ]; then
      echo '[setup] Cloning repository...'
      sudo mkdir -p '$(dirname "$DEPLOY_DIR")'
      sudo git clone --branch '$BRANCH' '$CLONE_URL' '$DEPLOY_DIR'
    else
      echo '[setup] Repository already cloned'
    fi

    # ── Create required data directories ─────────────────────────────────────
    echo '[setup] Creating data directories...'
    sudo mkdir -p '$DEPLOY_DIR'/{videos,music,thumbnails,uploads,cache/hls,cache/remote,logs,data,backups}

    # ── Copy .env template ───────────────────────────────────────────────────
    if [ ! -f '$DEPLOY_DIR/.env' ]; then
      sudo cp '$DEPLOY_DIR/.env.example' '$DEPLOY_DIR/.env'
      sudo chmod 600 '$DEPLOY_DIR/.env'
      echo '[setup] Created .env from template — edit it with your settings!'
    fi

    # ── Set ownership ────────────────────────────────────────────────────────
    sudo chown -R mediaserver:mediaserver '$DEPLOY_DIR'

    # ── Install systemd service ──────────────────────────────────────────────
    if [ -f '$DEPLOY_DIR/systemd/media-server.service' ]; then
      sudo cp '$DEPLOY_DIR/systemd/media-server.service' '/etc/systemd/system/$SERVICE.service'
      sudo systemctl daemon-reload
      sudo systemctl enable '$SERVICE'
      echo '[setup] systemd service installed and enabled'
    fi

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

  # Clone if the directory doesn't exist yet
  if [ ! -d '$DEPLOY_DIR/.git' ]; then
    echo '[deploy] Deploy directory not found — cloning repository...'
    mkdir -p '$DEPLOY_DIR'
    git clone --branch '$BRANCH' '$CLONE_URL' '$DEPLOY_DIR'
    echo '[deploy] Clone complete'
  fi

  cd '$DEPLOY_DIR'

  # Ensure the remote URL uses the token
  git remote set-url origin '$CLONE_URL'

  git fetch origin '$BRANCH'
  git checkout '$BRANCH'
  git reset --hard 'origin/$BRANCH'

  echo \"[deploy] HEAD is now: \$(git log --oneline -1)\"
"

# ── Ensure dependencies are installed on VPS ─────────────────────────────────
info "Checking dependencies on VPS..."
run_or_dry vps "
  set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin

  # ── apt packages (git, curl, build-essential, ffmpeg) ─────────────────────
  MISSING_APT=()
  for pkg in git curl build-essential ffmpeg; do
    dpkg -s \"\$pkg\" &>/dev/null || MISSING_APT+=(\"\$pkg\")
  done
  if [ \${#MISSING_APT[@]} -gt 0 ]; then
    echo '[deps] Installing apt packages: '\${MISSING_APT[*]}
    sudo apt-get update -qq
    sudo apt-get install -y \"\${MISSING_APT[@]}\"
  else
    echo '[deps] apt packages already installed'
  fi

  # ffprobe ships with ffmpeg — verify it is present
  if command -v ffprobe &>/dev/null; then
    echo \"[deps] ffprobe: \$(ffprobe -version 2>&1 | head -1)\"
  else
    echo '[deps] WARNING: ffprobe not found even after ffmpeg install'
  fi

  # ── Go ─────────────────────────────────────────────────────────────────────
  NEED_GO=false
  if ! command -v go &>/dev/null; then
    NEED_GO=true
    echo '[deps] Go not found — installing...'
  elif [ \"\$(go version 2>/dev/null | grep -oP 'go[0-9]+\.[0-9]+\.[0-9]+')\" != 'go$GO_VERSION' ]; then
    NEED_GO=true
    echo \"[deps] Go version mismatch — upgrading to $GO_VERSION...\"
  else
    echo \"[deps] Go $GO_VERSION already installed\"
  fi

  if \$NEED_GO; then
    ARCH=\$(dpkg --print-architecture 2>/dev/null || uname -m)
    case \"\$ARCH\" in
      amd64|x86_64)  GO_ARCH=amd64 ;;
      arm64|aarch64) GO_ARCH=arm64 ;;
      armv6l|armv7l) GO_ARCH=armv6l ;;
      *)             GO_ARCH=amd64 ;;
    esac
    GO_TAR=\"go${GO_VERSION}.linux-\${GO_ARCH}.tar.gz\"
    echo \"[deps] Downloading \$GO_TAR...\"
    curl -fsSL \"https://go.dev/dl/\$GO_TAR\" -o /tmp/go.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    echo 'export PATH=\$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh > /dev/null
    export PATH=\$PATH:/usr/local/go/bin
    echo \"[deps] Installed \$(go version)\"
  fi

  # ── Node.js + npm ──────────────────────────────────────────────────────────
  NEED_NODE=false
  if ! command -v node &>/dev/null; then
    NEED_NODE=true
    echo '[deps] Node.js not found — installing...'
  else
    NODE_MAJOR_INSTALLED=\$(node --version | grep -oP '(?<=v)\d+')
    if [ \"\$NODE_MAJOR_INSTALLED\" -lt '$NODE_MAJOR' ]; then
      NEED_NODE=true
      echo \"[deps] Node.js \$(node --version) is too old — upgrading to $NODE_MAJOR...\"
    else
      echo \"[deps] Node.js \$(node --version) already installed\"
    fi
  fi

  if \$NEED_NODE; then
    curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | sudo -E bash - 2>/dev/null
    sudo apt-get install -y nodejs
    echo \"[deps] Installed Node \$(node --version) / npm \$(npm --version)\"
  fi

  # ── Vite (global, so 'vite' works in PATH during CI-style builds) ──────────
  if ! command -v vite &>/dev/null; then
    echo '[deps] Installing Vite globally...'
    sudo npm install -g vite 2>/dev/null
    echo \"[deps] Vite \$(vite --version 2>/dev/null || echo installed)\"
  else
    echo \"[deps] Vite already installed: \$(vite --version 2>/dev/null || echo ok)\"
  fi
"

# ── Build on VPS

info "Building on VPS..."

run_or_dry vps "
  set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin:/usr/local/bin

  cd '$DEPLOY_DIR'

  # ── React frontend (always built before Go binary) ─────────────────────────
  echo '[deploy] Building React frontend...'
  cd web/frontend
  if [ -f package-lock.json ]; then
    echo '[deploy] package-lock.json found — using npm ci'
    npm ci
  else
    echo '[deploy] No package-lock.json — using npm install'
    npm install
  fi
  npm run build
  cd ../..
  echo '[deploy] React build complete'

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

# ── Ensure service user and directories ───────────────────────────────────────
run_or_dry vps "
  # Create the service user if it doesn't exist yet
  if ! id mediaserver &>/dev/null 2>&1; then
    echo '[deploy] Creating mediaserver system user...'
    sudo useradd -r -s /usr/sbin/nologin -d '$DEPLOY_DIR' -m mediaserver
  fi

  # Ensure data directories exist
  sudo mkdir -p '$DEPLOY_DIR'/{videos,music,thumbnails,uploads,cache/hls,cache/remote,logs,data,backups}

  # Secure .env file permissions
  [ -f '$DEPLOY_DIR/.env' ] && sudo chmod 600 '$DEPLOY_DIR/.env'

  sudo chown -R mediaserver:mediaserver '$DEPLOY_DIR'
"

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

  # Poll health endpoint until fully ready (200) or timeout after 90s.
  # The server returns 503 while the initial media scan is in progress;
  # we wait for 200 to ensure media is ready before declaring success.
  PORT=\$(grep -o 'SERVER_PORT=[0-9]*' '$DEPLOY_DIR/.env' 2>/dev/null | cut -d= -f2 || echo 8080)
  HEALTH_URL=\"http://127.0.0.1:\${PORT}/health\"
  echo \"[deploy] Polling \$HEALTH_URL (waiting for media scan to complete)...\"
  OK=false
  for i in \$(seq 1 30); do
    CODE=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \"\$HEALTH_URL\" 2>/dev/null || echo 000)
    if [ \"\$CODE\" = '200' ]; then
      echo \"[deploy] Health check: HTTP 200 — server ready\"
      OK=true
      break
    elif [ \"\$CODE\" = '503' ]; then
      echo \"[deploy] Health check: HTTP 503 — still initializing (\${i}/30)\"
    fi
    sleep 3
  done
  \$OK || echo '[deploy] WARNING: health endpoint did not reach 200 — check logs: journalctl -u $SERVICE -n 50'
"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
success "Deploy complete."
if ! $DRY_RUN; then
  info "Status: $(vps "systemctl is-active '$SERVICE' 2>/dev/null || echo unknown")"
  info "Logs:   ssh -p $VPS_PORT $VPS_USER@$VPS_HOST 'journalctl -u $SERVICE -f'"
fi
echo ""
