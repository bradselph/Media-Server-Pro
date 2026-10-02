/**
 * Recovery for the player's direct (progressive `<video src>`) stream.
 *
 * hls.js retries failed playlist/fragment loads on its own (see useHLS.ts); a
 * plain media element does not. A connection that drops mid-stream (Wi-Fi
 * blip, mobile network handover, the server restarting during a deploy) ends
 * in a MEDIA_ERR_NETWORK error and playback stops until the page is reloaded;
 * a connection that silently hangs leaves the element "waiting" forever. And
 * a source the browser could not even start because the server answered with
 * an error (503 while the library loads after a restart, 429 stream cap, 401
 * expired session) is reported as MEDIA_ERR_SRC_NOT_SUPPORTED — the same code
 * as a genuinely unplayable format.
 *
 * This composable reloads the source and resumes at the last position with
 * exponential backoff, and probes the stream URL (a 1-byte Range GET) first to
 * tell a transient server condition from a terminal one.
 */

import type {Ref} from 'vue'

/** Reload attempts per failure episode before giving up. */
export const DIRECT_MAX_RETRIES = 6
/** A stall with no download progress for this long is treated as a hung connection. */
export const DIRECT_STALL_TIMEOUT_MS = 20_000
/** Playback this far past the failure point ends the episode and restores the full retry budget. */
export const DIRECT_EPISODE_RESET_S = 20
const MAX_RETRY_DELAY_MS = 60_000
const PROBE_TIMEOUT_MS = 10_000
const STALL_CHECK_INTERVAL_MS = 2_500
const RETRY_DELAYS_MS = [1_000, 2_000, 4_000, 8_000, 15_000, 30_000]
// HTMLMediaElement.readyState values (the constants live on the prototype,
// which happy-dom doesn't provide).
const HAVE_METADATA = 1
const HAVE_FUTURE_DATA = 3

export type ProbeOutcome =
    | { kind: 'ok' } // 2xx: the server is serving the file
    | { kind: 'transient'; retryAfterMs?: number } // 5xx / 429 / 408, or no response at all
    | { kind: 'terminal'; message: string } // retrying won't help (401, 403, 404, ...)

export type RecoveryTrigger = 'network' | 'decode' | 'unsupported' | 'stall'

/** Parse a Retry-After header (delta-seconds or HTTP-date) into a capped delay. */
export function parseRetryAfterMs(value: string | null | undefined, now = Date.now()): number | undefined {
    if (!value) return undefined
    const trimmed = value.trim()
    if (/^\d+$/.test(trimmed)) return Math.min(Number(trimmed) * 1000, MAX_RETRY_DELAY_MS)
    const at = Date.parse(trimmed)
    if (Number.isNaN(at)) return undefined
    return Math.min(Math.max(at - now, 0), MAX_RETRY_DELAY_MS)
}

/** Classify the stream URL's response to a 1-byte probe. */
export function classifyProbe(status: number, retryAfter?: string | null, serverMessage?: string): ProbeOutcome {
    if (status >= 200 && status < 300) return {kind: 'ok'}
    if (status === 408 || status === 425 || status === 429 || status >= 500) {
        return {kind: 'transient', retryAfterMs: parseRetryAfterMs(retryAfter)}
    }
    if (status === 401) return {kind: 'terminal', message: serverMessage || 'Sign in to watch this'}
    if (status === 403) return {kind: 'terminal', message: serverMessage || 'You don\'t have access to this media'}
    if (status === 404 || status === 410) return {kind: 'terminal', message: 'This media is no longer available'}
    return {kind: 'terminal', message: serverMessage || `Playback failed (HTTP ${status})`}
}

/** Delay before reload attempt `attempt` (1-based), honoring a server Retry-After. */
export function directRetryDelayMs(attempt: number, retryAfterMs?: number): number {
    const base = RETRY_DELAYS_MS[Math.min(Math.max(attempt, 1), RETRY_DELAYS_MS.length) - 1]!
    return Math.min(Math.max(base, retryAfterMs ?? 0), MAX_RETRY_DELAY_MS)
}

/** Map a MediaError code to a recovery trigger; null = deliberately aborted, not ours to recover. */
export function triggerForMediaError(code: number | undefined): RecoveryTrigger | null {
    switch (code) {
        case 1: // MEDIA_ERR_ABORTED — the fetch was aborted on purpose
            return null
        case 3: // MEDIA_ERR_DECODE
            return 'decode'
        case 4: // MEDIA_ERR_SRC_NOT_SUPPORTED (also any non-2xx answer to the first request)
            return 'unsupported'
        default: // MEDIA_ERR_NETWORK, or a code this browser invented
            return 'network'
    }
}

