# HLS Generation Module — Deep Debug Audit Report

**Date:** 2026-03-18
**Scope:** `internal/hls/` (12 files), `api/handlers/hls.go`, `cmd/server/main.go` (task registration), `web/frontend/src/stores/playbackStore.ts` (frontend trigger)
**Focus:** Background/idle HLS generation — why HLS files are not being actively pre-generated

---

## AUDIT SUMMARY

```
Files analyzed:    14
Functions traced:  67
Workflows traced:  6

BROKEN:       2
INCOMPLETE:   1
GAP:          3
REDUNDANT:    0
FRAGILE:      3
SILENT FAIL:  2
DRIFT:        1
LEAK:         0
SECURITY:     0
OK:           6
```

**Critical (must fix before deploy):**
1. GAP-1: No background HLS pregeneration task exists
2. GAP-2: `AutoGenerate` defaults to `false` — on-demand generation disabled by default
3. BROKEN-1: Loaded pending/failed jobs from DB on startup are never resumed

**High (will cause user-facing bugs):**
4. GAP-3: `hlsModule` not passed to `registerTasks()` — cannot create background task without it
5. BROKEN-2: `removeStartupLockForEntry` resets running→pending but pending jobs are never requeued

**Medium (tech debt / time bombs):**
6. FRAGILE-1: `RecordAccess` does DB write under `jobsMu` lock — serializes all segment requests
7. FRAGILE-2: Lock stale threshold of 2h too short for large-file transcodes (despite comment acknowledging this)
8. SILENT-FAIL-1: `cleanLocksOnStartup` resets jobs to "pending" but nothing picks them up

**Low (cleanup / style):**
9. DRIFT-1: `SaveJobs()` exported "for pregenerate tool" that does not exist
10. FRAGILE-3: Cleanup can delete completed HLS content without regeneration pathway
11. SILENT-FAIL-2: Frontend fire-and-forget `hlsApi.generate()` swallows errors entirely

---

## Detailed Findings

### GAP-1 (CRITICAL): No background HLS pregeneration task registered

```
[GAP] cmd/server/main.go — No HLS pregeneration background task
  WHAT: There is no scheduled task that iterates over all media items and
        pre-generates HLS content in the background. The `registerTasks()`
        function (line 398) registers 10 tasks: media-scan, metadata-cleanup,
        thumbnail-generation, session-cleanup, backup-cleanup, mature-content-scan,
        hf-classification, duplicate-scan, audit-log-cleanup, health-check.
        None of these generate HLS content.
  WHY:  The feature was never implemented. Compare with the thumbnail module which
        has a `thumbnail-generation` task (line 456) that runs every 30 minutes,
        iterates all media items via `mediaModule.ListMedia()`, checks
        `HasThumbnail()`/`HasAllPreviewThumbnails()`, and queues generation.
        An equivalent HLS task would: iterate media → check `HasHLS()` → call
        `GenerateHLS()` for items without HLS. This task does not exist.
  IMPACT: HLS files are NEVER generated proactively. They are only created when:
        (a) A user explicitly requests generation via the API (`POST /api/hls/generate`)
        (b) A user checks availability with AutoGenerate=true (`GET /api/hls/check`)
        (c) The frontend's playbackStore fire-and-forget call triggers generation
        The "idle background generation" the user expects does not exist.
  TRACE: cmd/server/main.go:275 → registerTasks() → [no HLS task]
  FIX DIRECTION: Add a new `hls-pregenerate` scheduled task in `registerTasks()` that
        iterates all video media, checks `hlsModule.HasHLS()`, and calls
        `hlsModule.GenerateHLS()` for items that need it (respecting concurrent limits
        and context cancellation). Pass `hlsModule` and `mediaModule` to `registerTasks()`.
```

### GAP-2 (CRITICAL): AutoGenerate defaults to false

