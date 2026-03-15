# Deep Debug Audit Report — Media Server Pro 4

**Date:** 2026-03-14
**Branch:** development
**Commit:** 8fb17b1
**Last verified:** 2026-03-14
**Reprioritized:** 2026-03-14 (all fixes code-verified, remaining issues promoted)

---

## FIXED ITEMS (44 total — all code-verified 2026-03-14)

<details>
<summary>Click to expand verified fixes</summary>

| ID | Finding | Verification |
|----|---------|-------------|
| — | Analytics CompletionRate formula | ✅ Verified |
| — | Playlist AddItem swallows DB errors | ✅ Verified |
| — | Playlist removeItemLocked swallows DB error | ✅ Verified |
| — | Autodiscovery ClearAllSuggestions doesn't delete DB | ✅ Verified |
| — | Autodiscovery ClearSuggestion doesn't persist | ✅ Verified |
| — | Media-receiver range request fallthrough | ✅ Verified |
| — | HLS lazyTranscodeQuality activeJobs tracking | ✅ Verified |
| — | Receiver proxyViaHTTP ?id= vs ?path= | ✅ Verified |
| — | Database tryConnect connection leak | ✅ Verified |
| — | Security BanIP persistence (ban config row) | ✅ Verified |
| — | endpoints.ts !== null (undefined sent as param) | ✅ Verified |
| — | TrackPlayback divide-by-zero (duration=0) | ✅ Verified |
| — | SanitizeString null-byte ordering | ✅ Verified |
| — | Analytics temp file defer (Windows cleanup) | ✅ Verified |
| — | Media-receiver shutdown WebSocket writeMu | ✅ Verified |
| — | Receiver/remote double-stop panic | ✅ Verified |
| P0-5 | Extractor proxyStream header allowlist | ✅ `extractor.go:516-528` — allowlist confirmed |
| P0-8 | AdminUpdateConfig redacts secrets | ✅ `admin_config.go:11-33,68` — redactSensitiveConfigKeys |
| P0-10 | restoreFromBackup .tar.gz dead code | ✅ `updater.go:890-903` — removed |
| P1-1 | HLS GetJobStatus/ListJobs data race | ✅ `jobs.go:15-31,184-231` — copyHLSJob |
| P1-2 | SetPassword cache/DB divergence | ✅ `password.go:59-94` — copy-then-persist |
| P1-3 | CopyPlaylist partial failure | ✅ `playlist.go:464-520` — CreateWithItems tx |
| P1-4 | ClearAllPlaybackPositions DB cleanup | ✅ DeleteAllByUser confirmed |
| P1-5 | Slave extension list missing 7 formats | ✅ .3gp/.m2ts/.vob/.ogv/.aiff/.ape/.mka added |
| P1-6 | WriteTimeout kills long streams | ✅ Default 0 in config/defaults.go |
| P1-7 | No rollback on startup failure | ✅ `server.go:276-321` — reverse-order stop |
| P1-8 | JSON parse on non-JSON (api/client) | ✅ try/catch + ApiError |
| P1-10 | Last admin TOCTOU race | ✅ `auth.go:60` + `user.go:186-211` — lastAdminMu |
| P1-11 | Analytics backgroundLoop WaitGroup | ✅ `module.go:44,93,113,139` — bgWg lifecycle |
| P1-12 | CacheMedia context.Background() | ✅ `remote.go:613-614` — m.ctx |
| P1-13 | evictStaleProfiles TOCTOU | ✅ Re-check under write lock |
| P2-4 | HLS cleanup TOCTOU | ✅ Re-check under write lock |
| P2-5 | RecordAccess saveJob outside lock | ✅ saveJob under lock |
| P2-6 | cleanupDone double-close | ✅ cleanupDoneOnce sync.Once |
| P2-7 | handleStatus len(modules) outside lock | ✅ `server.go:500-516` — RLock |
| P2-23 | List Limit=0 OOM risk | ✅ analytics 10K / audit 100K caps |
| P2-26 | scheduleUnregisterUpload leak | ✅ `upload.go:74,132,166,318-331` — done chan |
| P3-2 | IsStrictlyExpired duplicate | ✅ `models.go:355-357` — delegates |
| P3-6 | Toast setTimeout not cleaned up | ✅ `Toast.tsx:14-19` — useRef + useEffect |
| P3-13 | godotenv.Load error discarded | ✅ Log non-NotFound errors |

</details>

---

## REMAINING ISSUES — Reprioritized (remaining open findings)

Items from the "Additional Findings" section have been promoted into the main priority tiers
based on real-world exploitability, crash potential, and data-loss risk.

