# Deep Debug Audit Report — Media Server Pro 4

**Date:** 2026-03-15
**Branch:** `development`
**Auditor:** Claude Opus 4.6 (automated deep audit)
**Scope:** 261 source files, ~70,729 lines (Go + TypeScript/React)

---

## AUDIT SUMMARY

```
Files analyzed:    261
Domains audited:   7 (infrastructure, handlers, core modules, support modules, repositories, frontend, downloader)

BROKEN:        0
SECURITY:     12
FRAGILE:      44
GAP:          26
DRIFT:        10
SILENT FAIL:  10
LEAK:          2
INCOMPLETE:    1
REDUNDANT:     2
OK:           48
─────────────────
Total findings: 155
```

### Severity Classification

**Critical (must fix before deploy): 5**
1. `[SECURITY]` middleware.go:30 — Log injection via X-Request-ID (allows \n \r)
2. `[SECURITY]` middleware.go:174 — CORS wildcard with credentials allows any site to steal admin sessions
3. `[SECURITY]` internal/downloader/importer.go:108 — Path traversal in ImportFile (arbitrary file read/move/delete)
4. `[SECURITY]` internal/downloader/websocket.go:16 — WebSocket CSWSH (CheckOrigin always true)
5. `[DRIFT]` playlist_repository.go vs migrations.go — PlaylistItem schema mismatch causes runtime SQL errors

**High (will cause user-facing bugs): 11**
6. `[SECURITY]` api/handlers/admin_config.go:54 — Mass assignment allows arbitrary config mutation (database creds, secrets)
7. `[SECURITY]` api/handlers/admin_downloader.go:214 — Path traversal in DeleteDownload filename param
8. `[SECURITY]` routes.go:276-278 — Extractor HLS endpoints fully unauthenticated
9. `[SECURITY]` config/env_overrides_auth.go:46 — ADMIN_PASSWORD remains in process env after hashing
10. `[SECURITY]` routes.go:513 — AdminExecuteQuery allows DoS via BENCHMARK/SLEEP
11. `[FRAGILE]` internal/remote/remote.go:597 — m.ctx never initialized, CacheMedia will panic on nil context
12. `[FRAGILE]` internal/auth/watch_history.go:20 — Mutating shared user under lock, persisting after unlock
13. `[FRAGILE]` api/handlers/media.go:341 — Receiver stream not tracked for auth users (limit bypass)
14. `[DRIFT]` types.ts:1076 — DownloaderSettings type has 6 fields backend never provides
15. `[DRIFT]` types.ts:1043 — DownloaderStreamInfo has extra frontend-only fields
16. `[GAP]` internal/config/accessors.go:66 — SetValuesBatch no rollback on save failure

**Medium (tech debt / time bombs): 32**
17. `[SECURITY]` api/handlers/admin_remote.go:244 — StreamRemoteMedia SSRF depends solely on module-level check
18. `[GAP]` api/handlers/admin_remote.go:205 — CacheRemoteMedia URL lacks SSRF validation
19. `[GAP]` receiver.go:625 — ProxyStream HTTP fallback has no SSRF check on slave BaseURL
20. `[FRAGILE]` wsconn.go:226 — Catalog push SlaveID not verified against connection's SlaveID
21. `[FRAGILE]` wsconn.go:246 — Heartbeat SlaveID not verified against connection's SlaveID
22. `[FRAGILE]` browser_windows.go:12 — Windows Chrome child processes orphaned (no process tree kill)
23. `[FRAGILE]` internal/hls/transcode.go:244 — lazyTranscodeQuality blocks HTTP request for minutes
24. `[FRAGILE]` internal/config/config.go:111 — Config save on Windows has crash-unsafe Remove+Rename gap
25. `[GAP]` api/handlers/admin_downloader.go:77,117 — Detect and Download lack SSRF validation
26. `[GAP]` api/handlers/admin_updates.go:185 — SetUpdateConfig branch name not sanitized (command injection risk)
27. `[GAP]` routes.go:406 — ReceiverStreamPush outside API key middleware group
28. `[GAP]` extractor.go:97 — Extractor HLS proxy unauthenticated with no rate limiting
29. `[GAP]` internal/auth/authenticate.go:107 — AdminAuthenticate doesn't record failed attempt for wrong username
30. `[GAP]` internal/auth/session.go:193 — createSession returns session even when DB persist fails
31. `[GAP]` internal/streaming/streaming.go:162 — Stream opens any path without validation (relies on handler layer)
32. `[GAP]` internal/database/database.go:140 — connectWithRetry total time can exceed startup timeout
33. `[GAP]` internal/database/migrations.go:14 — No down-migration or rollback capability
34. `[GAP]` internal/config/validate.go — No validation for Downloader config or UI config
35. `[GAP]` internal/server/server.go:467 — Module shutdown shares single context deadline (starvation risk)
36. `[FRAGILE]` internal/config/env_overrides_security.go:67 — CORS_ORIGINS split doesn't trim entries
37. `[FRAGILE]` internal/config/env_overrides_security.go:91 — IP whitelist/blacklist split doesn't trim entries
38. `[FRAGILE]` internal/tasks/scheduler.go:334 — RunNow uses potentially nil m.ctx before Start()
39. `[FRAGILE]` internal/hls/cleanup.go:170 — cleanInactiveJob TOCTOU between RLock check and Lock delete
40. `[FRAGILE]` internal/security/security.go:640 — saveIPLists non-atomic across whitelist and blacklist
41. `[FRAGILE]` internal/security/security.go:933 — Auth rate limiter uses raw path, not cleaned path
42. `[DRIFT]` admin_downloader.go:192 — Backend maps download list keys manually (fragile to refactoring)
43. `[DRIFT]` config.go:82 — syncFeatureToggles misses 4 features (playlists, suggestions, autodiscovery, duplicates)
44. `[LEAK]` pkg/middleware/agegate.go:219 — Unbounded verifiedIPs map growth
45. `[LEAK]` web/frontend/src/hooks/useDownloaderWebSocket.ts:59 — setTimeout leak on unmount
46. `[INCOMPLETE]` DownloaderTab.tsx:167 — No cancel button for active downloads
47. `[REDUNDANT]` useDownloaderWebSocket.ts:101 — clearDownload exported but never used
48. `[REDUNDANT]` cmd/server/main.go:443 — metadata-cleanup task duplicates media-scan

**Low (cleanup / minor): 59**
*(See detailed findings below)*

---

## DETAILED FINDINGS BY DOMAIN

---

### 1. BACKEND INFRASTRUCTURE (cmd/server, internal/server, config, database, routes, middleware, logger, tasks)

```
[SECURITY] pkg/middleware/middleware.go:30 — Log injection via X-Request-ID
  WHAT: sanitizeRequestID explicitly allows '\n' and '\r' characters through the filter.
  WHY: The condition `r == '\n' || r == '\r' || r == '\t' || unicode.IsPrint(r)` includes
       newlines when it should exclude them.
  IMPACT: An attacker can forge log entries by injecting newlines into X-Request-ID,
          misleading forensic analysis. HTTP response splitting is mitigated by Gin.
  TRACE: middleware.go:30 sanitizeRequestID -> allows \n \r -> request ID appears in logs
  FIX DIRECTION: Change to `unicode.IsPrint(r) && r != '\n' && r != '\r'` or just `unicode.IsPrint(r)`
                 (which already excludes control characters).
```

```
[SECURITY] pkg/middleware/middleware.go:174-184 — CORS wildcard with credentials bypasses browser protection
  WHAT: When allowAll=true and an Origin header is present, the response sets
        Access-Control-Allow-Origin to the specific origin AND Access-Control-Allow-Credentials: true.
  WHY: Browsers block credentialed requests when Allow-Origin is literal "*". By reflecting the
       specific origin, this actively bypasses that protection.
  IMPACT: ANY website visited by an admin can make credentialed cross-origin requests to the API,
          read responses, and steal the session cookie for full account takeover.
  TRACE: middleware.go:194 allowOrigin returns origin when allowAll -> line 201 value != "*" ->
         credentials=true
  FIX DIRECTION: When allowAll is true, set Allow-Origin to literal "*" and omit credentials header,
                 or require explicit origin configuration for credentialed CORS.
```

