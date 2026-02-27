# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Media Server Pro is a modular, fault-tolerant media streaming server written in Go with a Gin HTTP framework. It
provides video/audio streaming with HLS adaptive streaming, thumbnail generation, analytics, user authentication,
mature content scanning, and an admin panel. The frontend is a React 19 + TypeScript + Vite SPA served from
`web/static/react/`.

## Build and Run Commands

```bash
# Build (Windows / Linux)
go build -o server.exe ./cmd/server     # Windows
go build -o server ./cmd/server         # Linux/Mac

# IMPORTANT: Always use ./cmd/server (package path), NOT cmd/server/main.go
# The latter misses platform-specific files (diskspace_windows.go, etc.)

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

### Auxiliary Tools

```bash
# HLS pre-generation (batch generate HLS before runtime)
go build -o hls-pregenerate.exe ./cmd/hls-pregenerate
./hls-pregenerate.exe -workers 4 -skip-done=true

# Offline diagnostics (run while server is stopped)
go build -o media-doctor.exe ./cmd/media-doctor
./media-doctor.exe -fix -check-db -check-media -verbose
```

Both tools share the same config loading (`config.json` + `.env`) and HLS cache directory as the server.

### Frontend

```bash
cd web/frontend && npm run build    # builds to web/static/react/
./run.sh                            # builds React + Go, runs server
./run.sh --no-react                 # skip React build
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
`backup`, `autodiscovery`, `suggestions`, `categorizer`, `updater`, `remote`

### Directory Structure

```
cmd/server/              Entry point (main.go + platform-specific files)
cmd/hls-pregenerate/     HLS batch generation tool
cmd/media-doctor/        Offline diagnostic tool
internal/                Internal packages (all modules)
  ├── config/            Configuration management
  ├── database/          MySQL connection, migrations, sql/ embedded migrations
  ├── repositories/      Repository pattern for data access
  │   ├── interfaces.go  Repository contracts
  │   └── mysql/         GORM implementations (all repos use *gorm.DB)
  ├── auth/              Authentication & sessions
  ├── media/             Media library management
  ├── server/            Gin engine setup
  └── [20+ modules]/     hls, streaming, security, tasks, thumbnails, etc.
api/
  ├── handlers/          Gin HTTP handlers (22 domain files)
  │   └── handler.go     Handler struct, HandlerDeps, helper functions
  └── routes/            Route registration (routes.go — 90+ routes)
pkg/
  ├── helpers/           Utility functions
  ├── middleware/        HTTP middleware (Gin + standard net/http variants)
  └── models/            Shared data models
web/
  ├── frontend/          React 19 + TypeScript + Vite source
  │   └── src/           Pages, hooks, stores, API client
  ├── static/            Embedded assets (react/ bundle, other assets)
  └── server.go          Static file serving (go:embed)
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
```

Wildcard path parameters include a leading slash — always trim: `strings.TrimPrefix(c.Param("path"), "/")`

### Configuration

Loaded in order (later overrides earlier): `config.json` → `.env` (use `.env.example` as template)

Key sections: `server`, `directories`, `database`, `streaming`, `features`, `security`, `auth`, `ui`, `admin`

### Database & Repository Pattern

MySQL is the sole persistence backend. The database module is critical — server won't start without it.

All repositories use **GORM** (`*gorm.DB`), accessed via `dbModule.GORM()`:

```
internal/repositories/
  ├── interfaces.go                    Repository contracts
  └── mysql/
      ├── user_repository_gorm.go      Uses *gorm.DB
      ├── session_repository_gorm.go   Uses *gorm.DB
      ├── media_metadata_repository.go
      ├── scan_result_repository.go
      ├── analytics_repository.go
      ├── audit_log_repository.go
      ├── playlist_repository.go
      ├── user_permissions_repository.go
      └── user_preferences_repository.go
```

Repository constructors take `*gorm.DB`: `mysql.NewUserRepository(dbModule.GORM())`

Migrations use embedded SQL files in `internal/database/sql/` and raw `*sql.DB` via `dbModule.DB()`.

### Background Tasks

Registered in `cmd/server/main.go` → `registerTasks()`:

- **media-scan** (1h) — scans directories for new media
- **metadata-cleanup** (24h) — removes metadata for deleted files
- **thumbnail-generation** (30min) — generates missing thumbnails
- **mature-content-scan** (12h) — scans for mature content
- **health-check** (5min) — system diagnostics and disk space

```go
scheduler.RegisterTask("task-id", "Name", "Description", interval, func(ctx context.Context) error {
    return nil
})
```

## Build Tags

Platform-specific code: `diskspace_windows.go` / `diskspace_unix.go`, `signals_windows.go` / `signals_unix.go`

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

## Frontend

Key files:
- `web/frontend/src/App.tsx` — routes + code splitting (React.lazy)
- `web/frontend/src/api/endpoints.ts` — all typed API functions
- `web/frontend/src/api/types.ts` — TypeScript types matching Go JSON responses
- `web/frontend/src/stores/` — Zustand state (auth, media, playback, settings, theme)
- `web/frontend/src/hooks/useHLS.ts` — hls.js integration

## Common Issues

- **Port binding failed**: Use `SERVER_PORT=8080` or higher to avoid permission issues
- **Build fails with "undefined: getDiskUsage"**: Use `go build ./cmd/server` not `go build cmd/server/main.go`
- **Thumbnails/HLS not working**: Ensure ffmpeg/ffprobe are in PATH
- **Database connection failed**: Check MySQL is running, verify credentials in `.env`
- **Linux cross-compile fails with CGO errors**: Use `CGO_ENABLED=0`
