import {useCallback, useEffect, useRef, useState} from 'react'
import {Link, useNavigate, useSearchParams} from 'react-router-dom'
import {useQuery} from '@tanstack/react-query'
import {useAuthStore} from '@/stores/authStore'
import {usePlaylistStore} from '@/stores/playlistStore'
import {useToast} from '@/hooks/useToast'
import {SectionErrorBoundary} from '@/components/ErrorBoundary'
import {useHLS} from '@/hooks/useHLS'
import {useSettingsStore} from '@/stores/settingsStore'
import {useEqualizer} from '@/hooks/useEqualizer'
import {PlayerSettingsPanel} from '@/components/PlayerSettingsPanel'
import {ApiError} from '@/api/client'
import {analyticsApi, hlsApi, mediaApi, ratingsApi, suggestionsApi, watchHistoryApi} from '@/api/endpoints'
import type {HLSJob, Suggestion} from '@/api/types'
import {formatDuration, formatFileSize, formatTitle} from '@/utils/formatters'
import '@/styles/player.css'

// ── Similar Media Item ────────────────────────────────────────────────────────

function thumbnailUrlWithMatureBuster(url: string | undefined, canViewMature: boolean): string | undefined {
    if (!url) return undefined
    if (canViewMature) {
        const sep = url.includes('?') ? '&' : '?'
        return `${url}${sep}_m=1`
    }
    return url
}

function SimilarItem({entry, canViewMature}: { entry: Suggestion; canViewMature: boolean }) {
    const name = formatTitle(entry.title || entry.media_id)
    const thumbUrl = thumbnailUrlWithMatureBuster(entry.thumbnail_url ?? undefined, canViewMature)
    return (
        <Link to={`/player?id=${encodeURIComponent(entry.media_id)}`} className="related-item">
            {thumbUrl ? (
                <img
                    className="related-thumb"
                    src={thumbUrl}
                    alt={name}
                    loading="lazy"
                    onError={e => {
                        (e.target as HTMLImageElement).style.display = 'none'
                    }}
                />
            ) : (
                <div className="related-thumb-placeholder">
                    <i className={entry.media_type === 'audio' ? 'bi bi-music-note-beamed' : 'bi bi-play-circle'}/>
                </div>
            )}
            <div className="related-info">
                <div className="related-title">{name}</div>
                <div className="related-meta">
                    {entry.category && <span>{entry.category} · </span>}
                    {entry.score != null ? `${Math.round(entry.score * 100)}% match` : 'Similar'}
                </div>
            </div>
        </Link>
    )
}

// ── Main PlayerPage ───────────────────────────────────────────────────────────

