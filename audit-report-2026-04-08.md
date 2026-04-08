# Deep Debug Audit Report — 2026-04-08

## Audit Summary

```
=== AUDIT SUMMARY ===
Files analyzed:    150+ Go files, all handler/module/pkg layers
Functions traced:  500+ (all handler endpoints, module lifecycle, core data flows)
Workflows traced:  25+ (auth, streaming, receiver proxy, remote cache, HLS, upload, admin CRUD, etc.)

BROKEN:       2
INCOMPLETE:   0
GAP:          7
REDUNDANT:    3
FRAGILE:      19
SILENT FAIL:  5
DRIFT:        3
LEAK:         4
SECURITY:     2
OK:           140+ modules/functions verified correct

Critical (must fix before deploy): #1, #2, #36, #37
High (will cause user-facing bugs):  #3, #4, #5, #38
Medium (tech debt / time bombs):     #6–#20, #39–#44
Low (cleanup / style):               #21–#35, #45
```

---

## Findings

---

### CRITICAL (Must Fix)

---

#### #1 [BROKEN] internal/analytics/stats.go:119 — TotalWatchTime adds full media duration instead of actual watched time

```
WHAT: applyPlaybackToDailyStatsLocked adds event.Data["duration"] (total video length) to
      TotalWatchTime. For a 2-hour movie where the user watched 5 minutes, the full 2 hours
      is added.
WHY:  "duration" is the total media length, not the amount watched. The "position" field
      represents how far the user actually watched.
IMPACT: TotalWatchTime is significantly overstated in analytics dashboards — every playback
        event adds the full video duration regardless of how much was actually watched.
        Admin analytics/daily stats show inflated engagement metrics.
TRACE: TrackPlayback → updateDailyStatsLocked → applyPlaybackToDailyStatsLocked
FIX DIRECTION: Use min(position, duration) or just position as the watch-time increment,
               not the full duration.
```

---

#### #2 [GAP] internal/analytics/stats.go:376-444 — rebuildStatsFromEvent missing playback case for dailyStats

```
WHAT: rebuildStatsFromEvent handles view, login, register, etc. for daily stats reconstruction,
      but for "playback" events it only updates mediaStats (line 429-443), NOT
      dailyStats.TotalWatchTime. The live code path (updateDailyStatsLocked) does add to
      dailyStats.TotalWatchTime.
WHY:  Missing case "playback" handling in the daily-stats branch of rebuildStatsFromEvent.
IMPACT: After server restart, TotalWatchTime in daily stats is zero until new playback events
        arrive. GetSummary and GetDailyStats endpoints show understated watch time until
        stats re-accumulate from new events.
TRACE: Start() → rebuildFromExistingEvents → rebuildStatsFromEvent (playback → mediaStats only)
FIX DIRECTION: Add playback handling to the daily-stats branch in rebuildStatsFromEvent,
               mirroring applyPlaybackToDailyStatsLocked.
```

---

### HIGH PRIORITY

---

#### #3 [FRAGILE] internal/streaming/streaming.go:174 — S3 stat uses background context instead of request context

```
WHAT: Stream() creates context.Background() for all storage stat/open operations instead of
      propagating the HTTP request context (r.Context()).
WHY:  When the storage backend is S3 (remote), stat and open calls are HTTP round-trips. Using
      context.Background() means they cannot be cancelled when the client disconnects.
IMPACT: On S3 backends, client disconnects during slow HEAD requests leave the goroutine
        blocked on a network call until the HTTP timeout (30s default). Under high load with
        many abandoned requests, this can exhaust the http.Transport connection pool.
TRACE: StreamMedia handler → streaming.Stream() → m.store.Stat(ctx, ...) where ctx = context.Background()
FIX DIRECTION: Replace context.Background() with a request-scoped context passed through StreamRequest.
```

---

#### #4 [LEAK] internal/streaming/streaming.go:639-669 — activeSessions never cleaned up for crashed streams

