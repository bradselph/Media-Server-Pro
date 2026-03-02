import {useCallback, useEffect, useRef, useState} from 'react'
import {Link, useNavigate, useSearchParams} from 'react-router-dom'
import {useQuery} from '@tanstack/react-query'
import {useAuthStore} from '@/stores/authStore'
import {useToast} from '@/components/Toast'
import {SectionErrorBoundary} from '@/components/ErrorBoundary'
import {useHLS} from '@/hooks/useHLS'
import {useSettingsStore} from '@/stores/settingsStore'
import {useEqualizer} from '@/hooks/useEqualizer'
import {analyticsApi, hlsApi, mediaApi, ratingsApi, suggestionsApi, watchHistoryApi} from '@/api/endpoints'
import type {HLSJob, Suggestion} from '@/api/types'
import {formatDuration, formatFileSize, formatTitle} from '@/utils/formatters'
import '@/styles/player.css'

// ── Similar Media Item ────────────────────────────────────────────────────────

function SimilarItem({entry}: { entry: Suggestion }) {
    const name = formatTitle(entry.title || entry.media_id)
    return (
        <Link to={`/player?id=${encodeURIComponent(entry.media_id)}`} className="related-item">
            {entry.thumbnail_url ? (
                <img
                    className="related-thumb"
                    src={entry.thumbnail_url}
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
    const {showToast} = useToast()

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
    const [playbackRate, setPlaybackRate] = useState(1)
    const [showControls, setShowControls] = useState(true)
    const [showSettings, setShowSettings] = useState(false)
    const [isLoading, setIsLoading] = useState(true)
    const [showMatureWarning, setShowMatureWarning] = useState(false)
    const [matureAccepted, setMatureAccepted] = useState(false)

    // HLS state
    const [hlsJob, setHlsJob] = useState<HLSJob | null>(null)
    const [hlsPolling, setHlsPolling] = useState(false)
    const [activeHlsUrl, setActiveHlsUrl] = useState<string | null>(null)
    const [hlsAvailable, setHlsAvailable] = useState(false)
    const [hlsReadyUrl, setHlsReadyUrl] = useState<string | null>(null)
    const hlsEnabled = useSettingsStore((s) => s.serverSettings?.features?.enableHLS ?? true)

    // Rating state (Feature 2)
    const [userRating, setUserRating] = useState(0)
    const [ratingHover, setRatingHover] = useState(0)

    const handleRate = useCallback((rating: number) => {
        setUserRating(rating)
        if (mediaId) {
            ratingsApi.record(mediaId, rating).catch(() => {
            })
        }
    }, [mediaId])

    // Controls visibility timeout
    const controlsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

    // Fetch media item — mediaApi.get() handles URL encoding internally; do not pre-encode
    const {data: media, isLoading: mediaLoading, error: mediaError} = useQuery({
        queryKey: ['media-item', mediaId],
        queryFn: () => mediaApi.get(mediaId),
        enabled: !!mediaId,
    })

    // Stable fallback callback — must not be recreated on every render or
    // useHLS will tear down and rebuild the HLS instance on every time-update.
    const onHlsFallback = useCallback(() => setActiveHlsUrl(null), [])

    // Attach hls.js for video HLS playback when a stream URL is available
    // IC-14: destructure isLoading/error so hls.js lifecycle feeds the spinner and error UI
    const {qualities: hlsQualities, currentQuality, selectQuality, isLoading: hlsIsLoading, error: hlsError} = useHLS(
        videoRef,
        media?.type === 'video' && hlsEnabled ? activeHlsUrl : null,
        onHlsFallback,
    )

    // IC-14: surface hls.js errors via toast so users aren't left with a silent black screen
    useEffect(() => {
        if (hlsError) showToast(hlsError, 'error')
    }, [hlsError, showToast])

    // Mark audio element as ready after mount so the EQ hook gets the real DOM node
    useEffect(() => {
        if (audioRef.current) setAudioReady(true)
    }, [])

    // Wire equalizer to the audio element (EQ only applies to audio content, not video)
    useEqualizer(audioReady && media?.type === 'audio' ? audioRef.current : null)

    // Fetch similar media via suggestions engine (semantic similarity by category/tags/type)
    const {data: similarData = [], isLoading: relatedLoading} = useQuery({
        queryKey: ['media-similar', mediaId],
        queryFn: () => suggestionsApi.getSimilar(mediaId ?? ''),
        enabled: !!mediaId,
        select: data => (data ?? []).slice(0, 8),
    })

    const related = similarData

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

        // Fetch saved position so handleLoadedMetadata can seek to it.
        // Also apply it directly if loadedmetadata already fired (remote DB latency
        // can make the fetch return AFTER the event — we're near the start if so).
        resumePositionRef.current = 0
        let positionFetchCancelled = false
        if (user) {
            const elRef = el
            watchHistoryApi.getPosition(mediaId)
                .then(data => {
                    if (positionFetchCancelled) return
                    const pos = data?.position ?? 0
                    resumePositionRef.current = pos
                    // If the video already loaded its metadata and we're still near the
                    // beginning, seek now (handles high-latency DB race)
                    if (pos > 5 && elRef.readyState >= 1 && elRef.duration > 0
                        && pos < elRef.duration - 5 && elRef.currentTime < 2) {
                        elRef.currentTime = pos
                    }
                })
                .catch(() => {
                })
        }

        // Track analytics — use media.id (UUID), not mediaId (file path)
        analyticsApi.trackEvent({type: 'view', media_id: media.id}).catch(() => {
        })

        return () => {
            positionFetchCancelled = true
        }
    }, [mediaId, media, matureAccepted]) // eslint-disable-line react-hooks/exhaustive-deps

    // Check HLS availability — show popup instead of auto-switching
    useEffect(() => {
        if (!mediaId || media?.type !== 'video' || !hlsEnabled) return
        hlsApi.check(mediaId).then(hls => {
            if (hls.available && hls.hls_url) {
                setHlsAvailable(true)
                setHlsReadyUrl(hls.hls_url)
            } else if (hls.job_id && hls.status === 'running') {
                setHlsJob({
                    id: hls.job_id,
                    // HLS job status (media_path removed from API)
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
        return () => clearInterval(interval)
    }, [hlsPolling, hlsJob])

    // Restore direct stream when HLS becomes inactive (fallback or user never switched).
    // hls.destroy() clears el.src; without this the video element is left with no source.
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

    // Derived media type — declared here (before any useEffect that reads them)
    // so TypeScript block-scoping rules are satisfied.
    const isAudio = media?.type === 'audio'
    const isVideo = media?.type === 'video'

    // Controls auto-hide (video only).
    // Uses isPlayingRef so the setTimeout callback always reads the *current* playing state
    // rather than the stale value captured when the callback was created.
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

    // Keep isPlayingRef in sync and auto-manage controls visibility when play state changes.
    // Without this, controls stay visible forever when the user presses play without
    // moving the mouse (resetControlsTimer is only called on mouse-move).
    useEffect(() => {
        if (!isVideo) return
        isPlayingRef.current = isPlaying
        if (isPlaying) {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
            controlsTimerRef.current = setTimeout(() => setShowControls(false), 3000)
        } else {
            if (controlsTimerRef.current) clearTimeout(controlsTimerRef.current)
            setShowControls(true)
        }
    }, [isPlaying, isVideo])

    // Save position when page is hidden (tab switch, browser close) so seeking then
    // closing without pausing still persists the watch position
    useEffect(() => {
        if (!mediaId) return

        function handleVisibilityChange() {
            if (document.visibilityState === 'hidden' && currentTimeRef.current > 0 && durationRef.current > 0) {
                watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
                })
            }
        }

        document.addEventListener('visibilitychange', handleVisibilityChange)
        return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
    }, [mediaId])

    function getActiveEl() {
        if (!media) return null
        return media.type === 'video' ? videoRef.current : audioRef.current
    }

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
            // Resume from saved position if it's meaningful (>5 s from start, >5 s from end)
            const saved = resumePositionRef.current
            if (saved > 5 && el.duration > 0 && saved < el.duration - 5) {
                el.currentTime = saved
            }
            resumePositionRef.current = 0
            el.play().catch(() => {
            })
        }
    }

    function handlePause() {
        setIsPlaying(false)
        // Save position on pause so navigation away after pausing doesn't lose progress
        if (mediaId && currentTimeRef.current > 0 && durationRef.current > 0) {
            watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
            })
        }
    }

    // Fired when the element's duration attribute changes (e.g. after HLS switches stream).
    // loadedmetadata can fire before hls.js sets the correct duration, so this catches
    // the subsequent update that arrives once the manifest is fully parsed.
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

    // Touch-drag seeking for mobile — prevents page scroll while dragging the progress bar
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

    function handleSeeked() {
        // Save position immediately after a seek so closing the tab/navigating away
        // doesn't lose the new position (periodic saves only fire during playback)
        if (mediaId && currentTimeRef.current > 5 && durationRef.current > 0) {
            watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
            })
        }
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
        setShowSettings(false)
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
        // Save watch position
        if (mediaId) {
            watchHistoryApi.trackPosition(mediaId, duration, duration).catch(() => {
            })
        }
    }

    // ST-03: position tracking below (interval + handlePause + handleEnded + visibilitychange)
    // duplicates the logic in useMediaPosition hook. PlayerPage uses a ref-based auto-seek
    // approach (resumePositionRef) rather than useMediaPosition's dialog-based resume flow,
    // so consolidation requires aligning the two UX patterns first.

    // Save position periodically — read from refs so the interval is not recreated on
    // every timeupdate tick (which would reset the timer and make it never fire)
    useEffect(() => {
        if (!mediaId || !isPlaying || !duration) return
        const interval = setInterval(() => {
            if (durationRef.current > 0 && currentTimeRef.current > 0) {
                watchHistoryApi.trackPosition(mediaId, currentTimeRef.current, durationRef.current).catch(() => {
                })
            }
        }, 30000)
        return () => clearInterval(interval)
    }, [mediaId, isPlaying, duration])

    // Keyboard shortcuts — all handlers operate on the DOM element directly via
    // getActiveEl(), so this effect only needs to re-attach when the media type changes.
    useEffect(() => {
        function onKeyDown(e: KeyboardEvent) {
            if ((e.target as HTMLElement).tagName === 'INPUT' || (e.target as HTMLElement).tagName === 'TEXTAREA') return
            const el = getActiveEl()
            switch (e.key) {
                case ' ':
                case 'k':
                case 'K':
                    e.preventDefault()
                    if (el) {
                        el.paused ? el.play().catch(() => {
                        }) : el.pause()
                    }
                    break
                case 'ArrowLeft':
                    e.preventDefault()
                    if (el) el.currentTime = Math.max(0, el.currentTime - 10)
                    break
                case 'ArrowRight':
                    e.preventDefault()
                    if (el) el.currentTime = Math.min(el.duration, el.currentTime + 10)
                    break
                case 'ArrowUp':
                    e.preventDefault()
                    if (el) el.volume = Math.min(1, el.volume + 0.1)
                    break
                case 'ArrowDown':
                    e.preventDefault()
                    if (el) el.volume = Math.max(0, el.volume - 0.1)
                    break
                case 'm':
                case 'M':
                    e.preventDefault()
                    if (el) {
                        el.muted = !el.muted;
                        setIsMuted(el.muted)
                    }
                    break
                case 'f':
                case 'F':
                    e.preventDefault()
                    handleFullscreen()
                    break
            }
        }

        document.addEventListener('keydown', onKeyDown)
        return () => document.removeEventListener('keydown', onKeyDown)
    }, [media?.type])

    const progress = duration > 0 ? (currentTime / duration) * 100 : 0

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
        return (
            <div className="player-page">
                <div className="player-page-container">
                    <div className="player-header">
                        <Link to="/" className="player-back-btn"><i className="bi bi-arrow-left"/> Back to
                            Library</Link>
                    </div>
                    <div style={{textAlign: 'center', padding: '60px 0'}}>
                        <p style={{color: '#ef4444', marginBottom: 12}}>Media not found or unavailable.</p>
                        <Link to="/"><i className="bi bi-arrow-left"/> Back to Library</Link>
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
                                <button className="media-action-btn" onClick={() => navigate('/')}>
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

            <div className="player-page-container">
                <div className="player-header">
                    <Link to="/" className="player-back-btn"><i className="bi bi-arrow-left"/> Back to Library</Link>
                </div>

                <div className="player-layout">
                    {/* Main player column */}
                    <div className="player-main">
                        {/* Video / Audio container */}
                        <div
                            className={`video-wrapper${isVideo && isPlaying && !showControls ? ' playing-idle' : ''}`}
                            onMouseMove={isVideo ? resetControlsTimer : undefined}
                            onMouseLeave={isVideo && isPlaying ? () => setShowControls(false) : undefined}
                            onClick={isVideo ? togglePlay : undefined}
                        >
                            {/* Hidden audio element for audio type */}
                            {isAudio && (
                                <>
                                    <audio
                                        ref={audioRef}
                                        onTimeUpdate={handleTimeUpdate}
                                        onLoadedMetadata={handleLoadedMetadata}
                                        onDurationChange={handleDurationChange}
                                        onPlay={() => setIsPlaying(true)}
                                        onPause={handlePause}
                                        onEnded={handleEnded}
                                        onSeeked={handleSeeked}
                                        onWaiting={() => setIsLoading(true)}
                                        onCanPlay={() => setIsLoading(false)}
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
                                    onPlay={() => setIsPlaying(true)}
                                    onPause={handlePause}
                                    onEnded={handleEnded}
                                    onSeeked={handleSeeked}
                                    onWaiting={() => setIsLoading(true)}
                                    onCanPlay={() => setIsLoading(false)}
                                    preload="auto"
                                />
                            )}

                            {/* Loading spinner — covers both native buffering and hls.js level-loading (IC-14) */}
                            {(isLoading || hlsIsLoading) && (
                                <div className="player-loading"><i className="bi bi-arrow-repeat"/></div>
                            )}

                            {/* Custom controls */}
                            <div
                                className={`custom-controls ${showControls || !isPlaying ? 'show' : ''}`}
                                onClick={e => e.stopPropagation()}
                            >
                                {/* Progress bar */}
                                <div className="ctrl-progress-container" onClick={handleProgressClick}
                                     onTouchStart={handleProgressTouch} onTouchMove={handleProgressTouch}>
                                    <div className="ctrl-buffer-bar" style={{width: `${buffered}%`}}/>
                                    <div className="ctrl-progress-fill" style={{width: `${progress}%`}}/>
                                </div>

                                {/* Controls row */}
                                <div className="ctrl-row">
                                    <button className="ctrl-btn" onClick={togglePlay} title="Play/Pause (Space)">
                                        {isPlaying ? <i className="bi bi-pause-fill"/> :
                                            <i className="bi bi-play-fill"/>}
                                    </button>
                                    <button className="ctrl-btn" onClick={() => {
                                        const el = getActiveEl();
                                        if (el) el.currentTime = Math.max(0, el.currentTime - 10)
                                    }} title="Skip back 10s (←)">
                                        <i className="bi bi-skip-backward-fill"/>10
                                    </button>
                                    <button className="ctrl-btn" onClick={() => {
                                        const el = getActiveEl();
                                        if (el) el.currentTime = Math.min(el.duration, el.currentTime + 10)
                                    }} title="Skip forward 10s (→)">
                                        10<i className="bi bi-skip-forward-fill"/>
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
                                            onClick={e => e.stopPropagation()}
                                        />
                                    </div>
                                    <span
                                        className="ctrl-time">{formatDuration(currentTime)} / {formatDuration(duration)}</span>
                                    <div className="ctrl-spacer"/>
                                    <button
                                        className={`ctrl-btn ${isLooping ? 'active' : ''}`}
                                        onClick={toggleLoop}
                                        title="Toggle Loop"
                                    >
                                        <i className="bi bi-repeat"/>
                                    </button>
                                    {isVideo && (
                                        <button className="ctrl-btn" onClick={handlePiP} title="Picture-in-Picture">
                                            <i className="bi bi-pip-fill"/>
                                        </button>
                                    )}
                                    <button className="ctrl-btn" onClick={() => setShowSettings(s => !s)}
                                            title="Settings">
                                        <i className="bi bi-gear-fill"/>
                                    </button>
                                    {isVideo && hlsQualities.length > 1 && (
                                        <select
                                            className="ctrl-quality-select"
                                            value={currentQuality}
                                            onChange={e => selectQuality(Number(e.target.value))}
                                            onClick={e => e.stopPropagation()}
                                            title="Video quality"
                                        >
                                            <option value={-1}>Auto</option>
                                            {hlsQualities.map(q => (
                                                <option key={q.index} value={q.index}>{q.name}</option>
                                            ))}
                                        </select>
                                    )}
                                    {isVideo && (
                                        <button className="ctrl-btn" onClick={handleFullscreen} title="Fullscreen (F)">
                                            <i className="bi bi-fullscreen"/>
                                        </button>
                                    )}
                                </div>

                                {/* Settings menu */}
                                {showSettings && (
                                    <div className="ctrl-settings-menu" onClick={e => e.stopPropagation()}>
                                        <div className="ctrl-settings-item"
                                             style={{cursor: 'default', fontWeight: 600}}>
                                            <span>Playback Speed</span>
                                            <span>{playbackRate}x</span>
                                        </div>
                                        {[0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2].map(speed => (
                                            <div
                                                key={speed}
                                                className={`ctrl-settings-item ${playbackRate === speed ? 'active' : ''}`}
                                                onClick={() => setSpeed(speed)}
                                            >
                                                {speed === 1 ? 'Normal' : `${speed}x`}
                                            </div>
                                        ))}
                                    </div>
                                )}
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
                                        onTouchStart={handleProgressTouch}
                                        onTouchMove={handleProgressTouch}
                                    >
                                        <div className="ctrl-progress-fill"
                                             style={{width: `${progress}%`, background: '#667eea'}}/>
                                    </div>
                                </div>
                                {/* Controls row */}
                                <div style={{display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap'}}>
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
                                    {/* Speed cycle button: tap to step through 0.5→0.75→1→1.25→1.5→2 */}
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
                                        onClick={() => setHlsAvailable(false)}
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
                                    <span><i className="bi bi-aspect-ratio"/> {media.width}×{media.height}</span>}
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
                            <h3><i className="bi bi-play-fill"/> Similar Media</h3>
                            {relatedLoading ? (
                                <p style={{color: 'var(--text-muted)', fontSize: 13}}>Loading...</p>
                            ) : related.length === 0 ? (
                                <p style={{color: 'var(--text-muted)', fontSize: 13}}>No similar media found.</p>
                            ) : (
                                related.map(entry => <SimilarItem key={entry.media_id} entry={entry}/>)
                            )}
                        </div>

                        {/* Star Rating (Feature 2) */}
                        {user && (
                            <div className="player-sidebar-card">
                                <h3><i className="bi bi-star-fill"/> Rate This</h3>
                                <div style={{display: 'flex', gap: 4, marginTop: 4}}>
                                    {[1, 2, 3, 4, 5].map(star => (
                                        <button
                                            key={star}
                                            onClick={() => handleRate(star)}
                                            onMouseEnter={() => setRatingHover(star)}
                                            onMouseLeave={() => setRatingHover(0)}
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
                    </div>
                    </SectionErrorBoundary>
                </div>
            </div>
        </div>
    )
}
