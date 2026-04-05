# Backend Audit Report — 2026-04-05

> **Scope:** Go backend only (`api/`, `cmd/`, `internal/`, `pkg/`). All source files read and traced.
> **Auditor:** Claude Code deep-debug-audit skill (7 parallel agents)
> **Build status:** `go build ./...` passes cleanly. All tests pass.

---

## Methodological Note — `new(expr)` in Go

Multiple agents flagged `new(r.recordToRow(item))`, `new(time.Now())`, `new(*user)`, and similar patterns as compile errors or zero-value bugs. **These are all false positives.** Go's `new()` builtin accepts expressions (not just type literals). `new(expr)` allocates a `*T` initialized to the expression's value — verified empirically:

```go
user := &User{ID: "abc", Username: "alice"}
user = new(*user)  // creates *User{ID: "abc", Username: "alice"} — a copy, not zero

p := new(time.Now())  // creates *time.Time initialized to current time
```

All `new(expr)` patterns in this codebase compile and work correctly. They are non-idiomatic (prefer `t := expr; ptr = &t`) but not bugs. These have been excluded from the findings below.

---

## === AUDIT SUMMARY ===

```
Files analyzed:    ~190 Go source files (excluding vendor/)
Functions traced:  ~900+
Workflows traced:  18+ major user-facing flows

CRITICAL:      11   (must fix before production deploy)
HIGH:          28   (security or correctness bugs)
MEDIUM:        36   (tech debt, time bombs, fragile patterns)
LOW:           25   (cleanup, style, minor gaps)
OK:            14   (investigated and confirmed correct)
```

---

## CRITICAL — Must fix before deploy

These issues cause security vulnerabilities, data corruption, or exploitable logic flaws.

---

### C-01 [SECURITY] pkg/storage/local/local.go:40 — HasPrefix path traversal (no separator boundary)
```
WHAT: resolve() checks strings.HasPrefix(cleaned, b.root) without a path separator.
      Root="/data/media" accepts "/data/media-evil/secret.txt" because
      HasPrefix("/data/media-evil/...", "/data/media") == true.
IMPACT: Path traversal to sibling directories whose name starts with the root prefix.
FIX: strings.HasPrefix(cleaned, b.root+string(filepath.Separator)) || cleaned == b.root
```

### C-02 [SECURITY] pkg/storage/local/local.go:252 — AbsPath fallback bypasses security check
```
WHAT: On resolve() error (traversal rejection), AbsPath falls back to
      filepath.Join(b.root, filepath.Clean(path)) — silently returning a path outside root.
IMPACT: Callers using AbsPath (e.g. ffmpeg invocation) receive traversal paths.
FIX: Remove the fallback; return root or empty string on resolve() error.
```

### C-03 [SECURITY] pkg/storage/s3compat/s3.go:87 — S3 key allows ".." traversal outside prefix
```
WHAT: key() uses path.Clean which does NOT strip ".." from relative paths. A key like
      "../../secrets/admin.json" prefixed becomes "prefix/../../secrets/admin.json".
IMPACT: Read/write objects outside the configured S3 prefix.
FIX: Reject keys starting with ".." or containing "/../" after path.Clean.
```

### C-04 [BROKEN] internal/config/accessors.go:92 — SetValuesBatch never fires OnChange watchers
```
WHAT: SetValuesBatch saves config and calls syncFeatureToggles but NEVER invokes watchers.
      Update() does invoke watchers. Admin panel config changes go through SetValuesBatch.
IMPACT: Modules watching for config changes (security, streaming, CORS) are never notified
        of admin panel changes. Requires server restart for changes to take effect.
FIX: Dispatch watchers in SetValuesBatch after save(), matching Update()'s pattern.
```

### C-05 [BROKEN] internal/config/config.go:243 — Update() does not call syncFeatureToggles
```
WHAT: syncFeatureToggles() remaps feature flags → module Enabled fields. Called in Load()
      and SetValuesBatch but NOT in Update().
IMPACT: Feature flag changes via Update() leave module-level Enabled out of sync.
FIX: Add m.syncFeatureToggles() in Update() after the updater function runs.
```

