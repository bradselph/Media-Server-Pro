#!/usr/bin/env bash
# deploy-slave.sh — Build and deploy the media-receiver slave node.
#
# TWO MODES:
#   --local   Build and run the slave on THIS machine (no SSH needed).
#             Use this when the slave is the same machine you are on.
#   (default) Cross-compile and deploy to a REMOTE Linux device over SSH.
#             The slave device only needs: bash, systemd, scp/ssh access.
#
# Usage:
#   ./deploy-slave.sh --local            # build + run slave on this machine
#   ./deploy-slave.sh --local --stop     # stop the locally running slave
#   ./deploy-slave.sh --setup            # first-time setup on a REMOTE device
#   ./deploy-slave.sh                    # update binary on a REMOTE device
#   ./deploy-slave.sh --fix-env          # re-write .env on remote slave
#   ./deploy-slave.sh --rollback         # restore media-receiver.bak on remote slave
#   ./deploy-slave.sh --dry-run          # print what would happen, do nothing
#   ./deploy-slave.sh --help             # show this help
#
# Config (set in shell, .slave.env, or as flags — all modes):
#   MASTER_URL         Full URL of the master server         (required)
#   RECEIVER_API_KEY   API key copied from the master        (required)
#   MEDIA_DIRS         Comma-separated media directories     (required)
#
# Remote-only config:
#   SLAVE_HOST         SSH host of the slave device          (required for remote)
#   SLAVE_USER         SSH user              (default: pi)
#   SLAVE_PORT         SSH port              (default: 22)
#   KEY_FILE           SSH private key path  (default: ~/.ssh/id_ed25519)
#   SLAVE_DIR          Install directory     (default: /opt/media-receiver)
#   SLAVE_SERVICE      systemd unit name     (default: media-receiver)
#   SLAVE_ARCH         Target arch           (default: auto-detect via uname -m)
#
# Common optional config:
#   SLAVE_ID           Unique slave ID       (default: hostname)
#   SLAVE_NAME         Display name          (default: same as SLAVE_ID)
#   LISTEN_ADDR        Port to listen on     (default: :9090)
#   SCAN_INTERVAL      Catalog rescan rate   (default: 5m)
#   HEARTBEAT_INTERVAL Keepalive ping rate   (default: 15s)

set -euo pipefail

# ── Colour helpers ─────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[slave]${RESET} $*"; }
success() { echo -e "${GREEN}[slave]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[slave]${RESET} $*"; }
die()     { echo -e "${RED}[slave] ERROR:${RESET} $*" >&2; exit 1; }

# ── Load config — .deploy.env first (master settings), then .slave.env (overrides) ──
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# .deploy.env is written by deploy.sh --setup-receiver and contains MASTER_URL
# and RECEIVER_API_KEY so you never have to copy them manually.
[[ -f "$SCRIPT_DIR/.deploy.env" ]] && source "$SCRIPT_DIR/.deploy.env"
# .slave.env can override anything from .deploy.env (slave-specific settings)
[[ -f "$SCRIPT_DIR/.slave.env" ]]  && source "$SCRIPT_DIR/.slave.env"

# ── Defaults ──────────────────────────────────────────────────────────────────
SLAVE_HOST="${SLAVE_HOST:-}"
SLAVE_USER="${SLAVE_USER:-pi}"
SLAVE_PORT="${SLAVE_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/id_ed25519}"
SLAVE_DIR="${SLAVE_DIR:-/opt/media-receiver}"
SLAVE_SERVICE="${SLAVE_SERVICE:-media-receiver}"
SLAVE_ARCH="${SLAVE_ARCH:-}"       # empty = auto-detect

# Slave runtime config (written to .env on the slave)
MASTER_URL="${MASTER_URL:-}"
RECEIVER_API_KEY="${RECEIVER_API_KEY:-}"
MEDIA_DIRS="${MEDIA_DIRS:-}"
SLAVE_ID="${SLAVE_ID:-}"
SLAVE_NAME="${SLAVE_NAME:-}"
LISTEN_ADDR="${LISTEN_ADDR:-:9090}"
SCAN_INTERVAL="${SCAN_INTERVAL:-5m}"
HEARTBEAT_INTERVAL="${HEARTBEAT_INTERVAL:-15s}"

