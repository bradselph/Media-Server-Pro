#!/usr/bin/env bash
# ══════════════════════════════════════════════════════════════════════════════
#  Media Server Pro — Docker stack updater
# ══════════════════════════════════════════════════════════════════════════════
#
#  Safely updates a Docker Compose deployment in place:
#
#    1.  Pre-flight: must be root, repo must exist, stack must be defined
#    2.  Snapshot the database (mariadb-dump -> backups/db-YYYYMMDD-HHMMSS.sql.gz)
#    3.  Show what's about to change (git log range, dirty files)
#    4.  git pull --ff-only on the cloned repo
#    5.  Rebuild the image (docker compose build)
#    6.  Recreate ONLY the server container (db keeps running, no downtime
#        for in-flight DB connections from sibling tooling)
#    7.  Health-check /health on 127.0.0.1:<port>
#    8.  On failure, offer to roll back to the previous image
#
#  Persistence:
#    All host data lives in named Docker volumes (videos, music, db-data, etc.)
#    or on the host filesystem (.env.docker). Neither is ever touched by an
#    update — only the application binary inside the image gets replaced.
#
#  Usage:
#    sudo ./update.sh                  # interactive
#    sudo ./update.sh --yes            # accept defaults, no prompts
#    sudo ./update.sh --skip-backup    # skip DB snapshot (faster, riskier)
#    sudo ./update.sh --no-rebuild     # only `up -d`, don't rebuild image
#    sudo ./update.sh --rollback       # revert to the previous image tag
#    sudo ./update.sh --help
# ══════════════════════════════════════════════════════════════════════════════

set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="$SCRIPT_DIR/logs"
LOG_FILE="$LOG_DIR/update-$TIMESTAMP.log"
BACKUP_DIR="$SCRIPT_DIR/backups"
ENV_FILE="$SCRIPT_DIR/.env.docker"
COMPOSE_BASE_FILE="$SCRIPT_DIR/docker-compose.yml"

ASSUME_YES=false
SKIP_BACKUP=false
NO_REBUILD=false
ROLLBACK=false

# Colours
if [[ -t 1 ]] && [[ -z "${NO_COLOR:-}" ]]; then
  C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'
  C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'; C_YELLOW=$'\033[1;33m'
  C_CYAN=$'\033[0;36m'; C_BLUE=$'\033[0;34m'
else
  C_RESET=''; C_BOLD=''
  C_RED=''; C_GREEN=''; C_YELLOW=''; C_CYAN=''; C_BLUE=''
fi

_log()    { printf '[%s] %s\n' "$(date '+%F %T')" "$*" >> "$LOG_FILE" 2>/dev/null || true; }
info()    { printf "%s[i]%s %s\n" "$C_CYAN"   "$C_RESET" "$*"; _log "INFO  $*"; }
ok()      { printf "%s[\xe2\x9c\x93]%s %s\n" "$C_GREEN" "$C_RESET" "$*"; _log "OK    $*"; }
warn()    { printf "%s[!]%s %s\n" "$C_YELLOW" "$C_RESET" "$*"; _log "WARN  $*"; }
err()     { printf "%s[\xe2\x9c\x97]%s %s\n" "$C_RED"   "$C_RESET" "$*" >&2; _log "ERROR $*"; }
die()     { err "$*"; err "Log: $LOG_FILE"; exit 1; }
section() {
  printf "\n%s%s======================================================================%s\n" "$C_BOLD" "$C_BLUE" "$C_RESET"
  printf   "%s%s  %s%s\n" "$C_BOLD" "$C_BLUE" "$*" "$C_RESET"
  printf   "%s%s======================================================================%s\n\n" "$C_BOLD" "$C_BLUE" "$C_RESET"
}

prompt_yn() {
  local var="$1" text="$2" default="$3"
  local display input
  [[ "${default,,}" == "y" ]] && display="Y/n" || display="y/N"
  if $ASSUME_YES; then
    case "${default,,}" in y|yes) printf -v "$var" '%s' "true" ;; *) printf -v "$var" '%s' "false" ;; esac
    return
  fi
  read -rp "  $text [$display]: " input
  input="${input:-$default}"
  case "${input,,}" in y|yes) printf -v "$var" '%s' "true" ;; *) printf -v "$var" '%s' "false" ;; esac
}