### C-06 [SECURITY] crawler/browser.go:117 — Chrome --host-resolver-rules with CIDR notation is invalid
```
WHAT: Chrome's --host-resolver-rules does NOT support CIDR notation ("MAP 10.0.0.0/8 ~NOTFOUND").
      The entire hostRules string is silently ignored by Chrome.
      Combined with --disable-web-security and --no-sandbox, crawled pages have full
      unrestricted network access including to private IPs (169.254.169.254, 10.x.x.x).
IMPACT: Admin-triggered crawl of a malicious URL → SSRF from renderer + potential RCE.
FIX: Remove --disable-web-security; use CDP Network.setBlockedURLs for private IP blocking.
```

### C-07 [SECURITY] receiver/wsconn.go:248 — Catalog push/heartbeat accepted before slave registration
```
WHAT: When sw.slaveID is "" (no register message yet), the guard
      sw.slaveID != "" && data.SlaveID != sw.slaveID short-circuits to false.
      Any API key holder can push catalog data into an arbitrary slave's catalog.
IMPACT: Catalog poisoning; a rogue key holder can overwrite any slave's media list.
FIX: Reject catalog/heartbeat when sw.slaveID == "".
```

### C-08 [BROKEN] internal/playlist/playlist.go:411 — ReorderItems mutates in-memory before DB; no rollback
```
WHAT: reorderItemsLocked sets playlist.Items = newItems before the DB update loop.
      DB errors are only logged, not returned. On partial DB failure, in-memory and DB diverge.
IMPACT: Playlist order permanently inconsistent after any DB write failure during reorder.
FIX: Update DB first in a transaction; only update in-memory after commit.
```

### C-09 [BROKEN] internal/config/config.go:69 — json.Unmarshal zeroes defaults for partial config sections
```
WHAT: A config.json with "hls": {"auto_generate": true} causes SegmentDuration=0,
      ConcurrentLimit=0, ProbeTimeout=0 — overwriting defaults with zero values.
IMPACT: Partial config sections silently lose defaults; may cause validation failure or
        runtime errors (zero-timeout ffprobe, zero segment duration).
FIX: Unmarshal into a separate struct and merge only non-zero fields into defaults.
```

### C-10 [SECURITY] api/handlers/analytics.go:156 — Client can forge server-only analytics event types
```
WHAT: SubmitClientEvent's validTypes includes EventLogin, EventRegister, EventDownload, etc.
      Any authenticated user can inject fake login/registration/download events.
IMPACT: Analytics corruption; inflated traffic stats; undermines admin dashboards.
FIX: Split validTypes into client-safe and server-only lists; reject server-only from client.
```

### C-11 [SECURITY] upload/upload.go:374 — File size validated against client-controlled fh.Size
```
WHAT: validateUploadSize uses multipart FileHeader.Size which is client-supplied.
      A client can set fh.Size=1 to bypass MaxFileSize, then stream gigabytes.
IMPACT: Any user with CanUpload can bypass file size limits → disk exhaustion.
FIX: Wrap file reader in io.LimitReader(reader, maxFileSize+1) and check actual bytes copied.
```

---

## HIGH — Will cause user-facing bugs or exploitable security issues

---

### H-01 [SECURITY] api/handlers/feed.go:77 — RSS feed leaks mature content to all authenticated users
```
WHAT: GetRSSFeed calls ListMedia without filtering IsMature. No mature-content check applied.
FIX: Filter out IsMature items when user lacks CanViewMature permission.
```

### H-02 [SECURITY] api/handlers/thumbnails.go:265 — Responsive/preview thumbnails bypass mature check
```
WHAT: ServeThumbnailFile extracts mediaID as TrimSuffix(filename, ext). For "uuid-sm.webp"
      mediaID="uuid-sm" which fails DB lookup → mature check skipped → file served.
FIX: Strip -sm/-md/-lg and _preview_N suffixes before DB lookup.
```

### H-03 [SECURITY] admin_receiver.go:225 — Slave-controlled HTTP status code forwarded to browser
```
WHAT: X-Stream-Status header parsed and used in w.WriteHeader(). Rogue slave can send 301/302.
FIX: Whitelist valid codes: {200, 206, 404, 416, 503}.
```

### H-04 [SECURITY] admin_downloader.go:79 — URL forwarded without SSRF validation
```
WHAT: AdminDownloaderDetect/Download forward admin-supplied URLs to external downloader service
      without calling helpers.ValidateURLForSSRF. Downloader fetches arbitrary internal URLs.
FIX: Add helpers.ValidateURLForSSRF(req.URL) before forwarding.
```