DRY_RUN=false
SETUP=false
FIX_ENV=false
ROLLBACK=false
LOCAL=false
LOCAL_STOP=false

# ── Argument parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --local)    LOCAL=true    ; shift ;;
    --stop)     LOCAL_STOP=true ; shift ;;
    --setup)    SETUP=true    ; shift ;;
    --fix-env)  FIX_ENV=true  ; shift ;;
    --rollback) ROLLBACK=true ; shift ;;
    --dry-run)  DRY_RUN=true  ; shift ;;
    --help|-h)
      sed -n '/^# Usage:/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

# ── LOCAL MODE — run slave on this machine, no SSH ────────────────────────────
if $LOCAL; then
  [[ -z "$MASTER_URL" ]]       && die "MASTER_URL is required. Set it in .slave.env or export it."
  [[ -z "$RECEIVER_API_KEY" ]] && die "RECEIVER_API_KEY is required. Set it in .slave.env or export it."
  [[ -z "$MEDIA_DIRS" ]]       && die "MEDIA_DIRS is required. Set it in .slave.env or export it."

  # Resolve SLAVE_ID
  LOCAL_ID="${SLAVE_ID:-$(hostname -s 2>/dev/null || hostname)}"
  LOCAL_NAME="${SLAVE_NAME:-$LOCAL_ID}"

  # PID file for tracking the local process
  PID_FILE="${SCRIPT_DIR}/.media-receiver.pid"

  if $LOCAL_STOP; then
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

  echo -e "\n${BOLD}=== Media Server Pro — Slave (Local) ===${RESET}\n"
  info "Master:     $MASTER_URL"
  info "Slave ID:   $LOCAL_ID"
  info "Media dirs: $MEDIA_DIRS"
  info "Listen:     $LISTEN_ADDR"
  echo ""

  # Build the native binary for this platform
  info "Building media-receiver..."
  EXT=""
  [[ "$(uname -s)" =~ MINGW|MSYS|CYGWIN ]] && EXT=".exe"
  OUT="${SCRIPT_DIR}/media-receiver${EXT}"
  if ! $DRY_RUN; then
    go build -o "$OUT" ./cmd/media-receiver
    success "Built → $OUT"
  else
    info "[dry-run] go build -o $OUT ./cmd/media-receiver"
  fi

  # Stop any already-running instance
  if [[ -f "$PID_FILE" ]]; then
    OLD_PID=$(cat "$PID_FILE")
    if kill -0 "$OLD_PID" 2>/dev/null; then
      info "Stopping previous instance (PID $OLD_PID)..."
      kill "$OLD_PID" 2>/dev/null || true
      sleep 1
    fi
    rm -f "$PID_FILE"
  fi

  if $DRY_RUN; then
    info "[dry-run] Would start: MASTER_URL=$MASTER_URL RECEIVER_API_KEY=*** ... $OUT"
    exit 0
  fi

  # Start in background, inherit stdout/stderr so output appears in terminal
  MASTER_URL="$MASTER_URL" \
  RECEIVER_API_KEY="$RECEIVER_API_KEY" \
  SLAVE_ID="$LOCAL_ID" \
  SLAVE_NAME="$LOCAL_NAME" \
  MEDIA_DIRS="$MEDIA_DIRS" \
  LISTEN_ADDR="$LISTEN_ADDR" \
  SCAN_INTERVAL="$SCAN_INTERVAL" \
  HEARTBEAT_INTERVAL="$HEARTBEAT_INTERVAL" \
    "$OUT" &

  echo $! > "$PID_FILE"
  success "Started media-receiver (PID $(cat "$PID_FILE"))"
  info "To stop: ./deploy-slave.sh --local --stop"
  info "To follow output, re-run without backgrounding: remove the '&' from this script."
  echo ""
  echo "Press Ctrl+C to stop."
  echo ""
  # Wait so the user can see output and Ctrl+C cleanly
  wait
  exit 0
fi

[[ -z "$SLAVE_HOST" ]] && die "SLAVE_HOST is not set. Export it or add to .slave.env"

