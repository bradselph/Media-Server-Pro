#!/bin/sh
# Media Server Pro container entrypoint.
#
# Runs as root (PID 1 via tini) just long enough to ensure /data subdirs
# exist and are owned by the unprivileged `mediaserver` user, then drops
# privileges via setpriv and exec's the server.
#
# Why root for the bootstrap step:
#   - Docker named volumes are created on the host as root and mounted
#     into the container as root, regardless of USER in the Dockerfile.
#     The server would crash on first run trying to scan/write into
#     /data/videos, /data/thumbnails, etc.
#   - Fixing ownership inside the entrypoint is idempotent and only does
#     real work on first boot; subsequent boots are no-ops.
#
# Why setpriv instead of su / runuser:
#   - setpriv is a util-linux primitive (no PAM, no login overhead) and
#     correctly forwards signals from tini to the server, which is what
#     graceful shutdown depends on.

set -eu

APP_USER="${APP_USER:-mediaserver}"
APP_GROUP="${APP_GROUP:-mediaserver}"

# Subdirs the server writes to. Keep this list in sync with the *_DIR
# defaults in the Dockerfile -- the server creates these lazily, but
# named volumes need the ownership fix BEFORE that lazy mkdir runs or
# it fails with EACCES.
DATA_SUBDIRS="
  /data/videos
  /data/music
  /data/thumbnails
  /data/playlists
  /data/uploads
  /data/analytics
  /data/hls_cache
  /data/app
  /data/logs
  /data/temp
"

# Only do the chown dance when we're root. If the operator already ran us
# with `docker run --user=...`, ownership is whatever they set; we just
# skip and trust them. The server's own startup will then surface a clear
# error if /data isn't writable.
if [ "$(id -u)" = "0" ]; then
    for d in $DATA_SUBDIRS; do
        # mkdir -p is idempotent. Only chown when ownership is wrong, so
        # we don't churn inode mtimes on every boot.
        mkdir -p "$d"
        current_uid="$(stat -c %u "$d" 2>/dev/null || echo 0)"
        target_uid="$(id -u "$APP_USER" 2>/dev/null || echo 1000)"
        if [ "$current_uid" != "$target_uid" ]; then
            chown -R "$APP_USER":"$APP_GROUP" "$d"
        fi
    done

    # If the operator mounted a host directory at /data with their own
    # ownership scheme, the top-level chown above stays scoped to the
    # subdirs we manage. That keeps `docker run -v $PWD/media:/data`
    # from re-chowning the operator's whole library.
    if [ ! -O /data ]; then
        chown "$APP_USER":"$APP_GROUP" /data 2>/dev/null || true
    fi

    # Hand off to the unprivileged user. exec replaces this shell so
    # tini still sees the server as PID 1's child for signal forwarding.
    exec setpriv --reuid="$APP_USER" --regid="$APP_GROUP" --init-groups -- /app/server "$@"
fi

# Non-root path: just exec the server. CMD's "server" arg is dropped via
# the `case` below because the Dockerfile's CMD is `["server"]` (legacy
# nudge) -- if anyone overrides CMD with a real binary path we honor it.
case "${1:-}" in
    server|/app/server|"")
        exec /app/server
        ;;
    *)
        exec "$@"
        ;;
esac