### H-05 [SECURITY] crawler/crawler.go:441 — Same-host check bypass via www prefix substring
```
WHAT: strings.Contains(u.Hostname(), baseHostStripped) — "evil-example.com" passes for "example.com".
FIX: Use suffix match with dot boundary: u.Hostname() == base || HasSuffix(u.Hostname(), "."+base).
```

### H-06 [SECURITY] pkg/middleware/agegate.go:219 — Age-gate verify has no CSRF protection
```
WHAT: POST /api/age-verify requires no CSRF token. Cross-site POST sets age_verified cookie.
FIX: Require CSRF token or validate Origin/Referer header.
```

### H-07 [SECURITY] remote/remote.go:665 — CacheMedia writes to final path non-atomically
```
WHAT: os.Create(localPath) then io.Copy. Process kill → partial file. On restart, served as-is.
FIX: Write to .tmp, os.Rename on success; persist cache record after rename.
```

### H-08 [SECURITY] auth/authenticate.go:80 — Disabled-account check skips brute-force penalty
```
WHAT: user.Enabled==false returns ErrAccountDisabled without recordFailedAttempt.
      Enables username enumeration of disabled accounts at unlimited rate.
FIX: Always call recordFailedAttempt before returning; return generic ErrInvalidCredentials.
```

### H-09 [RACE] auth/session.go:128 — ValidateSession returns shared pointer after lock release
```
WHAT: Returns the original *Session from the map, not the copy. Concurrent reads race.
FIX: Return &sessionCopy instead of session.
```

### H-10 [SECURITY] auth/password.go:110 — Admin password change doesn't invalidate sessions
```
WHAT: ChangeAdminPassword returns without evicting existing admin sessions.
FIX: Call m.evictSessionsForUser for the admin username after password change.
```

### H-11 [SECURITY] auth/authenticate.go:110 — AdminSession pathway is orphaned dead code
```
WHAT: AdminAuthenticate creates AdminSession + regular Session. Only regular Session is used
      by middleware. adminSessions map grows unboundedly, never read by any route.
FIX: Remove AdminSession pathway; have AdminAuthenticate return a regular Session with Role=admin.
```

### H-12 [SECURITY] api/handlers/analytics.go:185 — Client-supplied session_id overrides server session
```
WHAT: sessionID := req.SessionID used even when authenticated session exists.
FIX: Always use server-side session ID for authenticated requests.
```

### H-13 [SECURITY] api/routes/routes.go:291 — Extractor HLS endpoints unauthenticated, no rate limit
```
WHAT: /extractor/hls/:id/* routes have no auth middleware and no rate limiting in handlers.
FIX: Add requireAuth or per-handler rate limiting; validate item exists before proxying.
```

### H-14 [BROKEN] api/handlers/deletion_requests.go:196 — DB status updated before actual deletion
```
WHAT: Status set to "approved" before auth.DeleteUser(). If DeleteUser fails, record stuck as
      approved but user still exists. Re-processing fails with "already processed".
FIX: Call DeleteUser first; only update DB status on success.
```

### H-15 [BROKEN] internal/database/database.go:148 — MaxRetries=0 yields nil DB with nil error
```
WHAT: Loop `for i := 0; i < dbCfg.MaxRetries; i++` never executes. Returns (nil, nil, nil).
FIX: max(dbCfg.MaxRetries, 1) to guarantee at least one attempt.
```

### H-16 [SECURITY] system.go:428 — SQL denylist trivially bypassable
```
WHAT: Denylist misses DO, GET_LOCK, INTO OUTFILE/DUMPFILE, MySQL comments can bypass prefix check.
FIX: Remove SQL query endpoint or add comprehensive denylist + verify MySQL user has no FILE privilege.
```

### H-17 [GAP] config/config.go:77 — validate() not called after Update/SetValuesBatch
```
WHAT: Validation runs only on Load(). Admin can set invalid values at runtime (port 0, etc).
FIX: Call m.validate() in Update() and SetValuesBatch() before saving.
```

### H-18 [SECURITY] api/handlers/media.go:539 — Extractor redirect bypasses mature + stream-limit checks
```
WHAT: Extractor items get 302 redirect to unauthenticated HLS proxy without mature check.
FIX: Add mature check before redirect; consider proxy approach instead of redirect.
```

