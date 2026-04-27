#!/usr/bin/env bash
# ══════════════════════════════════════════════════════════════════════════════
#  Media Server Pro — Fresh VPS Bootstrap
# ══════════════════════════════════════════════════════════════════════════════
#
#  Targets a brand-new Linux VPS (Ubuntu 22.04+/24.04, Debian 11/12, or any
#  RHEL-family distro: AlmaLinux/Rocky/CentOS Stream 9). Brings the box from
#  bare OS to a running Media Server Pro stack:
#
#    1.  Pre-flight checks (OS, arch, RAM, disk, root, network)
#    2.  System update
#    3.  Base packages (curl, git, ufw/firewalld, fail2ban, etc.)
#    4.  Docker engine + Compose v2 plugin
#    5.  Optional non-root deploy user with docker access
#    6.  Optional swap file
#    7.  Firewall + fail2ban
#    8.  Optional SSH hardening
#    9.  Optional Caddy reverse proxy with automatic Let's Encrypt TLS
#   10.  Clone or reuse the repo
#   11.  Generate .env.docker with strong random secrets
#   12.  Build/pull images and bring the stack up
#   13.  Health-check the stack and print a summary
#
#  Designed for a first-time operator. Every step is verbose, every prompt
#  has a sensible default, and a transcript is written to
#  ./logs/vps-bootstrap-YYYYMMDD-HHMMSS.log.
#
#  USAGE
#    sudo ./vps-bootstrap.sh                   # full interactive bootstrap
#    sudo ./vps-bootstrap.sh --help            # show all flags
#    sudo ./vps-bootstrap.sh --yes             # accept defaults, fewer prompts
#    sudo ./vps-bootstrap.sh --resume          # skip steps already completed
#    sudo ./vps-bootstrap.sh --skip-docker     # if Docker is already installed
#    sudo ./vps-bootstrap.sh --skip-firewall   # if you manage firewall yourself
#    sudo ./vps-bootstrap.sh --no-tls          # skip Caddy / reverse proxy
#    sudo ./vps-bootstrap.sh --log /tmp/x.log  # custom log file
#
#  Re-running the script is safe — every step is idempotent.
# ══════════════════════════════════════════════════════════════════════════════

set -o pipefail
# Deliberately no `set -e` — failures are handled per-step so the operator
# can retry, skip, or get a useful diagnostic instead of an empty terminal.

# ──────────────────────────────────────────────────────────────────────────────
#  0. METADATA
# ──────────────────────────────────────────────────────────────────────────────
SCRIPT_VERSION="1.0.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="$SCRIPT_DIR/logs"
LOG_FILE="$LOG_DIR/vps-bootstrap-$TIMESTAMP.log"
STATE_FILE="$SCRIPT_DIR/.vps-bootstrap.state"
ENV_FILE="$SCRIPT_DIR/.env.docker"
ENV_EXAMPLE="$SCRIPT_DIR/.env.docker.example"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"

# Runtime flags
ASSUME_YES=false
RESUME=false
SKIP_DOCKER=false
SKIP_FIREWALL=false
SKIP_TLS=false
SKIP_CLONE=false

REPO_URL_DEFAULT="https://github.com/bradselph/Media-Server-Pro.git"

# Captured during run
OS_ID=""
OS_VERSION_ID=""
OS_FAMILY=""        # debian | rhel
ARCH=""             # amd64 | arm64
PKG=""              # apt-get | dnf | yum
PUBLIC_IP=""
HAS_SYSTEMD=true

# ──────────────────────────────────────────────────────────────────────────────
#  1. LOGGING / OUTPUT
# ──────────────────────────────────────────────────────────────────────────────
if [[ -t 1 ]] && [[ -z "${NO_COLOR:-}" ]]; then
  # ANSI-C quoting ($'...') stores the actual ESC byte, so the codes render
  # correctly in both `printf` format strings AND `cat <<EOF` heredocs.
  C_RESET=$'\033[0m';   C_BOLD=$'\033[1m';   C_DIM=$'\033[2m'
  C_RED=$'\033[0;31m';  C_GREEN=$'\033[0;32m'; C_YELLOW=$'\033[1;33m'
  C_BLUE=$'\033[0;34m'; C_CYAN=$'\033[0;36m';  C_MAGENTA=$'\033[0;35m'
else
  C_RESET=''; C_BOLD=''; C_DIM=''
  C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_CYAN=''; C_MAGENTA=''
fi

_ts() { date '+%Y-%m-%d %H:%M:%S'; }

_log_raw() {
  [[ -z "${LOG_FILE:-}" ]] && return 0
  printf '[%s] %s\n' "$(_ts)" "$*" >> "$LOG_FILE" 2>/dev/null || true
}

info()    { printf "%s[i]%s %s\n" "$C_CYAN"   "$C_RESET" "$*"; _log_raw "INFO  $*"; }
ok()      { printf "%s[\xe2\x9c\x93]%s %s\n" "$C_GREEN" "$C_RESET" "$*"; _log_raw "OK    $*"; }
warn()    { printf "%s[!]%s %s\n" "$C_YELLOW" "$C_RESET" "$*"; _log_raw "WARN  $*"; }
err()     { printf "%s[\xe2\x9c\x97]%s %s\n" "$C_RED"   "$C_RESET" "$*" >&2; _log_raw "ERROR $*"; }
debug()   { [[ "${VERBOSE_DEBUG:-0}" == "1" ]] && printf "%s[d]%s %s\n" "$C_DIM" "$C_RESET" "$*"; _log_raw "DEBUG $*"; }

die() {
  err "$*"
  err "See log: $LOG_FILE"
  exit 1
}

section() {
  printf "\n%s%s======================================================================%s\n" "$C_BOLD" "$C_BLUE" "$C_RESET"
  printf   "%s%s  %s%s\n" "$C_BOLD" "$C_BLUE" "$*" "$C_RESET"
  printf   "%s%s======================================================================%s\n\n" "$C_BOLD" "$C_BLUE" "$C_RESET"
  _log_raw "===== SECTION: $* ====="
}

# Run a command, mirroring stdout/stderr to the log. Returns the command's
# exit code.
run_cmd() {
  local desc="$1"; shift
  debug "RUN: $* ($desc)"
  _log_raw "CMD   $*"
  if "$@" >>"$LOG_FILE" 2>&1; then
    return 0
  fi
  local rc=$?
  err "Command failed (rc=$rc): $desc"
  err "  cmd: $*"
  err "  see log tail above for details: $LOG_FILE"
  return $rc
}

# Run a command but show its output live and also log it.
run_cmd_live() {
  local desc="$1"; shift
  debug "RUN(live): $* ($desc)"
  _log_raw "CMD   $*"
  if "$@" 2>&1 | tee -a "$LOG_FILE"; then
    return 0
  fi
  return ${PIPESTATUS[0]}
}

# ──────────────────────────────────────────────────────────────────────────────
#  2. INPUT HELPERS
# ──────────────────────────────────────────────────────────────────────────────
prompt() {
  local var="$1" text="$2" default="${3:-}"
  local input
  if $ASSUME_YES && [[ -n "$default" ]]; then
    printf -v "$var" '%s' "$default"
    info "  (auto) $text = $default"
    return
  fi
  if [[ -n "$default" ]]; then
    read -rp "  $text [$default]: " input
    printf -v "$var" '%s' "${input:-$default}"
  else
    while true; do
      read -rp "  $text: " input
      if [[ -n "$input" ]]; then
        printf -v "$var" '%s' "$input"; return
      fi
      warn "This field is required."
    done
  fi
  _log_raw "PROMPT $text => ${!var}"
}

prompt_optional() {
  # Same as prompt(), but accepts a blank answer (does NOT loop).
  local var="$1" text="$2" default="${3:-}"
  local input
  if $ASSUME_YES; then
    printf -v "$var" '%s' "$default"
    [[ -n "$default" ]] && info "  (auto) $text = $default"
    return
  fi
  if [[ -n "$default" ]]; then
    read -rp "  $text [$default]: " input
    printf -v "$var" '%s' "${input:-$default}"
  else
    read -rp "  $text: " input
    printf -v "$var" '%s' "$input"
  fi
  _log_raw "PROMPT_OPT $text => ${!var:+(set)}"
}

prompt_secret() {
  local var="$1" text="$2"
  local input
  if $ASSUME_YES; then
    printf -v "$var" '%s' "$(generate_secret)"
    info "  (auto) generated secret for $text"
    return
  fi
  while true; do
    read -rsp "  $text (leave blank to auto-generate): " input; echo
    if [[ -z "$input" ]]; then
      printf -v "$var" '%s' "$(generate_secret)"
      info "    → auto-generated"
      return
    fi
    if [[ ${#input} -lt 12 ]]; then
      warn "Secret must be at least 12 characters."
      continue
    fi
    printf -v "$var" '%s' "$input"
    return
  done
  _log_raw "PROMPT_SECRET $text => (hidden)"
}

prompt_yn() {
  local var="$1" text="$2" default="$3"
  local display input
  [[ "${default,,}" == "y" ]] && display="Y/n" || display="y/N"
  if $ASSUME_YES; then
    case "${default,,}" in y|yes) printf -v "$var" '%s' "true" ;; *) printf -v "$var" '%s' "false" ;; esac
    info "  (auto) $text = ${!var}"
    return
  fi
  read -rp "  $text [$display]: " input
  input="${input:-$default}"
  case "${input,,}" in
    y|yes) printf -v "$var" '%s' "true" ;;
    *)     printf -v "$var" '%s' "false" ;;
  esac
  _log_raw "PROMPT_YN $text => ${!var}"
}

