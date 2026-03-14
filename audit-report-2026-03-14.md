# Deep Debug Audit Report — Media Server Pro 4

**Date:** 2026-03-14
**Branch:** development
**Commit:** 8fb17b1
**Last verified:** 2026-03-14

---

## FIXED ITEMS (44/193 — verified 2026-03-14)

| Finding | Status |
|---------|--------|
| Analytics CompletionRate formula | ✅ Verified |
| Playlist AddItem swallows DB errors | ✅ Verified |
| Playlist removeItemLocked swallows DB error | ✅ Verified |
| Autodiscovery ClearAllSuggestions doesn't delete DB | ✅ Verified |
| Autodiscovery ClearSuggestion doesn't persist | ✅ Verified |
| Media-receiver range request fallthrough | ✅ Verified |
| HLS lazyTranscodeQuality activeJobs tracking | ✅ Verified |
| Receiver proxyViaHTTP ?id= vs ?path= | ✅ Verified |
| Database tryConnect connection leak | ✅ Verified |
| Security BanIP persistence (ban config row) | ✅ Verified |
| endpoints.ts !== null (undefined sent as param) | ✅ Verified |
| TrackPlayback divide-by-zero (duration=0) | ✅ Verified |
| SanitizeString null-byte ordering | ✅ Verified |
| Analytics temp file defer (Windows cleanup) | ✅ Verified |
| Media-receiver shutdown WebSocket writeMu | ✅ Verified |
| Receiver/remote double-stop panic | ✅ Verified |
| **[FIXED]** P0-10 restoreFromBackup .tar.gz dead code | ✅ Removed |
| **[FIXED]** P1-2 SetPassword cache/DB divergence | ✅ Copy-then-persist |
| **[FIXED]** P1-3 CopyPlaylist partial failure orphans | ✅ CreateWithItems tx |
| **[FIXED]** P1-4 ClearAllPlaybackPositions DB cleanup | ✅ DeleteAllByUser |
| **[FIXED]** P1-6 WriteTimeout kills long streams | ✅ Default 0 |
| **[FIXED]** P1-8 JSON parse on non-JSON (api/client) | ✅ Try/catch → ApiError |
| **[FIXED]** P1-11 Analytics backgroundLoop WaitGroup | ✅ bgWg.Wait() in Stop |
| **[FIXED]** P0-8 AdminUpdateConfig logs secrets | ✅ redactSensitiveConfigKeys |
| **[FIXED]** P1-1 HLS GetJobStatus/ListJobs data race | ✅ Return copies |
| **[FIXED]** P1-12 CacheMedia context.Background() | ✅ Module ctx, cancel on Stop |
| **[FIXED]** P2-6 HLS cleanupDone double-close | ✅ cleanupDoneOnce sync.Once |
| **[FIXED]** P1-5 Slave extension list missing 7 formats | ✅ Added .3gp, .m2ts, .vob, .ogv, .aiff, .ape, .mka |
| **[FIXED]** P2-4 HLS cleanup TOCTOU | ✅ Re-check under write lock before removal |
| **[FIXED]** P3-13 godotenv.Load error discarded | ✅ Log non-NotFound errors |
| **[FIXED]** P1-13 evictStaleProfiles TOCTOU | ✅ Re-check LastUpdated under lock |
| **[FIXED]** P2-26 scheduleUnregisterUpload leak | ✅ done chan, select on shutdown |
| **[FIXED]** P2-23 List Limit=0 OOM risk | ✅ Default cap analytics/audit_log |
| **[FIXED]** P0-5 Extractor proxyStream header denylist | ✅ Allowlist |
| **[FIXED]** P1-7 No rollback on startup failure | ✅ Stop started modules in reverse |
| **[FIXED]** P2-5 HLS RecordAccess saveJob outside lock | ✅ Call saveJob under lock |
| **[FIXED]** P3-6 Toast setTimeout not cleaned up | ✅ useRef + useEffect cleanup |
| **[FIXED]** P3-2 IsStrictlyExpired duplicate | ✅ Delegate to IsExpired |
| **[FIXED]** P2-7 handleStatus len(modules) outside lock | ✅ Read under RLock |
| **[FIXED]** P1-10 Last admin TOCTOU race | ✅ lastAdminMu in UpdateUser |

