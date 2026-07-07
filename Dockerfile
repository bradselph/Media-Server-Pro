# syntax=docker/dockerfile:1.7
#
# Media Server Pro — production image.
#
# This image mirrors the binary that deploy.sh produces on a VPS: same
# Go/Node versions, same VERSION/BuildDate stamp, same embedded Nuxt build.
# It intentionally hardens the build further than the native path — CGO
# disabled + `-trimpath -s -w` — for a smaller, fully static binary (the
# server has no cgo dependencies, so behaviour is identical). Use it as a
# self-contained alternative to deploy.sh when you'd rather ship a container
# than build natively on the host.
#
# Build:
#   docker build -t media-server-pro:latest .
#
# Build args (defaults match go.mod / package.json + deploy-knobs.sh):
#   GO_VERSION   — Go toolchain version (default: 1.26.4, tracks go.mod)
#   NODE_VERSION — Node.js version for the Nuxt build (default: 24)
#   APP_UID      — uid of the runtime user (default: 1000)
#   APP_GID      — gid of the runtime group (default: 1000)
#   NUXT_PUBLIC_*  — frontend knobs baked into the Nuxt bundle. Match the
#                    "build" scope set in deploy-knobs.sh. Empty defaults =
#                    bundle ships with no GA, generic brand, blank legal.
#                    These must be set at *build* time; changing them
#                    requires a rebuild and a fresh image.
#
# Run (single container, external DB):
#   docker run --rm -p 3000:3000 \
#     --env-file .env \
#     -v ./videos:/data/videos \
#     -v ./music:/data/music \
#     media-server-pro:latest
#
# Full stack (MariaDB + server): see docker-compose.yml.

# GO_VERSION tracks go.mod's `go` directive so the image build uses the same
# toolchain deploy.sh installs on a VPS (get_go_version reads go.mod). Bump
# this in lockstep whenever go.mod's `go` line changes.
ARG GO_VERSION=1.26.4
# Node major must ship the same npm major that writes package-lock.json
# (npm 11, from Node 24). npm 10 rejects lockfiles where npm 11 left an
# optional peer dep unresolved ("Missing: <pkg> from lock file" EUSAGE).
ARG NODE_VERSION=24
ARG DEBIAN_VARIANT=bookworm-slim

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: build the Nuxt frontend → web/static/react/
#
# nuxt.config.ts has `nitro.output.publicDir = "../static/react"` so the
# generate step writes the static SPA directly into web/static/react/ where
# Go's //go:embed picks it up in stage 2.
# ─────────────────────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION}-bookworm-slim AS frontend

WORKDIR /src/web/nuxt-ui

# NUXT_PUBLIC_* build args are surfaced as runtimeConfig.public.<camelCase>
# in the bundle via runtimeConfig.public in nuxt.config.ts. They need to be
# set during `npm run build`, hence build-arg→ENV at this stage. Empty
# args (the default) mean the runtime falls through to app.config.ts
# defaults or hard-coded fallbacks.
ARG NUXT_PUBLIC_GA_ID=""
ARG NUXT_PUBLIC_BUILD_ID=""
ARG NUXT_PUBLIC_API_BASE=""
ARG NUXT_PUBLIC_BRAND_NAME=""
ARG NUXT_PUBLIC_BRAND_TAGLINE=""
ARG NUXT_PUBLIC_BRAND_GRADIENT=""
ARG NUXT_PUBLIC_COMPLIANCE_EMAIL=""
ARG NUXT_PUBLIC_COMPLIANCE_ADDRESS=""
ARG NUXT_PUBLIC_DMCA_AGENT_NAME=""
ARG NUXT_PUBLIC_DMCA_EMAIL=""
ARG NUXT_PUBLIC_DMCA_ADDRESS=""
ENV NUXT_PUBLIC_GA_ID=${NUXT_PUBLIC_GA_ID} \
    NUXT_PUBLIC_BUILD_ID=${NUXT_PUBLIC_BUILD_ID} \
    NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE} \
    NUXT_PUBLIC_BRAND_NAME=${NUXT_PUBLIC_BRAND_NAME} \
    NUXT_PUBLIC_BRAND_TAGLINE=${NUXT_PUBLIC_BRAND_TAGLINE} \
    NUXT_PUBLIC_BRAND_GRADIENT=${NUXT_PUBLIC_BRAND_GRADIENT} \
    NUXT_PUBLIC_COMPLIANCE_EMAIL=${NUXT_PUBLIC_COMPLIANCE_EMAIL} \
    NUXT_PUBLIC_COMPLIANCE_ADDRESS=${NUXT_PUBLIC_COMPLIANCE_ADDRESS} \
    NUXT_PUBLIC_DMCA_AGENT_NAME=${NUXT_PUBLIC_DMCA_AGENT_NAME} \
    NUXT_PUBLIC_DMCA_EMAIL=${NUXT_PUBLIC_DMCA_EMAIL} \
    NUXT_PUBLIC_DMCA_ADDRESS=${NUXT_PUBLIC_DMCA_ADDRESS}