confirm_or_exit() {
  local text="$1"
  $ASSUME_YES && return 0
  local answer
  read -rp "  $text [y/N]: " answer
  case "${answer,,}" in
    y|yes) return 0 ;;
    *) info "Aborted by user."; exit 0 ;;
  esac
}

generate_secret() {
  # 32-char URL-safe secret. Falls back through several entropy sources.
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 | tr -d '\n=+/' | cut -c1-32
  elif [[ -r /dev/urandom ]]; then
    LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32
  else
    date +%s%N | sha256sum | head -c 32
  fi
}

# ──────────────────────────────────────────────────────────────────────────────
#  3. STATE TRACKING (for --resume)
# ──────────────────────────────────────────────────────────────────────────────
mark_done() {
  local step="$1"
  touch "$STATE_FILE"
  grep -qxF "$step" "$STATE_FILE" 2>/dev/null || echo "$step" >> "$STATE_FILE"
  _log_raw "STEP_DONE $step"
}

is_done() {
  $RESUME || return 1
  [[ -f "$STATE_FILE" ]] || return 1
  grep -qxF "$1" "$STATE_FILE" 2>/dev/null
}

skip_if_done() {
  if is_done "$1"; then
    info "Step '$1' already complete — skipping (--resume)."
    return 0
  fi
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
#  4. ARG PARSING
# ──────────────────────────────────────────────────────────────────────────────
print_help() {
  sed -n '/^# ═*$/,/^# ═*$/p' "$0" | head -60
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)        print_help ;;
    -y|--yes)         ASSUME_YES=true; shift ;;
    --resume)         RESUME=true; shift ;;
    --skip-docker)    SKIP_DOCKER=true; shift ;;
    --skip-firewall)  SKIP_FIREWALL=true; shift ;;
    --no-tls)         SKIP_TLS=true; shift ;;
    --skip-clone)     SKIP_CLONE=true; shift ;;
    --log)            LOG_FILE="$2"; shift 2 ;;
    --debug)          VERBOSE_DEBUG=1; shift ;;
    *) err "Unknown flag: $1"; print_help ;;
  esac
done

mkdir -p "$LOG_DIR" 2>/dev/null || true
: > "$LOG_FILE" 2>/dev/null || true

# ──────────────────────────────────────────────────────────────────────────────
#  5. BANNER
# ──────────────────────────────────────────────────────────────────────────────
clear 2>/dev/null || true
cat <<EOF
${C_BOLD}${C_MAGENTA}
╔══════════════════════════════════════════════════════════════════════╗
║          Media Server Pro — Fresh VPS Bootstrap  v$SCRIPT_VERSION              ║
║                                                                      ║
║  This wizard will provision your VPS from scratch:                  ║
║    • System packages and Docker                                      ║
║    • Firewall, fail2ban, optional swap                               ║
║    • Optional non-root deploy user                                   ║
║    • Optional Caddy reverse proxy with automatic HTTPS               ║
║    • Repo clone, secret generation, and stack startup                ║
║                                                                      ║
║  Re-running is safe; pass --resume to skip completed steps.          ║
║  Full transcript: $LOG_FILE  ║
╚══════════════════════════════════════════════════════════════════════╝${C_RESET}

EOF

# ──────────────────────────────────────────────────────────────────────────────
#  6. PRE-FLIGHT
# ──────────────────────────────────────────────────────────────────────────────
section "Pre-flight checks"

# 6a. root
if [[ $EUID -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    err "This script must be run as root."
    info "Re-run with: ${C_BOLD}sudo $0 $*${C_RESET}"
  else
    err "This script must be run as root and 'sudo' is not installed."
  fi
  exit 1
fi
ok "Running as root."

# 6b. OS detection
if [[ ! -r /etc/os-release ]]; then
  die "/etc/os-release missing — cannot identify the distribution."
fi
# shellcheck disable=SC1091
. /etc/os-release
OS_ID="${ID:-unknown}"
OS_VERSION_ID="${VERSION_ID:-unknown}"

case "$OS_ID" in
  ubuntu|debian|raspbian)         OS_FAMILY="debian"; PKG="apt-get" ;;
  rhel|centos|almalinux|rocky|fedora|amzn)  OS_FAMILY="rhel"; PKG=$(command -v dnf >/dev/null 2>&1 && echo dnf || echo yum) ;;
  *)
    warn "Distro '$OS_ID' is not officially supported."
    warn "Tested: Ubuntu 22.04+, Debian 11/12, AlmaLinux/Rocky 9."
    confirm_or_exit "Continue anyway?"
    OS_FAMILY="debian"; PKG="apt-get"
    ;;
esac
ok "Detected: ${PRETTY_NAME:-$OS_ID $OS_VERSION_ID}  (family=$OS_FAMILY, pm=$PKG)"

# 6c. arch
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "Unsupported architecture: $(uname -m). Need amd64 or arm64." ;;
esac
ok "Architecture: $ARCH"

# 6d. systemd?
if ! command -v systemctl >/dev/null 2>&1; then
  HAS_SYSTEMD=false
  warn "systemd not detected. Service management steps will be skipped."
fi

# 6e. RAM (warn if < 2 GB)
MEM_MB=$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)
if [[ $MEM_MB -lt 1500 ]]; then
  warn "Only ${MEM_MB} MiB RAM detected. Recommended: 2 GiB+. We will offer to add a swap file."
else
  ok "Memory: ${MEM_MB} MiB"
fi

