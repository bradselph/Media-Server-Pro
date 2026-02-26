import {useCallback, useEffect, useRef, useState} from 'react'
import {watchHistoryApi} from '@/api/endpoints'

interface ResumeInfo {
    position: number
    formattedTime: string
}

interface UseMediaPositionResult {
    resumeInfo: ResumeInfo | null
    acceptResume: () => void
    declineResume: () => void
    savePosition: (currentTime: number, duration: number) => void
}

function formatTime(seconds: number): string {
    const m = Math.floor(seconds / 60)
    const s = Math.floor(seconds % 60)
    return `${m}:${s.toString().padStart(2, '0')}`
}

export function useMediaPosition(
    mediaPath: string | null,
    mediaElement: HTMLMediaElement | null,
): UseMediaPositionResult {
    const [resumeInfo, setResumeInfo] = useState<ResumeInfo | null>(null)
    const trackingInterval = useRef<ReturnType<typeof setInterval> | null>(null)
    const autoDeclineTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

    // Load position on media change
    useEffect(() => {
        if (!mediaPath) {
            setResumeInfo(null)
            return
        }

        const path = mediaPath  // narrow string | null → string for closure
        let cancelled = false

        async function checkResume() {
            try {
                const entries = await watchHistoryApi.getEntry(path)
                if (cancelled) return

                const entry = Array.isArray(entries) && entries.length > 0 ? entries[0] : null
                if (!entry) return

                const position = entry.position
                const duration = entry.duration
                const lastWatched = new Date(entry.watched_at).getTime()
                const dayAgo = Date.now() - 86400 * 1000

                // Resume if: >10s in, <90% done, watched within 24h
                const progress = duration > 0 ? position / duration : 0
                if (position > 10 && progress < 0.9 && lastWatched > dayAgo) {
                    setResumeInfo({
                        position,
                        formattedTime: formatTime(position),
                    })

                    // IC-13: set timer unconditionally here; cleanup cancels it via clearTimeout.
                    // Redundant cancelled-flag check inside the callback removed — cleanup is the
                    // single authoritative cancellation point.
                    autoDeclineTimer.current = setTimeout(() => {
                        setResumeInfo(null)
                    }, 10000)
                }
            } catch {
                // Position data may not be available
            }
        }

        checkResume()
        return () => {
            cancelled = true
            if (autoDeclineTimer.current) clearTimeout(autoDeclineTimer.current)
        }
    }, [mediaPath])

    // Track position every 15s while playing
    useEffect(() => {
        if (!mediaPath || !mediaElement) return

        function trackPosition() {
            if (!mediaElement || mediaElement.paused || !mediaPath) return
            const currentTime = mediaElement.currentTime
            const duration = mediaElement.duration
            if (currentTime > 0 && duration > 0) {
                watchHistoryApi.trackPosition(mediaPath, currentTime, duration).catch(() => {
                })
            }
        }

        trackingInterval.current = setInterval(trackPosition, 15000)

        // Save on pause
        const handlePause = () => trackPosition()
        const handleEnded = () => {
            if (mediaPath && mediaElement) {
                watchHistoryApi.trackPosition(mediaPath, mediaElement.duration, mediaElement.duration).catch(() => {
                })
            }
        }

        mediaElement.addEventListener('pause', handlePause)
        mediaElement.addEventListener('ended', handleEnded)

        return () => {
            if (trackingInterval.current) clearInterval(trackingInterval.current)
            mediaElement.removeEventListener('pause', handlePause)
            mediaElement.removeEventListener('ended', handleEnded)
        }
    }, [mediaPath, mediaElement])

    const acceptResume = useCallback(() => {
        if (resumeInfo && mediaElement) {
            mediaElement.currentTime = resumeInfo.position
        }
        setResumeInfo(null)
        if (autoDeclineTimer.current) clearTimeout(autoDeclineTimer.current)
    }, [resumeInfo, mediaElement])

    const declineResume = useCallback(() => {
        setResumeInfo(null)
        if (autoDeclineTimer.current) clearTimeout(autoDeclineTimer.current)
    }, [])

    const savePosition = useCallback((currentTime: number, duration: number) => {
        if (mediaPath && currentTime > 0 && duration > 0) {
            watchHistoryApi.trackPosition(mediaPath, currentTime, duration).catch(() => {
            })
        }
    }, [mediaPath])

    // DEPRECATED: R-07 — savePosition is exported but has no consumer; PlayerPage position
    // tracking is handled entirely by the interval/pause/ended listeners above. Safe to remove
    // once ST-03 (consolidate PlayerPage position tracking) is addressed.
    return {resumeInfo, acceptResume, declineResume, savePosition}
}
