#!/usr/bin/env bash
# deploy.sh — Deploy Media Server Pro (master server or slave node).
#
# MASTER SERVER (default):
#   ./deploy.sh                         # pull + build + restart on VPS (branch from .env or main)
#   ./deploy.sh --branch main           # deploy from the stable main branch
#   ./deploy.sh --branch development    # deploy from the development branch
#   ./deploy.sh --dev                   # shorthand for --branch development
#   ./deploy.sh --setup                 # first-time VPS provisioning
#   ./deploy.sh --setup-receiver        # configure master receiver for slave nodes
#   ./deploy.sh --fix-env               # patch .env on VPS (incl. optional: receiver, Hugging Face)
#   ./deploy.sh --rollback              # restore server.bak on VPS
#   ./deploy.sh --dry-run               # preview commands without executing
#
# SLAVE NODE (--slave):
#   ./deploy.sh --slave --local         # build and run slave on this machine
#   ./deploy.sh --slave --local --stop  # stop the local slave process
#   ./deploy.sh --slave --setup         # first-time setup on remote slave device
#   ./deploy.sh --slave                 # update slave binary on remote device
#   ./deploy.sh --slave --fix-env       # re-write .env on remote slave
#   ./deploy.sh --slave --rollback      # restore backup on remote slave
#   ./deploy.sh --slave --dry-run       # preview without executing
#
# INTERACTIVE SETUP:
#   ./setup.sh                          # guided first-time setup wizard
#
# Configuration (set in shell or config files):
#   .deploy.env    VPS connection + GitHub credentials
#   .slave.env     Slave node settings (overrides .deploy.env)
#
# Master variables:
#   VPS_HOST       SSH host          (required)
#   VPS_USER       SSH user          (default: root)
#   VPS_PORT       SSH port          (default: 22)
#   KEY_FILE       SSH private key   (default: ~/.ssh/id_ed25519)
#   DEPLOY_DIR     Remote app dir    (default: /opt/media-server)
#   SERVICE        systemd service   (default: media-server)
#   GITHUB_TOKEN   GitHub PAT        (required for private repos)
#   REPO_URL       Repository URL    (default: github.com/bradselph/Media-Server-Pro.git)
#
# Slave variables:
#   MASTER_URL         Master server URL       (required)
#   RECEIVER_API_KEY   API key from master      (required)
#   MEDIA_DIRS         Comma-separated dirs     (required for --setup/--local)
#   SLAVE_HOST         SSH host of slave device (required for remote)
#   SLAVE_USER         SSH user      (default: pi)
#   SLAVE_PORT         SSH port      (default: 22)
#   SLAVE_DIR          Install dir   (default: /opt/media-receiver)
#   SLAVE_SERVICE      systemd unit  (default: media-receiver)
#   SLAVE_ID           Unique ID     (default: hostname)
#   SLAVE_NAME         Display name  (default: SLAVE_ID)
#   SCAN_INTERVAL      Rescan rate   (default: 5m)
#   HEARTBEAT_INTERVAL Ping rate     (default: 15s)

set -euo pipefail

# ── Colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[deploy]${RESET} $*"; }
success() { echo -e "${GREEN}[deploy]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[deploy]${RESET} $*"; }
die()     { echo -e "${RED}[deploy] ERROR:${RESET} $*" >&2; exit 1; }

# ── Load config files ────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "$SCRIPT_DIR/.deploy.env" ]] && source "$SCRIPT_DIR/.deploy.env"
[[ -f "$SCRIPT_DIR/.slave.env" ]]  && source "$SCRIPT_DIR/.slave.env"

# ── Master defaults ──────────────────────────────────────────────────────────
VPS_HOST="${VPS_HOST:-}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/id_ed25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/media-server}"
SERVICE="${SERVICE:-media-server}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
REPO_URL="${REPO_URL:-github.com/bradselph/Media-Server-Pro.git}"
MASTER_URL="${MASTER_URL:-}"

GO_VERSION="$(get_go_version)"
NODE_MAJOR="$(get_node_version)"

# ── Slave defaults ───────────────────────────────────────────────────────────
SLAVE_HOST="${SLAVE_HOST:-}"
SLAVE_USER="${SLAVE_USER:-pi}"
SLAVE_PORT="${SLAVE_PORT:-22}"
SLAVE_DIR="${SLAVE_DIR:-/opt/media-receiver}"
SLAVE_SERVICE="${SLAVE_SERVICE:-media-receiver}"
SLAVE_ARCH="${SLAVE_ARCH:-}"
RECEIVER_API_KEY="${RECEIVER_API_KEY:-}"
MEDIA_DIRS="${MEDIA_DIRS:-}"
SLAVE_ID="${SLAVE_ID:-}"
SLAVE_NAME="${SLAVE_NAME:-}"
SCAN_INTERVAL="${SCAN_INTERVAL:-5m}"
HEARTBEAT_INTERVAL="${HEARTBEAT_INTERVAL:-15s}"

# ── Flags ────────────────────────────────────────────────────────────────────
DRY_RUN=false
FIX_ENV=false
ROLLBACK=false
SETUP=false
SETUP_RECEIVER=false
SLAVE_MODE=false
SLAVE_LOCAL=false
SLAVE_STOP=false

# Default branch: read from .env UPDATER_BRANCH, fall back to "main"
# Can be overridden by --branch or --dev flags (parsed after this block).
_BRANCH_DEFAULT=""
if [[ -f "$SCRIPT_DIR/.env" ]]; then
  _BRANCH_DEFAULT=$(grep -oP '(?<=^UPDATER_BRANCH=)\S+' "$SCRIPT_DIR/.env" 2>/dev/null || echo "")