# 6f. disk free in /
DISK_FREE=$(df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9')
if [[ -z "$DISK_FREE" ]]; then DISK_FREE=0; fi
if [[ $DISK_FREE -lt 10 ]]; then
  warn "Only ${DISK_FREE} GiB free on /. Recommended: 20 GiB+ (media library not included)."
else
  ok "Disk free on /: ${DISK_FREE} GiB"
fi

# 6g. network reachability
if command -v curl >/dev/null 2>&1; then
  if curl -fsS --max-time 5 https://download.docker.com/ >/dev/null 2>&1; then
    ok "Outbound HTTPS to download.docker.com works."
  else
    warn "Could not reach download.docker.com — Docker install may fail."
  fi
fi

# 6h. public IP (best-effort, never fatal)
for resolver in https://api.ipify.org https://ifconfig.me https://icanhazip.com; do
  if command -v curl >/dev/null 2>&1; then
    PUBLIC_IP=$(curl -fsS --max-time 5 "$resolver" 2>/dev/null || true)
    [[ -n "$PUBLIC_IP" ]] && break
  fi
done
[[ -n "$PUBLIC_IP" ]] && ok "Detected public IP: $PUBLIC_IP" || warn "Could not auto-detect public IP."

mark_done preflight

# 6i. self-update — if this script lives inside a git checkout that has
# newer commits on origin, pull them and re-exec. Saves operators from
# running stale copies after we ship a fix (the recurring foot-gun).
if [[ -z "${VPS_BOOTSTRAP_SELF_UPDATED:-}" ]] \
   && [[ -d "$SCRIPT_DIR/.git" ]] \
   && command -v git >/dev/null 2>&1; then
  # Git's safe.directory check refuses to operate on a repo owned by a
  # different user (we're root, but the repo is owned by the deploy user
  # after step 11). Whitelist it for root globally — idempotent.
  git config --global --get-all safe.directory 2>/dev/null \
    | grep -qxF "$SCRIPT_DIR" \
    || git config --global --add safe.directory "$SCRIPT_DIR" >/dev/null 2>&1
  if (cd "$SCRIPT_DIR" && git fetch --quiet origin 2>/dev/null); then
    LOCAL_HEAD=$(cd "$SCRIPT_DIR" && git rev-parse HEAD 2>/dev/null || echo "")
    REMOTE_HEAD=$(cd "$SCRIPT_DIR" && git rev-parse '@{u}' 2>/dev/null || echo "")
    if [[ -n "$LOCAL_HEAD" && -n "$REMOTE_HEAD" && "$LOCAL_HEAD" != "$REMOTE_HEAD" ]]; then
      info "A newer version of this script exists on origin (${LOCAL_HEAD:0:7} → ${REMOTE_HEAD:0:7})."
      prompt_yn DO_PULL "Pull the latest and re-exec the bootstrap?" "y"
      if [[ "$DO_PULL" == "true" ]]; then
        (cd "$SCRIPT_DIR" && git pull --ff-only) && {
          ok "Updated. Re-executing $0 with the same arguments…"
          export VPS_BOOTSTRAP_SELF_UPDATED=1
          exec "$0" "$@"
        }
      fi
    fi
  fi
fi

# ──────────────────────────────────────────────────────────────────────────────
#  7. UPFRONT QUESTIONS  (collect everything before doing destructive work)
# ──────────────────────────────────────────────────────────────────────────────
section "Configuration"

cat <<EOF
We'll ask a few questions now so the rest of the script can run unattended.
You can press ENTER to accept any default shown in [brackets].

EOF

# 7a. domain / hostname
prompt MS_DOMAIN "Public domain or hostname (blank = use IP only)" "${PUBLIC_IP:-}"
if [[ -z "$MS_DOMAIN" ]]; then
  warn "No domain provided — TLS via Let's Encrypt will not be possible."
  SKIP_TLS=true
elif [[ "$MS_DOMAIN" == "$PUBLIC_IP" ]]; then
  warn "Domain equals the server IP — Let's Encrypt cannot issue certs for raw IPs."
  SKIP_TLS=true
fi

# 7b. ports — single port. With Caddy in front, the container only needs
# loopback access; without Caddy, the same port is exposed publicly.
prompt MS_PORT "Server HTTP port" "8080"
MS_PUBLIC_PORT="$MS_PORT"

# 7c. deploy user
prompt_yn CREATE_USER "Create a dedicated non-root deploy user?" "y"
if [[ "$CREATE_USER" == "true" ]]; then
  prompt DEPLOY_USER "Deploy username" "deploy"
fi

# 7d. swap
NEED_SWAP=false
if [[ $MEM_MB -lt 2048 ]]; then NEED_SWAP=true; fi
prompt_yn ADD_SWAP "Add a 2 GiB swap file? (recommended on small VPS)" "$( $NEED_SWAP && echo y || echo n )"

# 7e. firewall
if ! $SKIP_FIREWALL; then
  prompt_yn ENABLE_FW "Configure firewall (ufw/firewalld) to allow only SSH + Media Server Pro?" "y"
  if [[ "$ENABLE_FW" != "true" ]]; then SKIP_FIREWALL=true; fi
fi

# 7f. ssh hardening
prompt_yn HARDEN_SSH "Harden SSHD (disable root password login, require keys)?" "n"
if [[ "$HARDEN_SSH" == "true" ]]; then
  warn "Make sure you have an SSH key installed on this account before continuing!"
  cat <<EOF
  Sanity check: the following authorized_keys will keep working after hardening:
EOF
  if [[ -r /root/.ssh/authorized_keys ]]; then
    head -n 3 /root/.ssh/authorized_keys 2>/dev/null | sed 's/^/    /'
  else
    warn "  /root/.ssh/authorized_keys is empty or missing!"
    confirm_or_exit "Continue with SSH hardening anyway? You may lose access."
  fi
fi

# 7g. caddy reverse proxy
# Two modes:
#   • Real domain   — Caddy on :443 with automatic Let's Encrypt TLS
#   • IP-only       — Caddy on :80, plain HTTP, no cert (still useful: drops
#                     :8080 from the URL, gives access logs + gzip + a clean
#                     place to add an IP allowlist later)
if $SKIP_TLS; then
  prompt_yn INSTALL_CADDY "Install Caddy as an HTTP reverse proxy on port 80? (no TLS — IP-only mode)" "y"
  CADDY_MODE="http"
else
  prompt_yn INSTALL_CADDY "Install Caddy as a reverse proxy with automatic HTTPS for $MS_DOMAIN?" "y"
  if [[ "$INSTALL_CADDY" == "true" ]]; then
    prompt CADDY_EMAIL "Email for Let's Encrypt notifications" "admin@${MS_DOMAIN}"
  fi
  CADDY_MODE="https"
fi

# 7h. repo clone
USE_EXISTING_REPO=false
if [[ -f "$COMPOSE_FILE" ]]; then
  USE_EXISTING_REPO=true
  ok "docker-compose.yml found here — will use this directory as the project root."
  SKIP_CLONE=true
fi
if ! $SKIP_CLONE; then
  prompt MS_REPO_URL    "Git URL to clone" "$REPO_URL_DEFAULT"
  prompt MS_REPO_DIR    "Directory to clone into" "/opt/media-server-pro"
  prompt MS_REPO_BRANCH "Branch to check out" "main"
  prompt_optional MS_REPO_TOKEN_TEXT "GitHub token (only if the repo is private; press ENTER for public repos)" ""
fi

# 7i. secrets
section "Secrets — leave blank to auto-generate strong values"
prompt_secret DB_ROOT_PW    "MariaDB root password"
prompt_secret DB_APP_PW     "MariaDB application user password"
prompt        DB_NAME       "Database name" "mediaserver"
prompt        DB_USER       "Database username" "mediaserver"
prompt        ADMIN_USER    "Admin username (for the web UI)" "admin"
prompt_secret ADMIN_PW      "Admin password (for the web UI)"

# 7j. optional minio
prompt_yn USE_MINIO "Enable bundled MinIO (S3) storage profile?" "n"
if [[ "$USE_MINIO" == "true" ]]; then
  prompt        MINIO_USER  "MinIO root user"      "mediaserver"
  prompt_secret MINIO_PW    "MinIO root password"
fi

# 7k. public-facing policy
section "Public-deployment policy"
cat <<EOF
These choices shape how the server treats anonymous traffic from the
open internet. Defaults are picked for a typical public deployment.

EOF
# Closed-by-default: a public site that lets strangers self-register often
# turns into spam/abuse fast. Operators who want open signup can flip this.
prompt_yn ALLOW_REGISTRATION_YN "Allow anonymous visitors to self-register accounts?" "n"
prompt DEFAULT_USER_TYPE_INPUT \
  "Default user type assigned at registration (standard|premium)" "standard"
case "${DEFAULT_USER_TYPE_INPUT,,}" in
  standard|premium) DEFAULT_USER_TYPE_INPUT="${DEFAULT_USER_TYPE_INPUT,,}" ;;
  *) warn "Unknown user type '$DEFAULT_USER_TYPE_INPUT' — falling back to 'standard'."
     DEFAULT_USER_TYPE_INPUT="standard" ;;
esac

# 7l. optional integrations (API keys for the Claude assistant + HF mature
#     classifier). Both are off by default — provide a key to enable.
section "Optional integrations (press ENTER to skip any)"
cat <<EOF
These are *optional* AI features. Leave the prompts blank to disable them.
You can always paste a key into .env.docker later and 'docker compose
restart server' to turn them on.

EOF
prompt_optional HF_API_KEY \
  "Hugging Face API key (visual mature-content classifier — blank = disabled)" ""
prompt_optional CLAUDE_API_KEY_INPUT \
  "Anthropic Claude API key (Claude assistant module — blank = disabled)" ""
if [[ -n "$CLAUDE_API_KEY_INPUT" ]]; then
  prompt CLAUDE_MODEL_INPUT \
    "Claude model (claude-opus-4-7 | claude-sonnet-4-6 | claude-haiku-4-5)" \
    "claude-sonnet-4-6"
  prompt CLAUDE_MODE_INPUT \
    "Claude execution mode (advisory = read-only suggestions; interactive = approve each write; autonomous = full automation)" \
    "advisory"
  case "${CLAUDE_MODE_INPUT,,}" in
    advisory|interactive|autonomous) CLAUDE_MODE_INPUT="${CLAUDE_MODE_INPUT,,}" ;;
    *) warn "Unknown Claude mode '$CLAUDE_MODE_INPUT' — using 'advisory'."
       CLAUDE_MODE_INPUT="advisory" ;;
  esac
fi

echo
info "Configuration captured. Ready to begin."
confirm_or_exit "Start the bootstrap now?"

# ──────────────────────────────────────────────────────────────────────────────
#  8. SYSTEM UPDATE
# ──────────────────────────────────────────────────────────────────────────────
section "System update"
if skip_if_done sysupdate; then :; else
  if [[ "$OS_FAMILY" == "debian" ]]; then
    export DEBIAN_FRONTEND=noninteractive
    run_cmd_live "apt-get update" $PKG update -y \
      || die "apt-get update failed."
    run_cmd_live "apt-get upgrade" $PKG -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" upgrade -y \
      || warn "apt-get upgrade had errors — see log."
  else
    run_cmd_live "$PKG upgrade" $PKG -y upgrade \
      || warn "$PKG upgrade had errors — see log."
  fi
  ok "System packages updated."
  mark_done sysupdate
fi

