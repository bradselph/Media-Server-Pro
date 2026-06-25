/**
 * HLS composable for the player page.
 *
 * Checks HLS availability for a media item, attaches hls.js to a video element,
 * manages quality selection, and cleans up on unmount.
 *
 * Falls back to direct video src if HLS is not available or not supported.
 */

import type {Ref} from 'vue'

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

    let hlsInstance: import('hls.js').default | null = null
    let pollTimer: ReturnType<typeof setInterval> | null = null
    let checkDebounce: ReturnType<typeof setTimeout> | null = null
    let networkRetryTimer: ReturnType<typeof setTimeout> | null = null
    let activationGen = 0
    let pollStartTime = 0
    const MAX_POLL_DURATION = 30 * 60 * 1000 // 30 minutes
    const MAX_CONSECUTIVE_ERRORS = 10

    function cleanup() {
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

    async function attachHLS(url: string) {
        const el = videoRef.value
        if (!el) return

        // Safari native HLS
        if (el.canPlayType('application/vnd.apple.mpegurl')) {
            el.src = url
            hlsLoading.value = false
            return
        }

        let Hls: typeof import('hls.js')['default']
        try {
            Hls = (await import('hls.js')).default
        } catch {
            hlsError.value = 'Failed to load HLS player'
            hlsLoading.value = false
            return
        }

        // Re-validate after async import — component may have unmounted
        if (!videoRef.value?.isConnected) {
            hlsActivated.value = false
            return
        }

        if (!Hls.isSupported()) {
            hlsError.value = 'HLS not supported in this browser'
            hlsLoading.value = false
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

        const hls = new Hls({
            debug: false,
            enableWorker: true,
            lowLatencyMode: false,
            backBufferLength: 90,
            maxBufferLength: 60,
            maxMaxBufferLength: 120,
            maxBufferSize: 60 * 1000 * 1000,
            maxBufferHole: 0.5,
            manifestLoadingTimeOut: 20000,
            manifestLoadingMaxRetry: 4,
            manifestLoadingRetryDelay: 1000,
            levelLoadingTimeOut: 20000,
            levelLoadingMaxRetry: 4,
            levelLoadingRetryDelay: 1000,
            fragLoadingTimeOut: 30000,
            fragLoadingMaxRetry: 8,
            fragLoadingRetryDelay: 1000,
            fragLoadingMaxRetryTimeout: 16000,
            startFragPrefetch: true,
            testBandwidth: true,
        })

        hlsInstance = hls

        hls.on(Hls.Events.MANIFEST_PARSED, (_event: unknown, data: {
            levels: Array<{ height: number; width: number; bitrate: number; videoCodec?: string; frameRate?: number }>
        }) => {
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
                // Allow up to 8 reconnect attempts with exponential backoff capped at 30s.
                // We do NOT destroy the hls.js instance — the MediaSource stays attached so
                // the already-buffered content continues to play while we wait for the server.
                if (networkRetryCount <= 8) {
                    const delay = Math.min(1000 * Math.pow(1.5, networkRetryCount - 1), 30000)
                    hlsReconnecting.value = true
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
                    }, delay)
                    return
                }
                // All retries exhausted — fall through to fatal handling below
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
            if (networkRetryTimer !== null) {
                clearTimeout(networkRetryTimer)
                networkRetryTimer = null
            }
            hls.destroy()
            hlsInstance = null
        })

        // Re-validate before attaching — component may have unmounted during event listener setup
        if (!el.isConnected) {
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
            return
        }

        if (thisGen !== activationGen) return

        attachHLS(capturedUrl).catch((err: unknown) => {
            if (thisGen === activationGen) {
                hlsActivated.value = false
                hlsError.value = 'HLS activation failed'
                console.error('[hls] activation error:', err)
            }
        })
    }

    // Extracted poll body to avoid exceeding 4 levels of function nesting (typescript:S2004)
    const consecutiveErrors = {count: 0}

    async function doPollCheck(id: string) {
        if (document.hidden) return
        if (Date.now() - pollStartTime > MAX_POLL_DURATION) {
            jobRunning.value = false
            hlsError.value = 'HLS generation timed out — try again later'
            if (pollTimer) {
                clearInterval(pollTimer)
                pollTimer = null
            }
            return
        }
        try {
            const updated = await hlsApi.check(id)
            consecutiveErrors.count = 0
            jobProgress.value = updated.progress
            if (updated.available && updated.hls_url) {
                jobRunning.value = false
                hlsError.value = null
                hlsAvailable.value = true
                hlsUrl.value = hlsApi.getMasterPlaylistUrl(id)
                if (pollTimer) {
                    clearInterval(pollTimer)
                    pollTimer = null
                }
                const settings = await settingsApi.get().catch(() => null)
                if (settings && settings.streaming?.adaptive !== false) await activateHLS()
            } else if (updated.status !== 'running' && updated.status !== 'pending') {
                jobRunning.value = false
                if (pollTimer) {
                    clearInterval(pollTimer)
                    pollTimer = null
                }
            }
        } catch {
            consecutiveErrors.count++
            if (consecutiveErrors.count >= MAX_CONSECUTIVE_ERRORS) {
                jobRunning.value = false
                hlsError.value = 'Lost connection to HLS service'
                if (pollTimer) {
                    clearInterval(pollTimer)
                    pollTimer = null
                }
            }
        }
    }

    // Runs the availability check for a media id: activates HLS if it's ready, or
    // starts the completion poll if a transcode job is in progress. Extracted so
    // recheck() can re-run it on demand (e.g. after manual generation) without
    // waiting for a mediaId change.
    async function runCheck(id: string) {
        try {
            const status = await hlsApi.check(id)
            if (status.available && status.hls_url) {
                hlsAvailable.value = true
                hlsUrl.value = hlsApi.getMasterPlaylistUrl(id)
                // Auto-activate HLS only when adaptive streaming is enabled in server settings.
                // When disabled, the player falls back to direct streaming; user can still
                // click "Switch to HLS" if the banner is shown.
                const settings = await settingsApi.get().catch(() => null)
                if (settings && settings.streaming?.adaptive !== false) {
                    await activateHLS()
                }
            } else if (status.status === 'running' || status.status === 'pending') {
                jobRunning.value = true
                jobProgress.value = status.progress

                // Poll for completion — skip while tab is hidden to avoid wasteful background requests
                pollStartTime = Date.now()
                consecutiveErrors.count = 0
                if (pollTimer) clearInterval(pollTimer)
                pollTimer = setInterval(() => doPollCheck(id), 3000)
            }
        } catch (err) {
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

    // Cleanup on unmount
    onUnmounted(() => {
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
    }
}
