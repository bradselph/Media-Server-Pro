# TODO count (as of last audit)

Rough count of `// TODO` comments left in the codebase after the recent fix pass.

| Area | Approx. count |
|------|----------------|
| **Go (internal/)** | ~90 |
| **Go (api/, cmd/, pkg/)** | ~35 |
| **Frontend (TS/TSX)** | ~12 |
| **Config (vite, tsconfig)** | ~3 |
| **Total** | **~140** |

Heaviest files: `cmd/media-receiver/main.go` (~20), `cmd/server/main.go` (~12), `internal/remote/remote.go` (6), `internal/admin/admin.go` (4), `internal/backup/backup.go` (4), `internal/updater/updater.go` (4), `internal/streaming/streaming.go` (4), `internal/extractor/extractor.go` (5), `internal/receiver/receiver.go` (5), `internal/media/discovery.go` (5), `api/routes/routes.go` (5).

Categories: bugs, performance (N+1, unbounded memory), incomplete features, API contract notes, security/SSRF, race conditions, dead code. Run `grep -r "// TODO" --include="*.go" --include="*.ts" --include="*.tsx" .` to refresh.
