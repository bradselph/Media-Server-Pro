#!/usr/bin/env bash
# deploy.sh — Deploy Media Server Pro to a VPS.
#
# Usage:
#   ./deploy.sh                         # pull + build + restart on VPS (branch from .env or main)
#   ./deploy.sh --branch main           # deploy from the stable main branch
#   ./deploy.sh --branch development    # deploy from the development branch
#   ./deploy.sh --dev                   # shorthand for --branch development
#   ./deploy.sh --setup                 # first-time VPS provisioning
#   ./deploy.sh --setup-receiver        # configure receiver API keys for federated peers
#   ./deploy.sh --setup-hidrive         # mount an IONOS HiDrive WebDAV share as
#                                       # a read-only video source (rclone +
#                                       # systemd unit). Reads the HIDRIVE_*
#                                       # knobs from .deploy.env. Re-run after
#                                       # toggling HIDRIVE_ENABLED to add or
#                                       # tear down the mount.
#   ./deploy.sh --fix-env               # patch .env on VPS (incl. optional: receiver, Hugging Face)
#   ./deploy.sh --rollback              # restore server.bak on VPS
#   ./deploy.sh --dry-run               # preview commands without executing
#   ./deploy.sh --configure             # interactive: prompt for ★ NEW knobs
#                                       # and exit. Equivalent to running
#                                       # ./deploy-configure.sh directly.
#   ./deploy.sh --review                # interactive: re-walk every knob,
#                                       # even ones already set, then exit.
#   ./deploy.sh --docker                # alternative: deploy via the GHCR
#                                       # image + docker compose instead of
#                                       # native build + systemd. Native
#                                       # path stays the default; --docker
#                                       # is opt-in for that single run.
#                                       # Stops/disables the systemd unit
#                                       # if it's running (port conflict),
#                                       # then `docker compose up -d`.
#
# Knob system:
#   .deploy.env is the single source of truth for deploy-time + runtime
#   config. Knobs are registered in deploy-knobs.sh — each one is scoped:
#     vps       — used locally by this script (VPS_HOST, SERVICE, …)
#     toolchain — version pins (MSP_GO_VERSION, MSP_NODE_MAJOR)
#     runtime   — upserted into $DEPLOY_DIR/.env on every deploy
#     build     — exported into the on-VPS `npm run build` shell so
#                 NUXT_PUBLIC_* knobs (e.g. NUXT_PUBLIC_GA_ID) are
#                 baked into the Nuxt bundle.
#   On the first run after a knob is added to deploy-knobs.sh, the
#   interactive prompter walks the operator through ★ NEW entries
#   before deploying. Tip: ./deploy.sh --configure to walk on demand.
#
# Federated peers (multi-server libraries) are configured at runtime through
# the admin UI or POST /api/admin/peer/connect — no separate slave binary or
# deploy step is needed.
#
# INTERACTIVE SETUP:
#   ./setup.sh                          # guided first-time setup wizard
#
# Configuration (set in shell or .deploy.env):
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

# ── Load config files ────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "$SCRIPT_DIR/.deploy.env" ]] && source "$SCRIPT_DIR/.deploy.env"

# ── Knob registry ────────────────────────────────────────────────────────────
# Populates KNOB_ORDER, KNOB_DESCRIPTION, KNOB_DEFAULT, KNOB_SCOPE, KNOB_SECTION,
# KNOB_SENSITIVE and the derived FORWARDED_RUNTIME / FORWARDED_BUILD arrays
# this script walks when generating the on-VPS env file and the npm build
# env prefix. Failure to source is fatal — the knob system is required.
if [[ -f "$SCRIPT_DIR/deploy-knobs.sh" ]]; then
  # shellcheck disable=SC1091
  source "$SCRIPT_DIR/deploy-knobs.sh"
else
  echo "[deploy] ERROR: deploy-knobs.sh not found alongside deploy.sh" >&2
  exit 1
fi

