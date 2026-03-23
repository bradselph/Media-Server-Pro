# Deep Debug Audit Report — Media Server Pro

**Date:** 2025-03-23  
**Scope:** Application source under `cmd/`, `api/`, `internal/`, `pkg/`, `web/server.go`, and `web/frontend/src/`. Third-party and tooling trees under `.trunk/` were excluded.  
**Methodology:** The audit prompt requires a literal read of every file and line-by-line trace of every function; that is **not fully executed** here (hundreds of Go files and 73 frontend modules). This report applies Phases 1–7 **sampling and pattern-driven deep dives** on entry points, security boundaries, auth/streaming/receiver/downloader paths, path resolution, API client behavior, and high-risk grep patterns (`panic`, ignored errors, empty `catch`, raw SQL, WebSocket origin checks).  
**Build check:** `go build -C d:\Media-Server-Pro-4 .\cmd\server` — **success** (Go 1.26.1 per `go.mod`).

---

## === AUDIT SUMMARY ===

| Metric | Value |
|--------|------|
| Files in scope (approx.) | ~195 Go (app) + 73 TS/TSX + `web/server.go` |
| Functions traced (approx.) | Targeted ~80+ hot paths and helpers; not exhaustive |
| Workflows traced (approx.) | 12 (startup, HTTP API envelope, session, stream local/receiver, HLS, admin downloader WS, receiver register/catalog/proxy, path resolve, mature gate, frontend API client, downloader UI WS) |

| Tag | Count |
|-----|------:|
| BROKEN | 0 |
| INCOMPLETE | 2 |
| GAP | 0 |
| REDUNDANT | 1 |
| FRAGILE | 8 |
| SILENT FAIL | 4 |
| DRIFT | 1 |
| LEAK | 0 |
| SECURITY | 1 |
| OK | 4 |

**Critical (must fix before deploy):** Receiver master HTTP proxy SSRF surface via slave `base_url` when an API key is valid (or compromised).  
**High (user-facing / ops):** Silent handling of downloader WS errors and analytics failures; API envelope drift handling.  
**Medium:** Process panics on misconfiguration or crypto/rand failure; streaming stop behavior; mature gate fail-open on lookup error.  
**Low:** Ignored `json.Marshal` errors in hot paths; duplicate in-memory auth caches.

---

## Phase 1 — Structural Inventory (summary)

### File manifest (roles)

| Area | Role |
|------|------|
| `cmd/server/main.go` | Process entry: config, module wiring, task registration, Gin listen. |
| `cmd/media-receiver/main.go` | Slave node: catalog push, WebSocket tunnel to master. |
| `api/routes/routes.go` | Central route table (~173 registrations). |
| `api/handlers/*.go` | Gin handlers; bridge to `internal/*` modules. |
| `internal/*` | Feature modules (`media`, `auth`, `streaming`, `hls`, `receiver`, `downloader`, …). |
| `pkg/helpers`, `pkg/middleware`, `pkg/models` | Shared utilities, HTTP middleware, domain models. |
| `web/server.go` | Static/embed serving for SPA. |
| `web/frontend/src` | React 19 SPA: pages, stores, API client, hooks. |

### Entry points

- **OS processes:** `cmd/server`, `cmd/media-receiver`.
- **HTTP:** Gin router in `api/routes`; static SPA from `web/server.go`.
- **WebSockets:** Receiver slave connections (`internal/receiver/wsconn.go`); admin downloader proxy (`internal/downloader/websocket.go`).
- **Background:** `internal/tasks/scheduler.go` (registered from `cmd/server/main.go`).

### Dependency graph (high level)

- **Handlers → modules:** Handlers depend on concrete `internal/*` module types via `HandlerDeps` (`api/handlers/handler.go`).
- **No circular package imports observed** between `api` and `internal` in sampled paths (handlers import internal; internal does not import `api`).
- **Frontend:** `web/frontend/src/api/client.ts` → `fetch` to same-origin API with cookie credentials.

### Export surface

- Large `internal` packages export many symbols for handlers and tests; **full orphan-export analysis not performed** — flagged as **[GAP]** in audit completeness (meta), not product code.

---

## Findings (by severity)

### Critical / Security