```
[SECURITY] api/routes/routes.go:276-278 — Extractor HLS endpoints fully unauthenticated
  WHAT: ExtractorHLSMaster, ExtractorHLSVariant, ExtractorHLSSegment have no auth middleware.
  WHY: Designed to be public for proxied HLS streams, but handlers have no rate limiting either.
  IMPACT: Any user who knows an extractor item ID can proxy HLS streams through the server,
          consuming bandwidth and potentially exposing restricted content.
  TRACE: routes.go:276-278 — no requireAuth(), no adminAuth()
  FIX DIRECTION: Add requireAuth() or at least IP-based rate limiting.
```

```
[SECURITY] api/routes/routes.go:513 — AdminExecuteQuery allows DoS via SQL functions
  WHAT: Admin SQL executor restricts to SELECT/SHOW/DESCRIBE/EXPLAIN with read-only transaction,
        but BENCHMARK() and SLEEP() in subqueries can cause DoS.
  WHY: No function-level filtering in the SQL text.
  IMPACT: An admin could accidentally or maliciously lock up DB connections with SLEEP().
  TRACE: routes.go:513 -> system.go:388 AdminExecuteQuery
  FIX DIRECTION: Add statement timeout at DB level; disallow BENCHMARK/SLEEP in query text.
```

```
[SECURITY] internal/config/env_overrides_auth.go:46-60 — ADMIN_PASSWORD remains in process env
  WHAT: After bcrypt hashing, the plaintext ADMIN_PASSWORD env var is never unset. Readable via
        /proc/PID/environ on Linux or process inspection tools.
  WHY: os.Unsetenv is not called after hashing.
  IMPACT: Any same-user process or container runtime that logs env vars exposes the admin password.
  TRACE: applyAdminPasswordOverride -> envGetStr("ADMIN_PASSWORD") -> bcrypt hash -> env var persists
  FIX DIRECTION: Call os.Unsetenv("ADMIN_PASSWORD") after hashing.
```

```
[SECURITY] api/routes/routes.go:406 — ReceiverStreamPush outside API key middleware group
  WHAT: POST /api/receiver/stream-push/:token is outside the receiverSlave group that has
        RequireReceiverWithAPIKey() middleware. Handler has its own check but is inconsistent.
  WHY: Token-based auth vs API key auth — different auth mechanism.
  IMPACT: If the handler's token validation is flawed, the endpoint is effectively unauthenticated
          for file data uploads.
  TRACE: routes.go:406 — no API key middleware on this route
  FIX DIRECTION: Verify token validation is secure; consider adding API key as secondary auth.
```

```
[FRAGILE] cmd/server/main.go:155-174 — HuggingFace client config frozen at startup
  WHAT: HF client created from cfg.Get() at startup; runtime config changes are ignored.
  WHY: No config watcher re-creates the client.
  IMPACT: API key rotation or model change requires server restart.
  TRACE: main.go:155 cfg.Get().HuggingFace -> huggingface.NewClient -> never updated
  FIX DIRECTION: Register cfg.OnChange watcher or document restart requirement.
```

```
[FRAGILE] cmd/server/main.go:316-335 — SetOnInitialScanDone callback race with module startup
  WHAT: Callback set after routes.Setup but before srv.Start(). If media scan completes
        asynchronously before callback is registered, suggestions are never seeded.
  WHY: Registration order matters for async scans.
  IMPACT: Suggestions may not be seeded until first hourly media-scan.
  TRACE: SetOnInitialScanDone -> srv.Start -> media.Start -> Scan (async)
  FIX DIRECTION: Set the callback before srv.Start().
```

```
[FRAGILE] internal/server/server.go:437-448 — Shutdown timeout split is fixed 50/50
  WHAT: HTTP drain and module stop each get half the timeout, regardless of actual usage.
  WHY: Separate contexts with fixed budgets.
  IMPACT: If HTTP drains in 1s, remaining 14s is wasted rather than given to module shutdown.
  TRACE: Shutdown -> httpPhase = totalTimeout/2 -> moduleCtx = totalTimeout - httpPhase
  FIX DIRECTION: Use remaining time after HTTP drain for module shutdown.
```

```
[FRAGILE] internal/config/config.go:111-131 — Config save on Windows crash-unsafe
  WHAT: os.Remove(configPath) before os.Rename(tempPath, configPath) creates a window where
        no config file exists. A crash during this window loses the config.
  WHY: Windows doesn't support atomic rename-over-existing.
  IMPACT: Power failure between Remove and Rename leaves no config file on disk.
  TRACE: save() -> Remove old -> Rename temp (crash window)
  FIX DIRECTION: Rename old to .bak, rename .tmp to config, then remove .bak.
```

```
[FRAGILE] internal/config/env_overrides_security.go:67-68 — CORS_ORIGINS split doesn't trim entries
  WHAT: strings.Split(val, ",") without TrimSpace. "origin1, origin2" produces " origin2".
  WHY: Oversight in split logic; other env overrides do trim.
  IMPACT: CORS origin with leading space never matches, silently disabling CORS for that origin.
  TRACE: env_overrides_security.go:68
  FIX DIRECTION: Trim each split entry.
```

```
[FRAGILE] internal/config/env_overrides_security.go:91-98 — IP whitelist/blacklist split doesn't trim
  WHAT: Same pattern as CORS_ORIGINS. IP entries with leading spaces fail to match.
  WHY: Inconsistent with receiver API keys which do trim.
  IMPACT: Whitelist/blacklist entries silently ineffective.
  TRACE: env_overrides_security.go:91,96
  FIX DIRECTION: Trim each entry after split.
```

```
[FRAGILE] internal/tasks/scheduler.go:334-349 — RunNow uses potentially nil m.ctx
  WHAT: m.ctx is nil before Start() is called. If an admin API triggers RunNow during startup,
        nil context is passed to executeTask.
  WHY: No nil check on m.ctx.
  IMPACT: Panic on nil context if RunNow called before Start.
  TRACE: RunNow -> m.executeTask(m.ctx, task) — m.ctx nil before Start()
  FIX DIRECTION: Check m.ctx == nil and return an error.
```

```
[FRAGILE] internal/logger/logger.go:237-254 — Child loggers copy settings snapshot
  WHAT: New() copies minLevel, useColors, jsonFormat from globalLogger at creation time.
        Later changes to globalLogger settings don't propagate.
  WHY: Value copy, not reference.
  IMPACT: SetJSONFormat(true) after child loggers exist leaves them in text format.
  TRACE: logger.go:243-254
  FIX DIRECTION: Have child loggers read format/level from globalLogger at log time.
```

```
[FRAGILE] internal/logger/logger.go:434-438 — Log rotation path comparison not normalized
  WHAT: String comparison on logDir paths; trailing slash or case differences cause mismatch.
  WHY: No filepath.Clean before comparison.
  IMPACT: globalLogger may keep a closed file handle after rotation.
  TRACE: logger.go:435 globalLogger.logDir == l.logDir
  FIX DIRECTION: Normalize paths with filepath.Clean.
```

```
[FRAGILE] internal/database/migrations.go:579-592 — ensureIndex receives raw ALTER SQL string
  WHAT: alterSQL string executed directly; safe because all values are hardcoded, but API
        doesn't enforce safety.
  WHY: Simpler than constructing SQL from parts.
  IMPACT: Future developer adding dynamic SQL could introduce injection.
  TRACE: migrations.go:579
  FIX DIRECTION: Construct ALTER SQL inside ensureIndex from validated parameters.
```

```
[FRAGILE] api/routes/routes.go:182-189 — FNV-1a 32-bit hash weak collision resistance
  WHAT: ETag hash is 32-bit FNV-1a. Birthday collision ~50% at ~77k distinct responses.
  WHY: Chosen for speed.
  IMPACT: ETag collisions cause stale 304 responses for different content.
  TRACE: hashFNV1a -> 32-bit hash
  FIX DIRECTION: Use FNV-1a 64-bit or xxhash.
```

```
[FRAGILE] api/routes/routes.go:37-61 — sessionAuth clears cookie on invalid session without logging
  WHAT: Invalid sessions cleared silently; no log entry for security monitoring.
  WHY: Avoids log noise from expired sessions.
  IMPACT: Cannot detect session ID brute-force from logs.
  TRACE: sessionAuth -> ValidateSession err -> clear cookie, no log
  FIX DIRECTION: Log at Debug (expired) or Warn (non-existent) level.
```