---

## REMAINING ISSUES — Reprioritized (149 findings)

### Priority counts:
```
P0 — CRITICAL (security + broken):   7 remaining
P1 — HIGH (user-facing bugs):        2 remaining
P2 — MEDIUM (tech debt / fragile):   35 remaining
P3 — LOW (cleanup / style):          11 remaining
Additional findings by module:       ~100 remaining
────────────────────────────────────
TOTAL REMAINING:                     149
```

---

## P0 — CRITICAL: Must fix before deploy (8 remaining)

### P0-1 [SECURITY] AdminExecuteQuery allows SELECT INTO OUTFILE / LOAD_FILE
- **File:** `api/handlers/system.go:386-521`
- **Impact:** Authenticated admin can exfiltrate/write server files via SQL
- **Fix:** Use `SET SESSION TRANSACTION READ ONLY` instead of prefix matching

### P0-2 [SECURITY] ReceiverWebSocket has no middleware-level auth
- **File:** `api/routes/routes.go:279`
- **Impact:** If internal auth check is bypassed, any client can push arbitrary catalogs
- **Fix:** Add `RequireReceiverWithAPIKey()` middleware to `/ws/receiver` route

### P0-3 [SECURITY] Unauthenticated streaming bypasses stream limits
- **File:** `api/handlers/media.go:300-355`
- **Impact:** Unlimited concurrent connections from unauthenticated users
- **Fix:** Add `Streaming.RequireAuth` config option; IP-based tracking for unauth

### P0-4 [SECURITY] Chrome launched with --disable-web-security
- **File:** `internal/crawler/browser.go:114-128`
- **Impact:** Malicious JS on target page can access local network
- **Fix:** Add `--proxy-server` or `--host-resolver-rules` to block private IPs

### P0-5 [SECURITY] Extractor proxyStream header denylist — **[FIXED]**
- **Fix applied:** Allowlist (Content-Type, Content-Length, Content-Range, etc.).

### P0-6 [SECURITY] GitHub credentials visible in process environment
- **File:** `internal/updater/updater.go:940-941`
- **Impact:** Any local user can read GitHub token from `/proc/<pid>/environ`
- **Fix:** Use `GIT_ASKPASS` or credential helper

### P0-7 [SECURITY] Default admin password printed to stderr
- **File:** `internal/auth/bootstrap.go:35`
- **Impact:** Password persists in systemd journal/container logs
- **Fix:** Write to file with 0600 permissions or require interactive setup

### P0-8 [SECURITY] AdminUpdateConfig logs secrets — **[FIXED]**
- **Fix applied:** redactSensitiveConfigKeys before logAdminAction.

### P0-9 [SECURITY] /api/status and /api/modules unauthenticated
- **File:** `internal/server/server.go:236-238`
- **Impact:** Attacker can fingerprint server; health messages may leak DB info
- **Fix:** Put behind `adminAuth()` or sanitize responses

### P0-10 [BROKEN] restoreFromBackup .tar.gz branch is dead code — **[FIXED]**
- **File:** `internal/updater/updater.go`
- **Fix applied:** Removed .tar.gz branch; createBackup only produces single-file backups.

---

## P1 — HIGH: Will cause user-facing bugs (6 remaining)

### P1-1 [FRAGILE] HLS GetJobStatus/ListJobs data race — **[FIXED]**
- **Fix applied:** copyHLSJob; return copies from GetJobStatus, ListJobs, GetJobByMediaPath.

### P1-2 [FRAGILE] SetPassword cache/DB divergence — **[FIXED]**
- **Fix applied:** Work on copy; update cache only after DB success.