### Priority counts:
```
P0 — CRITICAL (security / crash / data loss):  7
P1 — HIGH (user-facing bugs / fragile):       15
P2 — MEDIUM (tech debt / time bombs):         27
P3 — LOW (cleanup / style):                   11
────────────────────────────────────────────────
TOTAL REMAINING:                               60
```

---

## P0 — CRITICAL: Must fix before deploy (7)

### P0-1 [SECURITY] AdminExecuteQuery allows SELECT INTO OUTFILE / LOAD_FILE — ✅ FIXED
- **File:** `api/handlers/system.go:386-521`
- **Impact:** Authenticated admin can exfiltrate/write server files via SQL
- **Fix applied:** Use read-only transaction (`sql.TxOptions{ReadOnly: true}`) so MySQL rejects write ops
- **Note:** P2-12 (semicolon bypass) is the same attack surface — fix together

### P0-2 [SECURITY] ReceiverWebSocket has no middleware-level auth — ✅ FIXED
- **File:** `api/routes/routes.go:279`
- **Impact:** If internal auth check is bypassed, any client can push arbitrary catalogs
- **Fix applied:** Added `RequireReceiverWithAPIKey()` middleware before `ReceiverWebSocket` handler

### P0-3 [SECURITY] Unauthenticated streaming bypasses stream limits — ✅ FIXED
- **File:** `api/handlers/media.go:300-355`
- **Impact:** Unlimited concurrent connections from unauthenticated users (DoS vector)
- **Fix applied:** Added `Streaming.RequireAuth` and `Streaming.UnauthStreamLimit`; IP-based tracking via `ip:` prefix; `TrackProxyStream` for receiver media

### P0-4 [SECURITY] Chrome launched with --disable-web-security — ✅ FIXED
- **File:** `internal/crawler/browser.go:114-128`
- **Impact:** Malicious JS on target page can access local network
- **Fix applied:** Added `--host-resolver-rules` to map RFC1918 ranges (10/8, 172.16/12, 192.168/16) to 0.0.0.0

### P0-6 [SECURITY] GitHub credentials visible in process environment
- **File:** `internal/updater/updater.go:940-941`
- **Impact:** Any local user can read GitHub token from `/proc/<pid>/environ`
- **Fix:** Use `GIT_ASKPASS` or credential helper

### P0-7 [SECURITY] Default admin password printed to stderr — ✅ FIXED
- **File:** `internal/auth/bootstrap.go:35`
- **Impact:** Password persists in systemd journal/container logs
- **Fix applied:** Write to `{data_dir}/admin-initial-password.txt` with 0600; log path only

### P0-9 [SECURITY] /api/status and /api/modules unauthenticated — ✅ FIXED
- **File:** `internal/server/server.go:236-238`
- **Impact:** Attacker can fingerprint server; health messages may leak DB info
- **Fix applied:** Routes moved to routes.Setup with `adminAuth()` middleware

---

## P1 — HIGH: Will cause user-facing bugs or crashes (15)

*Promoted items from Additional Findings are marked with `[PROMOTED]`.*

### P1-9 [FRAGILE] RestartServer uses os.Exit(1)
- **File:** `api/handlers/admin_lifecycle.go:29`
- **Impact:** No defers, no DB close, no request drain — corrupts in-flight writes
- **Fix:** Use graceful shutdown (signal to self)

### P1-14 [DRIFT] Two different thumbnail timestamp spacing algorithms — ✅ FIXED
- **File:** `internal/thumbnails/preview.go:82-86`
- **Fix applied:** previewURLForIndex now uses previewTimestamp

### P1-15 [PROMOTED] ProxyHLSVariant panics on type assertion — ✅ FIXED
- **File:** `internal/extractor/extractor.go:389`
- **Fix applied:** Type assertion with ok check; return error on mismatch

### P1-16 [PROMOTED] Chrome child processes may not be killed — ✅ FIXED
- **File:** `internal/crawler/browser.go:130-139`
- **Fix applied:** browser_unix.go: Setpgid + killChromeProcessGroup; browser_windows.go stub

### P1-17 [PROMOTED] Authenticate mutates shared user pointer outside lock — ✅ FIXED
- **File:** `internal/auth/authenticate.go:91-96`
- **Fix applied:** Copy user before LastLogin mutation; Update copy, then update cache

### P1-18 [PROMOTED] ValidateSession fires background goroutine with shared pointer — ✅ FIXED
- **File:** `internal/auth/session.go:127-132`
- **Fix applied:** Pass session copy to goroutine instead of shared pointer