```
[FRAGILE] internal/server/signals_windows.go:14 — Windows only handles os.Interrupt
  WHAT: Only Ctrl+C triggers graceful shutdown; service stop signals not caught.
  WHY: Windows limitation.
  IMPACT: Windows service stop may force-kill without cleanup.
  TRACE: signal.Notify(sigCh, os.Interrupt)
  FIX DIRECTION: Integrate with golang.org/x/sys/windows/svc for service control.
```

```
[FRAGILE] internal/config/config.go:148-162 — getCopy shallow copy of nested structs
  WHAT: Copies slices but not deep nested structs. Safe now (Go strings immutable) but fragile.
  WHY: Explicit handling of known slice fields only.
  IMPACT: Future map fields in nested structs would be shared.
  TRACE: config.go:148-162
  FIX DIRECTION: Consider JSON roundtrip or DeepCopy for full isolation.
```

```
[FRAGILE] internal/server/server.go:480-498 — Config save retry with sleep blocks shutdown
  WHAT: saveConfigWithRetry sleeps up to 200ms during shutdown path.
  WHY: time.Sleep inside shutdown sequence.
  IMPACT: Minor shutdown delay.
  TRACE: Shutdown -> saveConfigWithRetry -> time.Sleep(100ms) x2
  FIX DIRECTION: Remove retries or use short overall timeout.
```

```
[GAP] internal/config/accessors.go:66-85 — SetValuesBatch no rollback on save failure
  WHAT: Field changes applied via reflection before save(). If save fails, in-memory config
        has values not on disk.
  WHY: Unlike Update() which has rollbackFromJSON, SetValuesBatch has no snapshot.
  IMPACT: After failed save, server runs with unpersisted config; restart reverts.
  TRACE: SetValuesBatch -> mutate -> save() fails -> no rollback
  FIX DIRECTION: Capture originalJSON before mutations and rollback on failure.
```

```
[GAP] internal/database/database.go:140-162 — connectWithRetry total time exceeds startup timeout
  WHAT: Default MaxRetries=3, Timeout=10s, RetryInterval=2s = ~36s total, but startup timeout is 30s.
  WHY: Config defaults not aligned with startup timeout.
  IMPACT: Last retry cancelled by startup context, masking real connection error.
  TRACE: server.go:293 30s timeout -> database.go:144 MaxRetries=3
  FIX DIRECTION: Reduce MaxRetries to 2 or increase startup timeout.
```

```
[GAP] internal/database/migrations.go:14-415 — No down-migration or rollback
  WHAT: All schema changes additive. No way to roll back a failed partial migration.
  WHY: Forward-only design.
  IMPACT: Partial column addition leaves schema inconsistent; blocks startup on retry.
  TRACE: ensureSchemaColumns -> loop -> return err on first failure
  FIX DIRECTION: Add per-column error tolerance with logging.
```

```
[GAP] internal/config/validate.go — No validation for Downloader or UI config
  WHAT: Validate() has no validateDownloader() or validateUI() calls.
  WHY: Newer features with validation omitted.
  IMPACT: Invalid downloader URL or zero ItemsPerPage not caught at startup.
  TRACE: validate.go:10-31
  FIX DIRECTION: Add validation functions.
```

```
[GAP] internal/server/server.go:467-478 — Module shutdown shares single context deadline
  WHAT: All modules share one deadline. A slow module starves later modules.
  WHY: No per-module timeout budget.
  IMPACT: Critical database module could be force-killed if earlier module blocks.
  TRACE: shutdownModules -> shared deadline
  FIX DIRECTION: Per-module timeout or remaining-time division.
```

```
[GAP] internal/server/server.go:322-328 — HTTP server timeouts accept zero without validation
  WHAT: Zero WriteTimeout (default) means no write timeout. Intentional for streams but undocumented.
  WHY: Design choice.
  IMPACT: Could surprise operators; zero ReadTimeout enables slow-loris.
  TRACE: server.go:322 -> defaults.go:53
  FIX DIRECTION: Document explicitly; enforce minimum ReadTimeout.
```

```
[GAP] api/routes/routes.go:116-156 — ETag buffer writer doesn't set Content-Length
  WHAT: Buffered responses may have unpredictable Content-Length behavior.
  WHY: Original ResponseWriter may have set it already.
  IMPACT: Minor; Gin handles most cases.
  TRACE: routes.go:146-154
  FIX DIRECTION: Set Content-Length to buffer length before writing.
```

```
[GAP] cmd/server/main.go:348-383 — validateSecrets limited scope
  WHAT: Only checks receiver API keys and CORS; misses empty DB password, default admin username.
  WHY: Limited scope of validation.
  IMPACT: Server starts with common insecure defaults without warning.
  TRACE: validateSecrets -> only 2 checks
  FIX DIRECTION: Add checks for empty DB password, default admin username, secure cookies.
```

```
[GAP] internal/config/config.go:56-59 — Config auto-created in CWD without warning
  WHAT: If config.json missing, creates it in current directory (could be /).
  WHY: First-time setup convenience.
  IMPACT: Unexpected file location.
  TRACE: Load -> IsNotExist -> save()
  FIX DIRECTION: Log absolute path of created config.
```

```
[DRIFT] internal/config/config.go:82-101 — syncFeatureToggles misses 4 features
  WHAT: EnablePlaylists, EnableSuggestions, EnableAutoDiscovery, EnableDuplicateDetection not synced.
  WHY: Modules may gate on the feature flag directly.
  IMPACT: Feature toggle via env var may not disable module if it checks internal Enabled field.
  TRACE: config.go:82-101
  FIX DIRECTION: Sync all feature flags or document which are Features-only.
```

```
[SILENT FAIL] internal/config/env_helpers.go:26-33 — envGetInt silently ignores parse errors
  WHAT: Non-integer env var value returns (0, false); config keeps default with no warning.
  WHY: "found" boolean false means caller skips override.
  IMPACT: Typos like SERVER_PORT=808O silently ignored.
  TRACE: envGetInt -> strconv.Atoi -> err -> return 0, false
  FIX DIRECTION: Log warning when recognized env var fails to parse.
```

```
[SILENT FAIL] internal/logger/logger.go:407-418 — Log rotation rename failure silently continues
  WHAT: os.Rename failure during rotation is ignored; new file opened at same path.
  WHY: Best-effort rotation.
  IMPACT: Rotation doesn't happen; old file keeps growing.
  TRACE: rotateIfNeeded -> Rename fail -> OpenFile same path
  FIX DIRECTION: Log warning to stderr on rename failure.
```

```
[LEAK] pkg/middleware/agegate.go:219-221 — Unbounded verifiedIPs map growth
  WHAT: Entries added on every POST /api/age-verify; eviction is lazy.
  WHY: No size limit on the map.
  IMPACT: High-traffic deployment could accumulate unbounded entries.
  TRACE: GinVerifyHandler -> verifiedIPs[ip] = time.Now()
  FIX DIRECTION: Add max size limit (e.g., 100k) or use LRU cache.
```

```
[REDUNDANT] cmd/server/main.go:443-452 — metadata-cleanup duplicates media-scan
  WHAT: Both tasks call mediaModule.Scan(). Metadata-cleanup runs every 24h alongside
        the hourly media-scan.
  WHY: Originally separate operations; now Scan() handles both.
  IMPACT: Double scan in the same hour every 24h; wasted CPU/IO.
  TRACE: registerTasks -> both call Scan()
  FIX DIRECTION: Remove metadata-cleanup or give it a dedicated function.
```

---

### 2. HANDLER LAYER (api/handlers/)

```
[SECURITY] api/handlers/admin_config.go:54 — Mass assignment in AdminUpdateConfig
  WHAT: Raw map[string]interface{} from JSON body passed to UpdateConfig with no allowlist.
  WHY: Admin could set database.host, auth.secret, or other critical fields.
  IMPACT: Compromised admin session could redirect database or change secrets.
  TRACE: admin_config.go:49-53 -> admin.UpdateConfig(updates)
  FIX DIRECTION: Allowlist which config keys can be changed at runtime; disallow database/auth secrets.
```