/** GET one byte of the stream to learn how the server is answering right now. */
export async function probeStream(url: string): Promise<ProbeOutcome> {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), PROBE_TIMEOUT_MS)
    try {
        const res = await fetch(url, {
            headers: {Range: 'bytes=0-0'},
            credentials: 'same-origin',
            cache: 'no-store',
            signal: ctrl.signal,
        })
        let message: string | undefined
        if (res.ok) {
            res.body?.cancel().catch(() => {})
        } else {
            try {
                const body = await res.json() as { error?: unknown }
                if (typeof body?.error === 'string') message = body.error
            } catch { /* not a JSON error body */ }
        }
        return classifyProbe(res.status, res.headers.get('Retry-After'), message)
    } catch {
        // No response at all: offline, DNS, server down, or the probe timed out.
        return {kind: 'transient'}
    } finally {
        clearTimeout(timer)
    }
}

interface ResumePoint {
    time: number
    wasPlaying: boolean
}

export interface DirectPlayRecoveryOptions {
    /** The player's media element (the `<video>` and `<audio>` branches share it). */
    mediaRef: Ref<HTMLMediaElement | null>
    /** The direct stream URL while direct play is the active source; null otherwise (HLS attached, hub embed, nothing loaded). */
    directUrl: () => string | null
    /**
     * The server serves the file but this browser can't play it (unsupported
     * format, or a decode error). Return true if an alternative source — HLS —
     * took over; false leaves the failure to be reported.
     */
    onUnplayable?: () => boolean
    /** Test seam for the URL probe. */
    probe?: (url: string) => Promise<ProbeOutcome>
}