fi
_BRANCH_DEFAULT="${_BRANCH_DEFAULT:-main}"
BRANCH="${_BRANCH_DEFAULT}"   # may be overridden by flag parsing below

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)         DRY_RUN=true          ; shift ;;
    --fix-env)         FIX_ENV=true          ; shift ;;
    --rollback)        ROLLBACK=true         ; shift ;;
    --setup)           SETUP=true            ; shift ;;
    --setup-receiver)  SETUP_RECEIVER=true   ; shift ;;
    --slave)           SLAVE_MODE=true       ; shift ;;
    --local)           SLAVE_LOCAL=true      ; shift ;;
    --stop)            SLAVE_STOP=true       ; shift ;;
    --branch)          BRANCH="$2"           ; shift 2 ;;
    --dev)             BRANCH="development"  ; shift ;;
    --help|-h)
      sed -n '/^# MASTER/,/^[^#]/p' "$0" | head -n -1
      echo ""
      sed -n '/^# SLAVE/,/^[^#]/p' "$0" | head -n -1
      echo ""
      sed -n '/^# INTERACTIVE/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

# ── Validation ───────────────────────────────────────────────────────────────
if $SLAVE_MODE; then
  if ! $SLAVE_LOCAL && ! $SLAVE_STOP && ! $SETUP; then
    [[ -z "$SLAVE_HOST" ]] && die "SLAVE_HOST is not set. Export it or add to .slave.env"
  fi
else
  [[ -z "$VPS_HOST" ]]     && die "VPS_HOST is not set. Export it or add to .deploy.env"
  [[ -z "$GITHUB_TOKEN" ]] && die "GITHUB_TOKEN is not set. Export it or add to .deploy.env"
fi

# ── Interactive branch selection (master mode, non-slave, no flag given) ──────
# If BRANCH wasn't set by a --branch/--dev flag, prompt the user to choose.
# Falls back to the default silently when stdin is not a terminal (CI/pipe).
if ! $SLAVE_MODE && [[ "$BRANCH" == "$_BRANCH_DEFAULT" ]] && [[ -t 0 ]]; then
  echo -e "${CYAN}[deploy]${RESET} Branch to deploy from [${BOLD}${_BRANCH_DEFAULT}${RESET}]:"
  echo "  1) main         — stable releases"
  echo "  2) development  — latest features"
  echo -n "  Choice (Enter = ${_BRANCH_DEFAULT}): "
  read -r _BRANCH_CHOICE </dev/tty
  case "$_BRANCH_CHOICE" in
    1|main)        BRANCH="main"        ;;
    2|dev*)        BRANCH="development" ;;
    "")            BRANCH="$_BRANCH_DEFAULT" ;;
    *)
      # Accept any branch name typed directly
      BRANCH="$_BRANCH_CHOICE"
      ;;
  esac
  echo ""
fi

# PAT in clone URL (stored in .git/config on VPS); use deploy key or credential helper for production.
CLONE_URL="https://${GITHUB_TOKEN}@${REPO_URL}"

# ── SSH auth setup ───────────────────────────────────────────────────────────
# Generates key if missing, strips passphrase, converts path for Windows,
# and installs public key on the remote host (one-time password prompt).
#
# After calling, SSH_OPTS and SCP_OPTS arrays are ready.
SSH_OPTS=()
SCP_OPTS=()

setup_ssh_auth() {
  local host="$1" user="$2" port="$3" keyfile="$4"

  # 1. Generate key if missing
  if [[ ! -f "$keyfile" ]]; then
    info "Generating SSH key at $keyfile..."
    mkdir -p "$(dirname "$keyfile")"
    ssh-keygen -t ed25519 -f "$keyfile" -N "" -C "mediaserver-deploy"
    echo ""
  fi

  # 2. Remove passphrase if present (BatchMode=yes cannot prompt)
  if ! ssh-keygen -y -P "" -f "$keyfile" &>/dev/null; then
    warn "SSH key has a passphrase — removing it for automated deploys."
    echo "    Enter the CURRENT key passphrase when prompted:"
    ssh-keygen -p -f "$keyfile" -N ""
    echo ""
    info "Passphrase removed."
  fi

  # 3. Convert POSIX path for Windows OpenSSH (Git Bash / MSYS2)
  local keyfile_ssh="$keyfile"
  if command -v cygpath &>/dev/null 2>&1; then
    keyfile_ssh="$(cygpath -m "$keyfile" 2>/dev/null || echo "$keyfile")"
  fi

  SSH_OPTS=(-i "$keyfile_ssh" -p "$port" -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8)
  SCP_OPTS=(-i "$keyfile_ssh" -P "$port" -o StrictHostKeyChecking=accept-new -o BatchMode=yes)

  # 4. Test key auth; install on remote if not yet authorised
  if ! ssh "${SSH_OPTS[@]}" "$user@$host" "exit 0" 2>/dev/null; then
    info "Key not yet authorised on $host — installing it now."
    echo "    Enter the password when prompted (one time only)."
    echo ""

    local pub_key
    pub_key="$(cat "${keyfile}.pub")"

    if command -v ssh-copy-id &>/dev/null; then
      ssh-copy-id -i "${keyfile}.pub" -p "$port" "$user@$host"
    else
      ssh -p "$port" \
          -o StrictHostKeyChecking=accept-new \
          -o ConnectTimeout=10 \
          "$user@$host" \
          "mkdir -p ~/.ssh && chmod 700 ~/.ssh && \
           echo '$pub_key' >> ~/.ssh/authorized_keys && \
           chmod 600 ~/.ssh/authorized_keys && \
           echo 'Key installed OK.'"
    fi

    if ! ssh "${SSH_OPTS[@]}" "$user@$host" "exit 0" 2>/dev/null; then
      die "SSH key auth still failing. Try: ssh -i \"$keyfile_ssh\" $user@$host"
    fi

    success "SSH key installed — future runs connect without a password."
    echo ""
  fi
}

# ── Helpers ──────────────────────────────────────────────────────────────────
# remote() is set up after SSH auth; it SSHes to the current target.
REMOTE_HOST=""
REMOTE_USER=""
remote() { ssh "${SSH_OPTS[@]}" "$REMOTE_USER@$REMOTE_HOST" -- "$@"; }

run_or_dry() {
  if $DRY_RUN; then
    info "[dry-run] $*"
  else
    "$@"
  fi
}