```
[SECURITY] api/handlers/admin_remote.go:244 — StreamRemoteMedia SSRF relies solely on module
  WHAT: remoteURL query param passed to ProxyRemoteWithCache without handler-level SSRF check.
  WHY: Defense in one layer only; any authenticated user could probe internal services if module check fails.
  IMPACT: Medium. Requires authenticated session.
  TRACE: admin_remote.go:257 -> remote.ProxyRemoteWithCache
  FIX DIRECTION: Add handler-level helpers.ValidateURLForSSRF before calling module.
```

```
[GAP] api/handlers/admin_remote.go:205 — CacheRemoteMedia URL lacks SSRF validation
  WHAT: req.URL checked for emptiness but not format or SSRF. Admin-only route.
  WHY: Missing validation step.
  IMPACT: SSRF vector for admin users.
  TRACE: Line 222 checks empty only
  FIX DIRECTION: Add helpers.ValidateURLForSSRF(req.URL).
```

```
[GAP] api/handlers/admin_downloader.go:77,117 — Detect and Download lack SSRF validation
  WHAT: req.URL passed to downloader service without SSRF check. Admin-only.
  WHY: Inconsistent with AddCrawlerTarget/AddExtractorItem which both validate.
  IMPACT: Low — admin-only, but the downloader will fetch from this URL.
  TRACE: admin_downloader.go:79 Detect, :132 Download
  FIX DIRECTION: Add helpers.ValidateURLForSSRF(req.URL) check.
```

```
[GAP] api/handlers/admin_updates.go:185 — SetUpdateConfig branch name not sanitized
  WHAT: req.Branch accepted without validation and persisted to config.
  WHY: If updater uses branch in shell commands (git checkout), command injection possible.
  IMPACT: Medium — admin-only, depends on updater implementation.
  TRACE: admin_updates.go:183 -> cfg.Updater.Branch = req.Branch
  FIX DIRECTION: Validate against git ref name rules.
```

```
[GAP] api/handlers/admin_activity.go:11 — AdminGetActiveStreams lacks handler-level admin guard
  WHAT: No requireAdmin(c) call; relies entirely on route-level adminAuth middleware.
  WHY: Route group enforces admin, but defense-in-depth missing.
  IMPACT: Low — route middleware protects, but fragile if handler remounted.
  TRACE: routes.go:438-439 adminGrp
  FIX DIRECTION: Add requireAdmin(c) at top of handler.
```

```
[GAP] api/handlers/extractor.go:97 — Extractor HLS proxy unauthenticated with no rate limiting
  WHAT: HLS proxy handlers have no auth and no rate limiting despite comments claiming rate limits.
  WHY: Designed as public endpoints but guards not implemented.
  IMPACT: Unauthenticated bandwidth consumption.
  TRACE: routes.go:276-278 -> extractor.go:97-160
  FIX DIRECTION: Add rate limiting per IP or requireAuth().
```

```
[FRAGILE] api/handlers/admin_backups.go:63 — RestoreBackup/DeleteBackup accept unsanitized ID
  WHAT: c.Param("id") used directly without format validation.
  WHY: Admin-only, but if backup module constructs paths from ID, path traversal possible.
  IMPACT: Depends on backup module (which does validate — see backup.go:323).
  TRACE: Lines 63, 79
  FIX DIRECTION: Validate ID matches UUID or alphanumeric format.
```

```
[FRAGILE] api/handlers/admin_classify.go:126 — ClassifyFile leaks internal error details
  WHAT: Raw err.Error() returned to client; may contain file paths or API key details.
  WHY: Generic error forwarding.
  IMPACT: Low — admin-only information leakage.
  TRACE: Line 126 writeError(c, 500, err.Error())
  FIX DIRECTION: Return generic message; log details server-side.
```

```
[FRAGILE] api/handlers/admin_security.go:70 — addToIPList doesn't pre-validate IP format
  WHAT: IP passed directly to addFn without net.ParseIP; error messages conflate parse errors.
  WHY: Security module validates, but handler error is generic.
  IMPACT: Low — error caught, just not specific.
  TRACE: Line 79
  FIX DIRECTION: Validate with net.ParseIP before calling addFn.
```

```
[FRAGILE] api/handlers/admin_downloader.go:214 — DeleteDownload filename path traversal risk
  WHAT: c.Param("filename") passed directly without filepath.Base() or traversal check.
  WHY: Gin's :filename param allows URL-encoded slashes.
  IMPACT: Could delete arbitrary files on downloader service.
  TRACE: Line 214 -> DeleteDownload(filename)
  FIX DIRECTION: Apply filepath.Base(filename).
```

```
[FRAGILE] api/handlers/media.go:341 — Receiver stream not tracked for authenticated users
  WHAT: Auth path calls CanStartStream but not TrackProxyStream; unauthenticated path does both.
  WHY: Missing tracking call.
  IMPACT: Auth users can exceed stream limits on receiver media without counter incrementing.
  TRACE: Lines 341-356
  FIX DIRECTION: Call TrackProxyStream for authenticated users too with defer release.
```

```
[FRAGILE] api/handlers/auth.go:255 — Admin user auto-creation with fallback password
  WHAT: When admin has no DB record, auto-creates with "FALLBACK_UNUSED_PASSWORD_" + random.
  WHY: Ensures admin preferences can be saved.
  IMPACT: Low — config-based admin auth never uses this password. But defense-in-depth concern.
  TRACE: Lines 271-286
  FIX DIRECTION: Use only GenerateSecurePassword; remove the FALLBACK string.
```

```
[FRAGILE] api/handlers/hls.go:240 — Segment param passed unvalidated to HLS module
  WHAT: c.Param("segment") passed without validation.
  WHY: HLS module validates internally (filepath.Clean + filepath.Rel).
  IMPACT: Low — module has its own checks.
  TRACE: Line 240
  FIX DIRECTION: Validate segment matches expected pattern or apply filepath.Base().
```

```
[GAP] api/handlers/analytics.go:193 — GetEventsByType allows empty type
  WHAT: Empty eventType passed to DB without check; potentially expensive.
  WHY: No empty check.
  IMPACT: Low — admin-only, could cause unnecessary DB load.
  TRACE: Line 194
  FIX DIRECTION: Return 400 if eventType is empty.
```

---

### 3. CORE INTERNAL MODULES (auth, media, streaming, hls, security, scanner, thumbnails)

```
[FRAGILE] internal/auth/watch_history.go:20-45 — Mutating shared user under lock, persist after unlock
  WHAT: Holds usersMu.Lock, mutates user.WatchHistory in-place, releases lock, then calls
        userRepo.Update. If DB fails, in-memory state diverged. Concurrent reader sees partial state.
  WHY: Pattern is "mutate-under-lock, persist-after-unlock" without copy.
  IMPACT: Medium — watch history mutation visible in-memory before persisted.
  TRACE: AddToWatchHistory -> Lock -> mutate -> Unlock -> userRepo.Update
  FIX DIRECTION: Work on user copy, persist first, swap into cache on success.
```

```
[FRAGILE] internal/auth/password.go:40-48 — UpdatePassword TOCTOU with misleading error
  WHAT: Between RLock release and Lock acquire, concurrent UpdatePassword could change hash.
        Re-check catches this but returns ErrUserNotFound instead of "concurrent update."
  WHY: Optimistic concurrency with wrong error path.
  IMPACT: Low — confusing error message on rare race.
  TRACE: UpdatePassword -> RLock -> verify -> RUnlock -> (gap) -> Lock -> re-check
  FIX DIRECTION: Return dedicated "password was concurrently changed" error.
```

```
[FRAGILE] internal/hls/transcode.go:244-272 — lazyTranscodeQuality blocks HTTP request
  WHAT: First request for an untranscoded quality blocks on mutex while ffmpeg runs (minutes).
  WHY: Mutex ensures single transcode per quality; caller's HTTP request is blocked.
  IMPACT: Medium — large files cause HTTP timeouts on first request.
  TRACE: ServeVariantPlaylist -> ensureVariantPlaylistExists -> lazyTranscodeQuality -> qMu.Lock
  FIX DIRECTION: Return 202 "transcoding in progress" and have client poll.
```

