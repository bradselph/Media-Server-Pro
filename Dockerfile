# syntax=docker/dockerfile:1.7
#
# Media Server Pro — production image for the master server.
#
# Multi-stage build:
#   1. frontend  — node:22 builds the Nuxt UI into web/static/react/
#   2. backend   — golang:1.26 builds the server + media-receiver binaries
#                  with the frontend embedded via //go:embed
#   3. runtime   — debian:bookworm-slim with ffmpeg + ca-certificates,
#                  running as a non-root user
#
# Build:
#   docker build -t media-server-pro:latest .
#
# Build args:
#   GO_VERSION   — Go toolchain version (default: 1.26)
#   NODE_VERSION — Node.js version for the Nuxt build (default: 22)
#   APP_UID      — uid of the runtime user (default: 1000)
#   APP_GID      — gid of the runtime group (default: 1000)

ARG GO_VERSION=1.26
ARG NODE_VERSION=22
ARG DEBIAN_VARIANT=bookworm-slim

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: build the Nuxt frontend → web/static/react/
# ─────────────────────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION}-bookworm-slim AS frontend

WORKDIR /src/web/nuxt-ui

# Install dependencies first (cached unless package*.json changes).
COPY web/nuxt-ui/package.json web/nuxt-ui/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

# OpenAPI spec is referenced by the codegen scripts; copy it before sources.
COPY api_spec /src/api_spec

# Frontend source.
COPY web/nuxt-ui ./

# nuxt.config.ts emits the static build to ../static/react via nitro.
# Run `nuxt generate` (= npm run build) and verify the embed target exists.
RUN npm run build \
 && test -f /src/web/static/react/index.html \
   || (echo "ERROR: nuxt generate did not produce web/static/react/index.html" >&2 && exit 1)

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: build the Go binaries
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-bookworm AS backend

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-buildvcs=false

WORKDIR /src

# Module cache layer.
COPY go.mod go.sum ./
COPY patches ./patches
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Source tree.
COPY VERSION ./VERSION
COPY api ./api
COPY api_spec ./api_spec
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY web ./web

# Pull the Nuxt build into web/static/react/ so //go:embed picks it up.
COPY --from=frontend /src/web/static/react ./web/static/react

# Build both binaries with version info baked in. The ldflag pattern matches
# what install.sh / deploy.sh use on the host.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    VERSION="$(cat VERSION 2>/dev/null | tr -d '[:space:]')" \
 && BUILD_DATE="$(date -u +%Y-%m-%d)" \
 && go build -trimpath \
      -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/server ./cmd/server \
 && go build -trimpath \
      -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/media-receiver ./cmd/media-receiver

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: runtime image
# ─────────────────────────────────────────────────────────────────────────────
FROM debian:${DEBIAN_VARIANT} AS runtime

ARG APP_UID=1000
ARG APP_GID=1000

ENV DEBIAN_FRONTEND=noninteractive \
    APP_HOME=/app \
    PATH=/app:${PATH} \
    # Defaults — override at runtime via -e or compose env.
    SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8080 \
    VIDEOS_DIR=/data/videos \
    MUSIC_DIR=/data/music \
    THUMBNAILS_DIR=/data/thumbnails \
    PLAYLISTS_DIR=/data/playlists \
    UPLOADS_DIR=/data/uploads \
    ANALYTICS_DIR=/data/analytics \
    HLS_CACHE_DIR=/data/hls_cache \
    DATA_DIR=/data/app \
    LOGS_DIR=/data/logs \
    TEMP_DIR=/data/temp

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        ffmpeg \
        ca-certificates \
        tini \
        curl \
        tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --system --gid "${APP_GID}" mediaserver \
 && useradd  --system --uid "${APP_UID}" --gid "${APP_GID}" \
        --home-dir "${APP_HOME}" --shell /usr/sbin/nologin mediaserver \
 && mkdir -p "${APP_HOME}" /data \
 && chown -R mediaserver:mediaserver "${APP_HOME}" /data

WORKDIR ${APP_HOME}

COPY --from=backend /out/server          /app/server
COPY --from=backend /out/media-receiver  /app/media-receiver
COPY --chown=mediaserver:mediaserver docker/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/server /app/media-receiver /app/entrypoint.sh

# NOTE: deliberately running PID 1 as root so the entrypoint can fix the
# ownership of named volumes (mounted as root by Docker) before dropping
# to the unprivileged `mediaserver` user via setpriv. See entrypoint.sh.

EXPOSE 8080

VOLUME ["/data"]

# Tini reaps zombies (ffmpeg children) and forwards signals cleanly.
ENTRYPOINT ["/usr/bin/tini", "--", "/app/entrypoint.sh"]
CMD ["server"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl --fail --silent --show-error \
        "http://127.0.0.1:${SERVER_PORT:-8080}/health" >/dev/null || exit 1

LABEL org.opencontainers.image.title="Media Server Pro" \
      org.opencontainers.image.source="https://github.com/bradselph/Media-Server-Pro" \
      org.opencontainers.image.licenses="See repository"
