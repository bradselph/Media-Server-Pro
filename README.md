# Media Server Pro

A self-hosted media streaming server. Go backend, Nuxt frontend, MariaDB datastore. Ships as a single binary for native installs and as a published OCI image for Docker. Designed to run on a VPS in front of Caddy or nginx and stream a personal video/audio library to any device.

Two Media Server Pro instances can federate: enter a peer's URL + receiver API key in the admin UI and the two libraries appear as one to users, with the master proxying byte streams from the slave on demand.

---

## Features

**Library and streaming**
- Video and audio streaming with HTTP range requests, resume-position tracking, watch history.
- HLS adaptive bitrate ladder (multi-quality master + variant playlists with on-disk segment cache).
- Thumbnail generation pipeline (ffmpeg-based, with cleanup, corrupt detection, and orphan removal).
- BlurHash placeholders for cards while thumbs load.
- Per-file content fingerprinting (SHA-256 head/tail + size) for deduplication across slaves.

**Users and access**
- Session-based auth with bcrypt password storage, configurable lockout, optional registration, optional guest browsing.
- Role-based permissions (`can_stream`, `can_download`, `can_upload`, `can_delete`, `can_manage`, `can_view_mature`, `can_create_playlists`).
- Personal playlists, favorites, ratings, and watch history.
- Self-serve account deletion request flow (admin-approved; no automatic erase).
- Mature-content age gate with cookie/IP TTL.

**Admin surface**
- Full admin UI: users, media library, scanner, classifier, HLS jobs, thumbnails, validator, suggestions, playlists, sources, security, audit log, backups, updates, system config, analytics, and the optional Claude assistant module.
- Live config reload — most security and feature settings flip without a server restart.
- Hot-reloadable rate limits, CORS origins, security headers, trusted-proxy CIDRs.
- Built-in backup/restore with pre-upgrade DB snapshots taken by `update.sh`.

**Distributed deployment (federated peers)**
- A full Media Server Pro instance can act as a slave to another by entering the peer's URL + receiver API key in the admin UI; catalog flows over WebSocket and byte streams via outbound HTTP push.
- Cross-server pairing helper (`Sources → Pair from peer`) configures the remote side from this admin so you don't have to log into both servers.
- Duplicate detection across slaves with admin-resolved conflict workflow.

**Optional integrations**
- S3-compatible object storage (MinIO, Backblaze B2 verified) with presigned URLs for ffmpeg.
- Hugging Face visual classifier for mature-content tagging.
- Claude assistant (admin-only) with three modes: advisory, interactive, autonomous.
- Standalone media downloader integration (proxy to a separate downloader service).
- Extractor module (HLS proxy for external video URLs).

**Operability**
- Single static binary, no CGO required.
- Embedded Nuxt SPA — the server serves UI and API from one process.
- `/health` endpoint for systemd / nginx upstream / uptime monitors.
- `/metrics` (admin-protected) for Prometheus scraping.
- Structured logs via the internal `logger` package.

What you will **not** find here: subtitles. They are explicitly out of scope and won't be added.

---

## Quick start

### Docker (recommended for fresh VPS)