# ── Helper: Extract Go version from go.mod ──────────────────────────────────
get_go_version() {
  local go_mod="${SCRIPT_DIR}/go.mod"
  local default_version="1.26.2"

  if [[ -f "$go_mod" ]]; then
    # Extract version using basic grep and cut for portability
    # Expected line in go.mod: "go 1.26.2"
    grep "^go [0-9]" "$go_mod" | head -n 1 | cut -d' ' -f2 || echo "$default_version"
  else
    echo "$default_version"
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

# ── Flags ────────────────────────────────────────────────────────────────────
DRY_RUN=false
FIX_ENV=false
ROLLBACK=false
SETUP=false
SETUP_RECEIVER=false
SETUP_HIDRIVE=false
CONFIGURE_ONLY=false
REVIEW_ONLY=false
# DOCKER_MODE swaps the build+systemd backend for `docker compose up` against
# the GHCR image. Native path is the default; --docker is opt-in per run and
# does NOT persist into .deploy.env — each deploy chooses its own backend.
# When set, deploy.sh:
#   1. Forwards the runtime knob set to $DEPLOY_DIR/.env.docker (not .env --
#      keeps the two deploy modes from clobbering each other's config file).
#   2. Skips the Go/Node toolchain install + native build steps.
#   3. Stops + disables the systemd unit so the host port is free for compose.
#   4. Installs docker if not present, then `docker compose pull && up -d`.
#   5. Polls the same /health endpoint as the native path.
DOCKER_MODE=false

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
    --setup-hidrive)   SETUP_HIDRIVE=true    ; shift ;;
    --branch)          BRANCH="$2"           ; shift 2 ;;
    --dev)             BRANCH="development"  ; shift ;;
    --configure)       CONFIGURE_ONLY=true   ; shift ;;
    --review)          REVIEW_ONLY=true      ; shift ;;
    --docker)          DOCKER_MODE=true      ; shift ;;
    --help|-h)
      sed -n '/^# Usage/,/^[^#]/p' "$0" | head -n -1
      exit 0
      ;;
    *) die "Unknown option: $1 (use --help)" ;;
  esac
done

# ── Knob walk (interactive: prompts only for ★ NEW knobs) ────────────────────
# Runs before validation so an operator who just pulled a release with a new
# knob gets prompted for it before deploy.sh complains about missing values.
# Non-TTY (CI, piped) falls back to --quiet (apply defaults, no prompts) inside
# deploy-configure.sh itself.
run_configure() {
  local mode="${1:-walk}"
  local args=()
  [[ "$mode" == "review" ]] && args+=(--review)
  if [[ -x "$SCRIPT_DIR/deploy-configure.sh" ]]; then
    "$SCRIPT_DIR/deploy-configure.sh" "${args[@]}"
  else
    bash "$SCRIPT_DIR/deploy-configure.sh" "${args[@]}"
  fi
  # Re-source so values written by the prompter become visible to the rest
  # of this script (VPS_HOST may have been set just now).
  [[ -f "$SCRIPT_DIR/.deploy.env" ]] && source "$SCRIPT_DIR/.deploy.env"
}

if $REVIEW_ONLY; then
  run_configure review
  exit 0
fi
if $CONFIGURE_ONLY; then
  run_configure walk
  exit 0
fi

# Implicit walk on every deploy: silent when there's nothing new, otherwise
# prompts only for the newly-added knobs since the last run.
run_configure walk

# Re-apply post-source overrides for the knobs deploy.sh itself reads — the
# operator may have just set VPS_HOST / GITHUB_TOKEN / DEPLOY_DIR through
# the prompter.
VPS_HOST="${VPS_HOST:-}"
VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
KEY_FILE="${KEY_FILE:-$HOME/.ssh/id_ed25519}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/media-server}"
SERVICE="${SERVICE:-media-server}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
REPO_URL="${REPO_URL:-github.com/bradselph/Media-Server-Pro.git}"
MASTER_URL="${MASTER_URL:-}"

# Toolchain knobs: operator-provided value wins over auto-detect.
GO_VERSION="${MSP_GO_VERSION:-$GO_VERSION}"
NODE_MAJOR="${MSP_NODE_MAJOR:-$NODE_MAJOR}"

# ── Validation ───────────────────────────────────────────────────────────────
[[ -z "$VPS_HOST" ]]     && die "VPS_HOST is not set. Run ./deploy.sh --configure or edit .deploy.env"
[[ -z "$GITHUB_TOKEN" ]] && die "GITHUB_TOKEN is not set. Run ./deploy.sh --configure or edit .deploy.env"

# ── Interactive branch selection (no flag given) ──────────────────────────────
# If BRANCH wasn't set by a --branch/--dev flag, prompt the user to choose.
# Falls back to the default silently when stdin is not a terminal (CI/pipe).
if [[ "$BRANCH" == "$_BRANCH_DEFAULT" ]] && [[ -t 0 ]]; then
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