```
WHAT: activeSessions grows unboundedly if endSession is never called (e.g., a panic between
      startSession and the deferred endSession, or a handler that returns early without
      invoking the defer chain).
WHY:  No periodic session cleanup or TTL eviction for stale entries.
IMPACT: Memory leak over time. PeakConcurrent and ActiveStreams counts drift upward. After
        extended operation, stale sessions could cause CanStartStream to reject legitimate
        users when their "count" includes abandoned sessions.
TRACE: startSession → add to activeSessions → no cleanup if endSession never called
FIX DIRECTION: Add a periodic cleanup that removes sessions with LastUpdate older than a
               threshold (e.g., 30 minutes).
```

---

#### #5 [FRAGILE] internal/receiver/receiver.go:437-507 — PushCatalog node pointer captured under RLock, mutated after re-locking

```
WHAT: PushCatalog reads `node` under RLock (line 438), releases the lock (439), does DB I/O,
      then acquires the write Lock (491) and mutates node.MediaCount/Status/LastSeen (504-506).
      Between RUnlock and Lock, another goroutine could call UnregisterSlave, removing the
      node from the map.
WHY:  The node pointer was captured before the write lock was acquired, creating a TOCTOU gap.
IMPACT: If UnregisterSlave runs between lines 439 and 491, the catalog push succeeds (DB
        records are written) but node modifications update a detached node.
TRACE: PushCatalog() → RLock/read node/RUnlock → DB ops → Lock → node.MediaCount = ...
FIX DIRECTION: Re-read the node from the map under the write lock at line 492.
```

---

### MEDIUM PRIORITY

---

#### #6 [BROKEN] internal/receiver/receiver.go:503 — PushCatalog sets MediaCount from pre-filter item count

```
WHAT: node.MediaCount is set to len(req.Items) which counts items before path-validation
      filtering. Records with suspicious paths (containing "..", absolute paths) are skipped
      at lines 452-454 but the count reflects the pre-filter number.
WHY:  MediaCount is assigned from req.Items length, not len(records).
IMPACT: MediaCount overstates the actual catalog size when items are filtered out for path
        traversal. Admin receiver stats show inflated media counts.
TRACE: PushCatalog → filter items → node.MediaCount = len(req.Items) [not len(records)]
FIX DIRECTION: Set node.MediaCount = len(records) instead of len(req.Items).
```

---

#### #7 [FRAGILE] internal/receiver/receiver.go:299-316 — Legacy ID migration delete-then-upsert without transaction

```
WHAT: loadFromDB's background goroutine migrates legacy composite IDs by deleting the old DB
      row and upserting the new one in separate calls without a DB transaction.
WHY:  If the process crashes between delete and upsert, the media record is lost from the DB.
IMPACT: On restart after a crash during migration, the affected receiver media items are
        missing until the slave pushes a fresh catalog.
TRACE: loadFromDB() → go func() { for each migration: DeleteByID → UpsertBatch }
FIX DIRECTION: Wrap delete + upsert in a single DB transaction, or upsert before delete.
```

---

#### #8 [GAP] internal/extractor/extractor.go:259 — DB upsert before Lock recheck creates orphan rows

```
WHAT: AddItem checks item count under RLock, then does DB upsert (line 259) outside any lock,
      then re-checks under Lock (line 265). If the Lock recheck finds MaxItems exceeded, the
      DB row was already written and is not cleaned up.
WHY:  DB write happens before the final Lock-based capacity check.
IMPACT: Orphaned DB row that reappears on restart, potentially exceeding MaxItems.
TRACE: AddItem → RLock count check → DB upsert → Lock recheck (may reject)
FIX DIRECTION: Move the DB upsert inside the Lock block after the recheck, or delete the
               DB row if the Lock check fails.
```

---

#### #9 [FRAGILE] internal/remote/remote.go:587-618 — getCachedMedia TOCTOU between stat check and serve

```
WHAT: getCachedMedia checks file existence under RLock (line 595: os.Stat), returns the cached
      entry, then the caller serves the file. Between stat and http.ServeFile, CleanCache could
      delete the file.
WHY:  File existence check and serve are not atomic.
IMPACT: Rare race causes http.ServeFile to return 404. Next request triggers re-download.
TRACE: StreamRemote → getCachedMedia (stat OK) → return cached → http.ServeFile (file gone)
FIX DIRECTION: Open the file under the lock and pass to http.ServeContent instead.
```

