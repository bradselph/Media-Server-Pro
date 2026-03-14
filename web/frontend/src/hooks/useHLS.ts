import {type RefObject, useCallback, useEffect, useRef, useState} from 'react'
import type { ErrorData } from 'hls.js'

export interface HLSQuality {
    index: number
    height: number
    width: number
    bitrate: number
    name: string
    codec?: string
}

export interface UseHLSResult {
    qualities: HLSQuality[]
    currentQuality: number
    autoLevel: number
    selectQuality: (index: number) => void
    isLoading: boolean
    error: string | null
    bandwidth: number
}

const QUALITY_PREF_KEY = 'media-server-quality-pref'

function mapLevelsToQualities(levels: Array<{ height: number; width: number; bitrate: number; videoCodec?: string }>): HLSQuality[] {
    return levels.map((level, i) => ({
        index: i,
        height: level.height,
        width: level.width,
        bitrate: level.bitrate,
        name: getQualityName(level.height),
        codec: level.videoCodec || undefined,
    }))
}

function getQualityName(height: number): string {
    if (height >= 2160) return '4K'
    if (height >= 1440) return '1440p'
    if (height >= 1080) return '1080p'
    if (height >= 720) return '720p'
    if (height >= 480) return '480p'
    if (height >= 360) return '360p'
    return `${height}p`
}

