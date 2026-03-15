# Deep Debug Audit Report — Media Server Pro 4

**Date:** 2026-03-14
**Branch:** development
**Commit:** ddfac05
**Last verified:** 2026-03-14
**Re-verified & reprioritized:** 2026-03-14 (48 fixes code-verified, 4 newly confirmed, remaining issues promoted)
**Updated:** 2026-03-15 — Additional fixes: P1-34, P1-35, P1-40, P2-34

---

## FIXED ITEMS (68 total — 48 code-verified 2026-03-14, 20 fixed 2026-03-15)

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
| P0-4 | Chrome --host-resolver-rules (loopback/link-local) | ✅ Fixed 2026-03-15 — browser.go: 127/8, 169.254/16, ::1/128 added |
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
| P0-11 | X-Request-ID log injection | ✅ Fixed 2026-03-15 — middleware.go: sanitizeRequestID, 64 chars, strip control chars |
| P0-12 | AdminExecuteQuery semicolon bypass (Unicode) | ✅ Fixed 2026-03-15 — system.go: normalize U+037E, U+FF1B before check |
| P0-13 | ageGateSecure X-Forwarded-Proto | ✅ Fixed 2026-03-15 — agegate.go: trust only from isTrustedProxy(remoteAddr) |
| P0-14 | GetHLSCapabilities/GetHLSStatus unauthenticated | ✅ Fixed 2026-03-15 — routes.go: requireAuth() on both |
| P0-16 | MoveMedia oldPath not validated | ✅ Fixed 2026-03-15 — management.go: validatePath(oldPath) before move |
| P0-17 | Rate-limit bypass /media prefix | ✅ Fixed 2026-03-15 — security.go: path.Clean, exempt only /media or /media/... |
| P0-18 | SQL executor regex (INSERT/CREATE/GRANT etc) | ✅ Fixed 2026-03-15 — SystemTab.tsx: extended confirmation regex |
| P0-15 | validateSecrets weak API keys | ✅ Fixed 2026-03-15 — main.go: min 32 chars warning for receiver API keys |
| P1-39 | SetValue doesn't call syncFeatureToggles | ✅ Fixed 2026-03-15 — accessors.go: syncFeatureToggles after save in SetValuesBatch |
| P1-43 | httpClient no ResponseHeaderTimeout | ✅ Fixed 2026-03-15 — receiver.go: 30s ResponseHeaderTimeout on transport |
| P1-44 | loadFromDB stops on first media load failure | ✅ Fixed 2026-03-15 — receiver.go: recover per row, log and continue |
| P1-46 | RecordRating no validation on rating value | ✅ Fixed 2026-03-15 — suggestions.go: validate 0–5 range |
| P1-47 | Inconsistent cookie-clearing strategies | ✅ Fixed 2026-03-15 — handler.go: clearSessionCookie; auth.go Logout + DeleteAccount |
| P1-31 | UpsertBatch not in transaction | ✅ Fixed 2026-03-15 — receiver_transfer_repository.go: Transaction wraps batch upsert |
| P2-56 | UpdatePlaylist/DeletePlaylist return 403 for all errors | ✅ Fixed 2026-03-15 — playlists.go: 404 for ErrPlaylistNotFound, 403 for ErrAccessDenied, 500 else |
| P1-34 | User Update uses Save() (full update) | ✅ Fixed 2026-03-15 — user_repository_gorm.go: Updates() with specific fields; password only when set |
| P1-35 | Session Update only persists LastActivity | ✅ Fixed 2026-03-15 — session_repository_gorm.go: Update persists all updatable fields |
| P2-34 | HLS error "use admin panel to reset" but no API | ✅ Fixed 2026-03-15 — jobs.go: removed misleading reset wording from error message |
| P1-40 | analytics mediaStats/mediaViewers grow without bound | ✅ Fixed 2026-03-15 — cleanup.go: evictExcessMediaStats cap 100K, evict by oldest LastViewed |

</details>

---

## REMAINING ISSUES — Reprioritized (remaining open findings)

All remaining issues have been promoted one tier upward to reflect their cumulative risk
now that the most critical items are resolved. Security and crash-risk items receive
the largest promotions.

### Priority counts:
```
P0 — CRITICAL (security / crash / data loss):   1  (9 fixed 2026-03-15)
P1 — HIGH (user-facing bugs / fragile):        15  (7 fixed 2026-03-15)
P2 — MEDIUM (tech debt / time bombs):          15  (2 fixed 2026-03-15)
P3 — LOW (cleanup / style):                     3
────────────────────────────────────────────────
TOTAL REMAINING:                                32
```

---

## P0 — CRITICAL: Must fix before deploy (1 remaining)

### P0-6 [SECURITY] GitHub credentials visible in process environment
- **File:** `internal/updater/updater.go:930-942`
- **Impact:** Token appears in `GIT_CONFIG_VALUE_0` env var — any local user can read via `/proc/<pid>/environ`
- **Fix:** Use `GIT_ASKPASS` helper script or credential helper

---

## P1 — HIGH: Will cause user-facing bugs or crashes (15 remaining)

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

### P1-32 REGEXP on every media query *(was P2-19)*
- **File:** `internal/repositories/mysql/media_metadata_repository.go:236-239`
- **Impact:** Full table scan on every media list query. Performance degrades linearly with media count
- **Fix:** Use indexed column filtering or pre-computed columns; move regex to application layer

### P1-33 Playlist Save() cascades to Items *(was P2-20)*
- **File:** `internal/repositories/mysql/playlist_repository.go:56-58`
- **Impact:** GORM `Save()` on playlist cascades to all items — any concurrent item modification could be overwritten
- **Fix:** Use targeted `Updates()` for playlist metadata only

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

### P1-41 RecordDuplicatesFromSlave loads entire table *(was P2-37)*
- **File:** `internal/duplicates/duplicates.go:220-243`
- **Impact:** `ListAll(ctx)` loads entire `receiver_media` table into memory per catalog push. Large slave networks cause memory spikes
- **Fix:** Use fingerprint-indexed lookup instead of full table load; or paginate

### P1-42 ScanLocalMedia loads entire metadata table *(was P2-38)*
- **File:** `internal/duplicates/duplicates.go:333-353`
- **Impact:** Same pattern as P1-41 — loads all metadata into memory for local duplicate scan
- **Fix:** Paginate or use streaming cursor

### P1-45 Background goroutines use context.Background() *(was P2-58)*
- **File:** `api/handlers/` (multiple files)
- **Impact:** Background goroutines launched from handlers use `context.Background()` instead of module contexts — they run indefinitely even during shutdown, causing resource leaks and potential crashes during stop
- **Fix:** Use module-scoped context or derive from server shutdown context

---

## P2 — MEDIUM: Tech debt / time bombs (15 remaining)

### Code Quality
- **P2-31** `internal/scanner/mature.go:521-552` — Custom keywords use substring matching (false positives on partial word matches)
- **P2-33** `internal/hls/locks.go:60-61` — 30-minute stale lock threshold too short for large file transcodes

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
*2026-03-15: P0-4..18, P0-15; P1-31,34,35,39,40,43,44,46,47; P2-34,56 fixed; 1 P0 and 32 total remaining*