# ── SSH auth setup ─────────────────────────────────────────────────────────────
setup_ssh_auth() {
  if [[ ! -f "$KEY_FILE" ]]; then
    info "Generating SSH key at $KEY_FILE..."
    mkdir -p "$(dirname "$KEY_FILE")"
    ssh-keygen -t ed25519 -f "$KEY_FILE" -N "" -C "slave-deploy"
    echo ""
  fi

  # Remove passphrase if present (required for BatchMode=yes)
  if ! ssh-keygen -y -P "" -f "$KEY_FILE" &>/dev/null; then
    warn "SSH key has a passphrase — removing it for automated deploys."
    ssh-keygen -p -f "$KEY_FILE" -N ""
    info "Passphrase removed."
    echo ""
  fi

  # Windows path conversion (Git Bash / MSYS2)
  KEY_FILE_SSH="$KEY_FILE"
  if command -v cygpath &>/dev/null 2>&1; then
    KEY_FILE_SSH="$(cygpath -m "$KEY_FILE" 2>/dev/null || echo "$KEY_FILE")"
  fi

  SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$SLAVE_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8)
  SCP_OPTS=(-i "$KEY_FILE_SSH" -P "$SLAVE_PORT" -o StrictHostKeyChecking=accept-new -o BatchMode=yes)

  if ! ssh "${SSH_OPTS[@]}" "$SLAVE_USER@$SLAVE_HOST" "exit 0" 2>/dev/null; then
    info "Key not yet authorised on slave — installing it now."
    echo "    Enter the slave password when prompted (one time only)."
    echo ""
    local pub_key
    pub_key="$(cat "${KEY_FILE}.pub")"
    if command -v ssh-copy-id &>/dev/null; then
      ssh-copy-id -i "${KEY_FILE}.pub" -p "$SLAVE_PORT" "$SLAVE_USER@$SLAVE_HOST"
    else
      ssh -p "$SLAVE_PORT" -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 \
          "$SLAVE_USER@$SLAVE_HOST" \
          "mkdir -p ~/.ssh && chmod 700 ~/.ssh && \
           echo '$pub_key' >> ~/.ssh/authorized_keys && \
           chmod 600 ~/.ssh/authorized_keys && echo 'Key installed.'"
    fi
    if ! ssh "${SSH_OPTS[@]}" "$SLAVE_USER@$SLAVE_HOST" "exit 0" 2>/dev/null; then
      die "SSH key auth still failing. Try: ssh -i \"$KEY_FILE_SSH\" $SLAVE_USER@$SLAVE_HOST"
    fi
    success "SSH key installed."
    echo ""
  fi
}

slave() { ssh "${SSH_OPTS[@]}" "$SLAVE_USER@$SLAVE_HOST" -- "$@"; }

run_or_dry() {
  if $DRY_RUN; then
    info "[dry-run] $*"
  else
    "$@"
  fi
}

# ── Detect slave architecture ─────────────────────────────────────────────────
detect_arch() {
  if [[ -n "$SLAVE_ARCH" ]]; then
    echo "$SLAVE_ARCH"
    return
  fi
  local raw
  raw=$(slave "uname -m" 2>/dev/null || echo "x86_64")
  case "$raw" in
    x86_64|amd64)  echo "amd64"  ;;
    aarch64|arm64) echo "arm64"  ;;
    armv7l|armv6l) echo "arm"    ;;
    *)             echo "amd64"  ;;
  esac
}

# ── Cross-compile the slave binary ────────────────────────────────────────────
build_binary() {
  local arch="$1"
  local goarm=""
  local goarch="$arch"

  if [[ "$arch" == "arm" ]]; then
    goarch="arm"
    goarm="6"   # ARMv6 covers Raspberry Pi Zero, Pi 1, Pi 2, Pi 3, Pi 4
  fi

  info "Cross-compiling media-receiver for linux/$goarch${goarm:+ (GOARM=$goarm)}..."

  local out="$SCRIPT_DIR/media-receiver-linux-${goarch}"
  if $DRY_RUN; then
    info "[dry-run] CGO_ENABLED=0 GOOS=linux GOARCH=$goarch${goarm:+ GOARM=$goarm} go build -o $out ./cmd/media-receiver"
    out="$out"
  else
    CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" ${goarm:+GOARM="$goarm"} \
      go build -o "$out" ./cmd/media-receiver
    success "Built → $out"
  fi
  echo "$out"
}