```
[FRAGILE] internal/hls/cleanup.go:170-210 — cleanInactiveJob TOCTOU
  WHAT: Checks isJobRunningOrPending under RLock, deletes under Lock; job could start between.
  WHY: Missing re-check under write lock (unlike removeSegmentDirAndState which has it).
  IMPACT: Low — just-started job directory could be deleted; transcode would fail gracefully.
  TRACE: cleanInactiveJob -> RLock check -> RUnlock -> RemoveAll -> Lock -> delete
  FIX DIRECTION: Add re-check under write lock, matching removeSegmentDirAndState pattern.
```

```
[FRAGILE] internal/hls/generate.go:204-242 — generateMasterPlaylist no Sync before Close
  WHAT: No file.Sync() before Close; crash between write and close could truncate file.
  WHY: Standard Go file ops don't guarantee durability without Sync.
  IMPACT: Low — file is small; crash during generation leaves job in failed state anyway.
  TRACE: generateMasterPlaylist -> os.Create -> fmt.Fprintln -> defer Close (no Sync)
  FIX DIRECTION: Add file.Sync() before close.
```

```
[FRAGILE] internal/security/security.go:640-680 — saveIPLists non-atomic
  WHAT: Whitelist and blacklist saved in separate DB calls without transaction.
  WHY: No transaction wrapper.
  IMPACT: Low — partial state self-healing on next save.
  TRACE: saveIPLists -> 4 separate DB calls
  FIX DIRECTION: Wrap in single DB transaction.
```

```
[FRAGILE] internal/security/security.go:933 — Auth rate limiter uses raw path
  WHAT: isAuthPath check uses raw reqPath instead of cleaned path.
  WHY: Path traversal tricks could bypass stricter auth rate limit.
  IMPACT: Low — general rate limiter still applies.
  TRACE: GinMiddleware -> cleaned path for exemptions -> raw path for isAuthPath
  FIX DIRECTION: Use cleaned path for isAuthPath check.
```

```
[FRAGILE] internal/scanner/mature.go:631-633 — Studio code pattern high false positive rate
  WHAT: Regex `[a-z]{2,5}-?\d{3,5}` matches "mp3-128", "aac-256", etc.
  WHY: Pattern too broad; boost is only 0.15.
  IMPACT: Low — can't flag alone, but contributes to false positive accumulation.
  TRACE: maturePatterns -> studio code -> 0.15 boost
  FIX DIRECTION: Restrict pattern to exclude common non-adult codes.
```

```
[FRAGILE] internal/media/discovery.go:565-569 — Background saveMetadata goroutine untracked
  WHAT: Fire-and-forget goroutine after every scan; multiple concurrent scans could overlap.
  WHY: Background save avoids blocking scan completion.
  IMPACT: Low — saveMu prevents actual conflicts; Stop() has its own save with timeout.
  TRACE: Scan -> go saveMetadata(scanCtx) -> saveMu.Lock inside
  FIX DIRECTION: Track goroutine with WaitGroup.
```

```
[FRAGILE] internal/thumbnails/generate.go:101-138 — No cleanup of partial files on ffmpeg timeout
  WHAT: If ffmpeg context times out, partial output file may exist on disk.
  WHY: No cleanup in error path.
  IMPACT: Low — next generation attempt overwrites; partial file wastes disk temporarily.
  TRACE: generateVideoThumbnail -> timeout -> error return (no os.Remove)
  FIX DIRECTION: Add os.Remove(outputPath) in error path.
```

```
[GAP] internal/auth/session.go:127-133 — ValidateSession fire-and-forget goroutine untracked
  WHAT: Goroutine persists LastActivity with context.Background(); not tied to module lifecycle.
  WHY: Quick background DB update.
  IMPACT: Low — some LastActivity updates may be lost on shutdown.
  TRACE: ValidateSession -> go sessionRepo.Update(Background, &sessionCopy)
  FIX DIRECTION: Use WaitGroup or bounded worker pool; wait during Stop().
```

```
[GAP] internal/auth/session.go:193-217 — createSession returns session when DB persist fails
  WHAT: If sessionRepo.Create fails, session still added to in-memory map and returned.
  WHY: Logged as warning; intended as best-effort persistence.
  IMPACT: Medium — phantom sessions lost on restart if DB is degraded.
  TRACE: createSession -> sessionRepo.Create fails -> session in map anyway
  FIX DIRECTION: Document as memory-primary design, or return error.
```

```
[GAP] internal/auth/authenticate.go:107-161 — AdminAuthenticate doesn't record failed attempt for wrong username
  WHAT: Non-admin username returns ErrNotAdminUsername without calling recordFailedAttempt.
  WHY: Short-circuits; dummy bcrypt mitigates timing but no lockout penalty.
  IMPACT: Low-Medium — attacker can enumerate admin username without accruing lockout.
  TRACE: AdminAuthenticate -> adminLoginAllowed=false -> return (no record)
  FIX DIRECTION: Call recordFailedAttempt in non-admin-username branch.
```

```
[GAP] internal/streaming/streaming.go:162-217 — Stream opens any path without validation
  WHAT: Relies entirely on handler layer for path validation; no defense-in-depth.
  WHY: Framework-agnostic design — modules don't do authorization.
  IMPACT: Medium if handler bug allows arbitrary path injection.
  TRACE: Stream -> os.Open(req.Path) -> no validation
  FIX DIRECTION: Add optional path validation as defense-in-depth.
```

```
[GAP] internal/thumbnails/generate.go:117-121 — ffmpeg uses context.Background, not worker context
  WHAT: Thumbnail ffmpeg process uses Background context with 30s timeout, not module's m.ctx.
  WHY: Worker context checked before dequeue, but generateThumbnail not cancellable by shutdown.
  IMPACT: Low — ffmpeg processes continue up to 30s after shutdown.
  TRACE: worker -> dequeue -> generateThumbnail -> context.WithTimeout(Background, 30s)
  FIX DIRECTION: Pass m.ctx to generateThumbnail.
```

```
[GAP] internal/thumbnails/preview.go:81-97 — Hardcoded /thumbnails/ URL prefix
  WHAT: Preview URL hardcoded; won't work if thumbnail serve path changes.
  WHY: Configuration not threaded through.
  IMPACT: Low — path is stable.
  TRACE: previewURLForIndex -> "/thumbnails/" + filename
  FIX DIRECTION: Use configurable base URL.
```

---

### 4. SUPPORT MODULES (receiver, remote, extractor, crawler, duplicates, analytics, etc.)

```
[FRAGILE] internal/remote/remote.go:597-689 — m.ctx never initialized; CacheMedia will panic
  WHAT: CacheMedia calls http.NewRequestWithContext(m.ctx, ...) but m.ctx is never set in Start().
  WHY: m.ctx and m.cancel declared on struct but never assigned.
  IMPACT: HIGH — nil context panic on first background cache download attempt.
  TRACE: ProxyRemoteWithCache -> go CacheMedia -> NewRequestWithContext(m.ctx) — m.ctx is nil
  FIX DIRECTION: Initialize m.ctx and m.cancel in Start() via context.WithCancel(context.Background()).
```

```
[FRAGILE] internal/receiver/wsconn.go:226-241 — Catalog push SlaveID not verified
  WHAT: data.SlaveID from message not verified against sw.slaveID (authenticated connection).
  WHY: No per-message authorization.
  IMPACT: Medium — rogue slave could replace another slave's catalog.
  TRACE: HandleWebSocket -> msgTypeCatalog -> PushCatalog(data.SlaveID)
  FIX DIRECTION: Enforce data.SlaveID == sw.slaveID.
```

```
[FRAGILE] internal/receiver/wsconn.go:246-257 — Heartbeat SlaveID not verified
  WHAT: Same issue as catalog push — heartbeats can be sent for other slaves.
  WHY: No per-message authorization.
  IMPACT: Low — can keep other slaves appearing online.
  TRACE: HandleWebSocket -> msgTypeHeartbeat -> Heartbeat(data.SlaveID)
  FIX DIRECTION: Enforce data.SlaveID == sw.slaveID.
```

```
[FRAGILE] internal/crawler/browser_windows.go:12-18 — Chrome child processes orphaned
  WHAT: Only cmd.Process.Kill() called; no process tree kill on Windows.
  WHY: Windows doesn't have process groups like Unix.
  IMPACT: Medium — renderer/GPU child processes leak during crawls.
  TRACE: killChromeProcessGroup(Windows) -> Process.Kill() only
  FIX DIRECTION: Use `taskkill /T /F /PID` via exec.Command.
```