### P1-3 [FRAGILE] CopyPlaylist partial failure — **[FIXED]**
- **Fix applied:** CreateWithItems repo method with transaction; no orphans on partial failure.

### P1-4 [FRAGILE] ClearAllPlaybackPositions doesn't clean DB — **[FIXED]**
- **Fix applied:** DeleteAllPlaybackPositionsByUser; positions no longer reappear after restart.

### P1-5 [DRIFT] Slave extension list — **[FIXED]**
- **Fix applied:** Added .3gp, .m2ts, .vob, .ogv, .aiff, .ape, .mka to classifyFile.

### P1-6 [FRAGILE] WriteTimeout kills long streams — **[FIXED]**
- **Fix applied:** Default WriteTimeout set to 0 in config/defaults.go.

### P1-7 [GAP] No rollback on startup failure — **[FIXED]**
- **Fix applied:** Stop already-started modules in reverse order on critical failure.

### P1-8 [FRAGILE] JSON parse on non-JSON — **[FIXED]**
- **Fix applied:** Read text + JSON.parse in try/catch; throw ApiError with status and preview.

### P1-9 [FRAGILE] RestartServer uses os.Exit(1)
- **File:** `api/handlers/admin_lifecycle.go:29`
- **Impact:** No defers, no DB close, no request drain
- **Fix:** Use graceful shutdown (signal to self)

### P1-10 [FRAGILE] "Last admin" TOCTOU race — **[FIXED]**
- **Fix applied:** lastAdminMu in UpdateUser; check under lock before demote/disable.

### P1-11 [LEAK] Analytics backgroundLoop WaitGroup — **[FIXED]**
- **Fix applied:** bgWg.Add(1), defer Done() in backgroundLoop; bgWg.Wait() in Stop().

### P1-12 [LEAK] CacheMedia context.Background() — **[FIXED]**
- **Fix applied:** Module ctx/cancel; CacheMedia uses m.ctx; cancel on Stop().

### P1-13 [FRAGILE] evictStaleProfiles TOCTOU — **[FIXED]**
- **Fix applied:** Re-check LastUpdated under write lock before delete.

### P1-14 [DRIFT] Two different thumbnail timestamp spacing algorithms
- **File:** `internal/thumbnails/preview.go:82-86`
- **Impact:** Preview thumbnails don't match expected URLs; cache misses
- **Fix:** Unify to use `previewTimestamp` everywhere

---

## P2 — MEDIUM: Tech debt / time bombs (40 remaining)

### Concurrency / Data Races
- **P2-1** `internal/auth/authenticate.go:91-96` — Authenticate mutates shared user pointer outside lock
- **P2-2** `internal/auth/session.go:127-132` — ValidateSession fires background goroutine with shared pointer
- **P2-3** `internal/auth/auth.go:194-205` — Cleanup goroutine exits permanently after panic (no restart loop)
- **P2-4** ~~cleanup TOCTOU~~ — **[FIXED]** Re-check under write lock before removal
- **P2-5** ~~RecordAccess saveJob outside lock~~ — **[FIXED]** Call saveJob under lock
- **P2-6** ~~cleanupDone double-close~~ — **[FIXED]** cleanupDoneOnce sync.Once
- **P2-7** ~~handleStatus len(modules) outside lock~~ — **[FIXED]** Read under RLock

### Security
- **P2-8** `pkg/middleware/middleware.go:69` — Client-supplied X-Request-ID propagated without sanitization
- **P2-9** `pkg/middleware/agegate.go:185` — ageGateSecure trusts X-Forwarded-Proto without proxy check
- **P2-10** `pkg/helpers/ssrf.go:67` — ValidateURLForSSRF vulnerable to DNS rebinding
- **P2-11** `api/handlers/handler.go:377-380` — EvalSymlinks failure falls back to raw path
- **P2-12** `api/handlers/system.go:410` — AdminExecuteQuery semicolon check bypassable
- **P2-13** `api/handlers/hls.go:300,303` — GetHLSCapabilities/GetHLSStatus unauthenticated
- **P2-14** `cmd/server/main.go:339-371` — validateSecrets allows weak API keys
- **P2-15** `cmd/media-receiver/main.go:869` — Path traversal check uses string matching
- **P2-16** `web/frontend/src/pages/admin/SystemTab.tsx:252-253` — SQL executor regex misses INSERT/CREATE/GRANT