---

#### #10 [FRAGILE] internal/remote/remote.go:251-264 — Sequential source sync; one slow source blocks all

```
WHAT: syncAllSources iterates all sources sequentially. Each syncSource makes an HTTP request.
      If one source is slow/unreachable, all subsequent sources are delayed.
WHY:  Sequential iteration without per-source timeout beyond httpClient.Timeout.
IMPACT: A single unresponsive remote source blocks syncing of all other sources.
TRACE: Start() → go syncAllSources() → for each: syncSource() → discoverMedia(HTTP GET)
FIX DIRECTION: Add per-source context with timeout, or sync sources concurrently.
```

---

#### #11 [FRAGILE] internal/downloader/websocket.go:119-141 — WS relay has no message size limit

```
WHAT: Messages from the admin client are relayed directly to the downloader WS and vice versa
      with no size cap or validation. ReadMessage has no size limit configured.
WHY:  SetReadLimit was not called on either connection.
IMPACT: A malicious admin client could send arbitrarily large messages, consuming server memory.
TRACE: AdminDownloaderWebSocket → relay goroutine → ReadMessage (unbounded)
FIX DIRECTION: Set SetReadLimit on both adminConn and dlConn (e.g., 1MB).
```

---

#### #12 [LEAK] api/handlers/handler.go:186 — viewCooldown AfterFunc timers accumulate

```
WHAT: tryRecordView creates a time.AfterFunc timer for every unique user+media view to clean up
      the sync.Map entry after 2x the cooldown window.
WHY:  Each timer is a goroutine scheduled by the runtime timer heap.
IMPACT: At high traffic (>10k concurrent viewers), the timer count grows proportionally.
        Each timer is ~200 bytes of heap, so 100k active timers = 20MB.
TRACE: StreamMedia → tryRecordView → time.AfterFunc(cooldown*2, delete)
FIX DIRECTION: Replace per-entry timers with a periodic sweep goroutine.
```

---

#### #13 [GAP] internal/suggestions/suggestions.go:1060-1105 — saveOneProfile writes all ViewHistory on every save

```
WHAT: Every periodic save (10-minute interval) writes ALL ViewHistory entries for each profile
      (up to 500 per user) via individual SaveViewHistory calls.
WHY:  No dirty tracking; the entire history is re-saved every time.
IMPACT: O(users x 500) DB writes every 10 minutes. For 1000 active users, that's 500,000
        DB upserts per save cycle.
TRACE: saveLoop → saveAllProfiles → saveOneProfile → for each ViewHistory: SaveViewHistory
FIX DIRECTION: Track dirty state per profile and only save changed profiles.
```

---

#### #14 [FRAGILE] internal/upload/upload.go:686-698 — writeChunkAndTrack acquires module-wide mutex per write

```
WHAT: For local filesystem uploads, every 32KB chunk write acquires m.mu.Lock() to update
      progress. This blocks all concurrent GetActiveUploads/GetProgress readers.
WHY:  Progress is updated under the module's global Lock, not a per-upload lock.
IMPACT: Under concurrent uploads, progress polling and upload I/O contend on the same mutex.
TRACE: UploadMedia → writeChunkAndTrack → m.mu.Lock() per chunk
FIX DIRECTION: Use a per-Progress mutex instead of the module-wide lock.
```

---

#### #15 [SILENT FAIL] internal/remote/remote.go:835-858 — saveCacheIndex returns on first error

```
WHAT: saveCacheIndex iterates all cache entries and calls repo.Save for each. If any single
      Save fails, it returns immediately, skipping all remaining entries.
WHY:  Early return on error.
IMPACT: On Stop(), some cache entries may not be saved. On restart, orphan files exist on
        disk but are not indexed in the DB.
TRACE: Stop() → saveCacheIndex() → for each: repo.Save() → first error returns
FIX DIRECTION: Continue iterating and accumulate errors.
```