# ──────────────────────────────────────────────────────────────────────────────
#  9. BASE PACKAGES
# ──────────────────────────────────────────────────────────────────────────────
section "Install base packages"
if skip_if_done basepkgs; then :; else
  if [[ "$OS_FAMILY" == "debian" ]]; then
    BASE_PKGS=(
      ca-certificates curl wget gnupg lsb-release
      git unzip tar jq htop
      ufw fail2ban
      apt-transport-https software-properties-common
      openssl tzdata
    )
    run_cmd_live "install base pkgs" $PKG install -y "${BASE_PKGS[@]}" \
      || die "Failed to install base packages."
  else
    BASE_PKGS=(
      ca-certificates curl wget gnupg
      git unzip tar jq htop
      firewalld fail2ban
      openssl tzdata
      epel-release
    )
    # epel-release is harmless on Fedora/Amazon Linux but errors on RHEL clones
    # without the right channel — install it separately and ignore failure.
    $PKG install -y epel-release >>"$LOG_FILE" 2>&1 || true
    run_cmd_live "install base pkgs" $PKG install -y "${BASE_PKGS[@]}" \
      || warn "Some base packages failed — review log."
  fi
  ok "Base packages installed."
  mark_done basepkgs
fi

# ──────────────────────────────────────────────────────────────────────────────
# 10. DOCKER
# ──────────────────────────────────────────────────────────────────────────────
install_docker_debian() {
  install -m 0755 -d /etc/apt/keyrings
  if [[ ! -s /etc/apt/keyrings/docker.gpg ]]; then
    curl -fsSL "https://download.docker.com/linux/$OS_ID/gpg" \
      | gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
      || die "Failed to fetch Docker GPG key."
    chmod a+r /etc/apt/keyrings/docker.gpg
  fi
  local codename
  codename="$(lsb_release -cs 2>/dev/null || echo "$VERSION_CODENAME")"
  echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$OS_ID $codename stable" \
    > /etc/apt/sources.list.d/docker.list
  run_cmd_live "apt-get update (docker)" $PKG update -y \
    || die "apt-get update failed after adding Docker repo."
  run_cmd_live "install docker-ce" $PKG install -y \
    docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin \
    || die "Failed to install Docker."
}

install_docker_rhel() {
  $PKG -y install dnf-plugins-core >>"$LOG_FILE" 2>&1 || true
  $PKG config-manager --add-repo "https://download.docker.com/linux/centos/docker-ce.repo" >>"$LOG_FILE" 2>&1 \
    || die "Failed to add Docker repo."
  run_cmd_live "install docker-ce" $PKG install -y \
    docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin \
    || die "Failed to install Docker."
}

section "Install Docker"
if $SKIP_DOCKER; then
  info "--skip-docker requested. Verifying existing install..."
  command -v docker >/dev/null 2>&1 || die "Docker not found but --skip-docker was requested."
  ok "Docker present: $(docker --version)"
elif skip_if_done docker; then :
elif command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  ok "Docker $(docker --version | awk '{print $3}' | tr -d ',') already installed."
  mark_done docker
else
  if [[ "$OS_FAMILY" == "debian" ]]; then install_docker_debian; else install_docker_rhel; fi
  if $HAS_SYSTEMD; then
    run_cmd "enable docker"  systemctl enable docker || warn "Could not enable docker.service"
    run_cmd "start docker"   systemctl start docker  || die  "Could not start docker.service"
  fi
  if docker run --rm hello-world >>"$LOG_FILE" 2>&1; then
    ok "Docker installed and verified (hello-world ran)."
  else
    warn "Docker is installed but 'hello-world' failed. Continuing — check log."
  fi
  mark_done docker
fi

# ──────────────────────────────────────────────────────────────────────────────
# 11. DEPLOY USER
# ──────────────────────────────────────────────────────────────────────────────
section "Deploy user"
if [[ "${CREATE_USER:-false}" != "true" ]]; then
  info "Skipping (user opted out)."
elif skip_if_done deploy_user; then :
else
  if id "$DEPLOY_USER" >/dev/null 2>&1; then
    ok "User '$DEPLOY_USER' already exists."
  else
    run_cmd "create user" useradd -m -s /bin/bash "$DEPLOY_USER" \
      || die "Could not create user '$DEPLOY_USER'."
    ok "Created user '$DEPLOY_USER'."
  fi
  if getent group docker >/dev/null 2>&1; then
    run_cmd "add to docker group" usermod -aG docker "$DEPLOY_USER" \
      || warn "Could not add '$DEPLOY_USER' to docker group."
    ok "'$DEPLOY_USER' added to docker group."
  fi
  # Mirror root's authorized_keys so the user can log in immediately.
  if [[ -r /root/.ssh/authorized_keys ]]; then
    install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" \
      "/home/$DEPLOY_USER/.ssh"
    install -m 600 -o "$DEPLOY_USER" -g "$DEPLOY_USER" \
      /root/.ssh/authorized_keys "/home/$DEPLOY_USER/.ssh/authorized_keys"
    ok "Copied root's SSH keys to '$DEPLOY_USER'."
  fi
  mark_done deploy_user
fi

# ──────────────────────────────────────────────────────────────────────────────
# 12. SWAP
# ──────────────────────────────────────────────────────────────────────────────
section "Swap file"
if [[ "${ADD_SWAP:-false}" != "true" ]]; then
  info "Skipping (user opted out)."
elif [[ -f /swapfile ]] || swapon --show 2>/dev/null | grep -q .; then
  ok "Swap is already configured."
  mark_done swap
elif skip_if_done swap; then :
else
  run_cmd "fallocate swap" fallocate -l 2G /swapfile \
    || run_cmd "dd swap (slow)" dd if=/dev/zero of=/swapfile bs=1M count=2048 \
    || die "Could not create /swapfile."
  chmod 600 /swapfile
  run_cmd "mkswap"  mkswap  /swapfile || die "mkswap failed."
  run_cmd "swapon"  swapon  /swapfile || die "swapon failed."
  if ! grep -q "^/swapfile" /etc/fstab; then
    echo "/swapfile none swap sw 0 0" >> /etc/fstab
  fi
  ok "2 GiB swap activated and persisted in /etc/fstab."
  mark_done swap
fi

# ──────────────────────────────────────────────────────────────────────────────
# 13. FIREWALL
# ──────────────────────────────────────────────────────────────────────────────
section "Firewall"
if $SKIP_FIREWALL; then
  info "Skipping (user opted out or --skip-firewall)."
elif skip_if_done firewall; then :
else
  if [[ "$OS_FAMILY" == "debian" ]] && command -v ufw >/dev/null 2>&1; then
    run_cmd "ufw allow ssh"  ufw allow OpenSSH || ufw allow 22/tcp
    if [[ "$INSTALL_CADDY" == "true" ]]; then
      run_cmd "ufw allow http"  ufw allow 80/tcp
      run_cmd "ufw allow https" ufw allow 443/tcp
    else
      run_cmd "ufw allow app"   ufw allow "${MS_PORT}/tcp"
    fi
    if ! ufw status 2>/dev/null | grep -q "Status: active"; then
      run_cmd "ufw enable" bash -c "yes | ufw enable" \
        || warn "Could not enable ufw — verify manually."
    fi
    ok "ufw configured."
  elif command -v firewall-cmd >/dev/null 2>&1; then
    $HAS_SYSTEMD && systemctl enable --now firewalld >>"$LOG_FILE" 2>&1
    firewall-cmd --permanent --add-service=ssh >>"$LOG_FILE" 2>&1
    if [[ "$INSTALL_CADDY" == "true" ]]; then
      firewall-cmd --permanent --add-service=http  >>"$LOG_FILE" 2>&1
      firewall-cmd --permanent --add-service=https >>"$LOG_FILE" 2>&1
    else
      firewall-cmd --permanent --add-port="${MS_PORT}/tcp" >>"$LOG_FILE" 2>&1
    fi
    firewall-cmd --reload >>"$LOG_FILE" 2>&1
    ok "firewalld configured."
  else
    warn "No supported firewall tool found (ufw/firewalld). Skipping."
  fi

  # fail2ban — best-effort. Default jail.conf already protects sshd.
  if $HAS_SYSTEMD && command -v fail2ban-client >/dev/null 2>&1; then
    run_cmd "enable fail2ban" systemctl enable --now fail2ban \
      || warn "Could not enable fail2ban."
    ok "fail2ban enabled."
  fi
  mark_done firewall
fi

# ──────────────────────────────────────────────────────────────────────────────
# 14. SSH HARDENING
# ──────────────────────────────────────────────────────────────────────────────
section "SSH hardening"
if [[ "${HARDEN_SSH:-false}" != "true" ]]; then
  info "Skipping (user opted out)."