### H-19 [SECURITY] auth/authenticate.go:229 — Lockout window resets fully, enables slow brute-force
```
WHAT: recordFailedAttempt resets Count=1 on window expiry. One attempt per lockout window
      runs indefinitely with no cumulative penalty.
FIX: Use a cumulative violation counter that doesn't reset between windows.
```

### H-20 [SECURITY] admin_discovery.go:41 — No EvalSymlinks before allow-list check; symlink escape
```
WHAT: filepath.Clean does not resolve symlinks. Symlink in media dir → scan arbitrary paths.
FIX: Call filepath.EvalSymlinks on req.Directory before the allow-list check.
```

### H-21 [LEAK] analytics/sessions.go:25 — Sessions map grows unboundedly between cleanup cycles
```
WHAT: updateSession adds entries per unique SessionID with no cap. Bot/scraper → OOM.
FIX: Enforce a maximum session count with LRU eviction.
```

### H-22 [BROKEN] backup/backup.go:378 — Potential deadlock in RestoreBackup → CreateBackup lock ordering
```
WHAT: RestoreBackup holds restoreMu then CreateBackup acquires mu.Lock. Concurrent RestoreBackup
      calls can deadlock (opposite lock ordering).
FIX: Release restoreMu before createPreRestoreBackup or enforce consistent lock ordering.
```

### H-23 [GAP] auth/session.go:83 — All repo errors mapped to ErrSessionNotFound
```
WHAT: DB timeout/connection errors return ErrSessionNotFound → 401 instead of 503.
FIX: Distinguish DB errors from not-found; propagate DB errors as 503.
```

### H-24 [GAP] handler.go:513 — checkMatureAccess allows on media lookup failure
```
WHAT: When GetMedia returns error, checkMatureAccess returns true (allow). During DB outage
      or scan, mature content gate is silently bypassed with no log.
FIX: Log a warning on lookup failure; consider deny-on-error for mature protection.
```

### H-25 [SECURITY] auth/tokens.go:74 — API tokens never expire
```
WHAT: No ExpiresAt field on APITokenRecord. Tokens valid indefinitely unless manually deleted.
FIX: Add optional ExpiresAt field; enforce in ValidateAPIToken; expose TTL in CreateAPIToken.
```

### H-26 [GAP] config/env_overrides — 20+ config fields have no env override
```
WHAT: Streaming.RequireAuth, UnauthStreamLimit, all Receiver WS fields, HLS.ProbeTimeout,
      RemoteMedia fields, Thumbnails eviction fields, Database.SlowQueryThreshold, UI fields,
      Analytics.ViewCooldown — none configurable via environment variables.
IMPACT: Docker/K8s operators cannot tune these without modifying config.json.
FIX: Add env var mappings for each missing field.
```

### H-27 [SECURITY] security.go:258 — CheckAccess doesn't check rate-limiter ban list
```
WHAT: When rate limiting is disabled, auto-bans and manual BanIP bans are not enforced.
FIX: Check IsBanned() in CheckAccess regardless of RateLimitEnabled.
```

### H-28 [BROKEN] server/server.go:462 — shutdownHTTPServer called when httpServer may be nil
```
WHAT: If Start() fails before HTTP server creation, Shutdown() → nil pointer panic.
FIX: Guard with if s.httpServer != nil.
```

---

## MEDIUM — Tech debt, time bombs, or correctness issues

---