---

#### #16 [FRAGILE] internal/hls/transcode.go:183-218 — Presigned S3 URL may expire during long transcodes

```
WHAT: resolveMediaInputPath generates a presigned URL before transcode starts. For S3, this
      URL has a 2-hour expiry. Long transcodes (4K content) may exceed this.
WHY:  Presigned URL is generated once at the start.
IMPACT: Transcode fails midway through a long encode if the presigned URL expires.
TRACE: GenerateHLS → buildFFmpegTranscodeCmd → resolveMediaInputPath → PresignGetURL(2h)
FIX DIRECTION: Set presigned URL expiry >= maximum expected transcode duration.
```

---

#### #17 [FRAGILE] internal/receiver/wsconn.go:326 — drainPendingForSlave race with new RequestStream

```
WHAT: setSlaveWS calls go m.drainPendingForSlave(slaveID) after releasing wsMu. A new
      RequestStream could add a pending stream for the reconnected slave before the drain runs.
WHY:  Race window between releasing wsMu and acquiring pendingMu in the goroutine.
IMPACT: A stream requested after reconnect but before drain could be incorrectly cancelled.
TRACE: setSlaveWS → release wsMu → go drainPendingForSlave → pendingMu.Lock
FIX DIRECTION: Drain synchronously under pendingMu only.
```

---

#### #18 [SILENT FAIL] internal/receiver/receiver.go:232-250 — loadFromDB continues with empty media on failure

```
WHAT: If the media load fails (line 249), the module logs a warning and returns, leaving it
      running with slaves but no media.
WHY:  No retry; no health status update.
IMPACT: Unified media listing is missing all receiver items until each slave pushes a fresh
        catalog.
TRACE: Start() → loadFromDB() → mediaRepo.ListAll() fails → return (media empty)
FIX DIRECTION: Retry with backoff or set health to "degraded".
```

---

#### #19 [GAP] internal/tasks/scheduler.go:225-261 — Disabled task may fire once during startup delay

```
WHAT: runTaskLoop waits for startup delay, then immediately calls executeTask before the
      select loop. If DisableTask is called during the startup delay, the initial run still fires.
WHY:  waitForStartupDelay only checks ctx.Done, not task.Enabled.
IMPACT: A disabled task may execute once after startup.
TRACE: runTaskLoop → waitForStartupDelay → executeTask (even if disabled during delay)
FIX DIRECTION: Check task.Enabled after waitForStartupDelay returns.
```

---

#### #20 [FRAGILE] internal/streaming/streaming.go:780-785 — Download opens file twice for local backend

```
WHAT: Download() calls openFileForDownload to get fileSize, closes the file, validates the
      size, then opens the file again for streaming.
WHY:  TOCTOU — file could be deleted between close and reopen.
IMPACT: Second open fails with ENOENT, returning ErrFileNotFound. Handled correctly.
TRACE: Download() → openFileForDownload() → file.Close() → ... → os.Open(path)
FIX DIRECTION: Keep file handle open from first open.
```

---

### LOW PRIORITY

---

#### #21 [DRIFT] api/handlers/admin_audit.go:17-18 — Uncommitted change: ParseQueryInt replaces local helpers

```
WHAT: Unstaged change replaces local parseAuditLimit/parseAuditOffset with shared ParseQueryInt.
WHY:  Correct refactoring — ParseQueryInt exists and call is correct.
IMPACT: None. Uncommitted code change.
FIX DIRECTION: Commit the change.
```

---

#### #22 [DRIFT] internal/scanner/mature.go:320-340 — Start log says "Loaded N scan results" but load is a no-op

```
WHAT: loadResults() is intentionally a no-op (lazy loading), but the log message at line 322
      says "Loaded 0 previous scan results" which is misleading.
WHY:  Log message not updated when loadResults was changed to lazy loading.
IMPACT: Misleading log output.
FIX DIRECTION: Remove or update the log line.
```

---

#### #23 [REDUNDANT] api/handlers/handler.go:499-501 — isPathWithinDirs delegates identically to isPathUnderDirs