# ── Print header ──────────────────────────────────────────────────────────────
echo -e "\n${BOLD}=== Media Server Pro — Slave Deploy ===${RESET}\n"
info "Slave      : $SLAVE_USER@$SLAVE_HOST:$SLAVE_PORT"
info "Install dir: $SLAVE_DIR"
info "Service    : $SLAVE_SERVICE"
$DRY_RUN && warn "DRY RUN — no commands will execute"
echo ""

if ! $DRY_RUN; then
  setup_ssh_auth
fi

# ── SSH_OPTS / SCP_OPTS must be set before any slave() calls below ────────────
# For dry-run mode where setup_ssh_auth is skipped, initialise with safe defaults.
if $DRY_RUN; then
  KEY_FILE_SSH="$KEY_FILE"
  SSH_OPTS=(-i "$KEY_FILE_SSH" -p "$SLAVE_PORT")
  SCP_OPTS=(-i "$KEY_FILE_SSH" -P "$SLAVE_PORT")
fi

# ── Rollback ──────────────────────────────────────────────────────────────────
if $ROLLBACK; then
  info "Rolling back to media-receiver.bak..."
  slave "
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

# ── First-time setup ──────────────────────────────────────────────────────────
if $SETUP; then
  [[ -z "$MASTER_URL" ]]       && die "MASTER_URL is required for --setup"
  [[ -z "$RECEIVER_API_KEY" ]] && die "RECEIVER_API_KEY is required for --setup"
  [[ -z "$MEDIA_DIRS" ]]       && die "MEDIA_DIRS is required for --setup"

  info "Running first-time slave setup on $SLAVE_HOST..."

  # Detect architecture before building
  info "Detecting slave architecture..."
  ARCH=$(detect_arch)
  info "Detected arch: $ARCH"

  # Build binary
  BINARY=$(build_binary "$ARCH")

  run_or_dry slave "
    set -euo pipefail

    # ── Create system user ──────────────────────────────────────────────────
    if ! id mediareceiver &>/dev/null 2>&1; then
      echo '[setup] Creating mediareceiver system user...'
      sudo useradd -r -s /usr/sbin/nologin -d '$SLAVE_DIR' -m mediareceiver 2>/dev/null || \
      sudo adduser --system --no-create-home --shell /usr/sbin/nologin mediareceiver 2>/dev/null || true
    else
      echo '[setup] mediareceiver user already exists'
    fi

    # ── Create directories ──────────────────────────────────────────────────
    echo '[setup] Creating directories...'
    sudo mkdir -p '$SLAVE_DIR'
    sudo chown mediareceiver:mediareceiver '$SLAVE_DIR' 2>/dev/null || \
    sudo chown mediareceiver '$SLAVE_DIR' 2>/dev/null || true
  "

  # Copy binary
  info "Copying binary to slave..."
  if ! $DRY_RUN; then
    scp "${SCP_OPTS[@]}" "$BINARY" "$SLAVE_USER@$SLAVE_HOST:/tmp/media-receiver"
    slave "
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
  # Resolve SLAVE_ID default to hostname if not provided
  RESOLVED_ID="${SLAVE_ID:-}"
  RESOLVED_NAME="${SLAVE_NAME:-}"
  if [[ -z "$RESOLVED_ID" ]] && ! $DRY_RUN; then
    RESOLVED_ID=$(slave "hostname -s 2>/dev/null || hostname" | tr -d '[:space:]')
  fi
  [[ -z "$RESOLVED_ID" ]] && RESOLVED_ID="slave-$(date +%s)"
  [[ -z "$RESOLVED_NAME" ]] && RESOLVED_NAME="$RESOLVED_ID"

  ENV_CONTENT="# Media Receiver Slave — configuration
# Generated by deploy-slave.sh on $(date)