# ── Args ──────────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)      sed -n '/^# ═*$/,/^# ═*$/p' "$0" | head -40; exit 0 ;;
    -y|--yes)       ASSUME_YES=true; shift ;;
    --skip-backup)  SKIP_BACKUP=true; shift ;;
    --no-rebuild)   NO_REBUILD=true; shift ;;
    --rollback)     ROLLBACK=true; shift ;;
    *) err "Unknown flag: $1"; exit 1 ;;
  esac
done

mkdir -p "$LOG_DIR" "$BACKUP_DIR" 2>/dev/null || true
: > "$LOG_FILE" 2>/dev/null || true

# ── Pre-flight ────────────────────────────────────────────────────────────────
section "Pre-flight"

[[ $EUID -eq 0 ]] || die "Must be run as root (sudo $0)"
[[ -f "$COMPOSE_BASE_FILE" ]] || die "$COMPOSE_BASE_FILE not found — run from the project directory"
[[ -f "$ENV_FILE" ]] || die "$ENV_FILE not found — run vps-bootstrap.sh first"
command -v docker >/dev/null 2>&1 || die "docker not installed"
docker compose version >/dev/null 2>&1 || die "docker compose plugin not installed"

cd "$SCRIPT_DIR" || die "Cannot cd to $SCRIPT_DIR"

# Bypass the dev override file (same logic as the bootstrap script).
COMPOSE_FILE_ARGS=(-f docker-compose.yml)
if [[ -f docker-compose.override.yml ]]; then
  warn "docker-compose.override.yml present — ignoring (dev-only)."
fi

# Whitelist for git so root can pull a deploy-user-owned repo.
if [[ -d .git ]]; then
  git config --global --get-all safe.directory 2>/dev/null \
    | grep -qxF "$SCRIPT_DIR" \
    || git config --global --add safe.directory "$SCRIPT_DIR" >/dev/null 2>&1
fi

ok "Repo: $SCRIPT_DIR"
ok "Env file: $ENV_FILE"
ok "Compose: $COMPOSE_BASE_FILE"
ok "Log: $LOG_FILE"

# ── Rollback path ─────────────────────────────────────────────────────────────
if $ROLLBACK; then
  section "Rollback"
  PREV_IMAGE="$(docker image ls media-server-pro --format '{{.ID}} {{.CreatedAt}}' \
                | sort -k 2 -r | awk 'NR==2 {print $1}')"
  if [[ -z "$PREV_IMAGE" ]]; then
    die "No previous media-server-pro image found to roll back to."
  fi
  info "Tagging $PREV_IMAGE as media-server-pro:latest"
  docker tag "$PREV_IMAGE" media-server-pro:latest >>"$LOG_FILE" 2>&1 \
    || die "docker tag failed"
  info "Recreating server container with the previous image…"
  docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
    up -d --no-build --force-recreate server >>"$LOG_FILE" 2>&1 \
    || die "Rollback compose up failed"
  ok "Rolled back to $PREV_IMAGE"
  exit 0
fi

# ── DB snapshot ───────────────────────────────────────────────────────────────
section "Database snapshot"
if $SKIP_BACKUP; then
  warn "Skipping DB snapshot (--skip-backup). If the upgrade introduces a bad"
  warn "schema migration there is no clean rollback target."
else
  if ! docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
       ps --format '{{.Service}}\t{{.Status}}' 2>/dev/null \
       | awk -F'\t' '$1=="db" && $2 ~ /Up/ {found=1} END{exit !found}'; then
    warn "db container is not running — cannot snapshot. Continuing without backup."
  else
    BACKUP_FILE="$BACKUP_DIR/db-$TIMESTAMP.sql.gz"
    info "Dumping MariaDB → $BACKUP_FILE"
    if docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
        exec -T db sh -c \
        'mariadb-dump -uroot -p"$MARIADB_ROOT_PASSWORD" --single-transaction --quick --lock-tables=false --all-databases' \
        2>>"$LOG_FILE" | gzip -c > "$BACKUP_FILE"; then
      SIZE=$(du -h "$BACKUP_FILE" 2>/dev/null | cut -f1)
      ok "Snapshot saved: $BACKUP_FILE ($SIZE)"
      ok "Restore with: gunzip -c $BACKUP_FILE | docker compose exec -T db mariadb -uroot -p\"\$MARIADB_ROOT_PASSWORD\""
    else
      warn "DB snapshot FAILED — continuing anyway. Inspect $LOG_FILE."
      rm -f "$BACKUP_FILE"
    fi
  fi