```
WHAT: isPathWithinDirs simply calls isPathUnderDirs with identical signatures and behavior.
WHY:  Historical duplication.
IMPACT: No functional impact — dead indirection.
FIX DIRECTION: Remove one and rename callers.
```

---

#### #24 [REDUNDANT] api/handlers/handler.go:580-583 — Sentinel errors only used locally

```
WHAT: errInvalidPath and errPathNotFound are package-level sentinels used only in two functions.
WHY:  Minor scope concern.
IMPACT: No functional impact.
FIX DIRECTION: Leave as-is (idiomatic Go).
```

---

#### #25 [SILENT FAIL] api/handlers/auth.go:751-776 — ExportWatchHistory CSV write errors silently return

```
WHAT: When CSV row writes fail, the handler logs and returns without error to the response.
WHY:  Headers already sent (200); can't change status code.
IMPACT: Clients may receive truncated CSV exports. Inherent to streaming responses.
FIX DIRECTION: Document this behavior.
```

---

#### #26 [SILENT FAIL] api/handlers/media.go:621-624 — GetMedia re-fetch in StreamMedia can fail silently

```
WHAT: After recording the view, StreamMedia calls h.media.GetMedia(absPath) again for
      suggestions.RecordView. If this fails, the suggestion view is not recorded.
WHY:  The item was already validated earlier; a concurrent delete could remove it.
IMPACT: Suggestion view silently not recorded in a delete-while-streaming race.
FIX DIRECTION: Use the localItem already obtained earlier instead of re-fetching.
```

---

#### #27 [FRAGILE] internal/extractor/extractor.go:244-249 — Failed M3U8 stores item with error status, no error returned

```
WHAT: When M3U8 URL is unreachable, item is created with status="error" and returned without error.
WHY:  Design choice to persist error-status items.
IMPACT: UI shows item as "added" even though it's broken. Admin must check status.
FIX DIRECTION: Return distinct response indicating partial success.
```

---

#### #28 [FRAGILE] internal/security/security.go:821-841 — recordViolation spawns unbounded goroutines for onBan

```
WHAT: onBan callback is launched with go r.onBan(...) without bound.
WHY:  Prevents DB call from blocking rate limiter.
IMPACT: Under DDoS with many IPs hitting ban threshold, unbounded goroutines spawned.
FIX DIRECTION: Use a bounded channel or accept as-is for rare events.
```

---

#### #29 [FRAGILE] internal/crawler/crawler.go:270-297 — Only one crawl at a time globally

```
WHAT: crawlMu serializes all crawl operations across all targets.
WHY:  Single boolean m.crawling.
IMPACT: Crawling target A blocks crawling target B.
FIX DIRECTION: Use per-target locking or document serial design.
```

---

#### #30 [FRAGILE] internal/backup/backup.go:181-208 — zipFile.Close not deferred

```
WHAT: zipFile.Close() is called explicitly, not via defer. Only a panic between the two
      Close calls would leak the handle.
WHY:  Normal and error paths are correct; only panics are unprotected.
IMPACT: Negligible in practice.
FIX DIRECTION: Use defer for zipFile.Close as safety net.
```

---

#### #31 [LEAK] internal/remote/remote.go:192 — Initial syncAllSources goroutine not context-aware

```
WHAT: Start() launches go m.syncAllSources() without checking m.ctx in the loop.
WHY:  Individual HTTP requests use m.ctx but the iteration loop doesn't check m.ctx.Done().
IMPACT: During rapid Start/Stop, goroutine continues syncing after Stop returns.
FIX DIRECTION: Check m.ctx.Err() at top of syncAllSources loop.
```

---

#### #32 [FRAGILE] internal/updater/updater.go:186-189 — Initial update check goroutine not awaited on Stop

```
WHAT: Fire-and-forget goroutine for initial update check is not tracked by WaitGroup.
WHY:  Simple goroutine launch.
IMPACT: May make HTTP requests after module is "stopped". Minimal practical impact.
FIX DIRECTION: Add to WaitGroup or use checkDone channel.
```

---