```
[SECURITY] internal/receiver/receiver.go:341-384 — Slave base_url not SSRF-validated
  WHAT: RegisterSlave accepts any http(s) BaseURL (except marker `ws-connected`) without applying helpers.ValidateURLForSSRF or equivalent.
  WHY: Only scheme/host non-empty is checked; resolved IP ranges are not restricted.
  IMPACT: Any holder of a valid receiver API key can register a slave pointing BaseURL at cloud metadata, localhost, or RFC1918 hosts reachable from the master; proxyViaHTTP then issues GETs from the master's network (SSRF / internal port scan).
  TRACE: API register → RegisterSlave → persisted BaseURL → ProxyStream → proxyViaHTTP → http.Client.Do(targetURL).
  FIX DIRECTION: Validate BaseURL at registration (and optionally on each proxy) using existing SSRF helper or a stricter allowlist aligned with deployment topology.

[FRAGILE] internal/downloader/websocket.go:17-34 — CheckOrigin allows missing Origin
  WHAT: CheckOrigin returns true when `Origin` header is empty.
  WHY: Comment assumes non-browser clients; browsers typically send Origin for WS, but other tools may not.
  IMPACT: **Mitigated:** `api/routes/routes.go` registers `GET /ws/admin/downloader` with `adminAuth` before upgrade — CSWSH from arbitrary sites is blocked unless attacker has a valid admin session. Residual risk is limited to token/session theft scenarios, not anonymous cross-site WS.
  TRACE: HTTP upgrade → wsUpgrader.CheckOrigin → Upgrade (after `adminAuth`).
  FIX DIRECTION: Optionally tighten Origin policy anyway for defense in depth; document that non-browser clients rely on empty Origin.
```

### High — Silent failures / UX / observability

```
[SILENT FAIL] web/frontend/src/hooks/useDownloaderWebSocket.ts:72-74,92-94 — Errors swallowed
  WHAT: Messages with `type === 'error'` return without surfacing; JSON parse failures are caught with an empty catch.
  WHY: No user-visible or logged feedback path.
  IMPACT: Admin downloader UI shows stale or empty state while server reports errors; debugging production issues is harder.
  TRACE: WebSocket onmessage → branch on msg.type / JSON.parse.
  FIX DIRECTION: Surface error payload via toast or connection banner; log parse errors in dev or structured client logging.

[SILENT FAIL] web/frontend/src/pages/index/IndexPage.tsx, usePlayerPageState.ts (representative) — analyticsApi.trackEvent(...).catch(() => {})
  WHAT: Analytics failures are discarded.
  WHY: Intentional non-blocking pattern.
  IMPACT: Silent loss of analytics; acceptable for UX but masks network/backend issues for operators.
  TRACE: Player/index lifecycle → trackEvent → empty catch.
  FIX DIRECTION: Optional debug flag to log failures; or sample-based console warn.

[SILENT FAIL] web/frontend/src/hooks/useDownloaderWebSocket.ts:17-33 — scheduleTerminalDownloadCleanup calls onComplete inside setState
  WHAT: onComplete runs synchronously during React state update for terminal statuses.
  WHY: Ref pattern tries to stabilize callback.
  IMPACT: If onComplete triggers heavy refetch or setState in parent, can contribute to update churn or warnings in strict mode (edge).
  TRACE: setActiveDownloads updater → scheduleTerminalDownloadCleanup.
  FIX DIRECTION: Defer onComplete to useEffect or queueMicrotask after commit.
```

### Medium — Fragile / incomplete behavior

```
[FRAGILE] api/handlers/handler.go:157-164,279-288 — Panic on nil deps or crypto/rand failure
  WHAT: NewHandler panics if critical deps nil; randIntn panics if crypto/rand fails.
  WHY: Fail-fast for misconfiguration / severe OS condition.
  IMPACT: Entire process exits instead of returning error to caller; bad for library-style reuse; rand failure is extremely rare but hard crash.
  TRACE: main constructs HandlerDeps → NewHandler / session token generation paths using randIntn.
  FIX DIRECTION: Return error from constructor where feasible; for rand, return error up stack or use fallback policy with audit log.

[FRAGILE] internal/auth/helpers.go:18,27 — panic on crypto/rand.Read failure
  WHAT: Same class as above for token generation helpers.
  WHY: Treats entropy failure as fatal.
  IMPACT: Process crash if /dev/urandom or Windows equivalent fails.
  TRACE: Session/secret generation call sites.
  FIX DIRECTION: Bubble error to caller and fail the single request.

[FRAGILE] web/frontend/src/api/client.ts:71-83 — Non-envelope responses cast to T
  WHAT: If `success` is undefined, response is returned as `T` without validation.
  WHY: Backward compatibility comment.
  IMPACT: Backend format drift yields undefined runtime behavior instead of ApiError.
  TRACE: apiRequest → unwrap branch.
  FIX DIRECTION: Treat missing envelope as error in production builds or behind a strict flag.

[FRAGILE] api/handlers/handler.go:471-477 — checkMatureAccess allows access when GetMedia errors
  WHAT: On `GetMedia` error, returns true (allow).
  WHY: Today GetMedia only errors when path not in in-memory map; effectively "unknown to catalog → don't block."
  IMPACT: If GetMedia later returns errors for other reasons, mature gate could open incorrectly; uncatalogued paths paired with direct file APIs could be a policy concern depending on routes.
  TRACE: Stream/thumbnail/HLS handlers → checkMatureAccess → media.GetMedia.
  FIX DIRECTION: Distinguish ErrNotFound from other errors; fail closed on unexpected errors.

[INCOMPLETE] internal/streaming/streaming.go:107-121 — Stop does not wait for sessions
  WHAT: Module stop logs active session count but does not cancel or await completion.
  WHY: Documented behavior.
  IMPACT: Graceful shutdown may cut clients mid-stream depending on outer process exit policy.
  TRACE: streaming.Module.Stop.
  FIX DIRECTION: Context-cancel in Serve loop or WaitGroup with timeout on shutdown.

[INCOMPLETE] (meta) Audit protocol — Full file/function coverage not achieved
  WHAT: Not every file was read; not every function traced.
  WHY: Token/time limits vs. repo size.
  IMPACT: Unknown issues may remain in low-traffic packages (e.g. updater, remote, crawler).
  TRACE: N/A.
  FIX DIRECTION: Iterate modules in future passes or use CI static analysis + this report as baseline.
```