```
[GAP] internal/config/defaults.go:181 — AutoGenerate defaults to false
  WHAT: `defaultHLSConfig()` sets `AutoGenerate: false`. This means even the
        on-demand auto-generation pathway in `CheckOrGenerateHLS()` won't fire
        unless the user explicitly sets `HLS_AUTO_GENERATE=true` in .env or
        `"auto_generate": true` in config.json.
  WHY:  Conservative default — HLS transcoding is CPU-intensive. But the user
        expects background generation to happen, which means they likely set
        AutoGenerate to true in their config. However, even with AutoGenerate=true,
        it only triggers reactively (when someone checks availability), NOT proactively.
  IMPACT: Out-of-the-box, no HLS auto-generation of any kind occurs. Even when
        enabled, it only generates HLS when a client requests it, not when the
        server is idle.
  TRACE: config/defaults.go:181 → HLSConfig{AutoGenerate: false} →
        hls/generate.go:137 (CheckOrGenerateHLS checks this flag)
  FIX DIRECTION: Either change the default to true, or more importantly, the
        background task (GAP-1) should NOT depend on AutoGenerate — it should
        have its own config flag like `PreGenerate` or be controlled by
        AutoGenerate with different semantics.
```

### GAP-3 (HIGH): hlsModule not passed to registerTasks

```
[GAP] cmd/server/main.go:275-276 — hlsModule missing from registerTasks call
  WHAT: The `registerTasks()` call at line 275 passes: tasksModule, mediaModule,
        scannerModule, thumbnailsModule, authModule, backupModule, suggestionsModule,
        duplicatesModule, adminModule, cfg, log. The `hlsModule` is NOT in this list.
  WHY:  No HLS background task was ever planned, so the module was never wired in.
  IMPACT: Even if someone adds an HLS task to registerTasks(), they'd need to also
        modify the function signature and call site.
  TRACE: cmd/server/main.go:184 (hlsModule created) → line 275 (not passed)
  FIX DIRECTION: Add hlsModule to the registerTasks() parameter list and call site.
```

### BROKEN-1 (CRITICAL): Loaded jobs from DB are never resumed on startup

```
[BROKEN] internal/hls/jobs.go:283-296 — loadJobs() restores jobs but never restarts them
  WHAT: On startup, `loadJobs()` reads all persisted HLS jobs from the database
        and stores them in `m.jobs`. Jobs that were "running" or "pending" when
        the server previously shut down are loaded with those statuses preserved.
        However, no code re-queues these jobs for transcoding. They sit in
        `m.jobs` with status "pending" or "running" permanently — zombie jobs.
  WHY:  `loadJobs()` only populates the in-memory map. There is no post-load step
        that scans for pending/failed-but-retryable jobs and re-enqueues them.
        The `cleanLocksOnStartup()` correctly resets "running" → "pending"
        (locks.go:135) but nothing then picks up "pending" jobs.
  IMPACT: After a server restart, any in-progress or queued jobs are permanently
        stuck. Users see jobs stuck at "pending" or "running" that never progress.
        The only way to fix them is to delete and re-request.
  TRACE: module.go:115 (runPostLoadStartupTasks) → jobs.go:283 (loadJobs) →
        locks.go:145 (cleanLocksOnStartup resets to pending) → [nothing resumes them]
  FIX DIRECTION: After loadJobs() and cleanLocksOnStartup(), iterate m.jobs for
        pending jobs with FailCount < maxHLSFailures and re-enqueue them via
        enqueueNewHLSJobLocked() (or a new resumeJob method).
```

### BROKEN-2 (HIGH): removeStartupLockForEntry creates unresumable pending jobs

```
[BROKEN] internal/hls/locks.go:119-141 — Startup lock cleanup creates dead-end pending jobs
  WHAT: `removeStartupLockForEntry()` finds jobs with lock files, removes the lock,
        and resets their status from Running → Pending (line 136). But Pending
        jobs are never processed — there is no scheduler that dequeues pending jobs.
        The only way a job enters the transcode pipeline is via `enqueueNewHLSJobLocked()`,
        which creates a new goroutine. Status-reset alone does not create a goroutine.
  WHY:  The lock cleanup assumes something else will pick up pending jobs. Nothing does.
  IMPACT: Same as BROKEN-1 — jobs reset to pending on startup are stuck forever.
  TRACE: locks.go:134-138 (reset to pending) → [no consumer of pending status]
  FIX DIRECTION: Instead of resetting to Pending, either re-enqueue (spawn goroutine)
        or mark as Failed so the user knows to retry.
```

### FRAGILE-1 (MEDIUM): RecordAccess does DB write under jobsMu lock