### P1-19 [PROMOTED] close(shutdownCh) called after logger.Shutdown() — ✅ FIXED
- **File:** `internal/server/server.go:441`
- **Fix applied:** Close shutdownCh before logger.Shutdown()

### P1-20 [PROMOTED] Single context timeout for HTTP shutdown + all module stops
- **File:** `internal/server/server.go + cmd/server/main.go`
- **Impact:** Slow module eats the entire budget; HTTP never drains, or vice versa
- **Fix:** Separate timeouts for HTTP drain vs module stop

### P1-21 [PROMOTED] Cleanup goroutine exits permanently after panic — ✅ FIXED
- **File:** `internal/auth/auth.go:194-205`
- **Fix applied:** Recover inside cleanupLoop tick handler; loop continues on panic

### P1-22 [PROMOTED] EvalSymlinks failure falls back to raw path — ✅ FIXED
- **File:** `api/handlers/handler.go:377-380`
- **Fix applied:** Return error instead of falling back to unresolved path

### P1-23 [PROMOTED] ValidateURLForSSRF vulnerable to DNS rebinding
- **File:** `pkg/helpers/ssrf.go:67`
- **Impact:** Attacker points URL at public DNS that resolves to internal IP after validation
- **Fix:** Re-validate resolved IP at dial time (custom `DialContext`)

### P1-24 [PROMOTED] fpCache grows unbounded (media-receiver)
- **File:** `cmd/media-receiver/main.go:95-98`
- **Impact:** Memory grows without limit on receivers with many files
- **Fix:** Add LRU eviction or size cap

### P1-25 [PROMOTED] FixFile has no output size limit — ✅ FIXED
- **File:** `internal/validator/validator.go:404-492`
- **Fix applied:** 10 GB max output; remove file and return error if exceeded

### P1-26 [PROMOTED] Path traversal check uses string matching (media-receiver) — ✅ FIXED
- **File:** `cmd/media-receiver/main.go:869`
- **Fix applied:** filepath.Clean, EvalSymlinks, filepath.Rel containment

### P1-27 [PROMOTED] UpdateConfig has no atomicity — ✅ FIXED
- **File:** `internal/admin/admin.go:248-255`
- **Fix applied:** SetValuesBatch applies all updates then saves once

---

## P2 — MEDIUM: Tech debt / time bombs (27)

### Security
- **P2-8** `pkg/middleware/middleware.go:69` — Client-supplied X-Request-ID propagated without sanitization
- **P2-9** `pkg/middleware/agegate.go:185` — ageGateSecure trusts X-Forwarded-Proto without proxy check
- **P2-12** `api/handlers/system.go:410` — AdminExecuteQuery semicolon check bypassable (fix with P0-1)
- **P2-13** `api/handlers/hls.go:300,303` — GetHLSCapabilities/GetHLSStatus unauthenticated
- **P2-14** `cmd/server/main.go:339-371` — validateSecrets allows weak API keys
- **P2-16** `web/frontend/src/pages/admin/SystemTab.tsx:252-253` — SQL executor regex misses INSERT/CREATE/GRANT

### Concurrency
- **P2-1** `internal/auth/authenticate.go:91-96` — (see P1-17; this is the broader pattern — review all auth mutation paths)
- **P2-2** `internal/auth/session.go:127-132` — (see P1-18; related goroutine pattern)

### Data Integrity / Persistence
- **P2-17** 6+ repository files — String timestamps instead of time.Time (timezone mismatch risk)
- **P2-18** `internal/repositories/mysql/receiver_transfer_repository.go:135-176` — UpsertBatch not in transaction
- **P2-19** `internal/repositories/mysql/media_metadata_repository.go:236-239` — REGEXP on every query (full table scan)
- **P2-20** `internal/repositories/mysql/playlist_repository.go:56-58` — Playlist Save() cascades to Items
- **P2-21** `internal/repositories/mysql/user_repository_gorm.go:116-140` — User Update uses Save() (full update)
- **P2-22** `internal/repositories/mysql/session_repository_gorm.go:44-53` — Session Update only persists LastActivity
- **P2-24** Multiple repositories — Delete methods don't check RowsAffected