elif skip_if_done ssh_hardening; then :
else
  SSHD="/etc/ssh/sshd_config"
  if [[ ! -f "$SSHD" ]]; then
    warn "$SSHD not found — skipping."
  else
    cp -a "$SSHD" "$SSHD.bak.$TIMESTAMP"
    sed -ri \
      -e 's/^[#[:space:]]*PasswordAuthentication.*/PasswordAuthentication no/' \
      -e 's/^[#[:space:]]*PermitRootLogin.*/PermitRootLogin prohibit-password/' \
      -e 's/^[#[:space:]]*ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' \
      "$SSHD"
    grep -q '^PasswordAuthentication' "$SSHD" || echo 'PasswordAuthentication no' >> "$SSHD"
    grep -q '^PermitRootLogin'        "$SSHD" || echo 'PermitRootLogin prohibit-password' >> "$SSHD"
    if sshd -t 2>>"$LOG_FILE"; then
      $HAS_SYSTEMD && systemctl reload sshd >>"$LOG_FILE" 2>&1 \
        || systemctl reload ssh  >>"$LOG_FILE" 2>&1 || true
      ok "SSHD hardened. Backup at $SSHD.bak.$TIMESTAMP"
    else
      mv "$SSHD.bak.$TIMESTAMP" "$SSHD"
      warn "sshd config test failed — reverted. See log."
    fi
  fi
  mark_done ssh_hardening
fi

# ──────────────────────────────────────────────────────────────────────────────
# 15. CLONE OR USE REPO
# ──────────────────────────────────────────────────────────────────────────────
section "Repository"
PROJECT_DIR="$SCRIPT_DIR"
if $USE_EXISTING_REPO; then
  ok "Using existing project at $SCRIPT_DIR"
elif skip_if_done clone; then
  PROJECT_DIR="$MS_REPO_DIR"
  ok "Already cloned to $PROJECT_DIR (resume)."
else
  # Build the effective clone URL — embed a token only if the user provided one.
  EFFECTIVE_URL="$MS_REPO_URL"
  if [[ -n "${MS_REPO_TOKEN_TEXT:-}" && "$MS_REPO_URL" =~ ^https://github\.com/ ]]; then
    EFFECTIVE_URL="${MS_REPO_URL/https:\/\/github.com\//https:\/\/x-access-token:${MS_REPO_TOKEN_TEXT}@github.com/}"
  fi

  # Pre-probe the URL anonymously so we fail early with a clear diagnostic
  # instead of letting git open an interactive credential prompt.
  if [[ "$MS_REPO_URL" =~ ^https?:// ]]; then
    PROBE_URL="${MS_REPO_URL%.git}/info/refs?service=git-upload-pack"
    PROBE_CODE=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 10 "$PROBE_URL" 2>/dev/null || echo "000")
    case "$PROBE_CODE" in
      200)
        ok "Repo is reachable anonymously (HTTP $PROBE_CODE)."
        ;;
      401|403|404)
        if [[ -z "${MS_REPO_TOKEN_TEXT:-}" ]]; then
          warn "Anonymous probe of $MS_REPO_URL returned HTTP $PROBE_CODE."
          warn "  • If the repo is PRIVATE, re-run the script and paste a GitHub PAT at the token prompt."
          warn "  • If the URL is wrong, double-check the owner/repo (case-sensitive)."
          warn "  • A 404 from GitHub for a public repo usually means the URL or branch name is wrong."
          confirm_or_exit "Try the clone anyway? (it will likely fail)"
        fi
        ;;
      000)
        warn "Could not reach $MS_REPO_URL at all (network/DNS issue?). Will still try clone."
        ;;
      *)
        warn "Probe returned unexpected HTTP $PROBE_CODE — proceeding cautiously."
        ;;
    esac
  fi

  if [[ -d "$MS_REPO_DIR/.git" ]]; then
    info "Updating existing checkout at $MS_REPO_DIR"
    # Whitelist repo for root (it's chowned to the deploy user later).
    git config --global --get-all safe.directory 2>/dev/null \
      | grep -qxF "$MS_REPO_DIR" \
      || git config --global --add safe.directory "$MS_REPO_DIR" >/dev/null 2>&1
    (
      cd "$MS_REPO_DIR" || exit 1
      GIT_TERMINAL_PROMPT=0 git -c credential.helper= fetch --all --prune \
        && git checkout "$MS_REPO_BRANCH" \
        && GIT_TERMINAL_PROMPT=0 git -c credential.helper= pull --ff-only
    ) >>"$LOG_FILE" 2>&1 || warn "git pull failed — see log."
  else
    install -d -m 755 "$(dirname "$MS_REPO_DIR")"
    # GIT_TERMINAL_PROMPT=0 + empty credential.helper => fail fast, no prompts.
    if ! GIT_TERMINAL_PROMPT=0 git -c credential.helper= clone \
        --branch "$MS_REPO_BRANCH" "$EFFECTIVE_URL" "$MS_REPO_DIR" 2>&1 | tee -a "$LOG_FILE"; then
      err "git clone failed."
      err ""
      err "Common causes:"
      err "  1) Wrong repo URL — check owner/repo spelling and case."
      err "     Default was: $REPO_URL_DEFAULT"
      err "     You entered: $MS_REPO_URL"
      err "  2) Repo is private — re-run and supply a GitHub Personal Access Token."
      err "     Create one at: https://github.com/settings/tokens (scope: repo)"
      err "  3) Wrong branch — '$MS_REPO_BRANCH' may not exist on the remote."
      err "  4) Network/firewall blocking outbound HTTPS."
      err ""
      err "You can re-run with:  sudo $0 --resume"
      die  "Aborting after clone failure."
    fi
  fi
  PROJECT_DIR="$MS_REPO_DIR"
  if [[ "${CREATE_USER:-false}" == "true" ]]; then
    chown -R "$DEPLOY_USER:$DEPLOY_USER" "$MS_REPO_DIR" || true
  fi
  ok "Repo ready at $PROJECT_DIR"
  mark_done clone
fi
cd "$PROJECT_DIR" || die "Cannot cd to $PROJECT_DIR"
ENV_FILE="$PROJECT_DIR/.env.docker"
ENV_EXAMPLE="$PROJECT_DIR/.env.docker.example"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.yml"

# ──────────────────────────────────────────────────────────────────────────────
# 16. .env.docker
# ──────────────────────────────────────────────────────────────────────────────
section "Generate .env.docker"
if [[ -f "$ENV_FILE" ]]; then
  warn ".env.docker already exists at $ENV_FILE"
  prompt_yn OVERWRITE_ENV "Back it up and regenerate?" "n"
  if [[ "$OVERWRITE_ENV" == "true" ]]; then
    cp -a "$ENV_FILE" "$ENV_FILE.bak.$TIMESTAMP"
    info "Backed up to $ENV_FILE.bak.$TIMESTAMP"
  else
    info "Keeping existing .env.docker. Make sure it has the values you want."
    SKIP_ENV_GEN=true
  fi
fi