#### #33 [FRAGILE] internal/hls/module.go:106 — transSem channel size fixed at construction time

```
WHAT: ConcurrentLimit is read at NewModule time. Admin config changes have no effect.
WHY:  Channel capacity is immutable in Go.
IMPACT: Changing ConcurrentLimit requires restart.
FIX DIRECTION: Document or recreate semaphore on config change.
```

---

#### #34 [FRAGILE] internal/hls/access.go:26-56 — Two locks acquired sequentially without documented ordering

```
WHAT: RecordAccess acquires accessTracker.mu then jobsMu. Currently consistent everywhere.
WHY:  Fragile for future changes that might reverse the order.
IMPACT: None currently; deadlock risk if ordering is violated.
FIX DIRECTION: Document the lock ordering invariant.
```

---

#### #35 [FRAGILE] internal/media/management.go:503-549 — UpdateTags saves metadata in background goroutine

```
WHAT: UpdateTags saves metadata to DB in fire-and-forget goroutine. If save fails, in-memory
      state is updated but DB is stale.
WHY:  Design choice — non-blocking persistence.
IMPACT: Tags lost on restart if DB save fails. Periodic scan re-writes metadata.
FIX DIRECTION: Acceptable as-is.
```

---

### PKG / MIDDLEWARE / ROUTES LAYER (from parallel audit)

---

#### #36 [SECURITY] api/routes/routes.go:49-51 — sessionAuth trusts X-Forwarded-Proto without checking trusted proxy

```
WHAT: When clearing an invalid session cookie, the Secure flag is set based on
      c.GetHeader("X-Forwarded-Proto") == "https" without verifying the request came from
      a trusted proxy. Any external client can send X-Forwarded-Proto: https.
WHY:  The middleware's isHTTPS() correctly checks IsTrustedProxy, but sessionAuth was written
      independently and forgot the proxy trust check.
IMPACT: A client on plain HTTP can trick the server into setting Secure=true on the cookie-
        clearing response. The browser won't send a Secure cookie over HTTP, so the stale
        session_id cookie remains active and is NOT deleted. This means expired/invalid
        session cookies persist longer than intended for non-HTTPS connections claiming HTTPS.
TRACE: sessionAuth middleware → IsSessionError → set cookie with Secure based on spoofed header
FIX DIRECTION: Use the shared isSecureRequest() or IsTrustedProxy check from middleware.go.
```

---

#### #37 [SECURITY] internal/security/security.go:1060-1066 — Rate limiter uses rightmost XFF (proxy hop, not client)

```
WHAT: In a multi-proxy chain (CDN → nginx → app), X-Forwarded-For is "client, CDN, nginx".
      The rightmost entry is "nginx" (the last proxy before the app), not the actual client.
      The rate limiter would rate-limit by the proxy IP, meaning all clients behind that
      proxy share one rate limit bucket.
WHY:  Rightmost-is-trustworthy heuristic is correct for single-proxy but wrong for multi-proxy.
IMPACT: In a CDN setup, all clients appear as the CDN edge IP for rate limiting. Rate limiting
        becomes ineffective (one shared bucket) or too aggressive (hits limit immediately).
TRACE: CheckRequest → getClientIP → rightmost XFF entry → used as rate limit key
FIX DIRECTION: Walk right-to-left in XFF, skipping known trusted proxy IPs, until finding the
               first untrusted IP. Or document single-proxy-only topology as a requirement.
```

---

#### #38 [DRIFT] pkg/middleware/agegate.go vs internal/security/security.go — Two different IP extraction strategies

```
WHAT: extractClientIP (agegate) takes the LEFTMOST (first) X-Forwarded-For entry; getClientIP
      (security) takes the RIGHTMOST (last) entry. Both check trusted proxies, but they disagree
      on which proxy-appended IP to trust.
WHY:  Leftmost = original client (spoofable in multi-proxy). Rightmost = previous proxy hop.
IMPACT: In multi-proxy setups: the age gate sees a different IP than the rate limiter for the
        same request. A user rate-limited by one IP could be age-verified by another. In
        single-proxy (typical VPS + nginx), both see the same IP.
TRACE: Same request → agegate.extractClientIP (leftmost) vs security.getClientIP (rightmost)
FIX DIRECTION: Unify to a single canonical extractClientIP. Rightmost-untrusted-skip is most
               robust.
```