### M-01 [RACE] auth/authenticate.go:29 — getOrLoadUser has TOCTOU cache-load window
### M-02 [SECURITY] auth/watch_history.go:20 — Update branch has no rollback on DB failure
### M-03 [FRAGILE] config/config.go:147 — migrateHLSQualityEnabled falsely re-enables all-disabled profiles
### M-04 [DRIFT] config/validate.go:10 — Two validation paths (private validate vs public Validate) with different coverage
### M-05 [FRAGILE] config/env_helpers.go:20 — envGetBool returns (false,true) for "yes"/"on" → disables features
### M-06 [FRAGILE] config/env_helpers.go:64 — envGetDuration only accepts integers, not duration strings
### M-07 [FRAGILE] config/envfile.go:54 — .env parser mishandles inline comments, multiline values
### M-08 [FRAGILE] config/config.go:192 — save() .bak not used as fallback on crash between rename steps
### M-09 [GAP] config/env_overrides_auth.go — Auth.AllowRegistration has no env override
### M-10 [FRAGILE] env_overrides_misc.go:49 — Mature scanner keywords not whitespace-trimmed on split
### M-11 [FRAGILE] env_overrides_updater.go:33 — AGE_GATE_BYPASS_IPS not whitespace-trimmed
### M-12 [FRAGILE] env_overrides_uploads.go:13 — UPLOADS_ALLOWED_EXTENSIONS not whitespace-trimmed
### M-13 [INCOMPLETE] config/config.go:226 — getCopy() does not deep-copy Storage.S3.Prefixes map
### M-14 [RACE] hls/cleanup.go:170 — cleanInactiveJob reads lastAccess outside write lock
### M-15 [RACE] hls/access.go:26 — RecordAccess and cleanup acquire locks in opposite orders
### M-16 [LEAK] hls/transcode.go:246 — lazyTranscodeQuality holds per-quality mutex across semaphore
### M-17 [SILENT_FAIL] hls/cleanup.go:12 — cleanupLoop dead code; RetentionMinutes silently ignored
### M-18 [GAP] hls/jobs.go:424 — findMediaPathForJob returns "" for completed jobs (lock file removed)
### M-19 [SECURITY] hls/serve.go:67 — CORS origin falls back to "*" for non-matching origins
### M-20 [FRAGILE] hls/locks.go:60 — Stale lock threshold hardcoded at 2 hours; kills long 4K encodes
### M-21 [FRAGILE] receiver/wsconn.go:302 — Replacing WS connection doesn't drain pending streams
### M-22 [RACE] receiver/wsconn.go:195 — Ping goroutine orphaned on reconnect for up to 25s
### M-23 [GAP] receiver/receiver.go:232 — Legacy composite DB IDs never persisted; stale rows accumulate
### M-24 [GAP] analytics/stats.go:350 — rebuildStatsFromEvent doesn't reconstruct UniqueUsers/AvgWatchDuration
### M-25 [GAP] analytics/stats.go:68 — updateStats uses wall clock not event.Timestamp
### M-26 [GAP] analytics/stats.go:350 — reconstructStats capped at 2000 events; may undercount
### M-27 [SECURITY] analytics/export.go:44 — CSV export includes raw IP addresses (GDPR risk)
### M-28 [BROKEN] playlist/playlist.go:461 — ClearPlaylist continues on DB error then clears in-memory
### M-29 [SECURITY] playlist/playlist.go:603 — ExportPlaylist M3U leaks filesystem paths
### M-30 [GAP] suggestions/suggestions.go:332 — RecordRating not persisted for up to 10 minutes
### M-31 [GAP] updater/updater.go:746 — verifyBinaryChecksum silently skips when no checksum exists
### M-32 [FRAGILE] updater/updater.go:1221 — rev-parse errors silently ignored in SourceUpdate
### M-33 [FRAGILE] remote_cache_repository.go:48 — String columns for timestamps vs GORM time.Time
### M-34 [GAP] user_repository_gorm.go:152 — Update silently does nothing if perms/prefs rows missing
### M-35 [RACE] tasks/scheduler.go:224 — Ticker reschedule doesn't drain buffered tick
### M-36 [SECURITY] extractor/extractor.go:325 — Access-Control-Allow-Origin: * on unauthenticated endpoints

---

## LOW — Cleanup, correctness, and maintenance issues

---

