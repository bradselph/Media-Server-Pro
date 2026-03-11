import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/authStore'
import { usePlaylistStore } from '@/stores/playlistStore'
import { useToast } from '@/hooks/useToast'
import { useHLS } from '@/hooks/useHLS'
import { useSettingsStore } from '@/stores/settingsStore'
import { useEqualizer } from '@/hooks/useEqualizer'
import { ApiError } from '@/api/client'
import {
    analyticsApi,
    hlsApi,
    mediaApi,
    ratingsApi,
    suggestionsApi,
    watchHistoryApi,
} from '@/api/endpoints'
import type { HLSJob } from '@/api/types'
import { usePlayerKeyboard } from './playerKeyboard'

const QUALITY_HEIGHT_MAP: Record<string, number> = {
    low: 360,
    medium: 480,
    high: 720,
    ultra: 1080,
}

function syncQualityPreference(q: string | undefined): void {
    if (!q || q === 'auto') return
    const height = QUALITY_HEIGHT_MAP[q]
    if (!height) return
    try {
        localStorage.setItem('media-server-quality-pref', String(height))
    } catch {
        /* storage full */
    }
}

function syncPlaylistIndex(
    mediaId: string,
    playlist: { media_id: string }[],
    currentIndex: number,
    setCurrentIndex: (i: number) => void,
): void {
    if (!mediaId || playlist.length === 0) return
    const idx = playlist.findIndex((i) => i.media_id === mediaId)
    if (idx === -1 || idx === currentIndex) return
    setCurrentIndex(idx)
}

function shouldResumeAtPosition(
    el: HTMLMediaElement,
    pos: number,
): boolean {
    if (pos <= 5 || el.readyState < 1 || el.duration <= 0) return false
    if (pos >= el.duration - 5 || el.currentTime >= 2) return false
    return true
}

/** Syncs user quality/preference and playlist index. Reduces main hook complexity. */
function usePlayerSyncEffects(
    user: { preferences?: { default_quality?: string } } | null,
    mediaId: string,
    currentPlaylist: { media_id: string }[],
    currentIndex: number,
    setCurrentIndex: (i: number) => void,
) {
    useEffect(() => {
        syncQualityPreference(user?.preferences?.default_quality)
    }, [user?.preferences?.default_quality])

    useEffect(() => {
        syncPlaylistIndex(mediaId, currentPlaylist, currentIndex, setCurrentIndex)
    }, [mediaId, currentPlaylist, currentIndex, setCurrentIndex])
}

