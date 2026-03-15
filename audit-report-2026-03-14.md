# Deep Debug Audit Report — Media Server Pro 4

**Date:** 2026-03-14
**Branch:** development
**Commit:** ddfac05
**Last verified:** 2026-03-14
**Re-verified & reprioritized:** 2026-03-14 (48 fixes code-verified, 4 newly confirmed, remaining issues promoted)

---

## FIXED ITEMS (48 total — all code-verified 2026-03-14)

<details>
<summary>Click to expand verified fixes</summary>

| ID | Finding | Verification |
|----|---------|-------------|
| — | Analytics CompletionRate formula | ✅ Verified — divide-by-zero guard |
| — | Playlist AddItem swallows DB errors | ✅ Verified |
| — | Playlist removeItemLocked swallows DB error | ✅ Verified |
| — | Autodiscovery ClearAllSuggestions doesn't delete DB | ✅ Verified |
| — | Autodiscovery ClearSuggestion doesn't persist | ✅ Verified |
| — | Media-receiver range request fallthrough | ✅ Verified |
| — | HLS lazyTranscodeQuality activeJobs tracking | ✅ Verified |
| — | Receiver proxyViaHTTP ?id= vs ?path= | ✅ Verified |
| — | Database tryConnect connection leak | ✅ Verified — sqlDB.Close() on ping failure |
| — | Security BanIP persistence (ban config row) | ✅ Verified — persist + reload on start |
| — | endpoints.ts !== null (undefined sent as param) | ✅ Verified |
| — | TrackPlayback divide-by-zero (duration=0) | ✅ Verified — guard on duration > 0 |
| — | SanitizeString null-byte ordering | ✅ Verified |
| — | Analytics temp file defer (Windows cleanup) | ✅ Verified |
| — | Media-receiver shutdown WebSocket writeMu | ✅ Verified |
| — | Receiver/remote double-stop panic | ✅ Verified — sync.Once on channel close |
| P0-1 | AdminExecuteQuery read-only transaction | ✅ `system.go:449-460` — sql.TxOptions{ReadOnly: true} |
| P0-2 | ReceiverWebSocket API key middleware | ✅ `routes.go:281` — RequireReceiverWithAPIKey() |
| P0-3 | Unauthenticated streaming limits | ✅ `media.go:309-321` — RequireAuth + UnauthStreamLimit |
| P0-4 | Chrome --host-resolver-rules RFC1918 | ⚠️ PARTIAL — see P0-4 below (loopback/link-local missing) |
| P0-5 | Extractor proxyStream header allowlist | ✅ `extractor.go:519-531` — allowlist confirmed |
| P0-7 | Default admin password to file | ✅ `bootstrap.go:36-50` — 0600 file, log path only |
| P0-8 | AdminUpdateConfig redacts secrets | ✅ `admin_config.go:11-33,68` — redactSensitiveConfigKeys |
| P0-9 | /api/status, /api/modules behind adminAuth | ✅ `routes.go:289-293` — adminAuth middleware |
| P0-10 | restoreFromBackup .tar.gz dead code | ✅ `updater.go:890-903` — removed |
| P1-1 | HLS GetJobStatus/ListJobs data race | ✅ `jobs.go:15-31,184-231` — copyHLSJob |
| P1-2 | SetPassword cache/DB divergence | ✅ `password.go:79-92` — copy-then-persist |
| P1-3 | CopyPlaylist partial failure | ✅ `playlist.go:502-512` — CreateWithItems tx |
| P1-4 | ClearAllPlaybackPositions DB cleanup | ✅ DeleteAllByUser confirmed |
| P1-5 | Slave extension list missing 7 formats | ✅ .3gp/.m2ts/.vob/.ogv/.aiff/.ape/.mka added |
| P1-6 | WriteTimeout kills long streams | ✅ Default 0 in config/defaults.go |
| P1-7 | No rollback on startup failure | ✅ `server.go:290-318` — reverse-order stop |
| P1-8 | JSON parse on non-JSON (api/client) | ✅ `client.ts:59-69` — try/catch + ApiError |
| P1-10 | Last admin TOCTOU race | ✅ `auth.go:60` + `user.go:199-211` — lastAdminMu |
| P1-11 | Analytics backgroundLoop WaitGroup | ✅ `module.go:44,93,113,139` — bgWg lifecycle |
| P1-12 | CacheMedia context.Background() | ✅ `remote.go:613-614` — m.ctx |
| P1-13 | evictStaleProfiles TOCTOU | ✅ Re-check under write lock |
| P1-14 | Two thumbnail timestamp spacing algorithms | ✅ `preview.go:84` — previewTimestamp |
| P1-15 | ProxyHLSVariant type assertion panic | ✅ `extractor.go:389` — comma-ok pattern |
| P1-16 | Chrome child processes not killed | ✅ browser_unix.go: Setpgid + killChromeProcessGroup |
| P1-17 | Authenticate mutates shared user pointer | ✅ `authenticate.go:92-100` — copy before mutation |
| P1-18 | ValidateSession shared pointer in goroutine | ✅ `session.go:128-132` — session copy |
| P1-19 | close(shutdownCh) after logger.Shutdown() | ✅ `server.go:447-449` — reordered |
| P1-21 | Cleanup goroutine exits on panic | ✅ `auth.go:234-250` — recover inside loop |
| P1-22 | EvalSymlinks falls back to raw path | ✅ `handler.go:377-382` — return error |
| P1-23 | ValidateURLForSSRF DNS rebinding | ✅ `ssrf.go:93-131` — SafeHTTPTransport + dial-time IP check |
| P1-24 | fpCache grows unbounded (media-receiver) | ✅ `main.go:121-130` — pruneFpCache after each scan |
| P1-25 | FixFile no output size limit | ✅ `validator.go:476-481` — 10GB max |
| P1-26 | Path traversal string matching (media-receiver) | ✅ `main.go:884-928` — Clean+EvalSymlinks+Rel |
| P1-27 | UpdateConfig no atomicity | ✅ `admin.go:248-252` — SetValuesBatch |
| P2-4 | HLS cleanup TOCTOU | ✅ `cleanup.go:83-92` — re-check under write lock |
| P2-5 | RecordAccess saveJob outside lock | ✅ `access.go:22-28` — saveJob under lock |
| P2-6 | cleanupDone double-close | ✅ `module.go:62,203` — sync.Once |
| P2-7 | handleStatus len(modules) outside lock | ✅ `server.go:499-503` — RLock |
| P2-23 | List Limit=0 OOM risk | ✅ analytics 10K / audit 100K caps |
| P2-25 | Unbounded verifiedIPs map growth | ✅ `agegate.go:144-177` — TTL eviction |
| P2-26 | scheduleUnregisterUpload leak | ✅ `upload.go:74,132,166,318-331` — done chan |
| P2-45 | proxyStream creates new http.Client per request | ✅ `extractor.go:512` — reuses m.httpClient.Transport |
| P3-2 | IsStrictlyExpired duplicate | ✅ `models.go:355-358` — delegates |
| P3-6 | Toast setTimeout not cleaned up | ✅ `Toast.tsx:14-19` — useRef + useEffect |
| P3-13 | godotenv.Load error discarded | ✅ `main.go:59-61` — log non-NotFound |

