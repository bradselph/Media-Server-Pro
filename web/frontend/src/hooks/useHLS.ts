import {type RefObject, useCallback, useEffect, useRef, useState} from 'react'

export interface HLSQuality {
    index: number
    height: number
    bitrate: number
    name: string
}

interface UseHLSResult {
    qualities: HLSQuality[]
    currentQuality: number
    autoLevel: number
    selectQuality: (index: number) => void
    isLoading: boolean
    error: string | null
    bandwidth: number
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

export function formatBitrate(bps: number): string {
    if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`
    if (bps >= 1_000) return `${(bps / 1_000).toFixed(0)} Kbps`
    return `${bps} bps`
}

export function useHLS(
    mediaRef: RefObject<HTMLMediaElement | null>,
    hlsUrl: string | null,
    onFallback?: () => void,
): UseHLSResult {
    const hlsRef = useRef<import('hls.js').default | null>(null)
    const [qualities, setQualities] = useState<HLSQuality[]>([])
    const [currentQuality, setCurrentQuality] = useState(-1)
    const [autoLevel, setAutoLevel] = useState(-1)
    const [bandwidth, setBandwidth] = useState(0)
    const [isLoading, setIsLoading] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const networkRetryCount = useRef(0)
    const mediaRetryCount = useRef(0)

    // Stable ref for onFallback so the effect doesn't re-run when the
    // consumer passes a new function reference on each render.
    const onFallbackRef = useRef(onFallback)
    onFallbackRef.current = onFallback

    const selectQuality = useCallback((index: number) => {
        if (hlsRef.current) {
            hlsRef.current.currentLevel = index
            setCurrentQuality(index)
        }
    }, [])

    useEffect(() => {
        if (!hlsUrl || !mediaRef.current) {
            setQualities([])
            setCurrentQuality(-1)
            setAutoLevel(-1)
            setBandwidth(0)
            setIsLoading(false)
            setError(null)
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

            // Destroy previous instance
            if (hlsRef.current) {
                hlsRef.current.destroy()
                hlsRef.current = null
            }

            setIsLoading(true)
            setError(null)
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

            hls.on(Hls.Events.MANIFEST_PARSED, (_event, data) => {
                if (cancelled) return
                const q: HLSQuality[] = data.levels.map((level, i) => ({
                    index: i,
                    height: level.height,
                    bitrate: level.bitrate,
                    name: getQualityName(level.height),
                }))
                setQualities(q)
                setCurrentQuality(-1) // auto
                setAutoLevel(-1)
                setIsLoading(false)
            })

            hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
                if (!cancelled) setAutoLevel(data.level)
            })

            hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
                if (cancelled) return
                const stats = data.frag.stats
                if (stats.loaded && stats.loading.end && stats.loading.start) {
                    const loadTime = stats.loading.end - stats.loading.start
                    if (loadTime > 0) {
                        const bw = (stats.loaded * 8) / (loadTime / 1000)
                        setBandwidth(bw)
                        if (bw < 1_000_000) {
                            hls.config.maxBufferLength = 30
                        } else if (bw < 3_000_000) {
                            hls.config.maxBufferLength = 45
                        } else {
                            hls.config.maxBufferLength = 60
                        }
                    }
                }
            })

            hls.on(Hls.Events.ERROR, (_event, data) => {
                if (cancelled) return
                if (!data.fatal) return

                if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                    networkRetryCount.current++
                    if (networkRetryCount.current <= 3) {
                        const delay = Math.min(1000 * Math.pow(2, networkRetryCount.current - 1), 8000)
                        setTimeout(() => {
                            if (!cancelled && hlsRef.current) {
                                hlsRef.current.startLoad()
                            }
                        }, delay)
                        return
                    }
                }

                if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                    mediaRetryCount.current++
                    if (mediaRetryCount.current <= 2) {
                        // Per hls.js docs: swap audio codec before second recovery
                        // attempt to handle AAC/MP3 codec mismatch issues.
                        if (mediaRetryCount.current === 2) {
                            hls.swapAudioCodec()
                        }
                        hls.recoverMediaError()
                        return
                    }
                }

                // All retries exhausted — fallback
                if (!cancelled) {
                    setError('HLS playback failed, falling back to direct streaming')
                    hls.destroy()
                    hlsRef.current = null
                    onFallbackRef.current?.()
                }
            })

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
            setBandwidth(0)
            setIsLoading(false)
        }
    }, [hlsUrl, mediaRef])

    return {qualities, currentQuality, autoLevel, selectQuality, isLoading, error, bandwidth}
}