The fastest path on a clean Debian/Ubuntu VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/bradselph/Media-Server-Pro/main/vps-bootstrap.sh -o vps-bootstrap.sh
chmod +x vps-bootstrap.sh
./vps-bootstrap.sh
```

The bootstrap installs Docker, prompts for credentials and integration keys, writes a fully-populated `.env.docker`, optionally configures Caddy with auto-HTTPS for your domain, and brings the stack up. Pre-built images are pulled from `ghcr.io/bradselph/media-server-pro:main`; if the pull fails, it falls back to a local build.

If you already have Docker:

```bash
git clone https://github.com/bradselph/Media-Server-Pro
cd Media-Server-Pro
cp .env.docker.example .env.docker
# edit .env.docker — at minimum set DB_ROOT_PASSWORD, DATABASE_PASSWORD,
# ADMIN_PASSWORD, and CORS_ORIGINS
docker compose --env-file .env.docker pull
docker compose --env-file .env.docker up -d
```

The server listens on `${SERVER_PORT}` (default 8080). The first admin login uses `ADMIN_USERNAME` / `ADMIN_PASSWORD` from `.env.docker`. If you leave the password blank, a random one is generated and written to `data/admin-initial-password.txt` (mode 0600).

### Native install

For a bare-metal install (no Docker), use `setup.sh`:

```bash
git clone https://github.com/bradselph/Media-Server-Pro
cd Media-Server-Pro
./setup.sh        # interactive: prompts for DB, admin, ports, features
./install.sh      # builds the binary and installs the systemd unit
```

`deploy.sh` automates the same flow from a developer workstation against a remote VPS over SSH (`./deploy.sh --setup` for first-time bring-up, plain `./deploy.sh` for subsequent updates).

### Updating

```bash
./update.sh                    # native install: pull, build, restart, with rollback
./update.sh --rollback         # restore previous binary if the new one regresses
docker compose pull && docker compose up -d   # Docker: rolling pull
```

`update.sh` snapshots the database to `./backups/` before every upgrade and keeps the previous Docker image tagged so rollbacks are immediate.

---

## Federated peers

Two full Media Server Pro instances can pair so each one's media appears on both servers as if local. Setup is entirely runtime — no separate slave binary, no extra deploy:

1. On the **source** server (the one with the media): `admin → Sources → Receiver` — copy any configured API key.
2. On the **receiving** server: `admin → Sources → Pair from peer` — paste the source's URL + API key.

The receiving server's helper hits `POST /api/admin/peer/connect`, which calls back to the source's `/api/receiver/pair` and configures the source's follower to push its catalog to the receiver. From then on, slave items appear seamlessly in the unified `/api/media` listing, with thumbnails proxied on demand and byte streams pushed over WebSocket-controlled HTTP.

Either side can also pre-seed pairing through env (`FOLLOWER_MASTER_URL`, `FOLLOWER_API_KEY`, `RECEIVER_API_KEYS`) — see `.env.docker.example`. The source makes only outbound connections; no inbound port needs opening on it.

---

## Configuration

Configuration comes from three layers, in order of precedence:

1. **Environment variables** (highest) — full matrix in `internal/config/env_overrides_*.go`.
2. **`config.json`** — written on first start, hot-reloaded on most field changes.
3. **Built-in defaults** — see `internal/config/defaults.go`.

For Docker, env vars are set via `.env.docker` (forwarded to the container by `env_file:`). For native installs, `.env` in the project root is loaded at startup. **Always single-quote secrets in either file** — unquoted values containing `#`, `$`, embedded whitespace, or special chars are silently mangled by the env-file parser, which is the most common cause of "admin login fails" reports. `vps-bootstrap.sh` and `setup.sh` quote automatically; manual edits should follow suit.

### Required env at minimum

| Var | Purpose |
|---|---|
| `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_NAME`, `DATABASE_USERNAME`, `DATABASE_PASSWORD` | MariaDB connection |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD` (or `ADMIN_PASSWORD_HASH`) | First admin login |
| `SERVER_PORT`, `SERVER_BIND` | Bind address — set to `127.0.0.1` behind Caddy/nginx |

### Useful flags / env

| Var | Default | Purpose |
|---|---|---|
| `AUTH_ALLOW_REGISTRATION` | `false` | Public self-registration |
| `AUTH_ALLOW_GUESTS` | `false` | Anonymous browsing without login |
| `AUTH_SECURE_COOKIES` | `false` | Set `true` once HTTPS is live |
| `RECEIVER_ENABLED` / `RECEIVER_API_KEYS` | off | Accept federated peers (slave catalog ingest) |
| `FOLLOWER_MASTER_URL` / `FOLLOWER_API_KEY` | off | This server pushes its catalog to a peer |
| `FEATURE_HUGGINGFACE` / `HUGGINGFACE_API_KEY` | off | Visual mature-content classifier |
| `FEATURE_CLAUDE` / `ANTHROPIC_API_KEY` / `CLAUDE_MODE` | off | Admin-only Claude assistant |
| `STORAGE_BACKEND` (`local`/`s3`) + `S3_ENDPOINT` / `S3_BUCKET` / `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY` | `local` | Object-storage backend |
| `HSTS_ENABLED`, `CSP_ENABLED` | mixed | HTTP security headers |
| `RATE_LIMIT_ENABLED`, `RATE_LIMIT_REQUESTS`, `RATE_LIMIT_WINDOW_SECONDS` | on, 100/60s | Per-IP rate limit |

The full matrix lives in `internal/config/env_overrides_*.go` (one file per concern: auth, server, storage, hls, security, receiver, follower, etc.).

---

## API

OpenAPI 3.0.3 spec at [`api_spec/openapi.yaml`](api_spec/openapi.yaml). Routes are registered in [`api/routes/routes.go`](api/routes/routes.go); the typed Nuxt client is regenerated from the spec via `npm run codegen:openapi`.

The API surface covers:

- `auth`, `users`, `tokens`, `permissions`, `preferences`
- `media`, `streaming`, `hls`, `playback`, `watch-history`, `favorites`, `ratings`, `playlists`
- `analytics`, `suggestions`, `feed`, `browse`
- `upload`, `thumbnails`, `storage`
- `receiver` (slave-facing) and `admin-receiver` (admin-facing)
- All `admin-*` modules: `admin-users`, `admin-media`, `admin-config`, `admin-tasks`, `admin-audit`, `admin-backups`, `admin-scanner`, `admin-hls`, `admin-thumbnails`, `admin-validator`, `admin-playlists`, `admin-security`, `admin-categorizer`, `admin-discovery`, `admin-suggestions`, `admin-remote`, `admin-updates`, `admin-database`, `admin-analytics`, `admin-classify`, `admin-extractor`, `admin-crawler`, `admin-downloader`, `admin-duplicates`, `admin-streams`

WebSocket endpoints are intentionally outside the OpenAPI spec — see `api/routes/routes.go` for `/ws/receiver` and `/ws/admin/downloader`.

---

## Repository layout

```
cmd/
  server/              # main server binary