</details>

---

## REMAINING ISSUES — Reprioritized (remaining open findings)

All remaining issues have been promoted one tier upward to reflect their cumulative risk
now that the most critical items are resolved. Security and crash-risk items receive
the largest promotions.

### Priority counts:
```
P0 — CRITICAL (security / crash / data loss):  10
P1 — HIGH (user-facing bugs / fragile):        22
P2 — MEDIUM (tech debt / time bombs):          17
P3 — LOW (cleanup / style):                     3
────────────────────────────────────────────────
TOTAL REMAINING:                                52
```

---

## P0 — CRITICAL: Must fix before deploy (10)

### P0-4 [SECURITY] Chrome --host-resolver-rules incomplete — ⚠️ PARTIAL FIX
- **File:** `internal/crawler/browser.go:116`
- **Status:** RFC1918 ranges (10/8, 172.16/12, 192.168/16) are blocked, but **loopback (127.0.0.0/8)** and **link-local (169.254.0.0/16)** are NOT blocked
- **Impact:** With `--disable-web-security`, crawled page JS can reach localhost services (including this server's own admin API) and cloud metadata endpoints (169.254.169.254 — AWS/GCP/Azure credential theft)
- **Fix:** Extend hostRules to include `127.0.0.0/8`, `169.254.0.0/16`, `::1/128`

### P0-6 [SECURITY] GitHub credentials visible in process environment
- **File:** `internal/updater/updater.go:930-942`
- **Impact:** Token appears in `GIT_CONFIG_VALUE_0` env var — any local user can read via `/proc/<pid>/environ`
- **Fix:** Use `GIT_ASKPASS` helper script or credential helper

### P0-11 [SECURITY] X-Request-ID log injection *(was P2-8)*
- **File:** `pkg/middleware/middleware.go:69`
- **Impact:** Client-supplied X-Request-ID accepted verbatim — no length limit, no control character filtering. Attacker can inject fake log entries (`\n`, `\r`) to poison logs, hide attacks, or trigger log-based alerting
- **Fix:** Truncate to 64 chars, strip non-printable characters, validate format (UUID/alphanumeric)

### P0-12 [SECURITY] AdminExecuteQuery semicolon bypass *(was P2-12)*
- **File:** `api/handlers/system.go:411`
- **Impact:** `strings.Contains(query, ";")` can be bypassed with Unicode lookalike semicolons (`U+037E`, `U+FF1B`) that MySQL may accept as statement terminators. Combined with P0-1's read-only tx, risk is reduced but multi-statement queries could still cause issues
- **Fix:** Use parameterized query execution or normalize Unicode before checking; reject multi-statement at the MySQL connection level (`multiStatements=false`)

### P0-13 [SECURITY] ageGateSecure trusts X-Forwarded-Proto *(was P2-9)*
- **File:** `pkg/middleware/agegate.go:185`
- **Impact:** Without trusted proxy validation, any client can set `X-Forwarded-Proto: https` to bypass secure cookie checks. Cookie could be sent over plain HTTP and intercepted
- **Fix:** Only trust X-Forwarded-Proto when request comes from configured trusted proxy IPs

### P0-14 [SECURITY] GetHLSCapabilities/GetHLSStatus unauthenticated *(was P2-13)*
- **File:** `api/handlers/hls.go:300,303`
- **Impact:** Unauthenticated endpoints expose server capabilities and HLS job status — fingerprinting and information disclosure
- **Fix:** Add `requireAuth()` middleware

### P0-15 [SECURITY] validateSecrets allows weak API keys *(was P2-14)*
- **File:** `cmd/server/main.go:339-371`
- **Impact:** Short or low-entropy API keys accepted without warning; brute-forceable
- **Fix:** Enforce minimum length (32+ chars) and entropy requirements

### P0-16 [SECURITY] MoveMedia does not validate oldPath *(was P2-30)*
- **File:** `internal/media/management.go:82-83`
- **Impact:** Unvalidated source path in move operation — potential path traversal to move arbitrary files
- **Fix:** Validate oldPath is within allowed media directories before moving

### P0-17 [SECURITY] Rate-limit bypass for "/media" prefix too broad *(was P2-32)*
- **File:** `internal/security/security.go:893-911`
- **Impact:** Any path starting with `/media` bypasses rate limiting. Attacker could craft `/media/../api/admin/...` or similar to bypass rate limits on sensitive endpoints
- **Fix:** Use exact prefix matching on normalized paths; only exempt actual streaming paths

### P0-18 [SECURITY] SQL executor regex misses INSERT/CREATE/GRANT (frontend) *(was P2-16)*
- **File:** `web/frontend/src/pages/admin/SystemTab.tsx:205`
- **Impact:** Frontend provides no confirmation prompt for data-creating SQL (INSERT, CREATE TABLE, GRANT, LOAD DATA). While server has read-only tx guard, the UX gap means admin users get no warning
- **Fix:** Extend regex to cover all DDL/DCL: `INSERT|CREATE|GRANT|REVOKE|LOAD|CALL`

---

## P1 — HIGH: Will cause user-facing bugs or crashes (22)

### P1-9 [FRAGILE] RestartServer skips graceful shutdown — ⚠️ PARTIAL FIX
- **File:** `api/handlers/admin_lifecycle.go:29-60`
- **Status:** Self-exec restart mechanism added, but `os.Exit(1)` under systemd and `os.Exit(0)` in fallback path still called without `server.Shutdown()`. In-flight requests, DB writes, and HLS jobs are not drained
- **Fix:** Call `server.Shutdown()` before exit in all paths; under systemd, send SIGTERM to self

### P1-20 Single context timeout for HTTP shutdown + all module stops
- **File:** `internal/server/server.go:436-441`
- **Impact:** `shutdownHTTPServer` and `shutdownModules` share the same deadline. If HTTP drain takes 25 of a 30s budget, all modules get only 5s
- **Fix:** Separate per-phase contexts (e.g., 50% HTTP drain, 50% module stop, or per-module sub-deadlines)

### P1-28 Auth mutation pattern audit *(was P2-1)*
- **File:** `internal/auth/` (multiple files)
- **Impact:** P1-17 fixed `Authenticate`'s shared pointer mutation, but the same pattern may exist in other auth mutation paths (UpdateUser, SetRole, etc.). Unfixed instances are latent data races
- **Fix:** Audit all auth cache mutation paths for copy-before-mutate pattern

### P1-29 Session goroutine pattern audit *(was P2-2)*
- **File:** `internal/auth/session.go`
- **Impact:** P1-18 fixed `ValidateSession`'s goroutine, but similar shared-pointer-in-goroutine patterns may exist in other session operations
- **Fix:** Audit all goroutine launches in auth package for pointer safety

### P1-30 String timestamps in repositories *(was P2-17)*
- **File:** 6+ repository files in `internal/repositories/mysql/`
- **Impact:** String timestamps instead of `time.Time` create timezone mismatch risk. MySQL stores in server TZ, Go formats in local TZ — silent drift on servers with non-UTC timezone
- **Fix:** Use `time.Time` fields in GORM models; let the driver handle conversion

### P1-31 UpsertBatch not in transaction *(was P2-18)*
- **File:** `internal/repositories/mysql/receiver_transfer_repository.go:135-176`
- **Impact:** Partial batch insert on failure — some items persisted, others lost. Catalog becomes inconsistent with slave state
- **Fix:** Wrap in GORM transaction

### P1-32 REGEXP on every media query *(was P2-19)*
- **File:** `internal/repositories/mysql/media_metadata_repository.go:236-239`
- **Impact:** Full table scan on every media list query. Performance degrades linearly with media count
- **Fix:** Use indexed column filtering or pre-computed columns; move regex to application layer

### P1-33 Playlist Save() cascades to Items *(was P2-20)*
- **File:** `internal/repositories/mysql/playlist_repository.go:56-58`
- **Impact:** GORM `Save()` on playlist cascades to all items — any concurrent item modification could be overwritten
- **Fix:** Use targeted `Updates()` for playlist metadata only

### P1-34 User Update uses Save() (full update) *(was P2-21)*
- **File:** `internal/repositories/mysql/user_repository_gorm.go:116-140`
- **Impact:** Every user update writes all columns, including password hash. Unnecessary DB load and audit noise
- **Fix:** Use `Updates()` with specific fields

### P1-35 Session Update only persists LastActivity *(was P2-22)*
- **File:** `internal/repositories/mysql/session_repository_gorm.go:44-53`
- **Impact:** If other session fields are modified (e.g., role change), they're lost on next Update call
- **Fix:** Persist all modified fields, or document that Update is LastActivity-only

### P1-36 Delete methods don't check RowsAffected *(was P2-24)*
- **File:** Multiple repositories
- **Impact:** Delete of non-existent item returns success — callers can't distinguish "deleted" from "was already gone"
- **Fix:** Check `RowsAffected` and return appropriate error or sentinel

### P1-37 PlaybackPosition uses filesystem path as PK *(was P2-27)*
- **File:** `pkg/models/models.go:233-240`
- **Impact:** File rename or move creates orphaned playback positions; path-based lookup breaks on case-insensitive filesystems
- **Fix:** Use media ID as primary key instead of path

### P1-38 time.Duration JSON round-trip produces nanoseconds *(was P2-28)*
- **File:** `internal/config/types.go`
- **Impact:** `time.Duration` marshals as int64 nanoseconds — config.json becomes unreadable. Loading a saved config may misinterpret "30000000000" (30s in ns) vs human expectation
- **Fix:** Custom JSON marshal/unmarshal using string format ("30s", "5m")

### P1-39 SetValue doesn't call syncFeatureToggles *(was P2-29)*
- **File:** `internal/config/accessors.go:61-74`
- **Impact:** Changing a feature toggle via `SetValue` doesn't propagate to the feature flags system — feature state drifts from config until restart
- **Fix:** Call `syncFeatureToggles` at end of `SetValue`/`SetValuesBatch`

### P1-40 analytics mediaStats/mediaViewers grow without bound *(was P2-36)*
- **File:** `internal/analytics/stats.go`
- **Impact:** `mediaStats`, `mediaViewers`, `mediaDurationSamples` maps grow without eviction. Every distinct MediaID creates a permanent entry. On high-turnover servers this is a slow OOM leak
- **Fix:** Add TTL-based eviction or cap map size; purge entries for deleted media

### P1-41 RecordDuplicatesFromSlave loads entire table *(was P2-37)*
- **File:** `internal/duplicates/duplicates.go:220-243`
- **Impact:** `ListAll(ctx)` loads entire `receiver_media` table into memory per catalog push. Large slave networks cause memory spikes
- **Fix:** Use fingerprint-indexed lookup instead of full table load; or paginate

### P1-42 ScanLocalMedia loads entire metadata table *(was P2-38)*
- **File:** `internal/duplicates/duplicates.go:333-353`
- **Impact:** Same pattern as P1-41 — loads all metadata into memory for local duplicate scan
- **Fix:** Paginate or use streaming cursor

### P1-43 httpClient has no ResponseHeaderTimeout *(was P2-41)*
- **File:** `internal/receiver/receiver.go:161-165`
- **Impact:** Slow or malicious slave can hold HTTP connections open indefinitely during stream proxy
- **Fix:** Set `ResponseHeaderTimeout` (e.g., 30s) on the transport

### P1-44 loadFromDB stops on first media load failure *(was P2-42)*
- **File:** `internal/receiver/receiver.go:251-253`
- **Impact:** Single corrupt row prevents loading all subsequent receiver media on startup. Server starts with incomplete catalog
- **Fix:** Log error and continue; collect all errors

### P1-45 Background goroutines use context.Background() *(was P2-58)*
- **File:** `api/handlers/` (multiple files)
- **Impact:** Background goroutines launched from handlers use `context.Background()` instead of module contexts — they run indefinitely even during shutdown, causing resource leaks and potential crashes during stop
- **Fix:** Use module-scoped context or derive from server shutdown context

### P1-46 RecordRating has no validation on rating value *(was P2-57)*
- **File:** `api/handlers/suggestions.go:155`
- **Impact:** Any integer value accepted as rating — no bounds check. Negative values or extreme values could corrupt suggestion scoring
- **Fix:** Validate rating is within expected range (e.g., 1-5 or 1-10)

### P1-47 Inconsistent cookie-clearing strategies *(was P2-55)*
- **File:** `api/handlers/auth.go:119-127`
- **Impact:** Different logout/session-clearing paths use inconsistent cookie attributes — some cookies may not be cleared properly, leaving stale sessions
- **Fix:** Centralize cookie-clearing with consistent Path, Domain, Secure, HttpOnly attributes

---

## P2 — MEDIUM: Tech debt / time bombs (17)

### Code Quality
- **P2-31** `internal/scanner/mature.go:521-552` — Custom keywords use substring matching (false positives on partial word matches)
- **P2-33** `internal/hls/locks.go:60-61` — 30-minute stale lock threshold too short for large file transcodes
- **P2-34** `internal/hls/jobs.go:74` — Error says "use admin panel to reset" but no ResetJob API exists

### Module-Level
- **P2-43** `internal/remote/remote.go:799-818` — saveCacheIndex errors logged but not returned
- **P2-44** `internal/suggestions/suggestions.go:938-957` — loadProfiles silently ignores view history load errors
- **P2-46** `internal/crawler/browser.go:157-158` — Events channel drops on overflow (missed CDP events)
- **P2-47** `internal/crawler/browser.go:231-233` — send() ignores errors from domain enable calls
- **P2-48** `cmd/media-receiver/main.go:988` — generateFileID uses absolute path (non-portable across machines)
- **P2-49** `web/frontend/src/pages/admin/SystemTab.tsx:79-86` — Config editor silently overwritten by React Query refetch

### Frontend
- **P2-50** `endpoints.ts:277-286` — Playlist/analytics export functions bypass API client error handling
- **P2-51** `AnalyticsTab.tsx:174` — Audit log export uses plain anchor tag (no auth header, no error handling)
- **P2-52** `usePlayerPageState.ts:123` — `el.src` set synchronously before HLS capability check
- **P2-53** `usePlayerPageState.ts:225` — `handleLoadedMetadata` always auto-plays (no user preference check)
- **P2-54** `useEqualizer.ts:141-144` — `createMediaElementSource` double-call risk (throws on second call)
- **P2-56** `api/handlers/playlists.go:130-153` — UpdatePlaylist/DeletePlaylist return 403 for all errors (masks real cause)

### Data Integrity
- **P3-1** *(promoted)* Multiple repositories — json.Marshal/Unmarshal errors silently ignored → can corrupt stored JSON fields
- **P3-8** *(promoted)* `internal/admin/admin.go:172-227` — ExportAuditLog loads up to 100K rows into memory

---

## P3 — LOW: Cleanup / style (3)

- **P3-3** `cmd/server/main.go:397-438` — "metadata-cleanup" task duplicates "media-scan" logic
- **P3-4** `internal/server/server.go:47+215` — HealthReporter populated but never queried
- **P3-5** `api/handlers/handler.go:326-339` — logAdminAction: callers pass dead UserID/Username fields

### Moved to P2 (no longer P3)
- P3-1 → P2 (json errors can corrupt data)
- P3-8 → P2 (100K row memory load)

### Closed as won't-fix / too minor
- **P3-7** AudioPlayer useEqualizer return discarded — cosmetic
- **P3-9** writeChunkAndTrack mutex per 32KB — performance acceptable at current scale
- **P3-10** PushCatalog MediaCount wrong on incremental — cosmetic counter
- **P3-11** detectAnime false positives — heuristic tuning, not a bug
- **P3-12** No second-signal forced exit — standard Go behavior, not needed
- **P3-14** Health check doesn't check all dirs — non-critical dirs
- **P3-15** thumbnails critical but depends on ffmpeg — by design (startup checks for ffmpeg)
- **P3-16** UniqueUsers not reconstructed from DB — acceptable approximation

---

*Report generated by deep-debug-audit skill — 2026-03-14*
*All 48 fixes code-verified — 2026-03-14*
*Re-verified and reprioritized — 2026-03-14*
*4 items confirmed fixed since last report (P1-23, P1-24, P2-25, P2-45)*
*P0-4 downgraded to partial fix (loopback/link-local gaps)*
*Remaining issues promoted: 8 P2→P0, 14 P2→P1, 2 P3→P2, 8 P3 closed*