### Resource / Lifecycle
- **P2-25** `pkg/middleware/agegate.go:212-214` — Unbounded verifiedIPs map growth
- **P2-27** `pkg/models/models.go:233-240` — PlaybackPosition uses filesystem path as primary key
- **P2-28** `internal/config/types.go` — time.Duration JSON round-trip produces nanoseconds
- **P2-29** `internal/config/accessors.go:61-74` — SetValue doesn't call syncFeatureToggles
- **P2-30** `internal/media/management.go:82-83` — MoveMedia does not validate oldPath
- **P2-36** `internal/analytics/stats.go` — mediaStats/mediaViewers maps grow without bound
- **P2-37** `internal/duplicates/duplicates.go:220-243` — RecordDuplicatesFromSlave loads entire table
- **P2-38** `internal/duplicates/duplicates.go:333-353` — ScanLocalMedia loads entire metadata table

### Code Quality
- **P2-31** `internal/scanner/mature.go:521-552` — Custom keywords use substring matching (false positives)
- **P2-32** `internal/security/security.go:893-911` — Rate-limit bypass for "/media" prefix too broad
- **P2-33** `internal/hls/locks.go:60-61` — 30-minute stale lock threshold too short for large files
- **P2-34** `internal/hls/jobs.go:74` — Error says "use admin panel to reset" but no ResetJob API

### Remaining Module-Level (not promoted)
- **P2-41** `internal/receiver/receiver.go:161-165` — httpClient has no ResponseHeaderTimeout
- **P2-42** `internal/receiver/receiver.go:251-253` — loadFromDB stops loading on first media load failure
- **P2-43** `internal/remote/remote.go:799-818` — saveCacheIndex errors logged but not returned
- **P2-44** `internal/suggestions/suggestions.go:938-957` — loadProfiles silently ignores view history load errors
- **P2-45** `internal/extractor/extractor.go:509` — proxyStream creates new http.Client per request
- **P2-46** `internal/crawler/browser.go:157-158` — Events channel drops on overflow
- **P2-47** `internal/crawler/browser.go:231-233` — send() ignores errors from domain enable calls
- **P2-48** `cmd/media-receiver/main.go:988` — generateFileID uses absolute path (non-portable)
- **P2-49** `web/frontend/src/pages/admin/SystemTab.tsx:79-86` — Config editor silently overwritten by refetch

### Frontend
- **P2-50** `endpoints.ts:277-286` — Playlist/analytics exports bypass API client error handling
- **P2-51** `AnalyticsTab.tsx:174` — Audit log export uses plain anchor tag
- **P2-52** `usePlayerPageState.ts:123` — el.src set synchronously before HLS check
- **P2-53** `usePlayerPageState.ts:225` — handleLoadedMetadata always auto-plays
- **P2-54** `useEqualizer.ts:141-144` — createMediaElementSource double-call risk
- **P2-55** `api/handlers/auth.go:119-127` — Inconsistent cookie-clearing strategies
- **P2-56** `api/handlers/playlists.go:130-153` — UpdatePlaylist/DeletePlaylist return 403 for all errors
- **P2-57** `api/handlers/suggestions.go:155` — RecordRating has no validation on rating value
- **P2-58** `api/handlers (multiple)` — Background goroutines use context.Background()

---

## P3 — LOW: Cleanup / style (11)

- **P3-1** Multiple repositories — json.Marshal/Unmarshal errors silently ignored
- **P3-3** `cmd/server/main.go:397-438` — "metadata-cleanup" task duplicates "media-scan"
- **P3-4** `internal/server/server.go:47+215` — HealthReporter populated but never queried
- **P3-5** `api/handlers/handler.go:326-339` — logAdminAction: callers pass dead UserID/Username
- **P3-7** `web/frontend/src/components/AudioPlayer.tsx:48` — useEqualizer return value discarded
- **P3-8** `internal/admin/admin.go:172-227` — ExportAuditLog loads up to 100K rows into memory
- **P3-9** `internal/upload/upload.go:546-553` — writeChunkAndTrack locks mutex on every 32KB chunk
- **P3-10** `internal/receiver/receiver.go:439` — PushCatalog MediaCount wrong on incremental pushes
- **P3-11** `internal/categorizer/categorizer.go:327-339` — detectAnime false positives
- **P3-12** `internal/server/signals_{unix,windows}.go` — No second-signal forced exit
- **P3-14** `cmd/server/main.go:628-646` — Health check doesn't check uploads/music/thumbnails/hls_cache dirs
- **P3-15** `cmd/server/main.go:174 + server.go:68` — thumbnails is critical but depends on optional ffmpeg
- **P3-16** `internal/analytics/stats.go:55-62` — UniqueUsers not reconstructed from DB

---

*Report generated by deep-debug-audit skill — 2026-03-14*
*All 44 fixes code-verified — 2026-03-14*
*Remaining issues reprioritized with promotions from Additional Findings — 2026-03-14*