api/
  handlers/            # gin handlers, one file per concern
  routes/              # route registration + middleware composition
internal/
  auth/ admin/         # session and admin authentication
  config/              # layered config: defaults → config.json → env overrides
  database/            # GORM init, migrations
  media/ streaming/    # library, range requests, content fingerprinting
  hls/                 # adaptive ladder, segment cache
  thumbnails/          # ffmpeg pipeline, cleanup, orphan removal
  analytics/           # event tracking, daily stats
  receiver/            # master side: catalog ingest, WS+HTTP proxy
  follower/            # full server acting as slave (in-server)
  duplicates/          # cross-slave dedup
  remote/              # remote media proxy / cache
  extractor/           # external URL HLS proxy
  crawler/             # external library discovery
  scanner/             # local-disk media discovery
  classify/            # mature-content tagging
  claude/              # admin Claude assistant module
  ...
pkg/
  models/              # domain types
  helpers/             # cross-cutting utilities (SafeHTTPTransport, etc.)
  middleware/          # gin middleware (rate limit, IP filter, security headers)
  storage/             # S3/MinIO backend
  huggingface/         # HF API client
repositories/          # GORM-backed persistence
web/
  nuxt-ui/             # Nuxt 3 SPA (frontend source)
  static/              # Embedded SPA build output
  server.go            # Static asset embedding
api_spec/openapi.yaml  # Authoritative API contract
patches/               # Vendored dependency patches (ffmpeg-go without aws-sdk-go-v1)
systemd/               # Service unit templates
docker-compose.yml     # Compose stack (server + MariaDB, optional MinIO profile)
Dockerfile             # Server image (Go + Nuxt build, single image)
deploy.sh              # SSH-based deploy/update for the server
setup.sh / install.sh  # Interactive native setup
update.sh              # Native upgrade with DB snapshot + rollback
vps-bootstrap.sh       # End-to-end fresh-VPS bootstrap (Docker mode)
```

---

## Development

```bash
# Backend (Go 1.26.2)
go build ./...
go test ./...

# Frontend (Node 22, pnpm/npm)
cd web/nuxt-ui
npm install
npm run dev          # standalone dev server (proxy to Go on :8080)
npm run check        # codegen + typecheck + generate
npm run build        # writes static SPA into web/static
```

The Go binary embeds `web/static`, so a full release is `cd web/nuxt-ui && npm run build && cd ../.. && go build ./cmd/server`.

For master/slave development on a single machine, the dedicated `--profile receiver` target in `docker-compose.yml` runs both sides in one stack.

---

## License

Proprietary. See repository for full terms.

---

## Project status

Active development. See `MEMORY.md` index files (when present) and the commit log for recent direction. Issues and pull requests welcome at <https://github.com/bradselph/Media-Server-Pro>.