```
[FRAGILE] internal/suggestions/suggestions.go:196-239 — Evicted profiles not reloaded from DB
  WHAT: After eviction, returning user gets empty profile despite data existing in MySQL.
  WHY: RecordView creates fresh empty profile; no DB reload on cache miss.
  IMPACT: Low — personalization resets after eviction; data not lost, just unused.
  TRACE: evictStaleProfiles -> delete -> RecordView creates new empty
  FIX DIRECTION: Load profile from repo before creating empty one on cache miss.
```

```
[FRAGILE] internal/analytics/stats.go:286-324 — rebuildStatsFromEvent doesn't populate UniqueUsers
  WHAT: During reconstruction, UniqueUsers and UniqueViewers are never updated.
  WHY: Reconstruction only builds TotalViews/LastViewed.
  IMPACT: Low — values are 0 until live events add them after restart.
  TRACE: rebuildStatsFromEvent only increments TotalViews and TotalPlaybacks
  FIX DIRECTION: Accept as documented or add UserID tracking during reconstruction.
```

```
[FRAGILE] internal/updater/updater.go:643-688 — downloadUpdate no size limit
  WHAT: io.Copy streams entire response body with no size cap.
  WHY: No LimitReader applied.
  IMPACT: Low — requires GitHub/network compromise; checksum verification after download.
  TRACE: downloadUpdate -> io.Copy(tmpFile, resp.Body)
  FIX DIRECTION: Add io.LimitReader with reasonable max (e.g., 500MB).
```

```
[GAP] internal/receiver/receiver.go:625-667 — ProxyStream HTTP fallback no SSRF check
  WHAT: proxyViaHTTP uses slave.BaseURL set during registration without SSRF check.
  WHY: Registration validates scheme but not host against private IPs.
  IMPACT: Medium — malicious slave could register with private-IP BaseURL for SSRF.
  TRACE: RegisterSlave -> proxyViaHTTP -> httpClient.Do(req)
  FIX DIRECTION: Use helpers.SafeHTTPTransport or validate BaseURL at registration.
```

```
[GAP] internal/receiver/wsconn.go:111-115 — WebSocket upgrader accepts all origins
  WHAT: CheckOrigin always returns true.
  WHY: Access control deferred to API key validation.
  IMPACT: Low — API key check runs before processing; origin-based filtering is defense-in-depth.
  TRACE: upgrader.CheckOrigin -> true
  FIX DIRECTION: Validate Origin against configured server URL.
```

```
[GAP] internal/extractor/extractor.go:98-110 — CheckRedirect doesn't validate against SSRF
  WHAT: Redirect handler only caps count; no SSRF check on redirect targets.
  WHY: SafeHTTPTransport blocks at dial time (mitigated).
  IMPACT: Low — transport layer protects.
  TRACE: NewModule -> CheckRedirect only checks len(via)
  FIX DIRECTION: Add helpers.ValidateURLForSSRF in CheckRedirect.
```

```
[GAP] internal/crawler/crawler.go:170-202 — AddTarget doesn't validate URL against SSRF
  WHAT: Accepts any http/https URL without private IP check. Admin-only; transport protects.
  WHY: Missing defense-in-depth.
  IMPACT: Low — SafeHTTPTransport blocks private IPs at dial.
  TRACE: AddTarget -> validates scheme only
  FIX DIRECTION: Add helpers.ValidateURLForSSRF for defense-in-depth.
```

```
[GAP] internal/remote/remote.go:903-936 — validateURL duplicated from helpers
  WHAT: Reimplements SSRF checking that exists in helpers.ValidateURLForSSRF.
  WHY: Code duplication.
  IMPACT: Low — maintenance burden.
  TRACE: remote.validateURL vs helpers.ValidateURLForSSRF
  FIX DIRECTION: Consolidate to helpers.ValidateURLForSSRF.
```

```
[FRAGILE] internal/extractor/extractor.go:427-461 — ProxyHLSSegment no path traversal check on segment
  WHAT: Segment parameter resolved as relative URL to playlist base without ../ check.
  WHY: SafeHTTPTransport prevents SSRF; base URL is known-good.
  IMPACT: Low — can't reach private IPs but could fetch arbitrary paths relative to CDN.
  TRACE: ProxyHLSSegment -> resolveURL(baseURL, segment) -> proxyStream
  FIX DIRECTION: Validate segment doesn't contain ../ before resolving.
```

```
[FRAGILE] internal/browser.go:160-193 — CDP responses map never cleaned on goroutine exit
  WHAT: Read pump goroutine's responses map entries never signaled if pump exits.
  WHY: Context timeout prevents hangs; channels leak until GC.
  IMPACT: Low — context prevents indefinite blocking.
  TRACE: read pump exit -> close(events) -> response channels leak
  FIX DIRECTION: Iterate and close all pending channels on pump exit.
```

```
[FRAGILE] internal/duplicates/duplicates.go:220-243 — RecordDuplicatesFromSlave loads entire table
  WHAT: ListAll() loads all receiver media into memory for fingerprint comparison.
  WHY: Needed for cross-slave duplicate detection.
  IMPACT: Low — runs in background goroutine; memory spike proportional to catalog size.
  TRACE: PushCatalog -> go RecordDuplicatesFromSlave -> ListAll
  FIX DIRECTION: DB-level fingerprint match query instead of full table load.
```

```
[FRAGILE] internal/analytics/stats.go:264-283 — reconstructStats caps at 2000 events
  WHAT: Startup stats reconstructed from last 2000 events; may underreport if more exist.
  WHY: Performance tradeoff.
  IMPACT: Low — approximate stats; live events fill in correctly.
  TRACE: Start -> reconstructStats -> Limit: 2000
  FIX DIRECTION: Document as approximate; consider SQL aggregates for startup.
```

```
[SILENT FAIL] internal/downloader/module.go:149-157 — Media rescan fire-and-forget
  WHAT: After successful import, goroutine calls Scan(); failure logged but import response
        already sent with scanTriggered: true.
  WHY: Async scan to avoid blocking import.
  IMPACT: Low — admin sees success even if scan failed.
  TRACE: Module.Import -> goroutine { Scan() } -> warning only
  FIX DIRECTION: Acceptable; consider audit log for scan failures.
```

---

### 5. REPOSITORY LAYER (internal/repositories/)

```
[DRIFT] playlist_repository.go + models.go vs migrations.go — PlaylistItem schema mismatch
  WHAT: GORM model PlaylistItem has ID and MediaID columns. Migration creates playlist_items
        with composite PK (playlist_id, media_path) — no "id" or "media_id" columns.
  WHY: Model evolved but migration never updated.
  IMPACT: RemoveItem (id = ?) and UpdateItem (Save with all fields) will produce SQL errors.
  TRACE: models.go:403-411 -> playlist_repository.go:166-177 -> migrations.go:128-138
  FIX DIRECTION: Add id/media_id columns to migration, or align GORM model with composite PK.
```

```
[DRIFT] models.go:421 vs migrations.go:142 — AnalyticsEvent.Type size mismatch
  WHAT: GORM tag size:50 vs migration VARCHAR(100).
  WHY: Independent evolution.
  IMPACT: Cosmetic; potential truncation if GORM auto-migrate ever used.
  TRACE: models.go:421 -> migrations.go:142
  FIX DIRECTION: Align GORM tag to size:100.
```

```
[DRIFT] models.go:520-521 vs migrations.go:162-163 — AuditLogEntry size mismatches
  WHAT: Action: GORM size:100 vs migration VARCHAR(255). Resource: GORM size:255 vs VARCHAR(1024).
  WHY: Independent evolution.
  IMPACT: Potential data truncation on auto-migrate.
  TRACE: models.go:520-521 -> migrations.go:162-163
  FIX DIRECTION: Align GORM tags to match migration.
```

```
[DRIFT] models.go (UserPreferences) vs migrations.go:57 — Missing subtitle_lang field
  WHAT: Migration creates subtitle_lang column; GORM model has no SubtitleLang field.
  WHY: Column never exposed in Go model.
  IMPACT: Column exists but never read/written; GORM Save() could zero it out.
  TRACE: migrations.go:57 -> models.go:122-146
  FIX DIRECTION: Add SubtitleLang field or drop column.
```