fi

# ── git pull ──────────────────────────────────────────────────────────────────
section "Pull latest code"
if [[ -d .git ]]; then
  CUR_HEAD=$(git rev-parse HEAD 2>/dev/null || echo "?")
  info "Current HEAD: ${CUR_HEAD:0:12}"
  if git fetch --quiet origin 2>>"$LOG_FILE"; then
    REMOTE_HEAD=$(git rev-parse '@{u}' 2>/dev/null || echo "")
    if [[ -z "$REMOTE_HEAD" || "$REMOTE_HEAD" == "$CUR_HEAD" ]]; then
      ok "Already on latest origin commit. Nothing to pull."
    else
      info "Incoming commits:"
      git log --oneline --no-decorate "$CUR_HEAD..$REMOTE_HEAD" 2>/dev/null | sed 's/^/    /' | head -20
      prompt_yn DO_PULL "Pull these commits?" "y"
      if [[ "$DO_PULL" == "true" ]]; then
        if git pull --ff-only origin >>"$LOG_FILE" 2>&1; then
          NEW_HEAD=$(git rev-parse HEAD 2>/dev/null || echo "?")
          ok "Pulled. HEAD: ${CUR_HEAD:0:12} → ${NEW_HEAD:0:12}"
        else
          err "git pull --ff-only failed (working tree may be dirty or branch diverged)."
          die "Resolve manually with: git status; git stash; git pull --ff-only"
        fi
      fi
    fi
  else
    warn "git fetch failed — skipping pull, will rebuild current code."
  fi
else
  warn "Not a git checkout — skipping pull, will rebuild current code."
fi

# ── Build ─────────────────────────────────────────────────────────────────────
section "Build image"
if $NO_REBUILD; then
  info "Skipping rebuild (--no-rebuild)."
else
  info "Building media-server-pro:latest (this can take 5-10 minutes)…"
  if ! docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
       build server 2>&1 | tee -a "$LOG_FILE"; then
    die "Build failed. The previous image is still tagged :latest, so the next 'compose up' will keep using it."
  fi
  ok "Image built."
fi

# ── Recreate ──────────────────────────────────────────────────────────────────
section "Recreate server container"
info "Stopping + restarting the server container (db, volumes, .env stay in place)…"
if ! docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
     up -d --no-build --force-recreate server 2>&1 | tee -a "$LOG_FILE"; then
  die "compose up failed."
fi
ok "Server container recreated."

# ── Health check ──────────────────────────────────────────────────────────────
section "Health check"
MS_PORT=$(grep -E '^SERVER_PORT=' "$ENV_FILE" | head -1 | cut -d= -f2- | tr -d '"')
MS_PORT="${MS_PORT:-8080}"
HEALTH_URL="http://127.0.0.1:${MS_PORT}/health"
info "Polling $HEALTH_URL for up to 3 minutes…"
HEALTHY=false
for i in $(seq 1 36); do
  if curl -fsS --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  if (( i % 6 == 0 )); then printf " [%ds]" $((i*5)); else printf "."; fi
  sleep 5
done
echo

if $HEALTHY; then
  ok "Server responded healthy on $HEALTH_URL"
  echo
  ok "${C_BOLD}Update complete.${C_RESET}"
  echo
  info "If something looks off, roll back with: sudo $0 --rollback"
  exit 0
fi

# ── Failure: offer rollback ───────────────────────────────────────────────────
err "Health check failed within 3 minutes. Server may be crash-looping."
warn "Container status:"
docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" ps 2>&1 | sed 's/^/    /'
warn "Last 80 server log lines:"
docker compose --env-file "$ENV_FILE" "${COMPOSE_FILE_ARGS[@]}" \
  logs --tail=80 server 2>&1 | sed 's/^/    /'

prompt_yn DO_ROLLBACK "Roll back to the previous image now?" "y"
if [[ "$DO_ROLLBACK" == "true" ]]; then
  exec "$0" --rollback ${ASSUME_YES:+--yes}
fi
exit 2