# Persist a key=value into the local .deploy.env
save_to_deploy_env() {
  local key="$1" val="$2"
  local file="${SCRIPT_DIR}/.deploy.env"
  touch "$file"
  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=${val}|" "$file"
  else
    echo "${key}=${val}" >> "$file"
  fi
}

# ── Helper: Extract Go version from go.mod ──────────────────────────────────
get_go_version() {
  local go_mod="${SCRIPT_DIR}/go.mod"
  if [[ -f "$go_mod" ]]; then
    grep -oP '(?<=^go )[0-9]+\.[0-9]+(?:\.[0-9]+)?' "$go_mod" 2>/dev/null || echo "1.26.2"
  else
    echo "1.26.2"
  fi
}

# ── Helper: Extract Node.js major version ───────────────────────────────────
get_node_version() {
  local pkg_json="${SCRIPT_DIR}/web/nuxt-ui/package.json"
  if [[ -f "$pkg_json" ]]; then
    local ver=""
    # 1. Try "engines": { "node": "..." }
    ver=$(grep -oP '(?<="node":\s*")[^"]*' "$pkg_json" 2>/dev/null | grep -oP '\d+' | head -1)
    # 2. Try "@types/node": "..." as fallback hint
    [[ -z "$ver" ]] && ver=$(grep -oP '(?<="@types/node":\s*")[^"]*' "$pkg_json" 2>/dev/null | grep -oP '\d+' | head -1)
    
    [[ -n "$ver" ]] && echo "$ver" && return
  fi

  # Default fallback
  echo "22"
}