MASTER_URL=$MASTER_URL
RECEIVER_API_KEY=$RECEIVER_API_KEY
SLAVE_ID=$RESOLVED_ID
SLAVE_NAME=$RESOLVED_NAME
MEDIA_DIRS=$MEDIA_DIRS
LISTEN_ADDR=$LISTEN_ADDR
SCAN_INTERVAL=$SCAN_INTERVAL
HEARTBEAT_INTERVAL=$HEARTBEAT_INTERVAL
"

  if ! $DRY_RUN; then
    echo "$ENV_CONTENT" | slave "sudo tee '$SLAVE_DIR/.env' > /dev/null && sudo chmod 600 '$SLAVE_DIR/.env'"
    success ".env written"
  else
    info "[dry-run] Would write .env to $SLAVE_DIR/.env:"
    echo "$ENV_CONTENT"
  fi

  # Install systemd service
  info "Installing systemd service..."
  SERVICE_CONTENT=$(sed "s|__SLAVE_DIR__|$SLAVE_DIR|g" "$SCRIPT_DIR/systemd/media-receiver.service")
  if ! $DRY_RUN; then
    echo "$SERVICE_CONTENT" | slave "sudo tee '/etc/systemd/system/$SLAVE_SERVICE.service' > /dev/null"
    slave "
      sudo systemctl daemon-reload
      sudo systemctl enable '$SLAVE_SERVICE'
      sudo systemctl start '$SLAVE_SERVICE'
      echo '[setup] Service enabled and started'
    "
  else
    info "[dry-run] Would install systemd unit at /etc/systemd/system/$SLAVE_SERVICE.service"
  fi

  echo ""
  success "Slave setup complete."
  if ! $DRY_RUN; then
    info "Status: $(slave "systemctl is-active '$SLAVE_SERVICE' 2>/dev/null || echo unknown")"
    info "Logs:   ssh -p $SLAVE_PORT $SLAVE_USER@$SLAVE_HOST 'journalctl -u $SLAVE_SERVICE -f'"
  fi
  echo ""
  exit 0
fi

# ── Fix env ───────────────────────────────────────────────────────────────────
if $FIX_ENV; then
  [[ -z "$MASTER_URL" ]]       && die "MASTER_URL is required for --fix-env"
  [[ -z "$RECEIVER_API_KEY" ]] && die "RECEIVER_API_KEY is required for --fix-env"

  info "Updating .env on slave..."
  run_or_dry slave "
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
    ${MEDIA_DIRS:+patch_or_add MEDIA_DIRS '$MEDIA_DIRS'}
    ${SLAVE_ID:+patch_or_add SLAVE_ID '$SLAVE_ID'}
    ${SLAVE_NAME:+patch_or_add SLAVE_NAME '$SLAVE_NAME'}
    patch_or_add LISTEN_ADDR '$LISTEN_ADDR'
    patch_or_add SCAN_INTERVAL '$SCAN_INTERVAL'
    patch_or_add HEARTBEAT_INTERVAL '$HEARTBEAT_INTERVAL'
    sudo systemctl restart '$SLAVE_SERVICE' 2>/dev/null || true
    echo 'Done — service restarted'
  "
  echo ""
  exit 0
fi

# ── Normal deploy — update binary only ───────────────────────────────────────
info "Detecting slave architecture..."
ARCH=$(detect_arch)
info "Detected arch: $ARCH"

BINARY=$(build_binary "$ARCH")

info "Stopping service on slave..."
run_or_dry slave "sudo systemctl stop '$SLAVE_SERVICE' 2>/dev/null || true"

info "Backing up old binary..."
run_or_dry slave "[ -f '$SLAVE_DIR/media-receiver' ] && sudo cp '$SLAVE_DIR/media-receiver' '$SLAVE_DIR/media-receiver.bak' && echo 'Backed up → media-receiver.bak' || true"

info "Copying new binary..."
if ! $DRY_RUN; then
  scp "${SCP_OPTS[@]}" "$BINARY" "$SLAVE_USER@$SLAVE_HOST:/tmp/media-receiver"
  slave "
    sudo mv /tmp/media-receiver '$SLAVE_DIR/media-receiver'
    sudo chmod +x '$SLAVE_DIR/media-receiver'
    sudo chown mediareceiver '$SLAVE_DIR/media-receiver' 2>/dev/null || true
    echo 'Binary updated'
  "
else
  info "[dry-run] scp $BINARY $SLAVE_USER@$SLAVE_HOST:$SLAVE_DIR/media-receiver"
fi

info "Starting service..."
run_or_dry slave "
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
  info "Status: $(slave "systemctl is-active '$SLAVE_SERVICE' 2>/dev/null || echo unknown")"
  info "Logs:   ssh -p $SLAVE_PORT $SLAVE_USER@$SLAVE_HOST 'journalctl -u $SLAVE_SERVICE -f'"
fi
echo ""