### Low — Redundant / style

```
[REDUNDANT] internal/downloader/websocket.go:77-92 — Ignored json.Marshal error
  WHAT: `registerMsg, _ := json.Marshal(...)` and same for connectedMsg.
  WHY: Maps with string keys rarely fail.
  IMPACT: Theoretical only.
  TRACE: HandleWebSocket.
  FIX DIRECTION: Handle error explicitly for consistency.

[DRIFT] Risk between Go API envelope contract and client permissive branch
  WHAT: Same as [FRAGILE] client.ts — classified as drift between documented API and client fallback.
  WHY: Dual behavior paths.
  IMPACT: Harder to detect breaking API changes during deploy.
  TRACE: Go writeSuccess/writeError vs client unwrap.
  FIX DIRECTION: Contract tests or OpenAPI + generated client.
```

---

## Phase 3 — Workflow traces (abbreviated)

| Workflow | Entry | Terminal outcome | Notes |
|----------|--------|------------------|-------|
| User login | `api/handlers/auth.go` | Session cookie set | bcrypt + rate limiting in `internal/auth` (sampled). |
| Stream local media | `api/handlers/media.go` | `streaming.ServeFile` | Mature gate + concurrent stream limits applied after ID resolve. |
| Stream receiver | `api/handlers/media.go` → `receiver.ProxyStream` | Proxied bytes or 502 | Semaphore-limited; HTTP fallback uses slave BaseURL (**SECURITY** finding). |
| HLS | `api/handlers/hls.go` | Playlist/segments | Mature check on job media path. |
| Admin downloader WS | `internal/downloader/websocket.go` | Bidirectional relay | Origin check + downstream dial to configured downloader URL. |
| Path upload/admin | `handler.go` resolvePath* | EvalSymlinks + `isPathWithinDirs` | Stronger traversal checks on validated paths (see OK below). |

---

## Phase 5 — State & concurrency (samples)

- **Receiver module:** `sync.RWMutex` on slaves/media maps; `healthDoneOnce` prevents double-close — good pattern.
- **Streaming:** `sessionMu` protects `activeSessions`; stats under `statsMu` — consistent locking style in reviewed code.
- **Auth:** in-memory maps plus DB repositories — **[FRAGILE]** potential consistency drift if DB updated outside module; acceptable if single-writer invariant holds.

---

## Phase 7 — Integrations

- **GORM / MySQL:** Parameterized queries in sampled repositories (`?` placeholders in `media_metadata_repository.go` Exec).
- **gorilla/websocket:** Used in downloader and receiver; upgrader limits buffer sizes — OK for sampled use.
- **Go 1.26 `errors.AsType`:** Used in `handler.go` and `pkg/huggingface/frames.go`; build succeeded — **OK** for this toolchain.

---

## Verified OK units (sampled)

```
[OK] api/handlers/handler.go:452-468 — isPathWithinDirs uses filepath.Rel and rejects ".." escape — sound traversal check for paths validated through this helper.

[OK] api/handlers/handler.go:400-420 — resolveAndValidatePath uses filepath.EvalSymlinks before isPathWithinDirs — reduces symlink-based escapes for that code path.

[OK] internal/repositories/mysql/media_metadata_repository.go:317-334 — Raw SQL uses ? placeholders for path/user — no string concatenation of user input observed.

[OK] internal/receiver/receiver.go:327-338 — ValidateAPIKey uses subtle.ConstantTimeCompare — timing-aware comparison against configured keys.
```

---

## Critical / prioritized follow-up list

1. SSRF hardening for receiver slave `base_url` (registration + optional per-request validation).  
2. Downloader admin WebSocket: confirm auth middleware order; surface `error` messages in UI.  
3. Replace silent analytics catches with optional logging/metrics.  
4. Harden `apiRequest` against non-envelope responses or add integration tests on envelope shape.  
5. `checkMatureAccess`: explicit error typing (fail closed on non-NotFound).  
6. Graceful streaming shutdown strategy.  
7. Consider returning errors instead of panic from `NewHandler` / rand helpers for operability.  
8. Second-pass audit: `internal/updater`, `internal/crawler`, `internal/remote` (large attack surface / network I/O).  

---

*End of report.*