if [[ "${SKIP_ENV_GEN:-false}" != "true" ]]; then
  HOST_UID=$(id -u "${DEPLOY_USER:-root}" 2>/dev/null || echo 1000)
  HOST_GID=$(id -g "${DEPLOY_USER:-root}" 2>/dev/null || echo 1000)
  TZ_NOW=$(timedatectl show -p Timezone --value 2>/dev/null || echo UTC)
  # SERVER_BIND: bind only to loopback if Caddy is in front, else listen on all.
  if [[ "$INSTALL_CADDY" == "true" ]]; then SERVER_BIND="127.0.0.1"; else SERVER_BIND="0.0.0.0"; fi

  {
    echo "# Generated by vps-bootstrap.sh on $(date -u +%FT%TZ)"
    echo "# DO NOT COMMIT THIS FILE."
    echo
    echo "# ── Docker image source ─────────────────────────────────────"
    echo "# Where to pull pre-built images from. Override IMAGE_OWNER for"
    echo "# forks; pin IMAGE_TAG to a release version (e.g. 1.10.32) to"
    echo "# stop tracking :main."
    echo "IMAGE_REGISTRY=ghcr.io"
    echo "IMAGE_OWNER=bradselph"
    echo "IMAGE_TAG=main"
    echo
    echo "GO_VERSION=1.26"
    echo "NODE_VERSION=22"
    echo "APP_UID=$HOST_UID"
    echo "APP_GID=$HOST_GID"
    echo
    echo "SERVER_PORT=$MS_PORT"
    echo "SERVER_BIND=$SERVER_BIND"
    echo "LOG_LEVEL=info"
    echo "TZ=$TZ_NOW"
    echo
    echo "DB_ROOT_PASSWORD=$DB_ROOT_PW"
    echo "DATABASE_NAME=$DB_NAME"
    echo "DATABASE_USERNAME=$DB_USER"
    echo "DATABASE_PASSWORD=$DB_APP_PW"

    # ────────────────────────────────────────────────────────────────────
    # Defensive defaults for fields whose internal default historically
    # failed validation. Setting them here means the server boots even
    # when running an old image without the upstream defaults fix.
    # ────────────────────────────────────────────────────────────────────
    echo
    echo "# HLS — explicit, satisfies validate.go (>= 1)"
    echo "HLS_PLAYLIST_LENGTH=6"
    echo "HLS_SEGMENT_DURATION=6"

    # ────────────────────────────────────────────────────────────────────
    # Admin credentials. Without ADMIN_PASSWORD (or _HASH), the server
    # logs "admin enabled but no password hash — admin login will fail".
    # We write plaintext for simplicity; user may swap to a bcrypt hash
    # later via ADMIN_PASSWORD_HASH for production.
    # ────────────────────────────────────────────────────────────────────
    echo
    echo "# Admin login (web UI)"
    echo "ADMIN_ENABLED=true"
    echo "ADMIN_USERNAME=$ADMIN_USER"
    echo "ADMIN_PASSWORD=$ADMIN_PW"

    # ────────────────────────────────────────────────────────────────────
    # Sensible feature defaults that mirror what setup.sh writes for
    # native installs. The server picks safe defaults for everything
    # else; these are the ones a fresh deploy actually needs.
    # ────────────────────────────────────────────────────────────────────
    echo
    echo "# Authentication"
    echo "AUTH_SESSION_TIMEOUT_HOURS=24"
    echo "AUTH_ALLOW_GUESTS=false"
    echo "AUTH_ALLOW_REGISTRATION=$( [[ "${ALLOW_REGISTRATION_YN:-false}" == "true" ]] && echo true || echo false )"
    echo "AUTH_DEFAULT_USER_TYPE=${DEFAULT_USER_TYPE_INPUT:-standard}"
    echo "AUTH_SECURE_COOKIES=$( [[ "${CADDY_MODE:-}" == "https" ]] && echo true || echo false )"
    echo
    echo "# HTTP security headers (HSTS only when terminating TLS at Caddy)"
    echo "CSP_ENABLED=true"
    echo "HSTS_ENABLED=$( [[ "${CADDY_MODE:-}" == "https" ]] && echo true || echo false )"
    echo "HSTS_MAX_AGE=15552000"
    echo
    echo "# Streaming / uploads"
    echo "DOWNLOAD_ENABLED=true"
    echo "UPLOADS_ENABLED=true"
    echo "UPLOADS_MAX_FILE_SIZE=10737418240"
    echo
    echo "# Thumbnails"
    echo "THUMBNAILS_ENABLED=true"
    echo "THUMBNAILS_AUTO_GENERATE=true"
    echo
    echo "# Analytics"
    echo "ANALYTICS_ENABLED=true"
    echo "ANALYTICS_RETENTION_DAYS=90"
    echo
    echo "# Rate limit / CORS"
    echo "RATE_LIMIT_ENABLED=true"
    echo "RATE_LIMIT_REQUESTS=100"
    echo "RATE_LIMIT_WINDOW_SECONDS=60"
    echo "CORS_ENABLED=true"
    if [[ "${CADDY_MODE:-}" == "https" && -n "${MS_DOMAIN:-}" ]]; then
      echo "CORS_ORIGINS=https://${MS_DOMAIN}"
    elif [[ "${CADDY_MODE:-}" == "http" && -n "${MS_DOMAIN:-}" ]]; then
      echo "CORS_ORIGINS=http://${MS_DOMAIN}"
    else
      # No Caddy / no domain → fall back to permissive while the operator
      # is still iterating. Tighten this to the eventual public URL ASAP.
      echo "CORS_ORIGINS=*"
    fi

    # ────────────────────────────────────────────────────────────────────
    # Optional AI integrations. The feature flag is the authoritative
    # toggle (FEATURE_*). Keys are only emitted when the operator pasted
    # one — leaving them blank keeps these features disabled.
    # ────────────────────────────────────────────────────────────────────
    echo
    echo "# Hugging Face (visual mature-content classification)"
    if [[ -n "${HF_API_KEY:-}" ]]; then
      echo "FEATURE_HUGGINGFACE=true"
      echo "HUGGINGFACE_API_KEY=$HF_API_KEY"
    else
      echo "FEATURE_HUGGINGFACE=false"
      echo "# HUGGINGFACE_API_KEY="
    fi

    echo
    echo "# Claude assistant module (admin-only)"
    if [[ -n "${CLAUDE_API_KEY_INPUT:-}" ]]; then
      echo "FEATURE_CLAUDE=true"
      # Both names are accepted by the Anthropic SDK; emit both so the
      # bundled `claude` CLI and any direct API caller pick it up.
      echo "ANTHROPIC_API_KEY=$CLAUDE_API_KEY_INPUT"
      echo "CLAUDE_API_KEY=$CLAUDE_API_KEY_INPUT"
      echo "CLAUDE_MODEL=${CLAUDE_MODEL_INPUT:-claude-sonnet-4-6}"
      echo "CLAUDE_MODE=${CLAUDE_MODE_INPUT:-advisory}"
    else
      echo "FEATURE_CLAUDE=false"
      echo "# ANTHROPIC_API_KEY="
      echo "# CLAUDE_API_KEY="
      echo "# CLAUDE_MODEL=claude-sonnet-4-6"
      echo "# CLAUDE_MODE=advisory"
    fi

    # ────────────────────────────────────────────────────────────────────
    # Compose v2 interpolates env vars for EVERY service at parse time,
    # even ones gated behind `profiles:`. So we always emit placeholder
    # values for the receiver + minio services. They're inert until the
    # corresponding profile is activated.
    # ────────────────────────────────────────────────────────────────────
    echo
    echo "# Receiver profile (only used with --profile receiver)"
    echo "MASTER_URL=https://master.example.com"
    echo "RECEIVER_API_KEY=$(generate_secret)"
    echo "SLAVE_ID=receiver-1"
    echo "SLAVE_NAME=Docker Receiver"
    echo "SCAN_INTERVAL=15m"
    echo "HEARTBEAT_INTERVAL=30s"

    echo
    echo "# MinIO profile (only used with --profile minio)"
    echo "MINIO_IMAGE_TAG=RELEASE.2025-09-07T16-13-09Z"
    if [[ "$USE_MINIO" == "true" ]]; then
      echo "MINIO_ROOT_USER=$MINIO_USER"
      echo "MINIO_ROOT_PASSWORD=$MINIO_PW"
    else
      echo "MINIO_ROOT_USER=mediaserver"
      echo "MINIO_ROOT_PASSWORD=$(generate_secret)"
    fi
    echo "MINIO_API_PORT=9000"
    echo "MINIO_CONSOLE_PORT=9001"
  } > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  if [[ "${CREATE_USER:-false}" == "true" ]]; then
    chown "$DEPLOY_USER:$DEPLOY_USER" "$ENV_FILE" || true
  fi
  ok ".env.docker written to $ENV_FILE  (mode 600)"

  # Compose's default env file is `.env` (not `.env.docker`). Symlink so
  # bare `docker compose ps`/`logs`/etc. work without --env-file.
  if [[ ! -e "$PROJECT_DIR/.env" ]] || [[ -L "$PROJECT_DIR/.env" ]]; then
    ln -sf .env.docker "$PROJECT_DIR/.env"
    ok "Symlinked .env → .env.docker so naked 'docker compose' commands work."
  else
    warn "$PROJECT_DIR/.env already exists and is not a symlink — left untouched."
    warn "  Use --env-file .env.docker on every compose call, or merge files manually."
  fi
fi
mark_done env_file

# ──────────────────────────────────────────────────────────────────────────────
# 17. CADDY (optional reverse proxy)
# ──────────────────────────────────────────────────────────────────────────────
install_caddy_debian() {
  # Use the path Caddy's official source list expects so no rewriting needed.
  install -m 0755 -d /usr/share/keyrings
  curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key \
    | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg \
    || return 1
  chmod 0644 /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt \
    -o /etc/apt/sources.list.d/caddy-stable.list || return 1
  $PKG update -y >>"$LOG_FILE" 2>&1 || return 1
  $PKG install -y caddy >>"$LOG_FILE" 2>&1
}

install_caddy_rhel() {
  $PKG install -y 'dnf-command(copr)' >>"$LOG_FILE" 2>&1 || true
  $PKG copr enable -y @caddy/caddy   >>"$LOG_FILE" 2>&1 || true
  $PKG install -y caddy              >>"$LOG_FILE" 2>&1
}

# Helper: when Caddy install/setup fails after we already wrote .env.docker
# with SERVER_BIND=127.0.0.1 and ufw rules for 80/443, we'd end up with the
# server listening only on loopback and nothing reachable. Recover by
# rewriting .env.docker to bind publicly and opening MS_PORT in the firewall.
caddy_fallback_to_direct_exposure() {
  warn "Caddy unavailable — falling back to direct port exposure on :${MS_PORT}."
  INSTALL_CADDY=false
  CADDY_MODE=""
  # 1) Rewrite SERVER_BIND in .env.docker.
  if [[ -f "$ENV_FILE" ]] && grep -q '^SERVER_BIND=' "$ENV_FILE"; then
    sed -i 's/^SERVER_BIND=.*/SERVER_BIND=0.0.0.0/' "$ENV_FILE"
    ok "Rewrote $ENV_FILE → SERVER_BIND=0.0.0.0"
  fi
  # 2) Open the app port in the firewall (it was previously skipped because
  #    Caddy was supposed to front 80/443).
  if command -v ufw >/dev/null 2>&1; then
    run_cmd "ufw allow app fallback" ufw allow "${MS_PORT}/tcp" || true
  elif command -v firewall-cmd >/dev/null 2>&1; then
    firewall-cmd --permanent --add-port="${MS_PORT}/tcp" >>"$LOG_FILE" 2>&1 || true
    firewall-cmd --reload >>"$LOG_FILE" 2>&1 || true
  fi
}