function formatBitrate(bps: number): string {
    if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`
    if (bps >= 1_000) return `${Math.round(bps / 1_000)} kbps`
    return `${bps} bps`
}

/** Retrieve stored quality preference height (0 = auto). */
function getSavedQualityPref(): number {
    try {
        const val = localStorage.getItem(QUALITY_PREF_KEY)
        return val ? parseInt(val, 10) : 0
    } catch {
        return 0
    }
}

/** Persist quality preference as a height value (0 = auto). */
function saveQualityPref(height: number): void {
    try {
        localStorage.setItem(QUALITY_PREF_KEY, String(height))
    } catch {
        // localStorage may be full or disabled
    }
}

export {formatBitrate, getQualityName}

export function useHLS(
    mediaRef: RefObject<HTMLMediaElement | null>,
    hlsUrl: string | null,
    onFallback?: () => void,
): UseHLSResult {
    const hlsRef = useRef<import('hls.js').default | null>(null)
    const [qualities, setQualities] = useState<HLSQuality[]>([])
    const [currentQuality, setCurrentQuality] = useState(-1)
    const [autoLevel, setAutoLevel] = useState(-1)
    const [isLoading, setIsLoading] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const [bandwidth, setBandwidth] = useState(0)
    const networkRetryCount = useRef(0)
    const mediaRetryCount = useRef(0)

    const onFallbackRef = useRef(onFallback)
    onFallbackRef.current = onFallback

    const selectQuality = useCallback((index: number) => {
        if (hlsRef.current) {
            hlsRef.current.currentLevel = index
            setCurrentQuality(index)

            // Persist preference — store height for the selected quality, 0 for auto.
            if (index === -1) {
                saveQualityPref(0)
            } else {
                const level = hlsRef.current.levels[index]
                if (level) saveQualityPref(level.height)
            }
        }
    }, [])

    useEffect(() => {
        if (!hlsUrl || !mediaRef.current) {
            setQualities([])
            setCurrentQuality(-1)
            setAutoLevel(-1)
            setIsLoading(false)
            setError(null)
            setBandwidth(0)
            return
        }

        const el = mediaRef.current

        // Safari native HLS support
        if (el.canPlayType('application/vnd.apple.mpegurl')) {
            el.src = hlsUrl
            setIsLoading(false)
            return
        }

        let cancelled = false

        function onManifestParsed(_event: unknown, data: { levels: Array<{ height: number; width: number; bitrate: number; videoCodec?: string }> }) {
            if (cancelled) return
            const q = mapLevelsToQualities(data.levels)
            setQualities(q)
            setIsLoading(false)
            const savedHeight = getSavedQualityPref()
            if (savedHeight > 0 && hlsRef.current) {
                const match = q.find(level => level.height === savedHeight)
                if (match) {
                    hlsRef.current.currentLevel = match.index
                    setCurrentQuality(match.index)
                    return
                }
            }
            setCurrentQuality(-1)
        }

        function onLevelSwitched(_event: unknown, data: { level: number }) {
            if (cancelled || !hlsRef.current) return
            if (hlsRef.current.currentLevel === -1) setAutoLevel(data.level)
            else setCurrentQuality(data.level)
        }

        function onFragLoaded(_event: unknown, data: { frag: { stats: { loaded: number; loading: { start: number; end: number } } } }) {
            if (cancelled || !hlsRef.current) return
            const stats = data.frag.stats
            if (!stats.loaded || !stats.loading?.end || !stats.loading?.start) return
            const loadTime = stats.loading.end - stats.loading.start
            if (loadTime <= 0) return
            const bw = (stats.loaded * 8) / (loadTime / 1000)
            setBandwidth(bw)
            if (bw < 1_000_000) hlsRef.current.config.maxBufferLength = 30
            else if (bw < 3_000_000) hlsRef.current.config.maxBufferLength = 45
            else hlsRef.current.config.maxBufferLength = 60
        }

        function doStartLoad() {
            if (!cancelled && hlsRef.current) hlsRef.current.startLoad()
        }

        async function initHLS() {
            let Hls: typeof import('hls.js')['default']
            try {
                Hls = (await import('hls.js')).default
            } catch {
                if (!cancelled) {
                    setError('Failed to load HLS player')
                    onFallbackRef.current?.()
                }
                return
            }
            if (cancelled || !Hls.isSupported()) {
                if (!cancelled && !Hls.isSupported()) {
                    setError('HLS not supported in this browser')
                    onFallbackRef.current?.()
                }
                return
            }

            if (hlsRef.current) {
                hlsRef.current.destroy()
                hlsRef.current = null
            }

            setIsLoading(true)
            setError(null)
            setBandwidth(0)
            networkRetryCount.current = 0
            mediaRetryCount.current = 0

            const hls = new Hls({
                debug: false,
                enableWorker: true,
                lowLatencyMode: false,
                backBufferLength: 90,
                maxBufferLength: 60,
                maxMaxBufferLength: 120,
                maxBufferSize: 60 * 1000 * 1000,
                maxBufferHole: 0.5,
                highBufferWatchdogPeriod: 3,
                nudgeOffset: 0.1,
                nudgeMaxRetry: 5,
                maxFragLookUpTolerance: 0.25,
                liveSyncDurationCount: 3,
                liveMaxLatencyDurationCount: 10,
                manifestLoadingTimeOut: 20000,
                manifestLoadingMaxRetry: 4,
                manifestLoadingRetryDelay: 1000,
                levelLoadingTimeOut: 20000,
                levelLoadingMaxRetry: 4,
                levelLoadingRetryDelay: 1000,
                fragLoadingTimeOut: 30000,
                fragLoadingMaxRetry: 6,
                fragLoadingRetryDelay: 1000,
                startFragPrefetch: true,
                testBandwidth: true,
            })

            hlsRef.current = hls

            function onError(_event: unknown, data: ErrorData) {
                if (cancelled || !data.fatal) return
                if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                    networkRetryCount.current++
                    if (networkRetryCount.current <= 3) {
                        const delay = Math.min(1000 * Math.pow(2, networkRetryCount.current - 1), 8000)
                        setTimeout(doStartLoad, delay)
                        return
                    }
                }
                if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                    mediaRetryCount.current++
                    if (mediaRetryCount.current <= 2) {
                        if (mediaRetryCount.current === 2) hls.swapAudioCodec()
                        hls.recoverMediaError()
                        return
                    }
                }
                if (!cancelled) {
                    setError('HLS playback failed, falling back to direct streaming')
                    hls.destroy()
                    hlsRef.current = null
                    onFallbackRef.current?.()
                }
            }

            hls.on(Hls.Events.MANIFEST_PARSED, onManifestParsed)
            hls.on(Hls.Events.LEVEL_SWITCHED, onLevelSwitched)
            hls.on(Hls.Events.FRAG_LOADED, onFragLoaded)
            hls.on(Hls.Events.ERROR, onError)

            hls.loadSource(hlsUrl!)
            hls.attachMedia(el)
        }

        initHLS()

        return () => {
            cancelled = true
            if (hlsRef.current) {
                hlsRef.current.destroy()
                hlsRef.current = null
            }
            setQualities([])
            setCurrentQuality(-1)
            setAutoLevel(-1)
            setIsLoading(false)
            setBandwidth(0)
        }
    }, [hlsUrl, mediaRef])

    return {qualities, currentQuality, autoLevel, selectQuality, isLoading, error, bandwidth}
}