### Data Integrity / Persistence
- **P2-17** 6+ repository files — String timestamps instead of time.Time (timezone mismatch risk)
- **P2-18** `internal/repositories/mysql/receiver_transfer_repository.go:135-176` — UpsertBatch not in transaction
- **P2-19** `internal/repositories/mysql/media_metadata_repository.go:236-239` — REGEXP on every query (full table scan)
- **P2-20** `internal/repositories/mysql/playlist_repository.go:56-58` — Playlist Save() cascades to Items
- **P2-21** `internal/repositories/mysql/user_repository_gorm.go:116-140` — User Update uses Save() (full update)
- **P2-22** `internal/repositories/mysql/session_repository_gorm.go:44-53` — Session Update only persists LastActivity
- **P2-23** ~~List Limit=0 OOM risk~~ — **[FIXED]** Default cap (10K analytics, 100K audit)
- **P2-24** Multiple repositories — Delete methods don't check RowsAffected

### Resource / Lifecycle
- **P2-25** `pkg/middleware/agegate.go:212-214` — Unbounded verifiedIPs map growth
- **P2-26** ~~scheduleUnregisterUpload leak~~ — **[FIXED]** done chan, select on shutdown
- **P2-27** `pkg/models/models.go:233-240` — PlaybackPosition uses filesystem path as primary key
- **P2-28** `internal/config/types.go` — time.Duration JSON round-trip produces nanoseconds
- **P2-29** `internal/config/accessors.go:61-74` — SetValue doesn't call syncFeatureToggles
- **P2-30** `internal/media/management.go:82-83` — MoveMedia does not validate oldPath

### Code Quality
- **P2-31** `internal/scanner/mature.go:521-552` — Custom keywords use substring matching (false positives)
- **P2-32** `internal/security/security.go:893-911` — Rate-limit bypass for "/media" prefix too broad
- **P2-33** `internal/hls/locks.go:60-61` — 30-minute stale lock threshold too short for large files
- **P2-34** `internal/hls/jobs.go:74` — Error says "use admin panel to reset" but no ResetJob API
- **P2-35** `internal/analytics/stats.go:55-62` — UniqueUsers not reconstructed from DB
- **P2-36** `internal/analytics/stats.go` — mediaStats/mediaViewers maps grow without bound
- **P2-37** `internal/duplicates/duplicates.go:220-243` — RecordDuplicatesFromSlave loads entire table
- **P2-38** `internal/duplicates/duplicates.go:333-353` — ScanLocalMedia loads entire metadata table
- **P2-39** `api/handlers/handler.go:424-440` — isPathWithinDirs and isPathUnderDirs near-duplicates
- **P2-40** `web/frontend/src/pages/admin/SystemTab.tsx:79-86` — Config editor silently overwritten by refetch

---

## P3 — LOW: Cleanup / style (13 remaining)