export function PlayerPage() {
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const mediaId = searchParams.get('id') ?? ''
    const permissions = useAuthStore((s) => s.permissions)
    const user = useAuthStore((s) => s.user)
    const canViewMature = permissions.can_view_mature && (user?.preferences?.show_mature === true)
    const {showToast} = useToast()
    const {currentPlaylist, currentIndex, setCurrentIndex, playNext, playPrevious} = usePlaylistStore()

    // Media element refs
    const videoRef = useRef<HTMLVideoElement>(null)
    const audioRef = useRef<HTMLAudioElement>(null)
    // Saved position to seek to after metadata loads (not state — no re-render needed)
    const resumePositionRef = useRef(0)
    // Refs for periodic position tracking — avoids resetting the 30s interval on every
    // timeupdate tick (state updates would clear and re-create the interval constantly)
    const currentTimeRef = useRef(0)
    const durationRef = useRef(0)
    // Ref mirror of isPlaying so setTimeout callbacks read the current value without stale closure
    const isPlayingRef = useRef(false)
    // audioReady triggers after the audio element mounts so useEqualizer receives the
    // actual DOM node rather than null from the initial render pass
    const [audioReady, setAudioReady] = useState(false)

    // Playback state
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
    const [showMatureWarning, setShowMatureWarning] = useState(false)
    const [matureAccepted, setMatureAccepted] = useState(false)
    // Theater mode
    const [theaterMode, setTheaterMode] = useState(false)
    // Progress bar time tooltip
    const [hoverTime, setHoverTime] = useState<number | null>(null)
    const [hoverPos, setHoverPos] = useState(0)
    // HLS state
    const [hlsJob, setHlsJob] = useState<HLSJob | null>(null)
    const [hlsPolling, setHlsPolling] = useState(false)
    const [activeHlsUrl, setActiveHlsUrl] = useState<string | null>(null)
    const [hlsAvailable, setHlsAvailable] = useState(false)
    const [hlsReadyUrl, setHlsReadyUrl] = useState<string | null>(null)
    const hlsEnabled = useSettingsStore((s) => s.serverSettings?.features?.enableHLS ?? true)
    // Rating state
    const [userRating, setUserRating] = useState(0)
    const [ratingHover, setRatingHover] = useState(0)

    // Sync default_quality to HLS quality pref (useHLS reads from localStorage)
    useEffect(() => {
        const q = user?.preferences?.default_quality
        if (!q || q === 'auto') return
        const heightMap: Record<string, number> = { low: 360, medium: 480, high: 720, ultra: 1080 }
        const height = heightMap[q]
        if (height) {
            try {
                localStorage.setItem('media-server-quality-pref', String(height))
            } catch { /* storage full */ }
        }
    }, [user?.preferences?.default_quality])

    // Sync playlist store index when navigating to a media ID that's in the playlist
    useEffect(() => {
        if (!mediaId || currentPlaylist.length === 0) return
        const idx = currentPlaylist.findIndex(i => i.media_id === mediaId)
        if (idx !== -1 && idx !== currentIndex) setCurrentIndex(idx)
    }, [mediaId, currentPlaylist, currentIndex, setCurrentIndex])

    const handleRate = useCallback((rating: number) => {
        setUserRating(rating)
        if (mediaId) {
            ratingsApi.record(mediaId, rating).catch(() => {
            })
        }
    }, [mediaId])

    // Controls visibility timeout
    const controlsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    // Double-click fullscreen tracking
    const lastClickTimeRef = useRef(0)
    // Fetch media item — mediaApi.get() handles URL encoding internally; do not pre-encode
    const {data: media, isLoading: mediaLoading, error: mediaError} = useQuery({
        queryKey: ['media-item', mediaId],
        queryFn: () => mediaApi.get(mediaId),
        enabled: !!mediaId,
        // Retry aggressively on 503 (server initializing — media scan in progress)
        // so users don't see a false "not found" error during startup.
        retry: (failureCount, error) => {
            if (error instanceof ApiError && error.status === 503) return failureCount < 5
            return failureCount < 1
        },
        retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 10000),
    })

    // Stable fallback callback — must not be recreated on every render or
    // useHLS will tear down and rebuild the HLS instance on every time-update.
    const onHlsFallback = useCallback(() => { setActiveHlsUrl(null); }, [])

    // Attach hls.js for video HLS playback when a stream URL is available
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
    // Surface hls.js errors via toast so users aren't left with a silent black screen
    useEffect(() => {
        if (hlsError) showToast(hlsError, 'error')
    }, [hlsError, showToast])

    // Mark audio element as ready after mount so the EQ hook gets the real DOM node (defer to avoid setState-in-effect lint)
    useEffect(() => {
        if (audioRef.current) queueMicrotask(() => { setAudioReady(true); })
    }, [])

    // Wire equalizer to the audio element (EQ only applies to audio content, not video)
    useEqualizer(audioRef, audioReady && media?.type === 'audio')

    // Fetch similar media via suggestions engine (semantic similarity by category/tags/type)
    const {
        data: similarData = [],
        isLoading: relatedLoading,
        isError: similarError,
        refetch: similarRefetch,
    } = useQuery({
        queryKey: ['media-similar', mediaId, canViewMature],
        queryFn: () => suggestionsApi.getSimilar(mediaId ?? ''),
        enabled: !!mediaId,
        // Retry on 503 (suggestions catalogue not seeded yet during startup)
        retry: (failureCount, error) => {
            if (error instanceof ApiError && error.status === 503) return failureCount < 5
            return failureCount < 1
        },
        retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 10000),
        select: data => (data ?? []).slice(0, 8),
    })

    // Fallback: when similar returns empty (e.g. small library), show trending so sidebar is never blank
    const {data: trendingData = [], isLoading: trendingLoading} = useQuery({
        queryKey: ['suggestions-trending', canViewMature],
        queryFn: () => suggestionsApi.getTrending(),
        enabled: !!mediaId && !relatedLoading && !similarError && similarData.length === 0,
        staleTime: 60 * 1000,
        select: data => (data ?? []).slice(0, 8),
    })

    const useFallback = similarData.length === 0 && !similarError && !relatedLoading
    const related = similarData.length > 0 ? similarData : trendingData
    const relatedLabel = similarData.length > 0 ? 'Similar Media' : 'More to Explore'
    const relatedStillLoading = relatedLoading || (useFallback && trendingLoading)

    // Check mature content
    useEffect(() => {
        if (media?.is_mature && !matureAccepted) {
            const pref = user?.preferences?.show_mature
            if (!pref) {
                setShowMatureWarning(true)
            } else {
                setMatureAccepted(true)
            }
        }
    }, [media, user, matureAccepted])

    // Load media when path changes
    useEffect(() => {
        if (!mediaId || !media) return
        if (media.is_mature && !matureAccepted) return

        const el = media.type === 'video' ? videoRef.current : audioRef.current
        if (!el) return

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

        el.src = mediaApi.getStreamUrl(mediaId)
        el.volume = volume
        el.loop = isLooping
        el.playbackRate = playbackRate

        // Fetch saved position so handleLoadedMetadata can seek to it (only if user enabled resume)
        resumePositionRef.current = 0
        let positionFetchCancelled = false
        const resumeEnabled = user?.preferences?.resume_playback !== false
        if (user && resumeEnabled) {
            const elRef = el
            watchHistoryApi.getPosition(mediaId)
                .then(data => {
                    if (positionFetchCancelled) return
                    const pos = data?.position ?? 0
                    resumePositionRef.current = pos
                    if (pos > 5 && elRef.readyState >= 1 && elRef.duration > 0
                        && pos < elRef.duration - 5 && elRef.currentTime < 2) {
                        elRef.currentTime = pos
                    }
                })
                .catch(() => {
                })
        }
        // Track analytics
        analyticsApi.trackEvent({type: 'view', media_id: media.id}).catch(() => {
        })

        return () => {
            positionFetchCancelled = true
        }
    }, [mediaId, media, matureAccepted]) // eslint-disable-line react-hooks/exhaustive-deps
    // Check HLS availability
    useEffect(() => {
        if (!mediaId || media?.type !== 'video' || !hlsEnabled) return
        hlsApi.check(mediaId).then(hls => {
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
        }).catch(() => {
        })
    }, [mediaId, media])

    // HLS polling
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
        return () => { clearInterval(interval); }
    }, [hlsPolling, hlsJob])
    // Restore direct stream when HLS becomes inactive
    useEffect(() => {
        if (activeHlsUrl !== null) return
        if (!mediaId || !media) return
        if (media.is_mature && !matureAccepted) return
        const el = media.type === 'video' ? videoRef.current : audioRef.current
        if (!el) return
        if (!el.src || el.src === '' || el.src === window.location.href) {
            el.src = mediaApi.getStreamUrl(mediaId)
        }
    }, [activeHlsUrl, mediaId, media, matureAccepted])
    // Derived media type
    const isAudio = media?.type === 'audio'
    const isVideo = media?.type === 'video'
    // Controls auto-hide (video only)
    const resetControlsTimer = useCallback(() => {
        if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        setShowControls(true)
        controlsTimerRef.current = setTimeout(() => {
            if (isPlayingRef.current) setShowControls(false)
        }, 3000)
    }, [])

    useEffect(() => {
        return () => {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
        }
    }, [])
    useEffect(() => {
        if (!isVideo) return
        isPlayingRef.current = isPlaying
        if (isPlaying) {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
            controlsTimerRef.current = setTimeout(() => { setShowControls(false); }, 3000)
        } else {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
            setShowControls(true)
        }
    }, [isPlaying, isVideo])
    // Save position when page is hidden
    useEffect(() => {
        if (!mediaId) return

        function handleVisibilityChange() {
            if (document.visibilityState === 'hidden' && currentTimeRef.current > 0 && durationRef.current > 0) {
                watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
                })
            }
        }

        document.addEventListener('visibilitychange', handleVisibilityChange)
        return () => { document.removeEventListener('visibilitychange', handleVisibilityChange); }
    }, [mediaId])

    function getActiveEl() {
        if (!media) return null
        return media.type === 'video' ? videoRef.current : audioRef.current
    }

    // Sync playback speed from user preferences when they change
    useEffect(() => {
        const pref = user?.preferences?.playback_speed
        if (pref == null || pref < 0.25 || pref > 2) return
        setPlaybackRate(pref)
        const el = getActiveEl()
        if (el) el.playbackRate = pref
    }, [user?.preferences?.playback_speed, media?.type])

    function togglePlay() {
        const el = getActiveEl()
        if (!el) return
        if (el.paused) {
            el.play().catch(() => {
            })
        } else {
            el.pause()
        }
    }

    function handlePrevTrack() {
        const prevId = playPrevious()
        if (prevId) navigate(`/player?id=${encodeURIComponent(prevId)}`, {replace: true})
    }

    function handleNextTrack() {
        const nextId = playNext()
        if (nextId) navigate(`/player?id=${encodeURIComponent(nextId)}`, {replace: true})
    }

    const hasPlaylist = currentPlaylist.length > 1

    function handleVideoClick() {
        // eslint-disable-next-line react-hooks/purity -- event handler, not render; Date.now() is intentional
        const now = Date.now()
        if (now - lastClickTimeRef.current < 300) {
            handleFullscreen()
            lastClickTimeRef.current = 0
            return
        }
        lastClickTimeRef.current = now
        setTimeout(() => {
            if (lastClickTimeRef.current === now) {
                togglePlay()
            }
        }, 300)
    }

    const fireAnalytics = useCallback((type: string, data?: Record<string, unknown>) => {
        if (!mediaId) return
        analyticsApi.trackEvent({ type, media_id: mediaId, data }).catch(() => {})
    }, [mediaId])

    function handleTimeUpdate() {
        const el = getActiveEl()
        if (!el) return
        currentTimeRef.current = el.currentTime
        setCurrentTime(el.currentTime)
        if (el.buffered.length > 0) {
            setBuffered((el.buffered.end(el.buffered.length - 1) / el.duration) * 100)
        }
    }

    function handleLoadedMetadata() {
        const el = getActiveEl()
        if (el) {
            durationRef.current = el.duration
            setDuration(el.duration)
            setIsLoading(false)
            const resumeEnabled = user?.preferences?.resume_playback !== false
            const saved = resumePositionRef.current
            if (resumeEnabled && saved > 5 && el.duration > 0 && saved < el.duration - 5) {
                el.currentTime = saved
            }
            resumePositionRef.current = 0
            el.play().catch(() => {
            })
        }
    }

    function handlePause() {
        setIsPlaying(false)
        if (mediaId && currentTimeRef.current > 0 && durationRef.current > 0) {
            watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
            })
        }
        fireAnalytics('pause', {
            position: currentTimeRef.current,
            duration: durationRef.current,
        })
    }
    function handleDurationChange() {
        const el = getActiveEl()
        if (el && isFinite(el.duration) && el.duration > 0) {
            durationRef.current = el.duration
            setDuration(el.duration)
        }
    }

    function handleProgressClick(e: React.MouseEvent<HTMLDivElement>) {
        const el = getActiveEl()
        if (!el || !duration) return
        const rect = e.currentTarget.getBoundingClientRect()
        const ratio = (e.clientX - rect.left) / rect.width
        el.currentTime = ratio * duration
    }
    function handleProgressTouch(e: React.TouchEvent<HTMLDivElement>) {
        e.preventDefault()
        const touch = e.touches[0] ?? e.changedTouches[0]
        if (!touch) return
        const rect = e.currentTarget.getBoundingClientRect()
        const ratio = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width))
        const el = getActiveEl()
        if (!el || !duration) return
        el.currentTime = ratio * duration
    }
    function handleProgressHover(e: React.MouseEvent<HTMLDivElement>) {
        if (!duration) return
        const rect = e.currentTarget.getBoundingClientRect()
        const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
        setHoverTime(ratio * duration)
        setHoverPos(e.clientX - rect.left)
    }
    function handleProgressLeave() {
        setHoverTime(null)
    }
    function handleSeeked() {
        if (mediaId && currentTimeRef.current > 5 && durationRef.current > 0) {
            watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
            })
        }
        fireAnalytics('seek', {
            position: currentTimeRef.current,
            duration: durationRef.current,
        })
    }

    function handleVolumeChange(e: React.ChangeEvent<HTMLInputElement>) {
        const v = parseFloat(e.target.value)
        setVolume(v)
        const el = getActiveEl()
        if (el) el.volume = v
    }

    function toggleMute() {
        const el = getActiveEl()
        if (!el) return
        el.muted = !el.muted
        setIsMuted(el.muted)
    }

    function toggleLoop() {
        const el = getActiveEl()
        const newLoop = !isLooping
        setIsLooping(newLoop)
        if (el) el.loop = newLoop
    }

    function setSpeed(speed: number) {
        const el = getActiveEl()
        setPlaybackRate(speed)
        if (el) el.playbackRate = speed
    }

    function handleFullscreen() {
        const wrapper = videoRef.current?.parentElement
        if (!wrapper) return
        if (document.fullscreenElement) {
            document.exitFullscreen().catch(() => {
            })
        } else {
            wrapper.requestFullscreen().catch(() => {
            })
        }
    }

    const handlePlay = useCallback(() => {
        setIsPlaying(true)
        fireAnalytics('play', durationRef.current > 0 ? { duration: durationRef.current } : undefined)
    }, [fireAnalytics])

    const handleWaiting = useCallback(() => {
        setIsLoading(true)
        fireAnalytics('buffering', { state: 'start' })
    }, [fireAnalytics])

    const handleCanPlay = useCallback(() => {
        setIsLoading(false)
        fireAnalytics('buffering', { state: 'end' })
    }, [fireAnalytics])

    const handleSelectQualityWithAnalytics = useCallback((index: number) => {
        selectQuality(index)
        const name = index === -1
            ? 'Auto'
            : hlsQualities.find(q => q.index === index)?.name ?? String(index)
        fireAnalytics('quality_change', { quality_index: index, quality_name: name })
    }, [selectQuality, hlsQualities, fireAnalytics])

    function handlePiP() {
        const vid = videoRef.current
        if (!vid) return
        if (document.pictureInPictureElement) {
            document.exitPictureInPicture().catch(() => {
            })
        } else {
            vid.requestPictureInPicture().catch(() => {
            })
        }
    }

    function handleEnded() {
        if (mediaId) {
            watchHistoryApi.trackPosition(mediaId, duration, duration).catch(() => {
            })
            fireAnalytics('complete', { duration, position: duration })
        }
        // Auto-advance to next track only when user has autoplay enabled
        const autoPlayNext = user?.preferences?.auto_play === true
        const nextId = autoPlayNext ? playNext() : null
        if (nextId) {
            navigate(`/player?id=${encodeURIComponent(nextId)}`, {replace: true})
        }
    }
    // Fire fullscreen analytics when user toggles fullscreen (e.g. Escape)
    useEffect(() => {
        function onFullscreenChange() {
            if (mediaId) {
                fireAnalytics('fullscreen', { active: !!document.fullscreenElement })
            }
        }
        document.addEventListener('fullscreenchange', onFullscreenChange)
        return () => { document.removeEventListener('fullscreenchange', onFullscreenChange); }
    }, [mediaId, fireAnalytics])

    // Save position periodically
    useEffect(() => {
        if (!mediaId || !isPlaying || !duration) return
        const interval = setInterval(() => {
            if (durationRef.current > 0 && currentTimeRef.current > 0) {
                watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
                })
            }
        }, 30000)
        return () => { clearInterval(interval); }
    }, [mediaId, isPlaying, duration])
    // Keyboard shortcuts
    useEffect(() => {
        function onKeyDown(e: KeyboardEvent) {
            if ((e.target as HTMLElement).tagName === 'INPUT' || (e.target as HTMLElement).tagName === 'TEXTAREA' || (e.target as HTMLElement).tagName === 'SELECT') return
            const el = getActiveEl()
            switch (e.key) {
                case ' ':
                case 'k':
                case 'K':
                    e.preventDefault()
                    if (el) {
                        if (el.paused) el.play().catch(() => {})
                        else el.pause()
                    }
                    break
                case 'j':
                case 'J':
                    e.preventDefault()
                    if (el) el.currentTime = Math.max(0, el.currentTime - 10)
                    break
                case 'l':
                case 'L':
                    e.preventDefault()
                    if (el) el.currentTime = Math.min(el.duration, el.currentTime + 10)
                    break
                case 'ArrowLeft':
                    e.preventDefault()
                    if (el) el.currentTime = Math.max(0, el.currentTime - 5)
                    break
                case 'ArrowRight':
                    e.preventDefault()
                    if (el) el.currentTime = Math.min(el.duration, el.currentTime + 5)
                    break
                case 'ArrowUp':
                    e.preventDefault()
                    if (el) {
                        el.volume = Math.min(1, el.volume + 0.05)
                        setVolume(el.volume)
                    }
                    break
                case 'ArrowDown':
                    e.preventDefault()
                    if (el) {
                        el.volume = Math.max(0, el.volume - 0.05)
                        setVolume(el.volume)
                    }
                    break
                case 'm':
                case 'M':
                    e.preventDefault()
                    if (el) {
                        el.muted = !el.muted
                        setIsMuted(el.muted)
                    }
                    break
                case 'f':
                case 'F':
                    e.preventDefault()
                    handleFullscreen()
                    break
                case 't':
                case 'T':
                    e.preventDefault()
                    setTheaterMode(t => !t)
                    break
                case 'Escape':
                    if (showSettings) {
                        e.preventDefault()
                        setShowSettings(false)
                    }
                    break
                // Number keys 0-9: seek to percentage
                case '0': case '1': case '2': case '3': case '4':
                case '5': case '6': case '7': case '8': case '9':
                    e.preventDefault()
                    if (el && el.duration) {
                        el.currentTime = (parseInt(e.key) / 10) * el.duration
                    }
                    break
                case ',':
                    // Previous frame (when paused)
                    if (el && el.paused) {
                        e.preventDefault()
                        el.currentTime = Math.max(0, el.currentTime - (1 / 30))
                    }
                    break
                case '.':
                    // Next frame (when paused)
                    if (el && el.paused) {
                        e.preventDefault()
                        el.currentTime = Math.min(el.duration, el.currentTime + (1 / 30))
                    }
                    break
                case '<':
                    e.preventDefault()
                    setSpeed(Math.max(0.25, playbackRate - 0.25))
                    break
                case '>':
                    e.preventDefault()
                    setSpeed(Math.min(2, playbackRate + 0.25))
                    break
            }
        }

        document.addEventListener('keydown', onKeyDown)
        return () => { document.removeEventListener('keydown', onKeyDown); }
    }, [media?.type, showSettings, playbackRate])
    const progress = duration > 0 ? (currentTime / duration) * 100 : 0
    // Quality badge for the controls bar
    const qualityBadge = (() => {
        if (hlsQualities.length === 0) return null
        if (currentQuality === -1) {
            if (autoLevel >= 0 && hlsQualities[autoLevel]) return hlsQualities[autoLevel].name
            return null
        }
        const q = hlsQualities.find(q => q.index === currentQuality)
        return q?.name ?? null
    })()
    if (!mediaId) {
        return (
            <div className="player-page">
                <div className="player-page-container">
                    <div className="player-header">
                        <Link to="/" className="player-back-btn"><i className="bi bi-arrow-left"/> Back to
                            Library</Link>
                    </div>
                    <p style={{color: 'var(--text-muted)', textAlign: 'center', padding: '40px 0'}}>
                        No media ID specified. <Link to="/">Go to library</Link>.
                    </p>
                </div>
            </div>
        )
    }

    if (mediaLoading) {
        return (
            <div className="player-page">
                <div className="player-page-container">
                    <div className="player-header">
                        <Link to="/" className="player-back-btn"><i className="bi bi-arrow-left"/> Back to
                            Library</Link>
                    </div>
                    <div style={{textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)'}}>
                        Loading media...
                    </div>
                </div>
            </div>
        )
    }

    if (mediaError || !media) {
        const is403 = mediaError instanceof ApiError && mediaError.status === 403
        const errMsg = mediaError instanceof ApiError ? mediaError.message : ''
        const playerUrl = `/player?id=${encodeURIComponent(mediaId)}`

        return (
            <div className="player-page">
                <div className="player-page-container">
                    <div className="player-header">
                        <Link to="/" className="player-back-btn"><i className="bi bi-arrow-left"/> Back to
                            Library</Link>
                    </div>
                    <div style={{textAlign: 'center', padding: '60px 0'}}>
                        {is403 && errMsg.includes('log in') ? (
                            <>
                                <p style={{color: '#ef4444', marginBottom: 12}}>
                                    <i className="bi bi-shield-lock-fill"/> This content is marked as mature (18+).
                                </p>
                                <Link to={`/login?redirect=${encodeURIComponent(playerUrl)}`}>
                                    Sign in to view this content
                                </Link>
                            </>
                        ) : is403 && errMsg.includes('permission') ? (
                            <>
                                <p style={{color: '#ef4444', marginBottom: 12}}>
                                    <i className="bi bi-shield-lock-fill"/> Your account does not have permission to view mature content.
                                </p>
                                <p style={{color: 'var(--text-muted)', fontSize: 14}}>Contact an administrator to request access.</p>
                                <Link to="/" style={{marginTop: 12, display: 'inline-block'}}><i className="bi bi-arrow-left"/> Back to Library</Link>
                            </>
                        ) : is403 && (errMsg.includes('Enable') || errMsg.includes('preferences')) ? (
                            <>
                                <p style={{color: '#ef4444', marginBottom: 12}}>
                                    <i className="bi bi-shield-lock-fill"/> This content is marked as mature (18+).
                                </p>
                                <Link to={`/profile?mature_redirect=${encodeURIComponent(playerUrl)}`}>
                                    Enable mature content in profile settings
                                </Link>
                            </>
                        ) : (
                            <>
                                <p style={{color: '#ef4444', marginBottom: 12}}>Media not found or unavailable.</p>
                                <Link to="/"><i className="bi bi-arrow-left"/> Back to Library</Link>
                            </>
                        )}
                    </div>
                </div>
            </div>
        )
    }

    return (
        <div className="player-page">
            {/* Mature content warning */}
            {showMatureWarning && (
                <div className="mature-modal-overlay">
                    <div className="mature-modal-box">
                        <div className="mature-modal-header"><i className="bi bi-exclamation-triangle-fill"/> 18+
                            Content Warning
                        </div>
                        <div className="mature-modal-body">
                            <div className="mature-modal-icon"><i className="bi bi-exclamation-triangle-fill"/></div>
                            <h3>Adult Content Ahead</h3>
                            <p>
                                This media has been marked as containing mature/adult content (18+/NSFW).
                                By continuing, you confirm that you are at least 18 years old.
                            </p>
                            <div className="mature-modal-actions">
                                <button
                                    className="media-action-btn media-action-btn-primary"
                                    style={{background: '#dc3545', borderColor: '#dc3545'}}
                                    onClick={() => {
                                        setMatureAccepted(true);
                                        setShowMatureWarning(false)
                                    }}
                                >
                                    <i className="bi bi-check-circle"/> I am 18+, Continue
                                </button>
                                <button className="media-action-btn" onClick={() => { navigate('/'); }}>
                                    <i className="bi bi-arrow-left"/> Go Back
                                </button>
                            </div>
                            <p className="mature-modal-note">
                                You can disable this warning in your <Link to="/profile">profile settings</Link>.
                            </p>
                        </div>
                    </div>
                </div>
            )}

            <div className={`player-page-container ${theaterMode ? 'player-page-container--theater' : ''}`}>
                <div className="player-header">
                    <Link to="/" className="player-back-btn"><i className="bi bi-arrow-left"/> Back to Library</Link>
                    {isVideo && (
                        <button
                            className={`player-theater-btn ${theaterMode ? 'player-theater-btn--active' : ''}`}
                            onClick={() => { setTheaterMode(t => !t); }}
                            title="Theater mode (T)"
                        >
                            <i className={theaterMode ? 'bi bi-arrows-angle-contract' : 'bi bi-arrows-angle-expand'}/>
                        </button>
                    )}
                </div>
                <div className={`player-layout ${theaterMode ? 'player-layout--theater' : ''}`}>
                    {/* Main player column */}
                    <div className="player-main">
                        {/* Video / Audio container */}
                        <div
                            className={`video-wrapper${isVideo && isPlaying && !showControls ? ' playing-idle' : ''}`}
                            onMouseMove={isVideo ? resetControlsTimer : undefined}
                            onMouseLeave={isVideo && isPlaying ? () => { setShowControls(false); } : undefined}
                            onClick={isVideo ? handleVideoClick : undefined}
                        >
                            {/* Hidden audio element for audio type */}
                            {isAudio && (
                                <>
                                    <audio
                                        ref={audioRef}
                                        onTimeUpdate={handleTimeUpdate}
                                        onLoadedMetadata={handleLoadedMetadata}
                                        onDurationChange={handleDurationChange}
                                        onPlay={handlePlay}
                                        onPause={handlePause}
                                        onEnded={handleEnded}
                                        onSeeked={handleSeeked}
                                        onWaiting={handleWaiting}
                                        onCanPlay={handleCanPlay}
                                        preload="auto"
                                    />
                                    <div className="audio-visualizer">
                                        <div className="audio-visualizer-icon"><i className="bi bi-music-note-beamed"/>
                                        </div>
                                        <div className="audio-visualizer-title">{formatTitle(media.name)}</div>
                                    </div>
                                </>
                            )}

                            {/* Video element */}
                            {isVideo && (
                                <video
                                    ref={videoRef}
                                    onTimeUpdate={handleTimeUpdate}
                                    onLoadedMetadata={handleLoadedMetadata}
                                    onDurationChange={handleDurationChange}
                                    onPlay={handlePlay}
                                    onPause={handlePause}
                                    onEnded={handleEnded}
                                    onSeeked={handleSeeked}
                                    onWaiting={handleWaiting}
                                    onCanPlay={handleCanPlay}
                                    preload="auto"
                                />
                            )}
                            {/* Loading spinner */}
                            {(isLoading || hlsIsLoading) && (
                                <div className="player-loading">
                                    <div className="player-loading-spinner"/>
                                </div>
                            )}

                            {/* Custom controls */}
                            <div
                                className={`custom-controls ${showControls || !isPlaying ? 'show' : ''}`}
                                onClick={e => { e.stopPropagation(); }}
                            >
                                {/* Progress bar */}
                                <div
                                    className="ctrl-progress-container"
                                    onClick={handleProgressClick}
                                    onMouseMove={handleProgressHover}
                                    onMouseLeave={handleProgressLeave}
                                    onTouchStart={handleProgressTouch}
                                    onTouchMove={handleProgressTouch}
                                >
                                    <div className="ctrl-buffer-bar" style={{width: `${buffered}%`}}/>
                                    <div className="ctrl-progress-fill" style={{width: `${progress}%`}}/>
                                    {/* Time tooltip on hover */}
                                    {hoverTime !== null && (
                                        <div
                                            className="ctrl-progress-tooltip"
                                            style={{left: `${hoverPos}px`}}
                                        >
                                            {formatDuration(hoverTime)}
                                        </div>
                                    )}
                                </div>

                                {/* Controls row */}
                                <div className="ctrl-row">
                                    {hasPlaylist && (
                                        <button className="ctrl-btn" onClick={handlePrevTrack} title="Previous track">
                                            <i className="bi bi-skip-start-fill"/>
                                        </button>
                                    )}
                                    <button className="ctrl-btn" onClick={togglePlay} title="Play/Pause (K)">
                                        {isPlaying ? <i className="bi bi-pause-fill"/> :
                                            <i className="bi bi-play-fill"/>}
                                    </button>
                                    {hasPlaylist && (
                                        <button className="ctrl-btn" onClick={handleNextTrack} title="Next track">
                                            <i className="bi bi-skip-end-fill"/>
                                        </button>
                                    )}
                                    <button className="ctrl-btn" onClick={() => {
                                        const el = getActiveEl();
                                        if (el) el.currentTime = Math.max(0, el.currentTime - 10)
                                    }} title="Back 10s (J)">
                                        <i className="bi bi-skip-backward-fill"/>
                                        <span className="ctrl-btn-label">10</span>
                                    </button>
                                    <button className="ctrl-btn" onClick={() => {
                                        const el = getActiveEl();
                                        if (el) el.currentTime = Math.min(el.duration, el.currentTime + 10)
                                    }} title="Forward 10s (L)">
                                        <span className="ctrl-btn-label">10</span>
                                        <i className="bi bi-skip-forward-fill"/>
                                    </button>
                                    <div className="ctrl-volume-wrapper">
                                        <button className="ctrl-btn" onClick={toggleMute} title="Mute (M)">
                                            {isMuted || volume === 0 ?
                                                <i className="bi bi-volume-mute-fill"/> : volume < 0.5 ?
                                                    <i className="bi bi-volume-down-fill"/> :
                                                    <i className="bi bi-volume-up-fill"/>}
                                        </button>
                                        <input
                                            type="range"
                                            className="ctrl-volume-slider"
                                            min="0" max="1" step="0.05"
                                            value={isMuted ? 0 : volume}
                                            onChange={handleVolumeChange}
                                            onClick={e => { e.stopPropagation(); }}
                                        />
                                    </div>
                                    <span
                                        className="ctrl-time">{formatDuration(currentTime)} / {formatDuration(duration)}</span>
                                    <div className="ctrl-spacer"/>
                                    {/* Quality badge indicator */}
                                    {qualityBadge && (
                                        <span className="ctrl-quality-badge" title="Current quality">
                                            {qualityBadge}
                                        </span>
                                    )}
                                    {/* Speed indicator (only shows when not 1x) */}
                                    {playbackRate !== 1 && (
                                        <span className="ctrl-speed-badge" title="Playback speed">
                                            {playbackRate}x
                                        </span>
                                    )}
                                    {/* Settings gear — opens unified panel */}
                                    <div className="ctrl-settings-wrapper">
                                        <button
                                            className={`ctrl-btn ${showSettings ? 'active' : ''}`}
                                            onClick={() => { setShowSettings(s => !s); }}
                                            title="Settings"
                                        >
                                            <i className={`bi bi-gear-fill ${showSettings ? 'ctrl-gear-spin' : ''}`}/>
                                        </button>
                                        {showSettings && (
                                            <PlayerSettingsPanel
                                                qualities={hlsQualities}
                                                currentQuality={currentQuality}
                                                autoLevel={autoLevel}
                                                onSelectQuality={handleSelectQualityWithAnalytics}
                                                playbackRate={playbackRate}
                                                onSetSpeed={setSpeed}
                                                isLooping={isLooping}
                                                onToggleLoop={toggleLoop}
                                                showPiP={isVideo}
                                                onPiP={handlePiP}
                                                bandwidth={bandwidth}
                                                onClose={() => { setShowSettings(false); }}
                                            />
                                        )}
                                    </div>
                                    {isVideo && (
                                        <button className="ctrl-btn" onClick={handleFullscreen}
                                                title="Fullscreen (F)">
                                            <i className="bi bi-fullscreen"/>
                                        </button>
                                    )}
                                </div>
                            </div>
                        </div>

                        {/* Audio controls (for audio type, separate from video overlay) */}
                        {isAudio && (
                            <div style={{
                                background: 'var(--card-bg)',
                                border: '1px solid var(--border-color)',
                                borderRadius: 10,
                                padding: '12px 16px',
                            }}>
                                {/* Seek bar with touch drag support */}
                                <div style={{marginBottom: 10}}>
                                    <div
                                        className="ctrl-progress-container audio-progress"
                                        onClick={handleProgressClick}
                                        onMouseMove={handleProgressHover}
                                        onMouseLeave={handleProgressLeave}
                                        onTouchStart={handleProgressTouch}
                                        onTouchMove={handleProgressTouch}
                                    >
                                        <div className="ctrl-progress-fill"
                                             style={{width: `${progress}%`, background: '#667eea'}}/>
                                        {hoverTime !== null && (
                                            <div
                                                className="ctrl-progress-tooltip ctrl-progress-tooltip--audio"
                                                style={{left: `${hoverPos}px`}}
                                            >
                                                {formatDuration(hoverTime)}
                                            </div>
                                        )}
                                    </div>
                                </div>
                                {/* Controls row */}
                                <div style={{display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap'}}>
                                    {hasPlaylist && (
                                        <button className="ctrl-btn" style={{color: 'var(--text-color)'}}
                                                onClick={handlePrevTrack} title="Previous track">
                                            <i className="bi bi-skip-start-fill"/>
                                        </button>
                                    )}
                                    <button className="ctrl-btn" style={{color: 'var(--text-color)'}}
                                            onClick={() => {
                                                const el = getActiveEl()
                                                if (el) el.currentTime = Math.max(0, el.currentTime - 10)
                                            }} title="Back 10s">
                                        <i className="bi bi-skip-backward-fill"/>
                                    </button>
                                    <button className="ctrl-btn audio-play-btn" style={{color: 'var(--text-color)'}}
                                            onClick={togglePlay} title="Play/Pause (Space)">
                                        {isPlaying ? <i className="bi bi-pause-fill"/> :
                                            <i className="bi bi-play-fill"/>}
                                    </button>
                                    <button className="ctrl-btn" style={{color: 'var(--text-color)'}}
                                            onClick={() => {
                                                const el = getActiveEl()
                                                if (el) el.currentTime = Math.min(el.duration, el.currentTime + 10)
                                            }} title="Forward 10s">
                                        <i className="bi bi-skip-forward-fill"/>
                                    </button>
                                    {hasPlaylist && (
                                        <button className="ctrl-btn" style={{color: 'var(--text-color)'}}
                                                onClick={handleNextTrack} title="Next track">
                                            <i className="bi bi-skip-end-fill"/>
                                        </button>
                                    )}
                                    <span style={{
                                        fontSize: 12,
                                        color: 'var(--text-muted)',
                                        fontFamily: 'monospace',
                                        whiteSpace: 'nowrap',
                                        marginLeft: 4,
                                    }}>
                                        {formatDuration(currentTime)} / {formatDuration(duration)}
                                    </span>
                                    <div style={{flex: 1}}/>
                                    <button
                                        className={`ctrl-btn ${isLooping ? 'active' : ''}`}
                                        style={{color: isLooping ? '#667eea' : 'var(--text-color)', fontSize: 15}}
                                        onClick={toggleLoop} title="Loop">
                                        <i className="bi bi-repeat"/>
                                    </button>
                                    {/* Speed cycle button */}
                                    <button
                                        className="ctrl-btn"
                                        style={{
                                            color: playbackRate !== 1 ? '#667eea' : 'var(--text-color)',
                                            fontSize: 11,
                                            fontFamily: 'monospace',
                                            minWidth: 38,
                                            fontWeight: 600,
                                        }}
                                        onClick={() => {
                                            const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]
                                            const idx = speeds.indexOf(playbackRate)
                                            const next = speeds[(idx + 1) % speeds.length]
                                            setPlaybackRate(next)
                                            const el = getActiveEl()
                                            if (el) el.playbackRate = next
                                        }}
                                        title="Playback speed"
                                    >
                                        {playbackRate}x
                                    </button>
                                    <button className="ctrl-btn" style={{color: 'var(--text-color)'}}
                                            onClick={toggleMute} title="Mute (M)">
                                        {isMuted || volume === 0 ? <i className="bi bi-volume-mute-fill"/> :
                                            volume < 0.5 ? <i className="bi bi-volume-down-fill"/> :
                                                <i className="bi bi-volume-up-fill"/>}
                                    </button>
                                    <input
                                        type="range" min="0" max="1" step="0.05"
                                        value={isMuted ? 0 : volume}
                                        onChange={handleVolumeChange}
                                        style={{width: 70, cursor: 'pointer', accentColor: '#667eea'}}
                                    />
                                </div>
                            </div>
                        )}

                        {/* HLS available — user opt-in banner */}
                        {hlsAvailable && hlsReadyUrl && !activeHlsUrl && (
                            <div className="hls-available-banner">
                                <span><i className="bi bi-lightning-fill"/> HLS adaptive stream ready</span>
                                <div className="hls-banner-actions">
                                    <button
                                        className="hls-switch-btn"
                                        onClick={() => {
                                            setActiveHlsUrl(hlsReadyUrl);
                                            setHlsAvailable(false)
                                        }}
                                    >
                                        <i className="bi bi-play-circle"/> Switch to HLS
                                    </button>
                                    <button
                                        className="hls-dismiss-btn"
                                        onClick={() => { setHlsAvailable(false); }}
                                    >
                                        Dismiss
                                    </button>
                                </div>
                            </div>
                        )}
                        {/* HLS progress indicator */}
                        {hlsJob && hlsJob.status === 'running' && (
                            <div className="hls-progress-wrapper">
                                <div style={{color: 'var(--text-muted)', fontSize: 13}}>
                                    <i className="bi bi-arrow-repeat"/> Generating HLS adaptive stream...
                                </div>
                                <div className="hls-bar-bg">
                                    <div className="hls-bar-fill" style={{width: `${hlsJob.progress}%`}}/>
                                </div>
                                <div style={{fontSize: 12, color: 'var(--text-muted)'}}>{hlsJob.progress}% complete
                                </div>
                            </div>
                        )}

                        {/* Media info */}
                        <div className="media-info-card">
                            <h1 className="media-page-title">{formatTitle(media.name)}</h1>
                            <div className="media-page-stats">
                                <span><i className="bi bi-eye"/> {media.views} views</span>
                                {media.date_added && <span><i
                                    className="bi bi-calendar3"/> {new Date(media.date_added).toLocaleDateString()}</span>}
                                <span><i className="bi bi-hdd-fill"/> {formatFileSize(media.size)}</span>
                                <span><i className="bi bi-clock"/> {formatDuration(media.duration)}</span>
                                {media.width && media.height &&
                                    <span><i className="bi bi-aspect-ratio"/> {media.width}x{media.height}</span>}
                                {media.container && <span><i className="bi bi-file-play"/> {media.container}</span>}
                            </div>
                            <div className="media-action-buttons">
                                {permissions.can_download && (
                                    <a href={mediaApi.getDownloadUrl(media.id)} className="media-action-btn">
                                        <i className="bi bi-download"/> Download
                                    </a>
                                )}
                                <button
                                    className="media-action-btn"
                                    onClick={() => {
                                        const url = `${window.location.origin}/player?id=${encodeURIComponent(media.id)}`
                                        navigator.clipboard.writeText(url).then(
                                            () => showToast('Link copied to clipboard', 'success'),
                                            () => showToast('Could not copy link', 'error'),
                                        )
                                    }}
                                >
                                    <i className="bi bi-share-fill"/> Share
                                </button>
                            </div>
                            {media.category && (
                                <div className="media-category">
                                    <strong>Category:</strong> {media.category}
                                    {media.is_mature && <span className="media-card-type-badge badge-mature"
                                                              style={{marginLeft: 8}}>18+</span>}
                                </div>
                            )}
                        </div>
                    </div>

                    {/* Sidebar */}
                    <SectionErrorBoundary title="Sidebar unavailable">
                        <div className="player-sidebar">
                            <div className="player-sidebar-card">
                                <h3><i className="bi bi-play-fill"/> {relatedLabel}</h3>
                                {relatedStillLoading ? (
                                    <p style={{color: 'var(--text-muted)', fontSize: 13}}>Loading…</p>
                                ) : similarError ? (
                                    <p style={{color: 'var(--text-muted)', fontSize: 13}}>
                                        Suggestions still loading.{' '}
                                        <button type="button" className="media-action-btn" style={{marginTop: 4}} onClick={() => similarRefetch()}>
                                            Retry
                                        </button>
                                    </p>
                                ) : related.length === 0 ? (
                                    <p style={{color: 'var(--text-muted)', fontSize: 13}}>No suggestions yet. Add more media to your library.</p>
                                ) : (
                                    related.map(entry => <SimilarItem key={entry.media_id} entry={entry} canViewMature={canViewMature}/>)
                                )}
                            </div>

                            {/* Star Rating */}
                            {user && (
                                <div className="player-sidebar-card">
                                    <h3><i className="bi bi-star-fill"/> Rate This</h3>
                                    <div style={{display: 'flex', gap: 4, marginTop: 4}}>
                                        {[1, 2, 3, 4, 5].map(star => (
                                            <button
                                                key={star}
                                                onClick={() => { handleRate(star); }}
                                                onMouseEnter={() => { setRatingHover(star); }}
                                                onMouseLeave={() => { setRatingHover(0); }}
                                                style={{
                                                    background: 'none',
                                                    border: 'none',
                                                    cursor: 'pointer',
                                                    fontSize: 22,
                                                    padding: '2px 3px',
                                                    color: star <= (ratingHover || userRating) ? '#f59e0b' : 'var(--text-muted)',
                                                    transition: 'color 0.15s',
                                                }}
                                                title={`Rate ${star} star${star !== 1 ? 's' : ''}`}
                                            >
                                                <i className={`bi bi-star${star <= (ratingHover || userRating) ? '-fill' : ''}`}/>
                                            </button>
                                        ))}
                                    </div>
                                    {userRating > 0 && (
                                        <p style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 4}}>
                                            You rated this {userRating}/5
                                        </p>
                                    )}
                                </div>
                            )}

                            {/* Keyboard shortcuts card */}
                            <div className="player-sidebar-card player-shortcuts-card">
                                <h3><i className="bi bi-keyboard"/> Shortcuts</h3>
                                <div className="player-shortcuts-grid">
                                    <kbd>K</kbd> <span>Play/Pause</span>
                                    <kbd>J</kbd> <span>Back 10s</span>
                                    <kbd>L</kbd> <span>Forward 10s</span>
                                    <kbd>F</kbd> <span>Fullscreen</span>
                                    <kbd>T</kbd> <span>Theater</span>
                                    <kbd>M</kbd> <span>Mute</span>
                                    <kbd>0-9</kbd> <span>Seek %</span>
                                    <kbd>&lt; &gt;</kbd> <span>Speed</span>
                                </div>
                            </div>
                        </div>
                    </SectionErrorBoundary>
                </div>
            </div>
        </div>
    )
}