# ══════════════════════════════════════════════════════════════════════════════
#
#   SLAVE MODE
#
# ══════════════════════════════════════════════════════════════════════════════
if $SLAVE_MODE; then

  # ── Slave local mode ──────────────────────────────────────────────────────
  if $SLAVE_LOCAL; then
    PID_FILE="${SCRIPT_DIR}/.media-receiver.pid"

    # --stop: kill running instance
    if $SLAVE_STOP; then
      if [[ -f "$PID_FILE" ]]; then
        PID=$(cat "$PID_FILE")
        if kill -0 "$PID" 2>/dev/null; then
          info "Stopping media-receiver (PID $PID)..."
          kill "$PID"
          rm -f "$PID_FILE"
          success "Stopped."
        else
          warn "Process $PID not running. Removing stale PID file."
          rm -f "$PID_FILE"
        fi
      else
        warn "No PID file found — slave may not be running."
      fi
      exit 0
    fi

    # Validate required vars for local mode
    [[ -z "$MASTER_URL" ]]       && die "MASTER_URL is required. Set it in .slave.env or export it."
    [[ -z "$RECEIVER_API_KEY" ]] && die "RECEIVER_API_KEY is required. Set it in .slave.env or export it."
    [[ -z "$MEDIA_DIRS" ]]       && die "MEDIA_DIRS is required. Set it in .slave.env or export it."

    LOCAL_ID="${SLAVE_ID:-$(hostname -s 2>/dev/null || hostname)}"
    LOCAL_NAME="${SLAVE_NAME:-$LOCAL_ID}"

    echo -e "\n${BOLD}=== Media Server Pro — Slave (Local) ===${RESET}\n"
    info "Master:     $MASTER_URL"
    info "Slave ID:   $LOCAL_ID"
    info "Media dirs: $MEDIA_DIRS"
    echo ""

    # Build native binary
    info "Building media-receiver..."
    EXT=""
    [[ "$(uname -s)" =~ MINGW|MSYS|CYGWIN ]] && EXT=".exe"
    OUT="${SCRIPT_DIR}/media-receiver${EXT}"

    if ! $DRY_RUN; then
      go build -o "$OUT" ./cmd/media-receiver
      success "Built → $OUT"
    else
      info "[dry-run] go build -o $OUT ./cmd/media-receiver"
      exit 0
    fi

    # Stop previous instance
    if [[ -f "$PID_FILE" ]]; then
      OLD_PID=$(cat "$PID_FILE")
      if kill -0 "$OLD_PID" 2>/dev/null; then
        info "Stopping previous instance (PID $OLD_PID)..."
        kill "$OLD_PID" 2>/dev/null || true
        sleep 1
      fi
      rm -f "$PID_FILE"
    fi

    # Start in background
    MASTER_URL="$MASTER_URL" \
    RECEIVER_API_KEY="$RECEIVER_API_KEY" \
    SLAVE_ID="$LOCAL_ID" \
    SLAVE_NAME="$LOCAL_NAME" \
    MEDIA_DIRS="$MEDIA_DIRS" \
    SCAN_INTERVAL="$SCAN_INTERVAL" \
    HEARTBEAT_INTERVAL="$HEARTBEAT_INTERVAL" \
      "$OUT" &

    echo $! > "$PID_FILE"
    success "Started media-receiver (PID $(cat "$PID_FILE"))"
    info "To stop: ./deploy.sh --slave --local --stop"
    echo ""
    echo "Press Ctrl+C to stop."
    echo ""
    wait
    exit 0
  fi

  # ── Remote slave mode ─────────────────────────────────────────────────────
  echo -e "\n${BOLD}=== Media Server Pro — Slave Deploy ===${RESET}\n"
  info "Slave      : $SLAVE_USER@$SLAVE_HOST:$SLAVE_PORT"
  info "Install dir: $SLAVE_DIR"
  info "Service    : $SLAVE_SERVICE"
  $DRY_RUN && warn "DRY RUN — no commands will execute"
  echo ""

  # SSH auth for slave device
  if ! $DRY_RUN; then
    setup_ssh_auth "$SLAVE_HOST" "$SLAVE_USER" "$SLAVE_PORT" "$KEY_FILE"
    REMOTE_HOST="$SLAVE_HOST"
    REMOTE_USER="$SLAVE_USER"
  else
    # Dry-run: set up minimal SSH_OPTS for logging
    KEY_FILE_SSH="$KEY_FILE"
    SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$SLAVE_PORT")
    SCP_OPTS=(-i "$KEY_FILE_SSH" -P "$SLAVE_PORT")
    REMOTE_HOST="$SLAVE_HOST"
    REMOTE_USER="$SLAVE_USER"
  fi

  # ── Detect slave architecture ─────────────────────────────────────────────
  detect_arch() {
    if [[ -n "$SLAVE_ARCH" ]]; then echo "$SLAVE_ARCH"; return; fi
    local raw
    raw=$(remote "uname -m" 2>/dev/null || echo "x86_64")
    case "$raw" in
      x86_64|amd64)  echo "amd64"  ;;
      aarch64|arm64) echo "arm64"  ;;
      armv7l|armv6l) echo "arm"    ;;
      *)             echo "amd64"  ;;
    esac
  }

  # ── Cross-compile slave binary ────────────────────────────────────────────
  build_slave_binary() {
    local arch="$1"
    goarm=""
    goarch="$arch"
    if [[ "$arch" == "arm" ]]; then
      goarch="arm"; goarm="6"
    fi
    info "Cross-compiling media-receiver for linux/$goarch${goarm:+ (GOARM=$goarm)}..."
    local out="$SCRIPT_DIR/media-receiver-linux-${goarch}"
    if $DRY_RUN; then
      info "[dry-run] CGO_ENABLED=0 GOOS=linux GOARCH=$goarch${goarm:+ GOARM=$goarm} go build -o $out ./cmd/media-receiver"
    else
      CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" ${goarm:+GOARM="$goarm"} \
        go build -o "$out" ./cmd/media-receiver
      success "Built → $out"
    fi
    echo "$out"
  }

  # ── Slave rollback ────────────────────────────────────────────────────────
  if $ROLLBACK; then
    info "Rolling back to media-receiver.bak..."
    remote "
      if [ ! -f '$SLAVE_DIR/media-receiver.bak' ]; then
        echo 'ERROR: no media-receiver.bak found'; exit 1
      fi
      sudo systemctl stop '$SLAVE_SERVICE' 2>/dev/null || true
      sudo mv '$SLAVE_DIR/media-receiver.bak' '$SLAVE_DIR/media-receiver'
      sudo chmod +x '$SLAVE_DIR/media-receiver'
      sudo systemctl start '$SLAVE_SERVICE'
      echo 'Rollback complete'
    "
    exit 0
  fi

  # ── Slave first-time setup ───────────────────────────────────────────────
  if $SETUP; then
    [[ -z "$MASTER_URL" ]]       && die "MASTER_URL is required for --setup"
    [[ -z "$RECEIVER_API_KEY" ]] && die "RECEIVER_API_KEY is required for --setup"
    [[ -z "$MEDIA_DIRS" ]]       && die "MEDIA_DIRS is required for --setup"

    info "Running first-time slave setup on $SLAVE_HOST..."

    # Detect arch and build
    ARCH=$(detect_arch)
    info "Detected arch: $ARCH"
    BINARY=$(build_slave_binary "$ARCH")

    run_or_dry remote "
      set -euo pipefail
      # Create system user
      if ! id mediareceiver &>/dev/null 2>&1; then
        echo '[setup] Creating mediareceiver system user...'
        sudo useradd -r -s /usr/sbin/nologin -d '$SLAVE_DIR' -m mediareceiver 2>/dev/null || \
        sudo adduser --system --no-create-home --shell /usr/sbin/nologin mediareceiver 2>/dev/null || true
      else
        echo '[setup] mediareceiver user already exists'
      fi
      echo '[setup] Creating directories...'
      sudo mkdir -p '$SLAVE_DIR'
      sudo chown mediareceiver:mediareceiver '$SLAVE_DIR' 2>/dev/null || \
      sudo chown mediareceiver '$SLAVE_DIR' 2>/dev/null || true
    "

    # Copy binary
    info "Copying binary to slave..."
    if ! $DRY_RUN; then
      scp "${SCP_OPTS[@]}" "$BINARY" "$SLAVE_USER@$SLAVE_HOST:/tmp/media-receiver"
      remote "
        sudo mv /tmp/media-receiver '$SLAVE_DIR/media-receiver'
        sudo chmod +x '$SLAVE_DIR/media-receiver'
        sudo chown mediareceiver '$SLAVE_DIR/media-receiver' 2>/dev/null || true
        echo '[setup] Binary installed'
      "
    else
      info "[dry-run] scp $BINARY $SLAVE_USER@$SLAVE_HOST:$SLAVE_DIR/media-receiver"
    fi

    # Write .env on slave
    info "Writing .env on slave..."
    RESOLVED_ID="${SLAVE_ID:-}"
    RESOLVED_NAME="${SLAVE_NAME:-}"
    if [[ -z "$RESOLVED_ID" ]] && ! $DRY_RUN; then
      RESOLVED_ID=$(remote "hostname -s 2>/dev/null || hostname" | tr -d '[:space:]')
    fi
    [[ -z "$RESOLVED_ID" ]] && RESOLVED_ID="slave-$(date +%s)"
    [[ -z "$RESOLVED_NAME" ]] && RESOLVED_NAME="$RESOLVED_ID"

    ENV_CONTENT="# Media Receiver Slave — configuration
# Generated by deploy.sh on $(date)