section "Caddy reverse proxy"
if [[ "$INSTALL_CADDY" != "true" ]]; then
  info "Skipping (user opted out or no domain)."
elif skip_if_done caddy; then :
else
  if ! command -v caddy >/dev/null 2>&1; then
    if [[ "$OS_FAMILY" == "debian" ]]; then install_caddy_debian; else install_caddy_rhel; fi
  fi
  if ! command -v caddy >/dev/null 2>&1; then
    warn "Caddy install failed — see log."
    caddy_fallback_to_direct_exposure
    mark_done caddy
  else
    if [[ "${CADDY_MODE:-https}" == "http" ]]; then
      # IP-only / no-domain mode: listen on :80 plain HTTP, no cert.
      cat > /etc/caddy/Caddyfile <<EOF
# Generated by vps-bootstrap.sh on $(date -u +%FT%TZ)
# IP-only HTTP reverse proxy. Replace :80 with your domain to enable HTTPS.

:80 {
    encode zstd gzip
    reverse_proxy 127.0.0.1:$MS_PORT

    # Bigger upload limit for media uploads (default Caddy = 10 MiB).
    request_body {
        max_size 5GB
    }
}
EOF
      CADDY_PUBLIC_URL="http://${MS_DOMAIN:-$PUBLIC_IP}"
    else
      # Domain mode with Let's Encrypt TLS.
      cat > /etc/caddy/Caddyfile <<EOF
# Generated by vps-bootstrap.sh on $(date -u +%FT%TZ)
{
    email $CADDY_EMAIL
}

$MS_DOMAIN {
    encode zstd gzip
    reverse_proxy 127.0.0.1:$MS_PORT

    request_body {
        max_size 5GB
    }
}
EOF
      CADDY_PUBLIC_URL="https://$MS_DOMAIN"
    fi
    if caddy validate --config /etc/caddy/Caddyfile >>"$LOG_FILE" 2>&1; then
      caddy_running=false
      if $HAS_SYSTEMD; then
        systemctl enable --now caddy >>"$LOG_FILE" 2>&1 \
          && systemctl reload caddy  >>"$LOG_FILE" 2>&1 \
          || systemctl restart caddy >>"$LOG_FILE" 2>&1
        if systemctl is-active --quiet caddy; then
          caddy_running=true
        fi
      fi
      if $caddy_running; then
        ok "Caddy configured: $CADDY_PUBLIC_URL → 127.0.0.1:$MS_PORT"
      else
        warn "Caddy systemd unit not active — see: systemctl status caddy"
        caddy_fallback_to_direct_exposure
      fi
    else
      warn "Caddyfile validation failed — review /etc/caddy/Caddyfile."
      caddy_fallback_to_direct_exposure
    fi
  fi
  mark_done caddy
fi

# ──────────────────────────────────────────────────────────────────────────────
# 18. BRING UP THE STACK
# ──────────────────────────────────────────────────────────────────────────────
section "Start the Media Server Pro stack"

COMPOSE_PROFILES_ARGS=()
if [[ "$USE_MINIO" == "true" ]]; then
  COMPOSE_PROFILES_ARGS+=(--profile minio)
fi

cd "$PROJECT_DIR" || die "Cannot cd to $PROJECT_DIR"

# Compose auto-merges docker-compose.override.yml. On a fresh VPS deploy
# we explicitly pass `-f docker-compose.yml` to bypass any merge — but
# also actively rename a stale override left over from a pre-rename
# clone. The committed version was renamed to .yml.example upstream;
# this catches operators who pulled before that rename.
COMPOSE_FILE_ARGS=(-f docker-compose.yml)
if [[ -f "$PROJECT_DIR/docker-compose.override.yml" ]]; then
  STALE="$PROJECT_DIR/docker-compose.override.yml.disabled-by-bootstrap.$TIMESTAMP"
  warn "Renaming stale docker-compose.override.yml -> $(basename "$STALE")"
  warn "  (it's a dev-only file that pins 127.0.0.1 ports and breaks public access)"
  mv "$PROJECT_DIR/docker-compose.override.yml" "$STALE" || true
fi

# Tear down any stale stack from a previous failed attempt so its port
# bindings, networks, and orphan containers don't block the new run.
info "Cleaning up any prior stack state…"
docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" "${COMPOSE_PROFILES_ARGS[@]}" \
  down --remove-orphans >>"$LOG_FILE" 2>&1 || true

# Pre-flight: is something already bound to the port we want?
PORT_HOLDER=""
if command -v ss >/dev/null 2>&1; then
  PORT_HOLDER=$(ss -ltnp "( sport = :${MS_PORT} )" 2>/dev/null | tail -n +2 | head -1)
elif command -v netstat >/dev/null 2>&1; then
  PORT_HOLDER=$(netstat -ltnp 2>/dev/null | awk -v p=":${MS_PORT}" '$4 ~ p {print; exit}')
fi
if [[ -n "$PORT_HOLDER" ]]; then
  err "Port ${MS_PORT} is already in use:"
  err "  $PORT_HOLDER"
  err ""
  err "Likely candidates:"
  err "  • A native (non-docker) media-server-pro from a prior install — try:"
  err "      systemctl stop media-server-pro 2>/dev/null"
  err "      systemctl disable media-server-pro 2>/dev/null"
  err "  • A leftover container — try:"
  err "      docker ps -a"
  err "      docker rm -f \$(docker ps -aq --filter publish=${MS_PORT})"
  err "  • Some other service (nginx/apache) — pick a different MS_PORT and re-run."
  die "Refusing to attempt 'docker compose up' while ${MS_PORT} is held."
fi

# Helper — dump everything an operator needs to debug a stack failure.
dump_stack_diagnostics() {
  local label="$1"
  warn "=== $label diagnostics ==="
  warn "--- container status ---"
  docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
    "${COMPOSE_PROFILES_ARGS[@]}" ps 2>&1 | tee -a "$LOG_FILE" | sed 's/^/    /'
  warn "--- last 100 lines of server logs ---"
  docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
    "${COMPOSE_PROFILES_ARGS[@]}" logs --tail=100 server 2>&1 \
    | tee -a "$LOG_FILE" | sed 's/^/    /'
  warn "--- last 30 lines of db logs ---"
  docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
    "${COMPOSE_PROFILES_ARGS[@]}" logs --tail=30 db 2>&1 \
    | tee -a "$LOG_FILE" | sed 's/^/    /'
}

# Helper — match common server failure patterns and give targeted hints.
suggest_fix_from_logs() {
  local logs
  logs=$(docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
         "${COMPOSE_PROFILES_ARGS[@]}" logs --tail=200 server 2>&1 || true)
  if grep -qiE "permission denied.*\.log" <<<"$logs"; then
    warn "Detected: permission denied writing to /data/logs."
    warn "  Cause: stale named volumes from a pre-permission-fix container."
    warn "  Fix:   docker compose down -v   (DESTROYS volumes — only safe pre-data)"
    warn "         then re-run the bootstrap."
  fi
  if grep -qiE "playlist_length must be at least 1" <<<"$logs"; then
    warn "Detected: HLS playlist_length validation failure."
    warn "  Fix:   the new .env.docker pins HLS_PLAYLIST_LENGTH=6 — run --resume to regenerate."
  fi
  if grep -qiE "configuration validation failed" <<<"$logs"; then
    warn "Detected: configuration validation failure."
    warn "  Fix:   inspect the [ERROR] line above; usually a stale config.json."
    warn "         removing host config.json forces the binary to use defaults."
  fi
  if grep -qiE "address already in use" <<<"$logs"; then
    warn "Detected: server tried to bind a port already held by another process."
  fi
  if grep -qiE "access denied for user|connection refused.*3306" <<<"$logs"; then
    warn "Detected: database auth/connect failure."
    warn "  Fix:   docker compose down -v   (resets MariaDB so the password takes)"
    warn "         then re-run."
  fi
}

# Prefer the published GHCR image. Fall back to a local build only if the
# pull fails (e.g. private fork without auth, no network, or first publish
# hasn't happened yet for a brand-new owner).
#
# The image tag defaults to `main`. Override IMAGE_TAG in $ENV_FILE to pin
# a release (e.g. IMAGE_TAG=1.10.32) or follow a different channel.
IMAGE_TAG="${IMAGE_TAG:-main}"
export IMAGE_TAG
IMAGE="ghcr.io/bradselph/media-server-pro:${IMAGE_TAG}"