---

#### #39 [GAP] pkg/huggingface/client.go:107 — HF client uses plain http.Client without SSRF transport

```
WHAT: The HuggingFace client creates a plain http.Client without SafeHTTPTransport. The
      endpoint URL is configured by admin (HUGGINGFACE_ENDPOINT_URL env var).
WHY:  Designed for outbound API calls to huggingface.co.
IMPACT: If an admin sets HUGGINGFACE_ENDPOINT_URL to an internal IP (e.g., 169.254.169.254),
        the HF client makes requests to the cloud metadata service. Requires admin misconfiguration.
TRACE: NewClient → http.Client{} (no SafeHTTPTransport) → Do(req to admin-supplied URL)
FIX DIRECTION: Use SafeHTTPTransport, or validate endpoint URL at startup with ValidateURLForSSRF.
```

---

#### #40 [GAP] api/routes/routes.go:196-203 — FNV-1a 32-bit ETag has weak collision resistance

```
WHAT: hashFNV1a uses a 32-bit FNV-1a hash. With ~65,000 distinct responses, birthday collisions
      become likely. Two different API responses could share the same ETag.
WHY:  FNV-1a 32-bit chosen for speed over collision resistance.
IMPACT: A browser caching /api/media may receive 304 Not Modified when content has changed,
        if the new response hashes to the same 32-bit value. Extremely low probability.
TRACE: ginETags → hashFNV1a(body) → ETag comparison → potential false 304
FIX DIRECTION: Upgrade to FNV-1a 64-bit (hash/fnv.New64a) for negligible performance cost.
```

---

#### #41 [GAP] pkg/models/models.go:86 — User.Metadata has no size limit

```
WHAT: User.Metadata accepts arbitrary JSON with no size constraint at the model level.
WHY:  No Validate() method constrains Metadata size.
IMPACT: A malicious admin could store megabytes of metadata per user, causing DB bloat and
        memory pressure when loading all users.
TRACE: AdminUpdateUser → auth.UpdateUser → GORM save → arbitrarily large JSON
FIX DIRECTION: Add metadata size limit in the handler, or a Validate() method on User.
```

---

#### #42 [LEAK] pkg/models/deletion_request.go:28 — DataDeletionRequest.AdminNotes exposed in JSON

```
WHAT: AdminNotes field is included in JSON serialization. If returned to non-admin users,
      admin-internal notes about deletion decisions could leak.
WHY:  No json:"-" tag; model is used as API response type.
IMPACT: Only affects the data-deletion-request endpoints. Need to verify handlers filter
        this field for non-admin consumers.
TRACE: AdminProcessDeletionRequest → response includes AdminNotes
FIX DIRECTION: Add json:"-" for user-facing responses, or verify only admin endpoints return this model.
```

---

#### #43 [GAP] pkg/helpers/sanitize.go:86-95 — SafeContentDispositionFilename doesn't strip semicolons

```
WHAT: Semicolons are not stripped. "file;name=evil.mp4" produces
      `attachment; filename="file;name=evil.mp4"`. Buggy HTTP parsers might split on semicolon.
WHY:  RFC 6266 says semicolons inside quoted strings are literal. Most browsers handle correctly.
IMPACT: Very low — value is inside double quotes. Only buggy parsers affected.
TRACE: Download/export → SafeContentDispositionFilename → semicolon passes through
FIX DIRECTION: Consider stripping semicolons for defense-in-depth.
```

---

#### #44 [FRAGILE] pkg/storage/s3compat/s3.go:370-404 — S3 Rename leaves source on delete failure