export function useDirectPlayRecovery(opts: DirectPlayRecoveryOptions) {
    const probe = opts.probe ?? probeStream
    /** A recovery is scheduled or in flight. */
    const reconnecting = ref(false)
    /** Terminal failure message; null while playing or recovering. */
    const failure = ref<string | null>(null)

    let attempts = 0
    let busy = false
    let gen = 0
    let resume: ResumePoint | null = null
    let intentPlaying = false
    let loadedSinceReset = false
    let unsupportedRetried = false
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let watchdog: ReturnType<typeof setInterval> | null = null
    let stallSince = 0
    let lastProgressAt = 0
    let onlineWaiter: (() => void) | null = null
    let bound: HTMLMediaElement | null = null

    function clearTimers() {
        if (retryTimer) {
            clearTimeout(retryTimer)
            retryTimer = null
        }
        stopWatchdog()
        if (onlineWaiter) {
            globalThis.removeEventListener('online', onlineWaiter)
            onlineWaiter = null
        }
    }

    function stopWatchdog() {
        if (watchdog) {
            clearInterval(watchdog)
            watchdog = null
        }
        stallSince = 0
    }

    /** Forget all recovery state — call when the source changes. */
    function reset() {
        gen++
        clearTimers()
        attempts = 0
        busy = false
        resume = null
        loadedSinceReset = false
        unsupportedRetried = false
        reconnecting.value = false
        failure.value = null
    }

    function giveUp(message: string) {
        clearTimers()
        busy = false
        reconnecting.value = false
        failure.value = message
    }

    function waitForOnline(myGen: number): Promise<void> {
        return new Promise(resolve => {
            onlineWaiter = () => {
                if (onlineWaiter) globalThis.removeEventListener('online', onlineWaiter)
                onlineWaiter = null
                resolve()
            }
            globalThis.addEventListener('online', onlineWaiter)
            // reset() removes the listener without resolving; settle anyway so
            // the awaiting recover() can see its generation is stale and exit.
            const check = setInterval(() => {
                if (myGen !== gen) {
                    clearInterval(check)
                    resolve()
                } else if (!onlineWaiter) clearInterval(check)
            }, 1_000)
        })
    }

    function reload(el: HTMLMediaElement, point: ResumePoint, myGen: number) {
        if (myGen !== gen || opts.mediaRef.value !== el || !opts.directUrl()) return
        const onMeta = () => {
            if (myGen !== gen) return
            if (point.time > 0) {
                const dur = el.duration
                el.currentTime = Number.isFinite(dur) && dur > 1 ? Math.min(point.time, dur - 1) : point.time
            }
            if (point.wasPlaying) el.play().catch(() => {})
        }
        el.addEventListener('loadedmetadata', onMeta, {once: true})
        // A failure of this attempt must be able to schedule the next one.
        busy = false
        el.load()
    }

    async function recover(trigger: RecoveryTrigger): Promise<void> {
        const el = opts.mediaRef.value
        const url = opts.directUrl()
        if (!el || !url || busy) return
        busy = true
        const myGen = gen
        stopWatchdog()

        // Resume where playback actually was. Right after a reload the element
        // has no metadata yet and reports 0, so keep the earlier point then.
        if (el.readyState >= HAVE_METADATA && el.currentTime > 0) {
            resume = {time: el.currentTime, wasPlaying: resume?.wasPlaying ?? intentPlaying}
        } else if (!resume) {
            resume = {time: 0, wasPlaying: intentPlaying}
        }
        if (trigger === 'decode' && resume.time > 0) resume.time += 0.5 // step past the bad frame
        reconnecting.value = true
        failure.value = null

        if (typeof navigator !== 'undefined' && navigator.onLine === false) {
            // Offline: nothing will load — wait for the network without
            // spending attempts.
            await waitForOnline(myGen)
            if (myGen !== gen) return
        }

        const outcome = await probe(url)
        if (myGen !== gen) return

        if (outcome.kind === 'terminal') {
            giveUp(outcome.message)
            return
        }
        if (outcome.kind === 'ok' && (trigger === 'decode' || (trigger === 'unsupported' && !loadedSinceReset))) {
            // The server is serving the file fine. A first "unsupported" may
            // still have been a transient error that cleared before the probe
            // (e.g. 503 while the library loaded), so reload once more before
            // blaming the format.
            if (trigger === 'unsupported' && !unsupportedRetried) {
                unsupportedRetried = true
                reload(el, resume, myGen)
                return
            }
            if (opts.onUnplayable?.()) {
                // HLS took over; it brings its own retry handling.
                busy = false
                reconnecting.value = false
                return
            }
            if (trigger === 'unsupported') {
                giveUp('This file\'s format isn\'t supported by your browser')
                return
            }
        }

        attempts++
        if (attempts > DIRECT_MAX_RETRIES) {
            giveUp(trigger === 'stall'
                ? 'The stream stopped responding'
                : 'Lost connection to the server')
            return
        }
        // The server answered fine, so retry the first time right away; back
        // off when it is erroring or unreachable, honoring its Retry-After.
        const delay = outcome.kind === 'ok' && attempts === 1
            ? 0
            : directRetryDelayMs(attempts, outcome.kind === 'transient' ? outcome.retryAfterMs : undefined)
        const point = resume
        retryTimer = setTimeout(() => {
            retryTimer = null
            reload(el, point, myGen)
        }, delay)
    }

    /**
     * Handle the media element's `error` event. Returns true when recovery
     * took the error over (caller should not report it), false when the error
     * isn't a direct-play failure this composable handles.
     */
    function handleError(): boolean {
        const el = opts.mediaRef.value
        if (!el || !opts.directUrl()) return false
        const trigger = triggerForMediaError(el.error?.code)
        if (!trigger) return busy || reconnecting.value
        void recover(trigger)
        return true
    }

    /** Retry immediately with a fresh budget (e.g. from a "Retry" button). */
    function retryNow() {
        const point = resume
        reset()
        resume = point
        void recover('network')
    }

    function onStall() {
        const el = opts.mediaRef.value
        if (!el || !opts.directUrl() || busy || el.paused || el.ended) return
        if (!stallSince) stallSince = Date.now()
        if (!watchdog) watchdog = setInterval(checkStall, STALL_CHECK_INTERVAL_MS)
    }

    function checkStall() {
        const el = opts.mediaRef.value
        if (!el || !opts.directUrl() || busy || el.paused || el.ended || el.readyState >= HAVE_FUTURE_DATA) {
            stopWatchdog()
            return
        }
        const now = Date.now()
        // Only a stall with no download progress is a hung connection; a slow
        // connection that is still delivering bytes is left to the browser.
        if (now - stallSince >= DIRECT_STALL_TIMEOUT_MS && now - lastProgressAt >= DIRECT_STALL_TIMEOUT_MS) {
            void recover('stall')
        }
    }

    function onProgress() {
        lastProgressAt = Date.now()
    }

    function onLoadedMetadata() {
        loadedSinceReset = true
    }

    function onPlaying() {
        stopWatchdog()
        if (reconnecting.value && !busy && !retryTimer) reconnecting.value = false
        if (failure.value) failure.value = null
    }

    function onTimeUpdate() {
        const el = opts.mediaRef.value
        if (!el || !resume || attempts === 0 || busy) return
        if (el.currentTime >= resume.time + DIRECT_EPISODE_RESET_S) {
            // Played well past the failure point: the episode is over.
            attempts = 0
            resume = null
            unsupportedRetried = false
        }
    }

    function onPlay() {
        intentPlaying = true
    }

    function onPause() {
        const el = opts.mediaRef.value
        // A pause the browser issues around an error or a reload isn't the
        // viewer's choice.
        if (busy || reconnecting.value || el?.error) return
        intentPlaying = false
    }

    const listeners: Array<[string, () => void]> = [
        ['waiting', onStall],
        ['stalled', onStall],
        ['progress', onProgress],
        ['loadstart', onProgress],
        ['loadedmetadata', onLoadedMetadata],
        ['playing', onPlaying],
        ['timeupdate', onTimeUpdate],
        ['play', onPlay],
        ['pause', onPause],
    ]

    function bind(el: HTMLMediaElement | null) {
        if (bound === el) return
        if (bound) for (const [name, fn] of listeners) bound.removeEventListener(name, fn)
        bound = el
        if (el) for (const [name, fn] of listeners) el.addEventListener(name, fn)
    }

    watch(opts.mediaRef, el => bind(el), {immediate: true})
    // A new item, or a switch between direct play and HLS, starts fresh.
    watch(() => opts.directUrl(), () => reset())

    onUnmounted(() => {
        reset()
        bind(null)
    })

    return {reconnecting, failure, handleError, retryNow, reset}
}