MASTER_URL=$MASTER_URL
RECEIVER_API_KEY=$RECEIVER_API_KEY
SLAVE_ID=$RESOLVED_ID
SLAVE_NAME=$RESOLVED_NAME
MEDIA_DIRS=$MEDIA_DIRS
SCAN_INTERVAL=$SCAN_INTERVAL
HEARTBEAT_INTERVAL=$HEARTBEAT_INTERVAL
"

    if ! $DRY_RUN; then
      echo "$ENV_CONTENT" | remote "sudo tee '$SLAVE_DIR/.env' > /dev/null && sudo chmod 600 '$SLAVE_DIR/.env'"
      success ".env written"
    else
      info "[dry-run] Would write .env to $SLAVE_DIR/.env"
    fi

    # Install systemd service
    info "Installing systemd service..."
    if [[ -f "$SCRIPT_DIR/systemd/media-receiver.service" ]]; then
      SERVICE_CONTENT=$(sed "s|__SLAVE_DIR__|$SLAVE_DIR|g" "$SCRIPT_DIR/systemd/media-receiver.service")
      if ! $DRY_RUN; then
        echo "$SERVICE_CONTENT" | remote "sudo tee '/etc/systemd/system/$SLAVE_SERVICE.service' > /dev/null"
        remote "
          sudo systemctl daemon-reload
          sudo systemctl enable '$SLAVE_SERVICE'
          sudo systemctl start '$SLAVE_SERVICE'
          echo '[setup] Service enabled and started'
        "
      else
        info "[dry-run] Would install systemd unit at /etc/systemd/system/$SLAVE_SERVICE.service"
      fi
    else
      warn "systemd/media-receiver.service not found — skipping service install"
    fi

    # Clean up local cross-compiled binary
    [[ -f "$BINARY" ]] && rm -f "$BINARY"

    echo ""
    success "Slave setup complete."
    if ! $DRY_RUN; then
      info "Status: $(remote "systemctl is-active '$SLAVE_SERVICE' 2>/dev/null || echo unknown")"
      info "Logs:   ssh -p $SLAVE_PORT $SLAVE_USER@$SLAVE_HOST 'journalctl -u $SLAVE_SERVICE -f'"
    fi
    echo ""
    exit 0
  fi

  # ── Slave fix-env ─────────────────────────────────────────────────────────
  if $FIX_ENV; then
    [[ -z "$MASTER_URL" ]]       && die "MASTER_URL is required for --fix-env"
    [[ -z "$RECEIVER_API_KEY" ]] && die "RECEIVER_API_KEY is required for --fix-env"

    info "Updating .env on slave..."
    run_or_dry remote "
      ENV='$SLAVE_DIR/.env'
      patch_or_add() {
        local key=\$1 val=\$2
        if grep -q \"^\$key=\" \"\$ENV\" 2>/dev/null; then
          sudo sed -i \"s|^\$key=.*|\$key=\$val|\" \"\$ENV\"
        else
          echo \"\$key=\$val\" | sudo tee -a \"\$ENV\" > /dev/null
        fi
        echo \"  \$key=\$val\"
      }
      patch_or_add MASTER_URL '$MASTER_URL'
      patch_or_add RECEIVER_API_KEY '$RECEIVER_API_KEY'
      ${MEDIA_DIRS:+patch_or_add MEDIA_DIRS "$MEDIA_DIRS"}
      ${SLAVE_ID:+patch_or_add SLAVE_ID "$SLAVE_ID"}
      ${SLAVE_NAME:+patch_or_add SLAVE_NAME "$SLAVE_NAME"}
      patch_or_add SCAN_INTERVAL '$SCAN_INTERVAL'
      patch_or_add HEARTBEAT_INTERVAL '$HEARTBEAT_INTERVAL'
      sudo systemctl restart '$SLAVE_SERVICE' 2>/dev/null || true
      echo 'Done — service restarted'
    "
    echo ""
    exit 0
  fi

  # ── Slave normal deploy (update binary) ──────────────────────────────────
  info "Detecting slave architecture..."
  ARCH=$(detect_arch)
  info "Detected arch: $ARCH"

  BINARY=$(build_slave_binary "$ARCH")

  info "Stopping service on slave..."
  run_or_dry remote "sudo systemctl stop '$SLAVE_SERVICE' 2>/dev/null || true"

  info "Backing up old binary..."
  run_or_dry remote "[ -f '$SLAVE_DIR/media-receiver' ] && sudo cp '$SLAVE_DIR/media-receiver' '$SLAVE_DIR/media-receiver.bak' && echo 'Backed up → media-receiver.bak' || true"

  info "Copying new binary..."
  if ! $DRY_RUN; then
    scp "${SCP_OPTS[@]}" "$BINARY" "$SLAVE_USER@$SLAVE_HOST:/tmp/media-receiver"
    remote "
      sudo mv /tmp/media-receiver '$SLAVE_DIR/media-receiver'
      sudo chmod +x '$SLAVE_DIR/media-receiver'
      sudo chown mediareceiver '$SLAVE_DIR/media-receiver' 2>/dev/null || true
      echo 'Binary updated'
    "
  else
    info "[dry-run] scp $BINARY $SLAVE_USER@$SLAVE_HOST:$SLAVE_DIR/media-receiver"
  fi

  info "Starting service..."
  run_or_dry remote "
    sudo systemctl start '$SLAVE_SERVICE'
    sleep 2
    STATUS=\$(systemctl is-active '$SLAVE_SERVICE' 2>/dev/null || echo unknown)
    echo \"Service status: \$STATUS\"
    if [ \"\$STATUS\" != 'active' ]; then
      echo '--- Last 20 log lines ---'
      journalctl -u '$SLAVE_SERVICE' --no-pager -n 20 2>/dev/null || true
      exit 1
    fi
  "

  # Clean up local cross-compiled binary
  [[ -f "$BINARY" ]] && rm -f "$BINARY"

  echo ""
  success "Slave deploy complete."
  if ! $DRY_RUN; then
    info "Status: $(remote "systemctl is-active '$SLAVE_SERVICE' 2>/dev/null || echo unknown")"
    info "Logs:   ssh -p $SLAVE_PORT $SLAVE_USER@$SLAVE_HOST 'journalctl -u $SLAVE_SERVICE -f'"
  fi
  echo ""
  exit 0
fi


# ══════════════════════════════════════════════════════════════════════════════
#
#   MASTER MODE
#
# ══════════════════════════════════════════════════════════════════════════════

echo -e "\n${BOLD}=== Media Server Pro — Deploy ===${RESET}\n"
info "VPS        : $VPS_USER@$VPS_HOST:$VPS_PORT"
info "Deploy dir : $DEPLOY_DIR"
info "Service    : $SERVICE"
info "Branch     : $BRANCH"
$DRY_RUN && warn "DRY RUN — no commands will execute"
echo ""

# Ensure SSH key is ready and authorised
if ! $DRY_RUN; then
  setup_ssh_auth "$VPS_HOST" "$VPS_USER" "$VPS_PORT" "$KEY_FILE"
  REMOTE_HOST="$VPS_HOST"
  REMOTE_USER="$VPS_USER"
fi

# ── Rollback ──────────────────────────────────────────────────────────────────
if $ROLLBACK; then
  info "Rolling back to server.bak..."
  remote "
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
  run_or_dry remote "
    set -euo pipefail
    export PATH=\$PATH:/usr/local/go/bin

    # ── System packages ──────────────────────────────────────────────────────
    echo '[setup] Updating apt and installing base packages...'
    sudo apt-get update -qq
    sudo apt-get install -y git curl build-essential ffmpeg ufw openssl

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
    sudo mkdir -p '$DEPLOY_DIR'/{videos,music,thumbnails,playlists,uploads,analytics,cache/hls,cache/remote,logs,data,data/remote_cache,backups,temp}

    # ── Copy .env template ───────────────────────────────────────────────────
    if [ ! -f '$DEPLOY_DIR/.env' ]; then
      if [ -f '$DEPLOY_DIR/.env.example' ]; then
        sudo cp '$DEPLOY_DIR/.env.example' '$DEPLOY_DIR/.env'
        sudo chmod 600 '$DEPLOY_DIR/.env'
        echo '[setup] Created .env from template — edit it with your settings!'
      else
        sudo touch '$DEPLOY_DIR/.env'
        sudo chmod 600 '$DEPLOY_DIR/.env'
        echo '[setup] Created empty .env — run ./setup.sh to generate configuration'
      fi
    fi

    # ── Set ownership ────────────────────────────────────────────────────────
    sudo chown -R mediaserver:mediaserver '$DEPLOY_DIR'

    # ── Install systemd service ──────────────────────────────────────────────
    if [ -f '$DEPLOY_DIR/systemd/media-server.service' ]; then
      sed 's|__DEPLOY_DIR__|$DEPLOY_DIR|g' '$DEPLOY_DIR/systemd/media-server.service' \
        | sudo tee '/etc/systemd/system/$SERVICE.service' > /dev/null
      sudo systemctl daemon-reload
      sudo systemctl enable '$SERVICE'
      echo '[setup] systemd service installed and enabled'
    fi

    # ── Receiver API key ─────────────────────────────────────────────────────
    if [ -f '$DEPLOY_DIR/.env' ] && ! grep -q '^RECEIVER_API_KEYS=.\+' '$DEPLOY_DIR/.env'; then
      echo '[setup] Generating receiver API key...'
      RECV_KEY=\$(openssl rand -hex 32 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null | tr -d '-' || date +%s | sha256sum | head -c 32)
      echo \"RECEIVER_API_KEYS=\$RECV_KEY\" | sudo tee -a '$DEPLOY_DIR/.env' > /dev/null
      echo \"[setup] Receiver API key → \$RECV_KEY\"
      echo '[setup] Keep this key secret — slave nodes need it to register with this master'
    fi

    # ── UFW firewall — open app port ─────────────────────────────────────────
    if command -v ufw &>/dev/null; then
      echo '[setup] Configuring UFW firewall...'
      sudo ufw allow ssh 2>/dev/null || true
      APP_PORT=\$(grep -oP '(?<=^SERVER_PORT=)[0-9]+' '$DEPLOY_DIR/.env' 2>/dev/null || echo 8080)
      sudo ufw allow \"\${APP_PORT}/tcp\" 2>/dev/null || true
      sudo ufw --force enable 2>/dev/null || true
      echo \"[setup] UFW: opened port \${APP_PORT}/tcp\"
    else
      echo '[setup] WARNING: ufw not available — open port manually'
    fi

    echo ''
    echo '[setup] Done! Next steps:'
    echo '  1. Edit $DEPLOY_DIR/.env with your database credentials and settings'
    echo '  2. Run: ./deploy.sh              (to build and start)'
    echo '  3. Run: ./deploy.sh --fix-env    (to auto-patch common settings)'
  "

  # Save the API key locally so slave setup can read it
  if ! $DRY_RUN; then
    RECV_KEY=$(remote "grep -oP '(?<=^RECEIVER_API_KEYS=)\S+' '$DEPLOY_DIR/.env' 2>/dev/null | head -1" 2>/dev/null || echo "")
    if [[ -n "$RECV_KEY" ]]; then
      save_to_deploy_env "RECEIVER_API_KEY" "$RECV_KEY"
      success "Saved RECEIVER_API_KEY to .deploy.env"
    fi
  fi
  exit 0
fi

# ── Fix env ───────────────────────────────────────────────────────────────────
if $FIX_ENV; then
  info "Patching .env on VPS..."
  remote "
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
    add_if_missing() {
      local key=\$1 val=\$2
      if ! grep -q \"^\$key=\" \"\$ENV\" 2>/dev/null; then
        echo \"\$key=\$val\" >> \"\$ENV\"
        echo \"  \$key=\$val (added)\"
      else
        echo \"  \$key=(unchanged)\"
      fi
    }
    add_if_missing SERVER_PORT 8080
    add_if_missing SERVER_HOST 127.0.0.1

    # Add TLS mode for remote databases
    DB_HOST=\$(grep -oP '(?<=^DATABASE_HOST=)\S+' \"\$ENV\" 2>/dev/null || echo localhost)
    if [ \"\$DB_HOST\" != 'localhost' ] && [ \"\$DB_HOST\" != '127.0.0.1' ]; then
      patch_or_add DATABASE_TLS_MODE skip-verify
    fi

    echo '  [remote media proxy]'
    patch_or_add REMOTE_MEDIA_ENABLED false
    patch_or_add REMOTE_MEDIA_CACHE_ENABLED true
    patch_or_add REMOTE_MEDIA_CACHE_SIZE_MB 1024
    patch_or_add FEATURE_REMOTE_MEDIA false

    # Receiver (master) settings
    echo '  [receiver / master node]'
    patch_or_add RECEIVER_ENABLED false
    patch_or_add FEATURE_RECEIVER false
    if ! grep -q '^RECEIVER_API_KEYS=.\+' \"\$ENV\"; then
      RECV_KEY=\$(openssl rand -hex 32 2>/dev/null || echo \"change-me-\$(date +%s)\")
      patch_or_add RECEIVER_API_KEYS \"\$RECV_KEY\"
      echo \"  [IMPORTANT] New receiver API key written — give it to your slave nodes\"
    fi

    # Downloader integration (proxy to standalone downloader service)
    # Only adds defaults if not already configured — never overrides existing values.
    echo '  [downloader integration]'
    if ! grep -q '^FEATURE_DOWNLOADER=' \"\$ENV\" 2>/dev/null; then
      echo 'FEATURE_DOWNLOADER=false' >> \"\$ENV\"
      echo '  FEATURE_DOWNLOADER=false (set to true to enable downloader tab)'
    fi
    if ! grep -q '^DOWNLOADER_ENABLED=' \"\$ENV\" 2>/dev/null; then
      echo 'DOWNLOADER_ENABLED=false' >> \"\$ENV\"
    fi
    if ! grep -q '^DOWNLOADER_URL=' \"\$ENV\" 2>/dev/null; then
      echo 'DOWNLOADER_URL=http://localhost:4000' >> \"\$ENV\"
      echo '  DOWNLOADER_URL=http://localhost:4000 (default — change if downloader runs on different port)'
    fi
    if ! grep -q '^DOWNLOADER_DOWNLOADS_DIR=' \"\$ENV\" 2>/dev/null; then
      echo 'DOWNLOADER_DOWNLOADS_DIR=' >> \"\$ENV\"
      echo '  DOWNLOADER_DOWNLOADS_DIR= (set to downloader downloads path to enable file import)'
    fi
    if ! grep -q '^DOWNLOADER_IMPORT_DIR=' \"\$ENV\" 2>/dev/null; then
      echo 'DOWNLOADER_IMPORT_DIR=' >> \"\$ENV\"
    fi

    # Hugging Face visual classification (mature content tagging)
    echo '  [Hugging Face classification]'
    patch_or_add HUGGINGFACE_ENABLED false
    patch_or_add FEATURE_HUGGINGFACE false
    if ! grep -q '^HUGGINGFACE_API_KEY=' \"\$ENV\" 2>/dev/null; then
      echo 'HUGGINGFACE_API_KEY=' >> \"\$ENV\"
      echo '  HUGGINGFACE_API_KEY= (empty — set + FEATURE_HUGGINGFACE=true to enable)'
    fi
    patch_or_add HUGGINGFACE_MODEL 'Salesforce/blip-image-captioning-large'
    patch_or_add HUGGINGFACE_MAX_FRAMES 3
    patch_or_add HUGGINGFACE_RATE_LIMIT 30
  "
  echo ""
fi

# ── Receiver / master-node setup ─────────────────────────────────────────────
if $SETUP_RECEIVER; then
  info "Configuring master receiver..."
  run_or_dry remote "
    set -euo pipefail
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

    # Enable receiver and remote media proxy
    patch_or_add RECEIVER_ENABLED true
    patch_or_add FEATURE_RECEIVER true
    patch_or_add REMOTE_MEDIA_ENABLED true
    patch_or_add FEATURE_REMOTE_MEDIA true
    patch_or_add REMOTE_MEDIA_CACHE_ENABLED true

    # Generate API key if not already present
    if ! grep -q '^RECEIVER_API_KEYS=.\+' \"\$ENV\" 2>/dev/null; then
      RECV_KEY=\$(openssl rand -hex 32)
      patch_or_add RECEIVER_API_KEYS \"\$RECV_KEY\"
      echo ''
      echo '[receiver] *** Receiver API key generated ***'
      echo \"[receiver] Key: \$RECV_KEY\"
    else
      RECV_KEY=\$(grep -oP '(?<=^RECEIVER_API_KEYS=)\S+' \"\$ENV\" | head -1)
      echo \"[receiver] Existing API key: \$RECV_KEY\"
    fi

    # Ensure cache directory exists
    sudo mkdir -p '$DEPLOY_DIR/data/remote_cache'
    sudo chown mediaserver:mediaserver '$DEPLOY_DIR/data/remote_cache' 2>/dev/null || true

    # Open the server port in UFW so slave nodes can reach this master
    APP_PORT=\$(grep -oP '(?<=^SERVER_PORT=)[0-9]+' \"\$ENV\" 2>/dev/null || echo 8080)
    if command -v ufw &>/dev/null; then
      sudo ufw allow \"\${APP_PORT}/tcp\" 2>/dev/null || true
      sudo ufw allow ssh 2>/dev/null || true
      sudo ufw --force enable 2>/dev/null || true
      echo \"[receiver] UFW: opened port \${APP_PORT}/tcp for slave connections\"
    fi

    # ── Nginx: allow unlimited body size for slave stream push ────────────
    NGINX_CONF=\$(find /etc/nginx/sites-enabled/ -name '*.conf' -exec grep -l 'proxy_pass.*127.0.0.1.*\${APP_PORT}' {} \\; 2>/dev/null | head -1)
    if [ -z \"\$NGINX_CONF\" ]; then
      NGINX_CONF=\$(find /etc/nginx/sites-enabled/ -name '*.conf' 2>/dev/null | head -1)
    fi
    if [ -n \"\$NGINX_CONF\" ] && ! grep -q 'client_max_body_size' \"\$NGINX_CONF\" 2>/dev/null; then
      echo \"[receiver] Adding client_max_body_size 0 to \$NGINX_CONF\"
      sudo sed -i '0,/proxy_pass.*http:/{/proxy_pass.*http:/a \\    client_max_body_size 0;\n}' \"\$NGINX_CONF\"
      sudo nginx -t 2>/dev/null && sudo systemctl reload nginx && echo '[receiver] nginx reloaded' \
        || echo '[receiver] WARNING: nginx config test failed — check manually'
    elif [ -n \"\$NGINX_CONF\" ]; then
      echo '[receiver] client_max_body_size already configured in nginx'
    else
      echo '[receiver] WARNING: no nginx config found — add client_max_body_size 0 manually'
    fi

    echo '[receiver] Restarting service to apply changes...'
    sudo systemctl restart '$SERVICE' && echo '[receiver] Service restarted OK' \
      || echo '[receiver] WARNING: restart failed — run: sudo systemctl restart $SERVICE'
  "

  # Save API key and MASTER_URL locally
  if ! $DRY_RUN; then
    RECV_KEY=$(remote "grep -oP '(?<=^RECEIVER_API_KEYS=)\S+' '$DEPLOY_DIR/.env' 2>/dev/null | head -1" 2>/dev/null || echo "")
    if [[ -n "$RECV_KEY" ]]; then
      if [[ -z "$MASTER_URL" ]]; then
        MASTER_URL="http://$VPS_HOST"
        warn "MASTER_URL not set — using bare IP: $MASTER_URL"
        warn "If behind nginx/CloudPanel set your domain:"
        warn "  Add MASTER_URL=https://yourdomain.com to .deploy.env then re-run --setup-receiver"
        echo ""
      fi
      save_to_deploy_env "RECEIVER_API_KEY" "$RECV_KEY"
      save_to_deploy_env "MASTER_URL" "$MASTER_URL"
      success "Saved to .deploy.env:"
      info "  MASTER_URL=$MASTER_URL"
      info "  RECEIVER_API_KEY=$RECV_KEY"
      echo ""
      success "Start slave on this machine: ./deploy.sh --slave --local"
    fi
  fi
  exit 0
fi

# ── Pull latest code ─────────────────────────────────────────────────────────
info "Pulling latest code on VPS (branch: $BRANCH)..."
run_or_dry remote "
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

  # Remove any stale git lock files left by an interrupted operation
  find .git -name '*.lock' -delete 2>/dev/null || true

  # Ensure the remote URL uses the token (only update if it changed)
  CURRENT_URL=\$(git remote get-url origin 2>/dev/null || echo '')
  if [ \"\$CURRENT_URL\" != '$CLONE_URL' ]; then
    git remote set-url origin '$CLONE_URL'
  fi

  git fetch origin '$BRANCH'
  git checkout '$BRANCH'
  git reset --hard 'origin/$BRANCH'

  echo \"[deploy] HEAD is now: \$(git log --oneline -1)\"
"

# ── Ensure dependencies are installed on VPS ─────────────────────────────────
info "Checking dependencies on VPS..."

run_or_dry remote "
  set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin

  # ── apt packages ───────────────────────────────────────────────────────────
  MISSING_APT=()
  for pkg in git curl build-essential ffmpeg ufw openssl; do
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

  # nuxi is a local devDependency in web/nuxt-ui/; npm ci installs it.
  echo '[deps] nuxi provided by web/nuxt-ui node_modules (local)'
"

# ── Build on VPS ──────────────────────────────────────────────────────────────

info "Building on VPS..."

run_or_dry remote "
  set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin:/usr/local/bin

  cd '$DEPLOY_DIR'

  # ── Nuxt UI frontend (always built before Go binary) ──────────────────────
  echo '[deploy] Building Nuxt UI frontend...'
  cd web/nuxt-ui

  if [ -f package-lock.json ]; then
    echo '[deploy] package-lock.json found — trying npm ci'
    if ! npm ci 2>&1; then
      echo '[deploy] npm ci failed (lock file out of sync) — falling back to npm install'
      npm install
    fi
  else
    echo '[deploy] No package-lock.json — using npm install'
    npm install
  fi

  # Nuxt generates to ../static/react/ (configured in nuxt.config.ts nitro.output.publicDir)
  npm run build
  cd ../..
  echo '[deploy] Nuxt UI build complete'

  # Stop service before replacing binary
  sudo systemctl stop '$SERVICE' 2>/dev/null || true

  # Backup old binary
  [ -f server ] && cp server server.bak && echo '[deploy] Backed up server -> server.bak'

  # Build Go binary (server only — slave binary is built by --slave deploy)
  echo '[deploy] Building Go binary...'
  VERSION=\$(cat VERSION 2>/dev/null || echo 0.0.0)
  go build \\
    -ldflags \"-X main.Version=\$VERSION -X main.BuildDate=\$(date +%Y-%m-%d)\" \\
    -o server ./cmd/server

  echo '[deploy] Build complete'
"

# ── Update systemd unit if changed ────────────────────────────────────────────
run_or_dry remote "
  if [ -f '$DEPLOY_DIR/systemd/media-server.service' ]; then
    sed 's|__DEPLOY_DIR__|$DEPLOY_DIR|g' '$DEPLOY_DIR/systemd/media-server.service' \
      | sudo tee '/etc/systemd/system/$SERVICE.service' > /dev/null
    sudo systemctl daemon-reload
  fi
"

# ── Ensure service user and directories ───────────────────────────────────────
run_or_dry remote "
  # Create the service user if it doesn't exist yet
  if ! id mediaserver &>/dev/null 2>&1; then
    echo '[deploy] Creating mediaserver system user...'
    sudo useradd -r -s /usr/sbin/nologin -d '$DEPLOY_DIR' -m mediaserver
  fi

  sudo mkdir -p '$DEPLOY_DIR'/{videos,music,thumbnails,playlists,uploads,analytics,cache/hls,cache/remote,logs,data,data/remote_cache,backups,temp}

  # Secure .env file permissions
  [ -f '$DEPLOY_DIR/.env' ] && sudo chmod 600 '$DEPLOY_DIR/.env'

  sudo chown -R mediaserver:mediaserver '$DEPLOY_DIR'
"

# ── Start & health check ─────────────────────────────────────────────────────
info "Starting $SERVICE..."
run_or_dry remote "
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

  # Poll health endpoint until fully ready (200) or timeout after 90s
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
  info "Status: $(remote "systemctl is-active '$SERVICE' 2>/dev/null || echo unknown")"
  info "Logs:   ssh -p $VPS_PORT $VPS_USER@$VPS_HOST 'journalctl -u $SERVICE -f'"
fi
echo ""