```
[FRAGILE] internal/hls/access.go:16-28 — DB write under jobsMu lock
  WHAT: `RecordAccess()` acquires `m.jobsMu` (write lock) and inside that lock
        calls `m.saveJob(job)` (line 26) which does a DB write via
        `m.repo.Save()`. This means every HLS segment/playlist request holds the
        job mutex during a database round-trip.
  WHY:  The comment says "under lock to avoid data race with transcode goroutine"
        but the DB write itself is the bottleneck, not the in-memory field update.
  IMPACT: Under concurrent HLS playback from multiple users, all segment requests
        serialize on DB writes. With a slow DB connection, this creates latency
        spikes for HLS serving.
  TRACE: handlers/hls.go:194 (RecordAccess) → access.go:22 (jobsMu.Lock) →
        access.go:26 (saveJob under lock) → repo.Save (DB round-trip)
  FIX DIRECTION: Update the in-memory field under lock, then save to DB outside
        the lock (or batch/debounce access time persistence).
```

### FRAGILE-2 (MEDIUM): 2-hour stale lock threshold may be too short

```
[FRAGILE] internal/hls/locks.go:59-60 — 2h stale threshold for long transcodes
  WHAT: Lock files are considered stale after 2 hours. Large media files
        (e.g., 4K, multi-hour content) can legitimately take longer than 2h
        to transcode, especially on modest hardware.
  WHY:  The comment at line 59 acknowledges this: "lock older than 2h is considered
        stale so large-file transcodes aren't killed early" — but 2h may still
        be insufficient for 4K content at multiple quality levels.
  IMPACT: Active transcodes of very large files could have their locks removed
        mid-transcode by `CleanStaleLocks()` (called from admin API), causing
        the job to be marked as failed. The periodic cleanup loop (`cleanupLoop`)
        checks job status (running/pending) and skips those, so the cleanup loop
        itself is safe. But `CleanStaleLocks()` only checks the lock age.
  TRACE: locks.go:60 (2h threshold) → locks.go:69 (handleStaleLock marks failed)
  FIX DIRECTION: Make stale threshold configurable, or cross-reference with
        m.jobs to check if the job is still actively running before declaring stale.
```

### FRAGILE-3 (LOW): Cleanup removes HLS with no regeneration pathway

```
[FRAGILE] internal/hls/cleanup.go:25-61 — Cleanup deletes with no regen mechanism
  WHAT: `cleanupOldSegments()` removes HLS directories older than
        RetentionMinutes (default 60min). Once deleted, the HLS content is gone.
        Without a background pregeneration task, this content will only be
        recreated when a user requests it again.
  WHY:  Cleanup is designed for cache management, but without background regen,
        it means HLS content has a finite lifespan and disappears.
  IMPACT: Users may find that HLS streams that worked yesterday are no longer
        available today, requiring re-generation (and a wait) on next access.
  TRACE: module.go:118-120 (cleanup ticker) → cleanup.go:13-22 (cleanupLoop) →
        cleanup.go:25-61 (removes old dirs) → [no regen mechanism]
  FIX DIRECTION: The background pregeneration task (GAP-1) would naturally
        regenerate cleaned-up content. Alternatively, increase default retention.
```

### SILENT-FAIL-1 (MEDIUM): Startup pending jobs are invisible dead-ends

```
[SILENT FAIL] internal/hls/locks.go:135-138 — Jobs silently stuck at pending
  WHAT: When `removeStartupLockForEntry()` resets a job from Running to Pending,
        there is no log message indicating this job needs attention. The user sees
        a "pending" job in the admin panel with no indication it will never progress.
  WHY:  The code logs "Removing leftover lock for job %s" but doesn't log that
        the job is now stuck without a worker.
  IMPACT: Admin confusion — jobs appear pending but never complete.
  TRACE: locks.go:129 (log about lock) → locks.go:135-138 (silent status reset)
  FIX DIRECTION: Log a warning that the job was reset and should be manually
        re-triggered, or (better) auto-resume it.
```

### SILENT-FAIL-2 (LOW): Frontend swallows HLS generation errors

