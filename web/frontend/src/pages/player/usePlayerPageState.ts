import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { MutableRefObject, RefObject } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '@/stores/authStore'
import { usePlaylistStore } from '@/stores/playlistStore'
import { useToast } from '@/hooks/useToast'
import { useSettingsStore } from '@/stores/settingsStore'
import { useEqualizer } from '@/hooks/useEqualizer'
import {
    analyticsApi,
    mediaApi,
    ratingsApi,
    watchHistoryApi,
} from '@/api/endpoints'
import type { HLSJob, User, UserPermissions } from '@/api/types'
import { usePlayerKeyboard } from './playerKeyboard'
import { usePlayerMediaQueries } from './playerMediaQueries'
import { usePlayerHLS } from './playerHLS'

/** Params for playlist index sync. */
interface PlaylistSyncParams {
    mediaId: string
    playlist: { media_id: string }[]
    currentIndex: number
    setCurrentIndex: (i: number) => void
}

/** Params for resume-position check. */
interface ResumeCheckParams {
    element: HTMLMediaElement
    position: number
}

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

function syncPlaylistIndex(params: PlaylistSyncParams): void {
    const { mediaId, playlist, currentIndex, setCurrentIndex } = params
    if (!mediaId || playlist.length === 0) return
    const idx = playlist.findIndex((i) => i.media_id === mediaId)
    if (idx === -1 || idx === currentIndex) return
    setCurrentIndex(idx)
}

function shouldResumeAtPosition(params: ResumeCheckParams): boolean {
    const { element: el, position: pos } = params
    if (pos <= 5 || el.readyState < 1 || el.duration <= 0) return false
    if (pos >= el.duration - 5 || el.currentTime >= 2) return false
    return true
}

/** Syncs user quality/preference and playlist index. Reduces main hook complexity. */
function usePlayerSyncEffects(
    user: { preferences?: Record<string, unknown> } | null,
    mediaId: string,
    currentPlaylist: { media_id: string }[],
    currentIndex: number,
    setCurrentIndex: (i: number) => void,
) {
    useEffect(() => {
        syncQualityPreference(user?.preferences?.default_quality as string | undefined)
    }, [user?.preferences?.default_quality])

    useEffect(() => {
        syncPlaylistIndex({ mediaId, playlist: currentPlaylist, currentIndex, setCurrentIndex })
    }, [mediaId, currentPlaylist, currentIndex, setCurrentIndex])
}

/** Runs media source setup (src, resume, analytics). Called from effect in main hook. */
function runMediaSourceSetup(
    mediaId: string,
    media: { id: string; type: string; is_mature?: boolean },
    videoRef: RefObject<HTMLVideoElement | null>,
    audioRef: RefObject<HTMLAudioElement | null>,
    user: { preferences?: { resume_playback?: boolean } } | null,
    volume: number,
    isLooping: boolean,
    playbackRate: number,
    setters: {
        setIsLoading: (v: boolean) => void
        setCurrentTime: (v: number) => void
        setDuration: (v: number) => void
        setIsPlaying: (v: boolean) => void
        setActiveHlsUrl: (v: string | null) => void
        setHlsAvailable: (v: boolean) => void
        setHlsReadyUrl: (v: string | null) => void
        setHlsJob: (v: HLSJob | null) => void
        setHlsPolling: (v: boolean) => void
        setUserRating: (v: number) => void
    },
    resumePositionRef: MutableRefObject<number>,
) {
    const el = media.type === 'video' ? videoRef.current : audioRef.current
    if (!el) return () => {}

    queueMicrotask(() => {
        setters.setIsLoading(true)
        setters.setCurrentTime(0)
        setters.setDuration(0)
        setters.setIsPlaying(false)
        setters.setActiveHlsUrl(null)
        setters.setHlsAvailable(false)
        setters.setHlsReadyUrl(null)
        setters.setHlsJob(null)
        setters.setHlsPolling(false)
        setters.setUserRating(0)
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
                if (shouldResumeAtPosition({ element: elRef, position: pos })) {
                    elRef.currentTime = pos
                }
            })
            .catch(() => {})
    }
    analyticsApi.trackEvent({ type: 'view', media_id: media.id }).catch(() => {})

    return () => {
        positionFetchCancelled = true
    }
}

