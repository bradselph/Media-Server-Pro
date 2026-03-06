# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Media Server Pro is a modular, fault-tolerant media streaming server written in Go with a Gin HTTP framework. It
provides video/audio streaming with HLS adaptive streaming, thumbnail generation, analytics, user authentication,
mature content scanning, master-slave media distribution via WebSocket tunnels, and an admin panel. The frontend is a
React 19 + TypeScript + Vite SPA served from `web/static/react/`.

## Build and Run Commands

```bash
# Build (Windows / Linux)
go build -o server.exe ./cmd/server     # Windows
go build -o server ./cmd/server         # Linux/Mac

# IMPORTANT: Always use ./cmd/server (package path), NOT cmd/server/main.go
# The latter misses platform-specific files (signals_windows.go, etc.)

# Build with version info
go build -ldflags "-X main.Version=4.1.0 -X main.BuildDate=$(date +%Y-%m-%d)" -o server.exe ./cmd/server

# Linux cross-compile from Windows (CGO must be disabled)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./cmd/server

# Run
./server.exe                                    # defaults
./server.exe -config config.json -log-level debug

# Development
go build ./...    # check compilation
go fmt ./...      # format
go vet ./...      # static analysis
```

### Slave Node (media-receiver)

```bash
# Build the slave node binary (connects to master via WebSocket)
go build -o media-receiver.exe ./cmd/media-receiver   # Windows
go build -o media-receiver ./cmd/media-receiver       # Linux/Mac
```

The slave scans local media directories and pushes the catalog to the master via WebSocket. No public IP or port
forwarding is needed — the slave initiates all connections.

### Frontend

```bash
cd web/frontend && npm run build    # builds to web/static/react/
```

The built bundle is embedded into the Go binary via `//go:embed` in `web/server.go`. The binary is self-contained.

## Architecture

### Modular System

Every feature is an independent module implementing `server.Module`:

```go
type Module interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Health() models.HealthStatus
}
```

**Critical modules** (failure prevents startup): `database`, `security`, `auth`, `media`, `streaming`, `tasks`,
`scanner`, `thumbnails`

**Non-critical modules** (can fail gracefully): `hls`, `analytics`, `playlist`, `admin`, `upload`, `validator`,
`backup`, `autodiscovery`, `suggestions`, `categorizer`, `updater`, `remote`, `receiver`

### Directory Structure

```
cmd/
  ├── server/              Entry point (main.go)
  └── media-receiver/      Slave node binary (connects to master via WebSocket)
internal/                  Internal packages (all modules)
  ├── config/              Configuration management
  ├── database/            MySQL connection & GORM auto-migrations
  ├── repositories/        Repository pattern for data access
  │   ├── interfaces.go    Repository contracts
  │   └── mysql/           GORM implementations (18 repository files)
  ├── auth/                Authentication & sessions
  ├── media/               Media library management (discovery.go, management.go)
  ├── receiver/            Master-slave WebSocket tunnel (receiver.go, wsconn.go)
  ├── server/              Gin engine setup + platform-specific signal handling
  └── [18+ modules]/       hls, streaming, security, tasks, thumbnails, etc.
api/
  ├── handlers/            Gin HTTP handlers (24 files across domains)
  │   └── handler.go       Handler struct, HandlerDeps, helper functions
  └── routes/              Route registration (routes.go — 173 route registrations)
pkg/
  ├── helpers/             Utility functions
  ├── middleware/          HTTP middleware (Gin + standard net/http variants)
  └── models/              Shared data models
web/
  ├── frontend/            React 19 + TypeScript + Vite source
  │   └── src/             Pages, hooks, stores, API client
  ├── static/              Embedded assets (react/ bundle, other assets)
  └── server.go            Static file serving (go:embed)
```

### Request Flow

1. **Request arrives** → `api/routes/routes.go` (Gin router)
2. **Middleware pipeline** (applied in this order):
   - `GinRequestID` — unique request ID via X-Request-ID header
   - `GinSecurityHeaders` — CSP, HSTS, X-Frame-Options, etc.
   - `GinCORS` — CORS headers (if configured)
   - `gzip.Gzip` — compression (excludes media/stream paths)
   - `ginETags` — FNV-1a content-based ETags for API responses
   - `securityModule.GinMiddleware()` — rate limiting, IP filtering
   - `sessionAuth` — loads session/user from `session_id` cookie into gin context
3. **Route-level middleware**:
   - `requireAuth()` — requires valid, non-expired session
   - `adminAuth()` — requires session with `role=admin`