export function usePlayerPageState(mediaId: string) {
    const navigate = useNavigate()
    const permissions = useAuthStore((s) => s.permissions)
    const user = useAuthStore((s) => s.user)
    const canViewMature = permissions.can_view_mature && (user?.preferences?.show_mature === true)
    const { showToast } = useToast()
    const { currentPlaylist, currentIndex, setCurrentIndex, playNext, playPrevious } =
        usePlaylistStore()

    const videoRef = useRef<HTMLVideoElement>(null)
    const audioRef = useRef<HTMLAudioElement>(null)
    const resumePositionRef = useRef(0)
    const currentTimeRef = useRef(0)
    const durationRef = useRef(0)
    const isPlayingRef = useRef(false)
    const [audioReady, setAudioReady] = useState(false)

    const [isPlaying, setIsPlaying] = useState(false)
    const [currentTime, setCurrentTime] = useState(0)
    const [duration, setDuration] = useState(0)
    const [buffered, setBuffered] = useState(0)
    const [volume, setVolume] = useState(1)
    const [isMuted, setIsMuted] = useState(false)
    const [isLooping, setIsLooping] = useState(false)
    const defaultPlaybackSpeed = user?.preferences?.playback_speed ?? 1
    const [playbackRate, setPlaybackRate] = useState(defaultPlaybackSpeed)
    const [showControls, setShowControls] = useState(true)
    const [showSettings, setShowSettings] = useState(false)
    const [isLoading, setIsLoading] = useState(true)
    const [matureAccepted, setMatureAccepted] = useState(false)
    const [theaterMode, setTheaterMode] = useState(false)
    const [hoverTime, setHoverTime] = useState<number | null>(null)
    const [hoverPos, setHoverPos] = useState(0)
    const [hlsJob, setHlsJob] = useState<HLSJob | null>(null)
    const [hlsPolling, setHlsPolling] = useState(false)
    const [activeHlsUrl, setActiveHlsUrl] = useState<string | null>(null)
    const [hlsAvailable, setHlsAvailable] = useState(false)
    const [hlsReadyUrl, setHlsReadyUrl] = useState<string | null>(null)
    const hlsEnabled = useSettingsStore((s) => s.serverSettings?.features?.enableHLS ?? true)
    const [userRating, setUserRating] = useState(0)
    const [ratingHover, setRatingHover] = useState(0)

    usePlayerSyncEffects(user, mediaId, currentPlaylist, currentIndex, setCurrentIndex)

    const handleRate = useCallback(
        (rating: number) => {
            setUserRating(rating)
            if (mediaId) ratingsApi.record(mediaId, rating).catch(() => {})
        },
        [mediaId],
    )

    const controlsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    const lastClickTimeRef = useRef(0)

    const {
        data: media,
        isLoading: mediaLoading,
        error: mediaError,
    } = useQuery({
        queryKey: ['media-item', mediaId],
        queryFn: () => mediaApi.get(mediaId),
        enabled: !!mediaId,
        retry: (failureCount, error) => {
            if (error instanceof ApiError && error.status === 503) return failureCount < 5
            return failureCount < 1
        },
        retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 10000),
    })

    const matureAccessGranted =
        matureAccepted || (user?.preferences?.show_mature === true)
    const showMatureWarning = !!(media?.is_mature && !matureAccessGranted)

    const onHlsFallback = useCallback(() => setActiveHlsUrl(null), [])

    const {
        qualities: hlsQualities,
        currentQuality,
        autoLevel,
        selectQuality,
        isLoading: hlsIsLoading,
        error: hlsError,
        bandwidth,
    } = useHLS(
        videoRef,
        media?.type === 'video' && hlsEnabled ? activeHlsUrl : null,
        onHlsFallback,
    )

    useEffect(() => {
        if (hlsError) showToast(hlsError, 'error')
    }, [hlsError, showToast])

    useEffect(() => {
        if (audioRef.current) queueMicrotask(() => setAudioReady(true))
    }, [])

    useEqualizer(audioRef, audioReady && media?.type === 'audio')

    const {
        data: similarData = [],
        isLoading: relatedLoading,
        isError: similarError,
        refetch: similarRefetch,
    } = useQuery({
        queryKey: ['media-similar', mediaId, canViewMature],
        queryFn: () => suggestionsApi.getSimilar(mediaId ?? ''),
        enabled: !!mediaId,
        retry: (failureCount, error) => {
            if (error instanceof ApiError && error.status === 503) return failureCount < 5
            return failureCount < 1
        },
        retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 10000),
        select: (data) => (data ?? []).slice(0, 8),
    })

    const { data: trendingData = [], isLoading: trendingLoading } = useQuery({
        queryKey: ['suggestions-trending', canViewMature],
        queryFn: () => suggestionsApi.getTrending(),
        enabled:
            !!mediaId && !relatedLoading && !similarError && similarData.length === 0,
        staleTime: 60 * 1000,
        select: (data) => (data ?? []).slice(0, 8),
    })

    const useFallback = similarData.length === 0 && !similarError && !relatedLoading
    const related = similarData.length > 0 ? similarData : trendingData
    const relatedLabel = similarData.length > 0 ? 'Similar Media' : 'More to Explore'
    const relatedStillLoading = relatedLoading || (useFallback && trendingLoading)

    useEffect(() => {
        if (!mediaId || !media || (media.is_mature && !matureAccessGranted)) return
        const el = media.type === 'video' ? videoRef.current : audioRef.current
        if (!el) return

        queueMicrotask(() => {
            setIsLoading(true)
            setCurrentTime(0)
            setDuration(0)
            setIsPlaying(false)
            setActiveHlsUrl(null)
            setHlsAvailable(false)
            setHlsReadyUrl(null)
            setHlsJob(null)
            setHlsPolling(false)
            setUserRating(0)
        })

        el.src = mediaApi.getStreamUrl(mediaId)
        el.volume = volume
        el.loop = isLooping
        el.playbackRate = playbackRate

        resumePositionRef.current = 0
        let positionFetchCancelled = false
        const resumeEnabled = user?.preferences?.resume_playback !== false
        if (user && resumeEnabled) {
            const elRef = el
            watchHistoryApi
                .getPosition(mediaId)
                .then((data) => {
                    if (positionFetchCancelled) return
                    const pos = data?.position ?? 0
                    resumePositionRef.current = pos
                    if (shouldResumeAtPosition(elRef, pos)) {
                        elRef.currentTime = pos
                    }
                })
                .catch(() => {})
        }
        analyticsApi.trackEvent({ type: 'view', media_id: media.id }).catch(() => {})

        return () => {
            positionFetchCancelled = true
        }
    }, [mediaId, media, matureAccessGranted]) // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        if (!mediaId || media?.type !== 'video' || !hlsEnabled) return
        hlsApi
            .check(mediaId)
            .then((hls) => {
                if (hls.available && hls.hls_url) {
                    setHlsAvailable(true)
                    setHlsReadyUrl(hls.hls_url)
                } else if (hls.job_id && hls.status === 'running') {
                    setHlsJob({
                        id: hls.job_id,
                        status: 'running',
                        progress: hls.progress ?? 0,
                        qualities: hls.qualities ?? [],
                        started_at: hls.started_at ?? '',
                        error: hls.error ?? '',
                        available: false,
                    })
                    setHlsPolling(true)
                }
            })
            .catch(() => {})
    }, [mediaId, media, hlsEnabled])

    useEffect(() => {
        if (!hlsPolling || !hlsJob) return
        const interval = setInterval(async () => {
            try {
                const updated = await hlsApi.getStatus(hlsJob.id)
                setHlsJob(updated)
                if (updated.status === 'completed') {
                    setHlsPolling(false)
                    setHlsAvailable(true)
                    setHlsReadyUrl(hlsApi.getMasterPlaylistUrl(updated.id))
                } else if (updated.status === 'failed') {
                    setHlsPolling(false)
                }
            } catch {
                setHlsPolling(false)
            }
        }, 3000)
        return () => clearInterval(interval)
    }, [hlsPolling, hlsJob])

    useEffect(() => {
        if (activeHlsUrl !== null || !mediaId || !media || (media.is_mature && !matureAccessGranted))
            return
        const el = media.type === 'video' ? videoRef.current : audioRef.current
        if (!el || (el.src && el.src !== '' && el.src !== window.location.href)) return
        el.src = mediaApi.getStreamUrl(mediaId)
    }, [activeHlsUrl, mediaId, media, matureAccessGranted])

    const isVideo = media?.type === 'video'

    const resetControlsTimer = useCallback(() => {
        if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        setShowControls(true)
        controlsTimerRef.current = setTimeout(() => {
            if (isPlayingRef.current) setShowControls(false)
        }, 3000)
    }, [])

    useEffect(
        () => () => {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        },
        [],
    )

    useEffect(() => {
        if (!isVideo) return
        isPlayingRef.current = isPlaying
        if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        if (isPlaying) {
            controlsTimerRef.current = setTimeout(() => setShowControls(false), 3000)
        } else {
            queueMicrotask(() => setShowControls(true))
        }
    }, [isPlaying, isVideo])

    useEffect(() => {
        if (!mediaId) return
        const handleVisibilityChange = () => {
            if (
                document.visibilityState === 'hidden' &&
                currentTimeRef.current > 0 &&
                durationRef.current > 0
            ) {
                watchHistoryApi
                    .trackPosition(mediaId, currentTimeRef.current, durationRef.current)
                    .catch(() => {})
            }
        }
        document.addEventListener('visibilitychange', handleVisibilityChange)
        return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
    }, [mediaId])

    const getActiveEl = useCallback(() => {
        if (!media) return null
        return media.type === 'video' ? videoRef.current : audioRef.current
    }, [media])

    useEffect(() => {
        const pref = user?.preferences?.playback_speed
        if (pref === null || pref === undefined || pref < 0.25 || pref > 2) return
        queueMicrotask(() => setPlaybackRate(pref))
        const el = getActiveEl()
        if (el) el.playbackRate = pref
    }, [user?.preferences?.playback_speed, media?.type, getActiveEl])

    const togglePlay = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        if (el.paused) el.play().catch(() => {})
        else el.pause()
    }, [getActiveEl])

    const handlePrevTrack = useCallback(() => {
        const prevId = playPrevious()
        if (prevId) navigate(`/player?id=${encodeURIComponent(prevId)}`, { replace: true })
    }, [playPrevious, navigate])

    const handleNextTrack = useCallback(() => {
        const nextId = playNext()
        if (nextId) navigate(`/player?id=${encodeURIComponent(nextId)}`, { replace: true })
    }, [playNext, navigate])

    const hasPlaylist = currentPlaylist.length > 1

    const fireAnalytics = useCallback(
        (type: string, data?: Record<string, unknown>) => {
            if (!mediaId) return
            analyticsApi.trackEvent({ type, media_id: mediaId, data }).catch(() => {})
        },
        [mediaId],
    )

    const handleFullscreen = useCallback(() => {
        const wrapper = videoRef.current?.parentElement
        if (!wrapper) return
        if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
        else wrapper.requestFullscreen().catch(() => {})
    }, [])

    const handleVideoClick = useCallback(() => {
        const now = Date.now()
        if (now - lastClickTimeRef.current < 300) {
            handleFullscreen()
            lastClickTimeRef.current = 0
            return
        }
        lastClickTimeRef.current = now
        setTimeout(() => {
            if (lastClickTimeRef.current === now) togglePlay()
        }, 300)
    }, [handleFullscreen, togglePlay])

    const handleTimeUpdate = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        currentTimeRef.current = el.currentTime
        setCurrentTime(el.currentTime)
        if (el.buffered.length > 0) {
            setBuffered(
                (el.buffered.end(el.buffered.length - 1) / el.duration) * 100,
            )
        }
    }, [getActiveEl])

    const handleLoadedMetadata = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        durationRef.current = el.duration
        setDuration(el.duration)
        setIsLoading(false)
        const resumeEnabled = user?.preferences?.resume_playback !== false
        const saved = resumePositionRef.current
        if (
            resumeEnabled &&
            saved > 5 &&
            el.duration > 0 &&
            saved < el.duration - 5
        ) {
            el.currentTime = saved
        }
        resumePositionRef.current = 0
        el.play().catch(() => {})
    }, [getActiveEl, user?.preferences?.resume_playback])

    const handlePause = useCallback(() => {
        setIsPlaying(false)
        if (
            mediaId &&
            currentTimeRef.current > 0 &&
            durationRef.current > 0
        ) {
            watchHistoryApi
                .trackPosition(
                    mediaId,
                    currentTimeRef.current,
                    durationRef.current,
                )
                .catch(() => {})
        }
        fireAnalytics('pause', {
            position: currentTimeRef.current,
            duration: durationRef.current,
        })
    }, [mediaId, fireAnalytics])

    const handleDurationChange = useCallback(() => {
        const el = getActiveEl()
        if (el && isFinite(el.duration) && el.duration > 0) {
            durationRef.current = el.duration
            setDuration(el.duration)
        }
    }, [getActiveEl])

    const handleProgressClick = useCallback(
        (e: React.MouseEvent<HTMLDivElement>) => {
            const el = getActiveEl()
            if (!el || !duration) return
            const rect = e.currentTarget.getBoundingClientRect()
            const ratio = (e.clientX - rect.left) / rect.width
            el.currentTime = ratio * duration
        },
        [getActiveEl, duration],
    )

    const handleProgressTouch = useCallback(
        (e: React.TouchEvent<HTMLDivElement>) => {
            e.preventDefault()
            const touch = e.touches[0] ?? e.changedTouches[0]
            if (!touch) return
            const rect = e.currentTarget.getBoundingClientRect()
            const ratio = Math.max(
                0,
                Math.min(1, (touch.clientX - rect.left) / rect.width),
            )
            const el = getActiveEl()
            if (!el || !duration) return
            el.currentTime = ratio * duration
        },
        [getActiveEl, duration],
    )

    const handleProgressHover = useCallback(
        (e: React.MouseEvent<HTMLDivElement>) => {
            if (!duration) return
            const rect = e.currentTarget.getBoundingClientRect()
            const ratio = Math.max(
                0,
                Math.min(1, (e.clientX - rect.left) / rect.width),
            )
            setHoverTime(ratio * duration)
            setHoverPos(e.clientX - rect.left)
        },
        [duration],
    )

    const handleProgressLeave = useCallback(() => setHoverTime(null), [])

    const handleSeeked = useCallback(() => {
        if (
            mediaId &&
            currentTimeRef.current > 5 &&
            durationRef.current > 0
        ) {
            watchHistoryApi
                .trackPosition(
                    mediaId,
                    currentTimeRef.current,
                    durationRef.current,
                )
                .catch(() => {})
        }
        fireAnalytics('seek', {
            position: currentTimeRef.current,
            duration: durationRef.current,
        })
    }, [mediaId, fireAnalytics])

    const handleVolumeChange = useCallback(
        (e: React.ChangeEvent<HTMLInputElement>) => {
            const v = parseFloat(e.target.value)
            setVolume(v)
            const el = getActiveEl()
            if (el) el.volume = v
        },
        [getActiveEl],
    )

    const toggleMute = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        el.muted = !el.muted
        setIsMuted(el.muted)
    }, [getActiveEl])

    const toggleLoop = useCallback(() => {
        const el = getActiveEl()
        const newLoop = !isLooping
        setIsLooping(newLoop)
        if (el) el.loop = newLoop
    }, [getActiveEl, isLooping])

    const setSpeed = useCallback(
        (speed: number) => {
            const el = getActiveEl()
            setPlaybackRate(speed)
            if (el) el.playbackRate = speed
        },
        [getActiveEl],
    )

    const handlePlay = useCallback(() => {
        setIsPlaying(true)
        fireAnalytics(
            'play',
            durationRef.current > 0 ? { duration: durationRef.current } : undefined,
        )
    }, [fireAnalytics])

    const handleWaiting = useCallback(() => {
        setIsLoading(true)
        fireAnalytics('buffering', { state: 'start' })
    }, [fireAnalytics])

    const handleCanPlay = useCallback(() => {
        setIsLoading(false)
        fireAnalytics('buffering', { state: 'end' })
    }, [fireAnalytics])

    const handleSelectQualityWithAnalytics = useCallback(
        (index: number) => {
            selectQuality(index)
            const name =
                index === -1
                    ? 'Auto'
                    : hlsQualities.find((q) => q.index === index)?.name ?? String(index)
            fireAnalytics('quality_change', { quality_index: index, quality_name: name })
        },
        [selectQuality, hlsQualities, fireAnalytics],
    )

    const handlePiP = useCallback(() => {
        const vid = videoRef.current
        if (!vid) return
        if (document.pictureInPictureElement) {
            document.exitPictureInPicture().catch(() => {})
        } else {
            vid.requestPictureInPicture().catch(() => {})
        }
    }, [])

    const handleEnded = useCallback(() => {
        if (mediaId) {
            watchHistoryApi
                .trackPosition(mediaId, duration, duration)
                .catch(() => {})
            fireAnalytics('complete', { duration, position: duration })
        }
        const autoPlayNext = user?.preferences?.auto_play === true
        const nextId = autoPlayNext ? playNext() : null
        if (nextId) {
            navigate(`/player?id=${encodeURIComponent(nextId)}`, { replace: true })
        }
    }, [mediaId, duration, user?.preferences?.auto_play, playNext, navigate, fireAnalytics])

    useEffect(() => {
        const onFullscreenChange = () => {
            if (mediaId) {
                fireAnalytics('fullscreen', {
                    active: !!document.fullscreenElement,
                })
            }
        }
        document.addEventListener('fullscreenchange', onFullscreenChange)
        return () =>
            document.removeEventListener('fullscreenchange', onFullscreenChange)
    }, [mediaId, fireAnalytics])

    useEffect(() => {
        if (!mediaId || !isPlaying || !duration) return
        const interval = setInterval(() => {
            if (
                durationRef.current > 0 &&
                currentTimeRef.current > 0
            ) {
                watchHistoryApi
                    .trackPosition(
                        mediaId,
                        currentTimeRef.current,
                        durationRef.current,
                    )
                    .catch(() => {})
            }
        }, 30000)
        return () => clearInterval(interval)
    }, [mediaId, isPlaying, duration])

    usePlayerKeyboard({
        getActiveEl,
        togglePlay,
        setSpeed,
        setVolume,
        setIsMuted,
        handleFullscreen,
        setTheaterMode,
        setShowSettings,
        showSettings,
        playbackRate,
    })

    const progress = duration > 0 ? (currentTime / duration) * 100 : 0
    let qualityBadge: string | null = null
    if (hlsQualities.length > 0) {
        if (currentQuality === -1) {
            const qual = autoLevel >= 0 ? hlsQualities[autoLevel] : undefined
            qualityBadge = qual?.name ?? null
        } else {
            qualityBadge = hlsQualities.find((q) => q.index === currentQuality)?.name ?? null
        }
    }

    const isAudio = media?.type === 'audio'

    return {
        mediaId,
        media,
        mediaLoading,
        mediaError,
        videoRef,
        audioRef,
        permissions,
        user,
        canViewMature,
        showToast,
        isPlaying,
        currentTime,
        duration,
        buffered,
        volume,
        isMuted,
        isLooping,
        playbackRate,
        showControls,
        showSettings,
        isLoading,
        showMatureWarning,
        setMatureAccepted,
        theaterMode,
        setTheaterMode,
        setShowControls,
        setShowSettings,
        hoverTime,
        hoverPos,
        hlsJob,
        activeHlsUrl,
        hlsAvailable,
        hlsReadyUrl,
        setActiveHlsUrl,
        setHlsAvailable,
        hlsIsLoading,
        related,
        relatedLabel,
        relatedStillLoading,
        similarError,
        similarRefetch,
        isAudio,
        isVideo,
        resetControlsTimer,
        getActiveEl,
        togglePlay,
        handlePrevTrack,
        handleNextTrack,
        hasPlaylist,
        handleVideoClick,
        handleTimeUpdate,
        handleLoadedMetadata,
        handlePause,
        handleDurationChange,
        handleProgressClick,
        handleProgressTouch,
        handleProgressHover,
        handleProgressLeave,
        handleSeeked,
        handleVolumeChange,
        toggleMute,
        toggleLoop,
        setSpeed,
        handleFullscreen,
        handlePlay,
        handleWaiting,
        handleCanPlay,
        handleSelectQualityWithAnalytics,
        handlePiP,
        handleEnded,
        handleRate,
        setUserRating,
        setRatingHover,
        ratingHover,
        userRating,
        progress,
        qualityBadge,
        hlsQualities,
        currentQuality,
        autoLevel,
        bandwidth,
    }
}

export type PlayerPageState = ReturnType<typeof usePlayerPageState>