/** Playback event handlers and getActiveEl. Isolates callback logic from main hook. */
function usePlayerPlaybackHandlers(
    media: { type: string } | undefined,
    videoRef: RefObject<HTMLVideoElement | null>,
    audioRef: RefObject<HTMLAudioElement | null>,
    mediaId: string,
    user: { preferences?: { resume_playback?: boolean } } | null,
    currentTimeRef: MutableRefObject<number>,
    durationRef: MutableRefObject<number>,
    resumePositionRef: MutableRefObject<number>,
    setters: {
        setCurrentTime: (v: number) => void
        setDuration: (v: number) => void
        setIsPlaying: (v: boolean) => void
        setBuffered: (v: number) => void
        setIsLoading: (v: boolean) => void
        setVolume: (v: number) => void
        setIsMuted: (v: boolean) => void
        setIsLooping: (v: boolean) => void
        setPlaybackRate: (v: number) => void
        setHoverTime: (v: number | null) => void
        setHoverPos: (v: number) => void
    },
    isLooping: boolean,
    duration: number,
) {
    const getActiveEl = useCallback(() => {
        if (!media) return null
        return media.type === 'video' ? videoRef.current : audioRef.current
    }, [media, videoRef, audioRef])

    const fireAnalytics = useCallback(
        (type: string, data?: Record<string, unknown>) => {
            if (!mediaId) return
            analyticsApi.trackEvent({ type, media_id: mediaId, data }).catch(() => {})
        },
        [mediaId],
    )

    const togglePlay = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        if (el.paused) el.play().catch(() => {})
        else el.pause()
    }, [getActiveEl])

    const handleTimeUpdate = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        currentTimeRef.current = el.currentTime
        setters.setCurrentTime(el.currentTime)
        if (el.buffered.length > 0) {
            setters.setBuffered(
                (el.buffered.end(el.buffered.length - 1) / el.duration) * 100,
            )
        }
    }, [getActiveEl, currentTimeRef, setters])

    const handleLoadedMetadata = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        durationRef.current = el.duration
        setters.setDuration(el.duration)
        setters.setIsLoading(false)
        const resumeEnabled = user?.preferences?.resume_playback !== false
        const saved = resumePositionRef.current
        const hasValidDuration = el.duration > 0
        const savedInRange =
            saved > 5 && hasValidDuration && saved < el.duration - 5
        if (resumeEnabled && savedInRange) {
            el.currentTime = saved
        }
        resumePositionRef.current = 0
        el.play().catch(() => {})
    }, [getActiveEl, user?.preferences?.resume_playback, durationRef, resumePositionRef, setters])

    const handlePause = useCallback(() => {
        setters.setIsPlaying(false)
        const hasValidPosition =
            mediaId && currentTimeRef.current > 0 && durationRef.current > 0
        if (hasValidPosition) {
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
    }, [mediaId, fireAnalytics, currentTimeRef, durationRef, setters])

    const handleDurationChange = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        if (!isFinite(el.duration) || el.duration <= 0) return
        durationRef.current = el.duration
        setters.setDuration(el.duration)
    }, [getActiveEl, durationRef, setters])

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
            setters.setHoverTime(ratio * duration)
            setters.setHoverPos(e.clientX - rect.left)
        },
        [duration, setters],
    )

    const handleProgressLeave = useCallback(() => setters.setHoverTime(null), [setters])

    const handleSeeked = useCallback(() => {
        const shouldTrackPosition =
            !!mediaId && currentTimeRef.current > 5 && durationRef.current > 0
        if (shouldTrackPosition) {
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
    }, [mediaId, fireAnalytics, currentTimeRef, durationRef])

    const handleVolumeChange = useCallback(
        (e: React.ChangeEvent<HTMLInputElement>) => {
            const v = parseFloat(e.target.value)
            setters.setVolume(v)
            const el = getActiveEl()
            if (el) el.volume = v
        },
        [getActiveEl, setters],
    )

    const toggleMute = useCallback(() => {
        const el = getActiveEl()
        if (!el) return
        el.muted = !el.muted
        setters.setIsMuted(el.muted)
    }, [getActiveEl, setters])

    const toggleLoop = useCallback(() => {
        const el = getActiveEl()
        const newLoop = !isLooping
        setters.setIsLooping(newLoop)
        if (el) el.loop = newLoop
    }, [getActiveEl, isLooping, setters])

    const setSpeed = useCallback(
        (speed: number) => {
            const el = getActiveEl()
            setters.setPlaybackRate(speed)
            if (el) el.playbackRate = speed
        },
        [getActiveEl, setters],
    )

    const handlePlay = useCallback(() => {
        setters.setIsPlaying(true)
        fireAnalytics(
            'play',
            durationRef.current > 0 ? { duration: durationRef.current } : undefined,
        )
    }, [fireAnalytics, durationRef, setters])

    const handleWaiting = useCallback(() => {
        setters.setIsLoading(true)
        fireAnalytics('buffering', { state: 'start' })
    }, [fireAnalytics, setters])

    const handleCanPlay = useCallback(() => {
        setters.setIsLoading(false)
        fireAnalytics('buffering', { state: 'end' })
    }, [fireAnalytics, setters])

    return {
        getActiveEl,
        togglePlay,
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
        handlePlay,
        handleWaiting,
        handleCanPlay,
        fireAnalytics,
    }
}

