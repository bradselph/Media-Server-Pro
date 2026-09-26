/**
 * HLS composable for the player page.
 *
 * Checks HLS availability for a media item, attaches hls.js to a video element,
 * manages quality selection, and cleans up on unmount.
 *
 * Falls back to direct video src if HLS is not available or not supported.
 */

import type {Ref} from 'vue'
import {HLS_TUNING_CONFIG} from '~/utils/hlsConfig'

export interface HLSQuality {
    index: number
    height: number
    width: number
    bitrate: number
    name: string
    codec?: string
    fps?: number
}

export interface UseHLSReturn {
    /** Whether HLS is available for this media. */
    hlsAvailable: Ref<boolean>
    /** Whether HLS has been activated (hls.js is attached to the video element). */
    hlsActivated: Ref<boolean>
    /** The master playlist URL when available. */
    hlsUrl: Ref<string | null>
    /** Whether HLS is currently loading/initializing. */
    hlsLoading: Ref<boolean>
    /** HLS error message, if any. */
    hlsError: Ref<string | null>
    /** Whether HLS is currently reconnecting after a network failure. */
    hlsReconnecting: Ref<boolean>
    /** Available quality levels. */
    qualities: Ref<HLSQuality[]>
    /** Current quality index (-1 = auto). */
    currentQuality: Ref<number>
    /** Auto-selected level index when in auto mode. */
    autoLevel: Ref<number>
    /** Estimated bandwidth in bps. */
    bandwidth: Ref<number>
    /** Select a quality level by index (-1 for auto). */
    selectQuality: (index: number) => void
    /** Activate HLS playback (switch from direct to HLS). */
    activateHLS: () => Promise<void>
    /** HLS job progress (0-100) while generating. */
    jobProgress: Ref<number>
    /** Whether HLS job is currently generating. */
    jobRunning: Ref<boolean>
    /** Whether the job has been running long enough to fall back to a slow background poll (still in progress — not a failure). */
    jobSlow: Ref<boolean>
    /** Re-run the availability check for the current media (e.g. after manual generation). */
    recheck: () => void
}

const QUALITY_PREF_KEY = 'media-server-quality-pref'

function getQualityName(height: number): string {
    if (height >= 2160) return '4K'
    if (height >= 1440) return '1440p'
    if (height >= 1080) return '1080p'
    if (height >= 720) return '720p'
    if (height >= 480) return '480p'
    if (height >= 360) return '360p'
    return `${height}p`
}

function getSavedQualityPref(): number {
    try {
        const val = localStorage.getItem(QUALITY_PREF_KEY)
        return val ? Number.parseInt(val, 10) : 0
    } catch {
        return 0
    }
}

/**
 * Parse a default_quality preference string ('auto' | '1080p' | '720p' | …)
 * into a target height in pixels. Returns 0 for auto/unspecified/unparseable.
 */
function parseQualityPref(pref: string | null | undefined): number {
    if (!pref || pref === 'auto') return 0
    const n = Number.parseInt(pref, 10) // '1080p' -> 1080
    return Number.isFinite(n) && n > 0 ? n : 0
}

/**
 * Pick the quality level matching the requested height, falling back to the
 * highest level at or below it (so a 720p preference still resolves on a
 * 1080p/480p ladder). Returns undefined when nothing is at or below.
 */
function pickQualityAtOrBelow(levels: HLSQuality[], height: number): HLSQuality | undefined {
    const exact = levels.find(l => l.height === height)
    if (exact) return exact
    return levels
        .filter(l => l.height <= height)
        .sort((a, b) => b.height - a.height)[0]
}

function saveQualityPref(height: number): void {
    try {
        localStorage.setItem(QUALITY_PREF_KEY, String(height))
    } catch {
        // localStorage may be full or disabled
    }
}

export interface PlaybackState {
    time: number
    wasPlaying: boolean
}