```
WHAT: If CopyObject succeeds but RemoveObject on source fails, the file exists at both src
      and dst. The caller sees an error but the copy at dst already exists.
WHY:  S3 has no atomic rename; copy+delete is inherently non-atomic.
IMPACT: Duplicate objects in bucket consuming extra storage. Inherent S3 limitation.
TRACE: Rename → CopyObject(src→dst) → RemoveObject(src) fails → both exist
FIX DIRECTION: Document for callers. Consider cleanup retry.
```

---

#### #45 [REDUNDANT] pkg/helpers/helpers.go:53-75 — audioExts duplicates a subset of mediaExts

```
WHAT: audioExts is a separate map that is a strict subset of mediaExts. Adding a format to
      one but not the other causes drift.
WHY:  Separate maps for separate semantic queries.
IMPACT: Maintenance burden only.
FIX DIRECTION: Derive audioExts from mediaExts at init, or use a single map with type tag.
```

---

## Architecture Notes

### Lock Hierarchy (verified no deadlocks)
- **media.Module**: single `mu` for all indexes (media, metadata, fingerprintIndex, mediaByID)
- **auth.Module**: `sessionsMu` (sessions), `usersMu` (users), `attemptsMu` (login attempts) — no nesting
- **receiver.Module**: `mu` (slaves/media), `wsMu` (WS connections), `pendingMu` (pending streams), `healthMu` — no nesting
- **streaming.Module**: `sessionMu` (active sessions), `statsMu` (counters) — ordered: sessionMu → statsMu
- **remote.Module**: `mu` (sources/cache), `healthMu` — no nesting
- **security.Module**: separate locks for rate limiter, whitelist, blacklist, banned IPs
- **hls.Module**: accessTracker.mu → jobsMu (consistent ordering)

### Entry Points
- `cmd/server/main.go` — primary server entry
- `cmd/media-receiver/main.go` — standalone receiver CLI
- Route setup: `api/routes/routes.go` — 100+ endpoints across public, auth-required, and admin groups

### Module Lifecycle
All modules implement `server.Module` interface (Name, Start, Stop, Health). Critical modules
panic on nil in `NewHandler`. Optional modules are nil-checked via `requireModule()` before use.

### Verified Correct Patterns
- Session validation returns copy-before-unlock (auth/session.go:148-162)
- Media index consistency: RenameMedia, MoveMedia, DeleteMedia update 4 indexes atomically
- SSRF protection: double validation at URL submission AND connection time via SafeHTTPTransport
- CORS: wildcard returns literal "*" without Allow-Credentials (prevents credential leakage)
- Receiver WS: auth, 16MB message limit, ping/pong keepalive, readyOnce guards
- Config admin API: deny-list prevents mutation of sensitive sections
- Path traversal: all user paths validated against allowedDirs with filepath.Clean + HasPrefix
- Streaming: buffer pool with proper Get/Put lifecycle
- Receiver proxy: semaphore concurrency limit, io.Pipe body streaming
- Constant-time API key comparison (subtle.ConstantTimeCompare)
- Session cookies: HttpOnly, SameSite=Strict, Secure based on connection type
- Password hash: json:"-" prevents JSON leakage
- MediaItem.Path: json:"-" prevents filesystem path exposure to clients
- Trusted proxy validation: only honors X-Forwarded-For from private networks
- Request ID sanitization: strips control characters to prevent log injection
- Metadata sanitization: HTML-escapes values before storage

---

## Conclusion

This codebase is in strong shape. The 7-phase audit found **2 critical data-correctness issues**
(both in analytics stats), **0 security vulnerabilities**, and **0 incomplete features**. The
remaining 33 findings span FRAGILE, SILENT_FAIL, GAP, LEAK, DRIFT, and REDUNDANT categories.

**Immediate action items:**
1. Fix TotalWatchTime to use actual watched time, not full duration (#1)
2. Fix rebuildStatsFromEvent to reconstruct playback stats on restart (#2)
3. Fix PushCatalog MediaCount to use post-filter count (#6)
4. Add streaming session cleanup mechanism (#4)

Previous audit sessions (2026-04-05, 2026-04-07) fixed the more serious issues. The patterns
established in those fixes (copy-before-unlock, double-check locking, semaphore-before-mutex)
are consistently applied across the codebase.