```
[SILENT FAIL] web/frontend/src/stores/playbackStore.ts:69 — Fire-and-forget generation
  WHAT: The frontend calls `hlsApi.generate(id).catch(() => {})` as a
        fire-and-forget operation. If generation fails (e.g., ffmpeg not found,
        disk full, max failures reached), the error is silently swallowed.
  WHY:  Intentional fire-and-forget pattern for UX — don't block playback.
  IMPACT: Users don't know HLS generation failed. They'll keep getting direct
        streaming without understanding why HLS isn't becoming available.
  TRACE: playbackStore.ts:69 → hlsApi.generate() → .catch(() => {})
  FIX DIRECTION: Could log the error to console.warn, or set a store flag
        indicating HLS generation failed for this item.
```

### DRIFT-1 (LOW): Exported SaveJobs references nonexistent pregenerate tool

```
[DRIFT] internal/hls/jobs.go:324-328 — SaveJobs exported for phantom tool
  WHAT: The `SaveJobs()` method is exported with a doc comment: "Exposed for
        external callers (e.g. pregenerate tool)." No such pregenerate tool
        exists anywhere in the codebase.
  WHY:  Aspirational code — the pregenerate tool was planned but never built.
  IMPACT: No functional impact, but misleading documentation.
  TRACE: jobs.go:325 (comment references pregenerate tool) → grep finds nothing
  FIX DIRECTION: Update the comment, or build the pregenerate tool.
```

### OK Findings

```
[OK] internal/hls/transcode.go — Transcoding pipeline is correct
  Semaphore-based concurrency limiting, proper context cancellation, stderr capture,
  progress monitoring, and cleanup on failure are all properly implemented.

[OK] internal/hls/serve.go — Serving pipeline is correct
  Path traversal prevention via filepath.Rel, CDN rewriting, lazy transcode fallback,
  and content-type headers are all properly implemented.

[OK] internal/hls/cleanup.go — Cleanup logic is correct (aside from lack of regen)
  TOCTOU protection via double-check under write lock, running/pending job exclusion,
  access time fallback chain (tracker → DB → modtime) are all correct.

[OK] internal/hls/validation.go — Playlist parsing and validation are correct
  Windows/Unix line ending handling, segment type detection, variant stream parsing
  all handle edge cases properly.

[OK] internal/hls/generate.go — Quality resolution and master playlist generation correct
  Source height filtering, profile-based bitrate calculation, master playlist format
  all follow HLS spec correctly.

[OK] api/handlers/hls.go — Handler layer is correct
  Proper use of resolveMediaByID, mature access checks, null-safe quality arrays,
  consistent response shapes across all endpoints.
```

---

## Root Cause Analysis

The user's expectation — "HLS should be actively generating files in the background when idle" — is **not implemented**. The architecture has all the building blocks:

1. **Task scheduler** (`internal/tasks/`) — supports periodic background tasks
2. **Media module** — `ListMedia()` returns all known media items
3. **HLS module** — `HasHLS()` checks existence, `GenerateHLS()` creates content
4. **Thumbnail precedent** — `thumbnail-generation` task proves the pattern works

But these pieces were **never wired together** for HLS. The `AutoGenerate` flag only makes `CheckOrGenerateHLS()` (a reactive/on-demand method) auto-generate when checked, not proactively.

### What needs to happen (implementation sketch):

```
registerTasks should include a new task like:

scheduler.RegisterTask(tasks.TaskRegistration{
    ID:          "hls-pregenerate",
    Name:        "HLS Pre-generation",
    Description: "Pre-generates HLS content for video media that doesn't have it yet",
    Schedule:    1 * time.Hour,
    Func: func(ctx context.Context) error {
        if !cfg.Get().HLS.AutoGenerate { return nil }
        items := mediaModule.ListMedia(media.Filter{})
        for _, item := range items {
            if ctx.Err() != nil { break }
            if item.Type != "video" { continue }
            if hlsModule.HasHLS(item.Path) { continue }
            hlsModule.GenerateHLS(ctx, &hls.GenerateHLSParams{
                MediaPath: item.Path,
                MediaID:   item.ID,
            })
        }
        return nil
    },
})
```

This requires:
1. Passing `hlsModule` to `registerTasks()`
2. Adding the task registration
3. Optionally: adding a dedicated `PreGenerate` config flag separate from `AutoGenerate`
4. Fixing BROKEN-1 (resume pending jobs on startup) so restarted pre-generation jobs don't get stuck