4. **Handler** (from `api/handlers/`) processes request
5. **Module** (from `internal/`) performs business logic
6. **Response** — JSON via `writeSuccess(c, data)` / `writeError(c, status, msg)`

### Framework-Agnostic Module Design

Internal modules (`internal/`) use standard `http.ResponseWriter` and `*http.Request` in their method signatures.
This is **intentional** — modules are framework-agnostic so they can work with any HTTP framework. Gin handlers bridge
the gap by passing `c.Writer` and `c.Request`:

```go
// Handler (Gin-aware) calls module (framework-agnostic)
func (h *Handler) StreamMedia(c *gin.Context) {
    h.streaming.ServeFile(c.Writer, c.Request, path)
}
```

Do NOT convert module methods to use `*gin.Context`. Only `api/handlers/` and `api/routes/` should import Gin.

### Handler Helpers

All handlers are methods on `*Handler` (defined in `api/handlers/handler.go`):

```go
getSession(c *gin.Context) *models.Session   // get session from gin context
getUser(c *gin.Context) *models.User         // get user from gin context
writeSuccess(c *gin.Context, data interface{})
writeError(c *gin.Context, status int, message string)
resolveMediaByID(c *gin.Context, id string) (string, bool)  // resolve media ID to absolute path
checkMatureAccess(c *gin.Context, item *models.MediaItem) bool  // check mature content permission
```

Wildcard path parameters include a leading slash — always trim: `strings.TrimPrefix(c.Param("path"), "/")`

### Configuration

Loaded in order (later overrides earlier): `config.json` → `.env` (generated by `setup.sh` or created manually)

Both `FEATURE_*` and `FEATURES_*` env var prefixes are accepted.

Key sections: `server`, `directories`, `database`, `streaming`, `features`, `security`, `auth`, `ui`, `admin`

### Database & Repository Pattern

MySQL is the sole persistence backend. The database module is critical — server won't start without it.

All repositories use **GORM** (`*gorm.DB`), accessed via `dbModule.GORM()`:

```
internal/repositories/
  ├── interfaces.go                        Repository contracts
  └── mysql/                               18 GORM implementations
      ├── user_repository_gorm.go
      ├── session_repository_gorm.go
      ├── media_metadata_repository.go
      ├── scan_result_repository.go
      ├── analytics_repository.go
      ├── audit_log_repository.go
      ├── playlist_repository.go
      ├── user_permissions_repository.go
      ├── user_preferences_repository.go
      ├── autodiscovery_repository.go
      ├── backup_manifest_repository.go
      ├── categorized_item_repository.go
      ├── hls_job_repository.go
      ├── ip_list_repository.go
      ├── receiver_transfer_repository.go
      ├── remote_cache_repository.go
      ├── suggestion_profile_repository.go
      └── validation_result_repository.go
```

Repository constructors take `*gorm.DB`: `mysql.NewUserRepository(dbModule.GORM())`

Migrations are handled via GORM auto-migration (no embedded SQL files).

### Master-Slave (Receiver) Architecture

The receiver module enables distributed media serving via WebSocket tunnels:

- **Master** (`internal/receiver/`): Accepts slave connections, stores catalog, proxies streams
- **Slave** (`cmd/media-receiver/`): Scans local dirs, connects to master via WebSocket, pushes catalog
- **WebSocket protocol**: JSON envelope `{type, data}` with message types: `register`, `catalog`, `heartbeat`,
  `stream_request`
- **Streaming**: WebSocket proxy first, HTTP POST fallback (`/api/receiver/stream-push/:token`)
- **Dedup**: Content fingerprint (SHA-256 of size + first/last 64KB) prevents duplicates across slaves
- **Auth**: Slaves authenticate via `X-API-Key` header or `api_key` query param

### Background Tasks

Registered in `cmd/server/main.go` → `registerTasks()`:

- **media-scan** (1h) — scans directories for new media, feeds catalog to suggestions
- **metadata-cleanup** (24h) — re-scans to prune orphaned entries
- **thumbnail-generation** (30min) — generates missing thumbnails
- **session-cleanup** (1h) — removes expired user sessions
- **backup-cleanup** (24h) — removes old backups beyond retention count (keeps 10)
- **mature-content-scan** (12h) — scans for mature content
- **health-check** (5min) — periodic health check log entry

```go
scheduler.RegisterTask("task-id", "Name", "Description", interval, func(ctx context.Context) error {
    return nil
})
```

### HLS Adaptive Streaming

HLS transcoding lives in `internal/hls/hls.go`. Key behaviors:

- **Quality profiles**: Defined in `config.json` under `quality_profiles`; selected via `HLS_QUALITIES` env var
- **Lazy transcoding** (`HLS_LAZY_TRANSCODE=true`): Only the first quality is transcoded upfront; remaining qualities are transcoded on-demand when `ServeVariantPlaylist` is called. Per-quality mutexes prevent duplicate concurrent transcodes.
- **CDN base URL** (`HLS_CDN_BASE_URL=https://cdn.example.com`): Rewrites all HLS master/variant playlist URLs to absolute CDN paths, enabling origin-pull CDN caching. Thumbnail `Cache-Control` is `public` (not `private`) when CDN mode is active.
- **Access-time cleanup**: `cleanupOldSegments` uses `LastAccessedAt` (persisted on `HLSJob`) to determine staleness, falling back to `ModTime`. Running/pending jobs are never cleaned up.
- **`HLSJob` model** has `LastAccessedAt *time.Time` — auto-migrated via GORM.

## Build Tags

Platform-specific code in `internal/server/`: `signals_windows.go` / `signals_unix.go`

## Common Patterns

### Adding a New Module

1. Create package in `internal/mymodule/`
2. Implement `server.Module` interface (Name, Start, Stop, Health)
3. Register in `cmd/server/main.go` — create instance, add to modules slice, add to `criticalModules` if critical
4. Add field to `HandlerDeps` in `api/handlers/handler.go` if handlers need access

### Adding a New API Endpoint

1. Add handler method on `*Handler` in `api/handlers/`
2. Register route in `api/routes/routes.go` under the appropriate group:
   - Public routes (no auth) — under `api := r.Group("/api")`
   - Authenticated routes — under group with `requireAuth()` middleware
   - Admin routes — under group with `adminAuth()` middleware

### Adding a New Repository

1. Define interface in `internal/repositories/interfaces.go`
2. Implement in `internal/repositories/mysql/` using `*gorm.DB`
3. Use unexported GORM model structs (e.g., `mediaMetadataRow`) to decouple DB schema from domain models
4. Construct with `dbModule.GORM()` in `cmd/server/main.go` and inject via `HandlerDeps`

## Module Constructor Signatures

Modules that require database access take `(cfg, dbModule)`. Others take `(cfg)` only.

**With database** (`cfg, dbModule`):
`security`, `auth`, `media`, `scanner`, `hls`, `analytics`, `playlist`, `admin`, `validator`, `backup`,
`autodiscovery`, `suggestions`, `categorizer`, `remote`, `receiver`

**Without database** (`cfg` only):
`database` (is the database), `streaming`, `tasks`, `thumbnails`, `upload`

**Special**: `updater.NewModule(cfg, Version)` — takes version string instead of dbModule

**Error returns**: `auth`, `media`, `analytics`, `playlist`, `admin`, `scanner` return `(*Module, error)`.
All others return `*Module`.

## Frontend

### Structure
- `web/frontend/src/App.tsx` — routes + code splitting (React.lazy)
- `web/frontend/src/api/client.ts` — typed API client (unwraps Go JSON envelope)
- `web/frontend/src/api/endpoints.ts` — 16 API modules with typed functions
- `web/frontend/src/api/types.ts` — TypeScript interfaces matching Go JSON responses

### Pages (in `web/frontend/src/pages/`)
- `IndexPage` (`/`) — media grid, search, filters, upload
- `LoginPage` (`/login`, `/admin-login`) — shared login
- `SignupPage` (`/signup`) — registration
- `ProfilePage` (`/profile`) — user prefs, watch history, playlists
- `PlayerPage` (`/player`) — video/audio player, HLS, equalizer
- `AdminPage` (`/admin`) — 10 main tabs + subtabs (Dashboard, Users, Media, Streaming, Analytics, Content, Sources, Playlists, Security, Updates)

### State & Hooks
- **Stores** (Zustand): `authStore`, `playbackStore`, `playlistStore`, `settingsStore`, `themeStore`
- **Hooks**: `useHLS` (hls.js), `useEqualizer` (audio EQ), `useMediaPosition` (playback tracking)

### Components
`AgeGate`, `AudioPlayer`, `EqualizerPanel`, `ErrorBoundary` (+ `SectionErrorBoundary`), `RequireAuth`, `Toast`

## Common Issues

- **Port binding failed**: Use `SERVER_PORT=8080` or higher to avoid permission issues
- **Build fails with "undefined" errors**: Use `go build ./cmd/server` not `go build cmd/server/main.go`
- **Thumbnails/HLS not working**: Ensure ffmpeg/ffprobe are in PATH
- **Database connection failed**: Check MySQL is running, verify credentials in `.env`
- **Linux cross-compile fails with CGO errors**: Use `CGO_ENABLED=0`