# Install dependencies first (cached unless package*.json changes).
COPY web/nuxt-ui/package.json web/nuxt-ui/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

# OpenAPI spec is referenced by the codegen scripts; copy it before sources.
COPY api_spec /src/api_spec

# Frontend source.
COPY web/nuxt-ui ./

# `npm run build` runs `nuxt generate`; nuxt.config.ts emits to ../static/react/.
# Sanity-check the embed target exists so a silent failure here doesn't
# produce a backend image that 404s on every UI request.
RUN npm run build \
 && test -f /src/web/static/react/index.html \
   || (echo "ERROR: nuxt generate did not produce web/static/react/index.html" >&2 && exit 1)

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: build the Go binary
#
# CGO disabled so the runtime image doesn't need libc/libstdc++ matched to
# the build environment. ldflags mirror what install.sh / deploy.sh stamp
# on a native build so `server --version` reports the same string in both.
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-bookworm AS backend

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-buildvcs=false

WORKDIR /src

# Module cache layer — Go deps change less often than source.
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

# -trimpath strips absolute build paths from the binary so two builds on
# different hosts (or in CI vs local) produce identical bytes.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    VERSION="$(cat VERSION 2>/dev/null | tr -d '[:space:]')" \
 && BUILD_DATE="$(date -u +%Y-%m-%d)" \
 && go build -trimpath \
      -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/server ./cmd/server

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: runtime image
#
# debian:bookworm-slim is the minimum that gets us ffmpeg + ca-certs + tini
# without the full debian footprint. tini is PID 1 so ffmpeg child processes
# get reaped cleanly and SIGTERM forwards correctly through to the Go binary
# (the server then runs its graceful-shutdown path).
# ─────────────────────────────────────────────────────────────────────────────
FROM debian:${DEBIAN_VARIANT} AS runtime

ARG APP_UID=1000
ARG APP_GID=1000

ENV DEBIAN_FRONTEND=noninteractive \
    APP_HOME=/app \
    PATH=/app:${PATH} \
    # Defaults — override at runtime via -e / --env-file / compose env.
    # Container-appropriate values: SERVER_HOST=0.0.0.0 so the app is reachable
    # from outside the container (a native deploy.sh VPS install instead uses
    # 127.0.0.1 behind a reverse proxy). Paths are pinned to /data so a single
    # named volume works.
    SERVER_HOST=0.0.0.0 \
    SERVER_PORT=3000 \
    DATABASE_ENABLED=true \
    DATABASE_HOST=db \
    DATABASE_PORT=3306 \
    LOG_LEVEL=info \
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

COPY --from=backend /out/server /app/server
COPY --chown=mediaserver:mediaserver docker/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/server /app/entrypoint.sh

# NOTE: PID 1 starts as root so the entrypoint can chown /data subdirs to
# the mediaserver user — named Docker volumes mount as root by default and
# without this fix the server would crash trying to create thumbnails in a
# directory it can't write to. Entrypoint then drops to mediaserver via
# setpriv before exec'ing the server.

EXPOSE 3000

VOLUME ["/data"]

# tini reaps ffmpeg children and forwards signals so the Go binary's
# graceful-shutdown path (defined in internal/server) runs on SIGTERM.
ENTRYPOINT ["/usr/bin/tini", "--", "/app/entrypoint.sh"]
CMD ["server"]

# start-period/retries match docker-compose.yml so a standalone `docker run`
# tolerates the same first-boot DB-migration window (cold MariaDB + 30+
# CREATE TABLE statements) that compose users get.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=5 \
    CMD curl --fail --silent --show-error \
        "http://127.0.0.1:${SERVER_PORT:-3000}/health" >/dev/null || exit 1

LABEL org.opencontainers.image.title="Media Server Pro" \
      org.opencontainers.image.source="https://github.com/bradselph/Media-Server-Pro" \
      org.opencontainers.image.description="Self-hosted media streaming server (Go backend, Nuxt frontend, embedded SPA)" \
      org.opencontainers.image.licenses="See repository"