/** Pure helper: compute quality badge from HLS state. */
function computeQualityBadge(
    hlsQualities: { index: number; name: string }[],
    currentQuality: number,
    autoLevel: number,
): string | null {
    if (hlsQualities.length === 0) return null
    if (currentQuality === -1) {
        const qual = autoLevel >= 0 ? hlsQualities[autoLevel] : undefined
        return qual?.name ?? null
    }
    return hlsQualities.find((q) => q.index === currentQuality)?.name ?? null
}

/** Tracks fullscreen change for analytics. */
function useFullscreenAnalytics(mediaId: string, fireAnalytics: (type: string, data?: Record<string, unknown>) => void): void {
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
}

/** Tracks playback position every 30s while playing. */
function usePeriodicPositionTracking(opts: {
    mediaId: string
    isPlaying: boolean
    duration: number
    currentTimeRef: MutableRefObject<number>
    durationRef: MutableRefObject<number>
}): void {
    const { mediaId, isPlaying, duration, currentTimeRef, durationRef } = opts
    useEffect(() => {
        if (!mediaId || !isPlaying || !duration) return
        const interval = setInterval(() => {
            if (durationRef.current > 0 && currentTimeRef.current > 0) {
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
    }, [mediaId, isPlaying, duration, currentTimeRef, durationRef])
}

/** Navigation handlers (prev/next) and playlist flag. */
function usePlayerNavigation(
    playPrevious: () => string | null | undefined,
    playNext: () => string | null | undefined,
    navigate: ReturnType<typeof useNavigate>,
    currentPlaylist: { media_id: string }[],
) {
    const handlePrevTrack = useCallback(() => {
        const prevId = playPrevious()
        if (prevId) navigate(`/player?id=${encodeURIComponent(prevId)}`, { replace: true })
    }, [playPrevious, navigate])

    const handleNextTrack = useCallback(() => {
        const nextId = playNext()
        if (nextId) navigate(`/player?id=${encodeURIComponent(nextId)}`, { replace: true })
    }, [playNext, navigate])

    const hasPlaylist = currentPlaylist.length > 1
    return { handlePrevTrack, handleNextTrack, hasPlaylist }
}

/** Video-specific handlers: fullscreen, click-to-play/pause, PiP, ended. */
function usePlayerVideoHandlers(opts: {
    videoRef: RefObject<HTMLVideoElement | null>
    togglePlay: () => void
    mediaId: string
    duration: number
    user: { preferences?: { auto_play?: boolean } } | null
    playNext: () => string | null | undefined
    navigate: ReturnType<typeof useNavigate>
    fireAnalytics: (type: string, data?: Record<string, unknown>) => void
}) {
    const { videoRef, togglePlay, mediaId, duration, user, playNext, navigate, fireAnalytics } =
        opts
    const lastClickTimeRef = useRef(0)

    const handleFullscreen = useCallback(() => {
        const wrapper = videoRef.current?.parentElement
        if (!wrapper) return
        if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
        else wrapper.requestFullscreen().catch(() => {})
    }, [videoRef])

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

    const handlePiP = useCallback(() => {
        const vid = videoRef.current
        if (!vid) return
        if (document.pictureInPictureElement) {
            document.exitPictureInPicture().catch(() => {})
        } else {
            vid.requestPictureInPicture().catch(() => {})
        }
    }, [videoRef])

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

    return { handleFullscreen, handleVideoClick, handlePiP, handleEnded }
}

/** Refs and primitive state. Isolates useState/useRef from main hook. */
function usePlayerRefsAndState(
    user: { preferences?: { playback_speed?: number } } | null,
) {
    const videoRef = useRef<HTMLVideoElement>(null)
    const audioRef = useRef<HTMLAudioElement>(null)
    const resumePositionRef = useRef(0)
    const currentTimeRef = useRef(0)
    const durationRef = useRef(0)

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
    const [userRating, setUserRating] = useState(0)
    const [ratingHover, setRatingHover] = useState(0)

    return {
        videoRef,
        audioRef,
        resumePositionRef,
        currentTimeRef,
        durationRef,
        audioReady,
        setAudioReady,
        isPlaying,
        setIsPlaying,
        currentTime,
        setCurrentTime,
        duration,
        setDuration,
        buffered,
        setBuffered,
        volume,
        setVolume,
        isMuted,
        setIsMuted,
        isLooping,
        setIsLooping,
        playbackRate,
        setPlaybackRate,
        showControls,
        setShowControls,
        showSettings,
        setShowSettings,
        isLoading,
        setIsLoading,
        matureAccepted,
        setMatureAccepted,
        theaterMode,
        setTheaterMode,
        hoverTime,
        setHoverTime,
        hoverPos,
        setHoverPos,
        userRating,
        setUserRating,
        ratingHover,
        setRatingHover,
    }
}

/** Runs player-side effects (toast, audio ready, media source, fallback src, controls timer, visibility, playback rate). */
function usePlayerEffects(
    mediaId: string,
    media: { id?: string; type: string; is_mature?: boolean } | undefined,
    matureAccessGranted: boolean,
    videoRef: RefObject<HTMLVideoElement | null>,
    audioRef: RefObject<HTMLAudioElement | null>,
    activeHlsUrl: string | null,
    user: { preferences?: { resume_playback?: boolean; playback_speed?: number } } | null,
    volume: number,
    isLooping: boolean,
    playbackRate: number,
    isPlaying: boolean,
    isVideo: boolean,
    hlsError: string | null,
    showToast: (msg: string, type: 'error' | 'info') => void,
    setters: {
        setAudioReady: (v: boolean) => void
        setIsLoading: (v: boolean) => void
        setCurrentTime: (v: number) => void
        setDuration: (v: number) => void
        setIsPlaying: (v: boolean) => void
        setActiveHlsUrl: (v: string | null) => void
        setHlsAvailable: (v: boolean) => void
        setHlsReadyUrl: (v: string | null) => void
        setHlsJob: (v: HLSJob | null) => void
        setHlsPolling: (v: boolean) => void
        setUserRating: (v: number) => void
        setShowControls: (v: boolean) => void
        setPlaybackRate: (v: number) => void
    },
    currentTimeRef: MutableRefObject<number>,
    durationRef: MutableRefObject<number>,
    resumePositionRef: MutableRefObject<number>,
    controlsTimerRef: MutableRefObject<ReturnType<typeof setTimeout> | null>,
    isPlayingRef: MutableRefObject<boolean>,
) {
    useEffect(() => {
        if (hlsError) showToast(hlsError, 'error')
    }, [hlsError, showToast])

    useEffect(() => {
        if (audioRef.current) queueMicrotask(() => setters.setAudioReady(true))
    }, [audioRef, setters])

    useEffect(() => {
        if (!mediaId || !media || !media.id || (media.is_mature && !matureAccessGranted)) return
        const mediaWithId = media as { id: string; type: string; is_mature?: boolean }
        const cleanup = runMediaSourceSetup(
            mediaId,
            mediaWithId,
            videoRef,
            audioRef,
            user,
            volume,
            isLooping,
            playbackRate,
            {
                setIsLoading: setters.setIsLoading,
                setCurrentTime: setters.setCurrentTime,
                setDuration: setters.setDuration,
                setIsPlaying: setters.setIsPlaying,
                setActiveHlsUrl: setters.setActiveHlsUrl,
                setHlsAvailable: setters.setHlsAvailable,
                setHlsReadyUrl: setters.setHlsReadyUrl,
                setHlsJob: setters.setHlsJob,
            setHlsPolling: setters.setHlsPolling,
            setUserRating: setters.setUserRating,
            },
            resumePositionRef,
        )
        return cleanup
    }, [mediaId, media, matureAccessGranted]) // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        if (activeHlsUrl !== null || !mediaId || !media || (media.is_mature && !matureAccessGranted))
            return
        const el = media.type === 'video' ? videoRef.current : audioRef.current
        if (!el || (el.src && el.src !== '' && el.src !== window.location.href)) return
        el.src = mediaApi.getStreamUrl(mediaId)
    }, [activeHlsUrl, mediaId, media, matureAccessGranted, videoRef, audioRef])

    useEffect(
        () => () => {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        },
        [controlsTimerRef],
    )

    useEffect(() => {
        if (!isVideo) return
        isPlayingRef.current = isPlaying
        if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        if (isPlaying) {
            controlsTimerRef.current = setTimeout(() => setters.setShowControls(false), 3000)
        } else {
            queueMicrotask(() => setters.setShowControls(true))
        }
    }, [isPlaying, isVideo, controlsTimerRef, isPlayingRef, setters])

    useEffect(() => {
        if (!mediaId) return
        const handleVisibilityChange = () => {
            const isHidden = document.visibilityState === 'hidden'
            const hasValidPosition =
                currentTimeRef.current > 0 && durationRef.current > 0
            if (isHidden && hasValidPosition) {
                watchHistoryApi
                    .trackPosition(
                        mediaId,
                        currentTimeRef.current,
                        durationRef.current,
                    )
                    .catch(() => {})
            }
        }
        document.addEventListener('visibilitychange', handleVisibilityChange)
        return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
    }, [mediaId, currentTimeRef, durationRef])

    useEffect(() => {
        const pref = user?.preferences?.playback_speed
        if (pref === null || pref === undefined || pref < 0.25 || pref > 2) return
        queueMicrotask(() => setters.setPlaybackRate(pref))
        const el = media?.type === 'video' ? videoRef.current : audioRef.current
        if (el) el.playbackRate = pref
    }, [user?.preferences?.playback_speed, media?.type, videoRef, audioRef, setters])
}

/** Playback setters, handlers, navigation, video handlers, quality selection. Reduces main hook size. */
function usePlayerPageHandlers(opts: {
    state: ReturnType<typeof usePlayerRefsAndState>
    mediaHls: ReturnType<typeof usePlayerMediaHlsAndEffects>
    mediaId: string
    user: User | null
    navigate: ReturnType<typeof useNavigate>
    playNext: () => string | null | undefined
    playPrevious: () => string | null | undefined
    currentPlaylist: { media_id: string }[]
}) {
    const { state, mediaHls, mediaId, user, navigate, playNext, playPrevious, currentPlaylist } = opts
    const playbackSetters = useMemo(
        () => ({
            setCurrentTime: state.setCurrentTime,
            setDuration: state.setDuration,
            setIsPlaying: state.setIsPlaying,
            setBuffered: state.setBuffered,
            setIsLoading: state.setIsLoading,
            setVolume: state.setVolume,
            setIsMuted: state.setIsMuted,
            setIsLooping: state.setIsLooping,
            setPlaybackRate: state.setPlaybackRate,
            setHoverTime: state.setHoverTime,
            setHoverPos: state.setHoverPos,
        }),
        [
            state.setCurrentTime,
            state.setDuration,
            state.setIsPlaying,
            state.setBuffered,
            state.setIsLoading,
            state.setVolume,
            state.setIsMuted,
            state.setIsLooping,
            state.setPlaybackRate,
            state.setHoverTime,
            state.setHoverPos,
        ],
    )
    const playbackHandlers = usePlayerPlaybackHandlers(
        mediaHls.media,
        state.videoRef,
        state.audioRef,
        mediaId,
        user,
        state.currentTimeRef,
        state.durationRef,
        state.resumePositionRef,
        playbackSetters,
        state.isLooping,
        state.duration,
    )
    const { handlePrevTrack, handleNextTrack, hasPlaylist } = usePlayerNavigation(
        playPrevious,
        playNext,
        navigate,
        currentPlaylist,
    )
    const { handleFullscreen, handleVideoClick, handlePiP, handleEnded } =
        usePlayerVideoHandlers({
            videoRef: state.videoRef,
            togglePlay: playbackHandlers.togglePlay,
            mediaId,
            duration: state.duration,
            user,
            playNext,
            navigate,
            fireAnalytics: playbackHandlers.fireAnalytics,
        })
    const handleSelectQualityWithAnalytics = useCallback(
        (index: number) => {
            mediaHls.selectQuality(index)
            const name =
                index === -1
                    ? 'Auto'
                    : mediaHls.hlsQualities.find((q) => q.index === index)?.name ?? String(index)
            playbackHandlers.fireAnalytics('quality_change', { quality_index: index, quality_name: name })
        },
        [mediaHls, playbackHandlers],
    )
    return {
        ...playbackHandlers,
        handlePrevTrack,
        handleNextTrack,
        hasPlaylist,
        handleVideoClick,
        handleFullscreen,
        handlePiP,
        handleEnded,
        handleSelectQualityWithAnalytics,
    }
}

type PlayerPageReturnOpts = {
    mediaId: string
    permissions: UserPermissions
    user: User | null
    canViewMature: boolean
    showToast: (msg: string, type?: 'success' | 'error' | 'warning' | 'info') => void
    state: ReturnType<typeof usePlayerRefsAndState>
    mediaHls: ReturnType<typeof usePlayerMediaHlsAndEffects>
    handlers: ReturnType<typeof usePlayerPageHandlers>
    handleRate: (rating: number) => void
    resetControlsTimer: () => void
    progress: number
    qualityBadge: string | null
    isAudio: boolean
}

function getPlayerReturnStateAndMedia(opts: PlayerPageReturnOpts) {
    const { mediaId, permissions, user, canViewMature, showToast, state, mediaHls, progress, qualityBadge, isAudio } = opts
    return {
        mediaId,
        media: mediaHls.media,
        mediaLoading: mediaHls.mediaLoading,
        mediaError: mediaHls.mediaError,
        videoRef: state.videoRef,
        audioRef: state.audioRef,
        permissions,
        user,
        canViewMature,
        showToast,
        isPlaying: state.isPlaying,
        currentTime: state.currentTime,
        duration: state.duration,
        buffered: state.buffered,
        volume: state.volume,
        isMuted: state.isMuted,
        isLooping: state.isLooping,
        playbackRate: state.playbackRate,
        showControls: state.showControls,
        showSettings: state.showSettings,
        isLoading: state.isLoading,
        showMatureWarning: mediaHls.showMatureWarning,
        setMatureAccepted: state.setMatureAccepted,
        theaterMode: state.theaterMode,
        setTheaterMode: state.setTheaterMode,
        setShowControls: state.setShowControls,
        setShowSettings: state.setShowSettings,
        hoverTime: state.hoverTime,
        hoverPos: state.hoverPos,
        hlsJob: mediaHls.hlsJob,
        activeHlsUrl: mediaHls.activeHlsUrl,
        hlsAvailable: mediaHls.hlsAvailable,
        hlsReadyUrl: mediaHls.hlsReadyUrl,
        setActiveHlsUrl: mediaHls.setActiveHlsUrl,
        setHlsAvailable: mediaHls.setHlsAvailable,
        hlsIsLoading: mediaHls.hlsIsLoading,
        related: mediaHls.related,
        relatedLabel: mediaHls.relatedLabel,
        relatedStillLoading: mediaHls.relatedStillLoading,
        similarError: mediaHls.similarError,
        similarRefetch: mediaHls.similarRefetch,
        isAudio,
        isVideo: mediaHls.isVideo,
        progress,
        qualityBadge,
        hlsQualities: mediaHls.hlsQualities,
        currentQuality: mediaHls.currentQuality,
        autoLevel: mediaHls.autoLevel,
        bandwidth: mediaHls.bandwidth,
        setUserRating: state.setUserRating,
        setRatingHover: state.setRatingHover,
        ratingHover: state.ratingHover,
        userRating: state.userRating,
    }
}

function getPlayerReturnHandlers(opts: PlayerPageReturnOpts) {
    const { handlers, handleRate, resetControlsTimer } = opts
    return {
        resetControlsTimer,
        getActiveEl: handlers.getActiveEl,
        togglePlay: handlers.togglePlay,
        handlePrevTrack: handlers.handlePrevTrack,
        handleNextTrack: handlers.handleNextTrack,
        hasPlaylist: handlers.hasPlaylist,
        handleVideoClick: handlers.handleVideoClick,
        handleTimeUpdate: handlers.handleTimeUpdate,
        handleLoadedMetadata: handlers.handleLoadedMetadata,
        handlePause: handlers.handlePause,
        handleDurationChange: handlers.handleDurationChange,
        handleProgressClick: handlers.handleProgressClick,
        handleProgressTouch: handlers.handleProgressTouch,
        handleProgressHover: handlers.handleProgressHover,
        handleProgressLeave: handlers.handleProgressLeave,
        handleSeeked: handlers.handleSeeked,
        handleVolumeChange: handlers.handleVolumeChange,
        toggleMute: handlers.toggleMute,
        toggleLoop: handlers.toggleLoop,
        setSpeed: handlers.setSpeed,
        handleFullscreen: handlers.handleFullscreen,
        handlePlay: handlers.handlePlay,
        handleWaiting: handlers.handleWaiting,
        handleCanPlay: handlers.handleCanPlay,
        handleSelectQualityWithAnalytics: handlers.handleSelectQualityWithAnalytics,
        handlePiP: handlers.handlePiP,
        handleEnded: handlers.handleEnded,
        handleRate,
    }
}

/** Builds and returns the player page state object. Keeps usePlayerPageState under LoC limit. */
function usePlayerPageReturn(opts: PlayerPageReturnOpts) {
    return {
        ...getPlayerReturnStateAndMedia(opts),
        ...getPlayerReturnHandlers(opts),
    }
}

/** Media queries, HLS, equalizer, and effects. Isolates data/effect wiring from main hook. */
function usePlayerMediaHlsAndEffects(opts: {
    mediaId: string
    canViewMature: boolean
    user: { preferences?: { show_mature?: boolean; resume_playback?: boolean; playback_speed?: number } } | null
    currentPlaylist: { media_id: string }[]
    currentIndex: number
    setCurrentIndex: (i: number) => void
    state: ReturnType<typeof usePlayerRefsAndState>
    controlsTimerRef: MutableRefObject<ReturnType<typeof setTimeout> | null>
    isPlayingRef: MutableRefObject<boolean>
    hlsEnabled: boolean
    showToast: (msg: string, type: 'error' | 'info') => void
}) {
    const {
        mediaId,
        canViewMature,
        user,
        currentPlaylist,
        currentIndex,
        setCurrentIndex,
        state,
        controlsTimerRef,
        isPlayingRef,
        hlsEnabled,
        showToast,
    } = opts
    usePlayerSyncEffects(user, mediaId, currentPlaylist, currentIndex, setCurrentIndex)

    const {
        media,
        mediaLoading,
        mediaError,
        related,
        relatedLabel,
        relatedStillLoading,
        similarError,
        similarRefetch,
    } = usePlayerMediaQueries({ mediaId, canViewMature })

    const matureAccessGranted =
        state.matureAccepted || (user?.preferences?.show_mature === true)
    const showMatureWarning = !!(media?.is_mature && !matureAccessGranted)

    const {
        hlsJob,
        setHlsJob,
        setHlsPolling,
        activeHlsUrl,
        setActiveHlsUrl,
        hlsAvailable,
        setHlsAvailable,
        hlsReadyUrl,
        setHlsReadyUrl,
        hlsQualities,
        currentQuality,
        autoLevel,
        selectQuality,
        hlsIsLoading,
        hlsError,
        bandwidth,
    } = usePlayerHLS(mediaId, media, hlsEnabled, state.videoRef)

    useEqualizer(state.audioRef, state.audioReady && media?.type === 'audio')

    const isVideo = media?.type === 'video'

    usePlayerEffects(
        mediaId,
        media ?? undefined,
        matureAccessGranted,
        state.videoRef,
        state.audioRef,
        activeHlsUrl,
        user,
        state.volume,
        state.isLooping,
        state.playbackRate,
        state.isPlaying,
        isVideo,
        hlsError ?? null,
        showToast,
        {
            setAudioReady: state.setAudioReady,
            setIsLoading: state.setIsLoading,
            setCurrentTime: state.setCurrentTime,
            setDuration: state.setDuration,
            setIsPlaying: state.setIsPlaying,
            setActiveHlsUrl,
            setHlsAvailable,
            setHlsReadyUrl,
            setHlsJob,
            setHlsPolling,
            setUserRating: state.setUserRating,
            setShowControls: state.setShowControls,
            setPlaybackRate: state.setPlaybackRate,
        },
        state.currentTimeRef,
        state.durationRef,
        state.resumePositionRef,
        controlsTimerRef,
        isPlayingRef,
    )

    return {
        media,
        mediaLoading,
        mediaError,
        related,
        relatedLabel,
        relatedStillLoading,
        similarError,
        similarRefetch,
        matureAccessGranted,
        showMatureWarning,
        hlsJob,
        setHlsJob,
        setHlsPolling,
        activeHlsUrl,
        setActiveHlsUrl,
        hlsAvailable,
        setHlsAvailable,
        hlsReadyUrl,
        setHlsReadyUrl,
        hlsQualities,
        currentQuality,
        autoLevel,
        selectQuality,
        hlsIsLoading,
        hlsError,
        bandwidth,
        isVideo,
    }
}

/** Callbacks for rating and controls timer. Reduces usePlayerPageState complexity. */
function usePlayerPageCallbacks(opts: {
    mediaId: string
    state: ReturnType<typeof usePlayerRefsAndState>
    controlsTimerRef: MutableRefObject<ReturnType<typeof setTimeout> | null>
    isPlayingRef: MutableRefObject<boolean>
}) {
    const { mediaId, state, controlsTimerRef, isPlayingRef } = opts
    const handleRate = useCallback(
        (rating: number) => {
            state.setUserRating(rating)
            if (mediaId) ratingsApi.record(mediaId, rating).catch(() => {})
        },
        [mediaId, state],
    )
    const resetControlsTimer = useCallback(() => {
        if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        state.setShowControls(true)
        controlsTimerRef.current = setTimeout(() => {
            if (isPlayingRef.current) state.setShowControls(false)
        }, 3000)
    }, [state, controlsTimerRef, isPlayingRef])
    return { handleRate, resetControlsTimer }
}

/** Assembles stores, refs, callbacks, mediaHls, and handlers. Reduces usePlayerPageState LoC. */
function usePlayerPageCore(mediaId: string) {
    const navigate = useNavigate()
    const permissions = useAuthStore((s) => s.permissions)
    const user = useAuthStore((s) => s.user)
    const canViewMature = permissions.can_view_mature && (user?.preferences?.show_mature === true)
    const { showToast } = useToast()
    const { currentPlaylist, currentIndex, setCurrentIndex, playNext, playPrevious } =
        usePlaylistStore()
    const hlsEnabled = useSettingsStore((s) => s.serverSettings?.features?.enableHLS ?? true)

    const state = usePlayerRefsAndState(user)
    const controlsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    const isPlayingRef = useRef(false)

    const { handleRate, resetControlsTimer } = usePlayerPageCallbacks({
        mediaId,
        state,
        controlsTimerRef,
        isPlayingRef,
    })

    const mediaHls = usePlayerMediaHlsAndEffects({
        mediaId,
        canViewMature,
        user,
        currentPlaylist,
        currentIndex,
        setCurrentIndex,
        state,
        controlsTimerRef,
        isPlayingRef,
        hlsEnabled,
        showToast,
    })

    const handlers = usePlayerPageHandlers({
        state,
        mediaHls,
        mediaId,
        user,
        navigate,
        playNext,
        playPrevious,
        currentPlaylist,
    })

    return {
        permissions,
        user,
        canViewMature,
        showToast,
        state,
        mediaHls,
        handlers,
        handleRate,
        resetControlsTimer,
    }
}

export function usePlayerPageState(mediaId: string) {
    const core = usePlayerPageCore(mediaId)

    useFullscreenAnalytics(mediaId, core.handlers.fireAnalytics)
    usePeriodicPositionTracking({
        mediaId,
        isPlaying: core.state.isPlaying,
        duration: core.state.duration,
        currentTimeRef: core.state.currentTimeRef,
        durationRef: core.state.durationRef,
    })

    usePlayerKeyboard({
        getActiveEl: core.handlers.getActiveEl,
        togglePlay: core.handlers.togglePlay,
        setSpeed: core.handlers.setSpeed,
        setVolume: core.state.setVolume,
        setIsMuted: core.state.setIsMuted,
        handleFullscreen: core.handlers.handleFullscreen,
        setTheaterMode: core.state.setTheaterMode,
        setShowSettings: core.state.setShowSettings,
        showSettings: core.state.showSettings,
        playbackRate: core.state.playbackRate,
    })

    const progress = core.state.duration > 0 ? (core.state.currentTime / core.state.duration) * 100 : 0
    const qualityBadge = computeQualityBadge(
        core.mediaHls.hlsQualities,
        core.mediaHls.currentQuality,
        core.mediaHls.autoLevel,
    )
    const isAudio = core.mediaHls.media?.type === 'audio'

    return usePlayerPageReturn({
        mediaId,
        permissions: core.permissions,
        user: core.user,
        canViewMature: core.canViewMature,
        showToast: core.showToast,
        state: core.state,
        mediaHls: core.mediaHls,
        handlers: core.handlers,
        handleRate: core.handleRate,
        resetControlsTimer: core.resetControlsTimer,
        progress,
        qualityBadge,
        isAudio,
    })
}

export type PlayerPageState = ReturnType<typeof usePlayerPageState>