- **P3-1** Multiple repositories — json.Marshal/Unmarshal errors silently ignored
- **P3-2** ~~IsStrictlyExpired duplicate~~ — **[FIXED]** Delegate to IsExpired
- **P3-3** `cmd/server/main.go:397-438` — "metadata-cleanup" task duplicates "media-scan"
- **P3-4** `internal/server/server.go:47+215` — HealthReporter populated but never queried
- **P3-5** `api/handlers/handler.go:326-339` — logAdminAction: callers pass dead UserID/Username
- **P3-6** ~~Toast setTimeout not cleaned up~~ — **[FIXED]** useRef + useEffect cleanup
- **P3-7** `web/frontend/src/components/AudioPlayer.tsx:48` — useEqualizer return value discarded
- **P3-8** `internal/admin/admin.go:172-227` — ExportAuditLog loads up to 100K rows into memory
- **P3-9** `internal/upload/upload.go:546-553` — writeChunkAndTrack locks mutex on every 32KB chunk
- **P3-10** `internal/receiver/receiver.go:439` — PushCatalog MediaCount wrong on incremental pushes
- **P3-11** `internal/categorizer/categorizer.go:327-339` — detectAnime false positives
- **P3-12** `internal/server/signals_{unix,windows}.go` — No second-signal forced exit
- **P3-13** ~~godotenv.Load error discarded~~ — **[FIXED]** Log non-NotFound errors

---

## Additional Findings by Module (remaining)

### cmd/server/main.go
- `[LEAK]` main.go:329 — Already-started modules not stopped on Start() failure
- `[GAP]` main.go:628-646 — Health check doesn't check uploads/music/thumbnails/hls_cache dirs
- `[DRIFT]` main.go:174 + server.go:68 — thumbnails is critical but depends on optional ffmpeg

### internal/server/server.go
- `[FRAGILE]` server.go:441 — close(s.shutdownCh) called after logger.Shutdown()
- `[FRAGILE]` server.go + main.go — Single context timeout for HTTP shutdown + all module stops

### internal/receiver/
- `[FRAGILE]` receiver.go:161-165 — httpClient has no ResponseHeaderTimeout
- `[SILENT FAIL]` receiver.go:251-253 — loadFromDB stops loading on media load failure

### internal/remote/
- `[SILENT FAIL]` remote.go:799-818 — saveCacheIndex errors logged but not returned

### internal/suggestions/
- `[SILENT FAIL]` suggestions.go:938-957 — loadProfiles silently ignores view history load errors

### internal/extractor/
- `[FRAGILE]` extractor.go:389 — ProxyHLSVariant panics if cached is not *cachedPlaylist
- `[FRAGILE]` extractor.go:509 — proxyStream creates new http.Client per request

### internal/crawler/
- `[LEAK]` browser.go:130-139 — Chrome child processes may not be killed
- `[FRAGILE]` browser.go:157-158 — Events channel drops on overflow
- `[FRAGILE]` browser.go:231-233 — send() ignores errors from domain enable calls

### internal/validator/
- `[FRAGILE]` validator.go:404-492 — FixFile has no output size limit

### internal/admin/
- `[FRAGILE]` admin.go:248-255 — UpdateConfig has no atomicity

### cmd/media-receiver/main.go
- `[FRAGILE]` main.go:988 — generateFileID uses absolute path
- `[LEAK]` main.go:95-98 — fpCache grows unbounded

### Frontend
- `[SILENT FAIL]` endpoints.ts:277-286 — Playlist/analytics exports bypass API client error handling
- `[SILENT FAIL]` AnalyticsTab.tsx:174 — Audit log export uses plain anchor tag
- `[FRAGILE]` usePlayerPageState.ts:123 — el.src set synchronously before HLS check
- `[FRAGILE]` usePlayerPageState.ts:225 — handleLoadedMetadata always auto-plays
- `[LEAK]` useEqualizer.ts:141-144 — createMediaElementSource double-call risk
- `[FRAGILE]` api/handlers/auth.go:119-127 — Inconsistent cookie-clearing strategies
- `[FRAGILE]` api/handlers/playlists.go:130-153 — UpdatePlaylist/DeletePlaylist return 403 for all errors
- `[FRAGILE]` api/handlers/suggestions.go:155 — RecordRating has no validation on rating value
- `[FRAGILE]` api/handlers (multiple) — Background goroutines use context.Background()

---

*Report generated by deep-debug-audit skill — 2026-03-14*
*Fixes verified and priorities updated — 2026-03-14*