/**
 * Snapshot the position/paused state of a media element before a source swap
 * (direct <-> HLS) that would otherwise reset the element's load state and
 * silently rewind it to 0. Call sites only act on this when `time > 0`, so a
 * fresh element (nothing played yet) is left alone.
 */
export function capturePlaybackState(el: { currentTime: number; paused: boolean } | null | undefined): PlaybackState {
    return {
        time: el?.currentTime ?? 0,
        wasPlaying: !!el && !el.paused,
    }
}

export interface NetworkErrorDecision {
    action: 'retry' | 'fallback'
    delayMs: number
}

/**
 * Decide how to respond to a fatal hls.js NETWORK_ERROR. When something has
 * already buffered, a transient outage is worth riding out with a generous
 * exponential backoff (up to 8 attempts, capped at 30s) — the existing
 * MediaSource keeps playing from its buffer while we wait for the server.
 * When nothing has ever buffered (the very first manifest/fragment request
 * failed), there is no buffered content to protect: allow at most one quick
 * retry, then fall back to direct playback immediately instead of leaving
 * the player blank for the ~49s the full backoff loop would otherwise take.
 */
export function decideNetworkErrorAction(hasBuffered: boolean, attempt: number): NetworkErrorDecision {
    if (!hasBuffered) {
        return attempt <= 1 ? {action: 'retry', delayMs: 1000} : {action: 'fallback', delayMs: 0}
    }
    return attempt <= 8
        ? {action: 'retry', delayMs: Math.min(1000 * Math.pow(1.5, attempt - 1), 30000)}
        : {action: 'fallback', delayMs: 0}
}

/**
 * Message to surface when an HLS job status update indicates the job will
 * not complete on its own (failed or canceled). Returns null for statuses
 * that are still in progress or already succeeded, so callers only touch
 * hlsError for genuine terminal failures.
 */
export function resolveTerminalHlsError(status: string, error?: string | null): string | null {
    if (status === 'failed' || status === 'canceled') return error || 'HLS generation failed'
    return null
}

const FAST_POLL_INTERVAL_MS = 3000
const SLOW_POLL_INTERVAL_MS = 60 * 1000
const FAST_POLL_WINDOW_MS = 30 * 60 * 1000 // 30 minutes of fast (3s) polling
const HARD_POLL_LIMIT_MS = 3 * 60 * 60 * 1000 // outer bound — past the backend's 2h stale-lock threshold

export type PollPhase = 'fast' | 'slow' | 'expired'

/**
 * Which polling cadence doPollCheck should use for a job that has been
 * running for `elapsedMs`. 'fast' is the normal 3s poll; past
 * FAST_POLL_WINDOW_MS a legitimately slow encode (large file, multiple
 * qualities transcoded sequentially, CPU-constrained server) switches to a
 * 60s background poll rather than giving up outright; HARD_POLL_LIMIT_MS is
 * the sane outer bound where we finally stop.
 */
export function pollPhaseFor(elapsedMs: number): PollPhase {
    if (elapsedMs > HARD_POLL_LIMIT_MS) return 'expired'
    if (elapsedMs > FAST_POLL_WINDOW_MS) return 'slow'
    return 'fast'
}