# strip_control_chars VAL — echo VAL with terminal escape sequences (ANSI
# CSI/SS3, e.g. the arrow-key bytes \x1b[C a non-readline `read` captures during
# editing) and any other control characters removed. Defends against a polluted
# .deploy.env value (e.g. a URL stored as https://…\x1b[C\x1b[D…) producing a
# broken rclone.conf — rclone rejects it with "invalid control character".
strip_control_chars() {
  local esc
  esc=$(printf '\033')
  printf '%s' "$1" | sed -E "s/${esc}(\[|O)[0-9;?]*[A-Za-z~]//g" | tr -d '[:cntrl:]'
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

# ══════════════════════════════════════════════════════════════════════════════
#
#   SLAVE MODE
#
# ══════════════════════════════════════════════════════════════════════════════


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
if $DOCKER_MODE; then
  info "Backend    : docker compose (GHCR image: ghcr.io/bradselph/media-server-pro:${IMAGE_TAG:-$BRANCH})"
else
  info "Backend    : native (Go build + systemd)"
fi
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
    if ! grep -q '^DOWNLOADER_INTERNAL_TOKEN=.\+' \"\$ENV\" 2>/dev/null; then
      DL_TOKEN=\$(openssl rand -hex 32 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null | tr -d '-' || date +%s | sha256sum | head -c 64)
      if grep -q '^DOWNLOADER_INTERNAL_TOKEN=' \"\$ENV\" 2>/dev/null; then
        sed -i \"s|^DOWNLOADER_INTERNAL_TOKEN=.*|DOWNLOADER_INTERNAL_TOKEN=\$DL_TOKEN|\" \"\$ENV\"
      else
        echo \"DOWNLOADER_INTERNAL_TOKEN=\$DL_TOKEN\" >> \"\$ENV\"
      fi
      echo \"  [IMPORTANT] Set this same value on the downloader as MSP_INTERNAL_TOKEN:\"
      echo \"    DOWNLOADER_INTERNAL_TOKEN=\$DL_TOKEN\"
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
      success "Hand the API key to a peer's admin → Sources → Pair from peer (or POST /api/admin/peer/connect)."
    fi
  fi
  exit 0
fi

# ── HiDrive WebDAV mount setup ───────────────────────────────────────────────
# Mounts an IONOS HiDrive WebDAV share read-only via rclone + a systemd unit,
# straight into the video library at $VIDEOS_DIR/<HIDRIVE_LIBRARY_SUBDIR> so the
# scanner indexes it as a subfolder. rclone is used instead of davfs2 because it
# does true HTTP Range reads (--vfs-cache-mode off) rather than downloading whole
# files to a local cache — that's what makes streaming/seeking off WebDAV viable.
if $SETUP_HIDRIVE; then
  # Sanitize every value — a .deploy.env written before the input-stripping fix
  # may carry arrow-key escape bytes that would corrupt rclone.conf/the URL.
  HIDRIVE_URL="$(strip_control_chars "${HIDRIVE_WEBDAV_URL:-https://webdav.hidrive.ionos.com/}")"
  HIDRIVE_REMOTE="$(strip_control_chars "${HIDRIVE_REMOTE_PATH:-}")"
  HIDRIVE_SUBDIR="$(strip_control_chars "${HIDRIVE_LIBRARY_SUBDIR:-hidrive}")"
  HIDRIVE_USER="$(strip_control_chars "${HIDRIVE_USER:-}")"

  # Mount mode: read-only (default — a pull-only streaming source, no local cache)
  # vs read-write so the downloader can store imported media on HiDrive. Writes are
  # staged in rclone's vfs cache then uploaded, so --vfs-cache-mode writes is
  # required; "off" only supports read-only streaming.
  if [[ "${HIDRIVE_READONLY:-true}" == "false" ]]; then
    HIDRIVE_MOUNT_FLAGS="--vfs-cache-mode writes"
    HIDRIVE_MODE_LABEL="read-write (downloader can store here)"
  else
    HIDRIVE_MOUNT_FLAGS="--read-only --vfs-cache-mode off"
    HIDRIVE_MODE_LABEL="read-only (streaming source)"
  fi

  if [[ "${HIDRIVE_ENABLED:-false}" != "true" ]]; then
    # Teardown path — HIDRIVE_ENABLED is off, so make the flag reversible:
    # unmount and remove the unit instead of mounting.
    info "HIDRIVE_ENABLED is not 'true' — tearing down any existing HiDrive mount."
    run_or_dry remote "
      set -euo pipefail
      if [ -f /etc/systemd/system/hidrive-media.service ]; then
        sudo systemctl disable --now hidrive-media.service 2>/dev/null || true
        sudo rm -f /etc/systemd/system/hidrive-media.service
        sudo systemctl daemon-reload
        echo '[hidrive] Unmounted and removed hidrive-media.service'
      else
        echo '[hidrive] No hidrive-media.service installed — nothing to tear down'
      fi
    "
    exit 0
  fi

  [[ -n "${HIDRIVE_USER:-}" ]] || die "HIDRIVE_USER is empty. Run ./deploy.sh --configure (HiDrive mount section)."
  [[ -n "${HIDRIVE_PASS:-}" ]] || die "HIDRIVE_PASS is empty. Run ./deploy.sh --configure (HiDrive mount section)."

  info "Setting up HiDrive WebDAV mount on the VPS (rclone + systemd)..."
  info "  Endpoint : $HIDRIVE_URL"
  info "  Remote   : ${HIDRIVE_REMOTE:-/ (account root)}"
  info "  Graft    : \$VIDEOS_DIR/$HIDRIVE_SUBDIR"
  info "  Mode     : $HIDRIVE_MODE_LABEL"

  # Ship the password as a temp file over scp (never on the command line, where
  # it would land in the remote shell's argv and journald). The remote side
  # obscures it with `rclone obscure` and shreds the temp file immediately.
  if ! $DRY_RUN; then
    HIDRIVE_PASS_FILE="$(mktemp)"
    printf '%s' "$(strip_control_chars "$HIDRIVE_PASS")" > "$HIDRIVE_PASS_FILE"
    scp "${SCP_OPTS[@]}" "$HIDRIVE_PASS_FILE" "$REMOTE_USER@$REMOTE_HOST:/tmp/msp-hidrive-pass" >/dev/null
    rm -f "$HIDRIVE_PASS_FILE"
  fi

  run_or_dry remote "
    set -euo pipefail

    # 1. rclone + FUSE userspace tooling
    if ! command -v rclone &>/dev/null; then
      echo '[hidrive] Installing rclone...'
      curl -fsSL https://rclone.org/install.sh | sudo bash
    else
      echo \"[hidrive] rclone present: \$(rclone version 2>/dev/null | head -1)\"
    fi
    sudo apt-get install -y fuse3 2>/dev/null || sudo apt-get install -y fuse 2>/dev/null || true
    # --allow-other requires user_allow_other so the mediaserver service user
    # can read a FUSE mount established by root.
    if ! grep -q '^user_allow_other' /etc/fuse.conf 2>/dev/null; then
      echo 'user_allow_other' | sudo tee -a /etc/fuse.conf >/dev/null
    fi

    # 2. rclone remote config — obscure the password from the shipped temp file
    if [ ! -f /tmp/msp-hidrive-pass ]; then
      echo '[hidrive] ERROR: password file not received'; exit 1
    fi
    OBSCURED=\$(rclone obscure \"\$(cat /tmp/msp-hidrive-pass)\")
    shred -u /tmp/msp-hidrive-pass 2>/dev/null || rm -f /tmp/msp-hidrive-pass
    sudo mkdir -p /root/.config/rclone
    sudo tee /root/.config/rclone/rclone.conf >/dev/null <<RCLONE_CONF
[hidrive]
type = webdav
url = $HIDRIVE_URL
vendor = other
user = $HIDRIVE_USER
pass = \$OBSCURED
RCLONE_CONF
    sudo chmod 600 /root/.config/rclone/rclone.conf
    echo '[hidrive] Wrote /root/.config/rclone/rclone.conf'

    # 3. Resolve VIDEOS_DIR (the Go default './videos' is relative to \$DEPLOY_DIR)
    #    and the mediaserver uid/gid so mounted files appear owned by the service.
    VID_DIR=\$(grep -oP '(?<=^VIDEOS_DIR=)\S+' '$DEPLOY_DIR/.env' 2>/dev/null || echo '')
    case \"\$VID_DIR\" in
      '')  VID_DIR='$DEPLOY_DIR/videos' ;;
      /*)  ;;                                      # already absolute
      ./*) VID_DIR=\"$DEPLOY_DIR/\${VID_DIR#./}\" ;;  # './videos' → \$DEPLOY_DIR/videos
      *)   VID_DIR=\"$DEPLOY_DIR/\$VID_DIR\" ;;     # bare relative
    esac
    MOUNT_DIR=\"\$VID_DIR/$HIDRIVE_SUBDIR\"
    MS_UID=\$(id -u mediaserver 2>/dev/null || echo 0)
    MS_GID=\$(id -g mediaserver 2>/dev/null || echo 0)
    sudo mkdir -p \"\$MOUNT_DIR\"
    sudo chown \"\$MS_UID:\$MS_GID\" \"\$MOUNT_DIR\" 2>/dev/null || true

    # 4. systemd unit — reboot-persistent, auto-restart. Single-line ExecStart
    #    (no backslash continuations) to stay heredoc-safe. Mount flags come from
    #    HIDRIVE_MOUNT_FLAGS: read-only+cache off for a streaming source, or
    #    --vfs-cache-mode writes when HiDrive is a writable download target.
    sudo tee /etc/systemd/system/hidrive-media.service >/dev/null <<UNIT
[Unit]
Description=rclone WebDAV mount (IONOS HiDrive) for Media Server Pro
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/bin/rclone mount hidrive:$HIDRIVE_REMOTE \"\$MOUNT_DIR\" --config /root/.config/rclone/rclone.conf $HIDRIVE_MOUNT_FLAGS --dir-cache-time 12h --allow-other --uid \$MS_UID --gid \$MS_GID --umask 022 --log-level INFO
ExecStop=/bin/fusermount -uz \"\$MOUNT_DIR\"
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
UNIT
    sudo systemctl daemon-reload
    sudo systemctl enable --now hidrive-media.service

    # 5. Verify the mount actually came up before declaring success
    OK=false
    for i in 1 2 3 4 5; do
      if mountpoint -q \"\$MOUNT_DIR\"; then
        echo \"[hidrive] Mounted OK -> \$MOUNT_DIR\"
        ls -la \"\$MOUNT_DIR\" 2>/dev/null | head -5 || true
        OK=true
        break
      fi
      sleep 2
    done
    if ! \$OK; then
      echo '[hidrive] ERROR: mount did not come up — check: journalctl -u hidrive-media.service -n 40'
      exit 1
    fi
    echo '[hidrive] Done. Trigger a library rescan from the admin UI (or restart $SERVICE) to index the files.'
  "
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

# ── Forward configured knobs to the VPS ──────────────────────────────────────
# Walks FORWARDED_RUNTIME and FORWARDED_BUILD (derived in deploy-knobs.sh
# from KNOB_SCOPE) and ships non-empty values to the VPS as two payload
# files in /tmp:
#   /tmp/msp-runtime.env  — KEY=value lines merged into $DEPLOY_DIR/.env
#                           by deploy-knobs-merge.py (atomic rename).
#   /tmp/msp-build.env    — single-quoted `KEY='value'` lines sourced by
#                           the npm build shell so NUXT_PUBLIC_* knobs
#                           land in the bundle.
# Empty knobs are skipped on purpose — they would clobber a value the
# operator hand-set on the VPS. Values containing newlines are rejected
# (.env files don't survive multi-line values cleanly).

# Single-quote a string for use in a sourced bash file. Embedded single
# quotes become `'\''`, which closes-escapes-reopens the literal.
shell_quote() {
  local v="${1//\'/\'\\\'\'}"
  printf "'%s'" "$v"
}

RUNTIME_PAYLOAD=""
BUILD_PAYLOAD=""
RUNTIME_COUNT=0
BUILD_COUNT=0

if ! $DRY_RUN; then
  RUNTIME_PAYLOAD="$(mktemp)"
  BUILD_PAYLOAD="$(mktemp)"

  for _k in "${FORWARDED_RUNTIME[@]}"; do
    _v="${!_k:-}"
    [[ -z "$_v" ]] && continue
    if [[ "$_v" == *$'\n'* ]]; then
      warn "Skipping $_k — value contains newlines (not supported in .env)"
      continue
    fi
    printf '%s=%s\n' "$_k" "$_v" >> "$RUNTIME_PAYLOAD"
    RUNTIME_COUNT=$((RUNTIME_COUNT + 1))
  done

  for _k in "${FORWARDED_BUILD[@]}"; do
    _v="${!_k:-}"
    [[ -z "$_v" ]] && continue
    if [[ "$_v" == *$'\n'* ]]; then
      warn "Skipping $_k — value contains newlines (not supported in build env)"
      continue
    fi
    printf 'export %s=%s\n' "$_k" "$(shell_quote "$_v")" >> "$BUILD_PAYLOAD"
    BUILD_COUNT=$((BUILD_COUNT + 1))
  done
  unset _k _v

  # Target file depends on backend mode:
  #   native (default) — $DEPLOY_DIR/.env (consumed by the Go binary + systemd)
  #   docker           — $DEPLOY_DIR/.env.docker (consumed by `docker compose
  #                      --env-file .env.docker`). Different filename so the
  #                      two paths don't clobber each other's config and the
  #                      operator can switch back and forth without losing
  #                      hand-set values in the other mode's file.
  if $DOCKER_MODE; then
    RUNTIME_TARGET="$DEPLOY_DIR/.env.docker"
    RUNTIME_LABEL=".env.docker"
  else
    RUNTIME_TARGET="$DEPLOY_DIR/.env"
    RUNTIME_LABEL=".env"
  fi

  if [[ $RUNTIME_COUNT -gt 0 ]]; then
    info "Forwarding $RUNTIME_COUNT runtime knob(s) → $RUNTIME_TARGET"
    scp "${SCP_OPTS[@]}" "$RUNTIME_PAYLOAD" "$REMOTE_USER@$REMOTE_HOST:/tmp/msp-runtime.env" >/dev/null
    remote "
      set -euo pipefail
      ENV='$RUNTIME_TARGET'
      [ -f \"\$ENV\" ] || sudo touch \"\$ENV\"
      sudo python3 '$DEPLOY_DIR/deploy-knobs-merge.py' /tmp/msp-runtime.env \"\$ENV\"
      sudo chmod 600 \"\$ENV\"
      sudo rm -f /tmp/msp-runtime.env
    "
  else
    info "No runtime knobs to forward (everything in .deploy.env is empty)."
  fi

  if [[ $BUILD_COUNT -gt 0 ]]; then
    info "Forwarding $BUILD_COUNT build knob(s) → npm build env"
    scp "${SCP_OPTS[@]}" "$BUILD_PAYLOAD" "$REMOTE_USER@$REMOTE_HOST:/tmp/msp-build.env" >/dev/null
  else
    # Make sure no stale build env from a previous deploy is left behind —
    # the build block sources /tmp/msp-build.env unconditionally.
    remote "rm -f /tmp/msp-build.env" 2>/dev/null || true
  fi

  rm -f "$RUNTIME_PAYLOAD" "$BUILD_PAYLOAD"
fi

# ─────────────────────────────────────────────────────────────────────────────
# From here down, the script branches: native flow (default) builds Go + Nuxt
# on the VPS and runs the binary under systemd; docker flow installs Docker on
# the VPS and runs `docker compose up -d` against the GHCR image. Both end at
# the same /health poll, so the success criteria match.
# ─────────────────────────────────────────────────────────────────────────────

if ! $DOCKER_MODE; then

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

  # Install Node dependencies.
  # 1. Try npm ci against the committed lockfile (fast + deterministic).
  # 2. If the lock is out of sync with package.json, regenerate it with
  #    npm install. This shadows the committed lock for this build only —
  #    a stale committed lock keeps tripping the same path next deploy
  #    until the operator commits a refreshed one.
  # 3. After install, audit the resolved tree. A committed lock can pin
  #    transitively-vulnerable versions that npm ci faithfully reproduces;
  #    when high-severity issues are present, regenerate the lock and
  #    reinstall so the shipped artifact is clean even if the committed
  #    lock is not. Audit failures are non-fatal — they only inform.
  install_ok=1
  if [ -f package-lock.json ]; then
    echo '[deploy] package-lock.json found — trying npm ci'
    if ! npm ci --no-audit --no-fund 2>&1; then
      echo '[deploy] npm ci failed (lock out of sync) — regenerating package-lock.json'
      rm -f package-lock.json
      npm install --no-audit --no-fund || install_ok=0
    fi
  else
    echo '[deploy] No package-lock.json — running npm install'
    npm install --no-audit --no-fund || install_ok=0
  fi
  [ \"\$install_ok\" = 1 ] || { echo '[deploy] npm install failed'; exit 1; }

  if npm audit --omit=dev --audit-level=high --no-color >/dev/null 2>&1; then
    echo '[deploy] npm audit clean (no high/critical vulnerabilities)'
  else
    echo '[deploy] npm audit reports high/critical vulnerabilities — regenerating package-lock.json'
    rm -f package-lock.json
    npm install --no-audit --no-fund
    if npm audit --omit=dev --audit-level=high --no-color >/dev/null 2>&1; then
      echo '[deploy] Vulnerabilities resolved after lockfile regeneration'
    else
      echo '[deploy] WARNING: high/critical vulnerabilities remain after regen — see \`npm audit\`'
    fi
  fi

  # Load FORWARDED_BUILD knobs (NUXT_PUBLIC_*) from /tmp/msp-build.env if the
  # forwarding step shipped one. set -a auto-exports every assignment so
  # Nuxt's runtimeConfig.public reads them from process.env at build time.
  if [ -f /tmp/msp-build.env ]; then
    echo '[deploy] Sourcing build knobs from /tmp/msp-build.env'
    set -a
    # shellcheck disable=SC1091
    . /tmp/msp-build.env
    set +a
  fi

  # Nuxt generates to ../static/react/ (configured in nuxt.config.ts nitro.output.publicDir)
  npm run build
  rm -f /tmp/msp-build.env
  cd ../..
  echo '[deploy] Nuxt UI build complete'

  # Stop service before replacing binary
  sudo systemctl stop '$SERVICE' 2>/dev/null || true

  # Backup old binary
  [ -f server ] && cp server server.bak && echo '[deploy] Backed up server -> server.bak'

  # Build Go binary (server)
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

    if [ -f '$DEPLOY_DIR/server.bak' ]; then
      echo '[deploy] Rolling back...'
      cd '$DEPLOY_DIR'
      mv server.bak server
      chmod +x server
      sudo systemctl start '$SERVICE' && echo '[deploy] Rollback succeeded' || echo '[deploy] Rollback also failed'
    fi
    exit 1
  fi

  PORT=\$(grep -o 'SERVER_PORT=[0-9]*' '$DEPLOY_DIR/.env' 2>/dev/null | cut -d= -f2 || echo 8080)
  HEALTH_URL=\"http://127.0.0.1:\${PORT}/health\"
  echo \"[deploy] Polling \$HEALTH_URL (waiting for media scan to complete)...\"
  OK=false
  for i in \$(seq 1 30); do
    CODE=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \"\$HEALTH_URL\" 2>/dev/null || echo 000)
    if [ \"\$CODE\" = '200' ]; then
      echo \"[deploy] Health check: HTTP 200 -- server ready\"
      OK=true
      break
    elif [ \"\$CODE\" = '503' ]; then
      echo \"[deploy] Health check: HTTP 503 -- still initializing (\${i}/30)\"
    fi
    sleep 3
  done
  \$OK || echo '[deploy] WARNING: health endpoint did not reach 200 -- check logs: journalctl -u $SERVICE -n 50'
"

else  # DOCKER_MODE branch

# ── Docker mode: install Docker, stop systemd unit, compose up ───────────────
info "Checking Docker on VPS..."
run_or_dry remote "
  set -euo pipefail
  MISSING=()
  for pkg in curl ca-certificates; do
    dpkg -s \"\$pkg\" &>/dev/null || MISSING+=(\"\$pkg\")
  done
  if [ \${#MISSING[@]} -gt 0 ]; then
    sudo apt-get update -qq
    sudo apt-get install -y \"\${MISSING[@]}\"
  fi

  if ! command -v docker &>/dev/null; then
    echo '[docker] Installing Docker via the official convenience script...'
    curl -fsSL https://get.docker.com | sudo sh
  fi

  if ! docker compose version &>/dev/null 2>&1; then
    echo '[docker] ERROR: docker compose plugin not available even after install.'
    exit 1
  fi
  echo \"[docker] \$(docker --version)\"
  echo \"[docker] \$(docker compose version 2>&1 | head -1)\"
"

info "Stopping any native systemd unit (port would conflict otherwise)..."
run_or_dry remote "
  if systemctl is-active '$SERVICE' &>/dev/null; then
    sudo systemctl stop '$SERVICE' || true
    sudo systemctl disable '$SERVICE' || true
    echo '[docker] Stopped + disabled $SERVICE so compose owns the port.'
  else
    echo '[docker] No active $SERVICE unit -- nothing to stop.'
  fi
"

DOCKER_IMAGE_TAG="${IMAGE_TAG:-$BRANCH}"
info "Pulling ghcr.io/bradselph/media-server-pro:$DOCKER_IMAGE_TAG..."
run_or_dry remote "
  set -euo pipefail
  cd '$DEPLOY_DIR'

  if [ ! -f docker-compose.yml ]; then
    echo '[docker] ERROR: docker-compose.yml not found in $DEPLOY_DIR.'
    echo '         Branch $BRANCH may pre-date the Docker setup -- switch with --branch main.'
    exit 1
  fi
  if [ ! -f .env.docker ]; then
    echo '[docker] WARNING: .env.docker is empty -- compose will fail on the DB_ROOT_PASSWORD :? guard.'
    echo '         Run: ./deploy.sh --docker --review  to walk every knob, or edit .env.docker on the VPS.'
  fi

  export IMAGE_TAG='$DOCKER_IMAGE_TAG'

  if ! sudo IMAGE_TAG='$DOCKER_IMAGE_TAG' docker compose --env-file .env.docker pull server 2>/dev/null; then
    echo '[docker] Pull failed -- falling back to local build (a few minutes the first time).'
    sudo IMAGE_TAG='$DOCKER_IMAGE_TAG' docker compose --env-file .env.docker build server
  fi

  echo '[docker] Starting compose stack...'
  sudo IMAGE_TAG='$DOCKER_IMAGE_TAG' docker compose --env-file .env.docker up -d --remove-orphans

  PORT=\$(grep -E '^SERVER_PORT=' .env.docker 2>/dev/null | tail -1 | cut -d= -f2 || echo 3000)
  PORT=\${PORT:-3000}
  HEALTH_URL=\"http://127.0.0.1:\${PORT}/health\"
  echo \"[docker] Polling \$HEALTH_URL (waiting for container + DB to come up)...\"
  OK=false
  for i in \$(seq 1 30); do
    CODE=\$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \"\$HEALTH_URL\" 2>/dev/null || echo 000)
    if [ \"\$CODE\" = '200' ]; then
      echo \"[docker] Health check: HTTP 200 -- server ready\"
      OK=true
      break
    elif [ \"\$CODE\" = '503' ]; then
      echo \"[docker] Health check: HTTP 503 -- still initializing (\${i}/30)\"
    fi
    sleep 3
  done
  \$OK || echo '[docker] WARNING: health endpoint did not reach 200 -- check container logs: docker compose --env-file .env.docker logs server'
"

fi  # end DOCKER_MODE branch

# ── HiDrive mount reminder ────────────────────────────────────────────────────
# Cheap, opt-in: only pings the VPS when the operator has turned HiDrive on.
if ! $DRY_RUN && [[ "${HIDRIVE_ENABLED:-false}" == "true" ]]; then
  if ! remote "systemctl is-active hidrive-media.service" >/dev/null 2>&1; then
    warn "HIDRIVE_ENABLED=true but the HiDrive WebDAV mount is not active on the VPS."
    warn "Run: ./deploy.sh --setup-hidrive   to install and start it."
  fi
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
success "Deploy complete."
if ! $DRY_RUN; then
  if $DOCKER_MODE; then
    info "Status: $(remote "cd '$DEPLOY_DIR' && sudo docker compose --env-file .env.docker ps --format '{{.Name}} {{.Status}}' 2>/dev/null | head -3" || echo unknown)"
    info "Logs:   ssh -p $VPS_PORT $VPS_USER@$VPS_HOST 'cd $DEPLOY_DIR && sudo docker compose --env-file .env.docker logs -f server'"
  else
    info "Status: $(remote "systemctl is-active '$SERVICE' 2>/dev/null || echo unknown")"
    info "Logs:   ssh -p $VPS_PORT $VPS_USER@$VPS_HOST 'journalctl -u $SERVICE -f'"
  fi
fi
echo ""
