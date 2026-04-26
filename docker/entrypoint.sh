#!/bin/sh
# Media Server Pro — container entrypoint.
#
# Responsibilities:
#   1. Make sure runtime data directories exist (the binary expects them).
#   2. Optionally wait for the database before starting (best-effort probe).
#   3. Hand off to the requested binary via `exec` so PID 1 forwards signals.
#
# Usage:
#   /app/entrypoint.sh server               # default — runs the master server
#   /app/entrypoint.sh media-receiver ...   # runs the slave with extra args
#   /app/entrypoint.sh /path/to/binary ...  # passthrough
set -eu

# ── Directory bootstrap ─────────────────────────────────────────────────────
# Each path is overridable via its own env var (see internal/config/
# env_overrides_dirs.go). Honouring the override here means a single shared
# volume mounted under a different name still gets created.
ensure_dir() {
    d="$1"
    [ -n "$d" ] || return 0
    if [ ! -d "$d" ]; then
        mkdir -p "$d" 2>/dev/null || {
            echo "entrypoint: warning: cannot create $d (continuing)" >&2
            return 0
        }
    fi
}

ensure_dir "${VIDEOS_DIR:-/data/videos}"
ensure_dir "${MUSIC_DIR:-/data/music}"
ensure_dir "${THUMBNAILS_DIR:-/data/thumbnails}"
ensure_dir "${PLAYLISTS_DIR:-/data/playlists}"
ensure_dir "${UPLOADS_DIR:-/data/uploads}"
ensure_dir "${ANALYTICS_DIR:-/data/analytics}"
ensure_dir "${HLS_CACHE_DIR:-/data/hls_cache}"
ensure_dir "${DATA_DIR:-/data/app}"
ensure_dir "${LOGS_DIR:-/data/logs}"
ensure_dir "${TEMP_DIR:-/data/temp}"

# ── Volume ownership fix ────────────────────────────────────────────────────
# Docker mounts named volumes as root:root by default, shadowing the
# build-time chown in the Dockerfile. Fix it here so the unprivileged
# `mediaserver` user (created in the runtime stage) can write logs,
# thumbnails, HLS cache, etc. Idempotent — only runs as root.
#
# Look the user up at runtime instead of trusting env defaults — the
# Dockerfile's APP_UID/APP_GID build args may set any uid/gid (matched
# to the host's deploy user), and a stale env default would cause
# `setpriv --init-groups` to fail with "uid N not found".
APP_UID="$(id -u mediaserver 2>/dev/null || echo "${APP_UID:-1000}")"
APP_GID="$(id -g mediaserver 2>/dev/null || echo "${APP_GID:-1000}")"
if [ "$(id -u)" = "0" ]; then
    chown "${APP_UID}:${APP_GID}" /data 2>/dev/null || true
    for d in \
        "${VIDEOS_DIR:-/data/videos}" \
        "${MUSIC_DIR:-/data/music}" \
        "${THUMBNAILS_DIR:-/data/thumbnails}" \
        "${PLAYLISTS_DIR:-/data/playlists}" \
        "${UPLOADS_DIR:-/data/uploads}" \
        "${ANALYTICS_DIR:-/data/analytics}" \
        "${HLS_CACHE_DIR:-/data/hls_cache}" \
        "${DATA_DIR:-/data/app}" \
        "${LOGS_DIR:-/data/logs}" \
        "${TEMP_DIR:-/data/temp}"
    do
        [ -d "$d" ] || continue
        # Only chown the top of the tree; recursive chown on a populated
        # media library is expensive and unnecessary on subsequent boots.
        # If the dir is wrong-owned, fix it (and one level deep for
        # children created earlier under the wrong uid).
        owner=$(stat -c '%u:%g' "$d" 2>/dev/null || echo "")
        if [ "$owner" != "${APP_UID}:${APP_GID}" ]; then
            chown -R "${APP_UID}:${APP_GID}" "$d" 2>/dev/null || true
        fi
    done
fi

# ── Optional DB wait ────────────────────────────────────────────────────────
# Compose already orders us behind `db: service_healthy`, so this is mostly
# useful when running `docker run` directly. Probe with curl, which is in the
# image, by attempting a connect-only request — exit code 7 means the host
# is not reachable, anything else means the port answered.
wait_for_tcp() {
    host="$1"; port="$2"; timeout_s="${3:-60}"
    elapsed=0
    while [ "$elapsed" -lt "$timeout_s" ]; do
        # MySQL/MariaDB sends a banner; curl will fail to parse it but the
        # connect itself succeeds, giving us exit code 52 (empty reply).
        # Anything other than 7 (couldn't connect) means the port is open.
        rc=0
        curl --silent --output /dev/null --max-time 2 \
             "http://${host}:${port}/" || rc=$?
        if [ "$rc" != "7" ] && [ "$rc" != "28" ] && [ "$rc" != "6" ]; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

if [ "${WAIT_FOR_DB:-1}" = "1" ] && [ -n "${DATABASE_HOST:-}" ]; then
    db_port="${DATABASE_PORT:-3306}"
    echo "entrypoint: waiting for database at ${DATABASE_HOST}:${db_port} ..."
    if wait_for_tcp "${DATABASE_HOST}" "${db_port}" "${DB_WAIT_TIMEOUT:-60}"; then
        echo "entrypoint: database reachable."
    else
        echo "entrypoint: database not reachable yet — handing off to the app, which will retry." >&2
    fi
fi

# ── Dispatch ────────────────────────────────────────────────────────────────
# Friendly aliases are accepted; anything else is exec'd verbatim so users can
# drop into a shell with `docker run ... sh`.
cmd="${1:-server}"
[ "$#" -gt 0 ] && shift

# Drop privileges to the unprivileged mediaserver user before exec'ing the
# binary. setpriv ships in util-linux (already in debian-slim).
drop_privs() {
    # Already unprivileged — just exec.
    [ "$(id -u)" != "0" ] && exec "$@"

    # Prefer dropping by username (always resolves correctly regardless
    # of how APP_UID/APP_GID were chosen at build time). Fall back to
    # numeric ids if the user happens to be missing for some reason.
    if command -v setpriv >/dev/null 2>&1; then
        if id mediaserver >/dev/null 2>&1; then
            exec setpriv --reuid=mediaserver --regid=mediaserver --init-groups -- "$@"
        fi
        exec setpriv --reuid="${APP_UID}" --regid="${APP_GID}" --clear-groups -- "$@"
    fi
    if command -v su-exec >/dev/null 2>&1 && id mediaserver >/dev/null 2>&1; then
        exec su-exec mediaserver "$@"
    fi
    if command -v gosu >/dev/null 2>&1 && id mediaserver >/dev/null 2>&1; then
        exec gosu mediaserver "$@"
    fi
    # Last resort: stay as root. The server still runs; just less hardened.
    echo "entrypoint: warning: no setpriv/su-exec/gosu available — running as root" >&2
    exec "$@"
}

case "$cmd" in
    server)
        drop_privs /app/server "$@"
        ;;
    media-receiver|receiver|slave)
        drop_privs /app/media-receiver "$@"
        ;;
    *)
        drop_privs "$cmd" "$@"
        ;;
esac