### L-01 [GAP] cmd/server/main.go:430 — validateSecrets incomplete (no admin password, no S3 creds check)
### L-02 [GAP] cmd/server/main.go:770 — HLS pre-gen interval read once; config change ignored
### L-03 [FRAGILE] cmd/server/main.go:148 — os.Exit(1) after log.Error without logger flush
### L-04 [REDUNDANT] cmd/server/main.go:64 — .env loaded twice (godotenv + custom loader)
### L-05 [FRAGILE] auth/session.go:163 — LogoutAdmin holds sessionsMu across DB delete
### L-06 [REDUNDANT] auth/authenticate.go:169 — ValidateAdminSession is unreachable dead code
### L-07 [GAP] admin/admin.go:249 — UpdateConfig accepts arbitrary keys including security-sensitive
### L-08 [FRAGILE] admin/admin.go:173 — ExportAuditLog race on same-second concurrent exports
### L-09 [GAP] audit_log_repository.go:71 — GetByUser with limit=0 runs unbounded query
### L-10 [GAP] analytics.go:344 — AdminExportAnalytics defer calls f.Close() on nil file → panic
### L-11 [FRAGILE] admin_updates.go:100 — Source update audit log hardcodes "admin" actor
### L-12 [FRAGILE] auth.go:323 — Admin preference update silently creates DB user record
### L-13 [FRAGILE] system.go:362 — ClearMediaCache runs synchronous full scan in HTTP handler
### L-14 [FRAGILE] playlists.go:276 — AddPlaylistItem can't add receiver/extractor items
### L-15 [GAP] routes.go:87 — adminAuth returns 401 instead of 403 for wrong-role users
### L-16 [GAP] duplicates/duplicates.go:489 — findLocalPathByStableID O(N) full table scan
### L-17 [GAP] duplicates/duplicates.go:333 — ScanLocalMedia loads entire metadata table
### L-18 [FRAGILE] validator/validator.go:441 — FixFile output path collision
### L-19 [FRAGILE] logger/logger.go:415 — Log rotation only creates .1; cleanOldBackups no-op
### L-20 [RACE] handler.go:168 — viewCooldown sync.Map never purged; unbounded memory growth
### L-21 [GAP] Multiple files — filepath.Walk follows symlinks in scanner, categorizer, autodiscovery
### L-22 [GAP] Multiple files — context.Background() used for DB calls in module Stop paths
### L-23 [FRAGILE] downloader/websocket.go:67 — DefaultDialer.Dial has no handshake timeout
### L-24 [GAP] thumbnails/queue.go:79 — dequeue doesn't decrement stats.Pending on context cancel
### L-25 [FRAGILE] s3compat/s3.go:336 — Rename leaves partial dst on streamCopy failure

---

## OK — Investigated and confirmed correct

- `internal/auth/helpers.go:14-51` — Session ID and password generation use crypto/rand correctly.
- `internal/auth/tokens.go:81-84` — API token storage uses SHA-256 with sufficient entropy.
- `internal/auth/authenticate.go:39-42` — Timing-safe dummy bcrypt prevents username enumeration.
- `pkg/middleware/agegate.go:86-106` — extractClientIP verifies IsTrustedProxy before honoring XFF.
- `internal/repositories/mysql/user_repository_gorm.go:132-200` — Update uses explicit column map.
- `internal/backup/backup.go:321` — Backup path traversal doubly prevented; zip-slip blocked.
- `internal/security/security.go` — IP allowlist/blacklist logic correct; expired entries skipped.
- `api/routes/routes.go:469` — All admin routes gated behind adminAuth middleware consistently.
- `api/handlers/handler.go:432-468` — resolveAndValidatePath calls EvalSymlinks + re-validates.
- `api/handlers/handler.go:267-284` — isSecureRequest only honors XFF from trusted proxies.
- `internal/repositories/mysql/media_metadata_repository.go:18-22` — Parameterized queries throughout.
- `cmd/media-receiver/main.go:919-966` — resolveAndValidate correctly uses EvalSymlinks + Rel check.
- `cmd/server/main.go:492-830` — Task closures correctly read config at execution time.
- All `new(expr)` patterns — compile and work correctly (see methodological note above).

---

## Files Analyzed (complete list)

All non-vendor Go source files under `api/`, `cmd/`, `internal/`, `pkg/` — approximately 190 files including:

- `cmd/server/main.go`, `cmd/media-receiver/main.go`
- All `api/handlers/*.go` (44 files), `api/routes/routes.go`
- All `internal/auth/*.go`, `internal/hls/*.go`, `internal/analytics/*.go`, `internal/config/*.go`
- All `internal/repositories/mysql/*.go`
- All `pkg/storage/`, `pkg/helpers/`, `pkg/middleware/`, `pkg/models/`, `pkg/huggingface/`
- All remaining internal packages: thumbnails, playlist, suggestions, crawler, extractor, categorizer, updater, validator, duplicates, backup, autodiscovery, scanner, streaming, downloader, receiver, remote, tasks, logger, database, server, admin, security