PULLED=false
info "Trying to pull pre-built image from GHCR ($IMAGE)…"
if run_cmd_live "docker pull" docker pull "$IMAGE"; then
  PULLED=true
  ok "Pulled $IMAGE"
fi

if ! $PULLED; then
  warn "Could not pull from GHCR — falling back to local build (5-15 min)."
  if ! run_cmd_live "compose build server" docker compose --env-file "$ENV_FILE" \
       "${COMPOSE_FILE_ARGS[@]}" "${COMPOSE_PROFILES_ARGS[@]}" build server; then
    warn "Build failed. Re-run with --resume after fixing the underlying issue."
    die  "Aborting after build failure."
  fi
fi

if ! run_cmd_live "compose up -d" docker compose --env-file "$ENV_FILE" \
     "${COMPOSE_FILE_ARGS[@]}" "${COMPOSE_PROFILES_ARGS[@]}" up -d --no-build; then
  dump_stack_diagnostics "compose up"
  die "docker compose up failed — see diagnostics above and full log: $LOG_FILE"
fi
mark_done compose_up

# ──────────────────────────────────────────────────────────────────────────────
# 19. HEALTH CHECK
# ──────────────────────────────────────────────────────────────────────────────
section "Health check"
HEALTH_URL="http://127.0.0.1:${MS_PORT}/health"
# First boot does DB migrations + builds the module health map, which can
# take several minutes on a slow VPS. Poll for up to 5 minutes (60×5s).
info "Polling $HEALTH_URL — first boot can take several minutes on a fresh DB…"
HEALTHY=false
for i in $(seq 1 60); do
  if curl -fsS --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  # Show a status line every 30s so the operator knows it isn't hung.
  if (( i % 6 == 0 )); then
    printf " [%ds]" $((i*5))
  else
    printf "."
  fi
  sleep 5
done
echo

if $HEALTHY; then
  ok "Server responded healthy on $HEALTH_URL"
else
  warn "Server did not become healthy within 5 minutes."
  # Check for a crash-loop — common when config validation rejects defaults.
  STATE=$(docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
          "${COMPOSE_PROFILES_ARGS[@]}" ps --format '{{.Service}}\t{{.Status}}' 2>/dev/null \
          | awk -F'\t' '$1=="server"{print $2; exit}')
  if [[ -n "$STATE" ]]; then
    info "  server container status: $STATE"
    if grep -qiE "restarting" <<<"$STATE"; then
      warn "  → container is in a CRASH LOOP; collecting logs and matching known patterns…"
    fi
  fi
  dump_stack_diagnostics "health check"
  suggest_fix_from_logs
fi

# ──────────────────────────────────────────────────────────────────────────────
# 20. SUMMARY
# ──────────────────────────────────────────────────────────────────────────────
section "Summary"

if [[ "$INSTALL_CADDY" == "true" && "${CADDY_MODE:-https}" == "https" ]]; then
  PUBLIC_URL="https://$MS_DOMAIN"
elif [[ "$INSTALL_CADDY" == "true" ]]; then
  # Caddy in HTTP-only mode — drops the :8080 from the URL.
  PUBLIC_URL="http://${MS_DOMAIN:-$PUBLIC_IP}"
elif [[ -n "$MS_DOMAIN" && "$MS_DOMAIN" != "$PUBLIC_IP" ]]; then
  PUBLIC_URL="http://$MS_DOMAIN:${MS_PUBLIC_PORT}"
else
  PUBLIC_URL="http://${PUBLIC_IP:-<server-ip>}:${MS_PUBLIC_PORT}"
fi

# Pre-format the optional-feature status lines so the heredoc below stays
# readable (nested ${VAR:+x}${VAR:-y} substitution doesn't do what people
# usually think it does).
if [[ -n "${HF_API_KEY:-}" ]]; then
  HF_STATUS="enabled"
else
  HF_STATUS="disabled (set HUGGINGFACE_API_KEY + FEATURE_HUGGINGFACE=true)"
fi
if [[ -n "${CLAUDE_API_KEY_INPUT:-}" ]]; then
  CLAUDE_STATUS="enabled (mode=${CLAUDE_MODE_INPUT:-advisory}, model=${CLAUDE_MODEL_INPUT:-claude-sonnet-4-6})"
else
  CLAUDE_STATUS="disabled (set ANTHROPIC_API_KEY + FEATURE_CLAUDE=true)"
fi
if [[ "${ALLOW_REGISTRATION_YN:-false}" == "true" ]]; then
  REG_STATUS="OPEN — anyone can sign up"
else
  REG_STATUS="closed (admin-managed accounts only)"
fi
if [[ "${CADDY_MODE:-}" == "https" ]]; then
  HSTS_STATUS="on (TLS terminated at Caddy)"
else
  HSTS_STATUS="off (no TLS)"
fi

cat <<EOF

${C_BOLD}${C_GREEN}Media Server Pro is up.${C_RESET}

  Public URL        : ${C_BOLD}$PUBLIC_URL${C_RESET}
  Internal URL      : http://127.0.0.1:${MS_PORT}
  Project directory : $PROJECT_DIR
  Environment file  : $ENV_FILE  (mode 600 — keep secret!)
  Log file          : $LOG_FILE

${C_BOLD}Useful commands:${C_RESET}
  cd $PROJECT_DIR
  docker compose ps
  docker compose logs -f server
  docker compose restart server
  sudo ./update.sh                # pull :main from GHCR + recreate (default)
  sudo ./update.sh --tag 1.10.32  # pin to a specific release version
  sudo ./update.sh --build        # build locally instead (for source changes)
  sudo ./update.sh --rollback     # revert to the previous image

${C_BOLD}Image source:${C_RESET}
  Default: ghcr.io/bradselph/media-server-pro:main (multi-arch).
  Edit IMAGE_REGISTRY / IMAGE_OWNER / IMAGE_TAG in $ENV_FILE to track
  a fork or pin a version (e.g. IMAGE_TAG=1.10.32 or IMAGE_TAG=edge).

${C_BOLD}Persistence (what survives an update):${C_RESET}
  • All media + state lives in named Docker volumes — they are NOT
    touched by image pulls, rebuilds, container recreations, or
    'docker compose down'. Only 'docker compose down -v' destroys them.
       db-data, videos, music, thumbnails, playlists, uploads,
       analytics, hls-cache, app-data, logs, temp
  • $ENV_FILE lives on the host filesystem — never inside a container.
  • update.sh snapshots the DB to ./backups/ before every upgrade and
    keeps the previous image tagged so --rollback Just Works.

${C_BOLD}Login:${C_RESET}
  Open $PUBLIC_URL/admin-login in a browser and sign in with:
    Username : $ADMIN_USER
    Password : (the one you entered during this run — also stored in $ENV_FILE
               as ADMIN_PASSWORD)

${C_BOLD}Adding media:${C_RESET}
  The library is empty until you add files. Two options:

  A) Web upload (simplest):
       Sign in → /upload  (requires the admin user, or a user with
       can_upload permission). Files are saved to the uploads volume.

  B) Bulk add via host bind mount (for an existing media library):
       1. Stop the stack:        cd $PROJECT_DIR && docker compose down
       2. Copy the example:      cp docker-compose.override.yml.example \\
                                    docker-compose.override.yml
       3. Edit docker-compose.override.yml and uncomment / set:
            - /your/host/videos:/data/videos
            - /your/host/music:/data/music
       4. Restart:               docker compose up -d
       The scanner discovers new files automatically (~1 min).

${C_BOLD}DNS / TLS:${C_RESET}
  Currently reachable via raw IP. To use a domain with HTTPS:
    1. Point an A record at ${PUBLIC_IP:-<this server>}.
    2. Re-run this bootstrap with the domain at the first prompt — Caddy
       will switch to Let's Encrypt automatically.

${C_BOLD}Optional integrations:${C_RESET}
  • Hugging Face: $HF_STATUS
  • Claude:       $CLAUDE_STATUS
  After editing $ENV_FILE, apply with:
       cd $PROJECT_DIR && docker compose restart server

${C_BOLD}Public exposure:${C_RESET}
  • Self-registration:  $REG_STATUS
  • Default user type:  ${DEFAULT_USER_TYPE_INPUT:-standard}
  • HSTS:               $HSTS_STATUS
  • CSP:                on
  Toggle these in $ENV_FILE (AUTH_ALLOW_REGISTRATION, AUTH_DEFAULT_USER_TYPE,
  HSTS_ENABLED, CSP_ENABLED) and restart the server.

${C_BOLD}Backup:${C_RESET}
  Save $ENV_FILE somewhere safe — it has the DB and admin secrets and
  cannot be recovered if lost.

EOF

if [[ "$HARDEN_SSH" == "true" ]]; then
  warn "SSH password login is now DISABLED. Verify you can still log in with your key in a NEW terminal before closing this session."
fi
if ! $HEALTHY; then
  warn "Health check did NOT pass. Inspect 'docker compose logs server' above and the log file."
  exit 2
fi
ok "Bootstrap complete."
exit 0