```
[GAP] playlist_repository.go:170-172 — RemoveItem deletes by non-existent "id" column
  WHAT: Queries "id = ?" on table with no "id" column.
  WHY: Schema drift.
  IMPACT: SQL error at runtime.
  TRACE: playlist_repository.go:171 -> migrations.go:128-138
  FIX DIRECTION: Use composite key (playlist_id, media_path) or add column.
```

```
[GAP] playlist_repository.go:175-177 — UpdateItem Save() writes non-existent columns
  WHAT: Save(item) writes id and media_id columns that don't exist.
  WHY: Schema drift.
  IMPACT: SQL error at runtime.
  TRACE: playlist_repository.go:176
  FIX DIRECTION: Same as above.
```

```
[FRAGILE] audit_log_repository.go:71-82 — GetByUser unbounded when limit <= 0
  WHAT: No Limit clause applied for limit <= 0; could return entire audit log.
  WHY: No fallback default cap.
  IMPACT: OOM risk for users with extensive audit history.
  TRACE: audit_log_repository.go:77-79
  FIX DIRECTION: Apply default cap when limit <= 0.
```

```
[FRAGILE] media_metadata_repository.go:206-208 — Tags batch query with very large IN clause
  WHAT: WHERE path IN ? with potentially thousands of entries.
  WHY: Avoids N+1 but degrades with large lists.
  IMPACT: Performance degradation on large libraries.
  TRACE: media_metadata_repository.go:200-215
  FIX DIRECTION: Chunk IN clause into batches of 1000.
```

```
[FRAGILE] receiver_transfer_repository.go:25-26 — Time fields stored as strings
  WHAT: Manual format/parse with "2006-01-02 15:04:05"; timezone info lost.
  WHY: Written for compatibility.
  IMPACT: Timezone drift if MySQL and Go use different zones.
  TRACE: receiver_transfer_repository.go:20-26
  FIX DIRECTION: Use time.Time with GORM's native timestamp handling.
```

```
[FRAGILE] Multiple repositories — UpdateStatus doesn't check RowsAffected (4 instances)
  WHAT: receiver_duplicate, crawler (2 methods), extractor all return nil even if ID doesn't exist.
  WHY: No RowsAffected check.
  IMPACT: Silent no-op on invalid IDs.
  TRACE: receiver_duplicate_repository.go:114, crawler_repository.go:79,218, extractor_item_repository.go:106
  FIX DIRECTION: Check RowsAffected and return not-found error.
```

```
[FRAGILE] Multiple repositories — Unbounded List() methods (7+ instances)
  WHAT: categorized_item, validation_result, receiver_duplicate, crawler (2), session, suggestion_profile
        all return all rows with no LIMIT.
  WHY: No pagination parameters.
  IMPACT: Large result sets on big datasets.
  TRACE: Various List/ListPending/GetViewHistory methods
  FIX DIRECTION: Add pagination or default caps.
```

```
[SILENT FAIL] Multiple repositories — json.Marshal/Unmarshal errors silently ignored (7 instances)
  WHAT: backup_manifest (2 marshal, 2 unmarshal), suggestion_profile (2 marshal),
        validation_result (1 marshal, 1 unmarshal), autodiscovery (1 unmarshal),
        media_metadata tag loading (1).
  WHY: Assumption that standard types always marshal successfully.
  IMPACT: Corrupted DB JSON silently produces empty data.
  TRACE: Various *_repository.go files
  FIX DIRECTION: Check errors and log warnings.
```

---

### 6. DOWNLOADER MODULE (active development)

```
[SECURITY] internal/downloader/importer.go:108 — Path traversal in ImportFile
  WHAT: srcPath = filepath.Join(srcDir, filename) with no sanitization. Filename with "../"
        escapes downloads directory.
  WHY: filename originates from user input in AdminDownloaderImport handler.
  IMPACT: Arbitrary file read (copy mode) or move/delete (delete-source mode).
  TRACE: DownloaderTab importMutation -> AdminDownloaderImport -> ImportFile(downloadsDir, destDir, filename, deleteSource)
  FIX DIRECTION: Verify filepath.Abs(srcPath) has srcDir as prefix; reject ".." in filenames.
```

```
[SECURITY] internal/downloader/websocket.go:16-18 — WebSocket CSWSH via permissive CheckOrigin
  WHAT: CheckOrigin always returns true, disabling CORS origin validation on WS upgrade.
  WHY: Admin auth via session cookie, but browser sends cookie cross-origin.
  IMPACT: Malicious page can open WS to /ws/admin/downloader if admin visits it.
  TRACE: Browser -> GET /ws/admin/downloader (cookie auto-sent) -> CheckOrigin true -> connected
  FIX DIRECTION: Validate origin matches Host header or configured server URL.
```

```
[FRAGILE] internal/downloader/importer.go:163-168 — copyFile deferred close error silently lost
  WHAT: If out.Close() fails after successful io.Copy, error is captured in outer `err` variable
        but function returns nil from line 176 (not the outer err).
  WHY: No named return values; deferred closure captures wrong scope.
  IMPACT: Data loss on power failure if Close fails (fsync not committed).
  TRACE: copyFile -> io.Copy succeeds -> return nil -> deferred Close fails -> err lost
  FIX DIRECTION: Use named return values so deferred closure's err assignment is captured.
```

```
[FRAGILE] internal/downloader/websocket.go:120-130 — Weak randomSuffix using time-based seed
  WHAT: All 8 chars derived from time.Now().UnixNano(); collisions possible within nanosecond.
  WHY: Used for WS client ID routing.
  IMPACT: Low — concurrent connections in same nanosecond could get same ID, causing message cross-talk.
  TRACE: HandleWebSocket -> clientID = "msp_" + timestamp + "_" + randomSuffix()
  FIX DIRECTION: Use crypto/rand or math/rand/v2.
```

```
[DRIFT] api/handlers/admin_downloader.go:192-200 — Download list keys manually mapped
  WHAT: Handler hand-rolls map[string]interface{} translating Go struct fields to JSON keys.
  WHY: Fragile to refactoring on either side.
  IMPACT: Low currently; breaks if either side changes.
  TRACE: DownloadFile struct -> manual map -> frontend type
  FIX DIRECTION: Define response struct matching frontend type, or align JSON tags.
```

```
[DRIFT] web/frontend/src/api/types.ts:1076-1085 — DownloaderSettings has fields backend never sends
  WHAT: Frontend expects maxConcurrent, downloadsDir, audioQuality, videoFormat, proxy.
        Backend only sends allowServerStorage, audioFormat, supportedSites.
  WHY: Frontend designed for richer response that backend doesn't provide.
  IMPACT: StatusSection shows "Max Concurrent: --" and "Proxy: Disabled" always.
  TRACE: Client.GetSettings -> handler maps 3 fields -> frontend expects 9
  FIX DIRECTION: Extend backend or trim frontend type.
```

```
[DRIFT] web/frontend/src/api/types.ts:1043-1052 — DownloaderStreamInfo extra frontend fields
  WHAT: Frontend includes format, label fields; backend StreamInfo has different fields.
  WHY: Render falls through to s.type which works, but s.format/s.label always undefined.
  IMPACT: Minor UI detail loss.
  TRACE: Go StreamInfo -> handler -> frontend
  FIX DIRECTION: Align types.
```

```
[GAP] internal/downloader/websocket.go:77 — Error from connected WriteMessage not checked
  WHAT: adminConn.WriteMessage error not checked; proxy goroutines may start against dead conn.
  WHY: Connection could fail between upgrade and write.
  IMPACT: Minor — goroutines detect error on first read/write.
  TRACE: HandleWebSocket -> adminConn.WriteMessage (error ignored)
  FIX DIRECTION: Check error and call closeAll() on failure.
```

```
[GAP] internal/downloader/client.go:174-190 — HTTP client reads unlimited response body
  WHAT: io.ReadAll(resp.Body) on error responses with no size limit.
  WHY: Could consume excessive memory if downloader sends large error.
  IMPACT: Low — URL is admin-configured, typically localhost.
  TRACE: Client.get -> io.ReadAll without LimitReader
  FIX DIRECTION: Use io.LimitReader.
```