export function useHLS(
    videoRef: Ref<HTMLVideoElement | null>,
    mediaId: Ref<string>,
    opts?: { defaultQuality?: () => string | null | undefined },
): UseHLSReturn {
    const hlsApi = useHlsApi()
    const settingsApi = useSettingsApi()

    const hlsAvailable = ref(false)
    const hlsActivated = ref(false)
    const hlsUrl = ref<string | null>(null)
    const hlsLoading = ref(false)
    const hlsError = ref<string | null>(null)
    const hlsReconnecting = ref(false)
    const qualities = ref<HLSQuality[]>([])
    const currentQuality = ref(-1)
    const autoLevel = ref(-1)
    const bandwidth = ref(0)
    const jobProgress = ref(0)
    const jobRunning = ref(false)
    const jobSlow = ref(false)

    let hlsInstance: import('hls.js').default | null = null
    let pollTimer: ReturnType<typeof setInterval> | null = null
    let checkDebounce: ReturnType<typeof setTimeout> | null = null
    let networkRetryTimer: ReturnType<typeof setTimeout> | null = null
    let activationGen = 0
    let checkGen = 0
    let pollStartTime = 0
    let pollPhase: PollPhase = 'fast'
    let activePollId = ''
    const MAX_CONSECUTIVE_ERRORS = 10

    function cleanup() {
        // Invalidate any in-flight activation so a stale attachHLS (paused on its
        // hls.js dynamic import) can't attach the previous media's stream to the
        // (still-connected) video element after the user switches items.
        activationGen++
        checkGen++
        if (checkDebounce) {
            clearTimeout(checkDebounce)
            checkDebounce = null
        }
        if (pollTimer) {
            clearInterval(pollTimer)
            pollTimer = null
        }
        if (networkRetryTimer) {
            clearTimeout(networkRetryTimer)
            networkRetryTimer = null
        }
        if (hlsInstance) {
            hlsInstance.destroy()
            hlsInstance = null
        }
        qualities.value = []
        currentQuality.value = -1
        autoLevel.value = -1
        bandwidth.value = 0
        hlsLoading.value = false
        hlsError.value = null
        hlsReconnecting.value = false
        hlsActivated.value = false
        jobProgress.value = 0
        jobRunning.value = false
        jobSlow.value = false
        pollPhase = 'fast'
        activePollId = ''
        consecutiveErrors.count = 0
    }

    function selectQuality(index: number) {
        if (!hlsInstance) return
        hlsInstance.currentLevel = index
        currentQuality.value = index

        if (index === -1) {
            // Explicit "Auto" means fully adaptive with no ceiling — clear any
            // cap that a default_quality preference may have applied so ABR can
            // use the whole ladder.
            hlsInstance.autoLevelCapping = -1
            saveQualityPref(0)
        } else {
            const level = hlsInstance.levels[index]
            if (level) saveQualityPref(level.height)
        }
    }

    async function attachHLS(url: string, gen: number, preSwitch: PlaybackState = {time: 0, wasPlaying: false}) {
        const el = videoRef.value
        if (!el) return

        // Restore the position/play-state captured before the source swap once
        // the new source has loaded metadata. Guarded on preSwitch.time > 0 so a
        // genuine first-ever load (nothing played yet) is left to player.vue's
        // own restorePosition() flow, and on the gen token so a superseded
        // activation can't seek an element a newer one now owns. Reused for the
        // reverse handoff too (HLS -> direct fallback on a fatal error below),
        // passing the position captured right before that fallback instead.
        function scheduleResume(target: HTMLMediaElement, state: PlaybackState) {
            if (state.time <= 0) return
            const resume = () => {
                target.removeEventListener('loadedmetadata', resume)
                if (gen !== activationGen) return
                target.currentTime = state.time
                if (state.wasPlaying) target.play().catch(() => {})
            }
            target.addEventListener('loadedmetadata', resume, {once: true})
        }

        // Safari native HLS
        if (el.canPlayType('application/vnd.apple.mpegurl')) {
            el.src = url
            hlsLoading.value = false
            scheduleResume(el, preSwitch)
            return
        }

        let Hls: typeof import('hls.js')['default']
        try {
            Hls = (await import('hls.js')).default
        } catch {
            if (gen !== activationGen) return
            hlsActivated.value = false
            hlsError.value = 'Failed to load HLS player'
            hlsLoading.value = false
            return
        }

        // Re-validate after async import — the component may have unmounted, or
        // the user may have switched media (activationGen bumped by cleanup/a newer
        // activation) during the import. Either way this activation is stale.
        if (!videoRef.value?.isConnected || gen !== activationGen) {
            hlsActivated.value = false
            return
        }

        if (!Hls.isSupported()) {
            hlsActivated.value = false
            hlsError.value = 'HLS not supported in this browser'
            hlsLoading.value = false
            scheduleResume(el, preSwitch)
            return
        }

        if (hlsInstance) {
            hlsInstance.destroy()
            hlsInstance = null
        }

        hlsLoading.value = true
        hlsError.value = null
        bandwidth.value = 0

        let networkRetryCount = 0
        let mediaRetryCount = 0
        // Set once the manifest has parsed or a fragment has actually loaded —
        // i.e. there is something in the buffer. A fatal NETWORK_ERROR before
        // that point has nothing to "continue playing from" (see the ERROR
        // handler below), unlike a genuine mid-stream reconnect.
        let hasBuffered = false

        // Buffer/retry tuning is shared with useHubProxyPlayback.ts's hls.js
        // instance (see utils/hlsConfig.ts) so the two players can't drift apart.
        const hls = new Hls({...HLS_TUNING_CONFIG})

        hlsInstance = hls

        hls.on(Hls.Events.MANIFEST_PARSED, (_event: unknown, data: {
            levels: Array<{ height: number; width: number; bitrate: number; videoCodec?: string; frameRate?: number }>
        }) => {
            hasBuffered = true
            const q: HLSQuality[] = data.levels.map((level, i) => ({
                index: i,
                height: level.height,
                width: level.width,
                bitrate: level.bitrate,
                name: getQualityName(level.height),
                codec: level.videoCodec || undefined,
                fps: Math.round(level.frameRate || 0) || undefined,
            }))
            qualities.value = q
            hlsLoading.value = false
            scheduleResume(el, preSwitch)

            // Restore saved quality preference. Per-device localStorage takes
            // precedence (an explicit in-player pick should stick on this
            // device); otherwise fall back to the user's account default_quality.
            //
            // A localStorage entry is only ever written by selectQuality() — i.e.
            // the user deliberately pinned a resolution in this player on this
            // device — so it stays a hard lock (currentLevel), which disables ABR
            // by design.
            const savedHeight = getSavedQualityPref()
            if (savedHeight > 0) {
                const match = q.find(level => level.height === savedHeight)
                if (match) {
                    hls.currentLevel = match.index
                    currentQuality.value = match.index
                    return
                }
            }
            // The account-level default_quality is a *preference*, not a hard
            // pin: treat it as an adaptive ceiling. Staying in auto mode
            // (currentLevel -1) with autoLevelCapping set lets the player drop to
            // a lower rendition when the connection can't sustain the preferred
            // height — preventing the stall-forever-instead-of-adapt behaviour —
            // and climb back up to the cap when bandwidth recovers.
            const prefHeight = parseQualityPref(opts?.defaultQuality?.())
            if (prefHeight > 0) {
                const match = pickQualityAtOrBelow(q, prefHeight)
                if (match) {
                    hls.autoLevelCapping = match.index
                    currentQuality.value = -1
                    return
                }
            }
            currentQuality.value = -1
        })

        hls.on(Hls.Events.LEVEL_SWITCHED, (_event: unknown, data: { level: number }) => {
            if (hls.currentLevel === -1) autoLevel.value = data.level
            else currentQuality.value = data.level
        })

        hls.on(Hls.Events.FRAG_LOADED, (_event: unknown, data: {
            frag: { stats: { loaded: number; loading: { start: number; end: number } } }
        }) => {
            hasBuffered = true
            const stats = data.frag.stats
            if (!stats.loaded || !stats.loading?.end || !stats.loading?.start) return
            const loadTime = stats.loading.end - stats.loading.start
            if (loadTime <= 0) return
            const bw = (stats.loaded * 8) / (loadTime / 1000)
            bandwidth.value = bw
            // Connectivity restored — reset counters so future outages get a full retry budget
            if (networkRetryCount > 0 || hlsReconnecting.value) {
                networkRetryCount = 0
                hlsReconnecting.value = false
            }
        })

        hls.on(Hls.Events.ERROR, (_event: unknown, data: import('hls.js').ErrorData) => {
            if (!data.fatal) return

            if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                networkRetryCount++
                // hasBuffered decides whether this is a mid-stream reconnect (ride
                // it out with the full backoff budget, MediaSource stays attached
                // so buffered content keeps playing) or a pre-buffer failure
                // (nothing to continue playing — fail fast, see
                // decideNetworkErrorAction).
                const decision = decideNetworkErrorAction(hasBuffered, networkRetryCount)
                if (decision.action === 'retry') {
                    // Only claim we're "continuing from buffer" when something
                    // actually buffered — see player.vue's reconnecting banner.
                    if (hasBuffered) hlsReconnecting.value = true
                    // Clear any pending retry before scheduling a new one. Rapid
                    // successive NETWORK_ERROR events would otherwise leave multiple
                    // live timers all calling hls.startLoad() on the same instance.
                    if (networkRetryTimer !== null) {
                        clearTimeout(networkRetryTimer)
                        networkRetryTimer = null
                    }
                    networkRetryTimer = setTimeout(() => {
                        networkRetryTimer = null
                        if (hlsInstance === hls) hls.startLoad()
                    }, decision.delayMs)
                    return
                }
                // Retries exhausted (or none granted for a pre-buffer failure) —
                // fall through to fatal handling below.
                hlsReconnecting.value = false
            }

            if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                mediaRetryCount++
                if (mediaRetryCount <= 2) {
                    if (mediaRetryCount === 2) hls.swapAudioCodec()
                    hls.recoverMediaError()
                    return
                }
            }

            hlsLoading.value = false
            hlsError.value = 'HLS playback failed'
            // Capture position/play-state before flipping hlsActivated so the
            // fallback to direct play (below) can resume in place instead of
            // silently restarting from 0 — same handoff used going the other
            // direction in activateHLS(). If nothing ever buffered, `el`'s own
            // currentTime is already 0 (Vue's :src=undefined swap reset it
            // before hls.js got a chance to attach anything) — restore the
            // pre-switch position captured back in activateHLS() instead. Once
            // something has buffered, prefer the live position so a mid-stream
            // failure resumes where HLS playback actually got to.
            const fallbackState = hasBuffered ? capturePlaybackState(el) : preSwitch
            // Reset hlsActivated: hls.js is being destroyed here, and the <video>
            // :src is `hlsActivated ? undefined : directStreamUrl`. Leaving it true
            // would strand the element with src=undefined (permanent blank playback
            // with no in-page recovery); flipping it false falls back to direct play.
            hlsActivated.value = false
            if (networkRetryTimer !== null) {
                clearTimeout(networkRetryTimer)
                networkRetryTimer = null
            }
            hls.destroy()
            hlsInstance = null
            scheduleResume(el, fallbackState)
        })

        // Re-validate before attaching — the component may have unmounted, or the
        // media may have switched, during event-listener setup. The gen check is
        // what prevents this stale activation from attaching the previous media's
        // stream to the (still-connected) element.
        if (!el.isConnected || gen !== activationGen) {
            hls.destroy()
            hlsInstance = null
            hlsActivated.value = false
            return
        }

        hls.loadSource(url)
        hls.attachMedia(el)
    }

    async function activateHLS() {
        // Capture URL immediately — cleanup() can null hlsUrl.value during the
        // async retry loop below (e.g. when the user navigates to another item).
        const capturedUrl = hlsUrl.value
        if (!capturedUrl) return
        // Snapshot the current position/play-state before flipping :src to
        // undefined (which resets the element's load state) so a direct-to-HLS
        // handoff can resume in place instead of silently rewinding to 0. A
        // fresh element that hasn't played yet naturally captures time=0, which
        // attachHLS treats as "nothing to restore" and leaves to player.vue's
        // own restorePosition() flow.
        const preSwitch = capturePlaybackState(videoRef.value)
        hlsActivated.value = true
        const thisGen = ++activationGen

        // Wait for Vue to patch the DOM (removes :src binding) before hls.js
        // takes control of the video element — prevents a race where Vue's
        // nextTick DOM update overwrites hls.js's MediaSource blob URL.
        // If videoRef is not yet mounted (media still loading), retry a few
        // times with increasing delays to handle the auto-activate race.
        for (let attempt = 0; attempt < 10; attempt++) {
            await nextTick()
            if (videoRef.value?.isConnected) break
            await new Promise(r => setTimeout(r, 100 * (attempt + 1)))
        }

        if (!videoRef.value?.isConnected) {
            hlsActivated.value = false
            hlsLoading.value = false
            return
        }

        if (thisGen !== activationGen) return

        attachHLS(capturedUrl, thisGen, preSwitch).catch((err: unknown) => {
            if (thisGen === activationGen) {
                hlsActivated.value = false
                // Clear the loading flag here too: attachHLS sets it true before its
                // own success/error paths clear it, but a throw before those (e.g. Hls
                // constructor) skips them, leaving the overlay/spinner stuck on for the
                // whole page visit until unmount.
                hlsLoading.value = false
                hlsError.value = 'HLS activation failed'
                console.error('[hls] activation error:', err)
            }
        })
    }

    // Extracted poll body to avoid exceeding 4 levels of function nesting (typescript:S2004)
    const consecutiveErrors = {count: 0}

    async function doPollCheck(id: string, thisCheck: number) {
        if (thisCheck !== checkGen) return
        if (document.hidden) return
        const elapsed = Date.now() - pollStartTime
        const phase = pollPhaseFor(elapsed)
        if (phase === 'expired') {
            jobRunning.value = false
            jobSlow.value = false
            hlsError.value = 'HLS generation timed out — try again later'
            if (pollTimer) {
                clearInterval(pollTimer)
                pollTimer = null
            }
            return
        }
        if (phase === 'slow' && pollPhase !== 'slow') {
            // A legitimately slow encode (large file, multiple qualities
            // transcoded sequentially, CPU-constrained server) can outlast the
            // fast-poll window. Don't give up — switch to an infrequent
            // background poll so a real completion or failure is still picked
            // up without requiring the user to navigate away and back.
            pollPhase = 'slow'
            jobSlow.value = true
            if (pollTimer) clearInterval(pollTimer)
            pollTimer = setInterval(() => doPollCheck(id, thisCheck), SLOW_POLL_INTERVAL_MS)
        }
        try {
            const updated = await hlsApi.check(id)
            if (thisCheck !== checkGen) return
            consecutiveErrors.count = 0
            jobProgress.value = updated.progress
            if (updated.available && updated.hls_url) {
                jobRunning.value = false
                jobSlow.value = false
                hlsError.value = null
                hlsAvailable.value = true
                hlsUrl.value = hlsApi.getMasterPlaylistUrl(id)
                if (pollTimer) {
                    clearInterval(pollTimer)
                    pollTimer = null
                }
                await autoActivateIfEnabled(thisCheck)
            } else if (updated.status !== 'running' && updated.status !== 'pending') {
                jobRunning.value = false
                jobSlow.value = false
                const failure = resolveTerminalHlsError(updated.status, updated.error)
                if (failure) hlsError.value = failure
                if (pollTimer) {
                    clearInterval(pollTimer)
                    pollTimer = null
                }
            }
        } catch {
            if (thisCheck !== checkGen) return
            consecutiveErrors.count++
            if (consecutiveErrors.count >= MAX_CONSECUTIVE_ERRORS) {
                jobRunning.value = false
                jobSlow.value = false
                hlsError.value = 'Lost connection to HLS service'
                if (pollTimer) {
                    clearInterval(pollTimer)
                    pollTimer = null
                }
            }
        }
    }

    /** Force an immediate poll tick when the tab becomes visible again while a
     * job is running — background tabs are throttled by the browser (Chrome
     * caps repeating timers to ~1/minute after ~5 minutes hidden), so without
     * this the progress banner can look frozen for a while after switching back. */
    function onVisibilityChange() {
        if (!document.hidden && pollTimer && activePollId) {
            void doPollCheck(activePollId, checkGen)
        }
    }

    // Activate HLS only when adaptive streaming is enabled in server settings
    // (best-effort — a failed settings fetch leaves the player on direct stream).
    async function autoActivateIfEnabled(thisCheck = checkGen) {
        const settings = await settingsApi.get().catch(() => null)
        if (thisCheck !== checkGen) return
        if (settings && settings.streaming?.adaptive !== false) await activateHLS()
    }

    // Runs the availability check for a media id: activates HLS if it's ready, or
    // starts the completion poll if a transcode job is in progress. Extracted so
    // recheck() can re-run it on demand (e.g. after manual generation) without
    // waiting for a mediaId change.
    async function runCheck(id: string) {
        const thisCheck = ++checkGen
        if (pollTimer) {
            clearInterval(pollTimer)
            pollTimer = null
        }
        try {
            const status = await hlsApi.check(id)
            if (thisCheck !== checkGen) return
            if (status.available && status.hls_url) {
                hlsAvailable.value = true
                hlsUrl.value = hlsApi.getMasterPlaylistUrl(id)
                await autoActivateIfEnabled(thisCheck)
            } else if (status.status === 'running' || status.status === 'pending') {
                jobRunning.value = true
                jobProgress.value = status.progress

                // Poll for completion — skip while tab is hidden to avoid wasteful background requests
                pollStartTime = Date.now()
                pollPhase = 'fast'
                jobSlow.value = false
                consecutiveErrors.count = 0
                activePollId = id
                if (pollTimer) clearInterval(pollTimer)
                pollTimer = setInterval(() => doPollCheck(id, thisCheck), FAST_POLL_INTERVAL_MS)
            } else {
                // A page load (or manual recheck) that lands directly on an
                // already-failed/canceled job — surface it instead of silently
                // falling through to the generic "Generate HLS" prompt.
                const failure = resolveTerminalHlsError(status.status, status.error)
                if (failure) hlsError.value = failure
            }
        } catch (err) {
            if (thisCheck !== checkGen) return
            // HLS not available or check failed — fall back to direct streaming
            console.warn('[hls] check failed:', err)
        }
    }

    // Re-run the availability check for the current media without waiting for a
    // mediaId change — used after the user manually requests HLS generation so the
    // progress banner + poll start immediately rather than only after a reload.
    function recheck() {
        const id = mediaId.value
        if (!id) return
        if (checkDebounce) {
            clearTimeout(checkDebounce)
            checkDebounce = null
        }
        runCheck(id)
    }

    // Check HLS availability when media ID changes (debounced to prevent burst requests)
    watch(mediaId, (id) => {
        if (checkDebounce) {
            clearTimeout(checkDebounce)
            checkDebounce = null
        }
        cleanup()
        hlsAvailable.value = false
        hlsUrl.value = null

        if (!id) return

        checkDebounce = setTimeout(() => {
            checkDebounce = null
            runCheck(id)
        }, 50)
    }, {immediate: true})

    // Force an immediate re-check when the tab becomes visible again instead of
    // waiting out the rest of a (possibly browser-throttled) background-tab poll
    // interval — see onVisibilityChange's doc comment above.
    document.addEventListener('visibilitychange', onVisibilityChange)

    // Cleanup on unmount
    onUnmounted(() => {
        document.removeEventListener('visibilitychange', onVisibilityChange)
        cleanup()
    })

    return {
        hlsAvailable,
        hlsActivated,
        recheck,
        hlsUrl,
        hlsLoading,
        hlsError,
        hlsReconnecting,
        qualities,
        currentQuality,
        autoLevel,
        bandwidth,
        selectQuality,
        activateHLS,
        jobProgress,
        jobRunning,
        jobSlow,
    }
}