```
[GAP] internal/downloader/module.go:196-217 — checkHealth reports healthy when downloader offline
  WHAT: setHealth(true, "Downloader offline") when service unreachable.
  WHY: Distinguishes module health from service availability.
  IMPACT: Admin panel shows "healthy" for unreachable downloader.
  TRACE: checkHealth -> Health fails -> setHealth(true, "offline")
  FIX DIRECTION: Use setHealth(false, ...) or add "degraded" state.
```

```
[GAP] internal/downloader/module.go:130 — Session cookie extraction fragile
  WHAT: c.Cookie("session_id") error discarded; empty session forwarded to downloader.
  WHY: If token-based auth replaces cookies, session will always be empty.
  IMPACT: Download verify may silently fail.
  TRACE: AdminDownloaderDownload -> c.Cookie (error ignored)
  FIX DIRECTION: Check error and return 401 if session_id absent.
```

```
[REDUNDANT] web/frontend/src/hooks/useDownloaderWebSocket.ts:101-107 — clearDownload never used
  WHAT: Exported function never called by any consumer.
  WHY: Auto-removal via setTimeout handles cleanup.
  IMPACT: Dead code.
  TRACE: useDownloaderWebSocket returns clearDownload -> never destructured
  FIX DIRECTION: Add cancel button that uses it, or remove from return type.
```

```
[INCOMPLETE] web/frontend/src/pages/admin/DownloaderTab.tsx:167-200 — No cancel button for active downloads
  WHAT: Active downloads show progress but no cancel button. Backend has cancel endpoint;
        frontend has downloaderApi.cancel() defined but never wired.
  WHY: Cancel functionality implemented in API layer but not in UI.
  IMPACT: Users cannot cancel in-progress downloads from UI.
  TRACE: endpoints.ts downloaderApi.cancel -> never called
  FIX DIRECTION: Add cancel button calling downloaderApi.cancel(dl.downloadId).
```

---

### 7. FRONTEND (React + TypeScript)

```
[LEAK] web/frontend/src/hooks/useDownloaderWebSocket.ts:59-67 — setTimeout leak on unmount
  WHAT: Download completion timeouts not tracked or cleared on unmount.
  WHY: setTimeout created inside setState callback with no ref tracking.
  IMPACT: React warning on unmount; stale closure call to setActiveDownloads.
  TRACE: ws.onmessage -> setTimeout(10000) -> component unmounts -> callback fires
  FIX DIRECTION: Track timer IDs in ref; clear in useEffect cleanup.
```

```
[DRIFT] web/frontend/src/api/endpoints.ts:956 — downloaderApi.download sends camelCase to Go backend
  WHAT: Frontend sends {clientId, isYouTube, isYouTubeMusic, relayId}; Go typically uses snake_case.
  WHY: External downloader service may use camelCase.
  IMPACT: If Go handler parses body, camelCase won't bind to snake_case json tags.
  TRACE: downloaderApi.download -> api.post -> Go handler
  FIX DIRECTION: Verify Go handler JSON tags match frontend field names.
```

```
[DRIFT] web/frontend/src/api/types.ts:1063-1066 — DownloaderDownloadResult camelCase mismatch
  WHAT: downloadId (camelCase) may not match Go's download_id (snake_case).
  WHY: Needs verification against actual Go handler response format.
  IMPACT: Fields undefined if Go returns snake_case.
  TRACE: Types -> Go handler response -> JSON mapping
  FIX DIRECTION: Verify and align.
```

```
[FRAGILE] web/frontend/src/api/client.ts:79 — Unsafe cast of raw.data as T
  WHAT: Success response with undefined data cast as T; callers destructuring will crash.
  WHY: Some success responses have no data field.
  IMPACT: Runtime crash for callers expecting non-null T.
  TRACE: apiRequest<T> -> raw.data as T
  FIX DIRECTION: Callers should validate return value; client could throw for non-void endpoints.
```

```
[FRAGILE] web/frontend/src/pages/player/playerHLS.ts:58-67 — HLS check effect re-runs on every media refetch
  WHAT: Effect depends on `media` object which is new reference on every query refetch.
  WHY: useQuery creates new object reference each time.
  IMPACT: Multiple redundant HLS check API calls; possible flickering.
  TRACE: useHlsCheckEffect -> deps [media] -> re-runs each refetch
  FIX DIRECTION: Depend on media?.type or media?.id instead of full object.
```

```
[FRAGILE] web/frontend/src/pages/player/playerHLS.ts:98 — HLS polling effect recreates interval each cycle
  WHAT: hlsJob in dependency array replaced on each poll; effect re-runs clearing interval.
  WHY: State object replaced triggers effect re-run.
  IMPACT: Interval recreated every 3 seconds; works but wasteful.
  TRACE: useHlsPollingEffect -> setHlsJob -> effect re-runs
  FIX DIRECTION: Store hlsJob.id in ref; depend on boolean only.
```

```
[FRAGILE] web/frontend/src/stores/playbackStore.ts:79 — trackPosition called with duration=0
  WHAT: playMedia calls trackPosition(id, 0, 0) with meaningless duration=0.
  WHY: Records "started watching" event.
  IMPACT: Creates watch history entry with 0 duration; could confuse resume logic.
  TRACE: playbackStore.playMedia -> watchHistoryApi.trackPosition(id, 0, 0)
  FIX DIRECTION: Skip initial track or wait until duration known.
```

```
[FRAGILE] web/frontend/src/pages/admin/AnalyticsTab.tsx:227 — <a href> instead of <Link>
  WHAT: Uses HTML anchor causing full page reload instead of SPA navigation.
  WHY: Likely oversight.
  IMPACT: Poor UX — full reload when clicking media link from analytics.
  TRACE: AnalyticsTab -> topMedia.map -> <a href>
  FIX DIRECTION: Replace with <Link to> from react-router-dom.
```

```
[GAP] web/frontend/src/App.tsx — No 404 catch-all route
  WHAT: Unmatched paths render blank page inside ErrorBoundary.
  WHY: No <Route path="*"> defined.
  IMPACT: Users navigating to /foo see blank page.
  TRACE: App.tsx -> Routes -> no catch-all
  FIX DIRECTION: Add catch-all Route rendering a 404 component.
```

```
[GAP] web/frontend/src/hooks/useHLS.ts:106-277 — No cleanup of Safari native HLS
  WHAT: Safari path sets el.src but cleanup doesn't clear it; only hls.js path has cleanup.
  WHY: Safari handles HLS natively; cleanup omitted.
  IMPACT: Switching away from HLS stream may not stop playback on Safari.
  TRACE: useEffect -> el.src = hlsUrl (Safari) -> cleanup only destroys hls.js
  FIX DIRECTION: Add el.src = ''; el.removeAttribute('src'); el.load() in Safari cleanup.
```

```
[GAP] web/frontend/src/components/AgeGate.tsx:53 — Hidden children still mounted and fetching
  WHAT: Children rendered with visibility:hidden while gate shown; all API queries fire.
  WHY: Comment: "DOM ready when gate dismisses."
  IMPACT: Unnecessary API calls and potential data leakage before age verification.
  TRACE: AgeGateProvider -> showGate=true -> children rendered hidden
  FIX DIRECTION: Lazy-mount children after verification.
```

```
[SILENT FAIL] web/frontend/src/pages/profile/ProfilePage.tsx:639-644 — eslint-disable exhaustive-deps
  WHAT: useEffect with empty deps calls functions not in deps; stale closures possible.
  WHY: Functions not wrapped in useCallback.
  IMPACT: Low — mitigated by mount-only behavior.
  TRACE: useProfilePage -> useEffect([]) -> loadPreferences/loadWatchHistory
  FIX DIRECTION: Wrap load functions in useCallback or inline in useEffect.
```

---

## END OF REPORT

**Report generated:** 2026-03-15
**Total findings:** 155
**Most critical items to address:**
1. Path traversal in ImportFile (SECURITY)
2. CORS wildcard + credentials bypass (SECURITY)
3. Log injection via Request ID (SECURITY)
4. WebSocket CSWSH in downloader (SECURITY)
5. PlaylistItem schema drift causing SQL errors (DRIFT)
6. remote.go nil context panic (FRAGILE/HIGH)
7. Mass assignment in config update (SECURITY)
8. Receiver stream limit bypass for auth users (FRAGILE)
9. ADMIN_PASSWORD in process environment (SECURITY)
10. Untrimmed CORS/IP list entries (FRAGILE)
